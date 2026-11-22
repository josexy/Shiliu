# Design

## Context

动机与范围见 [proposal.md](proposal.md)，行为约束见 [spec.md](specs/tray-background-downloads/spec.md)。

`run.go` 在 OnBeforeQuit 调用幂等 shutdown；`app.go` 的 startSaver 每两秒保存、刷新文件。第二实例和通知已执行 Show/Focus。MyGo 0.10.0 的 `modules.go` 提供 NewTray/Menu，`window.go` 提供 OnClose/Hide/Show，`app.go` 提供 OnQuitCanceled。Linux 托盘依赖 libayatana-appindicator3，创建成功仍不等价于桌面可恢复；本次只核实源码，未实测原生行为。

实施顺序：无硬依赖；建议作为第二阶段首项。

## Goals / Non-Goals

**Goals:** 在现有包边界内实现上述行为，后台工作可取消、可用替身测试；保留旧数据读取与用户现有默认行为。

**Non-Goals:** 开机自启、无界面服务、系统睡眠唤醒和额外托盘弹出面板。

## Decisions

### 1. 状态与职责

新增 `app_tray.go` 和小型 tray/window/quit 适配接口，由 deps 注入。状态仅保存托盘句柄、监听取消函数和退出标记；复用现有下载接口。直接在视图中创建托盘会随重建重复注册，因此创建放在 WhenReady 后的 start。

### 2. 核心执行方式

设置 `CloseToTray` 默认 false。Windows 首期启用需完成原生验收；macOS/Linux 适配与实测作为分平台任务，未通过的平台关闭该选项并给出原因。创建失败始终保留正常关闭，禁止仅凭 GOOS 隐藏窗口。菜单总是提供恢复与退出，不依赖双击或 tooltip。

### 3. 一致性与边界

OnBeforeQuit 只标记 quitting，普通退出的不可逆 shutdown 放在不可取消的 OnQuit 中；MyGo 0.10.0 的 OnBeforeQuit、窗口 OnClose 和 OnWillQuit 都可能取消退出，不能提前关闭下载引擎。系统结束会话时 OnBeforeQuit 携带 SessionEnding，此时同步 shutdown 且不拦截关闭。普通 OnClose 仅在 !quitting、选项开启且托盘有效时 PreventDefault + Hide。OnQuitCanceled 清理退出标记，保留仍可运行的引擎；shutdown 使用现有 sync.Once，不能用 Window.Destroy 代替正常关闭。此调整于实施时经用户确认，源于当前 MyGo 退出钩子的实际取消顺序。

### 4. 生命周期与交互

复用 onEngineChange 的 dirty 标记，在独立于可见窗口帧的两秒刷新机制中把 Summary 投递到主线程更新菜单。关闭窗口不会停止 saver 或下载。所有 native 对象操作均走主线程，后台不持有 ui.Context。

### 5. 集成与验证

新增设置控件使用持久字段和稳定 c.Key；派生勾选值在操作回调写回，通知和第二实例复用统一 restoreWindow。与新增调度结合时托盘 ResumeAll 仍由引擎遵守时间条件。

## Risks / Trade-offs

- Linux 托盘宿主缺失但图标创建成功 → 未经真实桌面验证不开放隐藏模式，提供直接退出的退化路径。
- 退出钩子顺序误将退出拦截 → 假服务覆盖 OnBeforeQuit/OnClose 顺序与幂等清理，原生系统复核关闭和注销。

## Migration Plan

新增设置字段缺失即关闭；升级不改变旧用户关闭习惯。关闭功能可直接恢复原退出流程，不改下载记录格式。平台验证失败时只撤回该平台入口，不移除或更改下载数据。

先执行针对性用例，再执行全量测试与 MyGo 静态检查；界面变化检查两套主题、960×640 和 125% 缩放。平台服务以 deps 替身测试，原生行为另记真实验证范围。发布后更新 README 与 roadmap；本提案阶段不变更应用版本。
