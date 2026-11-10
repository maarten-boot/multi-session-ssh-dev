package main

// What claude cost at API prices (SPEC F18): every minute one ssh call per
// agent host reads what was added to the transcripts of its sessions'
// project folders (internal/cost); the counts are kept in <data
// dir>/cost.json and the window shows today's and the total cost.

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"session-workbench/internal/cost"
	"session-workbench/internal/idle"
	"session-workbench/internal/session"
	"session-workbench/internal/xfer"
)

type costState struct {
	mu     sync.Mutex
	ledger *cost.Ledger
	file   string
	failed map[string]time.Time   // host → when it last could not be read
	usage  map[string]*cost.Usage // host → the subscription usage saved there
	round  sync.Mutex             // one round at a time
}

func (a *App) costInit() {
	a.cost.file = filepath.Join(a.dataDir, "cost.json")
	a.cost.failed = map[string]time.Time{}
	a.cost.usage = map[string]*cost.Usage{}
	l, err := cost.Load(a.cost.file)
	if err != nil {
		log.Printf("cost: %v (counting again)", err)
	}
	a.cost.ledger = l
}

func (a *App) costLoop(ctx context.Context) {
	a.costRound(ctx)
	t := time.NewTicker(idleTick())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.costRound(ctx)
		}
	}
}

// costRetry: an agent host that could not be read is tried again after this.
const costRetry = 5 * time.Minute

// costRound reads the transcripts of the active sessions, a host at a time
// in parallel; a container session only while its container runs.
func (a *App) costRound(ctx context.Context) {
	cfg := a.sessions.Config()
	if cfg.UI.ShowCost != nil && !*cfg.UI.ShowCost {
		return
	}
	a.cost.round.Lock()
	defer a.cost.round.Unlock()
	byHost := map[string][]cost.Item{}
	var all []string
	for _, s := range cfg.Sessions {
		all = append(all, s.ID)
		if s.Inactive {
			continue
		}
		r, err := a.sessions.Resolve(s.ID)
		if err != nil || r.Host.SSHTarget == "" {
			continue
		}
		if r.Host.Kind == session.HostContainer && a.containerState(s.ID) != "running" {
			continue
		}
		byHost[r.Host.SSHTarget] = append(byHost[r.Host.SSHTarget], cost.Item{ID: s.ID, Dir: r.RemotePath})
	}
	table := cost.Table(cfg.Prices)
	var wg sync.WaitGroup
	for host, items := range byHost {
		a.cost.mu.Lock()
		skip := time.Since(a.cost.failed[host]) < costRetry
		known := a.cost.ledger.Known(host)
		a.cost.mu.Unlock()
		if skip {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := costRead(ctx, host, items, known)
			a.cost.mu.Lock()
			defer a.cost.mu.Unlock()
			if err != nil {
				log.Printf("cost: reading %s: %v", host, err)
				a.cost.failed[host] = time.Now()
				return
			}
			delete(a.cost.failed, host)
			ids := make([]string, len(items))
			for i, it := range items {
				ids[i] = it.ID
			}
			a.cost.ledger.Apply(host, ids, res, table, time.Now())
			if res.Usage != nil {
				res.Usage.Host = host
				a.cost.usage[host] = res.Usage
			}
		}()
	}
	wg.Wait()
	a.cost.mu.Lock()
	a.cost.ledger.Keep(all)
	err := a.cost.ledger.Save(a.cost.file)
	a.cost.mu.Unlock()
	if err != nil {
		log.Printf("cost: %v", err)
	}
	a.costChanged()
}

func costRead(ctx context.Context, host string, items []cost.Item, known map[string]int64) (cost.Result, error) {
	script, err := cost.Script(items, known, idle.Dir(), idle.StatusHelper)
	if err != nil {
		return cost.Result{}, err
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Minute) // the first time reads every transcript
	defer cancel()
	out, code, stderr, err := xfer.Run(c, host, shScript(script), nil)
	if err != nil {
		return cost.Result{}, err
	}
	if code != 0 {
		return cost.Result{}, fmt.Errorf("exit %d: %s", code, firstLine(stderr))
	}
	return cost.Parse(string(out)), nil
}

func (a *App) costChanged() { wruntime.EventsEmit(a.ctx, "cost", a.CostSummary()) }

// CostSummary is today's and the total cost (and per session) at API prices.
func (a *App) CostSummary() cost.Summary {
	table := cost.Table(a.sessions.Config().Prices)
	a.cost.mu.Lock()
	defer a.cost.mu.Unlock()
	s := a.cost.ledger.Summary(table, time.Now())
	for _, u := range a.cost.usage { // the freshest of the hosts
		if s.Usage == nil || u.At > s.Usage.At {
			s.Usage = u
		}
	}
	return s
}

// ResetCost starts the total again from now (today stays).
func (a *App) ResetCost() error {
	a.cost.mu.Lock()
	a.cost.ledger.ResetTotal(time.Now())
	err := a.cost.ledger.Save(a.cost.file)
	a.cost.mu.Unlock()
	a.costChanged()
	return err
}

// usageOn: claude gets the status line that saves the subscription usage
// (with the cost shown, SPEC F18).
func (a *App) usageOn() bool {
	u := a.sessions.Config().UI.ShowCost
	return u == nil || *u
}

// CostNow counts again at once (the settings turned the cost on).
func (a *App) CostNow() { go a.costRound(a.ctx) }
