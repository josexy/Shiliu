# Design

## Context

动机与范围见 [proposal.md](proposal.md)，行为约束见 [spec.md](specs/multi-connection-downloads/spec.md)。

fetch 当前单连接顺序 append，Restore 直接把 `.part` 文件长度当 Done，partMeta 写入未版本化且非原子；这不能表示稀疏区间。现有 path reservation 只含最终文件、.part、.part.meta。Manager.max 是任务数，并不是连接数。

实施顺序：硬依赖 `add-per-task-rate-limits`、`add-file-integrity-verification`、`add-expired-link-replacement`；建议第二阶段稳定后实施。

## Goals / Non-Goals

**Goals:** 在现有包边界内实现上述行为，后台工作可取消、可用替身测试；保留旧数据读取与用户现有默认行为。

**Non-Goals:** 保证提速、镜像多源、BT/HLS 和无限连接数。

## Decisions

### 1. 状态与职责

前置完成单任务限速、文件校验、失效链接更新。初版每任务连接默认 1、上限 8，全局连接默认 16、上限 64；有效 206+强 ETag+已知长度且文件至少 8 MiB 才选分片，小文件保留单连接。探测采用 GET 小范围并立即关闭，不把 HEAD 或 Accept-Ranges 当证据。

### 2. 核心执行方式

新增 transfer 策略和连接额度调度器：单连接、探测、分片和重试均一次获取一个可取消额度，不批量持有等待；FIFO 轮转等待者防饥饿。任务并发继续由 Manager 管理。降连接上限只限制新授予，既有连接排空后达到新限额。

### 3. 一致性与边界

分片按固定 4 MiB 区间调度（末片截短），各 worker 用 WriteAt 写独立 `.part.segments` 数据文件，不能把稀疏内容放旧 `.part`。`.part.meta` v2 标记 mode、资源版本、总长度、已完成区间；只有数据 Sync 成功才原子提交完成区间。崩溃最多重下未提交片段，不能靠文件长度恢复。

### 4. 生命周期与交互

扩展 taskPaths/冲突检查与删除覆盖 `.part.segments` 及元信息临时路径；创建、服务器更名、移除和恢复都使用统一预留集，禁止占用其他任务的最终或临时文件。旧连续 .part 默认继续单连接，不在迁移时预分配扩展它。

### 5. 集成与验证

每个请求带 If-Range 和 identity，验证 Content-Range 起止、Content-Length/实际读取量、总长度、强 ETag、最终资源源和路径。短读重试该片，越界/版本不一致停止全部 workers 并等待关闭，清理不可信分片后从零单连接；一次执行最多一次自动策略回退，避免循环。

### 6. 数据与性能

所有读取共享现有 ClientFactory、认证剥离、全局/任务 bucket 和 stall/cancel 机制。Done 统计已接收且不重复的字节，持久 Done 只认已提交完整片段；速度按实际网络字节计算，重试可有额外流量但不虚增进度。

### 7. 完成发布

完成前停止片段修改、检查区间无缝覆盖、文件 Sync 和长度，rename `.part.segments` 到保留最终路径，清理元信息，只触发一次完成。若设置 SHA-256，交给独立校验流程，不能把覆盖检查当成摘要匹配。

## Risks / Trade-offs

- 降级旧版本误把稀疏文件当完整前缀 → 稀疏数据使用独立路径；旧版本无 .part 时只能完整重下，不复用该文件，降级需先完成或清理分片任务。
- 磁盘预分配失败或元信息损坏 → 明确失败并保留可诊断状态，恢复对未知模式保守从零。
- 多个连接反而降低吞吐 → 默认单连接，记录本地受限/不限速场景的吞吐、连接数和 CPU，不设强制提速承诺。

## Migration Plan

记录新增模式与连接数，旧无版本 meta 仅作单连接读取。分片 meta 使用 schemaVersion=2 和原子写；未知版本拒绝复用。降级前完成/取消分片任务，独立 payload 避免旧版本错误续传；移除操作须清理新增路径。

先执行针对性用例，再执行全量测试与 MyGo 静态检查；界面变化检查两套主题、960×640 和 125% 缩放。平台服务以 deps 替身测试，原生行为另记真实验证范围。发布后更新 README 与 roadmap；本提案阶段不变更应用版本。
