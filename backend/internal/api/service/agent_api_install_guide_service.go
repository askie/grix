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

前提：本机已安装 Node.js 18+，以及 %s 且已可正常运行（官方登录或第三方 API 均可）。如果缺少其中之一，先告诉我，不要自行安装。

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

如果没连上，看 ~/.grix/log/ 下最新的日志。常见原因只有三个：%s 不在 PATH、CLI 起不来、api_key 复制不全。

更多细节见 grix-connector 的 README（安装后位于 $(npm root -g)/grix-connector/README.md）的 "Adding an agent to an existing setup" 一节。

⚠️ api_key 是一次性凭据，只写入 ~/.grix/config/agents.json，不要打印到日志、不要提交到 git。`

const connectorTaskEn = `Connect this Grix Agent to grix-connector on this machine. Follow the steps in order and report back when done.

Prerequisites: Node.js 18+ and %s, installed and able to run on this machine (authenticated through its own login or a third-party API key — either is fine). If either is missing, tell me first — do not install it yourself.

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

If it never connects, read the newest log under ~/.grix/log/. In practice it is one of three things: %s is not on PATH, the CLI does not start, or the api_key was truncated when copied.

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

// acpConfigEntry is connectorConfigEntry plus the two fields a generic ACP
// agent cannot do without: the platform knows the protocol but not which CLI
// speaks it, so the owner supplies the executable and its arguments. Every
// other field keeps the shape the connector already validates.
func acpConfigEntry() string {
	return fmt.Sprintf(`{
  "name": "{{agent_name}}",
  "ws_url": "{{api_endpoint}}",
  "agent_id": "{{agent_id}}",
  "api_key": "{{api_key}}",
  "client_type": %q,
  "command": "<REPLACE: your ACP CLI executable, e.g. my-agent>",
  "args": ["<REPLACE: the flags that start its ACP mode, e.g. --acp>"]
}`, model.AgentClientTypeACP)
}

