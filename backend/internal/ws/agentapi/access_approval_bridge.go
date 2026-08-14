package agentapi

// 群访问审批桥：陌生人在群里 @agent 被门禁拦下后，后端直接向主人发一张标准
// agent_question 审批卡（发在主人↔agent 的专用「访问审批」私聊线程，群内对
// 陌生人完全无感）。主人点「允许/拒绝」的卡片回传由 delegate 拦截器在服务端
// 就地消费——批准即写入访问名单，全程不经过 agent、无工具调用。

import (
	"context"
	"fmt"
	"strings"

	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/claudeaccess"
	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

const (
	accessApprovalRequestPrefix = "access:"
	accessApprovalThreadKey     = "access-approval"
	accessApprovalThreadTitle   = "访问审批"
)

// NotifyGroupAccessApproval 向 agent 主人发送群访问审批卡，返回是否成功送出。
// code 是 claudeaccess 为该申请生成的待批码（同一申请人未过期期间门禁层返回
// pairing_pending，不会重复触发本函数）。调用方（门禁层）依据返回值决定是否
// 回滚待批记录——卡没送出去时必须回滚，否则 pending 挡住重试而主人毫不知情。
func NotifyGroupAccessApproval(agentID, ownerID, senderID int64, groupSessionID, code string) bool {
	m := GetGlobal()
	if m == nil {
		logger.L.Warnf("access approval notify skipped: agentapi manager unavailable agent=%d sender=%d", agentID, senderID)
		return false
	}
	return m.notifyGroupAccessApproval(agentID, ownerID, senderID, groupSessionID, code)
}

func (m *Manager) notifyGroupAccessApproval(agentID, ownerID, senderID int64, groupSessionID, code string) bool {
	if m.sendFn == nil || agentID <= 0 || ownerID <= 0 || senderID <= 0 || strings.TrimSpace(code) == "" {
		return false
	}

	senderLabel := accessApprovalSenderLabel(senderID)
	groupLabel := accessApprovalGroupLabel(groupSessionID)

	resp, err := service.SessionCreateForAgentBinding(ownerID, agentID, accessApprovalThreadKey, accessApprovalThreadTitle)
	if err != nil || resp == nil || strings.TrimSpace(resp.SessionID) == "" {
		logger.L.Warnf("access approval thread resolve failed agent=%d owner=%d: %v", agentID, ownerID, err)
		return false
	}

	requestID := fmt.Sprintf("%s%d:%s", accessApprovalRequestPrefix, agentID, code)
	payload := map[string]any{
		"request_id":  requestID,
		"mode":        "form",
		"message":     fmt.Sprintf("%s 请求在群聊「%s」使用本 agent。", senderLabel, groupLabel),
		"footer_text": "申请 10 分钟内有效，过期后让对方重新在群里 @ 一次即可重新发起。允许后对方进入访问名单，可在群聊 @ 本 agent；私聊始终仅限主人。",
		"questions": []map[string]any{
			{
				"index":   1,
				"header":  "访问申请",
				"prompt":  fmt.Sprintf("是否允许 %s 使用本 agent？", senderLabel),
				"options": []string{"允许", "拒绝"},
			},
		},
	}
	content := buildLocalGrixCardLink("[Agent Question] 访问申请", "agent_question", payload)

	if _, err := m.sendFn(context.Background(), SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   strings.TrimSpace(resp.SessionID),
		ClientMsgID: fmt.Sprintf("agent_access_approval_%d_%s", agentID, strings.TrimSpace(code)),
		MsgType:     1,
		Content:     content,
	}); err != nil {
		logger.L.Warnf("access approval card send failed agent=%d owner=%d sender=%d: %v", agentID, ownerID, senderID, err)
		return false
	}
	return true
}

