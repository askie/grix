package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
)

var ErrSessionGroupBanned = sessionguard.ErrSessionBanned

const SessionAccessRevokedReasonGroupBanned = "group_banned"

func ensureSessionAccessible(ctx context.Context, sessionID string) error {
	return translateSessionAccessError(sessionguard.ValidateSessionAvailable(ctx, nil, sessionID))
}

func translateSessionAccessError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrSessionNotFound
	}
	if errors.Is(err, sessionguard.ErrSessionBanned) {
		return ErrSessionGroupBanned
	}
	return err
}

func ensureLoadedSessionAccessible(session model.Session) error {
	if session.IsDeleted {
		return ErrSessionNotFound
	}
	if session.SessionType == model.SessionTypeGroup &&
		session.ModerationStatus == model.SessionModerationStatusBanned {
		return ErrSessionGroupBanned
	}
	return nil
}

func EnsureHumanSessionAccessible(ctx context.Context, userID int64, sessionID string) error {
	if userID <= 0 {
		return ErrSessionPermissionDenied
	}
	if err := ensureSessionAccessible(ctx, sessionID); err != nil {
		return err
	}
	if err := ensureHumanSessionMembership(userID, sessionID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionPermissionDenied
		}
		return err
	}
	return nil
}

func NotifySessionAccessRevoked(
	sessionID string,
	userIDs []int64,
	reason string,
	message string,
) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || len(userIDs) == 0 {
		return
	}

	payload := protocol.SessionAccessRevokedPayload{
		SessionID: sid,
		Reason:    strings.TrimSpace(reason),
		Message:   strings.TrimSpace(message),
		UpdatedAt: time.Now().UnixMilli(),
	}

	for _, userID := range uniqueInt64IDs(userIDs) {
		if userID <= 0 {
			continue
		}
		pushRealtimeEvent(userID, protocol.CmdSessionAccessRevoked, payload)
	}
}
