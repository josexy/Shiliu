# Proposal

## Why

当前下载管理器按任务切片顺序启动队列，界面的名称、大小和时间排序无法表达启动优先级。独立且持久的队列顺序能让急需的任务先取得空闲槽位。

来源：第二阶段 · roadmap 5，对应 [迭代计划](../../../docs/roadmap.md)。

## What Changes

- 为尚未取得 worker 的排队任务提供置顶、上移和下移操作，并显示队列位置。
- 调度顺序独立于列表/卡片的展示排序，暂停再继续和重启恢复后保留相对顺序。
- 调整顺序不抢占运行任务，仍遵守并发数；删除和新增不会破坏其余任务顺序。

本次不包含：抢占已运行任务、带宽优先级和拖动排序。

## Capabilities

### New Capabilities

- `download-queue-priority`：队列优先级的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

`internal/download/manager.go`、`types.go`、`internal/store`、`internal/app/app_actions.go`、`view_items.go`、`app.go` 及测试。

实施依赖：无硬依赖；计划下载使用本 change 的调度顺序。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
