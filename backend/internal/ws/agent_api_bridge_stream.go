package ws

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/chatmarkdown"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/agentstream"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
	redis "github.com/redis/go-redis/v9"
	"gorm.io/datatypes"
)

type bufferedAgentAPIChunk struct {
	ChunkSeq     int64  `json:"chunk_seq"`
	DeltaContent string `json:"delta_content,omitempty"`
	IsFinish     bool   `json:"is_finish"`
}

type agentAPIStreamDrainResult struct {
	NextExpectedSeq int64
	FirstVisible    bool
	FinishSeen      bool
}

const defaultAgentAPIStreamFinishGrace = 150 * time.Millisecond

// agentAPIFenceDropLogSample 是 fenced drop 日志的采样间隔：每条流打第 1 次与
// 每第 N 次丢弃一条日志，并附 dropped_total 累计数，兼顾可观测与不刷屏。
const agentAPIFenceDropLogSample = 100

// shouldLogAgentAPIFenceDrop 决定第 n 次 fenced drop 是否打日志：
// 每条流的第 1 次与每第 agentAPIFenceDropLogSample 次打，其余静默。
func shouldLogAgentAPIFenceDrop(n int64) bool {
	return n == 1 || n%agentAPIFenceDropLogSample == 0
}

// noteAgentAPIFenceDrop 记录一次 fenced drop 并返回该 (agent, client_msg_id) 的
// 累计丢弃次数。条目不在流结束时主动清除（fence 路径无法感知结束），超上限整体
// 重置——只影响采样相位，不影响正确性。
func (s *Server) noteAgentAPIFenceDrop(agentID int64, clientMsgID string) int64 {
	s.agentAPIStreamFenceDropMu.Lock()
	defer s.agentAPIStreamFenceDropMu.Unlock()
	if s.agentAPIStreamFenceDropCounts == nil {
		s.agentAPIStreamFenceDropCounts = make(map[string]int64)
	}
	if len(s.agentAPIStreamFenceDropCounts) >= 4096 {
		s.agentAPIStreamFenceDropCounts = make(map[string]int64)
	}
	key := strconv.FormatInt(agentID, 10) + ":" + strings.TrimSpace(clientMsgID)
	s.agentAPIStreamFenceDropCounts[key]++
	return s.agentAPIStreamFenceDropCounts[key]
}

// defaultAgentAPIStreamStallReanchor 是流卡顿重锚的默认阈值。正常乱序重排在毫秒级
// 内补齐头分片，远低于该值；只有真正的"头分片永不到达"卡顿才会触发重锚。
const defaultAgentAPIStreamStallReanchor = 8 * time.Second

func resolveAgentAPIStreamVisibleTo(
	sessionID string,
	quotedMessageID int64,
	fallback []int64,
) []int64 {
	if quotedSenderID, quotedVisibleTo := handler.ResolveQuotedMessageOwnerAndVisibility(
		sessionID,
		quotedMessageID,
	); quotedSenderID > 0 && len(quotedVisibleTo) > 0 {
		return []int64{quotedSenderID}
	}
	if len(fallback) == 0 {
		return nil
	}
	return append([]int64(nil), fallback...)
}

// lockAgentAPIStream serializes stream state mutations for a given
// agent+client_msg_id pair to avoid stale state overwrites under concurrency.
func (s *Server) lockAgentAPIStream(agentID int64, clientMsgID string) func() {
	if s == nil {
		return func() {}
	}
	normalizedClientMsgID := strings.TrimSpace(clientMsgID)
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(strconv.FormatInt(agentID, 10)))
	_, _ = hasher.Write([]byte{':'})
	_, _ = hasher.Write([]byte(normalizedClientMsgID))
	idx := int(hasher.Sum32() % uint32(len(s.agentAPIStreamShardLocks)))
	mu := &s.agentAPIStreamShardLocks[idx]
	mu.Lock()
	return mu.Unlock
}

