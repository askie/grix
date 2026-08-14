package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
)

const friendSyncLimit = 100

func HandleFriendSync(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.FriendSyncPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("friend_sync payload error: %v", err)
		return
	}
	if payload.LastEventSeq < 0 {
		payload.LastEventSeq = 0
	}

	resp := protocol.FriendSyncRespPayload{
		HasMore:     false,
		Events:      []json.RawMessage{},
		MaxEventSeq: payload.LastEventSeq,
	}
	if store.RDB == nil {
		conn.SendPayload(protocol.CmdFriendSyncResp, pkt.Seq, resp)
		return
	}

	ctx := context.Background()
	zsetKey := fmt.Sprintf("im:friend:event:z:%d", conn.GetUserID())
	min := fmt.Sprintf("(%d", payload.LastEventSeq)
	items, err := store.RDB.ZRangeByScore(ctx, zsetKey, &redis.ZRangeBy{
		Min:    min,
		Max:    "+inf",
		Offset: 0,
		Count:  int64(friendSyncLimit + 1),
	}).Result()
	if err != nil {
		logger.L.Warnf("friend_sync query error user=%d err=%v", conn.GetUserID(), err)
		conn.SendPayload(protocol.CmdFriendSyncResp, pkt.Seq, resp)
		return
	}

	resp.HasMore = len(items) > friendSyncLimit
	if resp.HasMore {
		items = items[:friendSyncLimit]
	}

	events := make([]json.RawMessage, 0, len(items))
	maxSeq := payload.LastEventSeq
	for _, item := range items {
		raw := json.RawMessage(item)
		events = append(events, raw)

		var meta struct {
			EventSeq interface{} `json:"event_seq"`
		}
		if err := json.Unmarshal([]byte(item), &meta); err != nil {
			continue
		}
		if seq, ok := toInt64(meta.EventSeq); ok && seq > maxSeq {
			maxSeq = seq
		}
	}

	resp.Events = events
	resp.MaxEventSeq = maxSeq
	conn.SendPayload(protocol.CmdFriendSyncResp, pkt.Seq, resp)
}

func toInt64(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), true
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}
