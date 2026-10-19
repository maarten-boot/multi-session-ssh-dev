// Package winstate keeps the window's size and position across restarts: saved in
// <data dir>/window.json when the window closes, read at the next start
// (before the window exists, so it cannot live in the web view's storage).
package winstate

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Default is the size of a first start, or when the file is missing or bad.
var Default = State{Width: 1280, Height: 800}

// Limits for a saved size; anything outside gets Default.
const (
	MinWidth, MinHeight = 640, 400
	MaxSide             = 16384
)

// State is the window's normal (not maximised) size and position and
// whether it was maximised. Without a position (Pos false) the window
// manager places the window.
type State struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Pos       bool `json:"pos,omitempty"`
	X         int  `json:"x,omitempty"`
	Y         int  `json:"y,omitempty"`
	Maximised bool `json:"maximised"`
}

func file(dir string) string { return filepath.Join(dir, "window.json") }

// Load returns the saved state, or Default (dir "" included).
func Load(dir string) State {
	if dir == "" {
		return Default
	}
	b, err := os.ReadFile(file(dir))
	if err != nil {
		return Default
	}
	var s State
	if json.Unmarshal(b, &s) != nil {
		return Default
	}
	if s.Width < MinWidth || s.Height < MinHeight || s.Width > MaxSide || s.Height > MaxSide {
		s.Width, s.Height = Default.Width, Default.Height
	}
	return s
}

// Closing records the window as it closes. A fullscreen or minimised
// window (normal false) changes nothing: Windows reports a minimised window
// as not maximised even when it is. While maximised, size and position are
// not the ones to come back to, so the last normal ones are kept. posKnown
// false (Wayland: apps cannot see or set their position) keeps the saved
// position too.
func Closing(dir string, width, height, x, y int, maximised, normal, posKnown bool) error {
	if dir == "" {
		return nil
	}
	s := Load(dir)
	if !normal {
		return nil
	}
	s.Maximised = maximised
	if !maximised {
		s.Width, s.Height = width, height
		if posKnown {
			s.Pos, s.X, s.Y = true, x, y
		}
	}
	return Save(dir, s)
}

// Screen is a monitor's size; the primary one starts at 0,0.
type Screen struct {
	Width, Height int
	Primary       bool
}

// OnScreen reports whether a window at the saved position would still show
// enough of itself (100 pixels, and its title bar) with these monitors,
// which may have changed since it closed. Only the primary monitor's place
// is known (0,0); the others may be on any side of it, so the check only
// keeps the window from being put off every screen.
func (s State) OnScreen(screens []Screen) bool {
	if !s.Pos || len(screens) == 0 {
		return false
	}
	p := 0 // the primary monitor (the first if none says so)
	for i, sc := range screens {
		if sc.Primary {
			p = i
			break
		}
	}
	otherW, otherH := 0, 0
	for i, sc := range screens {
		if i != p {
			otherW += sc.Width
			otherH += sc.Height
		}
	}
	const margin = 100
	return s.X+s.Width >= margin-otherW && s.X <= screens[p].Width+otherW-margin &&
		s.Y >= -otherH && s.Y <= screens[p].Height+otherH-margin
}

// Save writes the state atomically (temp file + rename).
func Save(dir string, s State) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(s)
	f, err := os.CreateTemp(dir, ".window-*.json")
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), file(dir)); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}
