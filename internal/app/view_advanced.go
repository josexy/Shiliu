package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *app) advancedModal(c *ui.Context) {
	if a.advancedWasOpen && !a.advanced.open {
		a.clearAdvanced()
	}
	if !a.advanced.open {
		return
	}
	a.advancedWasOpen = true
	p := a.palette
	width, height := c.Size()
	c.Key("advanced-dialog")
	ui.Modal(c, &a.advanced.open, func() {
		ui.Column(c).Width(min(620, width-100)).Gap(14).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icoPlus).FontSize(18).TextColor(p.Accent)
				ui.Text(c, "高级新建下载").FontSize(20).Bold().Grow(1)
				iconButton(c, p, "关闭高级新建", icoX, false, a.clearAdvanced)
			})
			ui.Text(c, "为单个任务指定文件和请求信息。").FontSize(13).TextColor(p.Muted)
			ui.Scroll(c).Height(min(435, max(180, height-250))).Gap(12).Children(func() {
				ui.Field(c, "下载链接", func() {
					c.Key("advanced-url")
					ui.TextInput(c, &a.advanced.url).Label("高级下载链接").Placeholder("https://example.com/file.zip").AutoFocus()
				})
				ui.Field(c, "保存位置", func() {
					ui.Row(c).Gap(8).Children(func() {
						c.Key("advanced-dir")
						ui.TextInput(c, &a.advanced.dir).Label("任务保存位置").Grow(1).OnChange(func() { a.advanced.dirOverride = true })
						ui.Button(c, "选择…").OnClick(a.chooseAdvancedFolder)
					})
				})
				ui.Field(c, "文件名（可选）", func() {
					c.Key("advanced-name")
					ui.TextInput(c, &a.advanced.fileName).Label("任务文件名").Placeholder("留空时自动识别文件名")
				})
				c.Key("advanced-dir-override")
				ui.Checkbox(c, &a.advanced.dirOverride, "自定义目录（覆盖分类规则）")
				ui.Text(c, a.advancedDirectoryPreview()).FontSize(12).TextColor(p.Muted).SingleLine().Tooltip(a.advancedDirectoryPreview())
				ui.Field(c, "任务限速（KB/s，0 表示不另设上限）", func() {
					c.Key("advanced-task-rate")
					ui.TextInput(c, &a.advanced.rateKiB).Label("高级任务限速 KB/s").Placeholder("0 表示不另设任务上限")
				})
				ui.Field(c, "预期 SHA-256（可选）", func() {
					c.Key("advanced-sha256")
					ui.TextInput(c, &a.advanced.expectedSHA).Label("高级预期 SHA-256").FontSize(12).Placeholder("留空时不校验；否则填写 64 位十六进制摘要")
				})
				ui.Field(c, "任务连接数（1–8）", func() {
					c.Key("advanced-connections")
					ui.TextInput(c, &a.advanced.connections).Label("高级任务连接数")
				})
				ui.Field(c, "Referer（可选）", func() {
					c.Key("advanced-referer")
					ui.TextInput(c, &a.advanced.referer).Label("请求来源 Referer").Placeholder("https://example.com/")
				})
				ui.Field(c, "Cookie（可选）", func() {
					c.Key("advanced-cookie")
					ui.TextInput(c, &a.advanced.cookie).Password().Label("请求 Cookie").Placeholder("仅在本次运行中使用")
				})
				ui.Field(c, "其他请求头（可选）", func() {
					c.Key("advanced-headers")
					ui.TextArea(c, &a.advanced.headers).Height(78).Label("自定义请求头").Placeholder("每行一个，例如 User-Agent: Downloader")
				})
				ui.Text(c, "请求头仅在本次运行中保留；重启后可用同一链接补填，已有进度会保留。更新已有任务时沿用原保存路径。").FontSize(12).TextColor(p.Muted)
				a.scheduleFields(c, "advanced", &a.advanced.schedule)
			})
			if a.advanced.err != "" {
				ui.Text(c, a.advanced.err).FontSize(12).TextColor(p.Danger)
			}
			ui.Divider(c).Background(p.Divider)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "启用计划时按计划开始").FontSize(12).TextColor(p.Muted).Grow(1)
				ui.Button(c, "取消").OnClick(a.clearAdvanced)
				label := "创建下载"
				if a.advanced.busy {
					label = "正在创建…"
				}
				ui.PrimaryButton(c, label).Disabled(a.advanced.busy || strings.TrimSpace(a.advanced.url) == "").OnClick(a.submitAdvanced)
			})
		})
	}).Background(p.Card).Radius(14).Border(1, p.Border)
}
