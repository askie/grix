package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
)

// agentInvokeAction defines a registered action with its scope requirement.
type agentInvokeAction struct {
	Scope string // empty means no scope check required
}

type agentInvokeHooks struct {
	sendMessage func(SendMessageReq) (*SendMessageResult, error)
	deleteMsg   func(ctx context.Context, agentID, ownerID int64, payload DeleteMsgPayload) error
	// bindSession 向目标 agent 下发 session_control open 动作并同步等待绑定结果。
	bindSession func(agentID int64, sessionID, actorID, cwd, providerKey string) (*sessionBindResponse, error)
}

// actionRegistry maps action strings to their scope requirements.
var actionRegistry = map[string]agentInvokeAction{
	"contact_search":                    {Scope: agentscope.ScopeContactSearch},
	"session_search":                    {Scope: agentscope.ScopeSessionSearch},
	"message_history":                   {},
	"message_search":                    {},
	"agent_api_create":                  {Scope: agentscope.ScopeAgentAPICreate},
	"agent_category_list":               {Scope: agentscope.ScopeAgentCategoryList},
	"agent_category_create":             {Scope: agentscope.ScopeAgentCategoryCreate},
	"agent_category_update":             {Scope: agentscope.ScopeAgentCategoryUpdate},
	"agent_category_assign":             {Scope: agentscope.ScopeAgentCategoryAssign},
	"group_create":                      {Scope: agentscope.ScopeGroupCreate},
	"group_leave_self":                  {},
	"group_member_add":                  {Scope: agentscope.ScopeGroupMemberAdd},
	"group_member_remove":               {Scope: agentscope.ScopeGroupMemberRemove},
	"group_member_role_update":          {Scope: agentscope.ScopeGroupMemberRoleUpdate},
	"group_all_members_muted_update":    {Scope: agentscope.ScopeGroupSpeakingUpdate},
	"group_member_speaking_update":      {Scope: agentscope.ScopeGroupSpeakingUpdate},
	"group_dissolve":                    {Scope: agentscope.ScopeGroupDissolve},
	"group_detail_read":                 {},
	"claude_interaction_request_create": {},
	"agent_access_control":              {},
	// claude_access_control 是 agent_access_control 的历史别名，兼容已部署的旧版连接器。
	"claude_access_control":     {},
	"agent_api_key_rotate":      {Scope: agentscope.ScopeAgentAPICreate},
	"send_msg":                  {},
	"delete_msg":                {},
	"agent_introduction_update": {Scope: agentscope.ScopeAgentIntroUpdate},
	"call_owner":                {Scope: agentscope.ScopeOwnerCall},
	"session_send":              {Scope: agentscope.ScopeSessionSend},
	"dispatch_agent":            {Scope: agentscope.ScopeAgentDispatch},
	"chat_state_query":          {Scope: agentscope.ScopeAgentTaskQuery},
	"chat_state_update":         {Scope: agentscope.ScopeAgentTaskQuery},
	"audit_get_manifest":        {Scope: agentscope.ScopeConversationAuditRead},
	"audit_list_spans":          {Scope: agentscope.ScopeConversationAuditRead},
	"audit_get_content_chunk":   {Scope: agentscope.ScopeConversationAuditRead},
	"egg_search":                {},
	"egg_get":                   {},
	"search_favorite_sessions":  {Scope: agentscope.ScopeSessionSearch},
	"skill_set":                 {},
	"skill_get":                 {},
	"widget_visitor_ban":        {Scope: agentscope.ScopeWidgetVisitorBan},
	"webhook_create":            {Scope: agentscope.ScopeWebhookCreate},
}

// dispatchAgentInvoke routes an action to the corresponding service function.
// Returns (data, errorCode, errorMsg). errorCode==0 means success.
func dispatchAgentInvoke(agentID, ownerID int64, action string, params map[string]interface{}) (interface{}, int, string) {
	return dispatchAgentInvokeWithHooks(agentID, ownerID, action, params, agentInvokeHooks{})
}