func (s *Server) handleAgentAPIStreamChunk(
	_ context.Context,
	agentID, ownerID int64,
	chunk agentapi.AgentStreamChunkPayload,
) error {
	ctx := context.Background()
	unlockStream := s.lockAgentAPIStream(agentID, chunk.ClientMsgID)
	defer unlockStream()
	clientMsgID := strings.TrimSpace(chunk.ClientMsgID)
	eventID := strings.TrimSpace(chunk.EventID)
	threadID := strings.TrimSpace(chunk.ThreadID)
	streamState, hasStreamState := loadAgentAPIStreamState(ctx, agentID, clientMsgID)
	if eventID == "" {
		if mgr := agentapi.GetGlobal(); mgr != nil {
			if activeRun := mgr.LookupActiveRunBySessionOwner(ownerID, chunk.SessionID); activeRun != nil {
				eventID = strings.TrimSpace(activeRun.EventID)
				if threadID == "" {
					threadID = strings.TrimSpace(activeRun.ThreadID)
				}
				logger.L.Infof(
					"agent_api_stream resolved active run fallback agent=%d owner=%d session=%s thread_id=%s client_msg_id=%s event_id=%s trigger_msg_id=%d stream_msg_id=%d state=%s",
					agentID,
					ownerID,
					strings.TrimSpace(chunk.SessionID),
					threadID,
					clientMsgID,
					eventID,
					activeRun.TriggerMsgID,
					activeRun.StreamMsgID,
					strings.TrimSpace(activeRun.State),
				)
			}
		}
		if eventID == "" {
			logger.L.Warnf(
				"agent_api_stream missing event_id without active run fallback agent=%d owner=%d session=%s client_msg_id=%s is_finish=%t",
				agentID,
				ownerID,
				strings.TrimSpace(chunk.SessionID),
				clientMsgID,
				chunk.IsFinish,
			)
		}
	}
	if agentapi.ShouldSilentlyAckInboundOutput(chunk.DeltaContent, agentapi.IsNoReplyProtocolContext(eventID)) {
		s.discardAgentAPIStreamForNoReply(ctx, agentID, ownerID, chunk, streamState, hasStreamState)
		return nil
	}
	if threadID == "" && hasStreamState {
		threadID = strings.TrimSpace(streamState.ThreadID)
	}
	if chunk.QuotedMessageID <= 0 && hasStreamState {
		chunk.QuotedMessageID = streamState.QuotedMessageID
	}
	if threadID == "" && eventID != "" {
		if mgr := agentapi.GetGlobal(); mgr != nil {
			if activeRun := mgr.LookupActiveRun(eventID); activeRun != nil {
				threadID = strings.TrimSpace(activeRun.ThreadID)
			}
		}
	}
	chunk.ThreadID = threadID
	if agentstream.HasStoppedFence(ctx, agentID, clientMsgID) {
		if n := s.noteAgentAPIFenceDrop(agentID, clientMsgID); shouldLogAgentAPIFenceDrop(n) {
			logger.L.Infof(
				"agent_output_stop drop fenced client stream agent=%d owner=%d session=%s event_id=%s client_msg_id=%s is_finish=%t dropped_total=%d",
				agentID,
				ownerID,
				strings.TrimSpace(chunk.SessionID),
				eventID,
				clientMsgID,
				chunk.IsFinish,
				n,
			)
		}
		return nil
	}
	if mgr := agentapi.GetGlobal(); mgr != nil && eventID != "" && mgr.ShouldFenceEventReply(eventID) {
		if err := agentstream.FenceClientStream(ctx, agentID, clientMsgID, 0, agentstream.DefaultStoppedFenceTTL); err != nil {
			logger.L.Warnf("agent_api_stream fence stopped client stream failed agent=%d client_msg_id=%s err=%v", agentID, clientMsgID, err)
		}
		logger.L.Infof(
			"agent_output_stop drop chunk by event fence agent=%d owner=%d session=%s event_id=%s client_msg_id=%s is_finish=%t",
			agentID,
			ownerID,
			strings.TrimSpace(chunk.SessionID),
			eventID,
			clientMsgID,
			chunk.IsFinish,
		)
		return nil
	}
	identity, resolveErr := agentmsg.ResolveIdentity(ctx, agentmsg.IdentityParams{
		Mode:      agentmsg.ModeAgentAPI,
		SessionID: chunk.SessionID,
		OwnerID:   ownerID,
		AgentID:   agentID,
	})
	if resolveErr != nil {
		s.abortInactiveAgentStream(ctx, agentID, ownerID, chunk)
		return nil
	}

	// Resolve visible_to for the agent's reply: when the trigger message was a
	// hidden message (visible_to != nil), the reply should go back to the trigger
	// sender, not inherit the same audience list (which would make the reply
	// invisible to the original sender).
	var triggerVisibleTo []int64
	if eventID != "" {
		if mgr := agentapi.GetGlobal(); mgr != nil {
			if activeRun := mgr.LookupActiveRun(eventID); activeRun != nil && activeRun.TriggerMsgID > 0 {
				var triggerMsg model.Message
				if err := store.DB.Select("sender_id, visible_to").
					Where("msg_id = ? AND session_id = ?", activeRun.TriggerMsgID, chunk.SessionID).
					Find(&triggerMsg).Error; err == nil && triggerMsg.VisibleTo != nil {
					var rawIDs []int64
					if json.Unmarshal(triggerMsg.VisibleTo, &rawIDs) == nil && len(rawIDs) > 0 && triggerMsg.SenderID > 0 {
						triggerVisibleTo = []int64{triggerMsg.SenderID}
					}
				}
			}
		}
	}
	fallbackVisibleTo := triggerVisibleTo
	if len(fallbackVisibleTo) == 0 && hasStreamState && len(streamState.VisibleTo) > 0 {
		fallbackVisibleTo = append([]int64(nil), streamState.VisibleTo...)
	}
	streamVisibleTo := resolveAgentAPIStreamVisibleTo(
		chunk.SessionID,
		chunk.QuotedMessageID,
		fallbackVisibleTo,
	)
	streamKey, streamSeqKey := agentAPIStreamStateKeys(agentID, chunk.ClientMsgID)
	expectedChunkKey := agentstream.ExpectedChunkKey(agentID, chunk.ClientMsgID)
	pendingChunkKey := agentstream.PendingChunkKey(agentID, chunk.ClientMsgID)
	existing, err := store.RDB.Get(ctx, streamKey).Int64()
	currentChunkSeq := int64(0)
	if err == nil && existing > 0 {
		if seq, seqErr := store.RDB.Get(ctx, streamSeqKey).Int64(); seqErr == nil && seq > 0 {
			currentChunkSeq = seq
		}
	}
	expectedChunkSeq := loadAgentAPIExpectedChunkSeq(ctx, expectedChunkKey, currentChunkSeq)
	// 卡顿重锚：正常乱序重排会在毫秒级补齐缺失的头分片，因此 expected 不会长期停滞。
	// 但当 Agent 在长停顿/状态丢失后从缺失的头分片(seq 永不再到达)继续推流时，
	// 重排缓冲会把后续分片永久缓存，表现为空白消息气泡。这里仅当"卡在同一 expected
	// 处超过阈值"时，重锚到已缓存的最小待排 seq（放弃永不会到达的头分片），从而让内容
	// 落地而不是留空。阈值远大于正常乱序窗口，不影响合法的乱序重排。
	if chunk.ChunkSeq > expectedChunkSeq {
		if age := agentstream.MarkStallAge(ctx, agentID, chunk.ClientMsgID, expectedChunkSeq); age >= s.agentAPIStreamStallReanchorDuration() {
			reanchor := chunk.ChunkSeq
			if minPending, ok := minPendingChunkSeq(ctx, pendingChunkKey); ok && minPending > expectedChunkSeq && minPending < reanchor {
				reanchor = minPending
			}
			if reanchor > expectedChunkSeq {
				logger.L.Warnf(
					"agent_api_stream re-anchor stalled expected_seq agent=%d owner=%d session=%s event_id=%s client_msg_id=%s existing=%d expected_seq=%d->%d stalled=%s",
					agentID,
					ownerID,
					strings.TrimSpace(chunk.SessionID),
					eventID,
					clientMsgID,
					existing,
					expectedChunkSeq,
					reanchor,
					age,
				)
				agentstream.ClearStall(ctx, agentID, chunk.ClientMsgID, expectedChunkSeq)
				expectedChunkSeq = reanchor
			}
		}
	}
	if eventID != "" {
		logger.L.Debugf(
			"agent_api_stream chunk received agent=%d owner=%d session=%s event_id=%s client_msg_id=%s payload_seq=%d current_seq=%d expected_seq=%d delta_len=%d is_finish=%t",
			agentID,
			ownerID,
			strings.TrimSpace(chunk.SessionID),
			eventID,
			clientMsgID,
			chunk.ChunkSeq,
			currentChunkSeq,
			expectedChunkSeq,
			len(chunk.DeltaContent),
			chunk.IsFinish,
		)
	}
	if chunk.ChunkSeq < expectedChunkSeq {
		logger.L.Infof(
			"agent_api_stream drop stale chunk agent=%d owner=%d session=%s client_msg_id=%s payload_seq=%d expected_seq=%d",
			agentID,
			ownerID,
			strings.TrimSpace(chunk.SessionID),
			clientMsgID,
			chunk.ChunkSeq,
			expectedChunkSeq,
		)
		return nil
	}
	if chunk.ChunkSeq > expectedChunkSeq {
		logger.L.Infof(
			"agent_api_stream buffered future chunk agent=%d owner=%d session=%s client_msg_id=%s payload_seq=%d expected_seq=%d gap=%d",
			agentID,
			ownerID,
			strings.TrimSpace(chunk.SessionID),
			clientMsgID,
			chunk.ChunkSeq,
			expectedChunkSeq,
			chunk.ChunkSeq-expectedChunkSeq,
		)
	}

	// thinking 流标记:优先采用 connector 显式下发的 is_thinking;同时兼容旧约定——
	// client_msg_id 以 _thinking 结尾(与 finalize 判定一致)。据此随 stream_chunk 下发
	// is_thinking,供前端流式期即归类为思考气泡。
	isThinkingStream := chunk.IsThinking || strings.HasSuffix(clientMsgID, "_thinking")
	var ss *agentmsg.StreamSession
	createdNewSession := false
	if existing > 0 {
		ss = agentmsg.ResumeStreamSession(agentmsg.StreamSessionConfig{
			Ctx:             ctx,
			SessionID:       chunk.SessionID,
			ThreadID:        threadID,
			Identity:        identity,
			QuotedMessageID: chunk.QuotedMessageID,
			ChunkSeq:        currentChunkSeq,
			VisibleTo:       streamVisibleTo,
			IsThinking:      isThinkingStream,
		}, existing)
	} else {
		ss, err = agentmsg.NewStreamSession(agentmsg.StreamSessionConfig{
			Ctx:             ctx,
			SessionID:       chunk.SessionID,
			ThreadID:        threadID,
			Identity:        identity,
			QuotedMessageID: chunk.QuotedMessageID,
			VisibleTo:       streamVisibleTo,
			IsThinking:      isThinkingStream,
		})
		if err != nil {
			if errors.Is(err, sessionguard.ErrSpeakForbidden) ||
				errors.Is(err, sessionguard.ErrGroupAllMembersMuted) ||
				errors.Is(err, sessionguard.ErrMemberSpeakMuted) {
				return &agentapi.SendError{Code: 4003, Msg: sessionguard.ErrorMessage(err)}
			}
			return &agentapi.SendError{Code: 5001, Msg: "create placeholder failed"}
		}
		existing = ss.MsgID()
		createdNewSession = true
	}

	storeAgentAPIStreamState(ctx, agentID, chunk.ClientMsgID, agentstream.State{
		MsgID:           existing,
		SessionID:       chunk.SessionID,
		ThreadID:        threadID,
		SenderID:        identity.SenderID,
		SenderType:      identity.SenderType,
		QuotedMessageID: chunk.QuotedMessageID,
		VisibleTo:       streamVisibleTo,
		IsThinking:      isThinkingStream,
	})

	staged, stageErr := stageAgentAPIChunk(ctx, pendingChunkKey, chunk)
	if stageErr != nil {
		return &agentapi.SendError{Code: 5001, Msg: "stage stream chunk failed"}
	}
	if !staged {
		logger.L.Infof(
			"agent_api_stream ignore duplicate pending chunk agent=%d owner=%d session=%s client_msg_id=%s payload_seq=%d",
			agentID,
			ownerID,
			strings.TrimSpace(chunk.SessionID),
			clientMsgID,
			chunk.ChunkSeq,
		)
		return nil
	}

	drainResult, drainErr := s.drainAgentAPIStreamChunks(ctx, pendingChunkKey, ss, expectedChunkSeq)
	if drainErr != nil {
		return &agentapi.SendError{Code: 5001, Msg: "drain stream chunk failed"}
	}
	if drainResult.FirstVisible {
		if eventID != "" {
			pathLabel := "existing-path"
			if createdNewSession {
				pathLabel = "new-path"
			}
			logger.L.Infof(
				"agent_api_stream first_chunk broadcasted %s agent=%d owner=%d session=%s event_id=%s client_msg_id=%s msg_id=%d quoted_msg_id=%d payload_seq=%d",
				pathLabel,
				agentID,
				ownerID,
				chunk.SessionID,
				eventID,
				clientMsgID,
				ss.MsgID(),
				chunk.QuotedMessageID,
				chunk.ChunkSeq,
			)
		}
		if mgr := agentapi.GetGlobal(); mgr != nil && eventID != "" {
			mgr.MarkRunClientStreamStarted(eventID, ss.MsgID())
		}
	}

	if chunk.IsFinish || drainResult.FinishSeen {
		if err := s.scheduleAgentAPIStreamFinalize(chunk.SessionID, ownerID, agentID, clientMsgID, eventID); err != nil {
			return &agentapi.SendError{Code: 5001, Msg: "schedule stream finish failed"}
		}
	} else if !isThinkingStream && handler.HasActiveVoiceCallHint(ctx, chunk.SessionID) {
		// 边写边念：流式期间把已成型的完整句逐句注入语音侧（仅对有活跃语音通话的会话生效，
		// 廉价门控），让音频紧跟文字而非等整段写完。收尾整段由 finalize 统一补发(eot)。
		if raw, rerr := store.RDB.Get(ctx, ss.BuilderKey()).Result(); rerr == nil {
			handler.MaybeStreamVoiceSentence(ctx, chunk.SessionID, agentID, clientMsgID, raw, "", false)
		}
	}

	// Reset the result timeout timer on every successful chunk — the timeout
	// should be idle-based, not total-duration-based.
	if eventID != "" {
		if mgr := agentapi.GetGlobal(); mgr != nil {
			mgr.TouchPendingEventResult(eventID)
		}
	}
	pipe := store.RDB.TxPipeline()
	pipe.Set(ctx, streamKey, ss.MsgID(), agentmsg.DefaultBuilderTTL)
	pipe.Set(ctx, streamSeqKey, ss.ChunkSeq(), agentmsg.DefaultBuilderTTL)
	pipe.Set(ctx, expectedChunkKey, drainResult.NextExpectedSeq, agentmsg.DefaultBuilderTTL)
	pipe.Expire(ctx, pendingChunkKey, agentmsg.DefaultBuilderTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		logger.L.Warnf("agent_api_stream refresh state failed session=%s agent=%d msg_id=%d err=%v", chunk.SessionID, agentID, ss.MsgID(), err)
	}

	return nil
}

