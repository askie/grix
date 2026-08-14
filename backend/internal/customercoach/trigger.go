package customercoach

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"gorm.io/gorm"
)

const (
	EventTypeCustomerCoachSnapshot = "customer_coach_snapshot"
	coachRecentActivityWindow      = 2 * time.Hour
)

var (
	dispatchCommandDelegateEvent = wsagentapi.DispatchCommandDelegateEventWithContext
	ensureCustomerAutoDelegate   = apiservice.EnsureAutoDelegateForPrivateSession
	resolveCustomerAutoDelegate  = apiservice.ResolveAutoDelegateAgentID
)

func TriggerOnUserOpen(ctx context.Context, userID int64, source string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if userID <= 0 {
		return fmt.Errorf("invalid user id")
	}
	if !IsCoachTriggerAllowed(userID) {
		return nil
	}
	settings, err := systemsetting.GetAuthSettingsWithContext(ctx)
	if err != nil {
		return err
	}
	customerUserID := settings.AutoAddCustomerUserID
	if customerUserID <= 0 || customerUserID == userID {
		return nil
	}

	sessionID, err := findCustomerSession(ctx, userID, customerUserID)
	if err != nil {
		return err
	}
	if sessionID == "" {
		logger.L.Infof("customer coach skipped: customer session missing user=%d customer=%d", userID, customerUserID)
		return nil
	}
	recentActive, err := hasRecentCustomerSessionActivity(ctx, sessionID, userID, customerUserID)
	if err != nil {
		return err
	}
	if recentActive {
		logger.L.Infof(
			"customer coach skipped: recent customer session activity user=%d customer=%d session=%s",
			userID,
			customerUserID,
			sessionID,
		)
		return nil
	}

	snapshot, err := BuildSnapshot(ctx, userID, source, "client_open")
	if err != nil {
		return err
	}
	if ShouldSkipCoachDispatch(snapshot) {
		logger.L.Infof(
			"customer coach skipped: onboarding complete user=%d customer=%d agents=%d multi_agent_groups=%d voice_calls=%d",
			userID,
			customerUserID,
			snapshot.Overview.AgentTotal,
			snapshot.Sessions.MultiAgentGroups,
			snapshot.Usage.VoiceCallCount,
		)
		return nil
	}
	if !acquireCoachDispatch(ctx, userID, snapshot) {
		logger.L.Infof(
			"customer coach skipped: dispatch gate user=%d customer=%d missing=%s",
			userID,
			customerUserID,
			strings.Join(missingCoachSteps(snapshot), ","),
		)
		return nil
	}

	ensureCustomerAutoDelegate(sessionID, userID, customerUserID, 1)

	customerAgentID, ok, err := resolveCustomerAutoDelegate(customerUserID)
	if err != nil {
		return err
	}
	if !ok || customerAgentID <= 0 {
		logger.L.Infof("customer coach skipped: customer auto delegate missing customer=%d user=%d", customerUserID, userID)
		return nil
	}

	markdown := RenderMarkdown(snapshot)
	content := buildInternalTask(markdown)

	evt := wsagentapi.DelegateEventPayload{
		EventID:     fmt.Sprintf("customer_coach:%d:%s:%d", userID, normalizeEventIDPart(source), time.Now().UnixNano()),
		EventType:   EventTypeCustomerCoachSnapshot,
		AgentID:     customerAgentID,
		OwnerID:     customerUserID,
		SessionID:   sessionID,
		SessionType: model.SessionTypeDirect,
		SenderID:    customerUserID,
		MsgType:     1,
		Content:     content,
		Command:     true,
		CreatedAt:   time.Now().UnixMilli(),
	}
	if !dispatchCommandDelegateEvent(ctx, evt) {
		logger.L.Infof(
			"customer coach skipped: dispatch unavailable user=%d customer=%d agent=%d source=%s",
			userID,
			customerUserID,
			customerAgentID,
			source,
		)
		return nil
	}
	logger.L.Infof(
		"customer coach dispatched user=%d customer=%d agent=%d session=%s source=%s",
		userID,
		customerUserID,
		customerAgentID,
		sessionID,
		source,
	)
	return nil
}

func buildInternalTask(markdown string) string {
	return strings.TrimSpace(`你收到了一份 Grix 用户状态快照。这不是用户消息，不要原样复述快照。
请根据你的记忆、提示词和该用户历史上下文，自主判断是否需要给用户发一条新手引导消息。
发给用户的只能是自然客服口吻的对话文字，每次只引导一个下一步动作。
严禁把任何分析、推理、决策过程发给用户，例如“快照显示……”“按引导规则……”“用户 N 小时后再次打开客户端……”这类内容一个字都不能出现；也不要向用户提及快照、检测、规则等内部概念。
如果不需要引导，必须只返回固定命令 /no_reply，不要返回“选择沉默”“无需引导”之类的说明。

<snapshot_markdown>
` + strings.TrimSpace(markdown) + `
</snapshot_markdown>`)
}

func findCustomerSession(ctx context.Context, userID, customerUserID int64) (string, error) {
	if store.DB == nil {
		return "", fmt.Errorf("db not initialized")
	}
	directKey := buildDirectKey(userID, customerUserID, 1)
	var session model.Session
	if err := store.DB.WithContext(ctx).
		Select("session_id").
		Where("direct_key = ? AND session_type = ? AND is_deleted = ?", directKey, model.SessionTypeDirect, false).
		Order("updated_at DESC").
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(session.SessionID), nil
}

func hasRecentCustomerSessionActivity(ctx context.Context, sessionID string, userID, customerUserID int64) (bool, error) {
	if store.DB == nil {
		return false, fmt.Errorf("db not initialized")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || userID <= 0 || customerUserID <= 0 {
		return false, nil
	}
	cutoff := time.Now().UTC().Add(-coachRecentActivityWindow)
	var count int64
	if err := store.DB.WithContext(ctx).
		Model(&model.Message{}).
		Where("session_id = ?", sessionID).
		Where("sender_id = ? AND sender_type = ?", userID, int16(1)).
		Where("created_at >= ?", cutoff).
		Where("is_deleted = ? AND is_revoked = ?", false, false).
		Limit(1).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func buildDirectKey(userID, peerID int64, peerType int16) string {
	typeA := int16(1)
	idA := userID
	typeB := peerType
	idB := peerID

	if typeA > typeB || (typeA == typeB && idA > idB) {
		typeA, typeB = typeB, typeA
		idA, idB = idB, idA
	}
	return fmt.Sprintf("d:%d:%d|%d:%d", typeA, idA, typeB, idB)
}

func normalizeEventIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