// tryHandleAccessApprovalReply 消费主人在审批卡上的选择。命中即吞掉事件（不再投给
// agent）。该拦截器注册在最前，request_id 前缀保证不与其他 question 流冲突。
func (m *Manager) tryHandleAccessApprovalReply(evt DelegateEventPayload) bool {
	reply, matched, err := grixactions.ParseQuestionReply(evt.Content)
	if !matched {
		return false
	}
	if err != nil {
		// 解析失败拿不到 request_id：若内容明显属于 access 命名空间，就地吞掉并提示，
		// 不能漏给 gemini/claude 的 question 流去报「无待处理问题」误导主人。
		if strings.Contains(evt.Content, "access%3A") || strings.Contains(evt.Content, `"access:`) || strings.Contains(evt.Content, "request_id=access:") {
			return m.sendAccessApprovalStatusCard(evt, "warning", tooli18n.T(ownerCardLanguage(evt.OwnerID), "access_reply_unparseable"), "")
		}
		return false
	}
	requestID := strings.TrimSpace(reply.RequestID)
	if !strings.HasPrefix(requestID, accessApprovalRequestPrefix) {
		return false
	}

	var agentID int64
	var code string
	rest := strings.TrimPrefix(requestID, accessApprovalRequestPrefix)
	if idx := strings.IndexByte(rest, ':'); idx > 0 {
		fmt.Sscanf(rest[:idx], "%d", &agentID)
		code = strings.TrimSpace(rest[idx+1:])
	}
	lang := ownerCardLanguage(evt.OwnerID)
	if agentID <= 0 || code == "" || agentID != evt.AgentID {
		return m.sendAccessApprovalStatusCard(evt, "warning", tooli18n.T(lang, "access_request_unrecognized"), requestID)
	}

	// 只有 agent 主人能批。审批卡只发在主人线程，此处为纵深校验。
	var agent model.Agent
	if dbErr := store.DB.Select("id,owner_id").First(&agent, agentID).Error; dbErr != nil || agent.OwnerID != evt.SenderID {
		return m.sendAccessApprovalStatusCard(evt, "warning", tooli18n.T(lang, "access_owner_only"), requestID)
	}

	if strings.TrimSpace(reply.Action) == "cancel" {
		return m.sendAccessApprovalStatusCard(evt, "info", tooli18n.T(lang, "access_cancelled"), requestID)
	}

	// 只认选项值的精确匹配：子串匹配会把「不允许」误判成允许。
	decision := ""
	for _, answer := range questionReplyAnswers(reply.Response) {
		switch strings.TrimSpace(answer) {
		case "允许":
			decision = "allow"
		case "拒绝":
			decision = "deny"
		}
		if decision != "" {
			break
		}
	}

	switch decision {
	case "allow":
		result, approveErr := claudeaccess.ApprovePairing(context.Background(), agentID, code)
		if approveErr != nil {
			return m.sendAccessApprovalStatusCard(evt, "warning",
				tooli18n.T(lang, "access_expired_or_processed_hint"), requestID)
		}
		return m.sendAccessApprovalStatusCard(evt, "success",
			tooli18n.Tf(lang, "access_approved", accessApprovalSenderLabel(parseAccessSenderID(result.SenderID))), requestID)
	case "deny":
		if _, denyErr := claudeaccess.DenyPairing(context.Background(), agentID, code); denyErr != nil {
			return m.sendAccessApprovalStatusCard(evt, "warning", tooli18n.T(lang, "access_expired_or_processed"), requestID)
		}
		return m.sendAccessApprovalStatusCard(evt, "success", tooli18n.T(lang, "access_denied"), requestID)
	default:
		return m.sendAccessApprovalStatusCard(evt, "warning", tooli18n.T(lang, "access_choose_option"), requestID)
	}
}

func (m *Manager) sendAccessApprovalStatusCard(evt DelegateEventPayload, status, summary, requestID string) bool {
	if m.sendFn == nil {
		return true
	}
	reply := buildAgentStatusCardReply(map[string]any{
		"category":     "access",
		"status":       status,
		"summary":      summary,
		"reference_id": requestID,
	})
	if strings.TrimSpace(reply.content) == "" {
		return true
	}
	if _, err := m.sendFn(context.Background(), SendMessageReq{
		AgentID:         evt.AgentID,
		OwnerID:         evt.OwnerID,
		SessionID:       evt.SessionID,
		ClientMsgID:     fmt.Sprintf("agent_access_approval_result_%d_%d", evt.AgentID, evt.MsgID),
		MsgType:         1,
		Content:         reply.content,
		QuotedMessageID: evt.MsgID,
	}); err != nil {
		logger.L.Warnf("access approval status card send failed agent=%d session=%s: %v", evt.AgentID, evt.SessionID, err)
	}
	return true
}

func accessApprovalSenderLabel(senderID int64) string {
	if senderID <= 0 {
		return "对方"
	}
	var user model.User
	if err := store.DB.Select("id,nickname").First(&user, senderID).Error; err == nil {
		if nickname := strings.TrimSpace(user.Nickname); nickname != "" {
			return fmt.Sprintf("%s(%d)", nickname, senderID)
		}
	}
	return fmt.Sprintf("用户 %d", senderID)
}

func accessApprovalGroupLabel(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "群聊"
	}
	var session model.Session
	if err := store.DB.Select("session_id,group_name").Where("session_id = ?", sessionID).First(&session).Error; err == nil {
		if name := strings.TrimSpace(session.GroupName); name != "" {
			return name
		}
	}
	return sessionID
}

func parseAccessSenderID(raw string) int64 {
	var id int64
	fmt.Sscanf(strings.TrimSpace(raw), "%d", &id)
	return id
}

// questionReplyAnswers 展开 question 回传的 response 结构（single: {type,value}；
// map: {type,entries:[{key,value}]}），返回全部答案文本。
func questionReplyAnswers(response map[string]any) []string {
	if len(response) == 0 {
		return nil
	}
	var answers []string
	if value := strings.TrimSpace(fmt.Sprint(response["value"])); value != "" && value != "<nil>" {
		answers = append(answers, value)
	}
	if rawEntries, ok := response["entries"].([]any); ok {
		for _, rawEntry := range rawEntries {
			entry, ok := rawEntry.(map[string]any)
			if !ok {
				continue
			}
			if value := strings.TrimSpace(fmt.Sprint(entry["value"])); value != "" && value != "<nil>" {
				answers = append(answers, value)
			}
		}
	}
	return answers
}
