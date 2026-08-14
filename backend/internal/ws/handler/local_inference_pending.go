package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
)

func storePendingLocalInferenceHint(
	ctx context.Context,
	triggerMsgID int64,
	hint *protocol.LocalInferenceHint,
) error {
	if store.RDB == nil || triggerMsgID <= 0 || hint == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	data, err := json.Marshal(hint)
	if err != nil {
		return err
	}
	return store.RDB.Set(
		ctx,
		pendingLocalInferenceHintKey(triggerMsgID),
		data,
		localStreamTimeout,
	).Err()
}

func loadPendingLocalInferenceHint(
	ctx context.Context,
	triggerMsgID int64,
) (*protocol.LocalInferenceHint, error) {
	if store.RDB == nil || triggerMsgID <= 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := store.RDB.Get(ctx, pendingLocalInferenceHintKey(triggerMsgID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var hint protocol.LocalInferenceHint
	if err := json.Unmarshal(raw, &hint); err != nil {
		return nil, err
	}
	if hint.AgentID <= 0 || hint.SessionID == "" {
		return nil, nil
	}
	return &hint, nil
}

func clearPendingLocalInferenceHint(ctx context.Context, triggerMsgID int64) error {
	if store.RDB == nil || triggerMsgID <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return store.RDB.Del(ctx, pendingLocalInferenceHintKey(triggerMsgID)).Err()
}

func pendingLocalInferenceHintKey(triggerMsgID int64) string {
	return fmt.Sprintf("im:local_inference:pending:%d", triggerMsgID)
}