// acpGuide serves client_type "acp": any CLI implementing the Agent Client
// Protocol. It reuses the shared connector task in all app languages — the
// steps are identical — and only swaps in the CLI phrase and the config entry
// carrying command/args. No vendor CLI is named because none is known.
func acpGuide() agentAPIInstallGuideDef {
	entry := acpConfigEntry()
	intro := localizedGuideText{}
	for lang, pattern := range connectorIntroPatterns {
		intro[lang] = fmt.Sprintf(pattern, "ACP (Agent Client Protocol)")
	}
	task := localizedGuideText{}
	for lang, tmpl := range connectorTasks {
		cli := cliPhrase(lang, "ACP", "你在 command 里填的那个可执行文件", "the executable you put in command", "the executable set in command")
		task[lang] = fmt.Sprintf(tmpl, cli, connectorInstallCommand, entry, cli)
	}
	return agentAPIInstallGuideDef{
		Type:            model.AgentClientTypeACP,
		Label:           zhEn("ACP Agent", "ACP Agent"),
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
⛔ 如果本机还没装、或者装了还起不来，先告诉我，不要自行安装或认证——认证需要人工完成。

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

如果没连上，看 ~/.grix/log/ 下最新的日志。常见原因只有三个：kimi 不在 PATH、CLI 起不来、api_key 复制不全。

更多细节见 grix-connector 的 README（安装后位于 $(npm root -g)/grix-connector/README.md）的 "Adding an agent to an existing setup" 一节。

⚠️ api_key 是一次性凭据，只写入 ~/.grix/config/agents.json，不要打印到日志、不要提交到 git。`

const kimiConnectorTaskEn = `Connect this Grix Agent to grix-connector on this machine. Follow the steps in order and report back when done.

Prerequisite: Node.js 22.19+ is installed on this machine. If it is not, tell me first — do not install it yourself.

0) Install the Kimi Code CLI (skip if already installed, or upgrade it)
npm install -g @moonshot-ai/kimi-code
After installing, run kimi to open the interactive UI and enter /login to authenticate — this is required before first use, do not skip it.
If it is not installed, or installed but not able to run yet, tell me first — do not install or authenticate it yourself, authentication needs a human to complete.

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

If it never connects, read the newest log under ~/.grix/log/. In practice it is one of three things: kimi is not on PATH, the CLI does not start, or the api_key was truncated when copied.

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

如果没连上，看 ~/.grix/log/ 下最新的日志。常见原因只有三个：dsh 或 pnpm 不在 PATH、CLI 起不来、api_key 复制不全。

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

If it never connects, read the newest log under ~/.grix/log/. In practice it is one of three things: dsh or pnpm is not on PATH, the CLI does not start, or the api_key was truncated when copied.

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

// customCliConnectorTaskZhTemplate / EnTemplate mirror kimiConnectorTaskZh/En's
// shape (install+login CLI as step 0, then the shared connector steps 1-4) but
// parameterize the four bits that vary per CLI: the install command, the login
// instruction, the Node.js version floor, and the binary name used in the
// generic "common causes" troubleshooting line. %s slots in order:
// nodeVersion, displayName, installCmd, loginInstruction, connectorInstallCommand,
// agents.json entry, binName.
const customCliConnectorTaskZhTemplate = `把这个 Grix Agent 接入本机的 grix-connector。请按顺序执行，完成后回报结果。

前提：本机已安装 Node.js %s+。如果没有，先告诉我，不要自行安装。

0) 安装 %s CLI（已安装则跳过，或按需升级）
%s
%s
⛔ 如果本机还没装、或者装了还起不来，先告诉我，不要自行安装或认证——认证需要人工完成。

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

如果没连上，看 ~/.grix/log/ 下最新的日志。常见原因只有三个：%s 不在 PATH、CLI 起不来、api_key 复制不全。

更多细节见 grix-connector 的 README（安装后位于 $(npm root -g)/grix-connector/README.md）的 "Adding an agent to an existing setup" 一节。

⚠️ api_key 是一次性凭据，只写入 ~/.grix/config/agents.json，不要打印到日志、不要提交到 git。`

const customCliConnectorTaskEnTemplate = `Connect this Grix Agent to grix-connector on this machine. Follow the steps in order and report back when done.

Prerequisite: Node.js %s+ is installed on this machine. If it is not, tell me first — do not install it yourself.

0) Install the %s CLI (skip if already installed, or upgrade it)
%s
%s
If it is not installed, or installed but not able to run yet, tell me first — do not install or authenticate it yourself, authentication needs a human to complete.

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

If it never connects, read the newest log under ~/.grix/log/. In practice it is one of three things: %s is not on PATH, the CLI does not start, or the api_key was truncated when copied.

For the details, see the "Adding an agent to an existing setup" section of the grix-connector README, which ships with the package at $(npm root -g)/grix-connector/README.md.

The api_key is a one-time secret: write it into ~/.grix/config/agents.json and nowhere else. Do not echo it into logs or commit it to git.`

// customCliInstallGuide builds a connector guide for a CLI that (like Kimi and
// DeepSeek Harness) needs installing and authenticating before first use. Only
// zh/en are authored here; pickGuideText already falls back to en for any
// other app language, so the other nine languages read the English text until
// someone adds native copy — same degrade path zh/en-only guides already use
// elsewhere in this catalog (see zhEn()).
func customCliInstallGuide(clientType, displayName, nodeVersion, installCmd, loginInstruction, binName string) agentAPIInstallGuideDef {
	entry := connectorConfigEntry(clientType)
	intro := localizedGuideText{}
	for lang, pattern := range connectorIntroPatterns {
		intro[lang] = fmt.Sprintf(pattern, displayName)
	}
	task := localizedGuideText{
		"zh": fmt.Sprintf(customCliConnectorTaskZhTemplate, nodeVersion, displayName, installCmd, loginInstruction, connectorInstallCommand, entry, binName),
		"en": fmt.Sprintf(customCliConnectorTaskEnTemplate, nodeVersion, displayName, installCmd, loginInstruction, connectorInstallCommand, entry, binName),
	}
	return agentAPIInstallGuideDef{
		Type:            clientType,
		Label:           zhEn(displayName, displayName),
		Intro:           intro,
		ContentMode:     AgentAPIInstallGuideModeText,
		ContentTemplate: zhEn(installCmd, installCmd),
		CopyTemplate:    task,
	}
}

// qodercliGuide: official install is `curl -fsSL https://qoder.com/install | bash`
// (docs.qoder.com/cli/installation), login is browser OAuth via `qodercli login`
// (confirmed via `qodercli login --help` on this machine — "Sign in to your
// Qoder account through the browser"). Requires Node.js >=20 for the npm path.
func qodercliGuide() agentAPIInstallGuideDef {
	return customCliInstallGuide(
		model.AgentClientTypeQoderCLI, "Qoder CLI", "20",
		"curl -fsSL https://qoder.com/install | bash",
		// 未发现可脚本化的自定义/OpenAI 兼容端点入口（--list-models 只列账号自带模型，
		// -m/--model 的 "Custom" 档需要在交互式 UI 内手动配置，非 CLI 参数或 env）：
		// 不接入 Grix 中转，账号计费由 Qoder 自己的账户体系承担。
		"安装后执行 qodercli login，浏览器完成登录后再继续（首次使用必须登录才能用，不要跳过；该 CLI 使用你自己 Qoder 账号的模型额度计费，不经 Grix 中转）。",
		"qodercli",
	)
}

// qoderclicnGuide: Alibaba Qoder's CN-region distribution of the same product
// as qodercli, independent install channel — official install is
// `curl -fsSL https://static.qoder.com.cn/qoder-cli-cn/install.sh | bash`
// (docs.qoder.cn/cli/installation), login is `qoderclicn login` (same browser
// OAuth shape as qodercli, not independently confirmed on this machine because
// no CN account was available — see round1/round2a probe notes).
func qoderclicnGuide() agentAPIInstallGuideDef {
	return customCliInstallGuide(
		model.AgentClientTypeQoderCLICN, "Qoder CLI CN", "20",
		"curl -fsSL https://static.qoder.com.cn/qoder-cli-cn/install.sh | bash",
		// Same product family as qodercli: no scriptable custom-endpoint entry found.
		"安装后执行 qoderclicn login，浏览器完成登录后再继续（首次使用必须登录才能用，不要跳过；该 CLI 使用你自己 Qoder 账号的模型额度计费，不经 Grix 中转）。",
		"qoderclicn",
	)
}

// mcodeGuide: official install is `npm install -g @minimax-ai/code` (no
// independent install script found); login is `mcode login` (browser OAuth,
// confirmed via `mcode login --help` on this machine — supports --region cn|global).
// package.json declares engines.node >=22.19, higher than the other three.
func mcodeGuide() agentAPIInstallGuideDef {
	return customCliInstallGuide(
		model.AgentClientTypeMCode, "MiniMax Code", "22.19",
		"npm install -g @minimax-ai/code",
		"安装后执行 mcode login，浏览器完成登录后再继续（首次使用必须登录才能用，不要跳过；如需切换账号区域可加 --region cn 或 --region global）。",
		"mcode",
	)
}

// dimGuide: official install is `npm install -g dimcode` (no independent
// install script found); login is `dim auth login` (confirmed via
// `dim auth --help` on this machine — usage: dim auth <login|logout|refresh|status>).
func dimGuide() agentAPIInstallGuideDef {
	return customCliInstallGuide(
		model.AgentClientTypeDim, "DimAgent", "18",
		"npm install -g dimcode",
		"安装后执行 dim auth login，浏览器完成登录后再继续（首次使用必须登录才能用，不要跳过）。",
		"dim",
	)
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
		ContentTemplate: zhEn("openclaw plugins install grix-connector", "openclaw plugins install grix-connector"),
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
	qodercliGuide(),
	qoderclicnGuide(),
	mcodeGuide(),
	dimGuide(),
	acpGuide(),
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
