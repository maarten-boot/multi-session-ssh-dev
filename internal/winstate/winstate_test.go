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
	if err := Closing(dir, 1500, 900, false, true); err != nil {
		t.Fatal(err)
	}
	if s := Load(dir); s != (State{1500, 900, false}) {
		t.Errorf("after close: %+v", s)
	}
	// Maximised: remembered as such, the normal size is kept.
	Closing(dir, 2560, 1400, true, true)
	if s := Load(dir); s != (State{1500, 900, true}) {
		t.Errorf("maximised: %+v", s)
	}
	// Fullscreen or minimised (not normal): size kept.
	Closing(dir, 10, 10, false, false)
	if s := Load(dir); s != (State{1500, 900, false}) {
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