func (s *Server) discardAgentAPIStreamForNoReply(
	ctx context.Context,
	agentID, ownerID int64,
	chunk agentapi.AgentStreamChunkPayload,
	state agentstream.State,
	hasState bool,
) {
	clientMsgID := strings.TrimSpace(chunk.ClientMsgID)
	if clientMsgID == "" {
		return
	}
	if hasState && state.MsgID > 0 {
		sessionID := strings.TrimSpace(state.SessionID)
		if sessionID == "" {
			sessionID = strings.TrimSpace(chunk.SessionID)
		}
		if sessionID != "" {
			identity := agentAPIStreamStateIdentity(state)
			if identity == nil {
				identity = &agentmsg.SenderIdentity{SenderID: ownerID, SenderType: 1}
			}
			ss := agentmsg.ResumeStreamSession(agentmsg.StreamSessionConfig{
				Ctx:             ctx,
				SessionID:       sessionID,
				ThreadID:        firstNonEmptyString(chunk.ThreadID, state.ThreadID),
				Identity:        identity,
				QuotedMessageID: state.QuotedMessageID,
				VisibleTo:       state.VisibleTo,
				IsThinking:      state.IsThinking || strings.HasSuffix(clientMsgID, "_thinking"),
			}, state.MsgID)
			ss.DeletePlaceholder()
		}
	}
	s.cancelAgentAPIStreamFinalize(agentID, clientMsgID)
	cleanupAgentAPIStreamState(ctx, agentID, clientMsgID)
	if err := agentstream.FenceClientStream(ctx, agentID, clientMsgID, 0, agentstream.DefaultStoppedFenceTTL); err != nil {
		logger.L.Warnf("agent_api_stream no_reply fence failed agent=%d client_msg_id=%s err=%v", agentID, clientMsgID, err)
	}
}

