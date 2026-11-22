# Tasks

前置条件：无硬依赖；拖放的 TXT 解析复用本 change。

以下条目均为待实施任务，本轮提案生成不代表功能完成。各组同步完成其测试与文档，再进入下一组；网络验收只使用本地服务。

## 1. 解析与导出格式

- [x] 1.1 实现 internal/tasklist 的 TXT/BOM 解析、候选保留和脱敏预览；表驱动测试覆盖重复/非法协议/分隔符/认证及三个容量边界，注释输入格式。
- [x] 1.2 实现版本化 allowlist 导出 DTO；验证选中/全部、未知大小及认证/query/header/proxy/path 均不出现，并以 golden JSON 固定分享格式。
- [x] 1.3 实现可取消后台读写及同目录原子替换；临时目录测试覆盖读取失败、写失败、取消和保留原文件，并文档化非备份语义。

## 2. 界面与批量创建

- [x] 2.1 通过 deps 增加打开/保存选择器和预览界面；ui.Tester 覆盖取消、焦点、滚动、异步晚到与重复确认。
- [x] 2.2 确认时使用统一 Add 流程并重新去重；本地用例验证 AutoStart=false、路径冲突、确认前任务变化及部分创建错误汇总。
- [x] 2.3 更新 README TXT 限制/脱敏导出示例和 roadmap 导入导出项，核对示例可解析，生成检查相关快照。

## 3. 集成验收

- [x] 3.1 格式化 Go 改动并运行受影响包测试、`go test ./...`、`go tool mygo vet .` 和 `git diff --check`；所有命令通过后记录结果，后者 MyGo 检查包含 Go vet。
- [x] 3.2 在支持的工具链下对实际修改的并发包运行 `go test -race`，并设置 `CGO_ENABLED=0` 执行 `go test ./...` 与 `go build ./...`；记录实际平台及不支持的检查，不把编译结果当作原生验收。
- [x] 3.3 按 README 的 PowerShell/Bash 快照命令检查涉及界面的浅色/深色、960×640 和 125% 缩放；有可见变化时按需更新 docs 截图，确认不提交生成快照目录或真实用户数据。

## 实施验证（2026-11-22）

- Windows/amd64：受影响包及 `go test ./...`、`go tool mygo vet .`、`git diff --check` 通过。
- `go test -race ./internal/app ./internal/download ./internal/tasklist` 通过；`CGO_ENABLED=0` 的全量测试与构建通过。
- 已生成并检查浅/深主题导入、导出弹窗的 960×640 / 125% 真实像素快照，并更新 docs 主界面截图。生成目录保持 Git 忽略。
- 文件选择器使用 deps 替身验证；未将此描述为原生文件选择器或 macOS/Linux 实机验收。
- 保留工作区已有 MyGo 0.10.0 升级，补全其 CLI 打包所需间接依赖；标题栏测试改用坐标点击验证弹窗遮挡，适配新版 Tester.Click 的可达性检查。
