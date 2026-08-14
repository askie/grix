package agentscope

import (
	"errors"
	"strings"
)

const (
	ScopeAgentAPICreate        = "agent.api.create"
	ScopeAgentCategoryList     = "agent.category.list"
	ScopeAgentCategoryCreate   = "agent.category.create"
	ScopeAgentCategoryUpdate   = "agent.category.update"
	ScopeAgentCategoryAssign   = "agent.category.assign"
	ScopeSessionSearch         = "session.search"
	ScopeContactSearch         = "contact.search"
	ScopeGroupCreate           = "group.create"
	ScopeGroupMemberAdd        = "group.member.add"
	ScopeGroupMemberRemove     = "group.member.remove"
	ScopeGroupMemberRoleUpdate = "group.member.role.update"
	ScopeGroupSpeakingUpdate   = "group.speaking.update"
	ScopeGroupDissolve         = "group.dissolve"
	ScopeAgentDispatch         = "agent.dispatch"
	ScopeSessionSend           = "session.send"
	ScopeOwnerCall             = "owner.call"
	ScopeAgentIntroUpdate      = "agent.introduction.update"
	ScopeAgentTaskQuery        = "agent.task.query"
	// 对话审计回放读取（manifest / spans / content chunk 三个只读动作共用）。
	ScopeConversationAuditRead = "conversation.audit.read"
	// 媒体上传加签（HTTP /oss/presign 与 ws media_upload_init 共用）。
	ScopeMediaUpload = "media.upload"
	// APP 内置 MCP 工具能力（经 ws mcp_frame 透传给 APP MCP Server 调用）。
	ScopeAppLocalSearch = "app.local_search"
	ScopeAppOpenChat    = "app.open_chat"
	ScopeAppOpenPage    = "app.open_page"
	// widget 访客封禁（按会话 ban，附带 owner 全局 IP 封禁，见 security.BanWidgetIP）。
	ScopeWidgetVisitorBan = "widget.visitor.ban"
)

var ErrInvalidScope = errors.New("invalid agent scope")

var allowedScopeSet = map[string]struct{}{
	ScopeAgentAPICreate:        {},
	ScopeAgentCategoryList:     {},
	ScopeAgentCategoryCreate:   {},
	ScopeAgentCategoryUpdate:   {},
	ScopeAgentCategoryAssign:   {},
	ScopeSessionSearch:         {},
	ScopeContactSearch:         {},
	ScopeGroupCreate:           {},
	ScopeGroupMemberAdd:        {},
	ScopeGroupMemberRemove:     {},
	ScopeGroupMemberRoleUpdate: {},
	ScopeGroupSpeakingUpdate:   {},
	ScopeGroupDissolve:         {},
	ScopeAgentDispatch:         {},
	ScopeSessionSend:           {},
	ScopeOwnerCall:             {},
	ScopeAgentIntroUpdate:      {},
	ScopeAgentTaskQuery:        {},
	ScopeConversationAuditRead: {},
	ScopeMediaUpload:           {},
	ScopeAppLocalSearch:        {},
	ScopeAppOpenChat:           {},
	ScopeAppOpenPage:           {},
	ScopeWidgetVisitorBan:      {},
}

var allowedScopeList = []string{
	ScopeAgentAPICreate,
	ScopeAgentCategoryList,
	ScopeAgentCategoryCreate,
	ScopeAgentCategoryUpdate,
	ScopeAgentCategoryAssign,
	ScopeSessionSearch,
	ScopeContactSearch,
	ScopeGroupCreate,
	ScopeGroupMemberAdd,
	ScopeGroupMemberRemove,
	ScopeGroupMemberRoleUpdate,
	ScopeGroupSpeakingUpdate,
	ScopeGroupDissolve,
	ScopeAgentDispatch,
	ScopeSessionSend,
	ScopeOwnerCall,
	ScopeAgentIntroUpdate,
	ScopeAgentTaskQuery,
	ScopeConversationAuditRead,
	ScopeMediaUpload,
	ScopeAppLocalSearch,
	ScopeAppOpenChat,
	ScopeAppOpenPage,
	ScopeWidgetVisitorBan,
}

func IsAllowed(scope string) bool {
	normalized := strings.TrimSpace(scope)
	if normalized == "" {
		return false
	}
	_, ok := allowedScopeSet[normalized]
	return ok
}

func Normalize(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return []string{}, nil
	}

	seen := make(map[string]struct{}, len(scopes))
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		normalized := strings.TrimSpace(scope)
		if normalized == "" {
			return nil, ErrInvalidScope
		}
		if !IsAllowed(normalized) {
			return nil, ErrInvalidScope
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func AllowedScopes() []string {
	result := make([]string, len(allowedScopeList))
	copy(result, allowedScopeList)
	return result
}
