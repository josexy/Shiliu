package app

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
)

func (a *app) clipboardPrompt(c *ui.Context) {
	if a.tray.hidden || len(a.clipboard.pending) == 0 || !a.s.ClipboardPrompts {
		return
	}
	p := a.palette
	c.Key("clipboard-prompt")
	ui.Row(c).Padding(10, 12).Gap(10).AlignItems(ui.Center).Radius(8).Background(p.AccentTint).Border(1, p.AccentBorder).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, fmt.Sprintf("剪贴板发现 %d 个新链接", len(a.clipboard.pending))).FontSize(13).Bold()
			preview := download.DiagnosticURL(a.clipboard.pending[0])
			ui.Text(c, preview).FontSize(12).SingleLine().TextColor(p.Muted).Tooltip(preview)
		})
		ui.Button(c, "添加剪贴板链接").OnClick(a.addClipboardLinks)
		ui.Button(c, "忽略").OnClick(a.ignoreClipboard)
	})
}
