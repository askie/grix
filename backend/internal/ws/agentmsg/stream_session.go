package agentmsg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/chatmarkdown"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/askie/grix/backend/internal/ws/threadmeta"
	"gorm.io/datatypes"
)

// memberCache avoids repeated DB queries for session members during streaming.
// Each stream produces ~100+ chunks; caching members eliminates 100+ identical
// "SELECT * FROM session_members WHERE session_id=? AND member_type=1" queries.
var memberCache sync.Map // sessionID -> *memberCacheEntry

type memberCacheEntry struct {
	members  []model.SessionMember
	cachedAt time.Time
}

const memberCacheTTL = 30 * time.Second

func loadCachedMembers(sessionID string) []model.SessionMember {
	if v, ok := memberCache.Load(sessionID); ok {
		entry := v.(*memberCacheEntry)
		if time.Since(entry.cachedAt) < memberCacheTTL {
			return entry.members
		}
		memberCache.Delete(sessionID)
	}
	return nil
}

func storeCachedMembers(sessionID string, members []model.SessionMember) {
	memberCache.Store(sessionID, &memberCacheEntry{
		members:  members,
		cachedAt: time.Now(),
	})
}

const (
	DefaultBuilderTTL      = 1800 * time.Second
	sessionSummaryMaxRunes = 60
)

// 内置 AI 流式 chunk 合并开关（灰度/回滚用）。
// 将 StreamFlushInterval 置为 <=0 即退回"逐 chunk 广播"的旧行为。
var (
	StreamFlushInterval = 80 * time.Millisecond
	StreamFlushBytes    = 256
)

// ConfigureStreamCoalescing 在进程启动时按配置设置合并窗口。
// intervalMs<=0 表示关闭合并（退回逐 chunk 广播）；bytes<=0 时保留默认字节阈值。
func ConfigureStreamCoalescing(intervalMs, bytes int) {
	StreamFlushInterval = time.Duration(intervalMs) * time.Millisecond
	if bytes > 0 {
		StreamFlushBytes = bytes
	}
}

// StreamSessionConfig configures a new StreamSession.
type StreamSessionConfig struct {
	Ctx             context.Context
	SessionID       string
	ThreadID        string
	Identity        *SenderIdentity
	QuotedMessageID int64
	BuilderTTL      time.Duration
	ChunkSeq        int64   // starting chunk sequence (for ResumeStreamSession)
	VisibleTo       []int64 // restrict stream to these user IDs + sender
	IsThinking      bool    // 标记该流为思考过程流,随 stream_chunk 下发 is_thinking
}

// StreamSession manages the lifecycle of a single streaming message.
type StreamSession struct {
	ctx             context.Context
	sessionID       string
	threadID        string
	identity        *SenderIdentity
	quotedMessageID int64
	builderTTL      time.Duration
	visibleTo       []int64
	isThinking      bool

	msgID         int64
	builderKey    string
	chunkSeq      int64
	cachedMembers []model.SessionMember // cached human members for broadcast

	// chunk 合并缓冲（仅 AppendChunkBuffered 使用，面向内置 AI 顺序流式）。
	mu           sync.Mutex
	buf          strings.Builder
	lastFlush    time.Time
	streamedOnce bool
	luaSHA       string
}

