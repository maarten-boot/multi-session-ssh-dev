package reclog

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func lines(feed func(s *screen)) []string {
	var out []string
	s := &screen{emit: func(l string) { out = append(out, l) }}
	feed(s)
	if c := s.current(); c != "" {
		out = append(out, c+"…")
	}
	return out
}

func TestScreen(t *testing.T) {
	for _, c := range []struct {
		name, in string
		want     []string
	}{
		{"plain", "hello\r\nworld\r\n", []string{"hello", "world"}},
		{"colors and title", "\x1b]0;title\x07\x1b[1;32mgreen\x1b[0m text\r\n", []string{"green text"}},
		{"progress bar", "10%\r50%\r100%\r\n", []string{"100%"}},
		{"backspace", "abd\bc\r\n", []string{"abc"}},
		{"erase line", "old text\r\x1b[Knew\r\n", []string{"new"}},
		{"cursor to column", "abcdef\x1b[3Gx\r\n", []string{"abxdef"}},
		{"cursor moves end lines", "top\x1b[5;1Hbottom\r\n", []string{"top", "bottom"}},
		{"private modes and DCS", "\x1b[?2004h\x1bP1$r0m\x1b\\ok\r\n", []string{"ok"}},
		{"charset", "\x1b(Bplain\r\n", []string{"plain"}},
		{"tabs", "a\tb\r\n", []string{"a       b"}},
		{"blank lines kept", "a\r\n\r\nb\r\n", []string{"a", "", "b"}},
		{"unicode", "héllo ✓ 日本\r\n", []string{"héllo ✓ 日本"}},
		{"unfinished line", "$ ", []string{"$…"}},
	} {
		got := lines(func(s *screen) { s.write([]byte(c.in)) })
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	// Split anywhere, even inside a UTF-8 character or an escape sequence.
	in := "\x1b[1;31mrød ✓\x1b[0m\r\n"
	for cut := 1; cut < len(in); cut++ {
		got := lines(func(s *screen) { s.write([]byte(in[:cut])); s.write([]byte(in[cut:])) })
		if !slices.Equal(got, []string{"rød ✓"}) {
			t.Errorf("cut at %d: %q", cut, got)
		}
	}
}

func TestKeys(t *testing.T) {
	var out []string
	starts := 0
	k := &keys{start: func() { starts++ }, emit: func(l, suffix string) { out = append(out, l+suffix) }}
	k.write([]byte("git statsu\x7f\x7fus\r"))       // backspace
	k.write([]byte("\x1b[A\x1b[B\x1bOP\r"))         // arrows, F1, empty Enter: nothing
	k.write([]byte("sleep 9\x03"))                  // ^C ends the line
	k.write([]byte("\x1b[200~one\ntwo\x1b[201~\r")) // a paste of two lines
	k.write([]byte("caf\xc3"))
	k.write([]byte("\xa9\r")) // é split over two writes
	want := []string{"git status", "sleep 9^C", "one", "two", "café"}
	if !slices.Equal(out, want) {
		t.Errorf("got %q, want %q", out, want)
	}
	if starts != 5 {
		t.Errorf("%d line starts, want 5", starts)
	}
}

// clock is a settable test clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs") // created by Open
	c := &clock{t: time.Date(2026, 10, 19, 11, 37, 2, 0, time.Local)}
	l, err := Open(dir, "proj (vibe: /home/me/proj)", time.Hour, c.now, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	l.Output([]byte("me@vibe:~$ "))
	l.Input([]byte("ls\r"))
	l.Output([]byte("ls\r\na  b\r\n[sudo] password for me: "))
	l.Input([]byte("s3cret\r"))
	l.Output([]byte("\r\nme@vibe:~$ "))
	l.wait()
	c.add(30 * time.Minute) // 12:07: the next hour's file
	l.Input([]byte("exit\r"))
	l.Output([]byte("exit\r\n"))
	l.Close("connection ended")
	first := read(t, filepath.Join(dir, "20261019-1137.log"))
	want := "# proj (vibe: /home/me/proj) — recording from 2026-10-19 11:37:02\n" +
		"2026-10-19 11:37:02 > ls\n" +
		"me@vibe:~$ ls\n" +
		"a  b\n" +
		"2026-10-19 11:37:02 > [hidden]\n" +
		"[sudo] password for me:\n" +
		"# continued in 20261019-1200.log\n"
	if first != want {
		t.Errorf("first file:\n%s\nwant:\n%s", first, want)
	}
	if strings.Contains(first, "s3cret") {
		t.Error("the password was recorded")
	}
	second := read(t, filepath.Join(dir, "20261019-1200.log"))
	want = "# proj (vibe: /home/me/proj) — recording from 2026-10-19 12:07:02\n" +
		"2026-10-19 12:07:02 > exit\n" +
		"me@vibe:~$ exit\n" +
		"# connection ended at 2026-10-19 12:07:02\n"
	if second != want {
		t.Errorf("second file:\n%s\nwant:\n%s", second, want)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(filepath.Join(dir, "20261019-1200.log"))
		di, _ := os.Stat(dir)
		if fi.Mode().Perm() != 0o600 || di.Mode().Perm()&0o077 != 0 {
			t.Errorf("permissions: file %v, dir %v", fi.Mode().Perm(), di.Mode().Perm())
		}
	}
	if _, err := Open(dir, "x", time.Minute, c.now, nil, nil); err == nil {
		t.Error("a 1 minute segment was accepted")
	}
}

func TestSegmentEnd(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 19, h, m, 0, 0, time.Local) }
	for _, c := range []struct {
		t     time.Time
		every time.Duration
		want  time.Time
	}{
		{at(11, 37), time.Hour, at(12, 0)},
		{at(12, 0), time.Hour, at(13, 0)},
		{at(11, 37), 15 * time.Minute, at(11, 45)},
		{at(11, 37), 6 * time.Hour, at(12, 0)},
		{at(23, 59), 24 * time.Hour, at(24, 0)},
	} {
		if _, got := segment(c.t, c.every); !got.Equal(c.want) {
			t.Errorf("segment(%v, %v) ends %v, want %v", c.t, c.every, got, c.want)
		}
	}
}

