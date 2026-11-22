# Design

## Context

动机与范围见 [proposal.md](proposal.md)，行为约束见 [spec.md](specs/diagnostic-log-export/spec.md)。

任务只保留 Error 和 RetryCount，DiagnosticText/DiagnosticURL 已做 URL 脱敏，taskError 还清理已知请求头凭据；不能将任意原始 error 字符串直接交给新增日志。proxy.Check 和 ClientFactory 错误由 app 展示，当前无统一日志存储。

实施顺序：无硬依赖；可用于后续分片下载排错。

## Goals / Non-Goals

**Goals:** 在现有包边界内实现上述行为，后台工作可取消、可用替身测试；保留旧数据读取与用户现有默认行为。

**Non-Goals:** 远程遥测、永久后台日志、请求/响应抓包和导出用户凭据。

## Decisions

### 1. 状态与职责

新增 internal/diagnostics 定义 Event allowlist 与 Sink，小接口注入 download.Options 和代理调用装配；底层包可依赖该独立包，不依赖 app/ui。app 创建单个环形存储并负责导出，默认仅内存。

### 2. 核心执行方式

事件字段固定 timestamp/taskID/kind/httpStatus/retry/sourceHostAndPath/errorCode；不接收任意 headers/body/config。源 URL 在构造时经 DiagnosticURL，主机路径之外的本地 path 不采集；错误类别枚举从已知错误判断，未知错误只写 unknown，不写原文。

### 3. 一致性与边界

缓存上限 1000 条、编码字节 1 MiB、单条 2 KiB，截断先保持合法 UTF-8 和 JSON，再计算字节；淘汰增加 dropped 计数。同步入队仅短锁复制结构，不持下载锁回调，不执行文件 I/O。长文本字段直接省略而非模糊截断敏感值。

### 4. 生命周期与交互

导出复制一次有界快照，version=1/appVersion/platform/dropped/events，后台写同目录临时文件并原子替换。deps 提供 save dialog；没有记录也可导出带元数据的空列表，取消无副作用。

### 5. 集成与验证

重试、状态转换、HTTP 响应与代理失败分别在最终安全字段可用处发事件。不要以抓取全局 log.Println 作为日志源，因为无法保证全局文本安全或结构完整。

## Risks / Trade-offs

- 错误信息含未知形式令牌 → 只记录枚举错误类别，不把 taskError 当任意文本放行依据；用编码/回显凭据测试缓存与导出。
- 事件风暴 → 环形字节/数量双限，锁外采集和省略统计，race 验证并发快照。

## Migration Plan

不改用户任务和设置格式；日志不跨重启保存。导出 JSON 带版本号供人工分析，移除功能无需迁移文件。

先执行针对性用例，再执行全量测试与 MyGo 静态检查；界面变化检查两套主题、960×640 和 125% 缩放。平台服务以 deps 替身测试，原生行为另记真实验证范围。发布后更新 README 与 roadmap；本提案阶段不变更应用版本。
