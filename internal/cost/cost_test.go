package cost

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"session-workbench/internal/config"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestDollars(t *testing.T) {
	tb := Table(nil)
	// Opus 5.5: $4 in, $20 out, $0.20 cache read; writes 1.25× / 2× of in.
	d, ok := Dollars(tb, "claude-opus-5-5", Tokens{Input: 1e6, Output: 1e6, Read: 1e6, Write5: 1e6, Write1: 1e6, Web: 1000})
	if !ok || !near(d, 4+20+0.20+5+8+10) {
		t.Fatalf("opus: %v %v", d, ok)
	}
	if d, _ := Dollars(tb, "claude-opus-5-5:fast", Tokens{Output: 1e6}); !near(d, 40) {
		t.Fatalf("fast: %v", d)
	}
	// Longest prefix: claude-opus-5 must not price claude-opus-5-5.
	if d, _ := Dollars(tb, "claude-opus-5-20270101", Tokens{Input: 1e6}); !near(d, 5) {
		t.Fatalf("opus 5 dated: %v", d)
	}
	if d, _ := Dollars(tb, "claude-haiku-5-5:long", Tokens{Input: 1e6, Output: 1e6}); !near(d, 3) {
		t.Fatalf("haiku long: %v", d)
	}
	// Cache read defaults to a tenth of input.
	if d, _ := Dollars(tb, "claude-sonnet-4-6", Tokens{Read: 1e6}); !near(d, 0.3) {
		t.Fatalf("sonnet 4.6 read: %v", d)
	}
	if _, ok := Dollars(tb, "claude-new-9", Tokens{Input: 1}); ok {
		t.Fatal("unknown model priced")
	}
	if _, ok := Dollars(tb, "<synthetic>", Tokens{}); !ok {
		t.Fatal("nothing used is not unpriced")
	}
	own := Table(map[string]config.Price{"claude-new-9": {Input: 1, Output: 2}, "claude-opus-5-5": {Input: 1, Output: 1}})
	if d, ok := Dollars(own, "claude-new-9", Tokens{Output: 1e6}); !ok || !near(d, 2) {
		t.Fatalf("own: %v", d)
	}
	if d, _ := Dollars(own, "claude-opus-5-5", Tokens{Input: 1e6}); !near(d, 1) {
		t.Fatalf("override: %v", d)
	}
}

// line is a transcript line of a reply, as Claude Code writes it (the
// content has a tool call whose input has its own "model" and "usage").
func line(id, model, ts string, in, w5, w1, read, out int, fast bool) string {
	speed := "standard"
	if fast {
		speed = "fast"
	}
	return fmt.Sprintf(`{"parentUuid":"p","isSidechain":false,"cwd":"/x","message":{"model":%q,"id":%q,"type":"message","role":"assistant",`+
		`"content":[{"type":"text","text":"about \"usage\":{\"input_tokens\":99}"},{"type":"tool_use","name":"Agent","input":{"model":"haiku","usage":{"input_tokens":77}}}],`+
		`"stop_reason":null,"usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,`+
		`"server_tool_use":{"web_search_requests":0},"cache_creation":{"ephemeral_1h_input_tokens":%d,"ephemeral_5m_input_tokens":%d},`+
		`"iterations":[{"input_tokens":1,"output_tokens":1}],"speed":%q}},"requestId":"r","type":"assistant","uuid":"u","timestamp":%q}`+"\n",
		model, id, in, w5+w1, read, out, w1, w5, speed, ts)
}

const userLine = `{"type":"user","message":{"role":"user","content":"hi \"type\":\"assistant\" \"usage\":{"},"timestamp":"2026-11-10T10:00:00Z"}` + "\n"

func TestParseReply(t *testing.T) {
	l := strings.TrimSpace(line("msg_1", "claude-opus-5-5", "2026-11-10T10:00:00.123Z", 2, 100, 50, 1000, 30, true))
	c := strings.Index(l, `"content":`)
	u := strings.LastIndex(l, `"usage":{`)
	r, ok := parseReply(l[:c] + l[u:])
	want := Tokens{Input: 2, Write5: 100, Write1: 50, Read: 1000, Output: 30}
	if !ok || r.ID != "msg_1" || r.Model != "claude-opus-5-5" || !r.Fast || r.Tokens != want || r.Time.IsZero() {
		t.Fatalf("%+v", r)
	}
	if r.key(Table(nil)) != "claude-opus-5-5:fast" {
		t.Fatal(r.key(Table(nil)))
	}
}

// runScript runs Script with home as $HOME.
func runScript(t *testing.T, home string, items []Item, known map[string]int64) Result {
	t.Helper()
	sc, err := Script(items, known, "$HOME/state", testHelper)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", sc)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	return Parse(string(out))
}

const testHelper = "#!/bin/sh\necho test helper\n"

func enc(p string) string { return regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(p, "-") }

func TestScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the agent host runs sh")
	}
	home := t.TempDir()
	work := filepath.Join(home, "work", "my_proj.x")
	os.MkdirAll(work, 0o755)
	// The project folder is a symlink (as when transcripts are kept in a repo).
	real := filepath.Join(home, "elsewhere")
	os.MkdirAll(filepath.Join(real, "conv1", "subagents"), 0o755)
	proj := filepath.Join(home, ".claude", "projects", enc(work))
	os.MkdirAll(filepath.Dir(proj), 0o755)
	if err := os.Symlink(real, proj); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(real, "conv1.jsonl")
	// msg_1 twice (two content blocks; the second line has the final
	// count), msg_2, and msg_3 still being written.
	l3 := line("msg_3", "claude-sonnet-5-5", "2026-11-10T12:00:00Z", 1, 0, 0, 0, 7, false)
	os.WriteFile(a, []byte(userLine+
		line("msg_1", "claude-opus-5-5", "2026-11-09T23:30:00Z", 10, 100, 0, 1000, 5, false)+
		line("msg_1", "claude-opus-5-5", "2026-11-09T23:30:00Z", 10, 100, 0, 1000, 50, false)+userLine+
		line("msg_2", "claude-opus-5-5", "2026-11-10T11:00:00Z", 1, 0, 200, 0, 20, false)+l3[:40]), 0o644)
	sub := filepath.Join(real, "conv1", "subagents", "agent-a.jsonl")
	os.WriteFile(sub, []byte(line("msg_s", "claude-haiku-5-5", "2026-11-10T11:30:00Z", 5, 0, 0, 0, 5, false)), 0o644)
	// A second session whose hooks recorded a transcript in another folder.
	other := filepath.Join(home, ".claude", "projects", "custom")
	os.MkdirAll(other, 0o755)
	b := filepath.Join(other, "conv2.jsonl")
	os.WriteFile(b, []byte(line("msg_b", "claude-new-9", "2026-11-10T09:00:00Z", 1, 0, 0, 0, 1, false)), 0o644)
	os.MkdirAll(filepath.Join(home, "state"), 0o755)
	os.WriteFile(filepath.Join(home, "state", "s-2"), []byte("1 Stop 1 %1\n"+`{"session_id":"c","transcript_path":"`+b+`"}`), 0o644)

	items := []Item{{"s-1", "~/work/my_proj.x"}, {"s-2", "/nonexistent"}}
	res := runScript(t, home, items, nil)
	if got := res.Dirs["s-1"]; len(got) != 1 || got[0] != proj {
		t.Fatalf("dirs s-1: %q (want %q)", got, proj)
	}
	if got := res.Dirs["s-2"]; len(got) != 1 || got[0] != other {
		t.Fatalf("dirs s-2: %q", got)
	}
	if len(res.Chunks) != 3 {
		t.Fatalf("chunks: %+v", res.Chunks)
	}
	l := newLedger(&Ledger{})
	now := time.Date(2026, 11, 10, 13, 0, 0, 0, time.Local)
	tb := Table(nil)
	l.Apply("h", []string{"s-1", "s-2"}, res, tb, now)
	fa := l.Files["h\t"+filepath.Join(proj, "conv1.jsonl")]
	if fa == nil {
		t.Fatalf("files: %v", l.Files)
	}
	st, _ := os.Stat(a)
	if fa.Off != st.Size()-40 { // up to the line being written
		t.Fatalf("offset %d of %d", fa.Off, st.Size())
	}
	sum := l.all([]*File{fa}, "")
	if got := sum["claude-opus-5-5"]; got != (Tokens{Input: 11, Write5: 100, Write1: 200, Read: 1000, Output: 70}) {
		t.Fatalf("opus: %+v", got)
	}
	s := l.Summary(tb, now)
	if len(s.Unpriced) != 1 || s.Unpriced[0] != "claude-new-9" || !s.TodayPartial || !s.TotalPartial {
		t.Fatalf("unpriced %v", s.Unpriced)
	}
	opus1, _ := Dollars(tb, "claude-opus-5-5", Tokens{Input: 10, Write5: 100, Read: 1000, Output: 50})
	opus2, _ := Dollars(tb, "claude-opus-5-5", Tokens{Input: 1, Write1: 200, Output: 20})
	haiku, _ := Dollars(tb, "claude-haiku-5-5", Tokens{Input: 5, Output: 5})
	today := opus2 + haiku
	if Day(time.Date(2026, 11, 9, 23, 30, 0, 0, time.UTC)) == Day(now) { // east of UTC
		today += opus1
	}
	if !near(s.Total, opus1+opus2+haiku) || !near(s.Today, today) {
		t.Fatalf("today %v total %v, want %v %v", s.Today, s.Total, today, opus1+opus2+haiku)
	}
	if got := s.Sessions["s-1"]; !near(got.Total, s.Total) {
		t.Fatalf("s-1 %+v", got)
	}

	// The rest of msg_3 arrives, plus another line of msg_2 (its final
	// count): only the new bytes are read.
	f, _ := os.OpenFile(a, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(l3[40:])
	f.Close()
	res = runScript(t, home, items, l.Known("h"))
	if len(res.Chunks) != 1 || res.Chunks[0].From != fa.Off || len(res.Chunks[0].Replies) != 1 {
		t.Fatalf("second read: %+v", res.Chunks)
	}
	l.Apply("h", []string{"s-1", "s-2"}, res, tb, now)
	if got := l.all([]*File{fa}, "")["claude-sonnet-5-5"]; got.Output != 7 {
		t.Fatalf("sonnet %+v", got)
	}
	// Nothing new: nothing read.
	if res = runScript(t, home, items, l.Known("h")); len(res.Chunks) != 0 {
		t.Fatalf("third read: %+v", res.Chunks)
	}

	// Reset: the total starts again, today stays.
	before := l.Summary(tb, now)
	l.ResetTotal(now)
	if s := l.Summary(tb, now); !near(s.Total, 0) || !near(s.Today, before.Today) || s.Since == "" || s.TotalPartial || !s.TodayPartial {
		t.Fatalf("after reset %+v", s)
	}

	// The transcript shrinks (made anew): the old counts stay, it is read
	// from the start.
	os.WriteFile(a, []byte(line("msg_n", "claude-opus-5-5", "2026-11-10T12:30:00Z", 0, 0, 0, 0, 1000, false)), 0o644)
	res = runScript(t, home, items, l.Known("h"))
	if len(res.Chunks) != 1 || res.Chunks[0].From != 0 {
		t.Fatalf("shrunk: %+v", res.Chunks)
	}
	l.Apply("h", []string{"s-1", "s-2"}, res, tb, now)
	archived := 0
	for k := range l.Files {
		if strings.Contains(k, "\t~") {
			archived++
		}
	}
	n, _ := Dollars(tb, "claude-opus-5-5", Tokens{Output: 1000})
	if s := l.Summary(tb, now); archived != 1 || !near(s.Total, n) || !near(s.Today, before.Today+n) {
		t.Fatalf("archived %d, %+v", archived, s)
	}

	// The status line's helper is installed (made executable), and the
	// usage saved on the host comes back with its age.
	helper := filepath.Join(home, "state", "statusline")
	if b, err := os.ReadFile(helper); err != nil || string(b) != testHelper {
		t.Fatalf("helper: %q %v", b, err)
	}
	if st, _ := os.Stat(helper); st.Mode()&0o100 == 0 {
		t.Error("helper not executable")
	}
	if res.Usage != nil {
		t.Errorf("usage without a file: %+v", res.Usage)
	}
	os.WriteFile(filepath.Join(home, "state", "usage.json"), []byte(`{"model":{"id":"x"},"rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":1738425600},"seven_day":{"used_percentage":41.2,"resets_at":1738857600}}}`+"\n"), 0o644)
	old := time.Now().Add(-10 * time.Minute)
	os.Chtimes(filepath.Join(home, "state", "usage.json"), old, old)
	res = runScript(t, home, items, l.Known("h"))
	if u := res.Usage; u == nil || u.FiveHour == nil || u.FiveHour.Used != 23.5 || u.SevenDay.ResetsAt != 1738857600 ||
		time.Since(time.Unix(u.At, 0)) < 9*time.Minute || time.Since(time.Unix(u.At, 0)) > 11*time.Minute {
		t.Errorf("usage: %+v", res.Usage)
	}

	// Saved and read back.
	file := filepath.Join(t.TempDir(), "cost.json")
	if err := l.Save(file); err != nil {
		t.Fatal(err)
	}
	l2, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := json.Marshal(l.Summary(tb, now))
	b2, _ := json.Marshal(l2.Summary(tb, now))
	if string(b1) != string(b2) {
		t.Fatalf("%s\n%s", b1, b2)
	}
	l2.Keep([]string{"s-2"})
	if _, ok := l2.Dirs["s-1"]; ok {
		t.Fatal("s-1 kept")
	}
}

// TestRealTranscripts prints the cost of this machine's transcripts
// (SW_COST_REAL=<project folder>, e.g. the repo): a check by eye.
func TestRealTranscripts(t *testing.T) {
	dir := os.Getenv("SW_COST_REAL")
	if dir == "" || runtime.GOOS == "windows" {
		t.Skip("set SW_COST_REAL to a folder claude ran in")
	}
	home, _ := os.UserHomeDir()
	start := time.Now()
	res := runScript(t, home, []Item{{"s-real", dir}}, nil)
	l := newLedger(&Ledger{})
	l.Apply("local", []string{"s-real"}, res, Table(nil), time.Now())
	s := l.Summary(Table(nil), time.Now())
	t.Logf("%d transcripts in %v: today $%.2f, total $%.2f, unpriced %v", len(res.Chunks), time.Since(start), s.Today, s.Total, s.Unpriced)
}
