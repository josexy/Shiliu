package app

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
)

func (a *app) saveExpectedSHA() {
	expected, err := download.NormalizeSHA256(a.detailsSHA)
	if err == nil {
		err = a.engine.SetExpectedSHA256(a.detailsID, expected)
	}
	if err != nil {
		a.detailsSHAErr = err.Error()
		return
	}
	a.detailsSHA = expected
	a.detailsSHAErr = ""
	a.dirty.Store(true)
}

func (a *app) startVerification() {
	if err := a.engine.Verify(a.detailsID); err != nil {
		a.detailsSHAErr = err.Error()
	} else {
		a.detailsSHAErr = ""
	}
}

func verificationLabel(state download.VerificationState) string {
	switch state {
	case download.VerificationPending:
		return "待校验"
	case download.VerificationChecking:
		return "校验中"
	case download.VerificationMatched:
		return "匹配"
	case download.VerificationMismatched:
		return "不匹配（文件已保留）"
	case download.VerificationCanceled:
		return "已取消"
	case download.VerificationFailed:
		return "校验失败"
	default:
		return "未配置（未执行完整性校验）"
	}
}

func (a *app) verificationControls(c *ui.Context, it download.Item) {
	p := a.palette
	ui.Field(c, "预期 SHA-256（可选）", func() {
		c.Key("details-sha256-" + it.ID)
		ui.TextInput(c, &a.detailsSHA).Label("预期 SHA-256").FontSize(12).Placeholder("64 位十六进制摘要；清空可关闭校验").OnSubmit(a.saveExpectedSHA)
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		color := p.Muted
		if it.VerificationState == download.VerificationMatched {
			color = p.Success
		}
		if it.VerificationState == download.VerificationFailed || it.VerificationState == download.VerificationMismatched {
			color = p.Danger
		}
		if it.VerificationState == download.VerificationChecking {
			color = p.Accent
		}
		ui.Text(c, "完整性："+verificationLabel(it.VerificationState)).FontSize(13).Grow(1).TextColor(color)
		ui.Button(c, "保存摘要").OnClick(a.saveExpectedSHA)
		if it.VerificationState == download.VerificationChecking || it.VerificationQueued {
			ui.Button(c, "取消校验").OnClick(func() { a.engine.CancelVerification(it.ID) })
		} else {
			ui.Button(c, "重新校验").Disabled(it.ExpectedSHA256 == "" || it.State != download.StateCompleted || it.Missing).OnClick(a.startVerification)
		}
	})
	if it.VerificationState == download.VerificationChecking {
		ui.Text(c, fmt.Sprintf("已校验 %s / %s", formatBytes(it.VerificationDone), formatBytes(it.VerificationTotal))).FontSize(12).TextColor(p.Muted)
		ratio := float32(0)
		if it.VerificationTotal > 0 {
			ratio = min(1, float32(it.VerificationDone)/float32(it.VerificationTotal))
		}
		ui.Box(c).Height(5).FillWidth().Background(p.Track).Radius(3).Label("SHA-256 校验进度").Children(func() { ui.Box(c).Height(5).WidthPercent(100 * ratio).Background(p.Accent) })
	}
	if a.detailsSHAErr != "" {
		ui.Text(c, a.detailsSHAErr).FontSize(12).TextColor(p.Danger)
	}
	if it.VerificationError != "" {
		ui.Text(c, download.DiagnosticText(it.VerificationError)).FontSize(12).TextColor(p.Danger)
	}
	if last := it.LastVerification; !last.CheckedAt.IsZero() {
		ui.Text(c, "上次结果："+verificationLabel(last.State)+" · "+last.CheckedAt.Local().Format("2006-01-02 15:04:05")).FontSize(12).TextColor(p.Muted)
		if last.ActualSHA256 != "" {
			ui.Text(c, "上次实际摘要："+last.ActualSHA256).FontSize(12).Selectable().TextColor(p.Muted)
		}
	}
	ui.Text(c, "校验只反映检查时的文件内容；重启后需重新执行。摘要不匹配不会自动删除或重新下载文件。").FontSize(12).TextColor(p.Muted)
}
