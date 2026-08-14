package service

import (
	"context"
	"errors"
	"sort"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/inboxseq"
	"gorm.io/gorm"
)

func normalizePositiveUserIDs(userIDs []int64) []int64 {
	if len(userIDs) == 0 {
		return nil
	}

	normalized := make([]int64, 0, len(userIDs))
	seen := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		normalized = append(normalized, userID)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i] < normalized[j]
	})
	return normalized
}

func collectHumanInboxUserIDs(members []model.SessionMember) []int64 {
	if len(members) == 0 {
		return nil
	}

	userIDs := make([]int64, 0, len(members))
	for _, member := range members {
		if member.MemberType != 1 {
			continue
		}
		userIDs = append(userIDs, member.MemberID)
	}
	return normalizePositiveUserIDs(userIDs)
}

func buildMessageRevokeInboxRowsTx(
	ctx context.Context,
	tx *gorm.DB,
	members []model.SessionMember,
	sessionID string,
	msgID int64,
) ([]model.UserInbox, error) {
	if tx == nil {
		return nil, errors.New("inbox sequence create failed")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	userIDs := collectHumanInboxUserIDs(members)
	if len(userIDs) == 0 {
		return nil, nil
	}

	nextSeqByUser, err := inboxseq.AllocateNextBatchTx(ctx, tx, userIDs)
	if err != nil {
		return nil, err
	}

	rows := make([]model.UserInbox, 0, len(userIDs))
	for _, userID := range userIDs {
		rows = append(rows, model.UserInbox{
			UserID:    userID,
			InboxSeq:  nextSeqByUser[userID],
			MsgID:     msgID,
			SessionID: sessionID,
			EventKind: model.UserInboxEventKindRevoke,
		})
	}
	return rows, nil
}

func buildMessageEditInboxRowsTx(
	ctx context.Context,
	tx *gorm.DB,
	members []model.SessionMember,
	sessionID string,
	msgID int64,
) ([]model.UserInbox, error) {
	if tx == nil {
		return nil, errors.New("inbox sequence create failed")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	userIDs := collectHumanInboxUserIDs(members)
	if len(userIDs) == 0 {
		return nil, nil
	}

	nextSeqByUser, err := inboxseq.AllocateNextBatchTx(ctx, tx, userIDs)
	if err != nil {
		return nil, err
	}

	rows := make([]model.UserInbox, 0, len(userIDs))
	for _, userID := range userIDs {
		rows = append(rows, model.UserInbox{
			UserID:    userID,
			InboxSeq:  nextSeqByUser[userID],
			MsgID:     msgID,
			SessionID: sessionID,
			EventKind: model.UserInboxEventKindEdit,
		})
	}
	return rows, nil
}
