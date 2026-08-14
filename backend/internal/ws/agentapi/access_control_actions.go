package agentapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/claudeaccess"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// ensureAccessControlActor 校验 agentID 指向一个有效的 API 接入 agent，并且**确实属于 ownerID**。
// 归属校验对 agent 共享场景是必须的：被共享者 B 通过 A 的 agent X 调 access_control 时，
// conn.ownerID = B（不是 A），若此处不比对 X.OwnerID，B 就能改 X 的 access policy、批准任意配对码，
// 等价于接管 A 的接入权。
func ensureAccessControlActor(agentID, ownerID int64) error {
	if agentID <= 0 {
		return errors.New("agent_id required")
	}
	if ownerID <= 0 {
		return errors.New("owner_id required")
	}
	var agent model.Agent
	if err := store.DB.Select("id,owner_id,provider_type,agent_client_type,status").First(&agent, agentID).Error; err != nil {
		return err
	}
	if agent.Status == model.AgentStatusDeleted {
		return errors.New("agent is deleted")
	}
	if agent.ProviderType != model.AgentProviderAPI {
		return errors.New("access control requires an api agent actor")
	}
	if agent.OwnerID != ownerID {
		return errors.New("agent not owned by you")
	}
	return nil
}

func dispatchAccessControl(agentID, ownerID int64, params map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	verb, ok := paramString(params, "verb")
	if !ok || strings.TrimSpace(verb) == "" {
		return nil, 4001, "verb required"
	}

	payload := map[string]interface{}{}
	if rawPayload, exists := params["payload"]; exists {
		typedPayload, ok := rawPayload.(map[string]interface{})
		if !ok {
			return nil, 4001, "payload invalid"
		}
		payload = typedPayload
	}

	switch strings.TrimSpace(verb) {
	case "status_read":
		return dispatchAccessStatusRead(agentID, ownerID)
	case "policy_set":
		return dispatchAccessPolicySet(agentID, ownerID, payload)
	case "pair_approve":
		return dispatchAccessPairApprove(agentID, ownerID, payload, hooks)
	case "pair_deny":
		return dispatchAccessPairDeny(agentID, ownerID, payload, hooks)
	case "sender_allow":
		return dispatchAccessSenderAllow(agentID, ownerID, payload)
	case "sender_remove":
		return dispatchAccessSenderRemove(agentID, ownerID, payload)
	default:
		return nil, 4001, "verb invalid"
	}
}

func dispatchAccessStatusRead(agentID, ownerID int64) (interface{}, int, string) {
	if err := ensureAccessControlActor(agentID, ownerID); err != nil {
		return nil, 4003, err.Error()
	}
	status, err := claudeaccess.GetStatus(context.Background(), agentID)
	if err != nil {
		return nil, 5001, err.Error()
	}
	return status, 0, "ok"
}

func dispatchAccessPolicySet(agentID, ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	if err := ensureAccessControlActor(agentID, ownerID); err != nil {
		return nil, 4003, err.Error()
	}
	policy, ok := paramString(params, "policy")
	if !ok || strings.TrimSpace(policy) == "" {
		return nil, 4001, "policy required"
	}
	status, err := claudeaccess.SetPolicy(context.Background(), agentID, policy)
	if err != nil {
		return nil, 5001, err.Error()
	}
	return status, 0, "ok"
}

func dispatchAccessPairApprove(agentID, ownerID int64, params map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	if err := ensureAccessControlActor(agentID, ownerID); err != nil {
		return nil, 4003, err.Error()
	}
	code, ok := paramString(params, "code")
	if !ok || strings.TrimSpace(code) == "" {
		return nil, 4001, "code required"
	}
	result, err := claudeaccess.ApprovePairing(context.Background(), agentID, code)
	if err != nil {
		if strings.Contains(err.Error(), "pairing code not found or expired") {
			return nil, 4004, err.Error()
		}
		return nil, 5001, err.Error()
	}
	result.PairingNoticeSent = sendAccessControlNotice(hooks, agentID, ownerID, result.SessionID, fmt.Sprintf(
		"%s",
		claudeaccess.BuildStatusCardMarkdown("Paired! You can start chatting with the agent now.", claudeaccess.StatusSuccess, ""),
	), "agent_access_pair_ok")
	return result, 0, "ok"
}

func dispatchAccessPairDeny(agentID, ownerID int64, params map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	if err := ensureAccessControlActor(agentID, ownerID); err != nil {
		return nil, 4003, err.Error()
	}
	code, ok := paramString(params, "code")
	if !ok || strings.TrimSpace(code) == "" {
		return nil, 4001, "code required"
	}
	result, err := claudeaccess.DenyPairing(context.Background(), agentID, code)
	if err != nil {
		if strings.Contains(err.Error(), "pairing code not found or expired") {
			return nil, 4004, err.Error()
		}
		return nil, 5001, err.Error()
	}
	summary := fmt.Sprintf(
		"Pairing request %s was denied. Ask the sender to request a new pairing code if you still need access.",
		result.Code,
	)
	result.PairingNoticeSent = sendAccessControlNotice(
		hooks,
		agentID,
		ownerID,
		result.SessionID,
		claudeaccess.BuildStatusCardMarkdown(summary, claudeaccess.StatusWarning, result.Code),
		"agent_access_pair_denied",
	)
	return result, 0, "ok"
}

func dispatchAccessSenderAllow(agentID, ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	if err := ensureAccessControlActor(agentID, ownerID); err != nil {
		return nil, 4003, err.Error()
	}
	senderID, ok := paramString(params, "sender_id")
	if !ok || strings.TrimSpace(senderID) == "" {
		return nil, 4001, "sender_id required"
	}
	result, err := claudeaccess.AllowSender(context.Background(), agentID, senderID)
	if err != nil {
		return nil, 5001, err.Error()
	}
	return result, 0, "ok"
}

func dispatchAccessSenderRemove(agentID, ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	if err := ensureAccessControlActor(agentID, ownerID); err != nil {
		return nil, 4003, err.Error()
	}
	senderID, ok := paramString(params, "sender_id")
	if !ok || strings.TrimSpace(senderID) == "" {
		return nil, 4001, "sender_id required"
	}
	result, err := claudeaccess.RemoveSender(context.Background(), agentID, senderID)
	if err != nil {
		return nil, 5001, err.Error()
	}
	return result, 0, "ok"
}

func sendAccessControlNotice(hooks agentInvokeHooks, agentID, ownerID int64, sessionID, content, prefix string) bool {
	if hooks.sendMessage == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(content) == "" {
		return false
	}
	if _, err := hooks.sendMessage(SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ClientMsgID: fmt.Sprintf("%s_%d_%d", prefix, agentID, time.Now().UnixMilli()),
		MsgType:     1,
		Content:     content,
	}); err != nil {
		return false
	}
	return true
}