func TestDir(t *testing.T) {
	for _, c := range []struct{ host, path, want string }{
		{"vibe", "/home/me/proj", filepath.Join("B", "vibe", "home", "me", "proj")},
		{"lab", `C:\Users\me\proj`, filepath.Join("B", "lab", "C", "Users", "me", "proj")},
		{"a:b", "/x/../y/./z", filepath.Join("B", "ab", "x", "y", "z")},
		{"h", "/what?/trailing. ", filepath.Join("B", "h", "what", "trailing")},
		{"con", "/home/Nul.txt/lpt1/com10/console", filepath.Join("B", "con_", "home", "Nul_.txt", "lpt1_", "com10", "console")},
	} {
		if got := Dir("B", c.host, c.path); got != c.want {
			t.Errorf("Dir(%q, %q) = %q, want %q", c.host, c.path, got, c.want)
		}
	}
}

func TestRecorder(t *testing.T) {
	base := t.TempDir()
	on := map[string]bool{}
	c := &clock{t: time.Date(2026, 10, 19, 9, 5, 0, 0, time.Local)}
	r := &Recorder{Now: c.now, Settings: func(s string) (bool, string, time.Duration) { return on[s], base, time.Hour }}
	tgt := Target{Session: "s-1", Name: "proj", Host: "vibe", Path: "/home/me/proj"}
	r.Attach("t1", tgt)
	r.Output("t1", []byte("before\r\n"))
	r.Output("t2", []byte("not a remote tab\r\n"))
	on["s-1"] = true // switched on: records from now
	r.Refresh()
	if !r.Recording()["s-1"] {
		t.Error("not recording after switching on")
	}
	r.Output("t1", []byte("after\r\n"))
	on["s-1"] = false
	r.Refresh()
	r.Output("t1", []byte("off again\r\n"))
	got := read(t, filepath.Join(Dir(base, "vibe", "/home/me/proj"), "20261019-0905.log"))
	if strings.Contains(got, "before") || !strings.Contains(got, "after\n") || strings.Contains(got, "off again") ||
		!strings.Contains(got, "# recording stopped at") {
		t.Errorf("log:\n%s", got)
	}
	r.Exit("t1")
	if len(r.Recording()) != 0 || len(r.terms) != 0 {
		t.Error("terminal kept after exit")
	}

	// A reconnect in the same segment continues the file; after the
	// segment, or after switching off and on, a new file starts.
	on["s-1"] = true
	r.Attach("t2", tgt)
	r.Output("t2", []byte("first\r\n"))
	r.Exit("t2")
	c.add(10 * time.Minute) // 09:15
	r.Attach("t3", tgt)
	r.Output("t3", []byte("reconnected\r\n"))
	r.Exit("t3")
	dir := Dir(base, "vibe", "/home/me/proj")
	got = read(t, filepath.Join(dir, "20261019-0905.log"))
	if !strings.Contains(got, "first\n") || !strings.Contains(got, "reconnected\n") ||
		!strings.Contains(got, "# connection ended at 2026-10-19 09:05:00\n# proj (vibe: /home/me/proj) — recording resumed 2026-10-19 09:15:00\n") {
		t.Errorf("reconnect did not continue the file:\n%s", got)
	}
	c.add(time.Hour) // 10:15: the 09:00 segment is over
	r.Attach("t4", tgt)
	r.Output("t4", []byte("later\r\n"))
	r.Exit("t4")
	if got := read(t, filepath.Join(dir, "20261019-1015.log")); !strings.Contains(got, "later\n") {
		t.Errorf("new segment:\n%s", got)
	}
}