func loadAgentAPIExpectedChunkSeq(ctx context.Context, expectedChunkKey string, currentChunkSeq int64) int64 {
	if store.RDB == nil || strings.TrimSpace(expectedChunkKey) == "" {
		if currentChunkSeq > 0 {
			return currentChunkSeq + 1
		}
		return 1
	}
	expectedChunkSeq, err := store.RDB.Get(ctx, expectedChunkKey).Int64()
	if err == nil && expectedChunkSeq > 0 {
		return expectedChunkSeq
	}
	if currentChunkSeq > 0 {
		return currentChunkSeq + 1
	}
	return 1
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stageAgentAPIChunk(ctx context.Context, pendingChunkKey string, chunk agentapi.AgentStreamChunkPayload) (bool, error) {
	if store.RDB == nil || strings.TrimSpace(pendingChunkKey) == "" {
		return false, errors.New("redis unavailable")
	}
	raw, err := json.Marshal(bufferedAgentAPIChunk{
		ChunkSeq:     chunk.ChunkSeq,
		DeltaContent: chunk.DeltaContent,
		IsFinish:     chunk.IsFinish,
	})
	if err != nil {
		return false, err
	}
	field := strconv.FormatInt(chunk.ChunkSeq, 10)
	existing, err := store.RDB.HGet(ctx, pendingChunkKey, field).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return false, err
	}

	serialized := string(raw)
	if err == nil && existing == serialized {
		return false, nil
	}

	if err := store.RDB.HSet(ctx, pendingChunkKey, field, serialized).Err(); err != nil {
		return false, err
	}
	if err := store.RDB.Expire(ctx, pendingChunkKey, agentmsg.DefaultBuilderTTL).Err(); err != nil {
		logger.L.Warnf("agent_api_stream pending chunk expire failed key=%s seq=%d err=%v", pendingChunkKey, chunk.ChunkSeq, err)
	}
	if err == nil && existing != "" && existing != serialized {
		logger.L.Warnf(
			"agent_api_stream overwrite conflicting pending chunk key=%s seq=%d old_len=%d new_len=%d",
			pendingChunkKey,
			chunk.ChunkSeq,
			len(existing),
			len(serialized),
		)
	}
	return true, nil
}

