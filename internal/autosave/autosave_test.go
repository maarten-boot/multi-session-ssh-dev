package autosave

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func runner(save func(ctx context.Context, id string, progress func(int)) (string, error)) *Runner {
	return &Runner{
		Items: func() []Item { return []Item{{"s-a", "alpha"}, {"s-b", "beta"}} },
		Every: func() (time.Duration, bool) { return time.Hour, true },
		Save:  save,
	}
}

func TestNowAndStatus(t *testing.T) {
	var changes []Status
	r := runner(func(_ context.Context, id string, progress func(int)) (string, error) {
		progress(50)
		if id == "s-b" {
			return "", errors.New("no agent copy")
		}
		return "note", nil
	})
	r.Changed = func(id string, s Status) {
		if id == "s-a" {
			changes = append(changes, s)
		}
	}
	if err := r.Now(context.Background(), "s-a"); err != nil {
		t.Fatal(err)
	}
	if err := r.Now(context.Background(), "s-b"); err == nil {
		t.Error("the failing save gave no error")
	}
	st := r.Status()
	if a := st["s-a"]; a.Busy || a.LastOK.IsZero() || a.Note != "note" || a.Error != "" {
		t.Errorf("s-a: %+v", a)
	}
	if b := st["s-b"]; b.LastOK.IsZero() == false || b.Error != "no agent copy" {
		t.Errorf("s-b: %+v", b)
	}
	// busy (-1), 50%, done
	if len(changes) != 3 || !changes[0].Busy || changes[0].Percent != -1 || changes[1].Percent != 50 || changes[2].Busy {
		t.Errorf("changes: %+v", changes)
	}
}

func TestOneSaveAtATime(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	r := runner(func(context.Context, string, func(int)) (string, error) {
		started <- struct{}{}
		<-release
		return "", nil
	})
	go r.Now(context.Background(), "s-a")
	<-started
	if err := r.Now(context.Background(), "s-a"); !errors.Is(err, ErrBusy) {
		t.Errorf("second save: %v, want ErrBusy", err)
	}
	close(release)
}

func TestDue(t *testing.T) {
	now := time.Now()
	r := runner(nil)
	r.LastOK = func(id string) time.Time {
		if id == "s-a" {
			return now.Add(-30 * time.Minute) // saved in an earlier run
		}
		return time.Time{}
	}
	if r.due("s-a", time.Hour, now) {
		t.Error("s-a due 30 minutes after a save, every hour")
	}
	if !r.due("s-b", time.Hour, now) {
		t.Error("s-b, never saved, not due")
	}
	r.set("s-b", Status{Last: now.Add(-20 * time.Minute), Error: "down"})
	if !r.due("s-b", time.Hour, now) {
		t.Error("a failed save is not retried after 15 minutes")
	}
	r.set("s-b", Status{Busy: true})
	if r.due("s-b", time.Hour, now) {
		t.Error("a busy session is due")
	}
}

// All saves every item in order, reports each step, waits for a save
// already running and does not repeat it.
func TestAll(t *testing.T) {
	var mu sync.Mutex
	var saved []string
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var first atomic.Bool
	r := runner(func(_ context.Context, id string, progress func(int)) (string, error) {
		if id == "s-a" && first.CompareAndSwap(false, true) {
			started <- struct{}{}
			<-release // the scheduled save still running at quit
		}
		mu.Lock()
		saved = append(saved, id)
		mu.Unlock()
		progress(100)
		if id == "s-b" {
			return "", errors.New("unreachable")
		}
		return "", nil
	})
	go r.scheduled(context.Background(), "s-a")
	<-started
	var reports []Progress
	done := make(chan Progress)
	go func() {
		done <- r.All(context.Background(), func(p Progress) { reports = append(reports, p) })
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)
	p := <-done
	if p.Total != 2 || p.Done != 2 || len(p.Failed) != 1 || p.Failed[0] != "beta: unreachable" {
		t.Errorf("result: %+v", p)
	}
	if len(saved) != 2 || saved[0] != "s-a" || saved[1] != "s-b" {
		t.Errorf("saved %v: s-a once (the running save), then s-b", saved)
	}
	sawBeta100 := false
	for _, rp := range reports {
		if rp.Current == "beta" && rp.Percent == 100 {
			sawBeta100 = true
		}
	}
	if !sawBeta100 {
		t.Errorf("no progress for beta: %+v", reports)
	}
}

func TestAllStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := runner(func(context.Context, string, func(int)) (string, error) {
		cancel()
		return "", context.Canceled
	})
	p := r.All(ctx, func(Progress) {})
	if p.Done != 1 {
		t.Errorf("done %d after cancelling in the first save, want 1", p.Done)
	}
}

// A session never sent has nothing to save: no error in its status, not
// failed at quit, due again after the interval (not the 15-minute retry).
func TestNothingToSave(t *testing.T) {
	r := runner(func(_ context.Context, id string, _ func(int)) (string, error) {
		if id == "s-b" {
			return "", fmt.Errorf("%w: no agent copy yet", ErrNothingToSave)
		}
		return "", nil
	})
	p := r.All(context.Background(), func(Progress) {})
	if p.Done != 2 || len(p.Failed) != 0 || len(p.Skipped) != 1 || p.Skipped[0] != "beta" {
		t.Errorf("all: %+v", p)
	}
	b := r.Status()["s-b"]
	if b.Error != "" || b.Note == "" || !b.LastOK.IsZero() {
		t.Errorf("s-b: %+v", b)
	}
	if r.due("s-b", time.Hour, time.Now().Add(20*time.Minute)) {
		t.Error("nothing to save: due again after 20 minutes, every hour")
	}
}
