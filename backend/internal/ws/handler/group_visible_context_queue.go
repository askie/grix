package handler

import (
	"context"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type visibleGroupBufferMember struct {
	MemberID   int64 `gorm:"column:member_id"`
	MemberType int16 `gorm:"column:member_type"`
}

func BufferVisibleGroupMessage(
	ctx context.Context,
	sessionID string,
	senderID int64,
	senderType int16,
	msgID int64,
	visibleTo []int64,
) {
	if ctx == nil {
		ctx = context.Background()
	}
	if sessionID == "" || msgID <= 0 {
		return
	}

	var members []visibleGroupBufferMember
	memberQuery := store.DB.Model(&model.SessionMember{}).
		Select("member_id, member_type").
		Where("session_id = ? AND member_type IN ?", sessionID, []int16{1, 2})
	if len(visibleTo) > 0 {
		// When visible_to is set, only buffer for specified members plus the sender.
		visibleSet := make(map[int64]struct{}, len(visibleTo))
		for _, id := range visibleTo {
			visibleSet[id] = struct{}{}
		}
		visibleWithSender := make([]int64, 0, len(visibleTo)+1)
		visibleWithSender = append(visibleWithSender, visibleTo...)
		visibleWithSender = append(visibleWithSender, senderID)
		memberQuery = memberQuery.Where("member_id IN ?", visibleWithSender)
	}
	if err := memberQuery.Find(&members).Error; err != nil {
		logger.L.Warnf(
			"load visible group buffer members failed session=%s msg_id=%d: %v",
			sessionID,
			msgID,
			err,
		)
		return
	}

	for _, member := range members {
		if member.MemberID <= 0 {
			continue
		}
		if senderType == 2 && member.MemberType == 2 && member.MemberID == senderID {
			continue
		}
		if err := agentreceive.BufferVisibleMessage(
			ctx,
			sessionID,
			member.MemberType,
			member.MemberID,
			msgID,
		); err != nil {
			logger.L.Warnf(
				"buffer visible group message failed session=%s msg_id=%d member_type=%d member_id=%d: %v",
				sessionID,
				msgID,
				member.MemberType,
				member.MemberID,
				err,
			)
		}
	}
}

func resolveBufferedGroupDispatchContextMessages(
	ctx context.Context,
	sessionType int16,
	sessionID string,
	bufferMemberType int16,
	bufferMemberID int64,
	viewerUserID int64,
	backlogCount int,
	fallback []protocol.ContextMessagePayload,
) []protocol.ContextMessagePayload {
	if sessionType != 2 || sessionID == "" || bufferMemberID <= 0 {
		return fallback
	}
	if ctx == nil {
		ctx = context.Background()
	}

	contextMessages, err := agentreceive.LoadBufferedVisibleContextMessages(
		ctx,
		sessionID,
		bufferMemberType,
		bufferMemberID,
		viewerUserID,
		backlogCount,
	)
	if err != nil {
		logger.L.Warnf(
			"load buffered group dispatch context failed session=%s member_type=%d member_id=%d viewer=%d: %v",
			sessionID,
			bufferMemberType,
			bufferMemberID,
			viewerUserID,
			err,
		)
		return fallback
	}
	if len(contextMessages) == 0 {
		return fallback
	}
	return contextMessages
}

func buildDispatchContextMessages(
	ctx context.Context,
	sessionType int16,
	sessionID string,
	bufferMemberType int16,
	bufferMemberID int64,
	viewerUserID int64,
	backlogCount int,
	quotedMessageID int64,
	agentReceiveMode int16,
	fallback []protocol.ContextMessagePayload,
) []protocol.ContextMessagePayload {
	var contextMessages []protocol.ContextMessagePayload
	if agentReceiveMode != agentreceive.ModeMentionOnly {
		contextMessages = resolveBufferedGroupDispatchContextMessages(
			ctx,
			sessionType,
			sessionID,
			bufferMemberType,
			bufferMemberID,
			viewerUserID,
			backlogCount,
			fallback,
		)
	}
	// Prepend the quoted message for every session type (group and 1:1) so each
	// agent plugin can assemble the quoted original from the structured
	// context_messages it already receives. 1:1 carries no visible_to
	// restriction, so the same resolution path is safe to reuse.
	return prependQuotedContextMessage(
		contextMessages,
		ResolveQuotedContextMessage(sessionID, quotedMessageID),
	)
}

func clearBufferedGroupDispatchContext(
	ctx context.Context,
	sessionType int16,
	sessionID string,
	memberType int16,
	memberID int64,
) {
	if sessionType != 2 || sessionID == "" || memberID <= 0 {
		return
	}
	if err := agentreceive.ClearBuffer(ctx, sessionID, memberType, memberID); err != nil {
		logger.L.Warnf(
			"clear buffered group dispatch context failed session=%s member_type=%d member_id=%d: %v",
			sessionID,
			memberType,
			memberID,
			err,
		)
	}
}
