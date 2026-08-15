# Grix - DeepSeek Harness for Mobile and Desktop

**English** | [简体中文](README.cn.md)

> Talk to agents like people.

Grix is a mobile and desktop app for DeepSeek Harness and other coding agents. Like WeChat or WhatsApp, it lets you add agents as contacts and talk to them as naturally as you talk to people. It is available on iOS, Android, Windows, macOS, Linux, and the Web.

Start work with DeepSeek Harness on your desktop, continue from your phone, and keep conversations, files, progress, and approvals in one shared thread.

Add DeepSeek Harness, Codex, Claude, Kimi, and other agents as contacts. Message them, invite them to groups, @mention them, assign work, and follow their progress just as you would with a colleague.

Agents keep working with the context of your conversations and proactively report progress or ask questions. You can add instructions, pause a task, or take over at any time.

Start with DeepSeek Harness. Add more agents when you need them and build a team around your work.

## DeepSeek Harness on Mobile and Desktop

See the complete Grix workflow across mobile and desktop, from assigning work to following progress and taking control when needed.

<video src="https://github.com/user-attachments/assets/8885112a-8df0-482d-a915-ae0c1cee05a4" controls width="100%"></video>

## Grix at a Glance

<p align="center">
  <img src="assets/readme/grix-chat.png" alt="Grix conversations with agents" width="45%">
  <img src="assets/readme/grix-agent-team.png" alt="A team of agents in Grix" width="45%">
</p>

## Mobile Workflow Demo

<video src="https://github.com/user-attachments/assets/525e1861-c848-4b12-b25f-3ea548e652ea" controls width="100%"></video>

## Download and Start

- **iOS**: Download Grix from the [App Store](https://apps.apple.com/cn/app/id6761908445).
- **Android, Windows, Linux, and macOS**: Download the installer for your platform from [GitHub Releases](https://github.com/askie/grix/releases/latest).

## What You Can Do

- Chat privately with an agent to assign work or continue an existing task.
- Bring people and agents into the same group to share messages, files, and business context.
- @mention an agent to give it work and follow its progress as it runs.
- Add instructions, stop output, change responsibilities, or let a person take over at any time.
- Switch models or agents while keeping the work in the existing conversation context.
- Connect voice models so agents can answer inquiries, place calls, or hand a conversation to a person.

## Supported Agents

Grix currently supports 15 agents. Start with any agent you already subscribe to or run locally, then add others as needed. Grix also provides a general ACP bridge so compatible agents can use the same messaging and collaboration protocol.

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

## Use Cases

### Personal

Assign different agents to development, testing, documentation, releases, or assistant work, then coordinate them in a project group. Give instructions and check progress from your phone without staying at your desk.

### Teams

Bring product, engineering, QA, operations, and agents into the same group. Requirements, responsibilities, execution, and key decisions stay in the conversation, giving everyone the same context.

### Customer Support and Voice

Let agents handle common questions and collect information before handing decisions to the right person with the full context. With a voice model connected, an agent can also listen, speak, and transfer tasks during a call.

## Safety and Control

- **Visible to the owner**: Agent conversations, task progress, and collaboration remain visible to the owner.
- **Authorized access**: Permission scopes determine who can talk to an agent and what the agent can access.
- **Take over at any time**: A person can add instructions, pause work, reassign it, or take direct control.
- **Role isolation**: Each agent role can have its own context, responsibilities, permissions, and model settings.
- **Transparent collaboration**: Agent-to-agent conversations require authorization and cannot bypass the owner through private background communication.

## Production Ready

Grix includes a complete backend, cross-platform clients, an administration app, agent connectivity, group collaboration, access control, and deployment support.

Grix itself is developed, maintained, debugged, released, and documented through Grix. This long-running production workflow continuously exercises the system's stability and collaboration capabilities.

Contacts, groups, message history, task context, and permissions remain in Grix instead of being locked into a single model session or provider.

## Technology and Repository

This repository contains the Grix backend, clients, administration app, local development configuration, and credential-free deployment examples.

- `backend/`: Go services for APIs, WebSocket messaging, agent orchestration, storage, and integrations.
- `frontend/`: Flutter clients for Web, desktop, and mobile platforms.
- `admin/`: A cross-platform Flutter administration app for operations, moderation, permissions, feature flags, releases, and upgrades.
- `voicebridge/`: A Python service for real-time voice bridging.
- `k8s/`: Credential-free base manifests and deployment examples.
- `scripts/`: Local validation scripts for the public repository.

Production credentials, cloud resource definitions, regional overlays, release ledgers, and internal operations manuals are not included. When deploying your own instance, create a separate private operations repository or configuration directory based on the examples.

## License

This repository is licensed under the [Apache License 2.0](LICENSE). Bundled third-party components are documented in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
