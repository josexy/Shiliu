# Design

## Context

动机与范围见 [proposal.md](proposal.md)，行为约束见 [spec.md](specs/clipboard-link-prompts/spec.md)。

当前 pasteClipboard 仅用户点击时同步读取，copyURL/copyPath 使用 UI 服务写剪贴板。MyGo `Clipboard.OnChanged` 回调在主线程，开启时不报告已有内容；Windows/Linux 用通知，macOS 注册期间每 250ms 查变更计数；Linux 受桌面能力影响。

实施顺序：无硬依赖；后台托盘存在时只保存最新候选，恢复窗口后提示。

## Goals / Non-Goals

**Goals:** 在现有包边界内实现上述行为，后台工作可取消、可用替身测试；保留旧数据读取与用户现有默认行为。

**Non-Goals:** 默认监听、剪贴板历史、自动下载和云同步。

## Decisions

### 1. 状态与职责

deps 增加 watchClipboard 返回 off，以及受限读取文本服务；newApp 持有 off、enabled generation、最新候选和指纹 LRU。设置 ClipboardPrompts 默认 false，启动读取设置后才注册，不在 view 注册；关闭先使 generation 失效，再 off 和清空原文。

### 2. 核心执行方式

先用 Clipboard.Formats 协商文本，再调用 Clipboard.Read(transfer.Text)，不请求其他格式；读取后立即检查 1 MiB 上限，超过即丢弃，不解析或保留。只识别显式 http/https，不将一般域名或整段文本当 URL。MyGo 0.10.0 无读取前的字节上限参数，因此该限制约束应用保留/解析量，不承诺操作系统读取瞬间的分配上限。返回的 transfer.Data 已复制且可跨 goroutine 读取，解析与去重后台执行。

### 3. 一致性与边界

候选按去用户信息但保留用于区分签名更新的规范 URL 去重；缓存只存带进程随机盐的摘要，LRU 最大 256。已有任务集合与应用 copyURL/copyPath 写入前记录的指纹共同抑制自触发；确认仍提交内存原始 URL 以保留用户主动复制的认证。

### 4. 生命周期与交互

提示使用脱敏预览、添加和忽略动作，不自动创建；隐藏窗口仅替换最新候选，不抢焦点。批次被替换或关闭时清空原始文本，不把剪贴板内容加入日志。仅偏好进入 settings.json。

### 5. 集成与验证

观察不可用时不增加高速轮询替代，保留手动粘贴并在文档记录平台局限；假 deps 覆盖注销后的在途回调和自复制循环。

## Risks / Trade-offs

- 启用监听造成敏感内容留存 → 只处理链接、限制字节、内存候选生命周期和指纹缓存，导出完全排除剪贴板原文。
- 回调注销后仍到达 → 回调先检验 enabled/generation，再决定是否读取。

## Migration Plan

新增设置缺失为 false；去重历史不持久化，重启允许再次提示新复制事件。回滚仅失去提示能力。

先执行针对性用例，再执行全量测试与 MyGo 静态检查；界面变化检查两套主题、960×640 和 125% 缩放。平台服务以 deps 替身测试，原生行为另记真实验证范围。发布后更新 README 与 roadmap；本提案阶段不变更应用版本。