func (s *Server) drainAgentAPIStreamChunks(
	ctx context.Context,
	pendingChunkKey string,
	ss *agentmsg.StreamSession,
	expectedChunkSeq int64,
) (agentAPIStreamDrainResult, error) {
	result := agentAPIStreamDrainResult{
		NextExpectedSeq: expectedChunkSeq,
	}
	if store.RDB == nil || ss == nil || strings.TrimSpace(pendingChunkKey) == "" {
		return result, errors.New("invalid drain state")
	}
	if result.NextExpectedSeq <= 0 {
		result.NextExpectedSeq = 1
	}

	for {
		field := strconv.FormatInt(result.NextExpectedSeq, 10)
		raw, err := store.RDB.HGet(ctx, pendingChunkKey, field).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return result, nil
			}
			return result, err
		}

		var pending bufferedAgentAPIChunk
		if err := json.Unmarshal([]byte(raw), &pending); err != nil {
			return result, err
		}
		if err := store.RDB.HDel(ctx, pendingChunkKey, field).Err(); err != nil {
			return result, err
		}

		if pending.DeltaContent != "" {
			wasEmpty := ss.ChunkSeq() == 0
			ss.AppendChunk(pending.DeltaContent)
			if wasEmpty {
				result.FirstVisible = true
			}
		}

		result.NextExpectedSeq += 1
		if pending.IsFinish {
			result.FinishSeen = true
			return result, nil
		}
	}
}

func (s *Server) finalizeAgentAPIStream(
	ctx context.Context,
	sessionID string,
	ownerID int64,
	agentID int64,
	eventID string,
	ss *agentmsg.StreamSession,
	identity *agentmsg.SenderIdentity,
	clientMsgID string,
) {
	if ss == nil || identity == nil || sessionID == "" {
		return
	}

	isThinkingStream := ss.IsThinking() || strings.HasSuffix(clientMsgID, "_thinking")
	thinkingContent := ""
	if isThinkingStream {
		rawThinkingContent, err := store.RDB.Get(ctx, ss.BuilderKey()).Result()
		switch {
		case err == nil:
			thinkingContent = strings.TrimSpace(rawThinkingContent)
		case errors.Is(err, redis.Nil):
			thinkingContent = ""
		default:
			logger.L.Warnf(
				"agent_api_stream read thinking builder failed session=%s agent=%d msg_id=%d err=%v",
				sessionID,
				agentID,
				ss.MsgID(),
				err,
			)
		}
		if cardContent := buildThinkingCardMarkdown(thinkingContent); cardContent != "" {
			if err := ss.SetBuilderContent(cardContent); err != nil {
				logger.L.Warnf(
					"agent_api_stream set thinking card builder failed session=%s agent=%d msg_id=%d err=%v",
					sessionID,
					agentID,
					ss.MsgID(),
					err,
				)
			}
		}
	}
	rawFinalContent, builderErr := store.RDB.Get(ctx, ss.BuilderKey()).Result()
	if builderErr != nil {
		logger.L.Warnf("agent_api_stream read builder before finalize failed session=%s agent=%d msg_id=%d err=%v",
			sessionID, agentID, ss.MsgID(), builderErr)
		ss.DeletePlaceholder()
		return
	}
	normalizedFinalContent := chatmarkdown.RepairFinal(rawFinalContent).Output
	if agentapi.ShouldSilentlyAckInboundOutput(normalizedFinalContent, agentapi.IsNoReplyProtocolContext(eventID)) {
		ss.DeletePlaceholder()
		return
	}
	if strings.TrimSpace(normalizedFinalContent) == "" {
		logger.L.Warnf("agent_api_stream reject empty final content session=%s agent=%d msg_id=%d client_msg_id=%s",
			sessionID, agentID, ss.MsgID(), strings.TrimSpace(clientMsgID))
		ss.DeletePlaceholder()
		return
	}

	fullContent, err := ss.Finish()
	if err != nil {
		logger.L.Warnf("agent_api_stream finish error session=%s agent=%d msg_id=%d err=%v",
			sessionID, agentID, ss.MsgID(), err)
		return
	}
	if strings.TrimSpace(fullContent) == "" {
		ss.DeletePlaceholder()
		return
	}

	sessionType := resolveSessionType(sessionID)

	var extraRaw json.RawMessage
	if isThinkingStream {
		if thinkingContent == "" {
			thinkingContent = strings.TrimSpace(fullContent)
		}
		thinkingExtra, _ := json.Marshal(map[string]any{
			"channel_data": map[string]any{
				"grix": map[string]any{
					"thinking": map[string]any{
						"content": thinkingContent,
					},
				},
			},
		})
		extraRaw = thinkingExtra
	} else {
		extraRaw = normalizeAgentAPIStreamExtra(
			sessionID,
			ownerID,
			sessionType,
			fullContent,
			ss.QuotedMessageID(),
			ss.ThreadID(),
			agentID,
			identity,
		)
	}
	updates := map[string]any{
		"extra": datatypes.JSON(extraRaw),
	}
	if ss.ThreadID() != "" {
		updates["thread_id"] = ss.ThreadID()
	}
	if err := store.DB.Model(&model.Message{}).
		Where("msg_id = ? AND session_id = ?", ss.MsgID(), sessionID).
		Updates(updates).Error; err != nil {
		logger.L.Warnf("agent_api_stream update extra failed session=%s agent=%d msg_id=%d err=%v",
			sessionID, agentID, ss.MsgID(), err)
	}
	if s == nil || s.hub == nil {
		return
	}
	if sessionType == 2 {
		handler.BufferVisibleGroupMessage(
			ctx,
			sessionID,
			identity.SenderID,
			identity.SenderType,
			ss.MsgID(),
			ss.VisibleTo(),
		)
	}

	handler.TriggerDelegatesForMessage(
		s.hub, ctx, sessionID, identity.SenderID, identity.SenderType,
		ss.MsgID(), ss.QuotedMessageID(), 4, fullContent, json.RawMessage(extraRaw),
		ss.VisibleTo(),
	)
	// 接点B：若该会话正在进行 AI 语音通话，把文字大脑的回复注入语音侧（豆包 502）。
	// 无活跃语音通话时静默跳过，不影响纯文字会话。
	// 只注入给人看的最终文字：思考流(isThinkingStream)与工具卡片/思考标记(extraRaw)一律不注入，
	// 否则会被豆包当文字念出来污染通话（详见 handler.ShouldInjectVoiceMessage）。
	if !isThinkingStream && handler.ShouldInjectVoiceMessage(model.MsgTypeText, extraRaw) {
		// 边写边念收尾：补发整段(eot)——端到端通话整体念 fullContent(口径同旧逻辑)，
		// 语音大脑管线只补未念尾段；护栏(超时/每轮一次)在 MaybeStreamVoiceSentence 内统一施加。
		handler.MaybeStreamVoiceSentence(ctx, sessionID, agentID, clientMsgID, rawFinalContent, fullContent, true)
	}
	handler.TriggerDirectRouteForMessage(
		s.hub, ctx, sessionID, identity.SenderID, identity.SenderType,
		ss.MsgID(), ss.QuotedMessageID(), 4, fullContent, json.RawMessage(extraRaw),
		ss.VisibleTo(), nil,
	)
	service.ScheduleContentModeration(service.ContentModerationTask{
		SessionID: sessionID,
		MsgID:     ss.MsgID(),
	})
}

