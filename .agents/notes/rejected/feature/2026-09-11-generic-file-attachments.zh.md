# Agent Note: Generic file attachments in the composer

Status: rejected — every part of it lands in the harness core, and the format step changes the durable session format for every deployment, which does not belong on the Linglong launcher branch.

[English](2026-09-11-generic-file-attachments.md) | 中文

## Problem

输入框只接受文本与位图图片。复制一个非图片文件 —— 从文件管理器，或任何把 `text/uri-list` 写进 X11 剪贴板的应用 —— 再粘贴进输入框，什么都不会发生：打包壳的剪贴板桥读取 selection 后，因路径扩展名不在位图白名单里而拒绝，返回空结果，于是既没有附件也没有任何提示。[durable attachment 决策](../../implemented/feature/2026-07-22-web-multimodal-image-input-and-durable-attachments.zh.md)当时把通用文件、文件选择器与 PDF 明确记为后续项。

## Proposal

在图片之外新增第二种附件类型：不透明文件对象按字节存入既有的内容寻址附件库；线格式增加文件 part；可合并扩展的内容块表增加 `FileBlock`；模型侧投影为一个写明只读路径、媒体类型、字节长度与显示名的文本块（模型用自己的文件系统工具读取该文件）；输入框入口不再只筛图片；壳把复制的文件经剪贴板桥交上来。部署策略约束体积与数量，而不限制媒体类型。

由于文件块承载模型可见内容，方案还携带一次格式步骤：`SESSION_FORMAT_VERSION` 0→1、近恒等的 v0→v1 升级步骤、[版本机制](../../implemented/architecture/2026-08-10-session-log-version-mechanism.zh.md)此前推迟的相邻版本升级链，以及读取侧"拒绝本构建不认识的内容块类型而不是丢弃它"的守卫。

## Why rejected

有六层都按位图图片假设贯通 —— 附件服务定义及其本地 Provider、线格式准入、SDK 协议类型、`ContentBlockMap` 与 llm 投影、前端的媒体类型校验与其整条附件轨道、以及壳的剪贴板桥。只改 launcher 无法交付这个能力：交给输入框的 PDF 字节会被前端的魔数兜底误标、在准入处被拒、或被当作垃圾存下来。

`linglong-dev` 分支承载的是玲珑桌面启动器。从这里改 harness 的持久化会话格式，会牵动**所有**部署的日志格式、重写 144 个已记录会话 fixture、重新生成目录页，并要求它自己的快照与两个 SDK 证据 —— 爆炸半径远超该分支的职责。这个能力应该作为针对 harness 的独立核心提案提出，在那里承担相应的证据。

## Alternatives considered

**只做壳侧：把复制文件的路径以文本交给输入框。** 壳本来就读 `text/uri-list`，因此复制的文件可以粘贴为它的路径，文件仍能通过 agent 自己的读取工具到达模型 —— 不需要附件能力、不改线格式、也不动会话格式。如果目标只是"复制文件后粘贴不再毫无反应"，这是 launcher 范围内可行的选项；之所以没有采用，是因为当时要的是真正的附件，而不是路径文本。

**把文件表示成持久化的文本块，而不是新增块类型。** 完全不需要格式步骤，但历史里就没有结构化附件了：没有文件条、不能重新挂载，只剩一行路径文本。作为半成品被否，因为上面那个壳侧路径选项严格更便宜。
