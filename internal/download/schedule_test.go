package download

import (
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata"
)

func TestScheduleRestoreIntent(t *testing.T) {
	m := New(Options{Dir: t.TempDir()})
	defer m.Shutdown()
	start := time.Now().Add(time.Hour).UTC()
	plan := Schedule{Enabled: true, StartAt: start, StartReached: true, DailyEnabled: true, DailyStartMinute: 22 * 60, DailyEndMinute: 6 * 60}
	m.Restore([]Record{
		{ID: "legacy", URL: "https://example.com/old", Path: filepath.Join(m.dir, "old"), State: StateQueued},
		{ID: "plan", URL: "https://example.com/new", Path: filepath.Join(m.dir, "new"), Schedule: plan, ManualPaused: true},
		{ID: "failed", URL: "https://example.com/failed", Path: filepath.Join(m.dir, "failed"), Schedule: plan, State: StateFailed},
	})
	got := m.Records()
	if !got[0].ManualPaused || got[0].State != StatePaused || got[0].Schedule.Enabled {
		t.Fatal(got[0])
	}
	if got[1].Schedule != plan || !got[1].ManualPaused {
		t.Fatal(got[1])
	}
	if got[2].State != StateFailed {
		t.Fatal("scheduled failure was revived")
	}
}

func TestScheduleSystemLocalTimeRoundTrip(t *testing.T) {
	for _, raw := range []string{"2027-01-02 22:00", "2027-07-02 06:30"} {
		stamp, err := ParseScheduleStart(raw)
		if err != nil || FormatScheduleStart(stamp) != raw {
			t.Fatalf("local roundtrip %s: %v", raw, err)
		}
	}
	if _, err := ParseScheduleStart("invalid"); err == nil {
		t.Fatal("accepted invalid local time")
	}
	if delta := scheduleNow().Sub(time.Now()); delta < -time.Second || delta > time.Second {
		t.Fatal("clock changed absolute instant", delta)
	}
}

func TestScheduleEligibilityBoundariesAndZones(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		stamp      string
		start, end int
		allowed    bool
	}{
		{"2026-11-01T05:30:00Z", 60, 120, true},    // First 01:30, before DST fallback.
		{"2026-11-01T06:30:00Z", 60, 120, true},    // Repeated 01:30 is still eligible.
		{"2026-03-08T07:00:00Z", 120, 180, false},  // 02:00–03:00 was skipped.
		{"2026-11-22T03:00:00Z", 1320, 360, true},  // 22:00 included.
		{"2026-11-22T11:00:00Z", 1320, 360, false}, // 06:00 excluded.
	} {
		now, _ := time.Parse(time.RFC3339, tc.stamp)
		s := Schedule{Enabled: true, DailyEnabled: true, DailyStartMinute: tc.start, DailyEndMinute: tc.end}
		_, wait := ScheduleEligibility(s, now.In(zone))
		if (wait == "") != tc.allowed {
			t.Fatalf("%s: %q", tc.stamp, wait)
		}
	}
	now := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	s := Schedule{Enabled: true, StartAt: now.Add(time.Hour)}
	if reached, _ := ScheduleEligibility(s, now); reached {
		t.Fatal("early threshold")
	}
	s.StartReached, _ = ScheduleEligibility(s, now.Add(time.Hour))
	if reached, wait := ScheduleEligibility(s, now.Add(-time.Hour)); !reached || wait != "" {
		t.Fatal("rollback revoked threshold")
	}
	s = Schedule{DailyEnabled: true, DailyStartMinute: 8 * 60, DailyEndMinute: 9 * 60}
	_, a := ScheduleEligibility(s, now)
	_, b := ScheduleEligibility(s, now.In(time.FixedZone("east", 3600)))
	if a != "" || b == "" {
		t.Fatal("daily window ignored current local zone")
	}
	s.DailyEndMinute = s.DailyStartMinute
	if s.Validate() == nil {
		t.Fatal("equal boundaries accepted")
	}
}
