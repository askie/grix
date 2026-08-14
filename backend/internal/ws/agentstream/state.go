package agentstream

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

const DefaultStoppedFenceTTL = 10 * time.Minute

type State struct {
	MsgID           int64   `json:"msg_id"`
	SessionID       string  `json:"session_id"`
	ThreadID        string  `json:"thread_id,omitempty"`
	SenderID        int64   `json:"sender_id"`
	SenderType      int16   `json:"sender_type"`
	QuotedMessageID int64   `json:"quoted_message_id,omitempty"`
	VisibleTo       []int64 `json:"visible_to,omitempty"`
	// IsThinking 持久化该流是否为思考过程流,使 grace/finalize 路径也能在
	// 无 _thinking 后缀(纯显式标记)时正确归类并包成思考卡片。
	IsThinking bool `json:"is_thinking,omitempty"`
}

func StateKeys(agentID int64, clientMsgID string) (string, string) {
	return fmt.Sprintf("im:agent_stream:%d:%s", agentID, clientMsgID),
		fmt.Sprintf("im:agent_stream_seq:%d:%s", agentID, clientMsgID)
}

func ExpectedChunkKey(agentID int64, clientMsgID string) string {
	return fmt.Sprintf("im:agent_stream_expected:%d:%s", agentID, clientMsgID)
}

func PendingChunkKey(agentID int64, clientMsgID string) string {
	return fmt.Sprintf("im:agent_stream_pending:%d:%s", agentID, clientMsgID)
}

func RegistryKey(agentID int64) string {
	return fmt.Sprintf("im:agent_stream_meta:%d", agentID)
}

func BuilderKey(msgID int64) string {
	return fmt.Sprintf("ai:builder:%d", msgID)
}

func StoppedFenceKey(agentID int64, clientMsgID string) string {
	return fmt.Sprintf("im:agent_stream_stop:%d:%s", agentID, clientMsgID)
}

// DefaultStreamStallTTL 是卡顿标记的存活时间，足够覆盖一轮长流。
const DefaultStreamStallTTL = 30 * time.Minute

// StallKey 记录某条流卡在 expectedSeq 处(等待缺失头分片)的起始时间。
// key 中带 expectedSeq：一旦 expected 推进, 旧 key 自然弃用并随 TTL 过期。
func StallKey(agentID int64, clientMsgID string, expectedSeq int64) string {
	return fmt.Sprintf("im:agent_stream_stall:%d:%s:%d", agentID, clientMsgID, expectedSeq)
}

// MarkStallAge 在 expectedSeq 处登记/读取卡顿起始时间, 返回已卡顿时长。
// 首次调用以当前时间登记并返回 ~0; 后续调用返回距首次登记的时长。
func MarkStallAge(ctx context.Context, agentID int64, clientMsgID string, expectedSeq int64) time.Duration {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(clientMsgID) == "" {
		return 0
	}
	key := StallKey(agentID, clientMsgID, expectedSeq)
	nowMs := time.Now().UnixMilli()
	if err := store.RDB.SetNX(ctx, key, nowMs, DefaultStreamStallTTL).Err(); err != nil {
		return 0
	}
	startMs, err := store.RDB.Get(ctx, key).Int64()
	if err != nil || startMs <= 0 {
		return 0
	}
	if nowMs <= startMs {
		return 0
	}
	return time.Duration(nowMs-startMs) * time.Millisecond
}

// ClearStall 清理 expectedSeq 处的卡顿标记。
func ClearStall(ctx context.Context, agentID int64, clientMsgID string, expectedSeq int64) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(clientMsgID) == "" {
		return
	}
	store.RDB.Del(ctx, StallKey(agentID, clientMsgID, expectedSeq))
}

func StoreState(ctx context.Context, agentID int64, clientMsgID string, state State) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(clientMsgID) == "" || state.MsgID <= 0 || state.SessionID == "" {
		return
	}
	raw, err := json.Marshal(state)
	if err != nil {
		logger.L.Warnf("agent_stream marshal state failed agent=%d client_msg_id=%s err=%v", agentID, clientMsgID, err)
		return
	}
	if err := store.RDB.HSet(ctx, RegistryKey(agentID), clientMsgID, raw).Err(); err != nil {
		logger.L.Warnf("agent_stream store state failed agent=%d client_msg_id=%s err=%v", agentID, clientMsgID, err)
	}
}

func LoadState(ctx context.Context, agentID int64, clientMsgID string) (State, bool) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(clientMsgID) == "" {
		return State{}, false
	}
	raw, err := store.RDB.HGet(ctx, RegistryKey(agentID), clientMsgID).Result()
	if err != nil || strings.TrimSpace(raw) == "" {
		return State{}, false
	}
	var state State
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		logger.L.Warnf("agent_stream load state failed agent=%d client_msg_id=%s err=%v", agentID, clientMsgID, err)
		return State{}, false
	}
	return state, state.MsgID > 0 && state.SessionID != ""
}

