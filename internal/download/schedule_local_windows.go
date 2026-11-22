package download

import (
	"fmt"
	"golang.org/x/sys/windows"
	"time"
	"unsafe"
)

var (
	scheduleKernel     = windows.NewLazySystemDLL("kernel32.dll")
	scheduleLocalToUTC = scheduleKernel.NewProc("TzSpecificLocalTimeToSystemTime")
	scheduleUTCToLocal = scheduleKernel.NewProc("SystemTimeToTzSpecificLocalTime")
)

// Go caches time.Local at startup. Query Windows so changing the system zone
// while the app runs also changes daily eligibility and newly edited times.
func scheduleNow() time.Time {
	now := time.Now()
	var zone windows.Timezoneinformation
	id, err := windows.GetTimeZoneInformation(&zone)
	if err != nil {
		return now
	}
	bias := zone.Bias
	if id == windows.TIME_ZONE_ID_DAYLIGHT {
		bias += zone.DaylightBias
	} else if id == windows.TIME_ZONE_ID_STANDARD {
		bias += zone.StandardBias
	}
	return now.In(time.FixedZone("system", -int(bias)*60))
}
func refreshScheduleZone() {}
func windowsStamp(t time.Time) windows.Systemtime {
	return windows.Systemtime{Year: uint16(t.Year()), Month: uint16(t.Month()), Day: uint16(t.Day()), Hour: uint16(t.Hour()), Minute: uint16(t.Minute()), Second: uint16(t.Second())}
}
func stampTime(s windows.Systemtime) time.Time {
	return time.Date(int(s.Year), time.Month(s.Month), int(s.Day), int(s.Hour), int(s.Minute), int(s.Second), 0, time.UTC)
}

// ParseScheduleStart interprets a wall time using the current system zone's
// transition rules. Roundtripping rejects nonexistent DST wall times.
func ParseScheduleStart(raw string) (time.Time, error) {
	t, err := time.Parse("2006-01-02 15:04", raw)
	if err != nil {
		return time.Time{}, err
	}
	in := windowsStamp(t)
	var out windows.Systemtime
	ok, _, err := scheduleLocalToUTC.Call(0, uintptr(unsafe.Pointer(&in)), uintptr(unsafe.Pointer(&out)))
	if ok == 0 {
		return time.Time{}, fmt.Errorf("转换本地开始时间：%w", err)
	}
	result := stampTime(out)
	if FormatScheduleStart(result) != raw {
		return time.Time{}, fmt.Errorf("本地开始时间不存在")
	}
	return result, nil
}
func FormatScheduleStart(t time.Time) string {
	in := windowsStamp(t.UTC())
	var out windows.Systemtime
	ok, _, _ := scheduleUTCToLocal.Call(0, uintptr(unsafe.Pointer(&in)), uintptr(unsafe.Pointer(&out)))
	if ok == 0 {
		return t.Local().Format("2006-01-02 15:04")
	}
	return stampTime(out).Format("2006-01-02 15:04")
}
