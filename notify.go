package main

import (
	"context"
	goruntime "runtime"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// notifier shows desktop notifications when claude waits (a bell in a
// session you are not looking at; the frontend decides when). Clicking one
// brings the window forward and opens that session. Where notifications
// are not available (no notification service, or macOS refusing them for
// an unsigned app) nothing is shown; the badges still are.
type notifier struct {
	mu    sync.Mutex
	ctx   context.Context
	ok    bool
	asked bool // macOS: authorization requested
}

func notificationID(session string) string { return "wait-" + session }

func (n *notifier) start(ctx context.Context) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ctx = ctx
	if err := runtime.InitializeNotifications(ctx); err != nil {
		runtime.LogInfof(ctx, "desktop notifications unavailable: %v", err)
		return
	}
	n.ok = runtime.IsNotificationAvailable(ctx)
	runtime.OnNotificationResponse(ctx, func(r runtime.NotificationResult) {
		if r.Error != nil {
			return
		}
		runtime.WindowUnminimise(ctx)
		runtime.WindowShow(ctx)
		if id, _ := r.Response.UserInfo["session"].(string); id != "" {
			runtime.EventsEmit(ctx, "notification:open", id)
		}
	})
}

func (n *notifier) stop(ctx context.Context) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ok {
		runtime.CleanupNotifications(ctx)
		n.ok = false
	}
}

// Notify shows (or replaces) the notification for a session.
func (a *App) Notify(session, title, body string) error {
	n := &a.notify
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.ok {
		return nil
	}
	if goruntime.GOOS == "darwin" && !n.asked {
		n.asked = true
		if ok, err := runtime.RequestNotificationAuthorization(n.ctx); err != nil || !ok {
			n.ok = false
			return nil
		}
	}
	return runtime.SendNotification(n.ctx, runtime.NotificationOptions{
		ID: notificationID(session), Title: title, Body: body,
		Data: map[string]interface{}{"session": session},
	})
}

// ClearNotification withdraws a session's notification (it was looked at).
func (a *App) ClearNotification(session string) {
	n := &a.notify
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ok {
		runtime.RemoveNotification(n.ctx, notificationID(session))
	}
}
