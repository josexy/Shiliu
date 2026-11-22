# Proposal

## Why

链接过期时当前只能另建任务或对相同链接补填请求头，无法为已有记录更换地址。保留身份和目标路径、保守核验部分数据可避免重新整理任务以及错误拼接文件。

来源：第三阶段 · roadmap 9，对应 [迭代计划](../../../../docs/roadmap.md)。

## What Changes

- 为已停止的暂停/失败任务提供更新链接入口，保持 ID、路径、创建时间和已有任务配置。
- 更换地址后先验证新旧资源版本，仅在可信验证器、总长度和 Range 响应一致时继续；不能确认则从零下载。
- 新认证和请求头由用户重新提供，不向新源自动沿用旧凭据；错误和持久化继续脱敏。

本次不包含：自动刷新签名 URL、抓取网页链接、跨域资源身份推断和更新正在运行的任务。

## Capabilities

### New Capabilities

- `expired-link-replacement`：更新失效链接的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

`internal/download/request.go`、`fetch.go`、`meta.go`、`manager.go`、记录与 UI 操作；扩展现有认证、Range 和版本变化回归测试。

实施依赖：无硬依赖；建议在多连接分片前实施，再由分片 change 扩展部分数据迁移。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
