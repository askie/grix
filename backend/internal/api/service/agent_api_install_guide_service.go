package service

import (
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/model"
)

const (
	AgentAPIInstallGuideModeText = "text"
	AgentAPIInstallGuideModeLink = "link"
)

// AgentAPIInstallGuideCatalogResp is the API-driven catalog used by the app's
// agent connection setup page. CopyTemplate holds a complete, ready-to-run task
// the owner pastes into an AI agent (Claude, Codex, ...) so it performs the
// setup on the machine that will host this Grix Agent.
//
// Templates may contain the placeholders {{agent_name}}, {{agent_id}},
// {{api_key}} and {{api_endpoint}}; the client substitutes them before copying.
type AgentAPIInstallGuideCatalogResp struct {
	DefaultType string                     `json:"default_type"`
	List        []AgentAPIInstallGuideResp `json:"list"`
}

type AgentAPIInstallGuideResp struct {
	Type            string `json:"type"`
	Label           string `json:"label"`
	Intro           string `json:"intro"`
	ContentMode     string `json:"content_mode"`
	ContentTemplate string `json:"content_template,omitempty"`
	LinkLabel       string `json:"link_label,omitempty"`
	LinkURL         string `json:"link_url,omitempty"`
	CopyTemplate    string `json:"copy_template,omitempty"`
}

// localizedGuideText maps app-language code → text. zh and en are the
// reference texts; the other nine app languages live in the
// agent_api_install_guide_*_i18n.go files. Resolution order: lang → en → zh.
type localizedGuideText map[string]string

// zhEn is the shorthand for texts that only exist in the two reference
// languages (labels, commands): every other app language falls back to en.
func zhEn(zh, en string) localizedGuideText {
	return localizedGuideText{"zh": zh, "en": en}
}

type agentAPIInstallGuideDef struct {
	Type            string
	Label           localizedGuideText
	Intro           localizedGuideText
	ContentMode     string
	ContentTemplate localizedGuideText
	LinkLabel       localizedGuideText
	LinkURL         string
	CopyTemplate    localizedGuideText
}

const (
	connectorInstallCommand = "npm install -g grix-connector"
	deepseekInstallCommand  = "npm i -g pnpm\nnpm i -g @deepseek-ai/dsh"
)

// connectorTaskZh / connectorTaskEn drive every client_type served by
// grix-connector. Only the required CLI name and the client_type value differ,
// so the task itself stays identical across agents — the connector resolves the
// spawn command from client_type alone.
//
// The task deliberately stops at "merge the entry / apply / verify" and defers
// the rest to the connector README, which ships inside the npm package and is
// therefore readable by the agent performing the install.
const connectorTaskZh = `把这个 Grix Agent 接入本机的 grix-connector。请按顺序执行，完成后回报结果。

前提：本机已安装 Node.js 18+，以及 %s 并且已经登录。如果缺少其中之一，先告诉我，不要自行安装。

1) 安装连接器（已安装则升级到最新版）
%s

2) 把下面这条配置合并进 ~/.grix/config/agents.json
- 文件不存在 → 创建它，内容为 {"agents": [下面这条]}
- 文件已存在 → 用脚本读出 JSON，在 agents 数组里查找 agent_id 为 {{agent_id}} 的条目：找到就整条替换，没找到就追加。
  ⛔ 其余条目必须原样保留。禁止覆盖整个文件，禁止删改其他 Agent。

%s

3) 让配置生效
先执行 grix-connector status 判断：
- daemon 未运行 → grix-connector start
- daemon 已在运行 → grix-connector reload（热加载，不会打断其他 Agent 的会话）
⛔ 不要用 restart 来添加 Agent，它会重连所有 Agent、打断正在进行的对话。

4) 验证（必做）
grix-connector status 只报守护进程状态，不会列出 Agent。要确认这个 Agent 真的连上了，查本机的 admin 接口（daemon 起来后可能要等几秒）：
curl -s http://127.0.0.1:19580/api/agents
输出里应出现 "name":"{{agent_name}}" 且 "alive":true。（19580 是默认端口；若改过，真实端口写在 ~/.grix/data/admin-port。）

如果没连上，看 ~/.grix/log/ 下最新的日志。常见原因只有三个：%s 不在 PATH、CLI 没登录、api_key 复制不全。

更多细节见 grix-connector 的 README（安装后位于 $(npm root -g)/grix-connector/README.md）的 "Adding an agent to an existing setup" 一节。

⚠️ api_key 是一次性凭据，只写入 ~/.grix/config/agents.json，不要打印到日志、不要提交到 git。`

