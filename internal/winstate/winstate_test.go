package winstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if s := Load(dir); s != Default {
		t.Errorf("first start: %+v", s)
	}
	if err := Closing(dir, 1500, 900, 40, 30, false, true); err != nil {
		t.Fatal(err)
	}
	if s := Load(dir); s != (State{Width: 1500, Height: 900, Pos: true, X: 40, Y: 30}) {
		t.Errorf("after close: %+v", s)
	}
	// Maximised: remembered as such, the normal size and position are kept.
	Closing(dir, 2560, 1400, 0, 0, true, true)
	if s := Load(dir); s != (State{Width: 1500, Height: 900, Pos: true, X: 40, Y: 30, Maximised: true}) {
		t.Errorf("maximised: %+v", s)
	}
	// Fullscreen or minimised (not normal): size and position kept.
	Closing(dir, 10, 10, -5000, -5000, false, false)
	if s := Load(dir); s != (State{Width: 1500, Height: 900, Pos: true, X: 40, Y: 30}) {
		t.Errorf("not normal: %+v", s)
	}
}

func TestBadFiles(t *testing.T) {
	if s := Load(""); s != Default {
		t.Errorf("no data dir: %+v", s)
	}
	for _, body := range []string{"not json", `{"width":100,"height":100}`, `{"width":99999,"height":800}`} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "window.json"), []byte(body), 0o600)
		if s := Load(dir); s.Width != Default.Width || s.Height != Default.Height {
			t.Errorf("%s: %+v", body, s)
		}
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "window.json"), []byte(`{"width":100,"height":100,"maximised":true}`), 0o600)
	if s := Load(dir); !s.Maximised {
		t.Errorf("a bad size keeps maximised: %+v", s)
	}
}

func TestOnScreen(t *testing.T) {
	one := []Screen{{1920, 1080, true}}
	two := []Screen{{2560, 1440, false}, {1920, 1080, true}}
	at := func(x, y int) State { return State{Width: 1200, Height: 800, Pos: true, X: x, Y: y} }
	for _, c := range []struct {
		s       State
		screens []Screen
		want    bool
	}{
		{at(100, 50), one, true},
		{at(-1100, 50), one, true},                         // 100px showing on the left
		{at(-1150, 50), one, false},                        // 50px
		{at(1820, 980), one, true},                         // bottom right corner showing
		{at(1850, 50), one, false},                         // 70px on the right
		{at(100, 1000), one, false},                        // title bar near the bottom edge
		{at(100, -20), one, false},                         // title bar above the top
		{at(3000, 100), one, false},                        // was on a second monitor that is gone
		{at(3000, 100), two, true},                         // ... that is there
		{at(-2000, 100), two, true},                        // a monitor left of the primary one
		{at(-9000, 100), two, false},                       // beyond every monitor
		{State{Width: 1200, Height: 800}, one, false},      // never saved
		{at(100, 50), nil, false},                          // monitors unknown
		{at(100, 50), []Screen{{1920, 1080, false}}, true}, // no primary: the first
	} {
		if got := c.s.OnScreen(c.screens); got != c.want {
			t.Errorf("%+v on %v: %v, want %v", c.s, c.screens, got, c.want)
		}
	}
}
