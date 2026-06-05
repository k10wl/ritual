package main

import (
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// wailsNotifier adapts the Wails v3 notification service to the narrow
// notify.Notifier port (design-log/047). The notify subsystem stays Wails-free
// and unit-tested against a fake; this is the only seam that touches the
// platform service. On Windows SendNotification emits a native Toast — the
// service self-registers its AppUserModelID/CLSID under HKCU during
// ServiceStartup, so no runtime permission is involved.
type wailsNotifier struct{ svc *notifications.NotificationService }

func (w *wailsNotifier) Notify(id, title, body string) error {
	return w.svc.SendNotification(notifications.NotificationOptions{
		ID:    id,
		Title: title,
		Body:  body,
	})
}
