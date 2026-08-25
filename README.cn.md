# Grix - 移动端与桌面端的 DeepSeek Harness

[English](README.md) | **简体中文**

> Talk to agents like people.

<p align="center">
  <img src="https://github.com/user-attachments/assets/6d42d05e-9448-4160-bb9c-54f59c1c4551" alt="Grix 桌面端与移动端" width="100%">
</p>

Grix 是面向 DeepSeek Harness 和其他代码 Agent 的移动端与桌面端协作软件。像使用 WeChat 或 WhatsApp 一样，把 Agent 加为联系人，像与人沟通一样自然地对话。支持 iOS、Android、Windows、macOS、Linux 和 Web。

在桌面端让 DeepSeek Harness 执行任务，在手机上继续对话，并把消息、文件、进度和授权确认保留在同一个上下文中。

把 DeepSeek Harness、Codex、Claude、Kimi 等 Agent 加为联系人，像找同事一样给它发消息、拉群、@ 它、交代事情和跟进进度。

Agent 会带着对话上下文持续工作，并主动汇报、提问。你可以随时补充要求、暂停任务或接管工作。

先从 DeepSeek Harness 开始。需要时，再添加更多 Agent，组成自己的团队。

## DeepSeek Harness 移动端与桌面端演示

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

- **iOS**：在 [App Store](https://apps.apple.com/cn/app/id6761908445) 下载。
- **Android、Windows、Linux、macOS**：前往 [GitHub Releases](https://github.com/askie/grix/releases/latest) 下载对应平台的安装包。

## 你可以做什么

- 与 Agent 私聊，直接交代任务或继续之前的工作。
- 把人和 Agent 拉进同一个群，共享消息、文件和业务上下文。
- @ 一个 Agent 让它处理事情，并持续查看执行进度。
- 随时补充要求、停止输出、调整分工或由人接管。
- 更换模型或 Agent，让工作沿着已有上下文继续进行。
- 接入语音模型，让 Agent 接听咨询、主动通话或把问题转交给人。

## 支持的 Agent

当前支持 15 种 Agent。你可以先使用已有订阅或本地接入的任意一种，再按需要增加其他 Agent。Grix 也提供 ACP 通用桥，供兼容 Agent 接入同一套消息和协作协议。

| | |
| --- | --- |
| **DeepSeek Harness** | |
| Claude | Codex |
| Kimi | Qwen |
| Cursor | Copilot |
| Pi | OpenCode |
| Kiro | Reasonix |
| CodeWhale | Hermes |
| OpenClaw | Agy |

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

生产环境凭据、云资源定义、区域 overlay、发布台账和内部运维手册不在本仓库中。部署自己的实例时，请基于示例配置创建独立的私有运维仓库或配置目录。

## License

This repository is licensed under the [Apache License 2.0](LICENSE). Bundled third-party components are documented in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