func buildThinkingCardMarkdown(content string) string {
	normalized := strings.TrimSpace(content)
	if normalized == "" {
		return ""
	}
	cardURL := url.URL{
		Scheme: "grix",
		Host:   "card",
		Path:   "thinking",
	}
	query := cardURL.Query()
	query.Set("content", normalized)
	cardURL.RawQuery = query.Encode()
	return "[Thinking](" + cardURL.String() + ")"
}

func (s *Server) agentAPIStreamFinishGraceDuration() time.Duration {
	if s == nil || s.agentAPIStreamFinishGrace <= 0 {
		return defaultAgentAPIStreamFinishGrace
	}
	return s.agentAPIStreamFinishGrace
}

func (s *Server) agentAPIStreamStallReanchorDuration() time.Duration {
	if s == nil || s.agentAPIStreamStallReanchor <= 0 {
		return defaultAgentAPIStreamStallReanchor
	}
	return s.agentAPIStreamStallReanchor
}

// minPendingChunkSeq 返回 pending 缓冲中最小的待排 chunk_seq, 用于卡顿重锚时
// 从已缓存的最早分片开始恢复(放弃永不会到达的更早头分片)。
func minPendingChunkSeq(ctx context.Context, pendingChunkKey string) (int64, bool) {
	if store.RDB == nil || strings.TrimSpace(pendingChunkKey) == "" {
		return 0, false
	}
	fields, err := store.RDB.HKeys(ctx, pendingChunkKey).Result()
	if err != nil || len(fields) == 0 {
		return 0, false
	}
	minSeq := int64(0)
	found := false
	for _, field := range fields {
		seq, parseErr := strconv.ParseInt(field, 10, 64)
		if parseErr != nil {
			continue
		}
		if !found || seq < minSeq {
			minSeq = seq
			found = true
		}
	}
	return minSeq, found
}

func agentAPIStreamFinishHoldKey(agentID int64, clientMsgID string) string {
	return strconv.FormatInt(agentID, 10) + ":" + strings.TrimSpace(clientMsgID)
}

// scheduleAgentAPIStreamFinalize 排一次流式收尾（宽限期到点后落库、写 stopped fence）。
//
// ⛔ 计数必须在【排定/决定要做这份工作】的这一刻加、在【工作真正结束】的那一刻减，
// 两头都在同一把锁下面做，不能有缝：
//   - 早前的版本把计数放在 time.AfterFunc 的回调里，而 closing 时的就地收尾走的是
//     另一条路径，压根不经过那段计数代码——waitAgentAPIStreamFinalizers 看到
//     active==0 就直接放行，关停可能在收尾真正落库之前就把库关了。
//   - 现在改成「进这个函数、确定要做这份工作的瞬间」就 +1，无论是立即执行（grace<=0
//     或 closing）还是排一个定时器；只有 cancelAgentAPIStreamFinalize 成功用
//     Stop() 拦下了「还没触发」的定时器（意味着回调保证不会再跑）时才代它 -1，
//     否则一律由收尾工作自己跑完后 -1。这样任何一条路径都逃不出计数。
func (s *Server) scheduleAgentAPIStreamFinalize(sessionID string, ownerID, agentID int64, clientMsgID, eventID string) error {
	if s == nil || agentID <= 0 || strings.TrimSpace(clientMsgID) == "" {
		return nil
	}
	grace := s.agentAPIStreamFinishGraceDuration()
	key := agentAPIStreamFinishHoldKey(agentID, clientMsgID)
	s.agentAPIStreamFinishMu.Lock()

	if s.agentAPIStreamFinishTimers != nil {
		if _, exists := s.agentAPIStreamFinishTimers[key]; exists {
			s.agentAPIStreamFinishMu.Unlock()
			return nil
		}
	}

	if s.agentAPIStreamFinishClosing || grace <= 0 {
		// 正在关停，或宽限期为 0：不排定时器（排了也没人去停它），就地收尾。
		// 关停时宽限期本就是等后续分片，而关停后不会再有分片了。
		s.agentAPIStreamFinishActive++
		s.agentAPIStreamFinishMu.Unlock()
		defer func() {
			s.agentAPIStreamFinishMu.Lock()
			s.agentAPIStreamFinishActive--
			s.agentAPIStreamFinishMu.Unlock()
		}()
		s.finalizeAgentAPIStreamAfterGrace(sessionID, ownerID, agentID, clientMsgID, eventID)
		return nil
	}

	if s.agentAPIStreamFinishTimers == nil {
		s.agentAPIStreamFinishTimers = make(map[string]*time.Timer)
	}
	s.agentAPIStreamFinishActive++
	timer := time.AfterFunc(grace, func() {
		defer func() {
			s.agentAPIStreamFinishMu.Lock()
			s.agentAPIStreamFinishActive--
			s.agentAPIStreamFinishMu.Unlock()
		}()
		s.finalizeAgentAPIStreamAfterGrace(sessionID, ownerID, agentID, clientMsgID, eventID)
	})
	s.agentAPIStreamFinishTimers[key] = timer
	s.agentAPIStreamFinishMu.Unlock()
	return nil
}

