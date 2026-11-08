package main

// One project on several agent hosts (SPEC F16): session menu → Switch
// host… moves the session to another host with its Claude Code state
// (internal/handover); the project's requirements list is checked on a
// host (internal/requirements).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"session-workbench/internal/config"
	"session-workbench/internal/handover"
	"session-workbench/internal/remote"
	"session-workbench/internal/requirements"
	"session-workbench/internal/session"
	"session-workbench/internal/xfer"
)

// SwitchTarget is a host the session could move to.
type SwitchTarget struct {
	Name       string `json:"name"`
	Files      string `json:"files"`       // its file access mode
	RemotePath string `json:"remote_path"` // the project's path there
	Previous   bool   `json:"previous"`    // the session worked there before
	Problem    string `json:"problem"`     // why it cannot go there, else ""
}

// SwitchTargets lists the other hosts for Switch host…: those it worked on
// before first (most recent first), then the rest in the config's order.
func (a *App) SwitchTargets(id string) ([]SwitchTarget, error) {
	r, err := a.sessions.Resolve(id)
	if r.ID == "" {
		return nil, err
	}
	if why := session.CanSwitch(r.Host, r.Session); why != "" {
		return nil, fmt.Errorf("this session cannot switch hosts: %s", why)
	}
	cfg := a.sessions.Config()
	var out []SwitchTarget
	add := func(name string, previous bool) {
		if name == r.Host.Name || slices.ContainsFunc(out, func(t SwitchTarget) bool { return t.Name == name }) {
			return
		}
		t := SwitchTarget{Name: name, Previous: previous}
		if nr, err := a.sessions.ResolveOn(id, name); err != nil {
			t.Problem = err.Error()
		} else {
			t.RemotePath, t.Files = nr.RemotePath, session.FilesMode(nr.Host)
		}
		out = append(out, t)
	}
	for _, h := range r.Hosts {
		if slices.ContainsFunc(cfg.Hosts, func(c config.Host) bool { return c.Name == h }) {
			add(h, true)
		}
	}
	for _, h := range cfg.Hosts {
		add(h.Name, false)
	}
	return out, nil
}

// remoteAbs is path on target with a leading ~ made absolute (Claude Code
// files a project by its absolute path).
func remoteAbs(ctx context.Context, target, path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	out, code, stderr, err := xfer.Run(ctx, target, `printf '%s' "$HOME"`, nil)
	if err != nil {
		return "", err
	}
	home := strings.TrimSpace(string(out))
	if code != 0 || !strings.HasPrefix(home, "/") {
		return "", fmt.Errorf("%s: no home folder (%s)", target, strings.TrimSpace(stderr))
	}
	return strings.TrimRight(home, "/") + strings.TrimPrefix(path, "~"), nil
}

