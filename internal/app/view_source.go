package app

import (
	"github.com/egoist/mygo/ui"
	"strings"
)

func (a *app) sourceModal(c *ui.Context) {
	if a.source.wasOpen && !a.source.open {
		a.closeSource()
	}
	if !a.source.open {
		return
	}
	a.source.wasOpen = true
	p := a.palette
	width, height := c.Size()
	c.Key("source-dialog")
	ui.Modal(c, &a.source.open, func() {
		ui.Column(c).Width(min(620, width-100)).Gap(12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "更新下载链接").FontSize(20).Bold().Grow(1)
				iconButton(c, p, "关闭链接更新", icoX, false, a.closeSource)
			})
			if a.source.saved {
				ui.Text(c, "链接已更新，任务保持暂停。").FontSize(15).Bold()
				ui.Text(c, a.source.preview.Reason).FontSize(13).TextColor(p.Muted)
			} else {
				ui.Scroll(c).Height(min(300, max(180, height-310))).Gap(12).Children(func() {
					ui.Text(c, "保留原任务和保存位置。旧认证不会沿用，请明确填写新链接所需的请求头。").FontSize(13).TextColor(p.Muted)
					ui.Field(c, "新下载链接", func() {
						c.Key("source-url")
						ui.TextInput(c, &a.source.url).Label("新下载链接").AutoFocus().Disabled(a.source.busy).OnChange(a.invalidateSourcePreview)
					})
					ui.Field(c, "新请求头（可选，仅本次运行使用）", func() {
						c.Key("source-headers")
						ui.TextArea(c, &a.source.headers).Label("新请求头").Height(72).Placeholder("每行一个，例如 Authorization: Bearer …").Disabled(a.source.busy).OnChange(a.invalidateSourcePreview)
					})
					ui.Text(c, "安全复用需要重新下载并比对完整已有前缀，会产生额外流量；验证期间可随时取消。").FontSize(12).TextColor(p.Muted)
				})
			}
			if a.source.busy {
				ui.Text(c, "正在处理，可取消等待…").FontSize(13).TextColor(p.Muted)
			}
			if a.source.preview.URL != "" && !a.source.saved {
				ui.Text(c, a.source.preview.Reason).FontSize(13).TextColor(p.Accent)
				if !a.source.preview.Reuse {
					ui.Text(c, "保存链接后仍暂停；点击继续下载才会重置旧部分数据。").FontSize(12).TextColor(p.Muted)
				}
			}
			if a.source.err != "" {
				ui.Text(c, a.source.err).FontSize(12).TextColor(p.Danger)
			}
			ui.Divider(c).Background(p.Divider)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "关闭更新").OnClick(a.closeSource)
				if a.source.saved {
					ui.PrimaryButton(c, "继续下载").OnClick(a.continueSource)
				} else {
					ui.Button(c, "验证新链接").Disabled(a.source.busy || strings.TrimSpace(a.source.url) == "").OnClick(a.probeSource)
					ui.PrimaryButton(c, "保存新链接").Disabled(a.source.busy || a.source.preview.URL == "").OnClick(a.commitSource)
				}
			})
		})
	}).Background(p.Card).Radius(14).Border(1, p.Border)
}
