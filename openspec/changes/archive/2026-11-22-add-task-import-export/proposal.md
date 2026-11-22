# Proposal

## Why

当前批量添加依赖手动粘贴，也没有安全分享任务清单的入口。TXT 导入和脱敏清单导出能够减少重复操作，并复用已有解析与去重规则。

来源：按需穿插，对应 [迭代计划](../../../../docs/roadmap.md)。

## What Changes

- 从 UTF-8 TXT（含可选 BOM）导入链接，预览有效、重复和无效条目后确认添加。
- 导出所选任务或全部任务的版本化 JSON 清单，包含名称、脱敏 URL、大小和状态。
- 清单移除 URL 用户信息、整个查询串、片段及请求头，不包含代理凭据与本地绝对路径；提示脱敏链接可能无法直接重用。

本次不包含：完整任务备份、JSON 清单导入、部分文件/凭据迁移和导出后自动上传。

## Capabilities

### New Capabilities

- `task-import-export`：批量导入与导出的用户行为、兼容性和验收约束。

### Modified Capabilities

无。生成提案时主规格目录没有已有 capability，本 change 新增上述独立规格，不重建第一阶段基线。

## Impact

新增 `internal/tasklist` 负责有界解析与导出 DTO，`internal/app/deps.go` 和导入导出界面接入文件对话框，复用 `internal/download` 添加接口。

实施依赖：无硬依赖；拖放的 TXT 解析复用本 change。

实现完成后更新 README 的功能与限制，以及 roadmap 的对应进度；本轮仅产出规划，不勾选已实现项。
