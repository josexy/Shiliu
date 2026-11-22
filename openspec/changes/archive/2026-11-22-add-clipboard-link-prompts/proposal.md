# Proposal

## Why

已有粘贴按钮不会主动提示可下载链接。用户选择启用后提供一次性候选提示，可简化添加而不自动创建任务或持续积累剪贴板内容。

来源：按需穿插，对应 [迭代计划](../../../../docs/roadmap.md)。

## What Changes

- 设置中新增默认关闭的剪贴板链接提示，启用时监听后续文本变化并识别 HTTP/HTTPS 候选。
- 仅在用户确认后添加，支持忽略；当前会话内对相同链接集合、已有任务和应用自身复制去重。
- 关闭设置、退出或过期回调时停止读取/更新，限制文本大小和去重缓存，不持久化剪贴板原文。

本次不包含：默认监听、剪贴板历史、自动下载和云同步。

## Capabilities

### New Capabilities

- `clipboard-link-prompts`：剪贴板链接提示的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

`internal/settings`、`internal/app/deps.go`、`app_actions.go`、设置/候选弹窗；复用 MyGo 0.10.0 `Clipboard.OnChanged`。

实施依赖：无硬依赖；后台托盘存在时只保存最新候选，恢复窗口后提示。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
