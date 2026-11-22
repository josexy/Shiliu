package app

import (
	"context"
	"os"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

// Run only in a private bus: SHILIU_DBUS_TEST=1 dbus-run-session -- go test
// -run '^TestStatusNotifierHostSessionBus$' ./internal/app
func TestStatusNotifierHostSessionBus(t *testing.T) {
	if os.Getenv("SHILIU_DBUS_TEST") != "1" {
		t.Skip("requires an isolated dbus-run-session")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	const watcher = "org.kde.StatusNotifierWatcher"
	const property = "IsStatusNotifierHostRegistered"
	properties, err := prop.Export(conn, "/StatusNotifierWatcher", prop.Map{
		watcher: {property: {Value: false}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := conn.RequestName(watcher, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request watcher: %v, %v", reply, err)
	}
	ctx := context.Background()
	for _, available := range []bool{false, true, false, true} {
		properties.SetMust(watcher, property, available)
		if got := statusNotifierHostAvailable(ctx); got != available {
			t.Fatalf("host registered=%v, probe=%v", available, got)
		}
	}
	if _, err := conn.ReleaseName(watcher); err != nil {
		t.Fatal(err)
	}
	if statusNotifierHostAvailable(ctx) {
		t.Fatal("host remained available after the watcher exited")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if statusNotifierHostAvailable(canceled) {
		t.Fatal("canceled probe reported an available host")
	}
}