// NewStreamSession allocates a msg_id, creates the DB placeholder, and initializes the Redis builder.
func NewStreamSession(cfg StreamSessionConfig) (*StreamSession, error) {
	if cfg.BuilderTTL <= 0 {
		cfg.BuilderTTL = DefaultBuilderTTL
	}
	if cfg.Ctx == nil {
		cfg.Ctx = context.Background()
	}
	if cfg.Identity == nil || cfg.SessionID == "" {
		return nil, fmt.Errorf("invalid stream session config")
	}
	cfg.ThreadID = strings.TrimSpace(cfg.ThreadID)
	if err := sessionguard.ValidateSpeakPermission(
		cfg.Ctx,
		nil,
		cfg.SessionID,
		cfg.Identity.SenderID,
		cfg.Identity.SenderType,
	); err != nil {
		return nil, err
	}

	msgID := snowflake.GenID()
	builderKey := fmt.Sprintf("ai:builder:%d", msgID)

	extraJSON := marshalExtraWithThread(cfg.Identity.ExtraFields, cfg.ThreadID)

	placeholderMsg := model.Message{
		MsgID:           msgID,
		SessionID:       cfg.SessionID,
		ThreadID:        cfg.ThreadID,
		SenderID:        cfg.Identity.SenderID,
		SenderType:      cfg.Identity.SenderType,
		MsgType:         4, // streaming
		Content:         "",
		QuotedMessageID: cfg.QuotedMessageID,
		Extra:           extraJSON,
	}
	if len(cfg.VisibleTo) > 0 {
		visibleToJSON, _ := json.Marshal(cfg.VisibleTo)
		placeholderMsg.VisibleTo = datatypes.JSON(visibleToJSON)
	}
	if err := store.DB.Create(&placeholderMsg).Error; err != nil {
		return nil, fmt.Errorf("create placeholder failed: %w", err)
	}

	if err := store.RDB.Set(cfg.Ctx, builderKey, "", cfg.BuilderTTL).Err(); err != nil {
		// Rollback placeholder message
		store.DB.Where("msg_id = ? AND session_id = ?", msgID, cfg.SessionID).Delete(&model.Message{})
		return nil, fmt.Errorf("init builder key failed: %w", err)
	}

	// Pre-cache human members for broadcast to avoid N+1 queries.
	var members []model.SessionMember
	members = loadCachedMembers(cfg.SessionID)
	if members == nil {
		store.DB.Where("session_id = ? AND member_type = 1", cfg.SessionID).Find(&members)
		storeCachedMembers(cfg.SessionID, members)
	}

	// Filter cached members by visibleTo when set.
	if len(cfg.VisibleTo) > 0 {
		allowed := make(map[int64]struct{}, len(cfg.VisibleTo)+1)
		allowed[cfg.Identity.SenderID] = struct{}{}
		for _, id := range cfg.VisibleTo {
			allowed[id] = struct{}{}
		}
		var filtered []model.SessionMember
		for _, m := range members {
			if _, ok := allowed[m.MemberID]; ok {
				filtered = append(filtered, m)
			}
		}
		members = filtered
	}

	return &StreamSession{
		ctx:             cfg.Ctx,
		sessionID:       cfg.SessionID,
		threadID:        cfg.ThreadID,
		identity:        cfg.Identity,
		quotedMessageID: cfg.QuotedMessageID,
		builderTTL:      cfg.BuilderTTL,
		visibleTo:       cfg.VisibleTo,
		isThinking:      cfg.IsThinking,
		msgID:           msgID,
		builderKey:      builderKey,
		cachedMembers:   members,
	}, nil
}

// ResumeStreamSession resumes an existing stream session from a known msgID.
// cfg.ChunkSeq can be set to restore the chunk counter for accurate sequencing.
func ResumeStreamSession(cfg StreamSessionConfig, msgID int64) *StreamSession {
	if cfg.BuilderTTL <= 0 {
		cfg.BuilderTTL = DefaultBuilderTTL
	}
	if cfg.Ctx == nil {
		cfg.Ctx = context.Background()
	}

	// Load and filter members by visibleTo (same logic as NewStreamSession)
	// so that subsequent broadcastChunk/Finish calls respect visibility.
	var members []model.SessionMember
	if store.DB != nil && cfg.SessionID != "" {
		members = loadCachedMembers(cfg.SessionID)
		if members == nil {
			store.DB.Where("session_id = ? AND member_type = 1", cfg.SessionID).Find(&members)
			storeCachedMembers(cfg.SessionID, members)
		}
		if len(cfg.VisibleTo) > 0 {
			allowed := make(map[int64]struct{}, len(cfg.VisibleTo)+1)
			if cfg.Identity != nil {
				allowed[cfg.Identity.SenderID] = struct{}{}
			}
			for _, id := range cfg.VisibleTo {
				allowed[id] = struct{}{}
			}
			var filtered []model.SessionMember
			for _, m := range members {
				if _, ok := allowed[m.MemberID]; ok {
					filtered = append(filtered, m)
				}
			}
			members = filtered
		}
	}

	return &StreamSession{
		ctx:             cfg.Ctx,
		sessionID:       cfg.SessionID,
		threadID:        strings.TrimSpace(cfg.ThreadID),
		identity:        cfg.Identity,
		quotedMessageID: cfg.QuotedMessageID,
		builderTTL:      cfg.BuilderTTL,
		visibleTo:       cfg.VisibleTo,
		isThinking:      cfg.IsThinking,
		msgID:           msgID,
		builderKey:      fmt.Sprintf("ai:builder:%d", msgID),
		chunkSeq:        cfg.ChunkSeq,
		cachedMembers:   members,
	}
}

