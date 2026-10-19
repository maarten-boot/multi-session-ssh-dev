package reclog

import (
	"sync"
	"time"
)

// Target is what a Remote tab records: its session, for the settings, the
// log folder and the files' title.
type Target struct {
	Session string
	Name    string
	Host    string
	Path    string // the session's local path
}

// Settings says whether a session records, into which base folder, in
// segments of how long.
type Settings func(session string) (on bool, base string, every time.Duration)

// Recorder keeps the recordings of all Remote tabs: it is the terminal
// manager's tap and starts or stops each tab's log as the settings change,
// from that moment on.
type Recorder struct {
	Settings Settings
	// Changed reports a session starting or stopping to record, or failing
	// to (err set), in order, from a goroutine of its own.
	Changed func(session string, on bool, err error)
	Now     func() time.Time

	// ctl orders Attach, Refresh and Exit, which open and close files; mu
	// guards terms and is held only briefly, as the taps need it.
	ctl    sync.Mutex
	mu     sync.Mutex
	terms  map[string]*entry    // terminal id →
	resume map[string]*Resume   // session → its last file, for a reconnect (ctl)
	exited map[string]time.Time // terminal id → when it ended before Attach (ctl)

	evMu    sync.Mutex
	events  []event
	sending bool
}

type entry struct {
	t     Target
	log   *Log // set under ctl and mu
	dir   string
	every time.Duration
	// An Open that failed with these settings is not tried (and reported)
	// again until they change or the tab reconnects.
	failed    bool
	failDir   string
	failEvery time.Duration
}

type event struct {
	session string
	on      bool
	err     error
}

func (r *Recorder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Attach marks terminal id as a Remote tab of t; it records now if t's
// session should. An Attach after the terminal's Exit is ignored.
func (r *Recorder) Attach(id string, t Target) {
	r.ctl.Lock()
	defer r.ctl.Unlock()
	if _, ok := r.exited[id]; ok {
		delete(r.exited, id)
		return
	}
	r.mu.Lock()
	if r.terms == nil {
		r.terms = map[string]*entry{}
	}
	r.terms[id] = &entry{t: t}
	r.mu.Unlock()
	r.refresh()
}

// Refresh starts and stops recordings to match the settings; a recording
// whose folder or segment length changed starts anew.
func (r *Recorder) Refresh() {
	r.ctl.Lock()
	defer r.ctl.Unlock()
	r.refresh()
}

func (r *Recorder) refresh() {
	r.mu.Lock()
	es := make([]*entry, 0, len(r.terms))
	for _, e := range r.terms {
		es = append(es, e)
	}
	r.mu.Unlock()
	for _, e := range es {
		s := e.t.Session
		on, base, every := r.Settings(s)
		dir := Dir(base, e.t.Host, e.t.Path)
		if e.log != nil && (!on || dir != e.dir || every != e.every) {
			r.stop(e).Close("recording stopped")
			delete(r.resume, s) // switched on again: a new file
			if !on {
				r.changed(s, false, nil)
			}
		}
		if !on {
			e.failed = false
			continue
		}
		if e.log != nil || e.failed && e.failDir == dir && e.failEvery == every {
			continue
		}
		l, err := Open(dir, e.t.Name+" ("+e.t.Host+": "+e.t.Path+")", every, r.now, r.resume[s],
			func(err error) { r.changed(s, err == nil, err) })
		e.failed, e.failDir, e.failEvery = err != nil, dir, every
		if err == nil {
			r.mu.Lock()
			e.log, e.dir, e.every = l, dir, every
			r.mu.Unlock()
		}
		r.changed(s, err == nil, err)
	}
}

// stop detaches e's log from the taps and returns it.
func (r *Recorder) stop(e *entry) *Log {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := e.log
	e.log = nil
	return l
}

// changed queues an event for Changed; one goroutine at a time delivers
// them, in order.
func (r *Recorder) changed(session string, on bool, err error) {
	if r.Changed == nil {
		return
	}
	r.evMu.Lock()
	defer r.evMu.Unlock()
	r.events = append(r.events, event{session, on, err})
	if !r.sending {
		r.sending = true
		go r.deliver()
	}
}

func (r *Recorder) deliver() {
	for {
		r.evMu.Lock()
		if len(r.events) == 0 {
			r.sending = false
			r.evMu.Unlock()
			return
		}
		ev := r.events[0]
		r.events = r.events[1:]
		r.evMu.Unlock()
		r.Changed(ev.session, ev.on, ev.err)
	}
}

func (r *Recorder) log(id string) *Log {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.terms[id]; e != nil {
		return e.log
	}
	return nil
}

// Output, Input and Exit make Recorder a terminal tap; terminals that are
// not Remote tabs are ignored. Output and Input do not wait for the disk.
func (r *Recorder) Output(id string, b []byte) {
	if l := r.log(id); l != nil {
		l.Output(b)
	}
}

func (r *Recorder) Input(id string, b []byte) {
	if l := r.log(id); l != nil {
		l.Input(b)
	}
}

func (r *Recorder) Exit(id string) {
	r.ctl.Lock()
	defer r.ctl.Unlock()
	r.mu.Lock()
	e := r.terms[id]
	delete(r.terms, id)
	r.mu.Unlock()
	if e == nil {
		// Not (yet) attached: remembered a while for a late Attach.
		now := time.Now()
		for k, t := range r.exited {
			if now.Sub(t) > time.Minute {
				delete(r.exited, k)
			}
		}
		if r.exited == nil {
			r.exited = map[string]time.Time{}
		}
		r.exited[id] = now
		return
	}
	if e.log != nil {
		l := r.stop(e)
		l.Close("connection ended") // before a reconnect can open the file
		if r.resume == nil {
			r.resume = map[string]*Resume{}
		}
		r.resume[e.t.Session] = l.Resume()
		r.changed(e.t.Session, false, nil)
	}
}

// Recording lists the sessions recording right now.
func (r *Recorder) Recording() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]bool{}
	for _, e := range r.terms {
		if e.log != nil && !e.log.Failed() {
			out[e.t.Session] = true
		}
	}
	return out
}