func (s *Server) cancelAgentAPIStreamFinalize(agentID int64, clientMsgID string) {
	if s == nil || agentID <= 0 || strings.TrimSpace(clientMsgID) == "" {
		return
	}
	key := agentAPIStreamFinishHoldKey(agentID, clientMsgID)
	s.agentAPIStreamFinishMu.Lock()
	timer := s.agentAPIStreamFinishTimers[key]
	if s.agentAPIStreamFinishTimers != nil {
		delete(s.agentAPIStreamFinishTimers, key)
	}
	s.agentAPIStreamFinishMu.Unlock()
	if timer == nil {
		return
	}
	if timer.Stop() {
		// 成功拦下了还没触发的定时器：回调保证不会再跑了，计数要由这里代它减掉，
		// 否则 scheduleAgentAPIStreamFinalize 里加的那次 +1 永远等不到人来 -1。
		s.agentAPIStreamFinishMu.Lock()
		s.agentAPIStreamFinishActive--
		s.agentAPIStreamFinishMu.Unlock()
	}
	// Stop() 返回 false：定时器已经触发（回调正在跑或已跑完），计数由回调自己的
	// defer 负责，这里不用管。
}

func (s *Server) finalizeAgentAPIStreamAfterGrace(sessionID string, ownerID, agentID int64, clientMsgID, eventID string) {
	if s == nil || agentID <= 0 || strings.TrimSpace(clientMsgID) == "" {
		return
	}
	unlockStream := s.lockAgentAPIStream(agentID, clientMsgID)
	defer unlockStream()
	key := agentAPIStreamFinishHoldKey(agentID, clientMsgID)
	s.agentAPIStreamFinishMu.Lock()
	if s.agentAPIStreamFinishTimers != nil {
		delete(s.agentAPIStreamFinishTimers, key)
	}
	s.agentAPIStreamFinishMu.Unlock()

	ctx := context.Background()
	if agentstream.HasStoppedFence(ctx, agentID, clientMsgID) {
		return
	}
	if err := agentstream.SetStoppedFence(ctx, agentID, clientMsgID, agentstream.DefaultStoppedFenceTTL); err != nil {
		logger.L.Warnf("agent_api_stream set finish fence failed agent=%d client_msg_id=%s err=%v", agentID, clientMsgID, err)
	}

	state, ok := loadAgentAPIStreamState(ctx, agentID, clientMsgID)
	if !ok || state.MsgID <= 0 {
		return
	}

	resolvedSessionID := strings.TrimSpace(state.SessionID)
	if resolvedSessionID == "" {
		resolvedSessionID = strings.TrimSpace(sessionID)
	}
	identity := agentAPIStreamStateIdentity(state)
	if identity == nil {
		identity = &agentmsg.SenderIdentity{SenderID: ownerID, SenderType: 1}
	}

	_, streamSeqKey := agentAPIStreamStateKeys(agentID, clientMsgID)
	currentChunkSeq, _ := store.RDB.Get(ctx, streamSeqKey).Int64()
	expectedChunkKey := agentstream.ExpectedChunkKey(agentID, clientMsgID)
	expectedChunkSeq := loadAgentAPIExpectedChunkSeq(ctx, expectedChunkKey, currentChunkSeq)
	pendingChunkKey := agentstream.PendingChunkKey(agentID, clientMsgID)
	ss := agentmsg.ResumeStreamSession(agentmsg.StreamSessionConfig{
		Ctx:             ctx,
		SessionID:       resolvedSessionID,
		ThreadID:        state.ThreadID,
		Identity:        identity,
		QuotedMessageID: state.QuotedMessageID,
		ChunkSeq:        currentChunkSeq,
		VisibleTo:       state.VisibleTo,
		IsThinking:      state.IsThinking || strings.HasSuffix(strings.TrimSpace(clientMsgID), "_thinking"),
	}, state.MsgID)
	drainResult, err := s.drainAgentAPIStreamChunks(ctx, pendingChunkKey, ss, expectedChunkSeq)
	if err != nil {
		logger.L.Warnf("agent_api_stream grace drain failed session=%s agent=%d client_msg_id=%s err=%v", resolvedSessionID, agentID, clientMsgID, err)
	}
	if drainResult.FirstVisible {
		if mgr := agentapi.GetGlobal(); mgr != nil && eventID != "" {
			mgr.MarkRunClientStreamStarted(eventID, ss.MsgID())
		}
	}
	s.clearAgentAPIComposing(ctx, agentID, ownerID, resolvedSessionID)
	s.finalizeAgentAPIStream(ctx, resolvedSessionID, ownerID, agentID, eventID, ss, identity, clientMsgID)
	if err := agentstream.FenceClientStream(ctx, agentID, clientMsgID, ss.MsgID(), agentstream.DefaultStoppedFenceTTL); err != nil {
		logger.L.Warnf(
			"agent_api_stream cleanup after grace finish failed session=%s agent=%d client_msg_id=%s msg_id=%d err=%v",
			resolvedSessionID,
			agentID,
			clientMsgID,
			ss.MsgID(),
			err,
		)
	}
	if mgr := agentapi.GetGlobal(); mgr != nil && eventID != "" {
		mgr.MarkRunStreamDone(eventID)
	}
}

