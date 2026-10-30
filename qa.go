package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"session-workbench/internal/opener"
	"session-workbench/internal/qa"
	"session-workbench/internal/reclog"
)

// qaRuns are the questions being answered (session id → stop), one per
// session at a time.
var qaRuns = struct {
	sync.Mutex
	stop map[string]context.CancelFunc
}{stop: map[string]context.CancelFunc{}}

func (a *App) qaStore() qa.Store {
	if a.dataDir == "" {
		return qa.Store{}
	}
	return qa.Store{Dir: filepath.Join(a.dataDir, "qa")}
}

// QAInfo is the Q&A tab of a session: its conversation (without the API's
// raw messages), whether a question is being answered, the model and where
// the log goes.
type QAInfo struct {
	Turns  []qa.Turn `json:"turns"`
	Busy   bool      `json:"busy"`
	Model  string    `json:"model"`
	LogDir string    `json:"log_dir"`
}

func (a *App) qaModel() string {
	if m := strings.TrimSpace(a.sessions.Config().UI.QAModel); m != "" {
		return m
	}
	return qa.DefaultModel
}

// qaLogDir is the session's log folder (that of its Remote tab recordings).
func (a *App) qaLogDir(id string) (string, string, error) {
	r, err := a.sessions.Resolve(id)
	if r.ID == "" {
		return "", "", err
	}
	base := a.recordBase()
	if base == "" {
		return "", r.Name, fmt.Errorf("no folder for the Q&A log: set one in Settings → Backups and recordings")
	}
	return reclog.Dir(base, r.Host.Name, r.LocalPath), r.Name, nil
}

// QALoad returns the session's Q&A.
func (a *App) QALoad(id string) (QAInfo, error) {
	c, err := a.qaStore().Load(id)
	for i := range c.Turns {
		c.Turns[i].Raw = nil
	}
	qaRuns.Lock()
	busy := qaRuns.stop[id] != nil
	qaRuns.Unlock()
	dir, _, _ := a.qaLogDir(id)
	return QAInfo{Turns: c.Turns, Busy: busy, Model: a.qaModel(), LogDir: dir}, err
}

// QAAsk asks Claude the question after the session's conversation. The
// answer streams as "qa:<id>" events (text pieces); the finished turn is
// saved, logged and returned.
func (a *App) QAAsk(id, question string) (qa.Turn, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return qa.Turn{}, fmt.Errorf("the question is empty")
	}
	logDir, name, logErr := a.qaLogDir(id)
	if name == "" {
		return qa.Turn{}, logErr
	}
	store := a.qaStore()
	c, err := store.Load(id)
	if err != nil {
		return qa.Turn{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	qaRuns.Lock()
	if qaRuns.stop[id] != nil {
		qaRuns.Unlock()
		return qa.Turn{}, fmt.Errorf("a question of %s is being answered", name)
	}
	qaRuns.stop[id] = cancel
	qaRuns.Unlock()
	defer func() {
		qaRuns.Lock()
		delete(qaRuns.stop, id)
		qaRuns.Unlock()
	}()

	t := qa.Asker{Model: a.qaModel()}.Ask(ctx, c, question, func(s string) {
		runtime.EventsEmit(a.ctx, "qa:"+id, s)
	})
	c.Turns = append(c.Turns, t)
	// The answer is shown even when it could not be kept: the problem is
	// added to the turn.
	var problems []string
	if err := store.Save(id, c); err != nil {
		problems = append(problems, "not saved: "+err.Error())
	}
	if logErr != nil {
		problems = append(problems, "not logged: "+logErr.Error())
	} else if err := qa.Log(logDir, name, t); err != nil {
		problems = append(problems, "not logged: "+err.Error())
	}
	if len(problems) > 0 {
		t.Error = strings.Join(append([]string{t.Error}, problems...), "; ")
		t.Error = strings.TrimPrefix(t.Error, "; ")
	}
	t.Raw = nil
	return t, nil
}

// QAStop stops the answer being written for the session.
func (a *App) QAStop(id string) {
	qaRuns.Lock()
	defer qaRuns.Unlock()
	if stop := qaRuns.stop[id]; stop != nil {
		stop()
	}
}

// QAClear starts a new conversation (the log keeps the old one).
func (a *App) QAClear(id string) error {
	qaRuns.Lock()
	busy := qaRuns.stop[id] != nil
	qaRuns.Unlock()
	if busy {
		return fmt.Errorf("stop the answer first")
	}
	return a.qaStore().Clear(id)
}

// QAOpenLog opens the folder of the session's Q&A log.
func (a *App) QAOpenLog(id string) error {
	dir, name, err := a.qaLogDir(id)
	if dir == "" {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("nothing logged for %s yet (it goes to %s)", name, dir)
	}
	return opener.Open(dir)
}
