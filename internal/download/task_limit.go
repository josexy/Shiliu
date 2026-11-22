package download

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MaxTaskRateLimit is 1 GiB/s. Stored values and the engine use bytes/second;
// the UI uses KB/s with 1024 bytes per KB. Zero removes the task ceiling.
const MaxTaskRateLimit int64 = 1024 * 1024 * 1024

func validateTaskRateLimit(rate int64) error {
	if rate < 0 || rate > MaxTaskRateLimit {
		return fmt.Errorf("任务限速必须在 0–1048576 KB/s 之间")
	}
	return nil
}

func ParseTaskRateKiB(raw string) (int64, error) {
	n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > float64(MaxTaskRateLimit)/1024 {
		return 0, fmt.Errorf("任务限速必须为 0–1048576 KB/s 的数字")
	}
	if n == 0 {
		return 0, nil
	}
	return max(1, int64(math.Round(n*1024))), nil
}

// SetTaskRateLimit updates subsequent read budgets without reconnecting or
// resetting progress. Invalid input leaves the effective limit unchanged.
func (m *Manager) SetTaskRateLimit(id string, rate int64) error {
	if err := validateTaskRateLimit(rate); err != nil {
		return err
	}
	m.mu.Lock()
	t := m.byID[id]
	if m.closed || t == nil || t.removed {
		m.mu.Unlock()
		return fmt.Errorf("任务已移除或下载器正在退出")
	}
	t.item.TaskRateLimit = rate
	t.bandwidth.set(rate)
	m.mu.Unlock()
	m.notify()
	return nil
}
