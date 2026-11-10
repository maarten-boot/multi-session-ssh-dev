package cost

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"session-workbench/internal/config"
)

// Last is the reply counted last in a transcript: a reply has a line per
// content block, each with the usage so far, so a later line of the same
// reply replaces it.
type Last struct {
	ID     string `json:"id"`
	Day    string `json:"day"`
	Key    string `json:"key"`
	Tokens Tokens `json:"tokens"`
}

// File is what was counted of one transcript.
type File struct {
	Off  int64                        `json:"off"` // read up to here
	Last *Last                        `json:"last,omitempty"`
	Days map[string]map[string]Tokens `json:"days"` // day → ledger key → tokens
}

// Ledger is what was counted, kept in a file between runs.
type Ledger struct {
	// Files by "<host>\t<path>"; a transcript that shrank (rewritten, or
	// the container was made anew) keeps its counts under
	// "<host>\t<path>\t~<time>" and is counted again from the start.
	Files map[string]*File `json:"files"`
	// Dirs are the project folders of each session ("<host>\t<folder>").
	Dirs map[string][]string `json:"dirs"`
	// The total counts from ResetAt: Reset are the tokens counted then.
	ResetAt time.Time         `json:"reset_at,omitzero"`
	Reset   map[string]Tokens `json:"reset,omitempty"`
}

// Load reads the ledger file (an empty ledger when there is none).
func Load(file string) (*Ledger, error) {
	l := &Ledger{}
	b, err := os.ReadFile(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return newLedger(l), err
	default:
		if err := json.Unmarshal(b, l); err != nil {
			return newLedger(&Ledger{}), err
		}
	}
	return newLedger(l), nil
}

func newLedger(l *Ledger) *Ledger {
	if l.Files == nil {
		l.Files = map[string]*File{}
	}
	if l.Dirs == nil {
		l.Dirs = map[string][]string{}
	}
	return l
}

// Save writes the ledger file.
func (l *Ledger) Save(file string) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// Known are the offsets read so far of host's transcripts, for Script.
func (l *Ledger) Known(host string) map[string]int64 {
	k := map[string]int64{}
	for key, f := range l.Files {
		h, p, ok := strings.Cut(key, "\t")
		if ok && h == host && !strings.Contains(p, "\t") {
			k[p] = f.Off
		}
	}
	return k
}

// Day is the ledger's day of t: the local date.
func Day(t time.Time) string { return t.Local().Format("2006-01-02") }

// Apply counts what the script read on host for the sessions ids.
func (l *Ledger) Apply(host string, ids []string, res Result, table map[string]config.Price, now time.Time) {
	for _, id := range ids {
		var dirs []string
		for _, d := range res.Dirs[id] {
			dirs = append(dirs, host+"\t"+d)
		}
		l.Dirs[id] = dirs
	}
	for _, c := range res.Chunks {
		key := host + "\t" + c.Path
		f := l.Files[key]
		if f != nil && (c.Size < f.Off || c.From < f.Off) {
			l.Files[key+"\t~"+strconv.FormatInt(now.UnixNano(), 10)] = f
			f = nil
		}
		if f == nil {
			f = &File{Days: map[string]map[string]Tokens{}}
			l.Files[key] = f
		}
		for _, r := range c.Replies {
			if f.Last != nil && f.Last.ID == r.ID {
				f.sub(*f.Last)
			}
			day := Day(now)
			if !r.Time.IsZero() {
				day = Day(r.Time)
			}
			last := Last{ID: r.ID, Day: day, Key: r.key(table), Tokens: r.Tokens}
			f.addLast(last)
			f.Last = &last
		}
		f.Off = c.Next
	}
}

func (f *File) sub(x Last) {
	t := f.Days[x.Day][x.Key]
	t.add(x.Tokens, -1)
	if t.zero() {
		delete(f.Days[x.Day], x.Key)
		if len(f.Days[x.Day]) == 0 {
			delete(f.Days, x.Day)
		}
		return
	}
	f.Days[x.Day][x.Key] = t
}

func (f *File) addLast(x Last) {
	if f.Days[x.Day] == nil {
		f.Days[x.Day] = map[string]Tokens{}
	}
	t := f.Days[x.Day][x.Key]
	t.add(x.Tokens, 1)
	f.Days[x.Day][x.Key] = t
}

// Keep forgets the folders of sessions that are gone (their counts stay
// in the total).
func (l *Ledger) Keep(ids []string) {
	for id := range l.Dirs {
		if !slices.Contains(ids, id) {
			delete(l.Dirs, id)
		}
	}
}

// all sums the tokens per ledger key: of every day, or of one.
func (l *Ledger) all(files []*File, day string) map[string]Tokens {
	sum := map[string]Tokens{}
	for _, f := range files {
		for d, keys := range f.Days {
			if day != "" && d != day {
				continue
			}
			for k, t := range keys {
				s := sum[k]
				s.add(t, 1)
				sum[k] = s
			}
		}
	}
	return sum
}

// ResetTotal starts the total again from now.
func (l *Ledger) ResetTotal(now time.Time) {
	l.ResetAt = now
	l.Reset = l.all(slices.Collect(maps.Values(l.Files)), "")
}

// Amount is a today and a total in dollars.
type Amount struct {
	Today float64 `json:"today"`
	Total float64 `json:"total"`
}

// Summary is what the window shows.
type Summary struct {
	Amount
	// Since: the total counts from this reset ("" = everything counted).
	Since string `json:"since"`
	// Unpriced are models without a price (not in the dollars);
	// TodayPartial and TotalPartial: today's or the total has some.
	Unpriced     []string          `json:"unpriced"`
	TodayPartial bool              `json:"today_partial"`
	TotalPartial bool              `json:"total_partial"`
	Sessions     map[string]Amount `json:"sessions"`
}

func price(table map[string]config.Price, sum map[string]Tokens, unpriced map[string]bool) float64 {
	d := 0.0
	for k, t := range sum {
		v, ok := Dollars(table, k, t)
		if !ok {
			m, _, _ := split(k)
			unpriced[m] = true
		}
		d += v
	}
	return d
}

// partial: the day has replies of a model without a price.
func (l *Ledger) partial(table map[string]config.Price, files []*File, day string) bool {
	for k, t := range l.all(files, day) {
		if _, ok := Dollars(table, k, t); !ok {
			return true
		}
	}
	return false
}

// Summary is today's and the total cost, and per session.
func (l *Ledger) Summary(table map[string]config.Price, now time.Time) Summary {
	today := Day(now)
	unpriced := map[string]bool{}
	files := slices.Collect(maps.Values(l.Files))
	all := l.all(files, "")
	for k, t := range l.Reset {
		s := all[k]
		s.add(t, -1)
		all[k] = s
	}
	s := Summary{Sessions: map[string]Amount{}}
	s.Total = price(table, all, unpriced)
	s.TotalPartial = len(unpriced) > 0
	s.Today = price(table, l.all(files, today), unpriced)
	s.TodayPartial = l.partial(table, files, today)
	if !l.ResetAt.IsZero() {
		s.Since = l.ResetAt.Local().Format("2006-01-02 15:04")
	}
	for id, dirs := range l.Dirs {
		var mine []*File
		for key, f := range l.Files {
			for _, d := range dirs {
				if strings.HasPrefix(key, d+"/") {
					mine = append(mine, f)
					break
				}
			}
		}
		s.Sessions[id] = Amount{Today: price(table, l.all(mine, today), unpriced),
			Total: price(table, l.all(mine, ""), unpriced)}
	}
	s.Unpriced = slices.Sorted(maps.Keys(unpriced))
	return s
}
