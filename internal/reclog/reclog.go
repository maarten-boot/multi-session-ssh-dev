// Package reclog records what goes in and out of a session's Remote tab as
// readable text: output without escape codes, the user's input as
// timestamped "> " lines, input typed at a password prompt as "[hidden]".
// A log is split into files per time segment (clock times from local
// midnight, 1 hour by default), named after when they start:
// <dir>/<host>/<session path>/20261019-1130.log, owner-only.
package reclog

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Segment limits.
const (
	MinEvery = 5 * time.Minute
	MaxEvery = 24 * time.Hour
)

const stamp = "2006-01-02 15:04:05"

// passwordPrompt matches the end of an output line that asks for a secret
// (sudo, ssh, gpg, …); what is typed next is not recorded. The remote
// side's echo setting is not visible here, so a prompt it is.
var passwordPrompt = regexp.MustCompile(`(?i)(password|passphrase|passcode|\bpin\b|token|secret)[^\n]{0,40}[:?]\s*$`)

// Log is one Remote tab's recording. Output and Input never wait for the
// disk: a goroutine turns what they queue into text and writes it; what
// does not fit in the queue is left out, with a note saying so.
type Log struct {
	dir   string
	title string
	every time.Duration
	now   func() time.Time
	onErr func(error)
	ch    chan msg
	done  chan struct{} // the writer has finished (Close)

	mu         sync.Mutex
	closed     bool
	dropped    int // bytes left out since the last note
	failed     bool
	err        error
	start, end time.Time // the current file's name and when its segment ends

	// The writer's own.
	f       *os.File
	w       *bufio.Writer
	out     screen
	in      keys
	hidden  bool      // the input line being typed follows a password prompt
	sticky  bool      // a line of this write was hidden: so are the others (a paste)
	closing bool      // writing the last lines: no new file for them
	retry   time.Time // after a failure: when to try opening a file again
}

// msg is a piece of the queue: output or input, or a note that bytes were
// left out, or a request to report back (wait) or to finish (close).
type msg struct {
	b     []byte
	input bool
	note  int
	wait  chan struct{}
	close string
}

// queueLen bounds what waits for the disk (each message at most one read
// of the terminal, 32 KiB).
const queueLen = 256

// retryEvery is how often a failed file is tried again.
const retryEvery = time.Minute

// Open starts a recording in dir (created owner-only); title heads every
// file. now is the clock (time.Now outside tests). resume, if set, is a
// file of an earlier recording to continue while its segment lasts (a
// reconnect does not start a new file). onErr, if set, is called (from the
// writer) when writing fails, and with nil when it works again; meanwhile
// a new file is tried once a minute.
func Open(dir, title string, every time.Duration, now func() time.Time, resume *Resume, onErr func(error)) (*Log, error) {
	if every < MinEvery || every > MaxEvery {
		return nil, fmt.Errorf("reclog: segment %v: use %v to %v", every, MinEvery, MaxEvery)
	}
	l := &Log{dir: dir, title: title, every: every, now: now, onErr: onErr,
		ch: make(chan msg, queueLen), done: make(chan struct{})}
	l.out.emit = func(line string) { l.line(line) }
	l.in.start = func() {
		l.hidden = l.sticky || l.out.prompt || passwordPrompt.MatchString(l.out.current())
	}
	l.in.emit = func(text, suffix string) {
		// ^C alone at a password prompt holds no secret: shown as typed.
		if l.hidden && text != "" {
			text = "[hidden]"
			l.sticky = true
		}
		l.hidden, l.out.prompt = false, false
		l.line(l.now().Format(stamp) + " > " + text + suffix)
	}
	t := now()
	var err error
	if resume != nil && resume.Dir == dir && t.Before(resume.End) && resume.Every == every {
		err = l.openFile(resume.Start, resume.End, "resumed")
	} else {
		_, end := segment(t, every)
		err = l.openFile(t, end, "from")
	}
	if err != nil {
		return nil, err
	}
	go l.run()
	return l, nil
}

// Resume identifies a log file whose segment may still be running.
type Resume struct {
	Dir        string
	Start, End time.Time
	Every      time.Duration
}

// Resume returns where the log's current file can be continued.
func (l *Log) Resume() *Resume {
	l.mu.Lock()
	defer l.mu.Unlock()
	return &Resume{Dir: l.dir, Start: l.start, End: l.end, Every: l.every}
}

// segment is the segment t lies in. Its bounds are clock times counted
// from local midnight (00:00, 01:00, … for an hour; the last one of a day
// ends at midnight when every does not divide 24 hours), so a day with a
// DST change has a longer or shorter segment, not shifted ones.
func segment(t time.Time, every time.Duration) (start, end time.Time) {
	y, m, d := t.Date()
	h, mi, sec := t.Clock()
	n := int(every / time.Second)
	k := (h*3600 + mi*60 + sec) / n
	_, off := t.Zone()
	at := func(s int) time.Time {
		// In t's offset if that is the zone's at that clock time: of a
		// clock time that comes twice (DST ends), the one on t's side.
		v := time.Date(y, m, d, 0, 0, s, 0, time.FixedZone("", off)).In(t.Location())
		if w := time.Date(y, m, d, 0, 0, s, 0, time.UTC); v.Day() != w.Day() || v.Hour() != w.Hour() || v.Minute() != w.Minute() {
			v = time.Date(y, m, d, 0, 0, s, 0, t.Location())
		}
		return v
	}
	return at(k * n), at(min((k+1)*n, 24*3600))
}

