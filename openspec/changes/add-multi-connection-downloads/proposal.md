# Proposal

## Why

当前单任务仅用一条连接，部分受单连接带宽限制的服务可能无法充分利用网络。可选的多连接能力需要在响应版本、分片持久化和资源上限都可靠时才启用，并保持单连接回退。

来源：第三阶段 · roadmap 10，对应 [迭代计划](../../../docs/roadmap.md)。

## What Changes

- 默认保持单连接；可配置每任务连接数和全局连接上限，探测有效 Range、已知长度和强 ETag 后才启用分片。
- 实现分片边界、进度持久化、暂停恢复、失败重试、完整覆盖验证和完成发布。
- 所有连接遵守任务/全局限速、代理、认证重定向和取消规则；响应不安全时停止所有分片并安全回退。
- 使用本地可控服务器验证逐字节正确性，并记录受限与不受限场景的吞吐表现。

本次不包含：保证提速、镜像多源、BT/HLS 和无限连接数。

## Capabilities

### New Capabilities

- `multi-connection-downloads`：多连接分片下载的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

`internal/download/fetch.go`、`manager.go`、`meta.go`、`types.go`、`filename.go`、设置与存储、任务详情；分片元数据需版本化兼容旧单连接记录。

实施依赖：硬依赖 `add-per-task-rate-limits`、`add-file-integrity-verification`、`add-expired-link-replacement`；建议第二阶段稳定后实施。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
