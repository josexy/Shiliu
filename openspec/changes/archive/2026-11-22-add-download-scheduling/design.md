# Design

## Context

动机与范围见 [proposal.md](proposal.md)，行为约束见 [spec.md](specs/download-scheduling/spec.md)。

当前 StatePaused 混合用户暂停与退出保存，Records 把全部活动任务改写为 paused，Restore 不自动继续。`deps.now` 仅供 app 使用，download/util.go 使用真实时钟。计划必须在引擎内部建模并复用队列优先级，不能由 UI 定时点击 Resume。

实施顺序：硬依赖 `add-download-queue-priority`；可与托盘功能联用，但不依赖托盘。

## Goals / Non-Goals

**Goals:** 在现有包边界内实现上述行为，后台工作可取消、可用替身测试；保留旧数据读取与用户现有默认行为。

**Non-Goals:** 唤醒电脑、自动启动应用、操作系统计划任务和周期性重新下载已完成文件。

## Decisions

### 1. 状态与职责

在 Record/Item 增加 Schedule（Enabled、StartAt UTC、DailyStartMinute、DailyEndMinute、StartReached）及 ManualPaused；ScheduleWaiting 单独表示等待原因。保持传输状态与调度意图分离，关闭窗口不影响计划。AutoStart 仍管普通新建；用户明确启用计划授权其按时运行。

### 2. 核心执行方式

在 download.Options 注入小 Clock 接口：Now 与可停止/重置 Timer；调度 goroutine 根据最近边界定时，最长一秒再次评估以发现时钟/时区变化，测试用虚拟时钟。现有零值 Options 使用真实时间，不依赖 app 包。

### 3. 一致性与边界

资格函数统一检查未完成且未失败、计划启用、非 ManualPaused、StartReached 或已到 StartAt、当前每日窗口；合格者只进入既有优先级队列。到窗口终点取消网络并标记计划等待，等 finishWorker 释放槽位后才可再次启动，避免两个 worker 争同一文件。

### 4. 生命周期与交互

一次性时间持久化绝对时刻；每日窗口使用当前本地时区，起点含、终点不含，跨午夜取两段并集，相同起止拒绝。StartReached 单调保存，回拨不撤销已达到条件。缺失窗口不补发，DST 重复小时按当前资格评估且不重复 worker。

### 5. 集成与验证

Pause/PauseAll 设置 ManualPaused；Resume/ResumeAll 清除它但走统一资格判断。Retry 明确解除失败后再评估时间。禁用计划将未完成任务暂停；退出只取消执行而不把计划等待改为手动暂停。旧无计划任务仍重启暂停。

### 6. 数据与性能

高级表单和详情持有计划草稿、启用动作及中文下一执行原因，异步状态用 dispatch 发布；用 generation 拒绝编辑关闭后的回调，UI 不保存任何构建 Element。

## Risks / Trade-offs

- 后台计划越过用户暂停 → 所有入口共用资格判断，覆盖托盘、批量和重启路径。
- 时间跳变导致忙循环或重复下载 → 定时器有最小等待，记录 StartReached，worker 身份锁内唯一。

## Migration Plan

硬依赖队列优先级先实施。新增计划字段缺失即无计划，ManualPaused 对旧未完成记录视为 true；新计划保存其真实意图。旧版本忽略新字段并恢复暂停，属于安全降级。

先执行针对性用例，再执行全量测试与 MyGo 静态检查；界面变化检查两套主题、960×640 和 125% 缩放。平台服务以 deps 替身测试，原生行为另记真实验证范围。发布后更新 README 与 roadmap；本提案阶段不变更应用版本。
