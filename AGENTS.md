# 项目协作规范

本文件适用于整个仓库。应用名为「拾流 · Shiliu」，使用 Go 和 MyGo 原生 UI，界面语言为简体中文。修改前先阅读 [README.md](README.md)、相关实现和现有测试；Go 与 MyGo 的版本以 [go.mod](go.mod) 为准，应用版本与打包配置以 [mygo.json](mygo.json) 为准。

## 目录与依赖边界

| 位置 | 职责 |
| --- | --- |
| `main.go` | 保持为轻量启动入口，调用 `app.Run()` 并处理返回的错误 |
| `internal/app` | 应用生命周期、窗口状态、UI 绘制、主题、操作回调及平台服务装配 |
| `internal/download` | 下载队列、HTTP 请求、续传、重试、文件命名与任务快照 |
| `internal/proxy` | 代理配置、系统代理探测、HTTP/SOCKS5 传输与跳过规则 |
| `internal/settings` | 设置类型、默认值与规范化 |
| `internal/store` | 设置和任务记录持久化、凭据存储接口 |
| `internal/appinfo` | 应用名称、版本规范化与 HTTP User-Agent |
| `internal/diagnostics` | 有限容量的脱敏诊断记录 |
| `internal/rules`、`internal/tasklist` | 分类目录规则、任务清单解析与导入导出 |
| `resources`、`scripts`、`docs` | 打包资源、资源生成脚本与项目文档截图 |

- 新增界面、窗口状态和操作回调放在 `internal/app`，按现有 `view_*.go`、`app_*.go` 的职责组织文件；根目录的 Go 源码只保留启动入口。
- 下载、代理、设置、存储及其他非 UI 包不依赖 `internal/app` 或 `mygo/ui`。`internal/appinfo` 不依赖 UI 框架。具有独立职责的功能放进对应的 `internal` 包，避免循环依赖。
- `internal/app` 的启动边界为 `Run()`；应用内部状态保持非导出，通过 `deps` 和小接口隔离平台服务，便于测试。
- `app.Run()` 在主 goroutine 调用。保持单实例、语言初始化、窗口回调和退出保存的顺序。

## Go 编码约定

- 使用 `gofmt` 格式化改动的 Go 文件，沿用现有命名和注释风格；用户界面文案使用简体中文。
- 优先使用标准库和现有依赖。新增依赖时说明用途，依赖变更同步维护 `go.mod` 和 `go.sum`。
- 错误保留上下文，必要时用 `%w` 包装；后台任务使用 `context.Context` 支持取消和超时。
- 不把可恢复的下载、代理或文件错误变成 `panic`。复用当前错误展示与通知路径。
- 保持改动聚焦，保留工作区中已有的用户修改；重构时核对调用点、测试、文档和资源路径。

## 应用名称与打包配置

