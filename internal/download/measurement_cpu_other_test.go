//go:build !windows

package download

// Non-Windows measurements may use an external process CPU tool.
func measurementCPU() float64 { return -1 }
