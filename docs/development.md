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
| Windows | `Downloader.exe`、`downloader-setup-<版本>-<架构>.exe` |
| macOS | `Downloader.app`、`.dmg` |
| Linux | 可执行文件、安装归档、`.deb`、`.rpm`、`PKGBUILD` |

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

图标由 `python scripts/make-icon.py` 生成到 `resources/icon.png`，需要 Pillow；更新后重新构建应用。

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
