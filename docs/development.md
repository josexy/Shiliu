# 开发指南

[返回 README](../README.md) · [协作规范](../AGENTS.md)

以下命令均从仓库根目录执行。

## 本地运行

需要 Go 1.27.3 或更高版本，依赖版本以 [go.mod](../go.mod) 为准。构建无需 Node.js 或 cgo。

```sh
go mod download
go run .
```

开发时用 `go tool mygo dev` 自动重建并重启。

## 构建与打包

```sh
go build ./...
go tool mygo build
```

打包产物位于 `build/<os>-<arch>/`，名称与版本由 [mygo.json](../mygo.json) 配置。`mygo build` 和 `mygo dev` 会注入版本；直接 `go run .` 或 `go build` 未注入时显示 `dev`。

| 平台 | 主要产物 |
| --- | --- |
| Windows | `拾流.exe`、`shiliu-setup-<版本>-<架构>.exe` |
| macOS | `拾流.app`、`shiliu-<版本>-<架构>.dmg`（universal 无架构后缀） |
| Linux | `shiliu`、安装归档、`.deb`、`.rpm`、`PKGBUILD` |

配置遵循 [MyGo 配置规范](https://mygo.egoist.dev/docs/configuration)，并以当前锁定的 v0.10.0 实现为准。原生应用使用 `mygo.json`，无需 TypeScript 配置或前端构建字段：

- `name` 为「拾流」，用于可执行文件、应用菜单和安装器；`slug` 显式固定为 `shiliu`，用于 ASCII 包名。
- `identifier` 为 `io.github.josexy.shiliu`；发布后保持此标识和 slug 稳定。旧的 `com.example.downloader` 是模板占位标识，旧安装包不会被识别为同一产品；使用新版前请退出旧版。
- 版权沿用 LICENSE 中的 joxrays，`license` 为 `MIT`；Windows 发布者从版权信息推导，安装目录名为 `Shiliu`。
- 保留 `linux.webview: false`，原生 UI 不需要 WebKitGTK；`resourcesExclude` 排除图标设计原稿。
- 应用内展示名称与 HTTP User-Agent 统一在 `internal/appinfo`；改名时同步维护 `mygo.json`。

数据兼容：安装版优先使用「拾流」目录；该目录不存在而旧 `Downloader` 目录存在时继续使用旧目录及原凭据服务，不搬移或覆盖文件。`mygo dev` 对应「拾流 Dev」/`Downloader Dev`，直接 `go run .` 对应「拾流 Local」/`go-downloader`，彼此隔离。系统权限、旧安装器注册信息不会迁移。

跨平台打包：

```sh
go tool mygo build -platform windows/amd64,darwin/universal,linux/amd64
```

macOS 签名与 `.dmg` 制作需要 macOS。Windows 安装包需要 NSIS，MyGo 可在 Windows 上自动下载。

## 项目结构

| 位置 | 职责 |
| --- | --- |
| `main.go`、`internal/app` | 启动入口；应用生命周期、UI 与平台服务 |
| `internal/download`、`internal/proxy` | 下载引擎与代理 |
| `internal/settings`、`internal/store` | 设置、任务持久化与凭据存储 |
| `internal/rules`、`internal/tasklist` | 分类目录规则与任务清单 |
| `internal/diagnostics`、`internal/appinfo` | 脱敏诊断与版本信息 |

`resources/` 存放资源，`scripts/` 存放生成与测量脚本，`docs/` 存放使用、验收和测量文档。协作规范见 [AGENTS.md](../AGENTS.md)；UI 开发另见 [MyGo 维护技能](../.agents/skills/mygo-maintenance/SKILL.md)。

图标原稿为 `resources/design/icon-source.png`，由内置 ImageGen 生成，提示词见同目录的 `icon-prompt.txt`。运行 `python scripts/make-icon.py`（需要 Pillow）可重建 1024×1024 RGBA 的 `resources/icon.png`，保留透明边缘。UI、托盘和 MyGo 打包共享此文件；更新后重新构建应用。设计目录不随应用分发。

## 验证与视觉快照

常规检查：

```sh
go test ./...
go tool mygo vet .
```

MyGo vet 包含 Go vet。网络测试使用本地 HTTP/代理服务，UI 测试使用 `ui.Tester`；原生窗口、贴靠和通知仍需实机检查。纯文档修改只需核对命令、链接并运行 `git diff --check`。

无 cgo 验证：

```powershell
# PowerShell
$previousCGO = $env:CGO_ENABLED
try {
    $env:CGO_ENABLED = '0'
    go test ./...
    go build ./...
} finally {
    $env:CGO_ENABLED = $previousCGO
}
```

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build ./...
```

生成浅/深主题、960×640 最小窗口与 125% 缩放快照：

```powershell
# PowerShell
$previousSnapshot = $env:DOWNLOADER_SNAPSHOT
try {
    $env:DOWNLOADER_SNAPSHOT = '1'
    go test -run '^TestSnapshots$' -count=1 ./internal/app
} finally {
    $env:DOWNLOADER_SNAPSHOT = $previousSnapshot
}
```

```bash
DOWNLOADER_SNAPSHOT=1 go test -run '^TestSnapshots$' -count=1 ./internal/app
```

快照输出到已忽略的 `internal/app/testdata/snapshots/`。检查像素结果后，按需更新 `docs/screenshot-light.png` 和 `docs/screenshot-dark.png`。

性能验证见 [多连接测量](multi-connection-measurement.md) 与 [速度历史测量](speed-history-measurement.md)。

原生行为的检查步骤与平台记录见 [原生功能验收](native-transfer-acceptance.md)，后续工作见 [迭代计划](roadmap.md)。
