package app

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"go-downloader/internal/download"
)

func historySpeedLabel(value float64) string {
	if value <= 0 {
		return "0 B/s"
	}
	return formatSpeed(value)
}

func historySummary(samples []download.SpeedSample, it download.Item) (string, float64) {
	peak := 0.0
	valid, gaps := 0, 0
	for _, p := range samples {
		if p.Gap {
			gaps++
			continue
		}
		if p.BytesPerSecond >= 0 && !math.IsNaN(p.BytesPerSecond) && !math.IsInf(p.BytesPerSecond, 0) {
			valid++
			peak = max(peak, p.BytesPerSecond)
		}
	}
	if valid == 0 {
		return "等待采样：首次采样或时间缺口暂不估算速度", peak
	}
	current := "等待有效采样"
	last := samples[len(samples)-1]
	if !last.Gap {
		current = historySpeedLabel(last.BytesPerSecond)
	}
	if it.State != download.StateConnecting && it.State != download.StateDownloading {
		current = "0 B/s"
	}
	return fmt.Sprintf("当前 %s · 峰值 %s · 有效 %d 点 · 缺口 %d · 最近 %s", current, historySpeedLabel(peak), valid, gaps, last.At.Format("15:04:05")), peak
}

// Build immutable plot segments before painting. Missing samples and long
// intervals split lines, so a sleep is never rendered as an interpolated slope.
func historyLines(samples []download.SpeedSample, now time.Time, peak float64) [][][2]float32 {
	var lines [][][2]float32
	var line [][2]float32
	var previous time.Time
	flush := func() {
		if len(line) > 0 {
			lines = append(lines, line)
			line = nil
		}
	}
	for _, p := range samples {
		if p.Gap || p.BytesPerSecond < 0 || math.IsNaN(p.BytesPerSecond) || math.IsInf(p.BytesPerSecond, 0) || p.At.After(now) || p.At.Before(now.Add(-download.HistoryWindow)) {
			flush()
			previous = time.Time{}
			continue
		}
		if !previous.IsZero() && (p.At.Sub(previous) > 2*time.Second || !p.At.After(previous)) {
			flush()
		}
		x := 1 - float32(now.Sub(p.At).Seconds()/download.HistoryWindow.Seconds())
		y := float32(p.BytesPerSecond / max(1, peak))
		line = append(line, [2]float32{max(0, min(1, x)), max(0, min(1, y))})
		previous = p.At
	}
	flush()
	return lines
}

func (a *app) speedHistorySection(c *ui.Context, it download.Item) {
	samples := a.engine.History(it.ID)
	summary, peak := historySummary(samples, it)
	p := a.palette
	ui.Text(c, "最近 120 秒速度").FontSize(14).Bold()
	ui.Text(c, summary).FontSize(12).TextColor(p.Muted)
	unit, divisor := "B/s", 1.0
	if peak >= 1024*1024*1024 {
		unit, divisor = "GB/s", 1024*1024*1024
	} else if peak >= 1024*1024 {
		unit, divisor = "MB/s", 1024*1024
	} else if peak >= 1024 {
		unit, divisor = "KB/s", 1024
	}
	ui.Text(c, "速度（"+unit+"）").FontSize(11).TextColor(p.Muted)
	y := chart.NewLinear(0, max(1, peak/divisor)).Nice(3)
	ticks := y.Ticks(3)
	lines := historyLines(samples, a.now, y.Hi*divisor)
	axes := chart.Axes{Left: ticks, Bottom: []chart.Tick{{At: 0, Label: "-120 秒"}, {At: 0.5, Label: "-60 秒"}, {At: 1, Label: "现在"}}, Label: func(_ chart.Side, tick chart.Tick) { ui.Text(c, tick.Label).FontSize(11).NoWrap().TextColor(p.Muted) }}
	c.Key("history-chart-" + it.ID)
	chart.Plot(c, axes, func(area ui.Element) {
		area.Background(p.Canvas).Clip().Draw(func(painter *ui.Painter, r ui.Rect) {
			for _, tick := range ticks {
				at := chart.Y(r, tick.At)
				painter.Line(r.X, at, r.X+r.W, at, 1, p.CurveGrid)
			}
			for _, segment := range lines {
				points := make([][2]float32, len(segment))
				for i, point := range segment {
					points[i] = [2]float32{chart.X(r, point[0]), chart.Y(r, point[1])}
				}
				if len(points) == 1 {
					painter.Fill(ui.Rect{X: points[0][0] - 2, Y: points[0][1] - 2, W: 4, H: 4}, p.Curve, 2)
				} else {
					painter.StrokePolyline(points, 2, p.Curve)
				}
			}
		})
	}).Height(145).Gap(8).Padding(0, 24, 0, 0).Label("最近 120 秒下载速度曲线").Description(summary)
	ui.Text(c, "按实际网络接收量采样，包含重试流量；断开处表示缺口。仅保留本次运行。").FontSize(11).TextColor(p.Muted)
}
