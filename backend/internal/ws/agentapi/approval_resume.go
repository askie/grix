package agentapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// AccessRedispatchParams names the first blocked message to redispatch after access approval.
type AccessRedispatchParams struct {
	AgentID      int64
	SessionID    string
	TriggerMsgID int64
}

var accessRedispatchFn func(AccessRedispatchParams)

// SetAccessRedispatchHandler wires handler-layer redispatch (avoids agentapi → handler import).
func SetAccessRedispatchHandler(fn func(AccessRedispatchParams)) {
	accessRedispatchFn = fn
}

type scopeResumeContext struct {
	SessionID   string
	OwnerID     int64
	SenderID    int64
	SessionType int16
	CanWake     bool
}

func appendAgentScopeIDempotent(agentID int64, scope string) error {
	scope = strings.TrimSpace(scope)
	if agentID <= 0 || scope == "" {
		return fmt.Errorf("agent_id and scope required")
	}
	var count int64
	if err := store.DB.Model(&model.AgentAPIScope{}).
		Where("agent_id = ? AND scope = ?", agentID, scope).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return store.DB.Create(&model.AgentAPIScope{AgentID: agentID, Scope: scope}).Error
}

func (m *Manager) lookupUniqueActiveRunForAgent(agentID int64) (*ActiveRunSnapshot, bool) {
	if m == nil || agentID <= 0 {
		return nil, false
	}
	m.runsMu.Lock()
	defer m.runsMu.Unlock()
	var only *activeAgentRun
	count := 0
	for _, run := range m.runs {
		if run == nil || run.AgentID != agentID {
			continue
		}
		count++
		if count > 1 {
			return nil, false
		}
		cp := *run
		only = &cp
	}
	if count != 1 || only == nil {
		return nil, false
	}
	return snapshotActiveRun(only), true
}

func (m *Manager) captureScopeResumeContext(agentID int64) scopeResumeContext {
	run, ok := m.lookupUniqueActiveRunForAgent(agentID)
	if !ok || run == nil {
		return scopeResumeContext{CanWake: false}
	}
	return scopeResumeContext{
		SessionID:   strings.TrimSpace(run.SessionID),
		OwnerID:     run.OwnerID,
		SenderID:    run.SenderID,
		SessionType: run.SessionType,
		CanWake:     strings.TrimSpace(run.SessionID) != "" && run.OwnerID > 0,
	}
}

func (m *Manager) resumeAfterScopeApproval(agentID, ownerID int64, scope string, resume scopeResumeContext, lang string) (summary string) {
	if err := appendAgentScopeIDempotent(agentID, scope); err != nil {
		logger.L.Warnf("scope approval grant failed agent=%d scope=%s: %v", agentID, scope, err)
		return tooli18n.T(lang, "scope_grant_failed")
	}
	label := agentscopeScopeLabel(lang, scope)
	if resume.CanWake && resume.SessionID != "" {
		if err := m.dispatchScopeWakeEvent(agentID, ownerID, resume, label); err != nil {
			logger.L.Warnf("scope approval wake failed agent=%d session=%s: %v", agentID, resume.SessionID, err)
			return tooli18n.Tf(lang, "scope_approved_retry_manual", label)
		}
		return tooli18n.Tf(lang, "scope_approved_resumed", label)
	}
	return tooli18n.Tf(lang, "scope_approved_retry_manual", label)
}

func (m *Manager) resumeAfterAccessApproval(agentID int64, sessionID string, triggerMsgID int64, lang string, senderLabel string) string {
	if triggerMsgID > 0 && accessRedispatchFn != nil {
		accessRedispatchFn(AccessRedispatchParams{
			AgentID:      agentID,
			SessionID:    strings.TrimSpace(sessionID),
			TriggerMsgID: triggerMsgID,
		})
	}
	return tooli18n.Tf(lang, "access_approved", senderLabel)
}

func agentscopeScopeLabel(lang, scope string) string {
	for _, item := range agentscope.AllowedScopeItems(lang) {
		if item.Scope == scope {
			if strings.TrimSpace(item.Label) != "" {
				return item.Label
			}
			break
		}
	}
	return scope
}

func (m *Manager) dispatchScopeWakeEvent(agentID, ownerID int64, resume scopeResumeContext, scopeLabel string) error {
	if m == nil || m.sendFn == nil || agentID <= 0 || ownerID <= 0 || resume.SessionID == "" {
		return fmt.Errorf("scope wake unavailable")
	}
	lang := ownerCardLanguage(ownerID)
	body := tooli18n.Tf(lang, "scope_wake_message", scopeLabel)
	clientMsgID := fmt.Sprintf("scope_approval_wake_%d_%d", agentID, time.Now().UnixMilli())

	var visibleTo []int64
	if resume.SessionType == model.SessionTypeGroup {
		visibleTo = []int64{ownerID}
	}

	sendResult, err := m.sendFn(context.Background(), SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   resume.SessionID,
		ClientMsgID: clientMsgID,
		MsgType:     1,
		Content:     body,
		VisibleTo:   visibleTo,
	})
	if err != nil {
		return err
	}
	if sendResult == nil || sendResult.MsgID <= 0 {
		return fmt.Errorf("scope wake message not persisted")
	}

	eventOwnerID := resume.OwnerID
	if resume.SessionType == model.SessionTypeGroup {
		eventOwnerID = ownerID
	}
	eventType := "user_chat"
	if resume.SessionType == model.SessionTypeGroup {
		eventType = "group_mention"
	}
	evt := DelegateEventPayload{
		EventID:     fmt.Sprintf("%s:%d:%d:%d", resume.SessionID, ownerID, agentID, sendResult.MsgID),
		EventType:   eventType,
		AgentID:     agentID,
		OwnerID:     eventOwnerID,
		SessionID:   resume.SessionID,
		SessionType: resume.SessionType,
		MsgID:       sendResult.MsgID,
		SenderID:    ownerID,
		Content:     body,
		CreatedAt:   time.Now().UnixMilli(),
	}
	if !m.PushDelegateEvent(evt) {
		return fmt.Errorf("scope wake delegate dispatch failed")
	}
	m.resumeLiveActivity(ownerID, agentID, resume.SessionID)
	return nil
}
