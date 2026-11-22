//go:build !windows

package download

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

var scheduleZone atomic.Pointer[time.Location]

func currentScheduleZone() *time.Location {
	if name, ok := os.LookupEnv("TZ"); ok {
		if name == "" {
			return time.UTC
		}
		name = strings.TrimPrefix(name, ":")
		if !strings.HasPrefix(name, "/") {
			if loc, err := time.LoadLocation(name); err == nil {
				return loc
			}
		}
		if data, err := os.ReadFile(name); err == nil {
			if loc, err := time.LoadLocationFromTZData("Local", data); err == nil {
				return loc
			}
		}
	}
	if data, err := os.ReadFile("/etc/localtime"); err == nil {
		if loc, err := time.LoadLocationFromTZData("Local", data); err == nil {
			return loc
		}
	}
	return time.Local
}

// Read the small zone database outside the manager lock, in timer setup/reset.
func refreshScheduleZone() { scheduleZone.Store(currentScheduleZone()) }
func scheduleNow() time.Time {
	loc := scheduleZone.Load()
	if loc == nil {
		loc = time.Local
	}
	return time.Now().In(loc)
}
func ParseScheduleStart(raw string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04", raw, currentScheduleZone())
	if err != nil {
		return time.Time{}, err
	}
	if t.Format("2006-01-02 15:04") != raw {
		return time.Time{}, fmt.Errorf("本地开始时间不存在")
	}
	return t.UTC(), nil
}
func FormatScheduleStart(t time.Time) string {
	return t.In(currentScheduleZone()).Format("2006-01-02 15:04")
}
