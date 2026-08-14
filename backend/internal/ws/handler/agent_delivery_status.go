package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
)

const (
	agentDeliveryStatusTTL     = 48 * time.Hour
	agentDeliveryStatusMaxKeep = 256
)

func agentDeliveryStatusHashKey(userID int64) string {
	return fmt.Sprintf("im:agent_delivery_status:%d", userID)
}

func agentDeliveryStatusOrderKey(userID int64) string {
	return fmt.Sprintf("im:agent_delivery_status_order:%d", userID)
}

func RecordAgentDeliveryStatus(ctx context.Context, payload protocol.AgentDeliveryStatusPayload) {
	if payload.OwnerID <= 0 || payload.TriggerMsgID <= 0 {
		return
	}
	if store.RDB == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if payload.UpdatedAt <= 0 {
		payload.UpdatedAt = time.Now().UnixMilli()
	}

	field := strconv.FormatInt(payload.TriggerMsgID, 10)
	data, err := json.Marshal(payload)
	if err != nil {
		logger.L.Warnf("record agent delivery status marshal error user=%d msg=%d: %v", payload.OwnerID, payload.TriggerMsgID, err)
		return
	}

	pipe := store.RDB.TxPipeline()
	pipe.HSet(ctx, agentDeliveryStatusHashKey(payload.OwnerID), field, data)
	pipe.ZAdd(ctx, agentDeliveryStatusOrderKey(payload.OwnerID), redis.Z{
		Score:  float64(payload.UpdatedAt),
		Member: field,
	})
	pipe.Expire(ctx, agentDeliveryStatusHashKey(payload.OwnerID), agentDeliveryStatusTTL)
	pipe.Expire(ctx, agentDeliveryStatusOrderKey(payload.OwnerID), agentDeliveryStatusTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		logger.L.Warnf("record agent delivery status redis error user=%d msg=%d: %v", payload.OwnerID, payload.TriggerMsgID, err)
		return
	}

	card, err := store.RDB.ZCard(ctx, agentDeliveryStatusOrderKey(payload.OwnerID)).Result()
	if err != nil || card <= agentDeliveryStatusMaxKeep {
		return
	}

	excess := card - agentDeliveryStatusMaxKeep
	staleFields, err := store.RDB.ZRange(ctx, agentDeliveryStatusOrderKey(payload.OwnerID), 0, excess-1).Result()
	if err != nil || len(staleFields) == 0 {
		return
	}

	trimPipe := store.RDB.TxPipeline()
	trimMembers := make([]interface{}, 0, len(staleFields))
	for _, stale := range staleFields {
		trimMembers = append(trimMembers, stale)
	}
	trimPipe.ZRem(ctx, agentDeliveryStatusOrderKey(payload.OwnerID), trimMembers...)
	trimPipe.HDel(ctx, agentDeliveryStatusHashKey(payload.OwnerID), staleFields...)
	trimPipe.Expire(ctx, agentDeliveryStatusHashKey(payload.OwnerID), agentDeliveryStatusTTL)
	trimPipe.Expire(ctx, agentDeliveryStatusOrderKey(payload.OwnerID), agentDeliveryStatusTTL)
	if _, err := trimPipe.Exec(ctx); err != nil {
		logger.L.Warnf("trim agent delivery status redis error user=%d: %v", payload.OwnerID, err)
	}
}

func LoadAgentDeliveryStatuses(ctx context.Context, userID int64, limit int) []protocol.AgentDeliveryStatusPayload {
	if userID <= 0 {
		return nil
	}
	if store.RDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = agentDeliveryStatusMaxKeep
	}

	orderKey := agentDeliveryStatusOrderKey(userID)
	hashKey := agentDeliveryStatusHashKey(userID)
	start := int64(0)
	end := int64(limit - 1)
	card, err := store.RDB.ZCard(ctx, orderKey).Result()
	if err == nil && card > int64(limit) {
		start = card - int64(limit)
		end = card - 1
	}
	fields, err := store.RDB.ZRange(ctx, orderKey, start, end).Result()
	if err != nil || len(fields) == 0 {
		return nil
	}

	values, err := store.RDB.HMGet(ctx, hashKey, fields...).Result()
	if err != nil {
		logger.L.Warnf("load agent delivery status redis error user=%d: %v", userID, err)
		return nil
	}

	statuses := make([]protocol.AgentDeliveryStatusPayload, 0, len(values))
	for _, raw := range values {
		text, ok := raw.(string)
		if !ok || text == "" {
			continue
		}
		var status protocol.AgentDeliveryStatusPayload
		if err := json.Unmarshal([]byte(text), &status); err != nil {
			logger.L.Warnf("load agent delivery status decode error user=%d: %v", userID, err)
			continue
		}
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].UpdatedAt != statuses[j].UpdatedAt {
			return statuses[i].UpdatedAt < statuses[j].UpdatedAt
		}
		return statuses[i].TriggerMsgID < statuses[j].TriggerMsgID
	})
	return statuses
}

func PushStoredAgentDeliveryStatuses(conn ConnInterface) {
	if conn == nil || conn.GetUserID() <= 0 {
		return
	}
	if store.RDB == nil {
		return
	}
	statuses := LoadAgentDeliveryStatuses(context.Background(), conn.GetUserID(), agentDeliveryStatusMaxKeep)
	if len(statuses) == 0 {
		return
	}
	conn.SendPayload(
		protocol.CmdAgentDeliveryStatusBatch,
		conn.NextSeq(),
		protocol.AgentDeliveryStatusBatchPayload{Items: statuses},
	)
}
