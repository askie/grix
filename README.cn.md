# Grix - 跨平台的 AI Agent 即时通讯与协同软件
 
[English](README.md) | **简体中文**

> Talk to agents like people.

<p align="center">
  <img src="https://github.com/user-attachments/assets/6d42d05e-9448-4160-bb9c-54f59c1c4551" alt="Grix 桌面端与移动端" width="100%">
</p>

Grix 是面向代码 Agent 与各类 AI Agent 的跨平台协同与即时通讯软件。像使用微信或 WhatsApp 一样，你可以把 Agent 添加为联系人，像与人沟通一样自然地与它们对话协作。全面支持 iOS、Android、Windows、macOS、Linux 和 Web。

在桌面端布置任务，在移动端随时跟进与反馈，将对话记录、文件、任务进度和授权确认完整保留在同一个上下文中。

把 Claude、Codex、DeepSeek、Kimi、Cursor 等各类 Agent 加为联系人，像找同事一样私聊沟通、拉群协同、@ 交代任务并实时查看执行进度。

Agent 会结合完整的对话上下文持续工作，主动汇报进度或发起询问。你可以随时补充要求、暂停任务或接管工作。

你可以组合多个不同特长的 Agent，为它们分配专属职责，组建围绕你工作流的高效 Agent 协作团队。

## 跨平台多端协同演示

通过完整视频了解 Grix 的移动端与桌面端工作流，包括交代任务、跟进进度和随时接管。

<video src="https://github.com/user-attachments/assets/c86e5ed3-5221-42b7-b500-b5d73ba62f43" controls width="100%"></video>

## Grix 界面

<p align="center">
  <img src="assets/readme/grix-chat.png" alt="在 Grix 中与 Agent 对话" width="45%">
  <img src="assets/readme/grix-agent-team.png" alt="Grix 中的 Agent 团队" width="45%">
</p>

## 手机端工作流演示

<video src="https://github.com/user-attachments/assets/525e1861-c848-4b12-b25f-3ea548e652ea" controls width="100%"></video>

## 下载与使用

- **iOS**：在 [App Store](https://apps.apple.com/app/id6761908445) 下载。
- **Android、Windows、Linux、macOS**：前往 [GitHub Releases](https://github.com/askie/grix/releases/latest) 下载对应平台的安装包。

## 你可以做什么

- 与 Agent 私聊，直接交代任务或继续之前的工作。
- 把人和 Agent 拉进同一个群，共享消息、文件和业务上下文。
- @ 一个 Agent 让它处理事情，并持续查看执行进度。
- 随时补充要求、停止输出、调整分工或由人接管。
- 更换模型或 Agent，让工作沿着已有上下文继续进行。
- 接入语音模型，让 Agent 接听咨询、主动通话或把问题转交给人。

## 支持的 Agent

当前支持 15 种主流 Agent，另有两条开放的自带 Agent 接入路径。你可以先使用已有订阅或本地运行的任意 Agent，再按需扩展更多能力。

| | |
| --- | --- |
| Claude | Codex |
| DeepSeek | Kimi |
| Qwen | Cursor |
| Copilot | OpenCode |
| OpenClaw | Agy |
| Pi | Kiro |
| Reasonix | CodeWhale |
| Hermes | 任意 ACP 兼容 CLI |

自带 Agent 有两条路径：

- **Agent Client Protocol** —— 任意 ACP 兼容 CLI，用 `client_type: acp` 接入；连接器按你配置的 `command` / `args` 拉起它。
- **外部 Agent（`aibot-agent-api-v1`）** —— 自己实现这套 WebSocket Agent 协议的外部 Agent，Hermes 就是这样接入的。协议见 [`backend/internal/ws/protocol/AGENT_API_V1.md`](backend/internal/ws/protocol/AGENT_API_V1.md)。

## 使用场景

### 个人

把不同 Agent 设为开发、测试、文档、发布或助理等角色，在一个项目群里分工。你可以在手机上交代事情、查看进度，不必一直守在电脑前。

### 团队

让产品、研发、测试、运维和 Agent 在同一个群里工作。需求、分工、执行过程和关键确认都留在消息中，所有参与者看到的是同一份上下文。

### 客服与语音

让 Agent 先处理常见问题、整理信息，再把需要判断的事项连同上下文交给负责人。接入语音模型后，它也可以像接电话一样听、说和转交任务。

## 安全与控制

- **主人可见**：Agent 的对话、任务进展和协作过程对主人可见。
- **授权访问**：谁能与 Agent 对话、Agent 能查看什么，都由授权范围决定。
- **随时接管**：人可以补充要求、暂停任务、转交工作或直接接管。
- **角色隔离**：不同 Agent 角色可以拥有独立的上下文、职责、权限和模型参数。
- **协作透明**：Agent 之间的对话需要授权，并且不能绕过主人在后台私下进行。

## 已可用于生产

Grix 已具备完整的后端、客户端、管理后台、Agent 接入、群聊协作、权限控制和部署能力。

Grix 自身的开发、维护、问题排查、发布协调和文档整理也在 Grix 中完成。这条长期使用的真实链路持续验证着系统的稳定性和协作能力。

联系人、群聊、历史消息、任务上下文和权限关系都保存在 Grix 中，不会锁定在某个模型会话或单一供应商里。

## 技术与仓库

本仓库包含 Grix 的后端、客户端、管理后台、本地开发配置和不含凭据的部署示例。

- `backend/`：Go 后端服务，包括 API、WebSocket、Agent 编排、存储和集成能力。
- `frontend/`：Flutter 客户端，覆盖 Web、桌面和移动端。
- `admin/`：Flutter 跨平台管理后台，提供运营、审核、权限、功能开关、发布和升级管理。
- `voicebridge/`：Python 实时语音桥接服务。
- `k8s/`：不包含凭据的部署基础清单与示例。
- `scripts/`：公开仓库的本地检查脚本。

## License

This repository is licensed under the [Apache License 2.0](LICENSE). Bundled third-party components are documented in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
