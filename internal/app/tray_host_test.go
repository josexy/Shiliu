package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type fakeTrayHostObject struct {
	t    *testing.T
	call *dbus.Call
}

func (o fakeTrayHostObject) CallWithContext(ctx context.Context, method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	o.t.Helper()
	if ctx == nil || method != "org.freedesktop.DBus.Properties.Get" || flags != dbus.FlagNoAutoStart ||
		!reflect.DeepEqual(args, []interface{}{"org.kde.StatusNotifierWatcher", "IsStatusNotifierHostRegistered"}) {
		o.t.Fatalf("unexpected host query: %s %v %v", method, flags, args)
	}
	return o.call
}

func TestStatusNotifierHostRegistered(t *testing.T) {
	for _, test := range []struct {
		name string
		call *dbus.Call
		want bool
	}{
		{"registered", &dbus.Call{Body: []interface{}{dbus.MakeVariant(true)}}, true},
		{"no host", &dbus.Call{Body: []interface{}{dbus.MakeVariant(false)}}, false},
		{"wrong type", &dbus.Call{Body: []interface{}{dbus.MakeVariant("true")}}, false},
		{"no reply", &dbus.Call{}, false},
		{"no watcher", &dbus.Call{Err: errors.New("no owner")}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := statusNotifierHostRegistered(context.Background(), fakeTrayHostObject{t, test.call}); got != test.want {
				t.Fatalf("registered = %v, want %v", got, test.want)
			}
		})
	}
}

func TestWatchTrayHostTransitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	states := []bool{false, false, true, true, false}
	var got []bool
	index := 0
	watchTrayHost(ctx, func(context.Context) bool {
		state := states[min(index, len(states)-1)]
		index++
		return state
	}, time.Millisecond, func(available bool) {
		got = append(got, available)
		if len(got) == 3 {
			cancel()
		}
	})
	if !reflect.DeepEqual(got, []bool{false, true, false}) || index != len(states) {
		t.Fatalf("host transitions = %v, probes = %d", got, index)
	}
}

func TestWatchTrayHostDropsCanceledProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchTrayHost(ctx, func(context.Context) bool {
		cancel()
		return true
	}, time.Hour, func(bool) { t.Fatal("published a result after cancellation") })
}
