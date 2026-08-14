package handler

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
)

var (
	errLocalInferenceAgentNotInSession = errors.New("agent not in session")
	errLocalInferenceAgentUnavailable  = errors.New("agent is not available for local inference")
	errLocalInferenceTriggerInvalid    = errors.New("invalid trigger message")
)

func validateSessionSpeakTrigger(
	ctx context.Context,
	sessionID string,
	memberID int64,
	memberType int16,
) error {
	return sessionguard.ValidateSpeakPermission(
		ctx,
		nil,
		strings.TrimSpace(sessionID),
		memberID,
		memberType,
	)
}

func validateHumanSpeakTrigger(ctx context.Context, sessionID string, userID int64) error {
	return validateSessionSpeakTrigger(ctx, sessionID, userID, 1)
}

func sendStreamErrorToConn(
	conn ConnInterface,
	seq int64,
	sessionID string,
	senderID int64,
	code int,
	msg string,
) {
	if conn == nil {
		return
	}
	conn.SendPayload(protocol.CmdStreamError, seq, protocol.StreamErrorPayload{
		MsgID:     0,
		SessionID: strings.TrimSpace(sessionID),
		SenderID:  senderID,
		ErrorCode: code,
		ErrorMsg:  msg,
		CreatedAt: time.Now().UnixMilli(),
	})
}

func validateLocalInferenceTarget(
	ctx context.Context,
	sessionID string,
	requesterID int64,
	agentID int64,
	triggerMsgID int64,
) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || requesterID <= 0 || agentID <= 0 || triggerMsgID <= 0 {
		return errLocalInferenceTriggerInvalid
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var agentRow struct {
		ProviderType  int16  `gorm:"column:provider_type"`
		LocalEndpoint string `gorm:"column:local_endpoint"`
		Status        int16  `gorm:"column:status"`
	}
	if err := store.DB.WithContext(ctx).
		Table("session_members AS sm").
		Select("a.provider_type", "a.local_endpoint", "a.status").
		Joins("JOIN agents a ON a.id = sm.member_id").
		Where("sm.session_id = ? AND sm.member_id = ? AND sm.member_type = 2", sid, agentID).
		Take(&agentRow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errLocalInferenceAgentNotInSession
		}
		return err
	}
	if agentRow.ProviderType != model.AgentProviderLocal ||
		strings.TrimSpace(agentRow.LocalEndpoint) == "" ||
		agentRow.Status != model.AgentStatusActive {
		return errLocalInferenceAgentUnavailable
	}

	var triggerCount int64
	if err := store.DB.WithContext(ctx).
		Model(&model.Message{}).
		Where(
			"msg_id = ? AND session_id = ? AND sender_id = ? AND sender_type = ? AND is_deleted = false",
			triggerMsgID,
			sid,
			requesterID,
			1,
		).
		Count(&triggerCount).Error; err != nil {
		return err
	}
	if triggerCount == 0 {
		return errLocalInferenceTriggerInvalid
	}

	return nil
}