const connectorTaskEn = `Connect this Grix Agent to grix-connector on this machine. Follow the steps in order and report back when done.

Prerequisites: Node.js 18+ and %s, installed and logged in on this machine. If either is missing, tell me first — do not install it yourself.

1) Install the connector (upgrades to the latest version if already installed)
%s

2) Merge the entry below into ~/.grix/config/agents.json
- file does not exist -> create it as {"agents": [the entry below]}
- file already exists -> read it as JSON, look through the agents array for the entry whose agent_id is {{agent_id}}: replace it if found, append if not.
  Every other entry must be left untouched. Never overwrite the whole file, never drop another Agent.

%s

3) Apply the change
Run grix-connector status first:
- daemon not running -> grix-connector start
- daemon already running -> grix-connector reload (hot-loads the new Agent, leaves running Agents untouched)
Do not use restart to add an Agent — it reconnects everything and interrupts live conversations.

4) Verify (required)
grix-connector status only reports the daemon, it does not list agents. To confirm this Agent is actually connected, query the local admin API (give the daemon a few seconds after it starts):
curl -s http://127.0.0.1:19580/api/agents
The output must contain "name":"{{agent_name}}" with "alive":true. (19580 is the default port; if it was changed, the real one is in ~/.grix/data/admin-port.)

If it never connects, read the newest log under ~/.grix/log/. In practice it is one of three things: %s is not on PATH, the CLI is not logged in, or the api_key was truncated when copied.

For the details, see the "Adding an agent to an existing setup" section of the grix-connector README, which ships with the package at $(npm root -g)/grix-connector/README.md.

The api_key is a one-time secret: write it into ~/.grix/config/agents.json and nowhere else. Do not echo it into logs or commit it to git.`

// connectorConfigEntry is the exact agents.json entry shape validated by the
// connector: name / ws_url / agent_id / api_key / client_type are all required,
// everything else is optional and resolved from client_type.
func connectorConfigEntry(clientType string) string {
	return fmt.Sprintf(`{
  "name": "{{agent_name}}",
  "ws_url": "{{api_endpoint}}",
  "agent_id": "{{agent_id}}",
  "api_key": "{{api_key}}",
  "client_type": %q
}`, clientType)
}

// connectorGuide assembles the full language matrix for one grix-connector
// client type. introName is the product name embedded in the one-line intro
// (usually equal to cliDisplay; qwen differs). binZh/binEn/binNeutral are the
// binary names shown inside the CLI phrase — they only differ for copilot,
// whose "or" conjunction is language-specific.
func connectorGuide(clientType, label, introName, cliDisplay, binZh, binEn, binNeutral string) agentAPIInstallGuideDef {
	entry := connectorConfigEntry(clientType)
	intro := localizedGuideText{}
	for lang, pattern := range connectorIntroPatterns {
		intro[lang] = fmt.Sprintf(pattern, introName)
	}
	task := localizedGuideText{}
	for lang, tmpl := range connectorTasks {
		cli := cliPhrase(lang, cliDisplay, binZh, binEn, binNeutral)
		task[lang] = fmt.Sprintf(tmpl, cli, connectorInstallCommand, entry, cli)
	}
	return agentAPIInstallGuideDef{
		Type:            clientType,
		Label:           zhEn(label, label),
		Intro:           intro,
		ContentMode:     AgentAPIInstallGuideModeText,
		ContentTemplate: zhEn(connectorInstallCommand, connectorInstallCommand),
		CopyTemplate:    task,
	}
}

