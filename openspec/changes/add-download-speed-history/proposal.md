# Proposal

## Why

当前详情只显示即时速度，用户无法判断近期波动或限速效果。定长的近期速度曲线可以提供趋势信息，同时保持快照和内存开销可控。

来源：按需穿插 · 任务详情增强，对应 [迭代计划](../../../docs/roadmap.md)。

## What Changes

- 为每个任务采样最近 120 秒速度，任务详情显示时间轴、速度单位和当前值。
- 采用固定采样频率和固定容量，暂停/失败/完成显示零速，休眠缺口显示断开。
- 数据仅保存在内存，任务移除时释放，不在视图构建中采样或复制全部任务历史。

本次不包含：长期统计、历史持久化、图表导出和每个分片单独绘图。

## Capabilities

### New Capabilities

- `download-speed-history`：速度曲线的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

`internal/download` 的采样与按任务历史查询、`internal/app/view_details.go`、`style.go`、`deps.now` 或引擎时钟和快照测试。

实施依赖：无硬依赖；与单任务/多连接限速通过任务总速度集成。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
