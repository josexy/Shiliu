# Design

## Context

动机与范围见 [proposal.md](proposal.md)，行为约束见 [spec.md](specs/drag-drop-downloads/spec.md)。

MyGo 0.10.0 `ui/drop.go` 提供 DroppedFiles；外部文本/URI 使用 `ui.DropData` 和 transfer.DropOptions（见依赖 docs/ui/drag-and-drop.md），不能用仅限窗口内部值的 ui.Drop 处理浏览器链接。当前 app 无拖放入口，导入能力提供 TXT 解析。

实施顺序：硬依赖 `add-task-import-export` 的 TXT 解析；分类规则属于可选集成。

## Goals / Non-Goals

**Goals:** 在现有包边界内实现上述行为，后台工作可取消、可用替身测试；保留旧数据读取与用户现有默认行为。

**Non-Goals:** 拖入普通文件上传、递归扫描文件夹、HTML 网页抓取和平台快捷方式文件解析。

## Decisions

### 1. 状态与职责

依赖 add-task-import-export；添加区声明 transfer.URIList、Text 与本地文件格式，优先 URI 列表，其次文本，再按文件路径处理，避免同一投放多种表现重复。只认 .txt 本地文件，.url/.webloc 暂不支持，不读取 HTML 或递归目录。

### 2. 核心执行方式

回调在当前事件内取得需要的普通字符串/路径，转换为一次性的 batch ID；禁止把 Element/parts/ui.Context 保存到后台。外部传输的异步数据请求按框架生命周期完成后复制数据，带 generation 交给后台文件读取。

### 3. 一致性与边界

复用 tasklist 的有界解析和预览确认模型；支持混合多个 TXT，合并后的批次仍遵守总量限制。纯文本与 URI 解析使用同样候选去重，错误只显示脱敏内容。

### 4. 生命周期与交互

通过明确待处理队列或动作回调提交候选，不能在每次 view 构建无条件执行文件 I/O。新批次替代、取消、退出时取消旧 context；busy/generation 让晚结果只被丢弃。

### 5. 集成与验证

用 ui.Tester 验证悬停/投放/多帧/焦点；原生浏览器和 Explorer/Finder 文件投放按平台分别验收，不用内存 Tester 结果推断系统兼容。

## Risks / Trade-offs

- 原生数据形式与浏览器不同 → 记录实际格式组合，未识别时提供粘贴/TXT 导入退化。
- 文件符号链接或不可读路径 → 只读有界普通文件，失败按批次反馈，不执行目标。

## Migration Plan

无记录迁移；新增 UI 入口，功能关闭或不支持时原有粘贴/导入不受影响。

先执行针对性用例，再执行全量测试与 MyGo 静态检查；界面变化检查两套主题、960×640 和 125% 缩放。平台服务以 deps 替身测试，原生行为另记真实验证范围。发布后更新 README 与 roadmap；本提案阶段不变更应用版本。