// MsgID returns the allocated message ID.
func (ss *StreamSession) MsgID() int64 {
	return ss.msgID
}

// ChunkSeq returns the current chunk sequence number.
func (ss *StreamSession) ChunkSeq() int64 {
	return ss.chunkSeq
}

// QuotedMessageID returns the quoted message ID for this stream session.
func (ss *StreamSession) QuotedMessageID() int64 {
	return ss.quotedMessageID
}

// ThreadID returns the thread identifier for this stream session.
func (ss *StreamSession) ThreadID() string {
	return ss.threadID
}

// VisibleTo returns the visibility restriction for this stream session.
func (ss *StreamSession) VisibleTo() []int64 {
	return ss.visibleTo
}

// IsThinking reports whether this stream is a thinking-process stream.
func (ss *StreamSession) IsThinking() bool {
	return ss.isThinking
}

// AppendChunk appends delta content to the Redis builder and broadcasts a stream_chunk.
func (ss *StreamSession) AppendChunk(delta string) {
	if delta == "" {
		return
	}

	if err := store.RDB.Append(ss.ctx, ss.builderKey, delta).Err(); err != nil {
		logger.L.Warnf("stream_session append: redis append failed msg_id=%d key=%s err=%v", ss.msgID, ss.builderKey, err)
	}
	if err := store.RDB.Expire(ss.ctx, ss.builderKey, ss.builderTTL).Err(); err != nil {
		logger.L.Warnf("stream_session append: redis expire failed msg_id=%d key=%s err=%v", ss.msgID, ss.builderKey, err)
	}

	ss.chunkSeq++
	ss.broadcastChunk(delta)
}

// AppendChunkLua appends delta content using a Lua script for atomic append+expire.
// Falls back to plain Append if the script fails.
func (ss *StreamSession) AppendChunkLua(luaSHA, delta string) {
	if delta == "" {
		return
	}

	ttlSeconds := int(ss.builderTTL.Seconds())
	if luaSHA != "" {
		if err := store.RDB.EvalSha(
			ss.ctx,
			luaSHA,
			[]string{ss.builderKey},
			delta, ttlSeconds,
		).Err(); err == nil {
			ss.chunkSeq++
			ss.broadcastChunk(delta)
			return
		}
	}

	if err := store.RDB.Append(ss.ctx, ss.builderKey, delta).Err(); err != nil {
		logger.L.Warnf("stream_session append_lua fallback: redis append failed msg_id=%d key=%s err=%v", ss.msgID, ss.builderKey, err)
	}
	if err := store.RDB.Expire(ss.ctx, ss.builderKey, ss.builderTTL).Err(); err != nil {
		logger.L.Warnf("stream_session append_lua fallback: redis expire failed msg_id=%d key=%s err=%v", ss.msgID, ss.builderKey, err)
	}
	ss.chunkSeq++
	ss.broadcastChunk(delta)
}

// AppendChunkBuffered 立即把增量写入 builder（保证 builder 始终完整：stop/override 收尾、
// pull_sync 中途恢复都不会丢内容），但把真正的瓶颈——广播扇出（HGETALL 路由 + PUBLISH）——
// 按大小/时间窗口合并，消除"每个上游 chunk 扇出一次"的放大。
// 顺序由调用方单 goroutine 串行保证；finalize 前必须 FlushBuffer 以广播残余增量。
func (ss *StreamSession) AppendChunkBuffered(luaSHA, delta string) {
	if delta == "" {
		return
	}
	ss.luaSHA = luaSHA
	ss.appendToBuilder(delta) // 每 chunk 立即落 builder：单键写、无扇出，保证完整性
	ss.mu.Lock()
	ss.buf.WriteString(delta)
	due := StreamFlushInterval <= 0 || // 关闭合并：逐 chunk 广播
		!ss.streamedOnce || // 首个 chunk：立即广播，尽快清 composing
		ss.buf.Len() >= StreamFlushBytes ||
		time.Since(ss.lastFlush) >= StreamFlushInterval
	ss.mu.Unlock()
	if due {
		ss.FlushBuffer()
	}
}