// kimiConnectorTaskZh / kimiConnectorTaskEn are connectorTaskZh / connectorTaskEn
// with an inserted step 0 covering the Kimi Code CLI's own install command —
// unlike the other CLIs on this list it isn't something every developer already
// has lying around, so the generic "assume it's installed" prerequisite line
// leaves the setup task with nothing to act on. The safety rule stays the same:
// the executing agent still must not run this unprompted.
const kimiConnectorTaskZh = `把这个 Grix Agent 接入本机的 grix-connector。请按顺序执行，完成后回报结果。

前提：本机已安装 Node.js 22.19+。如果没有，先告诉我，不要自行安装。

0) 安装 Kimi Code CLI（已安装则跳过，或按需升级）
npm install -g @moonshot-ai/kimi-code
安装后执行 kimi 进入交互界面，输入 /login 完成登录（首次使用必须登录才能用，不要跳过）。
⛔ 如果本机还没装、也还没登录，先告诉我，不要自行安装或登录——登录需要人工完成认证。

1) 安装连接器（已安装则升级到最新版）
%s

2) 把下面这条配置合并进 ~/.grix/config/agents.json
- 文件不存在 → 创建它，内容为 {"agents": [下面这条]}
- 文件已存在 → 用脚本读出 JSON，在 agents 数组里查找 agent_id 为 {{agent_id}} 的条目：找到就整条替换，没找到就追加。
  ⛔ 其余条目必须原样保留。禁止覆盖整个文件，禁止删改其他 Agent。

%s

3) 让配置生效
先执行 grix-connector status 判断：
- daemon 未运行 → grix-connector start
- daemon 已在运行 → grix-connector reload（热加载，不会打断其他 Agent 的会话）
⛔ 不要用 restart 来添加 Agent，它会重连所有 Agent、打断正在进行的对话。

4) 验证（必做）
grix-connector status 只报守护进程状态，不会列出 Agent。要确认这个 Agent 真的连上了，查本机的 admin 接口（daemon 起来后可能要等几秒）：
curl -s http://127.0.0.1:19580/api/agents
输出里应出现 "name":"{{agent_name}}" 且 "alive":true。（19580 是默认端口；若改过，真实端口写在 ~/.grix/data/admin-port。）

如果没连上，看 ~/.grix/log/ 下最新的日志。常见原因只有三个：kimi 不在 PATH、CLI 没登录、api_key 复制不全。

更多细节见 grix-connector 的 README（安装后位于 $(npm root -g)/grix-connector/README.md）的 "Adding an agent to an existing setup" 一节。

⚠️ api_key 是一次性凭据，只写入 ~/.grix/config/agents.json，不要打印到日志、不要提交到 git。`

const kimiConnectorTaskEn = `Connect this Grix Agent to grix-connector on this machine. Follow the steps in order and report back when done.

Prerequisite: Node.js 22.19+ is installed on this machine. If it is not, tell me first — do not install it yourself.

0) Install the Kimi Code CLI (skip if already installed, or upgrade it)
npm install -g @moonshot-ai/kimi-code
After installing, run kimi to open the interactive UI and enter /login to authenticate — this is required before first use, do not skip it.
If it is not installed or not logged in yet, tell me first — do not install or log in yourself, authentication needs a human to complete.

1) Install the connector (upgrades to the latest version if already installed)
%s

2) Merge the entry below into ~/.grix/config/agents.json
- file does not exist -> create it as {"agents": [the entry below]}
- file already exists -> read it as JSON, look through the agents array for the entry whose agent_id is {{agent_id}}: replace it if found, append if not.
  Every other entry must be left untouched. Never overwrite the whole file, never drop another Agent.

%s

3) Apply the change
Run grix-connector status first:
- daemon not running -> grix-connector start
- daemon already running -> grix-connector reload (hot-loads the new Agent, leaves running Agents untouched)
Do not use restart to add an Agent — it reconnects everything and interrupts live conversations.

4) Verify (required)
grix-connector status only reports the daemon, it does not list agents. To confirm this Agent is actually connected, query the local admin API (give the daemon a few seconds after it starts):
curl -s http://127.0.0.1:19580/api/agents
The output must contain "name":"{{agent_name}}" with "alive":true. (19580 is the default port; if it was changed, the real one is in ~/.grix/data/admin-port.)

If it never connects, read the newest log under ~/.grix/log/. In practice it is one of three things: kimi is not on PATH, the CLI is not logged in, or the api_key was truncated when copied.

For the details, see the "Adding an agent to an existing setup" section of the grix-connector README, which ships with the package at $(npm root -g)/grix-connector/README.md.

The api_key is a one-time secret: write it into ~/.grix/config/agents.json and nowhere else. Do not echo it into logs or commit it to git.`

func kimiGuide() agentAPIInstallGuideDef {
	entry := connectorConfigEntry(model.AgentClientTypeKimi)
	intro := localizedGuideText{}
	for lang, pattern := range connectorIntroPatterns {
		intro[lang] = fmt.Sprintf(pattern, "Kimi")
	}
	task := localizedGuideText{}
	for lang, tmpl := range kimiConnectorTasks {
		task[lang] = fmt.Sprintf(tmpl, connectorInstallCommand, entry)
	}
	return agentAPIInstallGuideDef{
		Type:            model.AgentClientTypeKimi,
		Label:           zhEn("Kimi", "Kimi"),
		Intro:           intro,
		ContentMode:     AgentAPIInstallGuideModeText,
		ContentTemplate: zhEn(connectorInstallCommand, connectorInstallCommand),
		CopyTemplate:    task,
	}
}

