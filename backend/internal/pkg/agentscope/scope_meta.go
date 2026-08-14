package agentscope

import "strings"

type ScopeItem struct {
	Scope       string `json:"scope"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type localizedScopeText struct {
	ZH string
	EN string
}

type scopeText struct {
	Label       localizedScopeText
	Description localizedScopeText
}

var scopeTextCatalog = map[string]scopeText{
	ScopeAgentAPICreate: {
		Label:       localizedScopeText{ZH: "创建 API Agent", EN: "Create API Agent"},
		Description: localizedScopeText{ZH: "允许创建新的 Agent API 类型 Agent。", EN: "Allow creating new Agent API type agents."},
	},
	ScopeAgentCategoryList: {
		Label:       localizedScopeText{ZH: "查看 Agent 分类", EN: "List Agent Categories"},
		Description: localizedScopeText{ZH: "允许查看当前账号下的 Agent 分类列表。", EN: "Allow listing existing agent categories."},
	},
	ScopeAgentCategoryCreate: {
		Label:       localizedScopeText{ZH: "创建 Agent 分类", EN: "Create Agent Category"},
		Description: localizedScopeText{ZH: "允许创建新的 Agent 分类。", EN: "Allow creating a new agent category."},
	},
	ScopeAgentCategoryUpdate: {
		Label:       localizedScopeText{ZH: "修改 Agent 分类", EN: "Update Agent Category"},
		Description: localizedScopeText{ZH: "允许修改已有 Agent 分类。", EN: "Allow updating an existing agent category."},
	},
	ScopeAgentCategoryAssign: {
		Label:       localizedScopeText{ZH: "设置 Agent 分类", EN: "Assign Agent Category"},
		Description: localizedScopeText{ZH: "允许为 Agent 设置或清空分类。", EN: "Allow setting or clearing a category for an agent."},
	},
	ScopeSessionSearch: {
		Label:       localizedScopeText{ZH: "搜索会话", EN: "Search Sessions"},
		Description: localizedScopeText{ZH: "允许搜索会话。", EN: "Allow searching sessions."},
	},
	ScopeContactSearch: {
		Label:       localizedScopeText{ZH: "搜索联系人", EN: "Search Contacts"},
		Description: localizedScopeText{ZH: "允许搜索联系人。", EN: "Allow searching contacts."},
	},
	ScopeGroupCreate: {
		Label:       localizedScopeText{ZH: "建群", EN: "Create Group"},
		Description: localizedScopeText{ZH: "允许创建新的群聊会话。", EN: "Allow creating a new group session."},
	},
	ScopeGroupMemberAdd: {
		Label:       localizedScopeText{ZH: "拉人进群", EN: "Add Members"},
		Description: localizedScopeText{ZH: "允许向群聊添加成员。", EN: "Allow adding members to group sessions."},
	},
	ScopeGroupMemberRemove: {
		Label:       localizedScopeText{ZH: "移出成员", EN: "Remove Members"},
		Description: localizedScopeText{ZH: "允许将成员移出群聊。", EN: "Allow removing members from group sessions."},
	},
	ScopeGroupMemberRoleUpdate: {
		Label:       localizedScopeText{ZH: "调整成员角色", EN: "Update Member Role"},
		Description: localizedScopeText{ZH: "允许调整群成员角色。", EN: "Allow changing roles for group members."},
	},
	ScopeGroupSpeakingUpdate: {
		Label:       localizedScopeText{ZH: "调整发言权限", EN: "Update Speaking Rules"},
		Description: localizedScopeText{ZH: "允许调整全体禁言和成员发言权限设置。", EN: "Allow updating group mute-all and member speaking settings."},
	},
	ScopeGroupDissolve: {
		Label:       localizedScopeText{ZH: "解散群聊", EN: "Dissolve Group"},
		Description: localizedScopeText{ZH: "允许解散群聊。", EN: "Allow dissolving group sessions."},
	},
	ScopeAgentDispatch: {
		Label:       localizedScopeText{ZH: "派 Agent 干活", EN: "Dispatch Agent"},
		Description: localizedScopeText{ZH: "允许该 Agent 指派你旗下的其他 Agent 到指定目录执行任务。", EN: "Allow this agent to assign your other agents to run tasks in a specified directory."},
	},
	ScopeSessionSend: {
		Label:       localizedScopeText{ZH: "代主人发消息", EN: "Send as Owner"},
		Description: localizedScopeText{ZH: "允许该 Agent 以你的身份在会话中发送消息。", EN: "Allow this agent to send messages in sessions on your behalf."},
	},
	ScopeOwnerCall: {
		Label:       localizedScopeText{ZH: "呼叫主人", EN: "Call Owner"},
		Description: localizedScopeText{ZH: "允许该 Agent 呼叫你并发起语音通话。", EN: "Allow this agent to call you and start a voice session."},
	},
	ScopeAgentIntroUpdate: {
		Label:       localizedScopeText{ZH: "修改 Agent 介绍", EN: "Update Agent Introduction"},
		Description: localizedScopeText{ZH: "允许该 Agent 修改你旗下任意 Agent 的介绍。", EN: "Allow this agent to edit the introduction of any agent you own."},
	},
	ScopeAgentTaskQuery: {
		Label:       localizedScopeText{ZH: "查看任务状态", EN: "View Task Status"},
		Description: localizedScopeText{ZH: "允许该 Agent 查询你名下各会话的任务状态（进行中、已完成、待处理）。", EN: "Allow this agent to query the task status (running, completed, needs attention) of your sessions."},
	},
	ScopeConversationAuditRead: {
		Label:       localizedScopeText{ZH: "读取对话审计", EN: "Read conversation audit"},
		Description: localizedScopeText{ZH: "允许该 Agent 读取已生成的对话审计 manifest、调用时间线和正文分块。", EN: "Allow this Agent to read generated conversation audit manifests, timelines, and content chunks."},
	},
	ScopeMediaUpload: {
		Label:       localizedScopeText{ZH: "上传文件", EN: "Upload Files"},
		Description: localizedScopeText{ZH: "允许该 Agent 上传文件并发送图片、文档等媒体消息。", EN: "Allow this agent to upload files and send media messages such as images and documents."},
	},
	ScopeAppLocalSearch: {
		Label:       localizedScopeText{ZH: "搜索本机聊天记录", EN: "Search Local Chat History"},
		Description: localizedScopeText{ZH: "允许该 Agent 在你本机的聊天记录中按关键词搜索会话和消息。", EN: "Allow this agent to search your local chat sessions and messages by keyword."},
	},
	ScopeAppOpenChat: {
		Label:       localizedScopeText{ZH: "打开聊天会话", EN: "Open Chat"},
		Description: localizedScopeText{ZH: "允许该 Agent 打开指定的聊天会话页面。", EN: "Allow this agent to open a specific chat session."},
	},
	ScopeAppOpenPage: {
		Label:       localizedScopeText{ZH: "打开应用页面", EN: "Open App Page"},
		Description: localizedScopeText{ZH: "允许该 Agent 打开应用内的功能或设置页面。", EN: "Allow this agent to open a feature or settings page in the app."},
	},
	ScopeWidgetVisitorBan: {
		Label:       localizedScopeText{ZH: "封禁访客", EN: "Ban Visitors"},
		Description: localizedScopeText{ZH: "允许该 Agent 封禁网站 Widget 访客会话，并自动封禁该访客最近使用的 IP（全局生效，默认 7 天）。", EN: "Allow this agent to ban website widget visitor sessions and automatically ban the visitor's most recent IP (owner-wide, 7 days by default)."},
	},
}

func AllowedScopeItems(lang string) []ScopeItem {
	result := make([]ScopeItem, 0, len(allowedScopeList))
	for _, scope := range allowedScopeList {
		text := scopeTextCatalog[scope]
		result = append(result, ScopeItem{
			Scope:       scope,
			Label:       pickScopeText(text.Label, lang, scope),
			Description: pickScopeText(text.Description, lang, scope),
		})
	}
	return result
}

func pickScopeText(text localizedScopeText, lang, fallback string) string {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(lang), "_", "-"))
	if (normalized == "zh" || strings.HasPrefix(normalized, "zh-")) && strings.TrimSpace(text.ZH) != "" {
		return text.ZH
	}
	if strings.TrimSpace(text.EN) != "" {
		return text.EN
	}
	if strings.TrimSpace(text.ZH) != "" {
		return text.ZH
	}
	return fallback
}