// FlushBuffer 把缓冲中的增量合并成一次 stream_chunk 广播；空缓冲为 no-op。
// 内容已在 AppendChunkBuffered 时落 builder，这里只负责广播扇出。
func (ss *StreamSession) FlushBuffer() {
	ss.mu.Lock()
	if ss.buf.Len() == 0 {
		ss.mu.Unlock()
		return
	}
	delta := ss.buf.String()
	ss.buf.Reset()
	ss.lastFlush = time.Now()
	ss.streamedOnce = true
	ss.mu.Unlock()

	ss.chunkSeq++
	ss.broadcastChunk(delta)
}

// appendToBuilder 原子追加增量到 builder：有 luaSHA 时走 Lua(APPEND+EXPIRE)，否则降级为 Append+Expire。
func (ss *StreamSession) appendToBuilder(delta string) {
	if ss.luaSHA != "" {
		if err := store.RDB.EvalSha(ss.ctx, ss.luaSHA, []string{ss.builderKey}, delta, int(ss.builderTTL.Seconds())).Err(); err == nil {
			return
		}
	}
	if err := store.RDB.Append(ss.ctx, ss.builderKey, delta).Err(); err != nil {
		logger.L.Warnf("stream_session flush: redis append failed msg_id=%d key=%s err=%v", ss.msgID, ss.builderKey, err)
	}
	if err := store.RDB.Expire(ss.ctx, ss.builderKey, ss.builderTTL).Err(); err != nil {
		logger.L.Warnf("stream_session flush: redis expire failed msg_id=%d key=%s err=%v", ss.msgID, ss.builderKey, err)
	}
}

func (ss *StreamSession) broadcastChunk(delta string) {
	chunkPayload := protocol.StreamChunkPayload{
		MsgID:           ss.msgID,
		SessionID:       ss.sessionID,
		ThreadID:        ss.threadID,
		SenderID:        ss.identity.SenderID,
		SenderType:      ss.identity.SenderType,
		DeltaContent:    delta,
		ChunkSeq:        ss.chunkSeq,
		IsFinish:        false,
		CreatedAt:       time.Now().UnixMilli(),
		VisibleTo:       ss.visibleTo,
		IsThinking:      ss.isThinking,
		QuotedMessageID: ss.quotedMessageID,
	}
	BroadcastToSessionWithMembers(ss.ctx, ss.sessionID, protocol.CmdStreamChunk, chunkPayload, ss.cachedMembers)
}

// AppendChunkNoBC appends delta content to the Redis builder without broadcasting.
// Returns the updated chunk sequence number.
func (ss *StreamSession) AppendChunkNoBC(delta string) int64 {
	if delta == "" {
		return ss.chunkSeq
	}

	if err := store.RDB.Append(ss.ctx, ss.builderKey, delta).Err(); err != nil {
		logger.L.Warnf("stream_session append_no_bc: redis append failed msg_id=%d key=%s err=%v", ss.msgID, ss.builderKey, err)
	}
	if err := store.RDB.Expire(ss.ctx, ss.builderKey, ss.builderTTL).Err(); err != nil {
		logger.L.Warnf("stream_session append_no_bc: redis expire failed msg_id=%d key=%s err=%v", ss.msgID, ss.builderKey, err)
	}

	ss.chunkSeq++
	return ss.chunkSeq
}

