package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

const (
	friendEventMaxKeep = 500
)

func pushFriendEvent(userID int64, event map[string]interface{}) {
	if userID <= 0 {
		return
	}
	if store.RDB == nil {
		pushRealtimeEvent(userID, "friend_event", event)
		return
	}

	ctx := context.Background()
	seqKey := fmt.Sprintf("im:friend:event:seq:%d", userID)
	seq, err := store.RDB.Incr(ctx, seqKey).Result()
	if err != nil {
		logger.L.Warnf("friend event seq incr error user=%d err=%v", userID, err)
		pushRealtimeEvent(userID, "friend_event", event)
		return
	}

	envelope := make(map[string]interface{}, len(event)+2)
	for k, v := range event {
		envelope[k] = v
	}
	envelope["event_seq"] = fmt.Sprintf("%d", seq)
	envelope["event_ts"] = time.Now().UnixMilli()

	data, err := json.Marshal(envelope)
	if err != nil {
		logger.L.Warnf("marshal friend event error user=%d err=%v", userID, err)
		return
	}

	zsetKey := fmt.Sprintf("im:friend:event:z:%d", userID)
	if err := store.RDB.ZAdd(ctx, zsetKey, redis.Z{
		Score:  float64(seq),
		Member: string(data),
	}).Err(); err != nil {
		logger.L.Warnf("friend event zadd error user=%d err=%v", userID, err)
	}
	// Keep latest N events.
	_ = store.RDB.ZRemRangeByRank(ctx, zsetKey, 0, -friendEventMaxKeep-1).Err()

	pushRealtimeEvent(userID, "friend_event", envelope)
}
