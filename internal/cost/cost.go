// Package cost counts what claude's work costs at API prices (SPEC F18).
// Claude Code writes every reply to a transcript, one JSON line per
// content block, with the model, the time and the token counts (usage).
// A script on the agent host reads the transcripts of a session's project
// folder (~/.claude/projects/<the folder's path, - for every other
// character>, and the folder of the transcript its hooks recorded, see
// internal/idle) from where it stopped the last time, and prints only the
// head (model, id) and the tail (usage, time) of the reply lines. The
// tokens are summed per transcript, day and model in a Ledger; dollars are
// computed when shown, from the price table, so a price changed later
// counts for the past too. Counted is everything in those folders,
// including claude runs the app did not start.
package cost

import (
	"bufio"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"session-workbench/internal/config"
	"session-workbench/internal/remote"
)

// Prices are the API prices in dollars per million tokens (as of
// 2026-11-03); the config's [prices."<model>"] adds to and overrides them.
// A model is priced by the longest key it starts with.
var Prices = map[string]config.Price{
	"claude-fable-5-2":  {Input: 8, Output: 40, CacheRead: 0.20},
	"claude-mythos-5-2": {Input: 8, Output: 40, CacheRead: 0.20},
	"claude-fable-5-1":  {Input: 10, Output: 50},
	"claude-mythos-5-1": {Input: 10, Output: 50},
	"claude-fable-5":    {Input: 10, Output: 50},
	"claude-opus-5-5":   {Input: 4, Output: 20, CacheRead: 0.20},
	"claude-opus-5":     {Input: 5, Output: 25},
	"claude-opus-4-8":   {Input: 5, Output: 25},
	"claude-opus-4-7":   {Input: 5, Output: 25},
	"claude-opus-4-6":   {Input: 5, Output: 25},
	"claude-sonnet-5-5": {Input: 2, Output: 10, CacheRead: 0.10},
	"claude-sonnet-5":   {Input: 2, Output: 10},
	"claude-sonnet-4-6": {Input: 3, Output: 15},
	"claude-haiku-5-5":  {Input: 0.10, Output: 0.50, LongAbove: 100000, LongInput: 0.50, LongOutput: 2.50},
	"claude-haiku-4-5":  {Input: 1, Output: 5},
}

// Table is the price table: Prices with the config's own on top.
func Table(own map[string]config.Price) map[string]config.Price {
	t := maps.Clone(Prices)
	maps.Copy(t, own)
	return t
}

// find returns the price of model: the longest key it starts with.
func find(t map[string]config.Price, model string) (config.Price, bool) {
	best, ok := "", false
	for k := range t {
		if strings.HasPrefix(model, k) && len(k) >= len(best) {
			best, ok = k, true
		}
	}
	return t[best], ok
}

// Tokens of one model on one day (summed).
type Tokens struct {
	Input  int64 `json:"in,omitempty"`
	Write5 int64 `json:"w5,omitempty"` // cache writes kept 5 minutes
	Write1 int64 `json:"w1,omitempty"` // cache writes kept 1 hour
	Read   int64 `json:"cr,omitempty"` // cache reads
	Output int64 `json:"out,omitempty"`
	Web    int64 `json:"web,omitempty"` // web searches
}

func (t *Tokens) add(o Tokens, sign int64) {
	t.Input += sign * o.Input
	t.Write5 += sign * o.Write5
	t.Write1 += sign * o.Write1
	t.Read += sign * o.Read
	t.Output += sign * o.Output
	t.Web += sign * o.Web
}

func (t Tokens) zero() bool { return t == Tokens{} }

// Model keys in the ledger: the model, plus ":fast" (fast mode) and
// ":long" (a prompt above the price's LongAbove).
func split(key string) (model string, fast, long bool) {
	model, fast = strings.CutSuffix(key, ":fast")
	model, long = strings.CutSuffix(model, ":long")
	return model, fast, long
}

// Dollars is what t costs for the ledger key under the price table; ok is
// false for a model without a price.
func Dollars(table map[string]config.Price, key string, t Tokens) (float64, bool) {
	model, fast, long := split(key)
	p, ok := find(table, model)
	if !ok {
		return 0, t.zero()
	}
	in, out := p.Input, p.Output
	if long && p.LongInput > 0 {
		in, out = p.LongInput, p.LongOutput
	}
	read, w5, w1, web := p.CacheRead, p.CacheWrite5m, p.CacheWrite1h, p.WebSearch
	if read == 0 {
		read = in / 10
	}
	if w5 == 0 {
		w5 = in * 1.25
	}
	if w1 == 0 {
		w1 = in * 2
	}
	if web == 0 {
		web = 10
	}
	d := (float64(t.Input)*in + float64(t.Write5)*w5 + float64(t.Write1)*w1 +
		float64(t.Read)*read + float64(t.Output)*out) / 1e6
	if fast {
		f := p.Fast
		if f == 0 {
			f = 2
		}
		d *= f
	}
	return d + float64(t.Web)*web/1000, true
}

