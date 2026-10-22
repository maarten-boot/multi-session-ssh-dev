// Package autosave saves the agent copies of working-copy sessions
// regularly while the app runs, and all of them when it quits (SPEC F12):
// the saving itself is the caller's (syncer.Save into the session's saved
// copy); this package decides when, keeps one save per session at a time,
// and reports status and progress.
package autosave

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ScheduledTimeout bounds one scheduled save.
const ScheduledTimeout = 2 * time.Hour

// retryAfter is the wait after a failed save, when the interval is longer.
const retryAfter = 15 * time.Minute

// Item is a working-copy session.
type Item struct {
	ID   string
	Name string
}

// Status is the state of one session's saves.
type Status struct {
	Busy    bool      `json:"busy"`
	Percent int       `json:"percent"` // while busy; -1: not known
	Last    time.Time `json:"last"`    // last attempt (zero: none in this run)
	LastOK  time.Time `json:"last_ok"`
	Error   string    `json:"error"`
	Note    string    `json:"note"`
}

// Progress is the state of saving everything at quit.
type Progress struct {
	Total   int      `json:"total"`
	Done    int      `json:"done"`
	Current string   `json:"current"` // the session being saved
	Percent int      `json:"percent"` // of Current; -1: not known
	Failed  []string `json:"failed"`  // "name: error"
}

// Runner saves the items' agent copies.
type Runner struct {
	Items func() []Item
	// Every is the interval, and whether scheduled saves are on.
	Every func() (time.Duration, bool)
	// Save saves one session's agent copy, reporting a percentage when it
	// can; it returns a note for a save that worked with a caveat.
	Save func(ctx context.Context, id string, progress func(int)) (string, error)
	// LastOK is when an earlier run of the app last saved id (zero: never).
	LastOK func(id string) time.Time
	// Changed is called when a session's status changes.
	Changed func(id string, s Status)

	// StartDelay is the wait before the first round (default 1 minute).
	StartDelay time.Duration

	mu     sync.Mutex
	status map[string]Status
	locks  map[string]*sync.Mutex
}

// ErrBusy: a save of this session is already running.
var ErrBusy = errors.New("this session's agent copy is being saved already")

func (r *Runner) lock(id string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.locks == nil {
		r.locks = map[string]*sync.Mutex{}
	}
	l := r.locks[id]
	if l == nil {
		l = &sync.Mutex{}
		r.locks[id] = l
	}
	return l
}

// Status returns the status of every item.
func (r *Runner) Status() map[string]Status {
	out := map[string]Status{}
	for _, it := range r.Items() {
		out[it.ID] = r.get(it.ID)
	}
	return out
}

func (r *Runner) get(id string) Status {
	r.mu.Lock()
	s, ok := r.status[id]
	r.mu.Unlock()
	if !ok && r.LastOK != nil {
		s.LastOK = r.LastOK(id)
	}
	return s
}

func (r *Runner) set(id string, s Status) {
	r.mu.Lock()
	if r.status == nil {
		r.status = map[string]Status{}
	}
	r.status[id] = s
	r.mu.Unlock()
	if r.Changed != nil {
		r.Changed(id, s)
	}
}

// Now saves one session and waits for it; ErrBusy if it is being saved.
func (r *Runner) Now(ctx context.Context, id string) error {
	l := r.lock(id)
	if !l.TryLock() {
		return ErrBusy
	}
	defer l.Unlock()
	return r.run(ctx, id, nil)
}

// run saves id; the caller holds its lock. progress also gets the
// percentages (-1 at the start).
func (r *Runner) run(ctx context.Context, id string, progress func(int)) error {
	s := r.get(id)
	s.Busy, s.Percent = true, -1
	r.set(id, s)
	if progress != nil {
		progress(-1)
	}
	note, err := r.Save(ctx, id, func(p int) {
		cur := r.get(id)
		cur.Percent = p
		r.set(id, cur)
		if progress != nil {
			progress(p)
		}
	})
	s = r.get(id)
	s.Busy, s.Percent, s.Last, s.Error, s.Note = false, 0, time.Now(), "", note
	if err != nil {
		s.Error = err.Error()
	} else {
		s.LastOK = s.Last
	}
	r.set(id, s)
	return err
}

// due: the interval has passed since the last save (or attempt); after a
// failure, retry every 15 minutes at most.
func (r *Runner) due(id string, every time.Duration, now time.Time) bool {
	s := r.get(id)
	if s.Busy {
		return false
	}
	last, wait := s.LastOK, every
	if s.Last.After(last) {
		last = s.Last
	}
	if s.Error != "" && wait > retryAfter {
		wait = retryAfter
	}
	return now.Sub(last) >= wait
}

// scheduled is a save started by Start; skipped while one is running.
func (r *Runner) scheduled(ctx context.Context, id string) {
	l := r.lock(id)
	if !l.TryLock() {
		return
	}
	defer l.Unlock()
	ctx, cancel := context.WithTimeout(ctx, ScheduledTimeout)
	defer cancel()
	r.run(ctx, id, nil)
}

// Start runs scheduled saves in the background until ctx ends.
func (r *Runner) Start(ctx context.Context) {
	delay := r.StartDelay
	if delay == 0 {
		delay = time.Minute
	}
	go func() {
		t := time.NewTimer(delay)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if every, on := r.Every(); on {
				for _, it := range r.Items() {
					if r.due(it.ID, every, time.Now()) {
						go r.scheduled(ctx, it.ID)
					}
				}
			}
			t.Reset(time.Minute)
		}
	}()
}

// All saves every item, one after another (for quitting), and reports
// progress after each step. A save already running is waited for; when it
// ends after All started, that session counts as saved. It stops early
// when ctx ends.
func (r *Runner) All(ctx context.Context, report func(Progress)) Progress {
	start := time.Now()
	items := r.Items()
	p := Progress{Total: len(items), Percent: -1}
	report(p)
	for _, it := range items {
		if ctx.Err() != nil {
			break
		}
		p.Current, p.Percent = it.Name, -1
		report(p)
		l := r.lock(it.ID)
		l.Lock()
		var err error
		if !r.get(it.ID).LastOK.After(start) {
			err = r.run(ctx, it.ID, func(pct int) {
				p.Percent = pct
				report(p)
			})
		}
		l.Unlock()
		if err != nil {
			p.Failed = append(p.Failed, it.Name+": "+err.Error())
		}
		p.Done++
		p.Current, p.Percent = "", -1
		report(p)
	}
	return p
}
