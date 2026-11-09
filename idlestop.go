package main

// Stopping claude (SPEC F17): sessions whose start command runs claude in
// tmux (on an ssh host or in a container) start it with hooks
// (internal/idle). At start one ssh call per agent host finds the claudes
// stopped earlier; then every minute the hosts of sessions with an idle
// time are checked, and a claude that waited that long for input is
// stopped (not the session on screen). Session menu → Stop claude now
// stops one at once. A stopped claude resumes when its session is opened,
// or with Resume claude.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"session-workbench/internal/config"
	"session-workbench/internal/idle"
	"session-workbench/internal/remote"
	"session-workbench/internal/session"
	"session-workbench/internal/xfer"
)

type idleState struct {
	mu      sync.Mutex
	shown   string                // the session on screen: never stopped for being idle
	left    map[string]time.Time  // when a session left the screen: idle from then on
	stopped map[string]idle.State // claude stopped (idle or by hand), by session id
	working map[string]bool       // a stop or resume runs
}

// idleTick is how often the agent hosts are checked (SW_IDLE_TICK for the
// tests, else a minute).
func idleTick() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("SW_IDLE_TICK")); err == nil && d >= time.Second {
		return d
	}
	return time.Minute
}

// idleApplies: claude can be stopped and resumed in this session — its
// Remote tab runs in tmux and its start command runs claude.
func idleApplies(r session.Resolved) bool {
	return r.Tmux && idle.Runs(r.StartCommand)
}

// idleDir is where a respawned pane starts.
func idleDir(r session.Resolved) string {
	if r.RemotePath == "" {
		return "~"
	}
	return r.RemotePath
}

// shScript runs a POSIX script whatever the login shell is.
func shScript(script string) string { return "exec sh -c " + remote.Quote(script) }

func (a *App) idleLoop(ctx context.Context) {
	a.idleRound(ctx, true) // what was stopped before the app started
	t := time.NewTicker(idleTick())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.idleRound(ctx, false)
		}
	}
}

type idleItem struct {
	r session.Resolved
	d time.Duration // 0: only its state (no idle stop)
}

// idleRound checks the agent hosts: all claude sessions (first), else
// those with an idle time or a stopped claude. A container session only
// while its container runs.
func (a *App) idleRound(ctx context.Context, first bool) {
	cfg := a.sessions.Config()
	a.idle.mu.Lock()
	marked := maps.Clone(a.idle.stopped)
	a.idle.mu.Unlock()
	byHost := map[string][]idleItem{}
	checked := map[string]bool{}
	for _, s := range cfg.Sessions {
		d := config.IdleStop(cfg.UI, s)
		_, isMarked := marked[s.ID]
		if s.Inactive || (d == 0 && !isMarked && !first) {
			continue
		}
		r, err := a.sessions.Resolve(s.ID)
		if err != nil || !idleApplies(r) {
			continue
		}
		if r.Host.Kind == session.HostContainer && a.containerState(s.ID) != "running" {
			continue // its claude is gone with it
		}
		byHost[r.Host.SSHTarget] = append(byHost[r.Host.SSHTarget], idleItem{r, d})
		checked[s.ID] = true
	}
	var wg sync.WaitGroup
	for target, items := range byHost {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.idleHost(ctx, target, items, first)
		}()
	}
	wg.Wait()
	// Sessions no longer checked (deactivated, deleted, container
	// stopped) lose their mark.
	a.idle.mu.Lock()
	changed := false
	for id := range a.idle.stopped {
		if !checked[id] && !a.idle.working[id] {
			delete(a.idle.stopped, id)
			changed = true
		}
	}
	a.idle.mu.Unlock()
	if changed {
		a.idleChanged()
	}
}

// idleCheck reads the state of the sessions on one agent host.
func idleCheck(ctx context.Context, target string, ids []string) ([]idle.State, error) {
	script, err := idle.CheckScript(ids)
	if err != nil {
		return nil, err
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, code, stderr, err := xfer.Run(c, target, shScript(script), nil)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("%s: exit %d: %s", target, code, firstLine(stderr))
	}
	return idle.Parse(string(out)), nil
}

