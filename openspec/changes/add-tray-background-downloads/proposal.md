# Proposal

## Why

当前关闭主窗口会结束应用并暂停下载，长时间任务必须一直保留窗口。增加显式启用的后台运行选项，让用户能够隐藏窗口继续下载，并能可靠地恢复和退出。

来源：第二阶段 · roadmap 4，对应 [迭代计划](../../../docs/roadmap.md)。

## What Changes

- 设置中新增默认关闭的“关闭窗口后继续下载”，仅在托盘创建成功且平台验证通过时生效。
- 托盘菜单提供恢复窗口、活动数/排队数/总速度、全部暂停、全部继续和退出。
- 区分窗口关闭与真正退出；退出仍取消任务、保存记录并释放单实例资源，通知和第二实例能够恢复窗口。

本次不包含：开机自启、无界面服务、系统睡眠唤醒和额外托盘弹出面板。

## Capabilities

### New Capabilities

- `tray-background-downloads`：托盘与后台下载的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

`internal/app/run.go`、`app.go`、`deps.go`、`view_settings.go`、`internal/settings` 及测试；复用 MyGo 0.10.0 托盘与窗口 API，无新增运行依赖。

实施依赖：无硬依赖；建议作为第二阶段首项。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
