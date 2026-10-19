package reclog

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxLine bounds a line; a program that never sends a newline still gets
// its output written.
const maxLine = 10000

// screen turns terminal output into readable lines: escape sequences
// (colors, titles, modes) are dropped, and carriage returns, backspaces and
// line erases edit the current line as a terminal would, so a progress bar
// ends up as its last state. Cursor moves to another row end the line.
// Full-screen programs (claude's UI, vim) still come out as fragments.
type screen struct {
	line   []rune
	col    int
	st     int
	params []byte // of the CSI sequence being read
	pend   []byte // an incomplete UTF-8 sequence from the last write
	emit   func(line string)
}

const (
	ground = iota
	escape
	csi
	osc    // ESC ] … BEL or ST
	oscEsc // ESC inside OSC
	str    // DCS, SOS, PM, APC: ESC P/X/^/_ … ST
	strEsc
	escInter // ESC followed by intermediates, e.g. ESC ( B
)

func (s *screen) write(b []byte) {
	if len(s.pend) > 0 {
		b = append(s.pend, b...)
		s.pend = nil
	}
	for i := 0; i < len(b); {
		c := b[i]
		if s.st == ground && c >= 0x80 {
			if !utf8.FullRune(b[i:]) {
				s.pend = append([]byte(nil), b[i:]...)
				return
			}
			r, n := utf8.DecodeRune(b[i:])
			s.put(r)
			i += n
			continue
		}
		s.byte(c)
		i++
	}
}

func (s *screen) byte(c byte) {
	if c == 0x1b && s.st != osc && s.st != str {
		s.st, s.params = escape, s.params[:0]
		return
	}
	switch s.st {
	case ground:
		switch {
		case c == '\n':
			s.end(true)
		case c == '\r':
			s.col = 0
		case c == '\b':
			s.col = max(s.col-1, 0)
		case c == '\t':
			s.col = (s.col/8 + 1) * 8
		case c >= 0x20 && c < 0x7f:
			s.put(rune(c))
		}
	case escape:
		switch {
		case c == '[':
			s.st = csi
		case c == ']':
			s.st = osc
		case c == 'P' || c == 'X' || c == '^' || c == '_':
			s.st = str
		case c >= 0x20 && c <= 0x2f:
			s.st = escInter
		default:
			s.st = ground // ESC 7, ESC M, ESC c …
		}
	case escInter:
		if c < 0x20 || c > 0x2f {
			s.st = ground
		}
	case csi:
		switch {
		case c >= 0x30 && c <= 0x3f, c >= 0x20 && c <= 0x2f:
			s.params = append(s.params, c)
		case c >= 0x40 && c <= 0x7e:
			s.csi(c)
			s.st = ground
		}
	case osc:
		if c == 0x07 {
			s.st = ground
		} else if c == 0x1b {
			s.st = oscEsc
		}
	case oscEsc:
		s.st = ground // ST (ESC \) or a broken sequence
		if c != '\\' {
			s.byte(c)
		}
	case str:
		if c == 0x1b {
			s.st = strEsc
		}
	case strEsc:
		if c == '\\' {
			s.st = ground
		} else {
			s.st = str
		}
	}
}