// FinishNoBC reads the full content from the builder, updates DB, writes inbox, and cleans up Redis.
// It does NOT broadcast stream_finish — the caller is responsible for broadcasting.
func (ss *StreamSession) FinishNoBC() (string, error) {
	ss.FlushBuffer()
	fullContent, err := store.RDB.Get(ss.ctx, ss.builderKey).Result()
	if err != nil {
		logger.L.Warnf("stream_session finish_no_bc: redis get builder key failed msg_id=%d key=%s err=%v", ss.msgID, ss.builderKey, err)
		return "", fmt.Errorf("read builder content failed: %w", err)
	}
	fullContent = chatmarkdown.RepairFinal(fullContent).Output

	updates := map[string]any{
		"content":  fullContent,
		"msg_type": 1,
	}
	if ss.threadID != "" {
		updates["thread_id"] = ss.threadID
	}
	if len(ss.visibleTo) > 0 {
		visibleToJSON, _ := json.Marshal(ss.visibleTo)
		updates["visible_to"] = datatypes.JSON(visibleToJSON)
	}
	if err := store.DB.Model(&model.Message{}).
		Where("msg_id = ? AND session_id = ?", ss.msgID, ss.sessionID).
		Updates(updates).Error; err != nil {
		return fullContent, fmt.Errorf("update message failed: %w", err)
	}

	ss.updateSessionSummary(fullContent, "finish_no_bc")

	EnqueueStreamInbox(ss.ctx, ss.sessionID, ss.msgID, ss.identity.SenderID, ss.visibleTo)
	store.RDB.Del(ss.ctx, ss.builderKey)

	return fullContent, nil
}

// SetBuilderContent overwrites the Redis builder content (used to ensure final content consistency).
func (ss *StreamSession) SetBuilderContent(content string) error {
	return store.RDB.Set(ss.ctx, ss.builderKey, content, ss.builderTTL).Err()
}

// Finish reads the full content from the builder, updates DB, writes inbox, and broadcasts stream_finish.
// Returns the full content.
func (ss *StreamSession) Finish() (string, error) {
	return ss.ForceFinish(nil)
}

// ForceFinish is like Finish but allows passing additional DB column updates
// (e.g. {"extra": `{"is_overridden":true}`}). Used by handleStop/handleOverride
// so they go through the same lifecycle as a normal finish.
func (ss *StreamSession) ForceFinish(extraUpdates map[string]any) (string, error) {
	ss.FlushBuffer()
	fullContent, err := store.RDB.Get(ss.ctx, ss.builderKey).Result()
	if err != nil {
		logger.L.Warnf("stream_session finish: redis get builder key failed msg_id=%d key=%s err=%v", ss.msgID, ss.builderKey, err)
		return "", fmt.Errorf("read builder content failed: %w", err)
	}
	fullContent = chatmarkdown.RepairFinal(fullContent).Output

	updates := map[string]any{
		"content":  fullContent,
		"msg_type": 1,
	}
	if ss.threadID != "" {
		updates["thread_id"] = ss.threadID
	}
	if len(ss.visibleTo) > 0 {
		visibleToJSON, _ := json.Marshal(ss.visibleTo)
		updates["visible_to"] = datatypes.JSON(visibleToJSON)
	}
	for k, v := range extraUpdates {
		updates[k] = v
	}
	if err := store.DB.Model(&model.Message{}).
		Where("msg_id = ? AND session_id = ?", ss.msgID, ss.sessionID).
		Updates(updates).Error; err != nil {
		return fullContent, fmt.Errorf("update message failed: %w", err)
	}

	ss.updateSessionSummary(fullContent, "finish")

	EnqueueStreamInbox(ss.ctx, ss.sessionID, ss.msgID, ss.identity.SenderID, ss.visibleTo)

	finishPayload := protocol.StreamFinishPayload{
		MsgID:           ss.msgID,
		SessionID:       ss.sessionID,
		ThreadID:        ss.threadID,
		SenderID:        ss.identity.SenderID,
		SenderType:      ss.identity.SenderType,
		FinalContent:    fullContent,
		QuotedMessageID: ss.quotedMessageID,
		LastChunkSeq:    ss.chunkSeq,
		IsFinish:        true,
		CreatedAt:       time.Now().UnixMilli(),
		VisibleTo:       ss.visibleTo,
	}
	BroadcastToSessionWithMembers(ss.ctx, ss.sessionID, protocol.CmdStreamFinish, finishPayload, ss.cachedMembers)

	store.RDB.Del(ss.ctx, ss.builderKey)

	return fullContent, nil
}

