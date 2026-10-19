package reclog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tmuxSudo is what tmux (80x24) sent while a program in it printed a sudo
// prompt: the status bar is redrawn after it (the window is renamed), so
// the prompt is no longer the current line when the password is typed.
var tmuxSudo = "\x1b[?1049h\x1b[22;0;0t\x1b[?1h\x1b=\x1b[H\x1b[2J\x1b[?12l\x1b[?25h\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1005l\x1b[?2004h\x1b[?2031h\x1b[?996n\x1b(B\x1b[m\x1b[?12l\x1b[?25h\x1b[?1006l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[1;1H\x1b[1;24r\x1b[c\x1b[>c\x1b[>q\x1b]10;?\x1b\\\x1b]11;?\x1b\\\x1b[1;1H\x1b[?25l\x1b[K\r\n" +
	strings.Repeat("\x1b[K\r\n", 21) +
	"\x1b[K\x1b[30m\x1b[42m\r\n" +
	"[0] 0:tmux*                                           \"vibe-u26\" 14:01 19-Oct-26\x1b(B\x1b[m\x1b[?12l\x1b[?25h\x1b[1;1H\x1b[18t\x1b[14t\x1b(B\x1b[m\x1b[?12l\x1b[?25h\x1b[?1006l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[1;1H\x1b[1;24r\x1b[1;1H\x1b[?25l^@\x1b[K\r\n" +
	strings.Repeat("\x1b[K\r\n", 21) +
	"\x1b[K\x1b[30m\x1b[42m\r\n" +
	"[0] 0:tmux*                                           \"vibe-u26\" 14:01 19-Oct-26\x1b(B\x1b[m\x1b[?12l\x1b[?25h\x1b[1;3H[sudo] password for me: \x1b[?25l\x1b[30m\x1b[42m\x1b[24;1H[0] 0:bash*                                           \"vibe-u26\" 14:01 19-Oct-26\x1b(B\x1b[m\x1b[?12l\x1b[?25h\x1b[1;27H"

