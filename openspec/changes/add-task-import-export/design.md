# Design

## Context

动机与范围见 [proposal.md](proposal.md)，行为约束见 [spec.md](specs/task-import-export/spec.md)。

`NormalizeURLs` 返回已去用户信息的显示 URL，真正 Add/parseTargets 在内存保留认证；不能用显示 URL 作为导入提交值。现有 deps 只有 pickFolder，Store.writeJSON 已有原子写模式但不应把用户导出 DTO 与 tasks.json 混用。

实施顺序：无硬依赖；拖放的 TXT 解析复用本 change。

## Goals / Non-Goals

**Goals:** 在现有包边界内实现上述行为，后台工作可取消、可用替身测试；保留旧数据读取与用户现有默认行为。

**Non-Goals:** 完整任务备份、JSON 清单导入、部分文件/凭据迁移和导出后自动上传。

## Decisions

### 1. 状态与职责

新增无 UI 依赖的 internal/tasklist，进行 UTF-8/BOM、总字节、单 token 和候选数量检查。保留原始候选仅在内存提交给 Add；预览只返回脱敏显示与索引。分隔规则与既有 parseTargets 保持一致，必要时提取可共享解析层，不复制两个不同规范化器。

### 2. 核心执行方式

单次最大 10 MiB/一万候选/16 KiB token；超限或非法编码整批拒绝。预览统计重复，确认时再由引擎按当前状态去重。逐条创建失败汇总，不回滚已经成功的不同任务；重按确认通过 busy/generation 防重复。

### 3. 一致性与边界

导出使用专属 allowlist DTO version=1，字段 fileName/url/total/done/state，url 复用 DiagnosticURL。排除 path、headers、proxy、用户信息及查询串；这是分享清单而非恢复备份，默认不提供 JSON 导入。直接导出 Record 会泄露路径/后续敏感字段，故不用。

### 4. 生命周期与交互

通过 deps 增加 open/save 文件选择，读取和写入后台执行，generation 管理取消；用户选择目标后同目录临时文件写入并替换，取消/错误清理本次临时文件。UI 预览、结果汇总保持中文且可滚动。

## Risks / Trade-offs

- 脱敏 URL 丢失签名不可直接重用 → 导出确认明确说明不是备份，不承诺往返导入。
- 大量输入阻塞 → 解析和创建在可取消后台批次执行，错误列表截断且保留数量。

## Migration Plan

无 settings/tasks 格式变更；新导出格式固定 version=1，未来升级新增兼容字段。取消功能只移除入口，不影响已有任务或原 TXT。

先执行针对性用例，再执行全量测试与 MyGo 静态检查；界面变化检查两套主题、960×640 和 125% 缩放。平台服务以 deps 替身测试，原生行为另记真实验证范围。发布后更新 README 与 roadmap；本提案阶段不变更应用版本。
