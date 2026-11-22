# Proposal

## Why

任务详情仅保留当前错误和累计重试数，无法还原失败前的状态变化。容量受限且源头脱敏的诊断事件可帮助分析重试、HTTP 与代理问题。

来源：按需穿插，对应 [迭代计划](../../../../docs/roadmap.md)。

## What Changes

- 记录任务状态、重试次数、HTTP 状态与代理错误等结构化事件，环形缓存同时限制事件数和字节数。
- 在用户操作时导出本次运行的诊断日志，包含应用版本与平台信息。
- 采集时移除认证、请求头值、URL 用户信息/查询串/片段和本地绝对路径，不采集响应体。

本次不包含：远程遥测、永久后台日志、请求/响应抓包和导出用户凭据。

## Capabilities

### New Capabilities

- `diagnostic-log-export`：诊断日志导出的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

新增 `internal/diagnostics` 作为独立有界事件存储，`internal/download`/`internal/proxy` 通过小接口上报，`internal/app` 装配与导出。

实施依赖：无硬依赖；可用于后续分片下载排错。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