// Item is a session to count: its id and its folder on the agent host
// ("" = the home folder).
type Item struct{ ID, Dir string }

// fields of a reply line (head + tail of it, see Script).
var (
	reModel = regexp.MustCompile(`"model":"([^"]*)"`)
	reID    = regexp.MustCompile(`"id":"([^"]*)"`)
	reTime  = regexp.MustCompile(`"timestamp":"([^"]*)"`)
	reSpeed = regexp.MustCompile(`"speed":"([^"]*)"`)
	reNum   = map[string]*regexp.Regexp{}
)

func init() {
	for _, n := range []string{"input_tokens", "output_tokens", "cache_creation_input_tokens",
		"cache_read_input_tokens", "ephemeral_5m_input_tokens", "ephemeral_1h_input_tokens", "web_search_requests"} {
		reNum[n] = regexp.MustCompile(`"` + n + `":(\d+)`)
	}
}

// num is the first count called name in s (0 if none).
func num(s, name string) int64 {
	m := reNum[name].FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	return n
}

// Reply is one reply of claude, from a line of the script.
type Reply struct {
	ID     string
	Model  string
	Time   time.Time
	Fast   bool
	Tokens Tokens
}

// parseReply reads the head and tail of a reply line; ok is false when it
// has no id or no usage.
func parseReply(s string) (Reply, bool) {
	i := strings.Index(s, `"usage":{`)
	if i < 0 {
		return Reply{}, false
	}
	head, tail := s[:i], s[i:]
	var r Reply
	if m := reID.FindStringSubmatch(head); m != nil {
		r.ID = m[1]
	}
	if m := reModel.FindStringSubmatch(head); m != nil {
		r.Model = m[1]
	}
	if r.ID == "" {
		return r, false
	}
	if m := reTime.FindStringSubmatch(tail); m != nil {
		r.Time, _ = time.Parse(time.RFC3339Nano, m[1])
	} else if m := reTime.FindStringSubmatch(head); m != nil {
		r.Time, _ = time.Parse(time.RFC3339Nano, m[1])
	}
	if m := reSpeed.FindStringSubmatch(tail); m != nil {
		r.Fast = m[1] == "fast"
	}
	// The usage object first: its own counts come before "iterations".
	t := Tokens{
		Input:  num(tail, "input_tokens"),
		Read:   num(tail, "cache_read_input_tokens"),
		Output: num(tail, "output_tokens"),
		Web:    num(tail, "web_search_requests"),
	}
	w := num(tail, "cache_creation_input_tokens")
	t.Write5, t.Write1 = num(tail, "ephemeral_5m_input_tokens"), num(tail, "ephemeral_1h_input_tokens")
	if t.Write5+t.Write1 != w {
		t.Write5, t.Write1 = w, 0 // no split: the 5 minute price
	}
	r.Tokens = t
	return r, true
}

// key is the ledger key of r: the model with ":long" and ":fast".
func (r Reply) key(table map[string]config.Price) string {
	k := r.Model
	if p, ok := find(table, r.Model); ok && p.LongAbove > 0 &&
		r.Tokens.Input+r.Tokens.Write5+r.Tokens.Write1+r.Tokens.Read > int64(p.LongAbove) {
		k += ":long"
	}
	if r.Fast {
		k += ":fast"
	}
	return k
}

// awk prints, of every reply line of a transcript chunk, its start (bytes
// into the chunk) and its head (up to "content") and tail (from the last
// "usage"), and at the end the start of the last line.
const awkProg = `{ start = pos; pos += length($0) + 1; last = start
  if (index($0, "\"type\":\"assistant\"") == 0) next
  c = index($0, "\"content\":"); if (c == 0) next
  s = $0; u = 0
  while ((i = index(s, "\"usage\":{")) > 0) { u += i; s = substr(s, i + 1) }
  if (u <= c) next
  printf "L\t%d\t%s%s\n", start, substr($0, 1, c - 1), substr($0, u)
}
END { printf "E\t%d\n", last }`

