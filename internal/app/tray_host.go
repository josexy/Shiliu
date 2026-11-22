package app

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"
)

// statusNotifierHostAvailable requires a registered host, not just an indicator
// object or a running watcher. A private, short-lived connection recovers from
// session bus restarts and never launches a new bus or activates a service.
func statusNotifierHostAvailable(parent context.Context) bool {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	conn, err := dbus.SessionBusPrivateNoAutoStartup(dbus.WithContext(ctx))
	if err != nil {
		return false
	}
	defer conn.Close()
	if conn.Auth(nil) != nil || conn.Hello() != nil {
		return false
	}
	return statusNotifierHostRegistered(ctx, conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher"))
}

type trayHostObject interface {
	CallWithContext(context.Context, string, dbus.Flags, ...interface{}) *dbus.Call
}

func statusNotifierHostRegistered(ctx context.Context, object trayHostObject) bool {
	var value dbus.Variant
	err := object.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", dbus.FlagNoAutoStart,
		"org.kde.StatusNotifierWatcher", "IsStatusNotifierHostRegistered").Store(&value)
	if err != nil {
		return false
	}
	registered, ok := value.Value().(bool)
	return ok && registered
}

// watchTrayHost publishes only transitions, including the initial result.
// Cancellation suppresses a result from a probe that was already in flight.
func watchTrayHost(ctx context.Context, probe func(context.Context) bool, interval time.Duration, publish func(bool)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	first, previous := true, false
	for ctx.Err() == nil {
		available := probe(ctx)
		if ctx.Err() != nil {
			return
		}
		if first || available != previous {
			publish(available)
			first, previous = false, available
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
