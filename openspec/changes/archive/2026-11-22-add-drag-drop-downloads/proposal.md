# Proposal

## Why

当前从浏览器或链接文件添加任务必须复制粘贴。原生拖放可将链接或 TXT 链接列表送入已有添加流程，减少日常操作。

来源：按需穿插，对应 [迭代计划](../../../../docs/roadmap.md)。

## What Changes

- 主窗口添加区接受外部 URI 列表、纯文本 HTTP/HTTPS 链接和 TXT 链接文件，展示可接受状态。
- 拖放后显示候选链接并确认添加，沿用解析、去重、AutoStart、分类目录和错误提示。
- 有界后台读取链接文件，拒绝目录、可执行文件及不支持协议；一次投放只处理一次。

本次不包含：拖入普通文件上传、递归扫描文件夹、HTML 网页抓取和平台快捷方式文件解析。

## Capabilities

### New Capabilities

- `drag-drop-downloads`：拖放添加的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

`internal/app/view_add.go`、`app_actions.go`、`deps.go`、MyGo `ui` 外部传输 API 和 `internal/tasklist`；保持任务数据与 UI 构建值分离。

实施依赖：硬依赖 `add-task-import-export` 的 TXT 解析；分类规则属于可选集成。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