// deepseekConnectorTaskZh / deepseekConnectorTaskEn are connectorTaskZh / En
// with an inserted step 0: pnpm (dsh needs it on PATH for profile plugins)
// then the official npm CLI. Do not compile from source.
const deepseekConnectorTaskZh = `把这个 Grix Agent 接入本机的 grix-connector。请按顺序执行，完成后回报结果。

前提：本机已安装 Node.js 18+。如果没有，先告诉我，不要自行安装。

0) 安装 pnpm 和 DeepSeek Harness CLI（已安装则跳过，或按需升级）
npm i -g pnpm
npm i -g @deepseek-ai/dsh

1) 安装连接器（已安装则升级到最新版）
%s

2) 把下面这条配置合并进 ~/.grix/config/agents.json
- 文件不存在 → 创建它，内容为 {"agents": [下面这条]}
- 文件已存在 → 用脚本读出 JSON，在 agents 数组里查找 agent_id 为 {{agent_id}} 的条目：找到就整条替换，没找到就追加。
  ⛔ 其余条目必须原样保留。禁止覆盖整个文件，禁止删改其他 Agent。

%s

3) 让配置生效
先执行 grix-connector status 判断：
- daemon 未运行 → grix-connector start
- daemon 已在运行 → grix-connector reload（热加载，不会打断其他 Agent 的会话）
⛔ 不要用 restart 来添加 Agent，它会重连所有 Agent、打断正在进行的对话。

4) 验证（必做）
grix-connector status 只报守护进程状态，不会列出 Agent。要确认这个 Agent 真的连上了，查本机的 admin 接口（daemon 起来后可能要等几秒）：
curl -s http://127.0.0.1:19580/api/agents
输出里应出现 "name":"{{agent_name}}" 且 "alive":true。（19580 是默认端口；若改过，真实端口写在 ~/.grix/data/admin-port。）

如果没连上，看 ~/.grix/log/ 下最新的日志。常见原因只有三个：dsh 或 pnpm 不在 PATH、CLI 没登录、api_key 复制不全。

更多细节见 grix-connector 的 README（安装后位于 $(npm root -g)/grix-connector/README.md）的 "Adding an agent to an existing setup" 一节。

⚠️ api_key 是一次性凭据，只写入 ~/.grix/config/agents.json，不要打印到日志、不要提交到 git。`

const deepseekConnectorTaskEn = `Connect this Grix Agent to grix-connector on this machine. Follow the steps in order and report back when done.

Prerequisite: Node.js 18+ is installed on this machine. If it is not, tell me first — do not install it yourself.

0) Install pnpm and the DeepSeek Harness CLI (skip if already installed, or upgrade it)
npm i -g pnpm
npm i -g @deepseek-ai/dsh

1) Install the connector (upgrades to the latest version if already installed)
%s

2) Merge the entry below into ~/.grix/config/agents.json
- file does not exist -> create it as {"agents": [the entry below]}
- file already exists -> read it as JSON, look through the agents array for the entry whose agent_id is {{agent_id}}: replace it if found, append if not.
  Every other entry must be left untouched. Never overwrite the whole file, never drop another Agent.

%s

3) Apply the change
Run grix-connector status first:
- daemon not running -> grix-connector start
- daemon already running -> grix-connector reload (hot-loads the new Agent, leaves running Agents untouched)
Do not use restart to add an Agent — it reconnects everything and interrupts live conversations.

4) Verify (required)
grix-connector status only reports the daemon, it does not list agents. To confirm this Agent is actually connected, query the local admin API (give the daemon a few seconds after it starts):
curl -s http://127.0.0.1:19580/api/agents
The output must contain "name":"{{agent_name}}" with "alive":true. (19580 is the default port; if it was changed, the real one is in ~/.grix/data/admin-port.)

If it never connects, read the newest log under ~/.grix/log/. In practice it is one of three things: dsh or pnpm is not on PATH, the CLI is not logged in, or the api_key was truncated when copied.

For the details, see the "Adding an agent to an existing setup" section of the grix-connector README, which ships with the package at $(npm root -g)/grix-connector/README.md.

The api_key is a one-time secret: write it into ~/.grix/config/agents.json and nowhere else. Do not echo it into logs or commit it to git.`

