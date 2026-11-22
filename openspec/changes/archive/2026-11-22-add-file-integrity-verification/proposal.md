# Proposal

## Why

当前完成状态只确认传输和文件落盘，不能对照用户提供的摘要确认内容。后台 SHA-256 校验让传输完成与内容匹配成为可区分、可取消的结果。

来源：第三阶段 · roadmap 8，对应 [迭代计划](../../../../docs/roadmap.md)。

## What Changes

- 高级新建/任务详情允许输入和修改预期 SHA-256，校验 64 位十六进制格式。
- 传输完成后后台校验，单独显示待校验、校验中、匹配、不匹配、已取消或错误。
- 支持取消和重新校验，文件缺失/变化或任务删除时拒绝过期结果；持久化摘要及必要结果元信息。

本次不包含：数字签名验证、自动查询摘要和自动删除/重新下载不匹配的文件。

## Capabilities

### New Capabilities

- `file-integrity-verification`：文件完整性校验的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

`internal/download` 校验工作任务与快照、`internal/store`、`internal/app/view_advanced.go`、`view_details.go`；使用标准库 `crypto/sha256`。

实施依赖：无硬依赖；多连接分片完成后可复用本能力。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