func dispatchAgentInvokeWithHooks(agentID, ownerID int64, action string, params map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	reg, ok := actionRegistry[action]
	if !ok {
		return nil, 4004, fmt.Sprintf("unknown action: %s", action)
	}

	if reg.Scope != "" {
		if err := checkAgentScope(agentID, reg.Scope); err != nil {
			return nil, 4003, err.Error()
		}
	}

	switch action {
	case "contact_search":
		return dispatchContactSearch(ownerID, params)
	case "session_search":
		return dispatchSessionSearch(ownerID, params)
	case "search_favorite_sessions":
		return dispatchSearchFavoriteSessions(ownerID, params)
	case "message_history":
		return dispatchMessageHistory(ownerID, params)
	case "message_search":
		return dispatchMessageSearch(ownerID, params)
	case "agent_api_create":
		return dispatchAgentAPICreate(ownerID, params)
	case "agent_category_list":
		return dispatchAgentCategoryList(ownerID)
	case "agent_category_create":
		return dispatchAgentCategoryCreate(ownerID, params)
	case "agent_category_update":
		return dispatchAgentCategoryUpdate(ownerID, params)
	case "agent_category_assign":
		return dispatchAgentCategoryAssign(ownerID, params)
	case "group_create":
		return dispatchGroupCreate(ownerID, agentID, params)
	case "group_leave_self":
		return dispatchGroupLeave(agentID, ownerID, params)
	case "group_member_add":
		return dispatchGroupMemberAdd(ownerID, params)
	case "group_member_remove":
		return dispatchGroupMemberRemove(ownerID, params)
	case "group_member_role_update":
		return dispatchGroupMemberRoleUpdate(ownerID, params)
	case "group_all_members_muted_update":
		return dispatchGroupAllMembersMuted(ownerID, params)
	case "group_member_speaking_update":
		return dispatchGroupMemberSpeaking(ownerID, params)
	case "group_dissolve":
		return dispatchGroupDissolve(ownerID, params)
	case "group_detail_read":
		return dispatchGroupDetailRead(agentID, ownerID, params)
	case "claude_interaction_request_create":
		return dispatchClaudeInteractionRequestCreate(agentID, ownerID, params, hooks)
	case "agent_access_control", "claude_access_control":
		return dispatchAccessControl(agentID, ownerID, params, hooks)
	case "agent_api_key_rotate":
		return dispatchAgentAPIKeyRotate(ownerID, params)
	case "send_msg":
		return dispatchSendMsg(agentID, ownerID, params, hooks)
	case "delete_msg":
		return dispatchDeleteMsg(agentID, ownerID, params, hooks)
	case "agent_introduction_update":
		return dispatchAgentIntroductionUpdate(ownerID, params)
	case "call_owner":
		return dispatchCallOwner(agentID, ownerID, params, hooks)
	case "session_send":
		return dispatchSessionSend(agentID, ownerID, params, hooks)
	case "dispatch_agent":
		return dispatchDispatchAgent(agentID, ownerID, params, hooks)
	case "chat_state_query":
		return dispatchChatStateQuery(ownerID, agentID, params)
	case "chat_state_update":
		return dispatchChatStateUpdate(ownerID, params)
	case "audit_get_manifest", "audit_list_spans", "audit_get_content_chunk":
		return dispatchAuditReplayAction(ownerID, action, params)
	case "egg_search":
		return dispatchEggSearch(ownerID, params)
	case "egg_get":
		return dispatchEggGet(ownerID, params)
	case "skill_set":
		return dispatchSkillSet(ownerID, params)
	case "skill_get":
		return dispatchSkillGet(ownerID, params)
	case "widget_visitor_ban":
		return dispatchWidgetVisitorBan(ownerID, params)
	case "webhook_create":
		return dispatchWebhookCreate(agentID, ownerID, params)
	default:
		return nil, 4004, fmt.Sprintf("unhandled action: %s", action)
	}
}