func deepseekGuide() agentAPIInstallGuideDef {
	entry := connectorConfigEntry(model.AgentClientTypeDeepSeek)
	intro := localizedGuideText{}
	for lang, pattern := range connectorIntroPatterns {
		intro[lang] = fmt.Sprintf(pattern, "DeepSeek Harness")
	}
	task := localizedGuideText{}
	for lang, tmpl := range deepseekConnectorTasks {
		task[lang] = fmt.Sprintf(tmpl, connectorInstallCommand, entry)
	}
	return agentAPIInstallGuideDef{
		Type:            model.AgentClientTypeDeepSeek,
		Label:           zhEn("DeepSeek Harness", "DeepSeek Harness"),
		Intro:           intro,
		ContentMode:     AgentAPIInstallGuideModeText,
		ContentTemplate: zhEn(deepseekInstallCommand, deepseekInstallCommand),
		CopyTemplate:    task,
	}
}

var agentAPIInstallGuideDefs = []agentAPIInstallGuideDef{
	deepseekGuide(),
	connectorGuide(
		model.AgentClientTypeClaude, "Claude",
		"Claude Code", "Claude Code", "claude", "claude", "claude",
	),
	connectorGuide(
		model.AgentClientTypeCodex, "Codex",
		"Codex", "Codex", "codex", "codex", "codex",
	),
	kimiGuide(),
	connectorGuide(
		model.AgentClientTypeQwen, "Qwen",
		"Qwen", "Qwen Code", "qwen", "qwen", "qwen",
	),
	{
		Type:            model.AgentClientTypeOpenClaw,
		Label:           zhEn("OpenClaw", "OpenClaw"),
		Intro:           openclawIntros,
		ContentMode:     AgentAPIInstallGuideModeText,
		ContentTemplate: zhEn("openclaw plugin install grix-connector", "openclaw plugin install grix-connector"),
		CopyTemplate:    openclawTasks,
	},
	{
		Type:            model.AgentClientTypeHermes,
		Label:           zhEn("Hermes", "Hermes"),
		Intro:           hermesIntros,
		ContentMode:     AgentAPIInstallGuideModeText,
		ContentTemplate: zhEn("hermes plugins install askie/grix-hermes-python --enable", "hermes plugins install askie/grix-hermes-python --enable"),
		CopyTemplate:    hermesTasks,
	},
	connectorGuide(
		model.AgentClientTypeCursor, "Cursor",
		"Cursor Agent", "Cursor Agent", "agent", "agent", "agent",
	),
	connectorGuide(
		model.AgentClientTypeCopilot, "GitHub Copilot",
		"GitHub Copilot", "GitHub Copilot", "copilot 或 gh", "copilot or gh", "copilot / gh",
	),
	connectorGuide(
		model.AgentClientTypeKiro, "Kiro",
		"Kiro", "Kiro", "kiro-cli", "kiro-cli", "kiro-cli",
	),
	connectorGuide(
		model.AgentClientTypePi, "Pi",
		"Pi", "Pi", "pi", "pi", "pi",
	),
	connectorGuide(
		model.AgentClientTypeOpenCode, "OpenCode",
		"OpenCode", "OpenCode", "opencode", "opencode", "opencode",
	),
	connectorGuide(
		model.AgentClientTypeReasonix, "Reasonix",
		"Reasonix", "Reasonix", "reasonix", "reasonix", "reasonix",
	),
	connectorGuide(
		model.AgentClientTypeCodeWhale, "CodeWhale",
		"CodeWhale", "CodeWhale", "codewhale", "codewhale", "codewhale",
	),
	connectorGuide(
		model.AgentClientTypeAgy, "Antigravity",
		"Antigravity", "Antigravity", "agy", "agy", "agy",
	),
}

func AgentAPIInstallGuideCatalog(lang string) AgentAPIInstallGuideCatalogResp {
	list := make([]AgentAPIInstallGuideResp, 0, len(agentAPIInstallGuideDefs))
	for _, item := range agentAPIInstallGuideDefs {
		list = append(list, AgentAPIInstallGuideResp{
			Type:            item.Type,
			Label:           pickGuideText(item.Label, lang),
			Intro:           pickGuideText(item.Intro, lang),
			ContentMode:     item.ContentMode,
			ContentTemplate: pickGuideText(item.ContentTemplate, lang),
			LinkLabel:       pickGuideText(item.LinkLabel, lang),
			LinkURL:         strings.TrimSpace(item.LinkURL),
			CopyTemplate:    pickGuideText(item.CopyTemplate, lang),
		})
	}
	return AgentAPIInstallGuideCatalogResp{
		DefaultType: model.AgentClientTypeClaude,
		List:        list,
	}
}

func pickGuideText(text localizedGuideText, lang string) string {
	normalized := strings.ToLower(strings.TrimSpace(lang))
	if v := strings.TrimSpace(text[normalized]); v != "" {
		return v
	}
	if v := strings.TrimSpace(text["en"]); v != "" {
		return v
	}
	return strings.TrimSpace(text["zh"])
}
