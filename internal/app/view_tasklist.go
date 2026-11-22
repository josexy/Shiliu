package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (a *app) taskListModal(c *ui.Context) {
	if a.taskList.wasOpen && !a.taskList.open {
		a.closeTaskList()
	}
	if !a.taskList.open {
		return
	}
	a.taskList.wasOpen = true
	p := a.palette
	width, height := c.Size()
	title := "导入 TXT 链接"
	if a.taskList.export {
		title = "导出任务清单"
	}
	c.Key("task-list-dialog")
	ui.Modal(c, &a.taskList.open, func() {
		ui.Column(c).Width(min(660, width-100)).Gap(12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(20).Bold().Grow(1)
				iconButton(c, p, "关闭任务清单", icoX, false, a.closeTaskList)
			})
			if a.taskList.export {
				ui.Text(c, "仅分享名称、脱敏链接、大小、进度和状态。清单不是备份，不支持 JSON 导入。").FontSize(13).TextColor(p.Muted)
				ui.Text(c, "链接中的认证、查询参数和片段会被移除，导出的地址可能无法直接下载。").FontSize(13).TextColor(p.Muted)
			} else if !a.taskList.finished {
				ui.Text(c, "UTF-8 TXT（可含 BOM）· 最多 10 MiB / 10000 条 / 单条 16 KiB").FontSize(12).TextColor(p.Muted)
				ui.Button(c, "选择 TXT…").Disabled(a.taskList.busy).AutoFocus().OnClick(a.chooseTaskList)
				preview := a.taskList.preview
				ui.Text(c, fmt.Sprintf("有效 %d · 重复 %d · 无效 %d", preview.Valid, preview.Duplicate, preview.Invalid)).FontSize(13)
				a.taskList.list.Key = func(i int) any { return preview.Entries[i].Index }
				a.taskList.list.Label = func(i int) string { return fmt.Sprintf("候选 %d", preview.Entries[i].Index) }
				c.Key("task-list-preview")
				ui.List(c, &a.taskList.list, len(preview.Entries)).Height(min(260, max(100, height-350))).Background(p.Canvas).Radius(8).Clip().Rows(func(row ui.ListRow) {
					entry := preview.Entries[row.Index]
					ui.Row(row.Context).Height(34).Padding(6, 10).Gap(12).Children(func() {
						ui.Text(row.Context, fmt.Sprintf("%d · %s", entry.Index, entry.Kind)).Width(90).FontSize(12).TextColor(p.Muted)
						ui.Text(row.Context, entry.URL).SingleLine().Grow(1).FontSize(12).Tooltip(entry.URL)
					})
				})
				ui.Text(c, "确认时重新检查重复链接，并遵循「添加后自动开始」设置。取消会停止后续添加，已经创建的任务保留。").FontSize(12).TextColor(p.Muted)
			}
			if a.taskList.message != "" {
				ui.Scroll(c).MaxHeight(180).Children(func() { ui.Text(c, a.taskList.message).FontSize(13) })
			}
			if a.taskList.err != "" {
				ui.Text(c, a.taskList.err).FontSize(12).TextColor(p.Danger)
			}
			if a.taskList.busy {
				ui.Text(c, "正在处理…").FontSize(13).TextColor(p.Muted)
			}
			ui.Divider(c).Background(p.Divider)
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				ui.Button(c, "关闭清单").OnClick(a.closeTaskList)
				if a.taskList.finished {
					return
				}
				if a.taskList.export {
					ui.Button(c, "导出所选").Disabled(a.taskList.busy || a.selection.Len() == 0).OnClick(func() { a.exportTaskList(true) })
					ui.PrimaryButton(c, "导出全部").Disabled(a.taskList.busy).OnClick(func() { a.exportTaskList(false) })
				} else {
					ui.PrimaryButton(c, "确认导入").Disabled(a.taskList.busy || a.taskList.preview.Valid+a.taskList.preview.Duplicate == 0).OnClick(a.confirmImport)
				}
			})
		})
	}).Background(p.Card).Radius(14).Border(1, p.Border)
}
