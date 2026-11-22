package app

import (
	"crypto/rand"
	"fmt"
	"github.com/egoist/mygo/ui"
	"go-downloader/internal/download"
	"go-downloader/internal/rules"
	"go-downloader/internal/settings"
	"net/url"
	"strings"
)

type directoryRuleDraft struct {
	rule settings.DirectoryRule
	kind string
}
type ruleEditorState struct {
	open                         bool
	rows                         []*directoryRuleDraft
	previewURL, previewName, err string
}

func (a *app) openRules() {
	a.ruleEditor = ruleEditorState{open: true}
	for _, r := range a.s.DirectoryRules {
		label := "扩展名"
		if r.Kind == "domain" {
			label = "域名"
		}
		a.ruleEditor.rows = append(a.ruleEditor.rows, &directoryRuleDraft{rule: r, kind: label})
	}
}
func (a *app) addRule() {
	a.ruleEditor.rows = append(a.ruleEditor.rows, &directoryRuleDraft{rule: settings.DirectoryRule{ID: rand.Text(), Enabled: true, Kind: "extension", Dir: a.s.DownloadDir}, kind: "扩展名"})
}
func (a *app) moveRule(id string, delta int) {
	for i, r := range a.ruleEditor.rows {
		if r.rule.ID == id {
			j := i + delta
			if j >= 0 && j < len(a.ruleEditor.rows) {
				a.ruleEditor.rows[i], a.ruleEditor.rows[j] = a.ruleEditor.rows[j], a.ruleEditor.rows[i]
			}
			return
		}
	}
}
func (a *app) deleteRule(id string) {
	for i, r := range a.ruleEditor.rows {
		if r.rule.ID == id {
			a.ruleEditor.rows = append(a.ruleEditor.rows[:i], a.ruleEditor.rows[i+1:]...)
			return
		}
	}
}
func (a *app) ruleDrafts() ([]settings.DirectoryRule, error) {
	var rows []settings.DirectoryRule
	for _, draft := range a.ruleEditor.rows {
		r := draft.rule
		r.Kind = "extension"
		if draft.kind == "域名" {
			r.Kind = "domain"
		}
		rows = append(rows, r)
	}
	return settings.NormalizeDirectoryRules(rows)
}
func (a *app) saveRules() {
	rows, err := a.ruleDrafts()
	if err == nil {
		err = a.engine.SetDirectoryRules(rows)
	}
	if err != nil {
		a.ruleEditor.err = err.Error()
		return
	}
	a.s.DirectoryRules = rows
	a.saveSettings()
	a.ruleEditor = ruleEditorState{}
	a.toast("目录规则已保存，仅影响新任务")
}
func directoryPreview(rows []settings.DirectoryRule, raw, name, base, override string) string {
	valid, _ := download.NormalizeURLs(raw)
	if len(valid) != 1 {
		return "输入一个有效链接以预览目录"
	}
	if strings.TrimSpace(name) == "" {
		u, _ := url.Parse(valid[0])
		name = download.NameFromURL(u)
	} else {
		name = download.Sanitize(name)
	}
	dir, id := rules.Select(rows, valid[0], name, base, override)
	if override != "" {
		return "自定义目录：" + dir
	}
	for i, r := range rows {
		if r.ID == id {
			return fmt.Sprintf("命中第 %d 条（%s）：%s", i+1, r.Pattern, dir)
		}
	}
	return "默认目录：" + dir
}
func (a *app) advancedDirectoryPreview() string {
	override := ""
	if a.advanced.dirOverride {
		override = a.advanced.dir
	}
	return directoryPreview(a.s.DirectoryRules, a.advanced.url, a.advanced.fileName, a.s.DownloadDir, override)
}
func (a *app) rulesModal(c *ui.Context) {
	if !a.ruleEditor.open {
		return
	}
	p := a.palette
	width, height := c.Size()
	c.Key("directory-rules-dialog")
	ui.Modal(c, &a.ruleEditor.open, func() {
		ui.Column(c).Width(min(680, width-100)).Gap(12).Children(func() {
			ui.Text(c, "分类目录规则").FontSize(20).Bold()
			ui.Text(c, "首条启用且匹配的规则生效；自定义目录优先，只影响新任务。").FontSize(12).TextColor(p.Muted)
			ui.Scroll(c).Height(min(220, max(120, height-370))).Gap(10).Children(func() {
				if len(a.ruleEditor.rows) == 0 {
					ui.Text(c, "尚无规则，新任务使用默认保存目录。").FontSize(13).TextColor(p.Muted)
				}
				for i, r := range a.ruleEditor.rows {
					c.Key(r.rule.ID)
					ui.Column(c).Padding(10).Gap(8).Background(p.Canvas).Radius(8).Children(func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							c.Key("enabled")
							ui.Checkbox(c, &r.rule.Enabled, fmt.Sprintf("启用规则 %d", i+1))
							ui.Spacer(c)
							ui.Button(c, "上移").Label("规则上移 " + r.rule.ID).Disabled(i == 0).OnClick(func() { a.moveRule(r.rule.ID, -1) })
							ui.Button(c, "下移").Label("规则下移 " + r.rule.ID).Disabled(i == len(a.ruleEditor.rows)-1).OnClick(func() { a.moveRule(r.rule.ID, 1) })
							ui.Button(c, "删除").Label("删除规则 " + r.rule.ID).OnClick(func() { a.deleteRule(r.rule.ID) })
						})
						ui.Row(c).Gap(8).Children(func() {
							c.Key("kind")
							ui.Select(c, &r.kind, []string{"扩展名", "域名"}).Label("规则类型 " + r.rule.ID).Width(92)
							c.Key("pattern")
							ui.TextInput(c, &r.rule.Pattern).Label("规则条件 " + r.rule.ID).Placeholder("zip 或 *.example.com").Grow(1)
							c.Key("dir")
							ui.TextInput(c, &r.rule.Dir).Label("规则目录 " + r.rule.ID).Placeholder("绝对目标目录").Grow(1)
						})
					})
				}
			})
			ui.Button(c, "新增目录规则").OnClick(a.addRule)
			ui.Row(c).Gap(8).Children(func() {
				c.Key("rule-preview-url")
				ui.TextInput(c, &a.ruleEditor.previewURL).Label("规则预览链接").Placeholder("输入链接试算").Grow(2)
				c.Key("rule-preview-name")
				ui.TextInput(c, &a.ruleEditor.previewName).Label("规则预览文件名").Placeholder("可选文件名").Grow(1)
			})
			preview := "请先修正规则格式"
			if rows, err := a.ruleDrafts(); err == nil {
				preview = directoryPreview(rows, a.ruleEditor.previewURL, a.ruleEditor.previewName, a.s.DownloadDir, "")
			}
			ui.Text(c, preview).FontSize(12).TextColor(p.Muted).SingleLine().Tooltip(preview)
			if a.ruleEditor.err != "" {
				ui.Text(c, a.ruleEditor.err).FontSize(12).TextColor(p.Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "取消规则编辑").OnClick(func() { a.ruleEditor = ruleEditorState{} })
				ui.PrimaryButton(c, "保存规则").OnClick(a.saveRules)
			})
		})
	}).Background(p.Card).Radius(14).Border(1, p.Border)
}