// Abort finalizes a partially-streamed message: if there's content, finalize it; otherwise delete the placeholder.
func (ss *StreamSession) Abort() {
	ss.FlushBuffer()
	fullContent, err := store.RDB.Get(ss.ctx, ss.builderKey).Result()
	if err != nil {
		logger.L.Warnf("stream_session abort: redis get builder key failed msg_id=%d key=%s err=%v", ss.msgID, ss.builderKey, err)
		// Delete placeholder since we can't recover content
		store.DB.Where("msg_id = ? AND session_id = ?", ss.msgID, ss.sessionID).Delete(&model.Message{})
		store.RDB.Del(ss.ctx, ss.builderKey)
		return
	}

	if strings.TrimSpace(fullContent) == "" {
		store.DB.Where("msg_id = ? AND session_id = ?", ss.msgID, ss.sessionID).
			Delete(&model.Message{})
		store.RDB.Del(ss.ctx, ss.builderKey)
		return
	}

	updates := map[string]any{
		"content":  fullContent,
		"msg_type": 1,
	}
	if ss.threadID != "" {
		updates["thread_id"] = ss.threadID
	}
	if len(ss.visibleTo) > 0 {
		visibleToJSON, _ := json.Marshal(ss.visibleTo)
		updates["visible_to"] = datatypes.JSON(visibleToJSON)
	}
	if err := store.DB.Model(&model.Message{}).
		Where("msg_id = ? AND session_id = ?", ss.msgID, ss.sessionID).
		Updates(updates).Error; err != nil {
		store.DB.Where("msg_id = ? AND session_id = ?", ss.msgID, ss.sessionID).
			Delete(&model.Message{})
		store.RDB.Del(ss.ctx, ss.builderKey)
		return
	}

	ss.updateSessionSummary(fullContent, "abort")

	EnqueueStreamInbox(ss.ctx, ss.sessionID, ss.msgID, ss.identity.SenderID, ss.visibleTo)

	finishPayload := protocol.StreamFinishPayload{
		MsgID:           ss.msgID,
		SessionID:       ss.sessionID,
		ThreadID:        ss.threadID,
		SenderID:        ss.identity.SenderID,
		SenderType:      ss.identity.SenderType,
		FinalContent:    fullContent,
		QuotedMessageID: ss.quotedMessageID,
		LastChunkSeq:    ss.chunkSeq,
		IsFinish:        true,
		CreatedAt:       time.Now().UnixMilli(),
		VisibleTo:       ss.visibleTo,
	}
	BroadcastToSessionWithMembers(ss.ctx, ss.sessionID, protocol.CmdStreamFinish, finishPayload, ss.cachedMembers)

	store.RDB.Del(ss.ctx, ss.builderKey)
}

func (ss *StreamSession) updateSessionSummary(fullContent, caller string) {
	sessionUpdates := map[string]any{
		"last_msg_id": ss.msgID,
		"updated_at":  time.Now(),
	}
	if len(ss.visibleTo) == 0 && !textutil.IsStandaloneCardMessage(fullContent) {
		sessionUpdates["last_msg_summary"] = textutil.TruncateRunes(fullContent, sessionSummaryMaxRunes)
	}
	if err := store.DB.Model(&model.Session{}).
		Where("session_id = ?", ss.sessionID).
		Updates(sessionUpdates).Error; err != nil {
		logger.L.Warnf("stream_session %s: update session summary failed session=%s msg_id=%d err=%v", caller, ss.sessionID, ss.msgID, err)
	}
}

// DeletePlaceholder removes the placeholder message and cleans up Redis.
func (ss *StreamSession) DeletePlaceholder() {
	store.DB.Where("msg_id = ? AND session_id = ?", ss.msgID, ss.sessionID).
		Delete(&model.Message{})
	store.RDB.Del(ss.ctx, ss.builderKey)
}

// BuilderKey returns the Redis builder key for this stream session.
func (ss *StreamSession) BuilderKey() string {
	return ss.builderKey
}

func marshalExtra(fields map[string]any) datatypes.JSON {
	if len(fields) == 0 {
		return nil
	}
	data, err := json.Marshal(fields)
	if err != nil {
		logger.L.Warnf("marshalExtra: json.Marshal failed: %v", err)
		return nil
	}
	return datatypes.JSON(data)
}

func marshalExtraWithThread(fields map[string]any, threadID string) datatypes.JSON {
	merged := threadmeta.Merge(json.RawMessage(marshalExtra(fields)), threadID)
	if len(merged) == 0 {
		return nil
	}
	return datatypes.JSON(merged)
}

func init() {
	// Ensure logger is available (some callers initialize it late).
	_ = logger.L
}
