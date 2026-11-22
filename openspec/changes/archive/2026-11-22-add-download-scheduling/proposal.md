# Proposal

## Why

当前只能立即添加、暂停和继续，不能让下载在指定时间或每日允许时段自动运行。计划调度需要与用户暂停意图、队列顺序和退出恢复兼容，避免意外启动任务。

来源：第二阶段 · roadmap 6，对应 [迭代计划](../../../../docs/roadmap.md)。

## What Changes

- 为任务配置一次性最早开始时间和可选的每日允许时段；设置后显式启用计划。
- 到点且处于允许时段时进入正常队列，时段结束释放连接；用户主动暂停优先于自动调度。
- 持久化计划和暂停意图，定义跨午夜、错过开始、重启、系统时钟和时区变化的行为。

本次不包含：唤醒电脑、自动启动应用、操作系统计划任务和周期性重新下载已完成文件。

## Capabilities

### New Capabilities

- `download-scheduling`：计划下载的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

`internal/download` 的调度与记录、`internal/app` 高级表单/任务详情、`internal/store`；注入时钟以确定性测试时间边界。

实施依赖：硬依赖 `add-download-queue-priority`；可与托盘功能联用，但不依赖托盘。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
