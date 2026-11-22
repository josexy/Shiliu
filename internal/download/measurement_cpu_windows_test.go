//go:build windows

package download

import "golang.org/x/sys/windows"

func measurementCPU() float64 {
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user) != nil {
		return -1
	}
	ticks := func(t windows.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }
	return float64(ticks(kernel)+ticks(user)) / 1e7
}
