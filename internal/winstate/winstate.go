// Package winstate keeps the window's size across restarts: saved in
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

// State is the window's normal (not maximised) size and whether it was
// maximised.
type State struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
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

// Closing records the window as it closes. While maximised (or fullscreen
// or minimised, normal false) the size is not the one to come back to, so
// the last normal size is kept.
func Closing(dir string, width, height int, maximised, normal bool) error {
	s := Load(dir)
	s.Maximised = maximised
	if normal && !maximised {
		s.Width, s.Height = width, height
	}
	return Save(dir, s)
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
