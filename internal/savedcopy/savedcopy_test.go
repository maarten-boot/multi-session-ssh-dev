package savedcopy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKeep(t *testing.T) {
	now := time.Date(2026, 10, 22, 15, 0, 0, 0, time.Local)
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, time.Local) }
	times := []time.Time{ // newest first
		at(22, 14), at(22, 9), // today, within 24 hours
		at(21, 16), at(21, 10), // yesterday: 16:00 within 24 h, 10:00 not (but 16:00 is that day's newest)
		at(20, 18), at(20, 8), // the 20th: 18:00 kept as the day's last
		at(9, 12), // 13 days back: kept
		at(7, 12), // 15 days back: gone
	}
	got := fmt.Sprint(Keep(times, now, 24, 14))
	if want := "[true true true false true false true false]"; got != want {
		t.Errorf("Keep = %s, want %s", got, want)
	}
	// The newest copy stays, however old.
	if got := Keep([]time.Time{at(1, 1), at(1, 0)}, now, 24, 14); !got[0] || got[1] {
		t.Errorf("old copies: %v, want only the newest", got)
	}
	// No daily copies: only the recent ones.
	if got := fmt.Sprint(Keep(times[:4], now, 24, 0)); got != "[true true true false]" {
		t.Errorf("days 0: %s", got)
	}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCopiesOnDisk(t *testing.T) {
	base := t.TempDir()
	// A copy from before dated copies.
	write(t, filepath.Join(base, "files", "a.txt"), "old")
	when := time.Date(2026, 10, 20, 9, 30, 0, 0, time.Local)
	b, _ := json.Marshal(Info{When: when.Format(time.RFC3339), From: "agent:~/x", Files: 1})
	write(t, filepath.Join(base, "saved.json"), string(b))

	cs := List(base)
	if len(cs) != 1 || cs[0].Stamp != "20261020-093000" || cs[0].From != "agent:~/x" || cs[0].Files != 1 {
		t.Fatalf("migrated: %+v", cs)
	}
	if _, err := os.Stat(filepath.Join(cs[0].Dir, "a.txt")); err != nil {
		t.Error("migrated copy lost its file")
	}

	// Two new copies; a leftover of a save that did not finish.
	for i, now := range []time.Time{when.Add(24 * time.Hour), when.Add(48 * time.Hour)} {
		tmp, final := Next(base, now)
		write(t, filepath.Join(tmp, "a.txt"), fmt.Sprint(i))
		if err := Commit(tmp, final, Info{When: now.Format(time.RFC3339), Files: 1}); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(base, "20261023-000000.new", "x"), "half")
	cs = List(base)
	if len(cs) != 3 || cs[0].Stamp != "20261022-093000" || cs[2].Stamp != "20261020-093000" {
		t.Fatalf("list: %+v", cs)
	}
	if c, ok := Find(base, "20261021-093000"); !ok || c.When == "" {
		t.Errorf("Find: %+v %v", c, ok)
	}

	// Pruned on the 22nd at 10:00 with 2 hours and no days: the newest only.
	removed := Prune(base, when.Add(48*time.Hour+30*time.Minute), 2, 0)
	if fmt.Sprint(removed) != "[20261023-000000.new 20261021-093000 20261020-093000]" {
		t.Errorf("removed %v", removed)
	}
	if cs = List(base); len(cs) != 1 {
		t.Errorf("after pruning: %+v", cs)
	}
	if _, err := os.Stat(filepath.Join(base, "20261020-093000.json")); err == nil {
		t.Error("a pruned copy's .json stayed")
	}
}