- 应用内名称和 HTTP User-Agent 复用 `internal/appinfo`，调整名称时同步 `mygo.json`、窗口、托盘、关于页与文档。
- 原生应用保留 `mygo.json`。修改配置前核对当前 MyGo 依赖源码中的 `docs/configuration.md` 和 `cmd/mygo/config.go`；保持 `linux.webview: false`，无需引入前端配置或 JavaScript 运行时。
- `slug` 和 `identifier` 是安装与系统集成标识，发布后保持稳定，不能仅因展示名称调整而随意替换。打包命名与平台配置见 [开发指南](docs/development.md#构建与打包)。
- 保留 `internal/app/app_identity.go` 的旧数据目录与凭据服务兼容逻辑：新目录优先，旧目录复用时不搬移或覆盖数据；安装版、`mygo dev` 与直接运行保持隔离。数据目录选择必须在单实例锁及创建新数据目录之前完成。

## MyGo UI 开发

涉及 UI、窗口行为或 MyGo 升级时，先阅读 [mygo-maintenance 技能](.agents/skills/mygo-maintenance/SKILL.md)。通过以下命令定位当前依赖源码，并查阅 `ui/doc.go` 及相关 API 的实现注释：

```sh
go list -m -f '{{.Dir}}' github.com/egoist/mygo
```

- `ui.Element` 和控件 parts 只属于当前构建轮次，不存入应用状态或后台任务。持久状态使用数据字段、控件状态和 `ui.Handle`。
- 不在应用状态或后台任务中保存 `*ui.Context`。需要长期使用的剪贴板、URL 和重绘能力时捕获 `c.Services()`。
- 有状态控件在构造前通过 `c.Key(...)` 指定稳定身份；列表和网格使用任务 ID 作为稳定键。
- 输入绑定到持久字段；衍生的分段选择等值按技能中的输入时序在操作回调中写回。
- 视图可能为同一事件构建多次。下载、文件操作、设置保存等副作用通过操作回调或受保护的后台任务触发。
- 慢速 I/O 放在后台执行。后台结果通过 `Window.Update` 或现有 `dispatch` 发布到 UI 状态；`Invalidate` 仅请求重绘，不负责同步状态修改。
- 配色集中维护在 `internal/app/style.go`，同时检查浅色、深色、选中、悬停、禁用和错误状态。保留键盘焦点、工具提示和无障碍标签。
- 自定义标题栏保留拖动、最大化/还原、正常关闭流程以及 `MaximizeButton()` 的贴靠区域；不要用 `Destroy` 替代正常关闭。

## 下载、代理与持久化约束

- 最终文件、`.part` 和 `.part.meta` 一起参与路径预留，避免任务之间或与已有文件发生冲突。
- 续传必须校验 Range、有效的 `If-Range` 验证器和响应版本；完整响应替换旧内容时同步更新验证器，避免拼接不同版本的数据。
- 网络请求保持可取消；不要在持有下载管理器锁时执行长时间 I/O 或通知回调。`Snapshot()` 保持轻量，文件存在性检查走后台刷新。
- HTTP 与 SOCKS5 路径都遵守代理跳过规则，保留端口、IPv6 和 CIDR 等既有语义。
- 代理密码通过 `SecretStore` 保存，不写入 JSON。URL 内嵌凭据仅保存在内存，显示、复制和持久化的 URL 应去除凭据。
- 持久化沿用原子写入和损坏文件隔离机制。变更设置或任务格式时考虑已有数据的读取与默认值兼容。

## 验证与测试

按改动范围选择验证，不为纯文档或样式微调新增没有行为价值的测试。

- Go 逻辑变更：运行受影响的测试及 `go test ./...`，并执行 `go vet ./...`。
- UI 或 MyGo 相关变更：执行 `go tool mygo vet .`；此命令包含 Go vet，可替代单独的 `go vet ./...`。使用现有 `ui.Tester` 覆盖相关输入、焦点和界面状态。
- 布局、主题、应用名称或图标变更：生成并检查两套主题的真实像素快照，关注 960×640 最小窗口和 125% 缩放下的相关界面。原生拖动、贴靠等行为需要在目标系统核验，说明实际验证范围。
- 打包配置或图标变更：构建受影响平台的产物，核对文件命名、应用元信息、嵌入图标和资源排除规则；区分交叉编译、打包成功与目标系统安装/运行验收。
- 并发或共享状态变更：在支持的环境中对受影响包运行 `go test -race`；构建或依赖变更检查 `CGO_ENABLED=0` 构建/测试与受影响平台。
- 网络测试使用本地 `httptest`、本地代理或测试替身，不依赖外部下载站点；平台服务优先通过 `deps` 替换。
- 纯文档变更：核对命令、路径、相对链接及 `git diff --check`，不要求重复构建和生成截图。

快照命令的 Bash 与 PowerShell 写法见 [开发指南的验证说明](docs/development.md#验证与视觉快照)。生成目录为 `internal/app/testdata/snapshots/`，已被 Git 忽略；UI 有可见变化时按需更新 `docs/screenshot-light.png` 和 `docs/screenshot-dark.png`。

## 文档与交付

- 功能、目录结构、构建方式或限制变化时同步更新 README。文档命令从仓库根目录执行，带环境变量的示例兼顾 PowerShell 和 Bash。
- 不提交 `build/`、`.mygo/`、自动生成的 `.syso`、视觉快照目录或真实用户数据。`resources/icon.png`、图标原稿与提示词、文档截图纳入版本管理。
- 图标原稿与生成提示词保存在 `resources/design/icon-source.png`、`resources/design/icon-prompt.txt`。更新原稿后运行 `python scripts/make-icon.py`，重建 1024×1024 RGBA 的 `resources/icon.png` 并保留透明边缘；UI、托盘和打包共享此文件。脚本只依赖 Pillow，不属于应用运行依赖；保持 `resourcesExclude` 排除 `design`，避免原稿随应用分发。
- 用户要求提交时，沿用现有英文祈使句提交标题，描述最终改动及必要的验证信息。
- 完成说明写清具体变化、执行的检查和未验证事项，不把编译通过描述为已验证所有平台行为。