// checkAgentScope verifies the agent has the required scope.
func checkAgentScope(agentID int64, scope string) error {
	var count int64
	if err := store.DB.Model(&model.AgentAPIScope{}).
		Where("agent_id = ? AND scope = ?", agentID, scope).
		Count(&count).Error; err != nil {
		return fmt.Errorf("scope check failed: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("agent %d lacks scope %s", agentID, scope)
	}
	return nil
}

// --- Param helpers ---

func paramInt(params map[string]interface{}, key string) (int, bool) {
	v, ok := params[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func paramInt64(params map[string]interface{}, key string) (int64, bool) {
	v, ok := params[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func paramString(params map[string]interface{}, key string) (string, bool) {
	v, ok := params[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func paramBool(params map[string]interface{}, key string) (bool, bool) {
	v, ok := params[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func paramStringSlice(params map[string]interface{}, key string) ([]string, bool) {
	v, ok := params[key]
	if !ok {
		return nil, false
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		result = append(result, s)
	}
	return result, true
}

func paramIntSlice(params map[string]interface{}, key string) ([]int16, bool) {
	v, ok := params[key]
	if !ok {
		return nil, false
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	result := make([]int16, 0, len(arr))
	for _, item := range arr {
		switch n := item.(type) {
		case float64:
			result = append(result, int16(n))
		case int:
			result = append(result, int16(n))
		default:
			return nil, false
		}
	}
	return result, true
}

// --- Action dispatchers ---

func dispatchContactSearch(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	limit, _ := paramInt(params, "limit")
	if limit <= 0 {
		limit = 20
	}
	offset, _ := paramInt(params, "offset")

	idText, hasID := paramString(params, "id")
	keyword, hasKeyword := paramString(params, "keyword")

	switch {
	case hasID && strings.TrimSpace(idText) != "":
		id, err := strconv.ParseInt(strings.TrimSpace(idText), 10, 64)
		if err != nil {
			return nil, 4001, "id invalid"
		}
		data, svcErr := service.ContactSearchByID(ownerID, id, limit, offset)
		if svcErr != nil {
			return nil, 5001, svcErr.Error()
		}
		return data, 0, ""
	case hasKeyword && strings.TrimSpace(keyword) != "":
		data, svcErr := service.ContactSearch(ownerID, keyword, limit, offset)
		if svcErr != nil {
			return nil, 5001, svcErr.Error()
		}
		return data, 0, ""
	default:
		data, svcErr := service.ContactListAll(ownerID, limit, offset)
		if svcErr != nil {
			return nil, 5001, svcErr.Error()
		}
		return data, 0, ""
	}
}

func dispatchSessionSearch(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	limit, _ := paramInt(params, "limit")
	if limit <= 0 {
		limit = 20
	}
	offset, _ := paramInt(params, "offset")

	id, hasID := paramString(params, "id")
	keyword, hasKeyword := paramString(params, "keyword")
	peerID, hasPeerID := paramInt64(params, "peer_id")
	if _, provided := params["peer_id"]; provided && !hasPeerID {
		return nil, 4001, "peer_id invalid"
	}

	// session_type: 0 = 不过滤，1 = 私聊，2 = 群聊（可选参数）
	sessionTypeRaw, _ := paramInt(params, "session_type")
	var sessionType int16
	if sessionTypeRaw == 1 || sessionTypeRaw == 2 {
		sessionType = int16(sessionTypeRaw)
	}

	switch {
	case hasPeerID:
		// peer_id：按对方账户精确定位私聊会话（direct_key），共享场景下各使用者各自命中自己与 peer 的会话。
		if peerID <= 0 {
			return nil, 4001, "peer_id invalid"
		}
		data, svcErr := service.SessionSearchByPeer(ownerID, peerID)
		if svcErr != nil {
			return nil, 5001, svcErr.Error()
		}
		if len(data.List) == 0 {
			return nil, 4004, "no private session with peer"
		}
		return data, 0, ""
	case hasID && strings.TrimSpace(id) != "":
		data, svcErr := service.SessionSearchByID(ownerID, id, limit, offset, sessionType)
		if svcErr != nil {
			return nil, 5001, svcErr.Error()
		}
		return data, 0, ""
	case hasKeyword && strings.TrimSpace(keyword) != "":
		data, svcErr := service.SessionSearch(ownerID, keyword, limit, offset, sessionType)
		if svcErr != nil {
			return nil, 5001, svcErr.Error()
		}
		return data, 0, ""
	default:
		data, svcErr := service.SessionListAll(ownerID, limit, offset, sessionType)
		if svcErr != nil {
			return nil, 5001, svcErr.Error()
		}
		return data, 0, ""
	}
}

func dispatchMessageHistory(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	limit, _ := paramInt(params, "limit")
	if limit <= 0 {
		limit = 1
	}
	beforeID, _ := paramInt64(params, "before_id")

	data, svcErr := service.AgentMessageHistory(ownerID, sessionID, beforeID, limit)
	if svcErr != nil {
		return nil, 5001, svcErr.Error()
	}
	return data, 0, ""
}

func dispatchMessageSearch(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	keyword, ok := paramString(params, "keyword")
	if !ok || strings.TrimSpace(keyword) == "" {
		return nil, 4001, "keyword required"
	}
	limit, _ := paramInt(params, "limit")
	if limit <= 0 {
		limit = 20
	}
	beforeID, _ := paramInt64(params, "before_id")

	data, svcErr := service.MessageSearch(ownerID, sessionID, keyword, beforeID, limit)
	if svcErr != nil {
		return nil, 5001, svcErr.Error()
	}
	return data, 0, ""
}

func dispatchGroupCreate(ownerID, agentID int64, params map[string]interface{}) (interface{}, int, string) {
	name, ok := paramString(params, "name")
	if !ok || strings.TrimSpace(name) == "" {
		return nil, 4001, "name required"
	}
	rawMemberIDs, _ := paramStringSlice(params, "member_ids")
	memberTypes, _ := paramIntSlice(params, "member_types")

	memberIDs, parseErr := parseMemberIDsFromStrings(rawMemberIDs)
	if parseErr != nil {
		return nil, 4001, parseErr.Error()
	}

	data, svcErr := service.SessionCreateGroupByAgent(ownerID, agentID, name, memberIDs, memberTypes)
	if svcErr != nil {
		return classifySessionError(svcErr)
	}
	return data, 0, ""
}

func dispatchGroupLeave(agentID, ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	data, svcErr := service.SessionLeaveByAgent(agentID, ownerID, sessionID)
	if svcErr != nil {
		return classifySessionError(svcErr)
	}
	return data, 0, ""
}

func dispatchGroupMemberAdd(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	rawMemberIDs, _ := paramStringSlice(params, "member_ids")
	memberTypes, _ := paramIntSlice(params, "member_types")

	memberIDs, parseErr := parseMemberIDsFromStrings(rawMemberIDs)
	if parseErr != nil {
		return nil, 4001, parseErr.Error()
	}

	data, svcErr := service.SessionAddMembers(ownerID, sessionID, memberIDs, memberTypes)
	if svcErr != nil {
		return classifySessionError(svcErr)
	}
	return data, 0, ""
}

func dispatchGroupMemberRemove(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	rawMemberIDs, _ := paramStringSlice(params, "member_ids")
	memberTypes, _ := paramIntSlice(params, "member_types")

	memberIDs, parseErr := parseMemberIDsFromStrings(rawMemberIDs)
	if parseErr != nil {
		return nil, 4001, parseErr.Error()
	}

	data, svcErr := service.SessionRemoveMembers(ownerID, sessionID, memberIDs, memberTypes)
	if svcErr != nil {
		return classifySessionError(svcErr)
	}
	return data, 0, ""
}

func dispatchGroupMemberRoleUpdate(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	memberID, ok := paramInt64(params, "member_id")
	if !ok || memberID <= 0 {
		return nil, 4001, "member_id required"
	}
	memberType, _ := paramInt(params, "member_type")
	role, ok := paramInt(params, "role")
	if !ok {
		return nil, 4001, "role required"
	}

	data, svcErr := service.SessionUpdateMemberRole(ownerID, sessionID, memberID, int16(memberType), int16(role))
	if svcErr != nil {
		return classifySessionError(svcErr)
	}
	return data, 0, ""
}

func dispatchGroupAllMembersMuted(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	allMembersMuted, ok := paramBool(params, "all_members_muted")
	if !ok {
		return nil, 4001, "all_members_muted required"
	}

	data, svcErr := service.SessionUpdateAllMembersMuted(ownerID, sessionID, allMembersMuted)
	if svcErr != nil {
		return classifySessionError(svcErr)
	}
	return data, 0, ""
}

func dispatchGroupMemberSpeaking(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	memberID, ok := paramInt64(params, "member_id")
	if !ok || memberID <= 0 {
		return nil, 4001, "member_id required"
	}
	memberType, _ := paramInt(params, "member_type")

	var isSpeakMuted *bool
	if v, ok := paramBool(params, "is_speak_muted"); ok {
		isSpeakMuted = &v
	}
	var canSpeakWhenAllMuted *bool
	if v, ok := paramBool(params, "can_speak_when_all_muted"); ok {
		canSpeakWhenAllMuted = &v
	}
	if isSpeakMuted == nil && canSpeakWhenAllMuted == nil {
		return nil, 4001, "at least one of is_speak_muted or can_speak_when_all_muted required"
	}

	data, svcErr := service.SessionUpdateMemberSpeaking(ownerID, sessionID, memberID, int16(memberType), isSpeakMuted, canSpeakWhenAllMuted)
	if svcErr != nil {
		return classifySessionError(svcErr)
	}
	return data, 0, ""
}

func dispatchGroupDissolve(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	data, svcErr := service.SessionDissolve(ownerID, sessionID)
	if svcErr != nil {
		return classifySessionError(svcErr)
	}
	return data, 0, ""
}

func dispatchGroupDetailRead(agentID, ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	data, svcErr := service.SessionGroupDetail(agentID, ownerID, sessionID)
	if svcErr != nil {
		return classifySessionError(svcErr)
	}
	return data, 0, ""
}

func dispatchAgentAPICreate(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	agentName, ok := paramString(params, "agent_name")
	if !ok || strings.TrimSpace(agentName) == "" {
		return nil, 4001, "agent_name required"
	}
	introduction, _ := paramString(params, "introduction")
	systemPrompt, _ := paramString(params, "system_prompt")
	avatarURL, _ := paramString(params, "avatar_url")
	agentClientType, _ := paramString(params, "agent_client_type")
	isMain, _ := paramBool(params, "is_main")

	data, ec := service.AgentCreateAPIForOwner(
		ownerID,
		agentName,
		avatarURL,
		introduction,
		systemPrompt,
		agentClientType,
		isMain,
	)
	if ec != nil {
		return nil, ec.BizCode, ec.Msg
	}
	logger.L.Infof("agent_api_create result api_key_len=%d api_key_hint=%q",
		len(data.APIKey), data.APIKeyHint)
	return data, 0, ""
}

func dispatchAgentCategoryList(ownerID int64) (interface{}, int, string) {
	data, ec := service.AgentCategoryList(ownerID)
	if ec != nil {
		return nil, ec.BizCode, ec.Msg
	}
	return data, 0, ""
}

func dispatchAgentCategoryCreate(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	name, ok := paramString(params, "name")
	if !ok || strings.TrimSpace(name) == "" {
		return nil, 4001, "name required"
	}

	parentID, hasParentID := paramInt64(params, "parent_id")
	if !hasParentID {
		parentID = 0
	}
	sortOrder, _ := paramInt(params, "sort_order")

	data, ec := service.AgentCategoryCreate(ownerID, service.AgentCategoryReq{
		ParentID:  parentID,
		Name:      name,
		SortOrder: sortOrder,
	})
	if ec != nil {
		return nil, ec.BizCode, ec.Msg
	}
	return data, 0, ""
}

func dispatchAgentCategoryUpdate(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	categoryID, ok := paramInt64(params, "category_id")
	if !ok || categoryID <= 0 {
		return nil, 4001, "category_id required"
	}
	name, ok := paramString(params, "name")
	if !ok || strings.TrimSpace(name) == "" {
		return nil, 4001, "name required"
	}

	parentID, hasParentID := paramInt64(params, "parent_id")
	if !hasParentID {
		parentID = 0
	}
	sortOrder, _ := paramInt(params, "sort_order")

	data, ec := service.AgentCategoryUpdate(ownerID, categoryID, service.AgentCategoryReq{
		ParentID:  parentID,
		Name:      name,
		SortOrder: sortOrder,
	})
	if ec != nil {
		return nil, ec.BizCode, ec.Msg
	}
	return data, 0, ""
}

func dispatchAgentCategoryAssign(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	agentID, ok := paramInt64(params, "agent_id")
	if !ok || agentID <= 0 {
		return nil, 4001, "agent_id required"
	}
	categoryID, ok := paramInt64(params, "category_id")
	if !ok {
		return nil, 4001, "category_id required"
	}

	data, ec := service.AgentAssignCategory(ownerID, agentID, categoryID)
	if ec != nil {
		return nil, ec.BizCode, ec.Msg
	}
	return data, 0, ""
}

func dispatchAgentAPIKeyRotate(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	targetAgentID, ok := paramInt64(params, "agent_id")
	if !ok || targetAgentID <= 0 {
		return nil, 4001, "agent_id required"
	}
	data, ec := service.AgentRotateAPIKey(ownerID, targetAgentID)
	if ec != nil {
		return nil, ec.BizCode, ec.Msg
	}
	return data, 0, ""
}

// parseMemberIDsFromStrings converts []string to []int64.
func parseMemberIDsFromStrings(raw []string) ([]int64, error) {
	memberIDs := make([]int64, 0, len(raw))
	for _, mid := range raw {
		v, err := strconv.ParseInt(strings.TrimSpace(mid), 10, 64)
		if err != nil {
			return nil, service.ErrInvalidMemberID
		}
		memberIDs = append(memberIDs, v)
	}
	return memberIDs, nil
}

// classifySessionError maps service errors to (nil, code, msg).
func classifySessionError(err error) (interface{}, int, string) {
	msg := err.Error()
	switch {
	case isPermissionError(err):
		return nil, 4003, msg
	default:
		return nil, 5001, msg
	}
}

func isPermissionError(err error) bool {
	switch {
	case isErr(err, service.ErrSessionPermissionDenied),
		isErr(err, service.ErrSessionRoleDenied),
		isErr(err, service.ErrSessionGroupBanned),
		isErr(err, service.ErrSessionOwnerRequired),
		isErr(err, service.ErrSessionDissolveDenied),
		isErr(err, service.ErrSessionRemoveDenied),
		isErr(err, service.ErrSessionMemberSettingDenied),
		isErr(err, service.ErrSessionRuntimeSettingDenied),
		isErr(err, service.ErrSessionSpeakingTargetDenied),
		isErr(err, service.ErrSessionMemberInviteDisabled):
		return true
	default:
		return false
	}
}

func isErr(err, target error) bool {
	return errors.Is(err, target)
}

func dispatchSendMsg(agentID, ownerID int64, params map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	content, ok := paramString(params, "content")
	if !ok || strings.TrimSpace(content) == "" {
		return nil, 4001, "content required"
	}
	msgType, _ := paramInt(params, "msg_type")
	if msgType <= 0 {
		msgType = 1
	}
	threadID, _ := paramString(params, "thread_id")
	quotedMessageID, _ := paramInt64(params, "quoted_message_id")

	if hooks.sendMessage == nil {
		return nil, 5001, "message handler unavailable"
	}

	clientMsgID, _ := paramString(params, "client_msg_id")
	if strings.TrimSpace(clientMsgID) == "" {
		clientMsgID = fmt.Sprintf("invoke_send_%d_%d", agentID, time.Now().UnixNano())
	}

	result, err := hooks.sendMessage(SendMessageReq{
		AgentID:         agentID,
		OwnerID:         ownerID,
		SessionID:       sessionID,
		Content:         content,
		MsgType:         int16(msgType),
		ClientMsgID:     clientMsgID,
		ThreadID:        threadID,
		QuotedMessageID: quotedMessageID,
	})
	if err != nil {
		return nil, 5001, err.Error()
	}
	return map[string]interface{}{
		"msg_id":     result.MsgID,
		"inbox_seq":  result.InboxSeq,
		"created_at": result.CreatedAt,
	}, 0, ""
}

func dispatchDeleteMsg(agentID, ownerID int64, params map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	msgID, ok := paramInt64(params, "msg_id")
	if !ok || msgID <= 0 {
		return nil, 4001, "msg_id required"
	}

	if hooks.deleteMsg == nil {
		return nil, 5001, "delete handler unavailable"
	}

	if err := hooks.deleteMsg(context.Background(), agentID, ownerID, DeleteMsgPayload{
		SessionID: sessionID,
		MsgID:     msgID,
	}); err != nil {
		return nil, 5001, err.Error()
	}
	return map[string]interface{}{"deleted": true}, 0, ""
}

// dispatchAgentIntroductionUpdate 更新 owner 名下某个 agent 的名字和/或文字介绍。
// agent_id 以字符串形式传入；归属、重名与长度校验复用 service.AgentUpdate。
func dispatchAgentIntroductionUpdate(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	agentID, ok := paramInt64(params, "agent_id")
	if !ok || agentID <= 0 {
		return nil, 4001, "agent_id required"
	}
	var req service.AgentUpdateReq
	if introduction, ok := paramString(params, "introduction"); ok {
		req.Introduction = &introduction
	}
	if agentName, ok := paramString(params, "agent_name"); ok {
		req.AgentName = &agentName
	}
	if req.Introduction == nil && req.AgentName == nil {
		return nil, 4001, "agent_name or introduction required"
	}

	resp, ec := service.AgentUpdate(ownerID, agentID, req)
	if ec != nil {
		return nil, ec.BizCode, ec.Msg
	}
	return map[string]interface{}{
		"agent_id":     strconv.FormatInt(agentID, 10),
		"agent_name":   resp.AgentName,
		"introduction": resp.Introduction,
	}, 0, ""
}

// callOwnerCooldown 是同一 agent 在同一会话内重复呼叫主人的冷却窗口。
const callOwnerCooldown = 45 * time.Second

// dispatchCallOwner 让 agent 呼叫「当前使用者」进入会话语音沟通。
// 注意共享语义：ownerID = 触发该 invoke 的连接 owner，
// 主人自用时是 agent 主人，共享场景下是被共享者——工具呼叫的始终是这个使用者本人。
// 流程：校验该使用者已配置语音大脑 → 冷却兜底 → 向会话发一条呼叫卡片（自动触发离线推送）。
// 使用者点开会话后由前端识别新鲜呼叫卡片自动拉起语音大脑通话。
func dispatchCallOwner(agentID, ownerID int64, params map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	sessionID = strings.TrimSpace(sessionID)

	if hooks.sendMessage == nil {
		return nil, 5001, "message handler unavailable"
	}

	// 当前使用者未配置语音大脑则无法接通（共享场景下使用者是被共享者，需在自己账户配置）。
	if _, ok := service.LoadUserVoiceBrainAgentID(ownerID); !ok {
		return nil, 4002, "使用者尚未配置语音大脑，无法接通，请先在设置中配置后再呼叫"
	}

	// 防骚扰：同一 agent + 会话在冷却窗口内只能呼叫一次。redis 不可用时放行。
	cdKey := fmt.Sprintf("call_owner:cd:%d:%s", agentID, sessionID)
	if store.RDB != nil {
		acquired, err := store.RDB.SetNX(context.Background(), cdKey, "1", callOwnerCooldown).Result()
		if err == nil && !acquired {
			return nil, 4290, "呼叫太频繁，请稍后再试"
		}
	}

	// 取 agent 名用于卡片标题。
	agentName := "Agent"
	var agent model.Agent
	if err := store.DB.Select("agent_name").First(&agent, agentID).Error; err == nil {
		if n := strings.TrimSpace(agent.AgentName); n != "" {
			agentName = n
		}
	}

	// 构造呼叫卡片：[fallback](grix://card/call_owner?d={json})
	payload, _ := json.Marshal(map[string]interface{}{
		"session_id": sessionID,
		"agent_id":   strconv.FormatInt(agentID, 10),
		"agent_name": agentName,
		"ts":         time.Now().UnixMilli(),
	})
	content := fmt.Sprintf("[📞 %s 请求与你语音通话](grix://card/call_owner?d=%s)",
		agentName, url.QueryEscape(string(payload)))

	clientMsgID := fmt.Sprintf("call_owner_%d_%d", agentID, time.Now().UnixNano())
	result, err := hooks.sendMessage(SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		Content:     content,
		MsgType:     1,
		ClientMsgID: clientMsgID,
	})
	if err != nil {
		return nil, 5001, err.Error()
	}
	logger.L.Infof("call_owner sent agent=%d owner=%d session=%s msg_id=%v", agentID, ownerID, sessionID, result.MsgID)
	return map[string]interface{}{
		"msg_id":     result.MsgID,
		"created_at": result.CreatedAt,
	}, 0, ""
}

// dispatchSessionSend 以主人身份把消息发进指定会话。
// 校验主人确实是该会话成员后，用 ModeCaller(sender=owner) 发送。
func dispatchSessionSend(agentID, ownerID int64, params map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	sessionID = strings.TrimSpace(sessionID)
	content, ok := paramString(params, "content")
	if !ok || strings.TrimSpace(content) == "" {
		return nil, 4001, "content required"
	}
	quotedMessageID, quotedMessageIDValid := paramInt64(params, "quoted_message_id")
	if rawQuotedMessageID, provided := params["quoted_message_id"]; provided {
		if numeric, ok := rawQuotedMessageID.(float64); ok && numeric != math.Trunc(numeric) {
			quotedMessageIDValid = false
		}
		if !quotedMessageIDValid || quotedMessageID <= 0 {
			return nil, 4001, "quoted_message_id invalid"
		}
	}
	if hooks.sendMessage == nil {
		return nil, 5001, "message handler unavailable"
	}

	// 主人必须是该会话成员，避免借此往任意会话发主人消息。
	var memberCount int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, ownerID).
		Count(&memberCount).Error; err != nil {
		return nil, 5001, err.Error()
	}
	if memberCount == 0 {
		return nil, 4003, "owner is not a member of the session"
	}

	// 注意：不再校验 agent 是否为目标会话成员。派发回调（report_dispatch_result）
	// 必须以主人身份回写——以 agent 自己身份发送无法触发对端 agent 的引用唤醒，
	// 因此被派发 agent 即使同在回调会话中，也必须走 session_send。
	// 是否允许 impersonate 由 ScopeSessionSend 权限控制，权限不足时上层直接报错，
	// 由主人开通权限，不做成员身份层面的兼容或兜底。

	// Hard same-session check: quoted target must exist in the callback session.
	if quotedMessageID > 0 {
		var quoteCount int64
		if err := store.DB.Model(&model.Message{}).
			Where("session_id = ? AND msg_id = ? AND is_deleted = false AND is_revoked = false", sessionID, quotedMessageID).
			Count(&quoteCount).Error; err != nil {
			return nil, 5001, err.Error()
		}
		if quoteCount == 0 {
			return nil, 4001, "quoted_message_id not found in session"
		}
	}

	result, err := sendAsOwner(agentID, agentID, ownerID, sessionID, content, quotedMessageID, hooks)
	if err != nil {
		return nil, 5001, err.Error()
	}
	return map[string]interface{}{
		"msg_id":     result.MsgID,
		"inbox_seq":  result.InboxSeq,
		"created_at": result.CreatedAt,
	}, 0, ""
}

func dispatchSearchFavoriteSessions(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	keyword, _ := paramString(params, "keyword")
	limit, _ := paramInt(params, "limit")
	if limit <= 0 {
		limit = 20
	}
	offset, _ := paramInt(params, "offset")

	data, err := service.ListFavoriteSessionsForAgent(ownerID, keyword, limit, offset)
	if err != nil {
		return nil, 5001, err.Error()
	}
	return data, 0, ""
}

// sendAsOwner 以主人身份(ModeCaller, sender=owner)向会话发送一条文本消息。
// agentID 是消息挂靠的 agent（会话所属 / 目标）；originAgentID 是实际发出这条消息的
// agent，会写进 extra.origin_agent_id 供路由层把它自己排除在唤醒目标外。
// 二者不总是同一个：派发任务时 agentID 是被派发的目标，origin 必须是调用方，
// 否则目标会被当成"发出者"跳过，任务永远不投递。originAgentID<=0 时不打标。
func sendAsOwner(agentID, originAgentID, ownerID int64, sessionID, content string, quotedMessageID int64, hooks agentInvokeHooks) (*SendMessageResult, error) {
	clientMsgID := fmt.Sprintf("invoke_owner_%d_%d", ownerID, time.Now().UnixNano())
	var extra json.RawMessage
	if originAgentID > 0 {
		extra, _ = json.Marshal(map[string]string{"origin_agent_id": fmt.Sprintf("%d", originAgentID)})
	}
	return hooks.sendMessage(SendMessageReq{
		IdentityMode:    agentmsg.ModeCaller,
		AgentID:         agentID,
		OwnerID:         ownerID,
		CallerID:        ownerID,
		SessionID:       sessionID,
		Content:         content,
		MsgType:         1,
		ClientMsgID:     clientMsgID,
		QuotedMessageID: quotedMessageID,
		Extra:           extra,
	})
}

// dispatchWidgetVisitorBan 让有 widget.visitor.ban scope 的 agent 封禁一个 widget 访客会话。
// 复用 service.WidgetSessionBan：除会话封禁（status=3）外，还会把该会话最近 init IP
// 写入 owner 全局 IP 封禁（默认 7 天过期），与 App 内手动 ban 行为一致。
func dispatchWidgetVisitorBan(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	dto, err := service.WidgetSessionBan(service.WidgetSessionStatusUpdateInput{
		OwnerUserID: ownerID,
		SessionID:   strings.TrimSpace(sessionID),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrWidgetSiteInvalidInput):
			return nil, 4001, err.Error()
		case errors.Is(err, service.ErrWidgetSessionNotOwned):
			return nil, 4004, err.Error()
		default:
			return nil, 5001, err.Error()
		}
	}
	return map[string]interface{}{
		"session_id":  dto.SessionID,
		"visitor_key": dto.VisitorKey,
		"status":      dto.Status,
	}, 0, ""
}