// SwitchHost moves the session to host: with endOld it first ends the
// tmux session on the old host (claude there would go on writing), stops
// a live share, copies the project's Claude Code state over (newer wins,
// nothing deleted; progress as "switch:<id>" events), and then makes host
// the session's host. A working copy's code is the frontend's part (Get
// from agent before, Send to agent after).
func (a *App) SwitchHost(id, host string, endOld bool) (handover.Report, error) {
	var rep handover.Report
	r, err := a.sessions.Resolve(id)
	if err != nil {
		return rep, err
	}
	if r.Inactive {
		return rep, session.ErrInactive
	}
	nr, err := a.sessions.ResolveOn(id, host)
	if err != nil {
		return rep, err
	}
	if why := session.CanSwitch(r.Host, r.Session); why != "" {
		return rep, fmt.Errorf("this session cannot switch hosts: %s", why)
	}
	if a.dataDir == "" {
		return rep, errors.New("no data folder for the switch's files")
	}
	out := eventWriter(func(s string) { wruntime.EventsEmit(a.ctx, "switch:"+id, strings.ReplaceAll(s, "\n", "\r\n")) })
	if endOld && r.Tmux {
		fmt.Fprintf(out, "Ending tmux session %s on %s…\n", remote.TmuxName(id), r.Host.Name)
		if err := a.KillRemote(id); err != nil {
			return rep, err
		}
	}
	if session.FilesMode(r.Host) == session.FilesSSHFS {
		a.shares.Stop(id) // the live share of the old host
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
	defer cancel()
	from, err := remoteAbs(ctx, r.Host.SSHTarget, r.RemotePath)
	if err != nil {
		return rep, err
	}
	to, err := remoteAbs(ctx, nr.Host.SSHTarget, nr.RemotePath)
	if err != nil {
		return rep, err
	}
	stage := filepath.Join(a.dataDir, "handover", id)
	rep, err = handover.Switch(ctx, handover.Side{Target: r.Host.SSHTarget, Path: from},
		handover.Side{Target: nr.Host.SSHTarget, Path: to}, stage, out)
	if err != nil {
		return rep, err
	}
	if err := a.sessions.SetHost(id, host); err != nil {
		return rep, err
	}
	fmt.Fprintf(out, "The session is on %s now.\n", host)
	return rep, nil
}

// RequirementsFile is a project's requirements list for its window.
type RequirementsFile struct {
	Path   string `json:"path"`
	Text   string `json:"text"` // the file, or requirements.Template
	Exists bool   `json:"exists"`
}

func (a *App) requirementsPath(id string) (string, error) {
	r, err := a.sessions.Resolve(id)
	if r.ID == "" {
		return "", err
	}
	if r.Plain || r.LocalPath == "" {
		return "", session.ErrNoLocalDir
	}
	return filepath.Join(r.LocalPath, filepath.FromSlash(requirements.File)), nil
}

// Requirements reads the project's requirements list.
func (a *App) Requirements(id string) (RequirementsFile, error) {
	p, err := a.requirementsPath(id)
	if err != nil {
		return RequirementsFile{}, err
	}
	f := RequirementsFile{Path: p, Text: requirements.Template}
	if b, err := os.ReadFile(p); err == nil {
		f.Text, f.Exists = string(b), true
	} else if !errors.Is(err, os.ErrNotExist) {
		return f, err
	}
	return f, nil
}

// SaveRequirements writes the list (refused while a line cannot be read).
func (a *App) SaveRequirements(id, text string) (string, error) {
	p, err := a.requirementsPath(id)
	if err != nil {
		return "", err
	}
	if _, errs := requirements.Parse(text); len(errs) > 0 {
		return "", errors.Join(errs...)
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}

// RequirementsCheck is the list checked on one host.
type RequirementsCheck struct {
	Host    string                `json:"host"`
	Exists  bool                  `json:"exists"` // the project has a list
	Results []requirements.Result `json:"results"`
	Errors  []string              `json:"errors"`  // lines that could not be read
	Missing int                   `json:"missing"` // results not OK
}

// CheckRequirements checks the project's list (text, or the saved file
// when text is "") on host ("" = the session's own) over ssh.
func (a *App) CheckRequirements(id, host, text string) (RequirementsCheck, error) {
	r, err := a.sessions.Resolve(id)
	if err != nil {
		return RequirementsCheck{}, err
	}
	if host != "" && host != r.Host.Name {
		if r, err = a.sessions.ResolveOn(id, host); err != nil {
			return RequirementsCheck{}, err
		}
	}
	c := RequirementsCheck{Host: r.Host.Name}
	if text == "" {
		f, err := a.Requirements(id)
		if err != nil || !f.Exists {
			return c, err
		}
		text = f.Text
	}
	c.Exists = true
	reqs, errs := requirements.Parse(text)
	for _, e := range errs {
		c.Errors = append(c.Errors, e.Error())
	}
	if len(reqs) == 0 {
		return c, nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	out, code, stderr, err := xfer.Run(ctx, r.Host.SSHTarget, requirements.Script(reqs), nil)
	if err != nil {
		return c, err
	}
	if code == 255 {
		return c, fmt.Errorf("ssh to %s: %s", r.Host.SSHTarget, strings.TrimSpace(stderr))
	}
	c.Results = requirements.Evaluate(reqs, string(out))
	for _, res := range c.Results {
		if !res.OK {
			c.Missing++
		}
	}
	return c, nil
}
