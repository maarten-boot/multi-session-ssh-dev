package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"session-workbench/internal/config"
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

// QAEngine is an engine the Q&A tab offers.
type QAEngine struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// QAInfo is the Q&A tab of a session: its conversation (without the API's
// raw messages), whether a question is being answered, the engines, the
// session's engine and model, and where the log goes.
type QAInfo struct {
	Turns   []qa.Turn  `json:"turns"`
	Busy    bool       `json:"busy"`
	Engines []QAEngine `json:"engines"`
	Engine  string     `json:"engine"`
	Model   string     `json:"model"`
	LogDir  string     `json:"log_dir"`
}

func (a *App) qaModel() string {
	if m := strings.TrimSpace(a.sessions.Config().UI.QAModel); m != "" {
		return m
	}
	return qa.DefaultModel
}

// qaCommands are the configured Q&A commands by engine id (the first of a
// name), in config order.
func (a *App) qaCommands() ([]string, map[string]config.QACommand) {
	var ids []string
	byID := map[string]config.QACommand{}
	for _, c := range a.sessions.Config().UI.QACommands {
		id := qa.CommandPrefix + strings.TrimSpace(c.Name)
		if id == qa.CommandPrefix || byID[id].Name != "" {
			continue
		}
		ids = append(ids, id)
		byID[id] = c
	}
	return ids, byID
}

func (a *App) qaEngines() []QAEngine {
	es := []QAEngine{{qa.EngineAPI, qa.EngineName(qa.EngineAPI)}, {qa.EngineClaude, qa.EngineName(qa.EngineClaude)}}
	ids, _ := a.qaCommands()
	for _, id := range ids {
		es = append(es, QAEngine{id, qa.EngineName(id)})
	}
	return es
}

func (a *App) qaKnown(engine string) bool {
	for _, e := range a.qaEngines() {
		if e.ID == engine {
			return true
		}
	}
	return false
}

// qaDefaultEngine is qa_engine, or else the API when ANTHROPIC_API_KEY is
// set, else Claude Code when it is installed.
func (a *App) qaDefaultEngine() string {
	ui := a.sessions.Config().UI
	switch e := strings.TrimSpace(ui.QAEngine); {
	case e == qa.EngineAPI || e == qa.EngineClaude:
		return e
	case e != "" && a.qaKnown(qa.CommandPrefix+e):
		return qa.CommandPrefix + e
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		if _, err := qa.FindClaude(ui.QAClaude); err == nil {
			return qa.EngineClaude
		}
	}
	return qa.EngineAPI
}

// qaChoice is the engine and model of a conversation: its own choice, or
// the defaults.
func (a *App) qaChoice(c qa.Conversation) (engine, model string) {
	engine = c.Engine
	if !a.qaKnown(engine) {
		engine = a.qaDefaultEngine()
	}
	if engine == c.Engine {
		model = c.Model
	}
	if model == "" {
		switch _, cmds := a.qaCommands(); {
		case engine == qa.EngineAPI:
			model = a.qaModel()
		case cmds[engine].Name != "" && len(cmds[engine].Models) > 0:
			model = cmds[engine].Models[0]
		}
	}
	return engine, model
}

// qaEngine is what answers for engine with model.
func (a *App) qaEngine(engine, model string) qa.Engine {
	work := filepath.Join(a.dataDir, "qa", "work")
	switch engine {
	case qa.EngineAPI:
		return qa.Asker{Model: model}
	case qa.EngineClaude:
		return qa.ClaudeCode{Program: a.sessions.Config().UI.QAClaude, Model: model, Dir: work}
	}
	_, cmds := a.qaCommands()
	c := cmds[engine]
	return qa.Command{Name: c.Name, Argv: c.Command, Model: model, Dir: work}
}

// qaAPIModels caches the Claude API's model list.
var qaAPIModels struct {
	sync.Mutex
	list []qa.Model
	at   time.Time
}

// QAModels lists the models of an engine: the Claude API's from the API
// (kept 10 minutes), Claude Code's names, a command's configured ones.
func (a *App) QAModels(engine string) ([]qa.Model, error) {
	switch engine {
	case qa.EngineAPI:
		qaAPIModels.Lock()
		defer qaAPIModels.Unlock()
		if qaAPIModels.list != nil && time.Since(qaAPIModels.at) < 10*time.Minute {
			return qaAPIModels.list, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		ms, err := qa.Asker{}.Models(ctx)
		if err != nil {
			return nil, err
		}
		qaAPIModels.list, qaAPIModels.at = ms, time.Now()
		return ms, nil
	case qa.EngineClaude:
		return qa.ClaudeModels, nil
	}
	_, cmds := a.qaCommands()
	ms := []qa.Model{}
	for _, m := range cmds[engine].Models {
		ms = append(ms, qa.Model{ID: m, Name: m})
	}
	return ms, nil
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
	engine, model := a.qaChoice(c)
	return QAInfo{Turns: c.Turns, Busy: busy, Engines: a.qaEngines(), Engine: engine, Model: model, LogDir: dir}, err
}

// QAChoose sets the session's engine and model. Another engine needs a new
// conversation (each engine knows only its own).
func (a *App) QAChoose(id, engine, model string) error {
	if !a.qaKnown(engine) {
		return fmt.Errorf("unknown Q&A engine %q", engine)
	}
	qaRuns.Lock()
	defer qaRuns.Unlock()
	if qaRuns.stop[id] != nil {
		return fmt.Errorf("stop the answer first")
	}
	store := a.qaStore()
	c, err := store.Load(id)
	if err != nil {
		return err
	}
	if cur, _ := a.qaChoice(c); cur != engine && len(c.Turns) > 0 {
		return fmt.Errorf("start a new conversation to change the engine")
	}
	c.Engine, c.Model = engine, model
	return store.Save(id, c)
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

	engine, model := a.qaChoice(c)
	t := a.qaEngine(engine, model).Ask(ctx, c, question, func(s string) {
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

// QAClear starts a new conversation with the same engine and model (the
// log keeps the old one).
func (a *App) QAClear(id string) error {
	qaRuns.Lock()
	defer qaRuns.Unlock()
	if qaRuns.stop[id] != nil {
		return fmt.Errorf("stop the answer first")
	}
	store := a.qaStore()
	c, err := store.Load(id)
	if err != nil || c.Engine == "" && c.Model == "" {
		return store.Clear(id)
	}
	return store.Save(id, qa.Conversation{Engine: c.Engine, Model: c.Model})
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