func (a *App) idleHost(ctx context.Context, target string, items []idleItem, first bool) {
	byID := map[string]idleItem{}
	var ids []string
	for _, it := range items {
		byID[it.r.ID] = it
		ids = append(ids, it.r.ID)
	}
	states, err := idleCheck(ctx, target, ids)
	if err != nil {
		log.Printf("idle: checking %v", err)
		return // unreachable: the marks stay as they are
	}
	changed := false
	for _, st := range states {
		it, ok := byID[st.ID]
		if !ok {
			continue
		}
		a.idle.mu.Lock()
		working := a.idle.working[st.ID]
		_, was := a.idle.stopped[st.ID]
		if !working {
			if st.Stopped {
				a.idle.stopped[st.ID] = st
			} else {
				delete(a.idle.stopped, st.ID)
			}
			changed = changed || was != st.Stopped
		}
		shown := a.idle.shown == st.ID
		recent := time.Since(a.idle.left[st.ID]) < it.d // looked at within the idle time
		a.idle.mu.Unlock()
		switch {
		case working:
		case first && st.Stopped && shown:
			go a.idleResume(st.ID) // opened before the check knew it was stopped
		case !shown && !recent && st.Idle(remote.TmuxName(st.ID), it.d):
			if a.stopClaude(ctx, it.r, st, it.d, idle.IdleNotice(it.d)) == nil {
				log.Printf("idle: stopped claude of %s after %s without input", st.ID, idle.Human(it.d))
				changed = true
			}
		}
	}
	if changed {
		a.idleChanged()
	}
}

// errChanged: the claude changed between the check and the stop.
var errChanged = errors.New("claude changed meanwhile (check again)")

// stopClaude stops the claude of a check's state (quiet: how long its
// window must have shown nothing; 0 for Stop claude now).
func (a *App) stopClaude(ctx context.Context, r session.Resolved, st idle.State, quiet time.Duration, notice string) error {
	script, err := idle.StopScript(st, remote.TmuxName(st.ID), idleDir(r), quiet, notice)
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, code, stderr, err := xfer.Run(c, r.Host.SSHTarget, shScript(script), nil)
	switch {
	case err != nil:
		return err
	case code == 3 || code == 4:
		return errChanged
	case code != 0:
		log.Printf("idle: stopping claude of %s: exit %d %s", st.ID, code, stderr)
		return fmt.Errorf("stopping claude: exit %d: %s", code, firstLine(stderr))
	}
	conv := st.Conv
	if conv == "" {
		conv = "-"
	}
	st.Stopped, st.StoppedConv, st.StoppedTx, st.StoppedPane = true, conv, st.Transcript, st.Pane
	a.idle.mu.Lock()
	a.idle.stopped[st.ID] = st
	a.idle.mu.Unlock()
	return nil
}

// StopResult is the answer to Stop claude now: Busy says what claude is
// doing when it was not stopped for that (ask, then force).
type StopResult struct {
	Stopped bool   `json:"stopped"`
	Busy    string `json:"busy"`
}

// claudeSession resolves a session whose claude the app can stop.
func (a *App) claudeSession(id string) (session.Resolved, error) {
	r, err := a.sessions.Resolve(id)
	if err != nil {
		return r, err
	}
	switch {
	case r.Inactive:
		return r, session.ErrInactive
	case !idleApplies(r):
		return r, errors.New("this session does not run claude in tmux")
	case r.Host.Kind == session.HostContainer && a.containerState(id) != "running":
		return r, errors.New("its container is not running")
	}
	return r, nil
}

// StopClaude stops the session's claude now (session menu). A busy claude
// (a turn, a command, a question) is only stopped with force; without it
// the result says what it is doing.
func (a *App) StopClaude(id string, force bool) (StopResult, error) {
	r, err := a.claudeSession(id)
	if err != nil {
		return StopResult{}, err
	}
	if !a.idleBegin(id) {
		return StopResult{}, errors.New("claude is being stopped or resumed already")
	}
	defer a.idleEnd(id)
	states, err := idleCheck(a.ctx, r.Host.SSHTarget, []string{id})
	if err != nil {
		return StopResult{}, err
	}
	if len(states) != 1 {
		return StopResult{}, errors.New("no answer from the agent host")
	}
	st := states[0]
	switch {
	case st.Stopped:
		a.idleMark(st)
		return StopResult{Stopped: true}, nil
	case st.Event == "" || !st.Alive || st.Event == "SessionEnd":
		return StopResult{}, errors.New("no claude of this session runs with the app's hooks " +
			"(ended, or started before this version: end the remote session and reconnect)")
	case st.PaneSession != remote.TmuxName(id):
		return StopResult{}, errors.New("its claude is not in the session's tmux session")
	}
	if busy := busyText(st); busy != "" && !force {
		return StopResult{Busy: busy}, nil
	}
	if err := a.stopClaude(a.ctx, r, st, 0, idle.NowNotice); err != nil {
		return StopResult{}, err
	}
	log.Printf("idle: stopped claude of %s by hand", id)
	a.idleChanged()
	return StopResult{Stopped: true}, nil
}