// record feeds a fresh log: out is output, in input, alternating; it
// returns the file.
func record(t *testing.T, feed ...string) string {
	t.Helper()
	dir := t.TempDir()
	c := &clock{t: time.Date(2026, 10, 19, 11, 37, 2, 0, time.Local)}
	l, err := Open(dir, "x", time.Hour, c.now, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range feed {
		if i%2 == 0 {
			l.Output([]byte(s))
		} else {
			l.Input([]byte(s))
		}
	}
	l.Close("end")
	return read(t, filepath.Join(dir, "20261019-1137.log"))
}

func TestPasswordPrompt(t *testing.T) {
	for _, c := range []struct {
		name, out, in string
		want          []string // input lines
	}{
		{"tmux capture", tmuxSudo, "s3cret\r", []string{"[hidden]"}},
		{"tmux status redraw", "\x1b[1;3H[sudo] password for me: \x1b[24;1H[0] 0:bash*\x1b[1;27H", "s3cret\r", []string{"[hidden]"}},
		{"prompt then newline", "Password: \x1b[5;1H\r\n$ ", "ls\r", []string{"ls"}},
		{"^C at the prompt", "Password: ", "\x03", []string{"^C"}},
		{"typed then ^C", "Password: ", "s3\x03", []string{"[hidden]^C"}},
		{"multi-line paste", "Password: ", "\x1b[200~s3cret\nls\nmore\x1b[201~\r", []string{"[hidden]", "[hidden]", "[hidden]"}},
		{"plain multi-line paste", "Password: ", "s3cret\nls\n", []string{"[hidden]", "[hidden]"}},
	} {
		got := record(t, c.out, c.in, "\r\n$ ", "echo after\r")
		var in []string
		for _, l := range strings.Split(got, "\n") {
			if _, s, ok := strings.Cut(l, " > "); ok {
				in = append(in, s)
			}
		}
		if want := append(c.want, "echo after"); !slices.Equal(in, want) {
			t.Errorf("%s: input %q, want %q\n%s", c.name, in, want, got)
		}
		if strings.Contains(got, "s3") {
			t.Errorf("%s: the password was recorded:\n%s", c.name, got)
		}
	}
}

func TestScreenMore(t *testing.T) {
	long := strings.Repeat("x", maxLine+5)
	for _, c := range []struct {
		name, in string
		want     []string
	}{
		{"ESC inside CSI", "a\x1b[1\x1b[31mred\r\n", []string{"ared"}},
		{"OSC ended by ST", "\x1b]0;tïtle ✓\x1b\\ok\r\n", []string{"ok"}},
		{"OSC ended by BEL", "\x1b]2;日本\x07ok\r\n", []string{"ok"}},
		{"long line split", long + "\r\n", []string{long[:maxLine], "xxxxx"}},
		{"delete chars", "abcdef\r\x1b[2C\x1b[2P\r\n", []string{"abef"}},
		{"erase chars", "abcdef\r\x1b[2C\x1b[2X\r\n", []string{"ab  ef"}},
		{"insert blanks", "abcdef\r\x1b[2C\x1b[2@\r\n", []string{"ab  cdef"}},
		{"erase to the left", "abcdef\r\x1b[3C\x1b[1K\r\n", []string{"    ef"}},
	} {
		got := lines(func(s *screen) { s.write([]byte(c.in)) })
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// Huge parameters move the cursor or insert no further than a line holds.
func TestScreenHugeParams(t *testing.T) {
	for _, in := range []string{
		"\x1b[2147483647Gx", "\x1b[99999999Cx", "\x1b[99999999999999999999Gx",
		"abc\x1b[1G\x1b[99999@x", "abc\x1b[1G\x1b[2147483647@x", strings.Repeat("\t", 5000) + "x",
	} {
		total := 0
		s := &screen{emit: func(l string) { total += len(l) }}
		s.write([]byte(in))
		if total+len(s.line) > 2*maxLine {
			t.Errorf("%q: %d characters", in, total+len(s.line))
		}
	}
}

func TestKeysMore(t *testing.T) {
	for _, c := range []struct {
		name   string
		writes []string
		want   []string
	}{
		{"lone Esc then a key", []string{"ihello", "\x1b", ":wq\r"}, []string{"ihello:wq"}},
		{"Alt+x in one write", []string{"a\x1bxb\r"}, []string{"ab"}},
		{"X10 mouse report", []string{"ls\x1b[M !!", "\x1b[M\xff\xc3\x80 -l\r"}, []string{"ls -l"}},
		{"SGR mouse report", []string{"ls\x1b[<0;10;5M\r"}, []string{"ls"}},
		{"paste with ESC", []string{"\x1b[200~ab\x1bcd\x1b[Ae\x1b[201~\x1bx\r"}, []string{"abcde"}},
	} {
		var out []string
		k := &keys{start: func() {}, emit: func(l, suffix string) { out = append(out, l+suffix) }}
		for _, w := range c.writes {
			k.write([]byte(w))
		}
		if !slices.Equal(out, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, out, c.want)
		}
	}
}

func TestSegmentDST(t *testing.T) {
	ams, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Skip(err)
	}
	utc := func(mo time.Month, d, h, m int) time.Time {
		return time.Date(2026, mo, d, h, m, 0, 0, time.UTC).In(ams)
	}
	name := func(t time.Time) string { return t.Format("01-02 15:04 MST") }
	for _, c := range []struct {
		t          time.Time
		every      time.Duration
		start, end string
	}{
		// 25 October: 03:00 CEST becomes 02:00 CET, 02:xx comes twice.
		{utc(10, 24, 23, 30), time.Hour, "10-25 01:00 CEST", "10-25 02:00 CEST"},
		{utc(10, 25, 0, 30), time.Hour, "10-25 02:00 CEST", "10-25 03:00 CET"}, // the first 02:30
		{utc(10, 25, 1, 30), time.Hour, "10-25 02:00 CET", "10-25 03:00 CET"},  // the second
		{utc(10, 25, 2, 30), time.Hour, "10-25 03:00 CET", "10-25 04:00 CET"},
		{utc(10, 25, 1, 30), 6 * time.Hour, "10-25 00:00 CEST", "10-25 06:00 CET"},
		{utc(10, 25, 1, 30), 24 * time.Hour, "10-25 00:00 CEST", "10-26 00:00 CET"},
		{utc(10, 25, 5, 59), 7 * time.Hour, "10-25 00:00 CEST", "10-25 07:00 CET"},
		{utc(10, 25, 21, 0), 7 * time.Hour, "10-25 21:00 CET", "10-26 00:00 CET"},
		// 29 March: 02:00 CET becomes 03:00 CEST, 02:xx does not exist.
		{utc(3, 29, 0, 30), time.Hour, "03-29 01:00 CET", "03-29 03:00 CEST"},
		{utc(3, 29, 1, 30), time.Hour, "03-29 03:00 CEST", "03-29 04:00 CEST"},
		{utc(3, 29, 3, 0), 6 * time.Hour, "03-29 00:00 CET", "03-29 06:00 CEST"},
		{utc(3, 29, 12, 0), 24 * time.Hour, "03-29 00:00 CET", "03-30 00:00 CEST"},
		{utc(3, 29, 8, 0), 7 * time.Hour, "03-29 07:00 CEST", "03-29 14:00 CEST"},
	} {
		s, e := segment(c.t, c.every)
		if name(s) != c.start || name(e) != c.end {
			t.Errorf("segment(%s, %v) = %s – %s, want %s – %s", name(c.t), c.every, name(s), name(e), c.start, c.end)
		}
	}
	// Every minute of both days: t in its segment, the next one starts
	// where it ends, bounds at clock multiples from midnight.
	for _, day := range []time.Time{utc(10, 24, 22, 0), utc(3, 28, 23, 0)} {
		for _, every := range []time.Duration{15 * time.Minute, time.Hour, 6 * time.Hour, 7 * time.Hour, 24 * time.Hour} {
			for t0 := day; t0.Before(day.Add(25 * time.Hour)); t0 = t0.Add(time.Minute) {
				s, e := segment(t0, every)
				h, m, _ := s.Clock()
				if s.After(t0) || !e.After(t0) || time.Duration(h*60+m)*time.Minute%every != 0 {
					t.Fatalf("segment(%s, %v) = %s – %s", name(t0), every, name(s), name(e))
				}
				if s2, _ := segment(e, every); !s2.Equal(e) && e.Day() == t0.Day() {
					t.Fatalf("after %s (%v) comes %s", name(e), every, name(s2))
				}
			}
		}
	}
	// The files across the change: the hour from 02:00 holds both.
	dir := t.TempDir()
	c := &clock{t: utc(10, 24, 23, 30)}
	l, err := Open(dir, "x", time.Hour, c.now, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []time.Duration{40 * time.Minute, time.Hour, 50 * time.Minute} { // 02:10 CEST, 02:10 CET, 03:00 CET
		c.add(d)
		l.Output([]byte(name(c.now()) + "\r\n"))
		l.wait()
	}
	l.Close("end")
	got, _ := filepath.Glob(filepath.Join(dir, "*.log"))
	for i := range got {
		got[i] = filepath.Base(got[i])
	}
	if want := []string{"20261025-0130.log", "20261025-0200.log", "20261025-0300.log"}; !slices.Equal(got, want) {
		t.Errorf("files %q, want %q", got, want)
	}
	if s := read(t, filepath.Join(dir, "20261025-0200.log")); !strings.Contains(s, "02:10 CEST\n10-25 02:10 CET\n") {
		t.Errorf("02:00 file:\n%s", s)
	}
}

func noWrite(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("folder permissions do not stop this user")
	}
}

// A file that cannot be opened at rotation is reported, retried once a
// minute, and the recovery reported.
func TestLogRotationError(t *testing.T) {
	noWrite(t)
	dir := t.TempDir()
	c := &clock{t: time.Date(2026, 10, 19, 11, 50, 0, 0, time.Local)}
	var mu sync.Mutex
	var errs []error
	l, err := Open(dir, "x", time.Hour, c.now, nil, func(err error) { mu.Lock(); errs = append(errs, err); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o500)
	defer os.Chmod(dir, 0o700)
	step := func(d time.Duration, out string) {
		c.add(d)
		l.Output([]byte(out + "\r\n"))
		l.wait()
	}
	step(15*time.Minute, "lost-1") // 12:05: no new file
	step(30*time.Second, "lost-2") // not tried again yet
	os.Chmod(dir, 0o700)
	step(20*time.Second, "lost-3") // 12:05:50: still not
	step(20*time.Second, "back")   // 12:06:10: a new file
	l.Close("end")
	mu.Lock()
	defer mu.Unlock()
	if len(errs) != 2 || errs[0] == nil || errs[1] != nil || l.Err() == nil {
		t.Errorf("errors reported: %v", errs)
	}
	got := read(t, filepath.Join(dir, "20261019-1206.log"))
	if !strings.Contains(got, "recording from 2026-10-19 12:06:10\nback\n# end at") || strings.Contains(got, "lost") {
		t.Errorf("after the error:\n%s", got)
	}
}

// Close after the segment ended writes the last line into the current file.
func TestCloseAfterSegment(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t: time.Date(2026, 10, 19, 11, 50, 0, 0, time.Local)}
	l, err := Open(dir, "x", time.Hour, c.now, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	l.Output([]byte("$ "))
	l.wait()
	c.add(15 * time.Minute)
	l.Close("connection ended")
	if got, _ := filepath.Glob(filepath.Join(dir, "*.log")); len(got) != 1 {
		t.Errorf("files %q", got)
	}
	if got := read(t, filepath.Join(dir, "20261019-1150.log")); !strings.HasSuffix(got, "\n$\n# connection ended at 2026-10-19 12:05:00\n") {
		t.Errorf("log:\n%s", got)
	}
}

// What does not fit in the queue is left out with a note, and the next
// input is hidden (the prompt may have been in it).
func TestLogDropped(t *testing.T) {
	l := &Log{ch: make(chan msg, 2)}
	l.Output([]byte("one"))
	l.Output([]byte("two"))
	l.Output([]byte("three")) // full
	l.Input([]byte("four"))   // still full
	<-l.ch
	l.Output([]byte("five")) // the note goes first; no room for five
	<-l.ch
	if m := <-l.ch; m.note != 9 || l.dropped != 4 {
		t.Errorf("note %d, dropped %d", m.note, l.dropped)
	}
	dir := t.TempDir()
	c := &clock{t: time.Date(2026, 10, 19, 11, 50, 0, 0, time.Local)}
	l, _ = Open(dir, "x", time.Hour, c.now, nil, nil)
	l.ch <- msg{note: 42}
	l.Input([]byte("s3cret\r"))
	l.Close("end")
	if got := read(t, filepath.Join(dir, "20261019-1150.log")); !strings.Contains(got, "# 42 bytes not recorded") || strings.Contains(got, "s3cret") {
		t.Errorf("log:\n%s", got)
	}
}

// events collects what a Recorder reports.
type events struct {
	mu  sync.Mutex
	got []string
}

func (e *events) changed(s string, on bool, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.got = append(e.got, fmt.Sprintf("%s %v %v", s, on, err != nil))
}

// wait returns the events once there are n, and checks no more follow.
func (e *events) wait(t *testing.T, n int) []string {
	t.Helper()
	for i := 0; ; i++ {
		e.mu.Lock()
		got := slices.Clone(e.got)
		e.mu.Unlock()
		if len(got) > n || len(got) == n && i > 10 || i > 500 {
			if len(got) != n {
				t.Errorf("%d events, want %d: %q", len(got), n, got)
			}
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRecorderOpenError(t *testing.T) {
	noWrite(t)
	base := filepath.Join(t.TempDir(), "ro")
	os.Mkdir(base, 0o500)
	every := time.Hour
	ev := &events{}
	r := &Recorder{Changed: ev.changed, Settings: func(string) (bool, string, time.Duration) { return true, base, every }}
	r.Attach("t1", Target{Session: "s", Name: "n", Host: "h", Path: "/p"})
	r.Refresh() // unrelated changes: not tried or reported again
	r.Refresh()
	if got := ev.wait(t, 1); got[0] != "s false true" || len(r.Recording()) != 0 {
		t.Errorf("events %q", got)
	}
	every = 2 * time.Hour // tried again, and reported
	r.Refresh()
	ev.wait(t, 2)
	os.Chmod(base, 0o700)
	r.Exit("t1")
	r.Attach("t2", Target{Session: "s", Name: "n", Host: "h", Path: "/p"}) // a reconnect tries again
	if got := ev.wait(t, 3); got[2] != "s true false" || !r.Recording()["s"] {
		t.Errorf("events %q", got)
	}
	r.Exit("t2")
}

// A changed folder or segment length ends the file and starts one anew.
func TestRecorderSettingsChange(t *testing.T) {
	base1, base2 := t.TempDir(), t.TempDir()
	base, every := base1, time.Hour
	c := &clock{t: time.Date(2026, 10, 19, 9, 5, 0, 0, time.Local)}
	ev := &events{}
	r := &Recorder{Now: c.now, Changed: ev.changed, Settings: func(string) (bool, string, time.Duration) { return true, base, every }}
	tgt := Target{Session: "s", Name: "proj", Host: "vibe", Path: "/home/me/proj"}
	r.Attach("t1", tgt)
	r.Output("t1", []byte("one\r\n"))
	base = base2
	r.Refresh()
	r.Output("t1", []byte("two\r\n"))
	c.add(time.Minute)
	every = 2 * time.Hour
	r.Refresh()
	r.Output("t1", []byte("three\r\n"))
	r.Exit("t1")
	f1 := read(t, filepath.Join(Dir(base1, "vibe", "/home/me/proj"), "20261019-0905.log"))
	f2 := read(t, filepath.Join(Dir(base2, "vibe", "/home/me/proj"), "20261019-0905.log"))
	f3 := read(t, filepath.Join(Dir(base2, "vibe", "/home/me/proj"), "20261019-0906.log"))
	if !strings.HasSuffix(f1, "one\n# recording stopped at 2026-10-19 09:05:00\n") ||
		!strings.HasSuffix(f2, "from 2026-10-19 09:05:00\ntwo\n# recording stopped at 2026-10-19 09:06:00\n") ||
		!strings.HasSuffix(f3, "from 2026-10-19 09:06:00\nthree\n# connection ended at 2026-10-19 09:06:00\n") {
		t.Errorf("files:\n%s\n%s\n%s", f1, f2, f3)
	}
	if got := ev.wait(t, 4); !slices.Equal(got, []string{"s true false", "s true false", "s true false", "s false false"}) {
		t.Errorf("events %q", got)
	}
}

// A terminal that ended before Attach is not left recording.
func TestRecorderExitBeforeAttach(t *testing.T) {
	ev := &events{}
	base := t.TempDir()
	r := &Recorder{Changed: ev.changed, Settings: func(string) (bool, string, time.Duration) { return true, base, time.Hour }}
	r.Exit("t1")
	r.Attach("t1", Target{Session: "s", Name: "n", Host: "h", Path: "/p"})
	if len(r.Recording()) != 0 || len(r.terms) != 0 {
		t.Error("recording a terminal that ended")
	}
	ev.wait(t, 0)
}

// Events come in the order of the changes, and a reconnect resumes the
// file after its last line.
func TestRecorderEventOrder(t *testing.T) {
	ev := &events{}
	base := t.TempDir()
	r := &Recorder{Changed: ev.changed, Settings: func(string) (bool, string, time.Duration) { return true, base, time.Hour }}
	var want []string
	for i := range 50 {
		id := fmt.Sprint("t", i)
		r.Attach(id, Target{Session: "s", Name: "n", Host: "h", Path: "/p"})
		r.Output(id, []byte(fmt.Sprint("line ", i, "\r\n")))
		r.Exit(id)
		want = append(want, "s true false", "s false false")
	}
	if got := ev.wait(t, len(want)); !slices.Equal(got, want) {
		t.Errorf("events %q", got)
	}
	files, _ := filepath.Glob(filepath.Join(Dir(base, "h", "/p"), "*.log"))
	for _, f := range files {
		s := read(t, f)
		if strings.Count(s, "# connection ended") != strings.Count(s, "— recording") {
			t.Errorf("%s:\n%s", f, s)
		}
	}
}

// Taps, settings changes, exits and attaches at once (run with -race).
func TestRecorderConcurrent(t *testing.T) {
	base := t.TempDir()
	var on atomic.Bool
	var n atomic.Int64
	r := &Recorder{Changed: func(string, bool, error) { n.Add(1) },
		Settings: func(string) (bool, string, time.Duration) { return on.Load(), base, time.Hour }}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := range 4 {
		id := fmt.Sprint("t", i)
		r.Attach(id, Target{Session: id, Name: "n", Host: "h", Path: "/p" + id})
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				r.Output(id, []byte(fmt.Sprint("out ", j, "\r\n")))
				r.Input(id, []byte("x\r"))
				r.Recording()
			}
		}()
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			on.Store(!on.Load())
			r.Refresh()
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 100 {
			r.Exit("t0")
			r.Attach("t0", Target{Session: "t0", Name: "n", Host: "h", Path: fmt.Sprint("/p", i%3)})
		}
	}()
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
	for i := range 4 {
		r.Exit(fmt.Sprint("t", i))
	}
	if len(r.terms) != 0 || n.Load() == 0 {
		t.Errorf("%d terminals left, %d events", len(r.terms), n.Load())
	}
}