// FileName is the name of a file starting at t.
func FileName(t time.Time) string { return t.Format("20060102-1504") + ".log" }

// openFile opens (or appends to) the file named after start.
func (l *Log) openFile(start, end time.Time, how string) error {
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(l.dir, FileName(start)), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	l.f, l.w = f, bufio.NewWriter(f)
	l.mu.Lock()
	l.start, l.end = start, end
	l.mu.Unlock()
	fmt.Fprintf(l.w, "# %s — recording %s %s\n", l.title, how, l.now().Format(stamp))
	if err := l.w.Flush(); err != nil {
		f.Close()
		l.f = nil
		return err
	}
	return nil
}

// run is the writer.
func (l *Log) run() {
	defer close(l.done)
	for m := range l.ch {
		if m.note > 0 {
			l.line(fmt.Sprintf("# %d bytes not recorded (the disk was too slow)", m.note))
			l.out.prompt = true // a password prompt may have been in them
		}
		switch {
		case m.input:
			l.in.write(m.b)
			l.sticky = false
		case m.b != nil:
			l.out.write(m.b)
		}
		if m.close != "" {
			l.closing = true
			if l.out.current() != "" {
				l.out.end(false)
			}
			l.line(fmt.Sprintf("# %s at %s", m.close, l.now().Format(stamp)))
			l.flush()
			if l.f != nil {
				l.f.Close()
				l.f = nil
			}
			return
		}
		if m.wait != nil || len(l.ch) == 0 {
			l.flush()
		}
		if m.wait != nil {
			close(m.wait)
		}
	}
}

// rotate starts the next file once the segment is over, or after a
// failure tries a file again.
func (l *Log) rotate() {
	t := l.now()
	if l.f == nil {
		if t.Before(l.retry) {
			return
		}
	} else {
		if t.Before(l.end) {
			return
		}
		start, _ := segment(t, l.every)
		fmt.Fprintf(l.w, "# continued in %s\n", FileName(start))
		l.w.Flush()
		l.f.Close()
		l.f = nil
	}
	start, end := segment(t, l.every)
	if l.failed {
		start = t // a new start, as at Open
	}
	if err := l.openFile(start, end, "from"); err != nil {
		l.fail(err)
		return
	}
	l.mu.Lock()
	recovered := l.failed
	l.failed = false
	l.mu.Unlock()
	if recovered && l.onErr != nil {
		l.onErr(nil)
	}
}

// fail drops the current file after an error; the first error of a run of
// them is reported.
func (l *Log) fail(err error) {
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
	l.retry = l.now().Add(retryEvery)
	l.mu.Lock()
	report := !l.failed
	l.failed = true
	if l.err == nil {
		l.err = err
	}
	l.mu.Unlock()
	if report && l.onErr != nil {
		l.onErr(err)
	}
}

func (l *Log) line(s string) {
	if !l.closing {
		l.rotate()
	}
	if l.f == nil {
		return
	}
	l.w.WriteString(s)
	l.w.WriteByte('\n')
}

func (l *Log) flush() {
	if l.f != nil {
		if err := l.w.Flush(); err != nil {
			l.fail(err)
		}
	}
}

// send queues m for the writer, or counts its bytes as left out when the
// queue is full.
func (l *Log) send(m msg) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	if l.dropped > 0 {
		select {
		case l.ch <- msg{note: l.dropped}:
			l.dropped = 0
		default:
			l.dropped += len(m.b)
			return
		}
	}
	select {
	case l.ch <- m:
	default:
		l.dropped += len(m.b)
	}
}

// Output records what the remote side sent.
func (l *Log) Output(b []byte) { l.send(msg{b: append([]byte{}, b...)}) }

// Input records what the user typed or pasted.
func (l *Log) Input(b []byte) { l.send(msg{b: append([]byte{}, b...), input: true}) }

// wait returns once everything queued before it is written (for tests).
func (l *Log) wait() {
	m := msg{wait: make(chan struct{})}
	select {
	case l.ch <- m:
	case <-l.done:
		return
	}
	select {
	case <-m.wait:
	case <-l.done:
	}
}

// Err is the first write error, if any.
func (l *Log) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// Failed says whether the log cannot write right now.
func (l *Log) Failed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.failed
}

// Close writes what is queued and ends the recording with a last line
// saying why, in the current file even when its segment is over.
func (l *Log) Close(reason string) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	n := l.dropped
	l.mu.Unlock()
	l.ch <- msg{note: n, close: reason}
	<-l.done
}

// Dir is where a session's logs go: base/<host>/<session path>, each part
// made safe as a folder name on every OS ("C:\Users\me" → C/Users/me).
func Dir(base, host, path string) string {
	parts := []string{safe(host)}
	for _, p := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if p != "." && p != ".." {
			parts = append(parts, safe(p))
		}
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

// reserved are names Windows does not allow for a file, with or without
// an extension.
var reserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\.|$)`)

func safe(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimRight(s, ". ")
	if s == "" {
		return "_"
	}
	if m := reserved.FindStringSubmatchIndex(s); m != nil {
		s = s[:m[3]] + "_" + s[m[3]:] // "nul.txt" → "nul_.txt"
	}
	return s
}