func CleanupState(ctx context.Context, agentID int64, clientMsgID string) {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(clientMsgID) == "" {
		return
	}
	streamKey, streamSeqKey := StateKeys(agentID, clientMsgID)
	store.RDB.Del(ctx, streamKey, streamSeqKey, ExpectedChunkKey(agentID, clientMsgID), PendingChunkKey(agentID, clientMsgID))
	store.RDB.HDel(ctx, RegistryKey(agentID), clientMsgID)
}

// RenewActiveStreams 续期指定 agent 所有活跃 stream 的状态 key TTL。
// 在 agent 做工具调用期间无 chunk 输出时，通过 composing 心跳触发，防止 key 过期导致 stream 状态丢失。
func RenewActiveStreams(ctx context.Context, agentID int64, ttl time.Duration) {
	if store.RDB == nil || agentID <= 0 || ttl <= 0 {
		return
	}
	rawStates, err := store.RDB.HGetAll(ctx, RegistryKey(agentID)).Result()
	if err != nil || len(rawStates) == 0 {
		return
	}
	pipe := store.RDB.Pipeline()
	for clientMsgID := range rawStates {
		normalizedID := strings.TrimSpace(clientMsgID)
		if normalizedID == "" {
			continue
		}
		streamKey, streamSeqKey := StateKeys(agentID, normalizedID)
		pipe.Expire(ctx, streamKey, ttl)
		pipe.Expire(ctx, streamSeqKey, ttl)
		pipe.Expire(ctx, ExpectedChunkKey(agentID, normalizedID), ttl)
		pipe.Expire(ctx, PendingChunkKey(agentID, normalizedID), ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		logger.L.Warnf("agent_stream renew active streams failed agent=%d err=%v", agentID, err)
	}
}

func HasStoppedFence(ctx context.Context, agentID int64, clientMsgID string) bool {
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(clientMsgID) == "" {
		return false
	}
	exists, err := store.RDB.Exists(ctx, StoppedFenceKey(agentID, clientMsgID)).Result()
	return err == nil && exists > 0
}

func SetStoppedFence(ctx context.Context, agentID int64, clientMsgID string, ttl time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(clientMsgID) == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = DefaultStoppedFenceTTL
	}
	return store.RDB.Set(ctx, StoppedFenceKey(agentID, clientMsgID), "1", ttl).Err()
}

func FenceClientStream(ctx context.Context, agentID int64, clientMsgID string, msgID int64, ttl time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.RDB == nil || agentID <= 0 {
		return nil
	}

	normalizedClientMsgID := strings.TrimSpace(clientMsgID)
	if ttl <= 0 {
		ttl = DefaultStoppedFenceTTL
	}

	resolvedMsgID := msgID
	if normalizedClientMsgID != "" && resolvedMsgID <= 0 {
		if state, ok := LoadState(ctx, agentID, normalizedClientMsgID); ok {
			resolvedMsgID = state.MsgID
		}
	}

	pipe := store.RDB.TxPipeline()
	if normalizedClientMsgID != "" {
		streamKey, streamSeqKey := StateKeys(agentID, normalizedClientMsgID)
		pipe.Del(
			ctx,
			streamKey,
			streamSeqKey,
			ExpectedChunkKey(agentID, normalizedClientMsgID),
			PendingChunkKey(agentID, normalizedClientMsgID),
		)
		pipe.HDel(ctx, RegistryKey(agentID), normalizedClientMsgID)
		pipe.Set(ctx, StoppedFenceKey(agentID, normalizedClientMsgID), "1", ttl)
	}
	if resolvedMsgID > 0 {
		pipe.Del(ctx, BuilderKey(resolvedMsgID))
	}
	_, err := pipe.Exec(ctx)
	return err
}

func FenceStreamsByMsgID(ctx context.Context, agentID, msgID int64, ttl time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.RDB == nil || agentID <= 0 || msgID <= 0 {
		return nil
	}
	if ttl <= 0 {
		ttl = DefaultStoppedFenceTTL
	}

	rawStates, err := store.RDB.HGetAll(ctx, RegistryKey(agentID)).Result()
	if err != nil {
		return err
	}

	matched := false
	for clientMsgID, raw := range rawStates {
		var state State
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			logger.L.Warnf("agent_stream decode state failed agent=%d client_msg_id=%s err=%v", agentID, clientMsgID, err)
			continue
		}
		if state.MsgID != msgID {
			continue
		}
		matched = true
		if err := FenceClientStream(ctx, agentID, clientMsgID, msgID, ttl); err != nil {
			return err
		}
	}
	if matched {
		return nil
	}
	return store.RDB.Del(ctx, BuilderKey(msgID)).Err()
}
