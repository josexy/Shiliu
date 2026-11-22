# Design

## Context

动机与范围见 [proposal.md](proposal.md)，行为约束见 [spec.md](specs/download-directory-rules/spec.md)。

Settings 目前只有 DownloadDir；AddTask 的 Dir 为空才用默认值，advancedState 打开时直接预填全局目录。因此增加规则必须区分“默认显示值”与用户明确覆盖，不能让高级入口始终绕过规则。

实施顺序：无硬依赖；批量导入、拖放与剪贴板确认添加均走相同的新建路径。

## Goals / Non-Goals

**Goals:** 在现有包边界内实现上述行为，后台工作可取消、可用替身测试；保留旧数据读取与用户现有默认行为。

**Non-Goals:** 移动已有文件、按文件内容识别类型、重定向后改目录和正则表达式规则。

## Decisions

### 1. 状态与职责

settings 增加有序 DirectoryRules：ID、Enabled、Kind、Pattern、Dir。确定规则模型和规范化放 internal/settings，纯匹配放新增 internal/download/directory_rules.go 或独立 internal/rules；本规划选 internal/rules，使 settings/download 均不依赖 app 或 mygo。

实现时 settings 保留 DirectoryRule 类型别名和规范化入口，共享模型及校验细节置于 internal/rules；引擎只依赖 rules，避免经 settings 引入 proxy 而与现有代理集成测试形成循环依赖。持久格式和上述职责不变。

### 2. 核心执行方式

匹配用创建时 custom filename（若有）否则 URL 推断名的最后后缀；扩展名统一小写并去前导点。域名用 url.Hostname，IDNA 通过现有 x/net/idna 规范化，小写、去末尾点；*.domain 使用标签边界且不含根域名。拒绝路径、端口和任意正则模式。

### 3. 一致性与边界

新建使用显式 DirOverride 标记。高级表单新增“使用分类规则/自定义目录”控制，默认目录只作预览，不算 override；快速添加亦调用同一目录选择器。相同链接补填认证仍沿用旧路径，不能因规则更新迁移任务。

### 4. 生命周期与交互

引擎拿规则快照在任务创建阶段解析目录，并复用可写校验及最终/.part/.part.meta 预留；不得在锁内进行慢文件 I/O。响应改名只修改 basename，不改变已选择目录。规则编辑时可验证格式并试算匹配，创建时再次检查目录。

### 5. 集成与验证

有序规则列表使用稳定 ID 作 c.Key，字段持久绑定，删除/移动通过操作回调；工作区现有 theme/style 统一控制选中、禁用和错误颜色。

## Risks / Trade-offs

- 规则表单默认值意外抢占单任务选择 → 明确 override 标记与预览测试。
- 域名后缀误匹配 evil-example.com → 标签边界和 IDNA/IPv6 回归测试。

## Migration Plan

旧设置缺失规则即空列表，行为不变；字段未知可忽略。编辑规则不扫描/移动现有文件。回滚版本忽略规则并用默认目录，新任务行为变化须在说明中指出。

先执行针对性用例，再执行全量测试与 MyGo 静态检查；界面变化检查两套主题、960×640 和 125% 缩放。平台服务以 deps 替身测试，原生行为另记真实验证范围。发布后更新 README 与 roadmap；本提案阶段不变更应用版本。
