package app

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
)

func connectionStatus(it download.Item) string {
	mode := "单连接"
	if it.TransferMode == "segments" {
		mode = "多连接分片"
	}
	return fmt.Sprintf("%s · 活动 %d · 等待 %d · 任务上限 %d", mode, it.ActiveConnections, it.WaitingConnections, max(1, it.ConnectionLimit))
}

func (a *app) applyConnections() {
	global, err := download.ParseConnectionCount(a.globalConnections, 64)
	if err != nil {
		a.connectionsErr = err.Error()
		return
	}
	perTask, err := download.ParseConnectionCount(a.defaultConnections, 8)
	if err != nil {
		a.connectionsErr = err.Error()
		return
	}
	if err = a.engine.SetMaxConnections(global); err == nil {
		err = a.engine.SetDefaultConnections(perTask)
	}
	if err != nil {
		a.connectionsErr = err.Error()
		return
	}
	a.s.MaxConnections, a.s.TaskConnections = global, perTask
	a.connectionsErr = ""
	a.saveSettings()
}

func (a *app) connectionSettings(c *ui.Context) {
	p := a.palette
	ui.Column(c).Padding(12).Gap(10).Background(p.Canvas).Radius(8).Children(func() {
		ui.Text(c, "连接数量").FontSize(14)
		ui.Text(c, "全局 1–64，默认任务 1–8。多连接不保证提速。").FontSize(12).TextColor(p.Muted)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, "全局").Width(34)
				c.Key("global-connections")
				ui.TextInput(c, &a.globalConnections).Width(70).Label("全局连接上限").OnSubmit(a.applyConnections)
				ui.Text(c, "默认").Width(34)
				c.Key("default-connections")
				ui.TextInput(c, &a.defaultConnections).Width(70).Label("默认任务连接数").OnSubmit(a.applyConnections)
			})
			ui.Spacer(c)
			ui.Button(c, "应用连接设置").OnClick(a.applyConnections)
		})
		ui.Text(c, "全局上限立即生效；默认连接数只影响新任务。").FontSize(12).TextColor(p.Muted)
	})
	if a.connectionsErr != "" {
		ui.Text(c, a.connectionsErr).FontSize(12).TextColor(p.Danger)
	}
}

func (a *app) applyTaskConnections() {
	n, err := download.ParseConnectionCount(a.detailsConnections, 8)
	if err == nil {
		err = a.engine.SetTaskConnections(a.detailsID, n)
	}
	if err != nil {
		a.detailsConnectionsErr = err.Error()
		return
	}
	a.detailsConnectionsErr = ""
	a.dirty.Store(true)
	a.toast("连接数将在此任务下次开始时生效")
}

func (a *app) taskConnectionControls(c *ui.Context, it download.Item) {
	p := a.palette
	ui.Field(c, "任务连接数（1–8，下次开始生效）", func() {
		ui.Row(c).Gap(8).Children(func() {
			c.Key("task-connections-" + it.ID)
			ui.TextInput(c, &a.detailsConnections).Label("详情任务连接数").Grow(1).OnSubmit(a.applyTaskConnections)
			ui.Button(c, "应用连接数").OnClick(a.applyTaskConnections)
		})
	})
	if it.FallbackReason != "" {
		ui.Text(c, it.FallbackReason).FontSize(12).TextColor(p.Muted)
	}
	if a.detailsConnectionsErr != "" {
		ui.Text(c, a.detailsConnectionsErr).FontSize(12).TextColor(p.Danger)
	}
}