func (s *Server) clearAgentAPIComposing(ctx context.Context, agentID, ownerID int64, sessionID string) {
	if s == nil || s.hub == nil || sessionID == "" || agentID <= 0 {
		return
	}
	if err := handler.SetSessionActivityFromAgentAPI(ctx, s.hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    false,
	}); err != nil && !errors.Is(err, agentmsg.ErrPermissionDenied) {
		logger.L.Warnf("clear agent api composing failed session=%s agent=%d owner=%d: %v", sessionID, agentID, ownerID, err)
	}
}

func (s *Server) abortInactiveAgentStream(
	ctx context.Context,
	agentID, ownerID int64,
	chunk agentapi.AgentStreamChunkPayload,
) {
	state, ok := loadAgentAPIStreamState(ctx, agentID, chunk.ClientMsgID)
	if !ok {
		streamKey, _ := agentAPIStreamStateKeys(agentID, chunk.ClientMsgID)
		msgID, err := store.RDB.Get(ctx, streamKey).Int64()
		if err != nil || msgID <= 0 {
			return
		}
		state = agentstream.State{
			MsgID:           msgID,
			SessionID:       chunk.SessionID,
			ThreadID:        chunk.ThreadID,
			SenderID:        ownerID,
			SenderType:      1,
			QuotedMessageID: chunk.QuotedMessageID,
		}
	}
	if state.MsgID <= 0 {
		return
	}

	identity := agentAPIStreamStateIdentity(state)
	if identity == nil {
		identity = &agentmsg.SenderIdentity{SenderID: ownerID, SenderType: 1}
	}
	sessionID := strings.TrimSpace(chunk.SessionID)
	if sessionID == "" {
		sessionID = state.SessionID
	}
	ss := agentmsg.ResumeStreamSession(agentmsg.StreamSessionConfig{
		Ctx:             ctx,
		SessionID:       sessionID,
		ThreadID:        firstNonEmptyString(chunk.ThreadID, state.ThreadID),
		Identity:        identity,
		QuotedMessageID: state.QuotedMessageID,
		VisibleTo:       state.VisibleTo,
	}, state.MsgID)
	ss.Abort()
	s.cancelAgentAPIStreamFinalize(agentID, chunk.ClientMsgID)
	cleanupAgentAPIStreamState(ctx, agentID, chunk.ClientMsgID)
}

func (s *Server) handleAgentAPIStreamDisconnect(ctx context.Context, agentID, ownerID int64) {
	if ctx == nil {
		ctx = context.Background()
	}
	if agentID <= 0 {
		return
	}

	rawStates, err := store.RDB.HGetAll(ctx, agentAPIStreamRegistryKey(agentID)).Result()
	if err != nil || len(rawStates) == 0 {
		return
	}

	for clientMsgID, raw := range rawStates {
		func(clientMsgID, raw string) {
			unlockStream := s.lockAgentAPIStream(agentID, clientMsgID)
			defer unlockStream()

			var state agentstream.State
			if err := json.Unmarshal([]byte(raw), &state); err != nil {
				logger.L.Warnf("agent_api_stream disconnect state decode failed agent=%d client_msg_id=%s err=%v", agentID, clientMsgID, err)
				cleanupAgentAPIStreamState(ctx, agentID, clientMsgID)
				return
			}
			if state.MsgID <= 0 || strings.TrimSpace(state.SessionID) == "" {
				cleanupAgentAPIStreamState(ctx, agentID, clientMsgID)
				return
			}

			identity := agentAPIStreamStateIdentity(state)
			if identity == nil {
				identity = &agentmsg.SenderIdentity{SenderID: ownerID, SenderType: 1}
			}
			ss := agentmsg.ResumeStreamSession(agentmsg.StreamSessionConfig{
				Ctx:             ctx,
				SessionID:       state.SessionID,
				ThreadID:        state.ThreadID,
				Identity:        identity,
				QuotedMessageID: state.QuotedMessageID,
				VisibleTo:       state.VisibleTo,
			}, state.MsgID)
			ss.Abort()
			s.cancelAgentAPIStreamFinalize(agentID, clientMsgID)
			cleanupAgentAPIStreamState(ctx, agentID, clientMsgID)
		}(clientMsgID, raw)
	}
}

// handleForceFinalizeSessionStreams force-finalizes all active streaming
// sessions for a given agent+session. Used when an approval card is sent to
// break the streaming boundary so subsequent chunks start a new message.
func (s *Server) handleForceFinalizeSessionStreams(ctx context.Context, agentID, ownerID int64, sessionID string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.RDB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" {
		return
	}
	registryKey := agentAPIStreamRegistryKey(agentID)
	rawStates, err := store.RDB.HGetAll(ctx, registryKey).Result()
	if err != nil || len(rawStates) == 0 {
		return
	}
	for clientMsgID, raw := range rawStates {
		var state agentstream.State
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			continue
		}
		if strings.TrimSpace(state.SessionID) != strings.TrimSpace(sessionID) {
			continue
		}
		unlockStream := s.lockAgentAPIStream(agentID, clientMsgID)
		identity := agentAPIStreamStateIdentity(state)
		if identity == nil {
			identity = &agentmsg.SenderIdentity{SenderID: state.SenderID, SenderType: state.SenderType}
		}
		ss := agentmsg.ResumeStreamSession(agentmsg.StreamSessionConfig{
			Ctx:             ctx,
			SessionID:       state.SessionID,
			ThreadID:        state.ThreadID,
			Identity:        identity,
			QuotedMessageID: state.QuotedMessageID,
			VisibleTo:       state.VisibleTo,
		}, state.MsgID)
		ss.Abort()
		s.cancelAgentAPIStreamFinalize(agentID, clientMsgID)
		cleanupAgentAPIStreamState(ctx, agentID, clientMsgID)
		unlockStream()
	}
}
