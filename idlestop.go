package main

// Stopping an idle claude (SPEC F17): sessions with an idle time
// (Settings, or Edit session…) start claude with hooks (internal/idle);
// every minute one ssh call per agent host reads their state, stops a
// claude that waited that long for input (not the session on screen), and
// the session is resumed when it is opened.

import (
	"context"
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
	shown   string                // the session on screen: never stopped
	left    map[string]time.Time  // when a session left the screen: idle from then on
	stopped map[string]idle.State // stopped for being idle, by session id
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
// Remote tab runs in tmux on an ssh host and its start command runs claude.
func idleApplies(r session.Resolved) bool {
	return r.Tmux && r.Host.Kind != session.HostContainer && idle.Runs(r.StartCommand)
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
	t := time.NewTicker(idleTick())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.idleRound(ctx)
		}
	}
}

type idleItem struct {
	r session.Resolved
	d time.Duration
}

// idleRound checks all agent hosts with sessions that have an idle time.
func (a *App) idleRound(ctx context.Context) {
	cfg := a.sessions.Config()
	byHost := map[string][]idleItem{}
	checked := map[string]bool{}
	for _, s := range cfg.Sessions {
		d := config.IdleStop(cfg.UI, s)
		if d == 0 || s.Inactive {
			continue
		}
		r, err := a.sessions.Resolve(s.ID)
		if err != nil || !idleApplies(r) {
			continue
		}
		byHost[r.Host.SSHTarget] = append(byHost[r.Host.SSHTarget], idleItem{r, d})
		checked[s.ID] = true
	}
	var wg sync.WaitGroup
	for target, items := range byHost {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.idleHost(ctx, target, items)
		}()
	}
	wg.Wait()
	// Sessions no longer checked (setting off, deactivated, deleted) lose
	// their mark; their claude stays stopped until the session is opened.
	a.idle.mu.Lock()
	changed := false
	for id := range a.idle.stopped {
		if !checked[id] {
			delete(a.idle.stopped, id)
			changed = true
		}
	}
	a.idle.mu.Unlock()
	if changed {
		a.idleChanged()
	}
}

func (a *App) idleHost(ctx context.Context, target string, items []idleItem) {
	byID := map[string]idleItem{}
	var ids []string
	for _, it := range items {
		byID[it.r.ID] = it
		ids = append(ids, it.r.ID)
	}
	script, err := idle.CheckScript(ids)
	if err != nil {
		log.Printf("idle: %v", err)
		return
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, code, stderr, err := xfer.Run(c, target, shScript(script), nil)
	if err != nil || code != 0 {
		log.Printf("idle: checking %s: exit %d %v %s", target, code, err, stderr)
		return // unreachable: the marks stay as they are
	}
	changed := false
	for _, st := range idle.Parse(string(out)) {
		it, ok := byID[st.ID]
		if !ok {
			continue
		}
		a.idle.mu.Lock()
		_, was := a.idle.stopped[st.ID]
		if st.Stopped {
			a.idle.stopped[st.ID] = st
		} else {
			delete(a.idle.stopped, st.ID)
		}
		changed = changed || was != st.Stopped
		shown, working := a.idle.shown == st.ID, a.idle.working[st.ID]
		recent := time.Since(a.idle.left[st.ID]) < it.d // looked at within the idle time
		a.idle.mu.Unlock()
		switch {
		case working:
		case st.Stopped && shown:
			go a.idleResume(st.ID) // opened before the check knew it was stopped
		case !shown && !recent && st.Idle(remote.TmuxName(st.ID), it.d):
			if a.idleStop(ctx, it, st) {
				changed = true
			}
		}
	}
	if changed {
		a.idleChanged()
	}
}

// idleStop stops the claude of a check's state; true when it did.
func (a *App) idleStop(ctx context.Context, it idleItem, st idle.State) bool {
	script, err := idle.StopScript(st, remote.TmuxName(st.ID), idleDir(it.r), it.d)
	if err != nil {
		log.Printf("idle: %v", err)
		return false
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, code, stderr, err := xfer.Run(c, it.r.Host.SSHTarget, shScript(script), nil)
	if err != nil || code != 0 {
		if code != 3 && code != 4 { // 3, 4: it changed meanwhile
			log.Printf("idle: stopping claude of %s: exit %d %v %s", st.ID, code, err, stderr)
		}
		return false
	}
	conv := st.Conv
	if conv == "" {
		conv = "-"
	}
	st.Stopped, st.StoppedConv, st.StoppedTx, st.StoppedPane = true, conv, st.Transcript, st.Pane
	a.idle.mu.Lock()
	a.idle.stopped[st.ID] = st
	a.idle.mu.Unlock()
	log.Printf("idle: stopped claude of %s after %s without input", st.ID, idle.Human(it.d))
	return true
}

// idleResume resumes the session's stopped claude in its tmux pane.
func (a *App) idleResume(id string) {
	a.idle.mu.Lock()
	st, ok := a.idle.stopped[id]
	if !ok || a.idle.working[id] {
		a.idle.mu.Unlock()
		return
	}
	a.idle.working[id] = true
	a.idle.mu.Unlock()
	defer func() {
		a.idle.mu.Lock()
		delete(a.idle.working, id)
		a.idle.mu.Unlock()
	}()
	r, err := a.sessions.Resolve(id)
	if err != nil || r.Inactive {
		return
	}
	conv := st.StoppedConv
	if conv == "-" || !st.StoppedTx {
		conv = "" // no prompt was sent: the start command again
	}
	pane, err := remote.PaneCommand(idle.ResumeCommand(r.StartCommand, id, conv))
	if err == nil {
		var script string
		if script, err = idle.ResumeScript(st, remote.TmuxName(id), idleDir(r), pane); err == nil {
			ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
			defer cancel()
			var code int
			var stderr string
			_, code, stderr, err = xfer.Run(ctx, r.Host.SSHTarget, shScript(script), nil)
			switch {
			case err != nil:
			case code == 0:
				wruntime.EventsEmit(a.ctx, "idle:resumed", id)
			case code == 3 || code == 4: // gone, or taken over by hand
			default:
				log.Printf("idle: resuming claude of %s: exit %d %s", id, code, stderr)
				return // the mark stays: tried again when opened again
			}
		}
	}
	if err != nil {
		log.Printf("idle: resuming claude of %s: %v", id, err)
		return
	}
	a.idle.mu.Lock()
	delete(a.idle.stopped, id)
	a.idle.mu.Unlock()
	a.idleChanged()
}

func (a *App) idleChanged() { wruntime.EventsEmit(a.ctx, "idle", a.IdleStopped()) }

// IdleStopped lists the sessions whose claude was stopped for being idle.
func (a *App) IdleStopped() []string {
	a.idle.mu.Lock()
	defer a.idle.mu.Unlock()
	return slices.Sorted(maps.Keys(a.idle.stopped))
}

// SessionShown tells which session is on screen ("" none): it is never
// stopped, and resumed when it was; the one it replaces counts as idle
// from now on at the earliest.
func (a *App) SessionShown(id string) {
	a.idle.mu.Lock()
	if old := a.idle.shown; old != "" && old != id {
		a.idle.left[old] = time.Now()
	}
	a.idle.shown = id
	_, stopped := a.idle.stopped[id]
	a.idle.mu.Unlock()
	if stopped {
		go a.idleResume(id)
	}
}
