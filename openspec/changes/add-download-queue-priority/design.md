# Design

## Context

动机与范围见 [proposal.md](proposal.md)，行为约束见 [spec.md](specs/download-queue-priority/spec.md)。

`manager.go/kick` 顺序遍历 m.tasks 选取 StateQueued；`app.go/computeVisible` 独立复制并排序 Item。Records 按 m.tasks 序列保存，Restore 把未完成任务置为暂停；manager_test 已覆盖并发与重启。尚无主规格需要修改。

实施顺序：无硬依赖；计划下载使用本 change 的调度顺序。

## Goals / Non-Goals

**Goals:** 在现有包边界内实现上述行为，后台工作可取消、可用替身测试；保留旧数据读取与用户现有默认行为。

**Non-Goals:** 抢占已运行任务、带宽优先级和拖动排序。

## Decisions

### 1. 状态与职责

在 task/Record 增加可选的队列序号 QueueOrder，Item 暴露当前队列位置。调度以序号排序的候选为准，不复用 computeVisible 或修改展示 Sort 设置；只用 m.tasks 的数组位置虽改动小，但容易与持久化、过滤展示耦合。

### 2. 核心执行方式

引擎提供按 ID 的 move-to-front/up/down 操作；加锁重新验证 queued 且 !working。只交换当前可调队列的排序序号，或在置顶后压实顺序；暂停项保留自己的相对锚点。所有通知锁外发送，界面不能先改本地位置再假定引擎接受。

### 3. 一致性与边界

旧记录按原数组顺序赋值；新记录中重复、缺失或非法序号稳定修复并压实，新增使用末尾序号。暂停、继续不重新分配序号，删除只移除目标。重启仍遵守现有默认暂停，不借排序功能改变恢复策略。

### 4. 生命周期与交互

列表/卡片菜单和详情显示真实队列位置，置顶/上下移按钮仅对可调项启用。搜索筛选不改变相邻定义；按任务 ID 给控件稳定身份，通过统一 app_actions 回调操作引擎。

## Risks / Trade-offs

- 队列变动与 worker 启动竞争 → 操作时锁内重新验证，测试接受与拒绝两种有序结果。
- 持久化写入顺序被展示排序污染 → 存储读取引擎 Record，不从 visible 构建。

## Migration Plan

任务字段使用 omitempty；读取旧记录时保留原记录顺序。回滚到旧版本仍能读取记录，但不保证新优先级继续生效；升级后所有未完成任务保持原先暂停规则。

先执行针对性用例，再执行全量测试与 MyGo 静态检查；界面变化检查两套主题、960×640 和 125% 缩放。平台服务以 deps 替身测试，原生行为另记真实验证范围。发布后更新 README 与 roadmap；本提案阶段不变更应用版本。
