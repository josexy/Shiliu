# Proposal

## Why

已有全局共享限速无法单独约束占用较大的任务。单任务上限与全局上限叠加，可在不中断连接和进度的情况下控制各任务带宽。

来源：第三阶段 · roadmap 7，对应 [迭代计划](../../../../docs/roadmap.md)。

## What Changes

- 高级新建和任务详情支持独立速度上限，KB/s 按 1024 字节换算，0 表示不另设任务上限。
- 全局与任务限速同时生效，动态修改唤醒等待者且不重建连接。
- 任务上限随记录持久化；取消、短读返还和停滞检测继续正确工作。

本次不包含：带宽最低保证、按时段变化的带宽策略和带宽优先级。

## Capabilities

### New Capabilities

- `per-task-rate-limits`：单任务限速的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

`internal/download/limit.go`、`fetch.go`、`types.go`、`request.go`、`internal/store`、高级新建和任务详情及本地网络测试。

实施依赖：无硬依赖；在多连接分片前实施，所有分片共享同一任务额度。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
