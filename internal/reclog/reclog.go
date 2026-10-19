// Package reclog records what goes in and out of a session's Remote tab as
// readable text: output without escape codes, the user's input as
// timestamped "> " lines, input typed at a password prompt as "[hidden]".
// A log is split into files per time segment (aligned to local midnight,
// 1 hour by default), named after when they start:
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

// Log is one Remote tab's recording.
type Log struct {
	mu     sync.Mutex
	dir    string
	title  string
	every  time.Duration
	now    func() time.Time
	f      *os.File
	w      *bufio.Writer
	start  time.Time // the current file's name
	end    time.Time // when the current file's segment ends
	out    screen
	in     keys
	hidden bool // the input line being typed follows a password prompt
	err    error
}

// Open starts a recording in dir (created owner-only); title heads every
// file. now is the clock (time.Now outside tests). resume, if set, is a
// file of an earlier recording to continue while its segment lasts (a
// reconnect does not start a new file).
func Open(dir, title string, every time.Duration, now func() time.Time, resume *Resume) (*Log, error) {
	if every < MinEvery || every > MaxEvery {
		return nil, fmt.Errorf("reclog: segment %v: use %v to %v", every, MinEvery, MaxEvery)
	}
	l := &Log{dir: dir, title: title, every: every, now: now}
	l.out.emit = func(line string) { l.line(line) }
	l.in.start = func() { l.hidden = passwordPrompt.MatchString(l.out.current()) }
	l.in.emit = func(line string) {
		if l.hidden {
			line = "[hidden]"
		}
		l.hidden = false
		l.line(l.now().Format(stamp) + " > " + line)
	}
	t := now()
	if resume != nil && resume.Dir == dir && t.Before(resume.End) && resume.Every == every {
		if err := l.openFile(resume.Start, resume.End, "resumed"); err != nil {
			return nil, err
		}
		return l, nil
	}
	if err := l.open(t); err != nil {
		return nil, err
	}
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

// segmentEnd is the end of the segment t lies in, counted from local
// midnight.
func segmentEnd(t time.Time, every time.Duration) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	n := t.Sub(day)/every + 1
	return day.Add(n * every)
}

// FileName is the name of a file starting at t.
func FileName(t time.Time) string { return t.Format("20060102-1504") + ".log" }

func (l *Log) open(t time.Time) error {
	return l.openFile(t, segmentEnd(t, l.every), "from")
}

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
	l.start, l.end = start, end
	fmt.Fprintf(l.w, "# %s — recording %s %s\n", l.title, how, l.now().Format(stamp))
	return l.w.Flush()
}

// rotate starts the next file once the segment is over.
func (l *Log) rotate() {
	t := l.now()
	if l.f == nil || t.Before(l.end) {
		return
	}
	next := segmentEnd(t, l.every).Add(-l.every) // the segment t is in
	fmt.Fprintf(l.w, "# continued in %s\n", FileName(next))
	l.w.Flush()
	l.f.Close()
	l.f = nil
	if err := l.open(next); err != nil {
		l.err = err
	}
}

func (l *Log) line(s string) {
	l.rotate()
	if l.f == nil {
		return
	}
	l.w.WriteString(s)
	l.w.WriteByte('\n')
}

// Output records what the remote side sent.
func (l *Log) Output(b []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out.write(b)
	l.flush()
}

// Input records what the user typed or pasted.
func (l *Log) Input(b []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.in.write(b)
	l.flush()
}

func (l *Log) flush() {
	if l.f != nil {
		if err := l.w.Flush(); err != nil && l.err == nil {
			l.err = err
		}
	}
}

// Err is the first write error, if any.
func (l *Log) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// Close ends the recording with a last line saying why.
func (l *Log) Close(reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	if l.out.current() != "" {
		l.out.end(false)
	}
	l.line(fmt.Sprintf("# %s at %s", reason, l.now().Format(stamp)))
	l.w.Flush()
	l.f.Close()
	l.f = nil
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
	return s
}