// busyText says what a claude is doing ("" when it waits for input).
func busyText(st idle.State) string {
	switch {
	case st.Busy > 0:
		return "claude is running a command"
	case st.Event == "UserPromptSubmit":
		return "claude is working on a prompt"
	case st.Event == "Notification":
		return "claude is asking you a question"
	}
	return ""
}

// ResumeClaude resumes the session's stopped claude (session menu).
func (a *App) ResumeClaude(id string) error {
	if _, err := a.claudeSession(id); err != nil {
		return err
	}
	a.idle.mu.Lock()
	_, ok := a.idle.stopped[id]
	a.idle.mu.Unlock()
	if !ok {
		return errors.New("its claude is not stopped")
	}
	return a.resume(id)
}

func (a *App) idleBegin(id string) bool {
	a.idle.mu.Lock()
	defer a.idle.mu.Unlock()
	if a.idle.working[id] {
		return false
	}
	a.idle.working[id] = true
	return true
}

func (a *App) idleEnd(id string) {
	a.idle.mu.Lock()
	delete(a.idle.working, id)
	a.idle.mu.Unlock()
}

func (a *App) idleMark(st idle.State) {
	a.idle.mu.Lock()
	a.idle.stopped[st.ID] = st
	a.idle.mu.Unlock()
	a.idleChanged()
}

// idleResume resumes in the background (opening a session).
func (a *App) idleResume(id string) {
	if err := a.resume(id); err != nil {
		log.Printf("idle: resuming claude of %s: %v", id, err)
	}
}

// resume resumes the session's stopped claude in its tmux pane.
func (a *App) resume(id string) error {
	a.idle.mu.Lock()
	st, ok := a.idle.stopped[id]
	a.idle.mu.Unlock()
	if !ok {
		return nil
	}
	if !a.idleBegin(id) {
		return errors.New("claude is being stopped or resumed already")
	}
	defer a.idleEnd(id)
	r, err := a.sessions.Resolve(id)
	if err != nil {
		return err
	}
	if r.Inactive {
		return session.ErrInactive
	}
	conv := st.StoppedConv
	if conv == "-" || !st.StoppedTx {
		conv = "" // no prompt was sent: the start command again
	}
	pane, err := remote.PaneCommand(idle.ResumeCommand(r.StartCommand, id, conv))
	if err != nil {
		return err
	}
	script, err := idle.ResumeScript(st, remote.TmuxName(id), idleDir(r), pane)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	_, code, stderr, err := xfer.Run(ctx, r.Host.SSHTarget, shScript(script), nil)
	switch {
	case err != nil:
		return err // the mark stays: tried again when opened again
	case code == 0:
		wruntime.EventsEmit(a.ctx, "idle:resumed", id)
	case code == 3 || code == 4: // gone, or taken over by hand
	default:
		return fmt.Errorf("exit %d: %s", code, firstLine(stderr))
	}
	a.idle.mu.Lock()
	delete(a.idle.stopped, id)
	a.idle.mu.Unlock()
	a.idleChanged()
	return nil
}

func (a *App) idleChanged() { wruntime.EventsEmit(a.ctx, "idle", a.IdleStopped()) }

// IdleStopped lists the sessions whose claude is stopped.
func (a *App) IdleStopped() []string {
	a.idle.mu.Lock()
	defer a.idle.mu.Unlock()
	return slices.Sorted(maps.Keys(a.idle.stopped))
}

// SessionShown tells which session is on screen ("" none): it is never
// stopped for being idle, and resumed when it was stopped; the one it
// replaces counts as idle from now on at the earliest.
func (a *App) SessionShown(id string) {
	a.idle.mu.Lock()
	if old := a.idle.shown; old != "" && old != id {
		a.idle.left[old] = time.Now()
	}
	changed := a.idle.shown != id
	a.idle.shown = id
	_, stopped := a.idle.stopped[id]
	a.idle.mu.Unlock()
	if stopped && changed {
		go a.idleResume(id)
	}
}
