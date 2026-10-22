// Package savedcopy keeps the dated saved copies of a working-copy
// session's agent copy (SPEC F12): <base>/<stamp>/ holds one copy and
// <stamp>.json what it is; the stamp is the local time of the save,
// YYYYMMDD-HHMMSS. Pruning keeps every copy of the last hours and the last
// copy of each earlier day for some days.
package savedcopy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

// StampLayout is the folder name of a copy.
const StampLayout = "20060102-150405"

var stampRE = regexp.MustCompile(`^\d{8}-\d{6}$`)

// Info is what a copy is (its <stamp>.json).
type Info struct {
	When  string `json:"when"` // RFC 3339
	From  string `json:"from"` // target:path it was saved from
	Files int    `json:"files"`
	Note  string `json:"note"`
}

// Copy is one saved copy.
type Copy struct {
	Stamp string `json:"stamp"`
	Dir   string `json:"dir"`
	Info
	time time.Time
}

// List returns the copies in base, newest first. A copy from before dated
// copies (base/files with base/saved.json) is moved to a dated one first.
func List(base string) []Copy {
	migrate(base)
	entries, _ := os.ReadDir(base)
	var out []Copy
	for _, e := range entries {
		if !e.IsDir() || !stampRE.MatchString(e.Name()) {
			continue
		}
		t, err := time.ParseInLocation(StampLayout, e.Name(), time.Local)
		if err != nil {
			continue
		}
		c := Copy{Stamp: e.Name(), Dir: filepath.Join(base, e.Name()), time: t}
		if b, err := os.ReadFile(filepath.Join(base, e.Name()+".json")); err == nil {
			json.Unmarshal(b, &c.Info)
		}
		if c.When == "" {
			c.When = t.Format(time.RFC3339)
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Copy) int { return b.time.Compare(a.time) })
	return out
}

// Find returns the copy with this stamp.
func Find(base, stamp string) (Copy, bool) {
	for _, c := range List(base) {
		if c.Stamp == stamp {
			return c, true
		}
	}
	return Copy{}, false
}

// migrate moves base/files (+ saved.json) to base/<stamp> (+ <stamp>.json).
func migrate(base string) {
	old := filepath.Join(base, "files")
	fi, err := os.Stat(old)
	if err != nil || !fi.IsDir() {
		return
	}
	t := fi.ModTime()
	b, _ := os.ReadFile(filepath.Join(base, "saved.json"))
	var info Info
	if json.Unmarshal(b, &info) == nil {
		if w, err := time.Parse(time.RFC3339, info.When); err == nil {
			t = w
		}
	}
	stamp := t.Local().Format(StampLayout)
	if os.Rename(old, filepath.Join(base, stamp)) == nil && b != nil {
		os.Rename(filepath.Join(base, "saved.json"), filepath.Join(base, stamp+".json"))
	}
}

// Next is where a new copy is written (<stamp>.new), and its final name.
func Next(base string, now time.Time) (tmp, final string) {
	stamp := now.Local().Format(StampLayout)
	return filepath.Join(base, stamp+".new"), filepath.Join(base, stamp)
}

// Commit makes the copy in tmp final and records info next to it; a
// leftover with the same stamp (a save within the same second) is replaced.
func Commit(tmp, final string, info Info) error {
	if _, err := os.Stat(final); err == nil {
		if err := os.RemoveAll(final); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(final+".json", b, 0o600)
}

// Discard removes an unfinished copy.
func Discard(tmp string) { os.RemoveAll(tmp) }

// Prune removes the copies Keep does not keep, and leftovers of saves that
// did not finish (*.new). It returns what it removed.
func Prune(base string, now time.Time, hours, days int) []string {
	var removed []string
	if news, _ := filepath.Glob(filepath.Join(base, "*.new")); len(news) > 0 {
		for _, n := range news {
			if os.RemoveAll(n) == nil {
				removed = append(removed, filepath.Base(n))
			}
		}
	}
	copies := List(base)
	times := make([]time.Time, len(copies))
	for i, c := range copies {
		times[i] = c.time
	}
	keep := Keep(times, now, hours, days)
	for i, c := range copies {
		if keep[i] {
			continue
		}
		if err := os.RemoveAll(c.Dir); err == nil || errors.Is(err, os.ErrNotExist) {
			os.Remove(c.Dir + ".json")
			removed = append(removed, c.Stamp)
		}
	}
	return removed
}

// Keep says which copies to keep (times newest first): all of the last
// hours, the newest of each earlier day for days days (counted from today
// back, local time), and the newest copy whatever its age.
func Keep(times []time.Time, now time.Time, hours, days int) []bool {
	keep := make([]bool, len(times))
	recent := now.Add(-time.Duration(hours) * time.Hour)
	y, m, d := now.Local().Date()
	oldest := time.Date(y, m, d, 0, 0, 0, 0, time.Local).AddDate(0, 0, -days)
	seenDay := map[string]bool{}
	for i, t := range times {
		day := t.Local().Format("20060102")
		switch {
		case i == 0, t.After(recent):
			keep[i] = true
		case !t.Before(oldest) && !seenDay[day]:
			keep[i] = true
		}
		seenDay[day] = true
	}
	return keep
}
