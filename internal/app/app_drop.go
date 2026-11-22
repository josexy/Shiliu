package app

import (
	"fmt"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/tasklist"
	"net/url"
	"strings"
)

var dropOptions = transfer.DropOptions{Formats: []transfer.Format{transfer.URIList, transfer.Text, transfer.FileList}, Operations: transfer.Copy}

type dropInput struct {
	texts [][]byte
	paths []string
}

// MyGo freezes accepted representations when the native session ends. Copy
// only the preferred representation of each item into ordinary owned bytes and
// paths here; no Element, parts, Context, or native provider reaches a worker.
func captureDrop(data transfer.Data) (dropInput, error) {
	var input dropInput
	total := 0
	items := data.Items()
	if len(items) > tasklist.MaxCandidates {
		return input, fmt.Errorf("投放项目过多")
	}
	for _, item := range items {
		format, ok := transfer.PreferredFormat(item.Formats(), transfer.URIList, transfer.Text, transfer.FileList)
		if !ok {
			return input, fmt.Errorf("未提供链接或 TXT 路径，请使用粘贴或导入 TXT")
		}
		bytes, err := item.Read(format)
		if err != nil {
			return input, fmt.Errorf("无法取得拖放数据，请使用粘贴或导入 TXT")
		}
		total += len(bytes)
		if total > tasklist.MaxBytes {
			return input, fmt.Errorf("投放数据不能超过 10 MiB")
		}
		if format == transfer.Text {
			input.texts = append(input.texts, bytes)
			continue
		}
		for _, line := range strings.Split(strings.ReplaceAll(strings.TrimRight(string(bytes), "\x00"), "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			u, err := url.Parse(line)
			if err != nil {
				return input, fmt.Errorf("URI 列表格式无效，请使用粘贴或导入 TXT")
			}
			if u.Scheme == "file" {
				files, err := transfer.New(transfer.NewItem(transfer.Bytes(transfer.URIList, []byte(line)))).Files()
				if err != nil || len(files) != 1 {
					return input, fmt.Errorf("仅支持本地 TXT 路径")
				}
				input.paths = append(input.paths, files[0])
			} else if u.Scheme == "http" || u.Scheme == "https" {
				input.texts = append(input.texts, []byte(line))
			} else {
				return input, fmt.Errorf("仅支持 HTTP/HTTPS 链接和本地 TXT 文件")
			}
		}
	}
	if len(input.texts)+len(input.paths) == 0 {
		return input, fmt.Errorf("没有可用链接或 TXT 文件，请使用粘贴或导入 TXT")
	}
	return input, nil
}

func (a *app) beginDrop(data transfer.Data) {
	if a.stopping {
		return
	}
	if a.deps.dropObserved != nil {
		a.deps.dropObserved(data.Formats())
	}
	a.openTaskList(false)
	a.dropBatchID++
	a.taskList.drop = true
	a.taskList.batchID = a.dropBatchID
	input, err := captureDrop(data)
	if err != nil {
		a.taskList.err = err.Error()
		return
	}
	ctx, generation, batch := a.taskList.ctx, a.taskList.generation, a.taskList.batchID
	items, _ := a.engine.Snapshot()
	a.taskList.busy = true
	a.taskListJobs.Add(1)
	a.deps.async(func() {
		defer a.taskListJobs.Done()
		preview, err := tasklist.ReadBatch(ctx, input.texts, input.paths, items)
		a.dispatch(func() {
			if !a.taskListCurrent(generation) || a.taskList.batchID != batch {
				return
			}
			a.taskList.busy = false
			if err != nil {
				a.taskList.err = err.Error()
				return
			}
			a.taskList.preview = preview
			if preview.Valid+preview.Duplicate == 0 {
				a.taskList.err = "没有有效下载链接，请使用粘贴或导入 TXT"
			}
		})
	})
}

func (a *app) dropTarget(c *ui.Context, card ui.Element) bool {
	_, hover := ui.DataDragOver(card, dropOptions)
	hover = card.FileDragOver() || hover
	if drop, ok := ui.DropData(card, dropOptions); ok {
		if !a.dropSeen {
			a.restoreQuickInput = a.quickInput.Focused(c)
			a.dropSeen = true
			a.beginDrop(drop.Data)
		}
	} else if paths := card.DroppedFiles(); len(paths) > 0 {
		if !a.dropSeen {
			a.restoreQuickInput = a.quickInput.Focused(c)
			a.dropSeen = true
			data, err := transfer.FileData(append([]string(nil), paths...)...)
			if err != nil {
				a.toast("投放路径无效，请使用导入 TXT")
			} else {
				a.beginDrop(data)
			}
		}
	} else {
		a.dropSeen = false
	}
	return hover
}
