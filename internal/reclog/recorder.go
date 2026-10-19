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
	// to (err set).
	Changed func(session string, on bool, err error)
	Now     func() time.Time

	mu     sync.Mutex
	terms  map[string]*entry  // terminal id →
	resume map[string]*Resume // session → its last file, for a reconnect
}

type entry struct {
	t   Target
	log *Log
}

func (r *Recorder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Attach marks terminal id as a Remote tab of t; it records now if t's
// session should.
func (r *Recorder) Attach(id string, t Target) {
	r.mu.Lock()
	if r.terms == nil {
		r.terms = map[string]*entry{}
	}
	e := &entry{t: t}
	r.terms[id] = e
	r.mu.Unlock()
	r.Refresh()
}

// Refresh starts and stops recordings to match the settings.
func (r *Recorder) Refresh() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.terms {
		on, base, every := r.Settings(e.t.Session)
		switch {
		case on && e.log == nil:
			l, err := Open(Dir(base, e.t.Host, e.t.Path), e.t.Name+" ("+e.t.Host+": "+e.t.Path+")", every, r.now, r.resume[e.t.Session])
			e.log = l
			r.changed(e.t.Session, err == nil, err)
		case !on && e.log != nil:
			e.log.Close("recording stopped")
			delete(r.resume, e.t.Session) // switched on again: a new file
			e.log = nil
			r.changed(e.t.Session, false, nil)
		}
	}
}

func (r *Recorder) changed(session string, on bool, err error) {
	if r.Changed != nil {
		go r.Changed(session, on, err)
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
// not Remote tabs are ignored.
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
	r.mu.Lock()
	e := r.terms[id]
	delete(r.terms, id)
	if e != nil && e.log != nil {
		if r.resume == nil {
			r.resume = map[string]*Resume{}
		}
		r.resume[e.t.Session] = e.log.Resume()
	}
	r.mu.Unlock()
	if e != nil && e.log != nil {
		e.log.Close("connection ended")
		r.changed(e.t.Session, false, nil)
	}
}

// Recording lists the sessions recording right now.
func (r *Recorder) Recording() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]bool{}
	for _, e := range r.terms {
		if e.log != nil {
			out[e.t.Session] = true
		}
	}
	return out
}