// Script reads, on one agent host, the transcripts of items' project
// folders: known are the offsets read so far by path. It prints
// "D <id> <folder>" for every folder of a session, then for every
// transcript that grew "F <from> <size> <ends with newline 0|1> <path>",
// its reply lines ("L <start> <head+tail>") and "E <start of the last line>".
func Script(items []Item, known map[string]int64, stateDir string) (string, error) {
	var b strings.Builder
	b.WriteString(`LC_ALL=C; export LC_ALL
P="$HOME/.claude/projects"; S="` + stateDir + `"
T=$(mktemp -d) || exit 1
trap 'rm -rf "$T"' EXIT
tab=$(printf '\t')
cat > "$T/k" <<'SW_KNOWN'
`)
	for _, p := range slices.Sorted(maps.Keys(known)) {
		if strings.ContainsAny(p, "\t\n") {
			continue
		}
		fmt.Fprintf(&b, "%d\t%s\n", known[p], p)
	}
	b.WriteString(`SW_KNOWN
enc() { printf '%s' "$1" | sed 's/[^a-zA-Z0-9]/-/g'; }
dirs() {
  id=$1; d=$2
  case $d in "" | "~") d=$HOME ;; "~/"*) d=$HOME/${d#"~/"} ;; esac
  for w in "$(cd "$d" 2>/dev/null && pwd)" "$(cd "$d" 2>/dev/null && pwd -P)"; do
    [ -n "$w" ] || continue
    e=$(enc "$w")
    if [ ${#e} -le 200 ]; then
      [ -d "$P/$e" ] && printf 'D\t%s\t%s\n' "$id" "$P/$e"
    else
      for x in "$P/$(printf '%s' "$e" | cut -c1-200)"-*; do [ -d "$x" ] && printf 'D\t%s\t%s\n' "$id" "$x"; done
    fi
  done
  if [ -f "$S/$id" ]; then
    t=$(sed -n '2,$p' "$S/$id" | grep -o '"transcript_path":"[^"]*"' | head -n 1 | sed 's/^"transcript_path":"//; s/"$//')
    [ -n "$t" ] && [ -d "$(dirname "$t")" ] && printf 'D\t%s\t%s\n' "$id" "$(dirname "$t")"
  fi
  return 0
}
{
`)
	for _, it := range items {
		if !remote.ValidID(it.ID) {
			return "", fmt.Errorf("session id %q", it.ID)
		}
		fmt.Fprintf(&b, "dirs %s %s\n", it.ID, remote.Quote(it.Dir))
	}
	b.WriteString(`} | sort -u > "$T/d"
cat "$T/d"
cut -f3 "$T/d" | sort -u | while IFS= read -r pd; do find "$pd/" -name '*.jsonl' -type f; done |
  sed 's#//*#/#g' | sort -u | while IFS= read -r f; do printf '%s\t%s\n' "$(wc -c < "$f" | tr -d ' ')" "$f"; done > "$T/s"
awk -F'\t' 'FILENAME == ARGV[1] { k[$2] = $1; next }
  { o = ($2 in k) ? k[$2] + 0 : 0; if ($1 + 0 > o) print o "\t" $1 "\t" $2; else if ($1 + 0 < o) print 0 "\t" $1 "\t" $2 }' "$T/k" "$T/s" > "$T/j"
while IFS="$tab" read -r o s f; do
  n=0
  [ "$(tail -c +"$s" "$f" | head -c 1 | od -An -tx1 | tr -d ' \n')" = 0a ] && n=1
  printf 'F\t%s\t%s\t%s\t%s\n' "$o" "$s" "$n" "$f"
  tail -c +"$((o + 1))" "$f" | head -c "$((s - o))" | awk '` + awkProg + `'
done < "$T/j"
`)
	return b.String(), nil
}

// Chunk is what the script read of one transcript.
type Chunk struct {
	Path    string
	From    int64 // the offset it read from (0 after the file shrank)
	Size    int64
	Next    int64 // where to read from next time
	Replies []Reply
}

// Result is the script's output: the folders of each session and the
// chunks read.
type Result struct {
	Dirs   map[string][]string // session id → project folders
	Chunks []Chunk
}

// Parse reads the script's output.
func Parse(out string) Result {
	res := Result{Dirs: map[string][]string{}}
	type line struct {
		start int64
		text  string
	}
	var cur *Chunk
	var nl bool
	var lines []line
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 1<<16), 1<<26)
	for sc.Scan() {
		f := strings.SplitN(sc.Text(), "\t", 5)
		switch {
		case f[0] == "D" && len(f) == 3:
			if !slices.Contains(res.Dirs[f[1]], path.Clean(f[2])) {
				res.Dirs[f[1]] = append(res.Dirs[f[1]], path.Clean(f[2]))
			}
		case f[0] == "F" && len(f) == 5:
			from, _ := strconv.ParseInt(f[1], 10, 64)
			size, _ := strconv.ParseInt(f[2], 10, 64)
			cur, nl, lines = &Chunk{Path: f[4], From: from, Size: size}, f[3] == "1", nil
		case f[0] == "L" && len(f) >= 3 && cur != nil:
			start, _ := strconv.ParseInt(f[1], 10, 64)
			lines = append(lines, line{start, strings.Join(f[2:], "\t")})
		case f[0] == "E" && len(f) == 2 && cur != nil:
			last, _ := strconv.ParseInt(f[1], 10, 64)
			cur.Next = cur.Size
			if !nl {
				cur.Next = cur.From + last // the last line is still being written
			}
			for _, l := range lines {
				if !nl && l.start >= last {
					continue
				}
				if r, ok := parseReply(l.text); ok {
					cur.Replies = append(cur.Replies, r)
				}
			}
			res.Chunks = append(res.Chunks, *cur)
			cur = nil
		}
	}
	return res
}
