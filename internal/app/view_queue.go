package app

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
)

func queuePositionLabel(it download.Item) string {
	if it.QueuePosition == 0 {
		return "未在等待队列中"
	}
	return fmt.Sprintf("队列位置 %d / %d", it.QueuePosition, it.QueueLength)
}

type queueAction struct {
	label    string
	move     download.QueueMove
	disabled bool
}

func queueActions(it download.Item) []queueAction {
	return []queueAction{
		{"队列置顶", download.QueueFront, it.QueuePosition <= 1},
		{"队列上移", download.QueueUp, it.QueuePosition <= 1},
		{"队列下移", download.QueueDown, it.QueuePosition == 0 || it.QueuePosition >= it.QueueLength},
	}
}

func (a *app) moveQueue(id string, move download.QueueMove) {
	if err := a.engine.MoveQueue(id, move); err != nil {
		a.toast(err.Error())
		return
	}
	a.dirty.Store(true)
}

func (a *app) queueControls(c *ui.Context, it download.Item) {
	ui.Text(c, queuePositionLabel(it)).FontSize(13).TextColor(a.palette.Muted)
	ui.Row(c).Gap(8).Children(func() {
		for _, action := range queueActions(it) {
			c.Key("queue-" + it.ID + string(action.move))
			ui.Button(c, action.label).Disabled(action.disabled).OnClick(func() { a.moveQueue(it.ID, action.move) })
		}
	})
}
