# 剩余迭代 OpenSpec 索引

生成日期：2026-11-22。来源：[docs/roadmap.md](../docs/roadmap.md)。

第一阶段已经完成，不重复生成提案。按用户确认，暂不生成浏览器扩展（第 11 项）；BT/磁力、HLS 等暂不优先方向也不在本轮范围。其余 13 项均拆为独立 change，使用 spec-driven 工作流。

每个 change 都包含 proposal、增量 specs、design 和 tasks。规划齐全不代表功能已经实现，实施进度以各 change 的 tasks 为准；存在前置 change 的项目需先完成对应依赖。各提案使用 Go 1.27.3 / MyGo 0.10.0 作为规划依据，当前依赖以 go.mod 为准。

实施更新（2026-11-22）：用户选定的任务导入导出、单任务限速、文件校验、失效链接更新和多连接下载已完成；托盘代码已完成，Windows 原生验收按用户安排暂缓，生产入口保持禁用。六项共完成 60/61 个任务，其余七项仍为待实施规划。

## 第二阶段

| roadmap 项 | Change | 前置条件 |
| --- | --- | --- |
| 4. 托盘与后台下载 | [add-tray-background-downloads](changes/add-tray-background-downloads/proposal.md) | 无硬依赖 |
| 5. 队列优先级 | [add-download-queue-priority](changes/add-download-queue-priority/proposal.md) | 无硬依赖 |
| 6. 计划下载 | [add-download-scheduling](changes/add-download-scheduling/proposal.md) | 队列优先级 |

按托盘、队列优先级、计划下载顺序推进。托盘先完成 Windows 实测，再按平台开放；未经原生验证的环境明确退化，不能把 API 存在视为功能已验证。计划调度只在应用运行时生效，手动暂停优先。

## 第三阶段

| roadmap 项 | Change | 前置条件 |
| --- | --- | --- |
| 7. 单任务限速 | [add-per-task-rate-limits](changes/add-per-task-rate-limits/proposal.md) | 无硬依赖；推荐第二阶段稳定后实施 |
| 8. 文件完整性校验 | [add-file-integrity-verification](changes/add-file-integrity-verification/proposal.md) | 无硬依赖；推荐第二阶段稳定后实施 |
| 9. 更新失效链接 | [add-expired-link-replacement](changes/add-expired-link-replacement/proposal.md) | 无硬依赖；推荐第二阶段稳定后实施 |
| 10. 多连接分片下载 | [add-multi-connection-downloads](changes/add-multi-connection-downloads/proposal.md) | 单任务限速、文件校验、失效链接更新 |

多连接默认关闭，先保留单连接路径；分片正确性、持久化和取消是验收重点，不承诺吞吐提升。失效链接更新使用保守资源判断与已有前缀比较，额外验证流量和不能复用的情况写入设计。

## 可按需穿插

| 功能 | Change | 前置条件 |
| --- | --- | --- |
| 批量导入与导出 | [add-task-import-export](changes/add-task-import-export/proposal.md) | 无硬依赖 |
| 分类与目录规则 | [add-download-directory-rules](changes/add-download-directory-rules/proposal.md) | 无硬依赖 |
| 拖放添加 | [add-drag-drop-downloads](changes/add-drag-drop-downloads/proposal.md) | 批量导入与导出的 TXT 解析 |
| 剪贴板链接提示 | [add-clipboard-link-prompts](changes/add-clipboard-link-prompts/proposal.md) | 无硬依赖 |
| 诊断日志导出 | [add-diagnostic-log-export](changes/add-diagnostic-log-export/proposal.md) | 无硬依赖 |
| 速度曲线 | [add-download-speed-history](changes/add-download-speed-history/proposal.md) | 无硬依赖 |

分类目录由统一新建流程应用；剪贴板提示默认关闭；导出任务清单与诊断事件均去除认证信息。速度历史仅驻留内存。并行实施涉及相同 manager/Record/高级表单字段时，需要协调字段和测试替身；这不授权自动启动多个 change 的实现。

## 审阅与实施

进入某个 change 后依次阅读：
- `proposal.md`：范围、动机、capability 和依赖。
- `specs/<capability>/spec.md`：可验收行为、边界及异常场景。
- `design.md`：当前代码依据、设计取舍、兼容及回滚。
- `tasks.md`：分组实施项，每项带验证方式，组内同步测试和文档。

从仓库根目录查看和校验：

```sh
openspec list --json
openspec status --change add-tray-background-downloads
openspec validate --changes --strict
```

审阅后明确使用 `openspec-apply-change` 技能并指定 change 才进入实施。实现验收后再勾选 roadmap 对应项。已实施变更的自动化检查与交叉构建结果记录在对应 tasks 中；交叉构建不代表原生行为通过验收，托盘原生验收仍待完成。