// csi handles the sequences that change the text of the current line.
func (s *screen) csi(final byte) {
	p := string(s.params)
	if strings.ContainsAny(p, "?<=>") { // private modes
		return
	}
	n := func(i, def int) int {
		parts := strings.Split(p, ";")
		if i < len(parts) {
			if v, err := strconv.Atoi(parts[i]); err == nil && v > 0 {
				return v
			}
		}
		return def
	}
	switch final {
	case 'K': // erase in line
		switch n(0, 0) {
		case 0:
			if s.col < len(s.line) {
				s.line = s.line[:s.col]
			}
		case 1:
			for i := 0; i < s.col && i < len(s.line); i++ {
				s.line[i] = ' '
			}
		case 2:
			s.line = s.line[:0]
		}
	case 'G', '`':
		s.col = n(0, 1) - 1
	case 'C':
		s.col += n(0, 1)
	case 'D':
		s.col = max(s.col-n(0, 1), 0)
	case 'P': // delete characters
		if s.col < len(s.line) {
			k := min(n(0, 1), len(s.line)-s.col)
			s.line = append(s.line[:s.col], s.line[s.col+k:]...)
		}
	case 'X': // erase characters
		for i := s.col; i < s.col+n(0, 1) && i < len(s.line); i++ {
			s.line[i] = ' '
		}
	case '@': // insert blanks
		if s.col < len(s.line) {
			k := n(0, 1)
			s.line = append(s.line[:s.col], append([]rune(strings.Repeat(" ", k)), s.line[s.col:]...)...)
		}
	case 'H', 'f', 'A', 'B', 'E', 'F', 'd', 'J':
		s.end(false) // another row (or a cleared screen): the line is done
	}
}

func (s *screen) put(r rune) {
	for len(s.line) < s.col {
		s.line = append(s.line, ' ')
	}
	if s.col < len(s.line) {
		s.line[s.col] = r
	} else {
		s.line = append(s.line, r)
	}
	s.col++
	if len(s.line) >= maxLine {
		s.end(true)
	}
}

// end writes the current line; an empty one only for a real newline.
func (s *screen) end(newline bool) {
	text := strings.TrimRight(string(s.line), " ")
	s.line, s.col = s.line[:0], 0
	if text != "" || newline {
		s.emit(text)
	}
}

// current is the line so far (for spotting a password prompt).
func (s *screen) current() string {
	return strings.TrimRight(string(s.line), " ")
}

// keys collects what the user types into lines: editing keys are applied,
// other keys (arrows, function keys) dropped, a paste kept as typed text
// (each of its lines a line).
type keys struct {
	buf   []rune
	st    int
	pend  []byte
	start func()            // the first character of a line
	emit  func(line string) // a finished line (Enter, or ^C / ^D)
}

func (k *keys) write(b []byte) {
	if len(k.pend) > 0 {
		b = append(k.pend, b...)
		k.pend = nil
	}
	for i := 0; i < len(b); {
		c := b[i]
		if k.st == ground && c >= 0x80 {
			if !utf8.FullRune(b[i:]) {
				k.pend = append([]byte(nil), b[i:]...)
				return
			}
			r, n := utf8.DecodeRune(b[i:])
			k.add(r)
			i += n
			continue
		}
		k.byte(c)
		i++
	}
}

func (k *keys) byte(c byte) {
	switch k.st {
	case escape:
		switch c {
		case '[':
			k.st = csi
		case 'O':
			k.st = escInter // SS3: one more byte (F1–F4, keypad)
		default:
			k.st = ground // Alt+key
		}
		return
	case escInter:
		k.st = ground
		return
	case csi: // arrows, function keys, the paste brackets: dropped
		if c >= 0x40 && c <= 0x7e {
			k.st = ground
		}
		return
	}
	switch {
	case c == 0x1b:
		k.st = escape
	case c == '\r' || c == '\n': // Enter, or a line break in a paste
		k.done("")
	case c == 0x7f || c == '\b':
		if len(k.buf) > 0 {
			k.buf = k.buf[:len(k.buf)-1]
		}
	case c == 0x03:
		k.done("^C")
	case c == 0x04:
		k.done("^D")
	case c == 0x1a:
		k.done("^Z")
	case c == '\t' || c >= 0x20:
		k.add(rune(c))
	}
}

func (k *keys) add(r rune) {
	if len(k.buf) == 0 {
		k.start()
	}
	if len(k.buf) < maxLine {
		k.buf = append(k.buf, r)
	}
}

// done ends the line; an empty Enter is not written.
func (k *keys) done(suffix string) {
	line := string(k.buf) + suffix
	k.buf = k.buf[:0]
	if line != "" {
		k.emit(line)
	}
}
