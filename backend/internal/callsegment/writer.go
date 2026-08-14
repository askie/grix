// Package callsegment 实现通话片段写入 IM 消息流（msg_type=6）。
package callsegment

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/voicerefiner"
	"github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
)

// WriteReq 是写入一条通话片段消息的请求。
type WriteReq struct {
	SessionID       string
	OwnerID         int64
	AgentID         int64
	DirectAICall    bool // agent == callee: 用户调用自己的 agent
	CallID          string
	SegmentSeq      int
	SpeakerRole     string // "caller" | "callee" | "ai_bot"
	SpeakerUserID   int64
	TranscriptRaw   string
	AudioURL        string
	AudioDurationMs int
	StartedAtMs     int64
	EndedAtMs       int64
	Provider        string // "openai_realtime"
}

// CountUpdater 在片段写入成功后递增 call_records.segment_count。
type CountUpdater func(ctx context.Context, callID string) error

// Writer 将通话片段写入 messages 表并推送给会话成员。
type Writer struct {
	refiner      voicerefiner.TranscriptRefiner
	sendFn       func(ctx context.Context, req agentapi.SendMessageReq) (*agentapi.SendMessageResult, error)
	countUpdater CountUpdater // 可为 nil
	mu           sync.Mutex
	recent       map[string]recentTranscript
}

type recentTranscript struct {
	msgID  int64
	seenAt time.Time
	done   chan struct{}
}

const (
	doubaoHumanTranscriptDedupWindow = 5 * time.Second
	doubaoHumanTranscriptDedupMaxAge = 30 * time.Second
)

// New 创建 Writer。sendFn 通常是 agentapi.SendMessage，countUpdater 可为 nil。
func New(refiner voicerefiner.TranscriptRefiner,
	sendFn func(ctx context.Context, req agentapi.SendMessageReq) (*agentapi.SendMessageResult, error),
	countUpdater CountUpdater) *Writer {
	return &Writer{refiner: refiner, sendFn: sendFn, countUpdater: countUpdater, recent: make(map[string]recentTranscript)}
}

// Write 改写转写文本后写入一条 msg_type=6 消息。
func (w *Writer) Write(ctx context.Context, req WriteReq) (msgID int64, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dedupKey := w.doubaoHumanTranscriptDedupKey(req)
	if dedupKey != "" {
		if msgID, dup, waitErr := w.beginRecentTranscript(ctx, dedupKey); dup || waitErr != nil {
			return msgID, waitErr
		}
	}

	// 1. Refine 转写文本
	refined, refineErr := w.refiner.Refine(ctx, req.TranscriptRaw)
	if refineErr != nil {
		// 降级：Refiner 失败用原始文本
		refined = req.TranscriptRaw
	}

	// 2. 构造 extra
	extra := model.CallSegmentExtra{
		Kind:               "call_segment",
		CallID:             req.CallID,
		SegmentSeq:         req.SegmentSeq,
		SpeakerRole:        req.SpeakerRole,
		SpeakerUserID:      fmt.Sprintf("%d", req.SpeakerUserID),
		AudioURL:           req.AudioURL,
		AudioDurationMs:    req.AudioDurationMs,
		Transcript:         refined,
		TranscriptRaw:      req.TranscriptRaw,
		TranscriptStatus:   "final",
		TranscriptProvider: req.Provider,
		TranscriptRefined:  refineErr == nil && refined != req.TranscriptRaw,
		StartedAtMs:        req.StartedAtMs,
		EndedAtMs:          req.EndedAtMs,
	}
	extraJSON, err := json.Marshal(extra)
	if err != nil {
		return 0, fmt.Errorf("marshal extra: %w", err)
	}

	// 3. 写入消息（复用 agentapi.SendMessage 管道）
	// 语音通话只有两侧：owner 侧和 caller(访客)侧。
	// AI 代接时与 owner 是一体的，用 delegate 模式（sender_id=OwnerID, sender_type=1）；
	// caller 说话且非 owner 本人时用 caller 模式（sender_id=SpeakerUserID, sender_type=1）；
	// 其余人类说话时用 delegate 模式。
	identityMode := agentmsg.ModeDelegate
	if req.DirectAICall {
		// DirectAICall: user calls own agent — caller uses ModeCaller, ai_bot uses ModeAIDirect.
		// Avoids both sides mapping sender_id to OwnerID via ModeDelegate.
		switch req.SpeakerRole {
		case "caller":
			identityMode = agentmsg.ModeCaller
		case "ai_bot":
			identityMode = agentmsg.ModeAIDirect
		}
	} else if req.SpeakerRole == "caller" && req.SpeakerUserID > 0 && req.SpeakerUserID != req.OwnerID {
		identityMode = agentmsg.ModeCaller
	}

	result, err := w.sendFn(ctx, agentapi.SendMessageReq{
		AgentID:      req.AgentID,
		OwnerID:      req.OwnerID,
		CallerID:     req.SpeakerUserID,
		IdentityMode: identityMode,
		SessionID:    req.SessionID,
		ClientMsgID:  fmt.Sprintf("voice-call-segment:%s:%d", req.CallID, req.SegmentSeq),
		MsgType:      model.MsgTypeCallSegment,
		Content:      refined,
		Extra:        extraJSON,
	})
	if err != nil {
		if dedupKey != "" {
			w.finishRecentTranscript(dedupKey, 0, false)
		}
		return 0, fmt.Errorf("send call segment: %w", err)
	}
	if dedupKey != "" {
		w.finishRecentTranscript(dedupKey, result.MsgID, true)
	}

	// 异步递增 segment_count，不阻塞主流程
	if w.countUpdater != nil {
		go func() {
			if err := w.countUpdater(context.Background(), req.CallID); err != nil {
				// 非关键路径，只记录日志
				_ = err
			}
		}()
	}

	return result.MsgID, nil
}

func (w *Writer) doubaoHumanTranscriptDedupKey(req WriteReq) string {
	if req.Provider != "doubao_realtime" || req.SpeakerRole == "ai_bot" {
		return ""
	}
	text := strings.Join(strings.Fields(req.TranscriptRaw), " ")
	if text == "" {
		return ""
	}
	return fmt.Sprintf("%s\x00%s\x00%s", req.CallID, req.SpeakerRole, text)
}

func (w *Writer) beginRecentTranscript(ctx context.Context, key string) (int64, bool, error) {
	for {
		now := time.Now()
		w.mu.Lock()
		w.cleanupRecentTranscriptsLocked(now)
		if w.recent == nil {
			w.recent = make(map[string]recentTranscript)
		}
		if item, ok := w.recent[key]; ok && now.Sub(item.seenAt) <= doubaoHumanTranscriptDedupWindow {
			if item.msgID > 0 {
				w.mu.Unlock()
				return item.msgID, true, nil
			}
			if item.done != nil {
				done := item.done
				w.mu.Unlock()
				select {
				case <-done:
					continue
				case <-ctx.Done():
					return 0, false, ctx.Err()
				}
			}
		}
		w.recent[key] = recentTranscript{seenAt: now, done: make(chan struct{})}
		w.mu.Unlock()
		return 0, false, nil
	}
}

func (w *Writer) finishRecentTranscript(key string, msgID int64, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	item := w.recent[key]
	if ok {
		item.msgID = msgID
		item.seenAt = time.Now()
		w.recent[key] = item
	} else {
		delete(w.recent, key)
	}
	if item.done != nil {
		close(item.done)
	}
}

func (w *Writer) cleanupRecentTranscriptsLocked(now time.Time) {
	for k, item := range w.recent {
		if item.msgID > 0 && now.Sub(item.seenAt) > doubaoHumanTranscriptDedupMaxAge {
			delete(w.recent, k)
		}
	}
}

// MockSendFn 是测试用的 sendFn，返回自增 MsgID，并发安全。
// 返回 sendFn、calls 切片指针（仅在无并发时读取）和线程安全的 Len 函数。
func MockSendFn() (
	func(ctx context.Context, req agentapi.SendMessageReq) (*agentapi.SendMessageResult, error),
	*[]agentapi.SendMessageReq,
	func() int,
) {
	var calls []agentapi.SendMessageReq
	var mu sync.Mutex
	var seq int64
	fn := func(_ context.Context, req agentapi.SendMessageReq) (*agentapi.SendMessageResult, error) {
		mu.Lock()
		calls = append(calls, req)
		seq++
		id := seq
		mu.Unlock()
		return &agentapi.SendMessageResult{MsgID: snowflake.GenID(), InboxSeq: id}, nil
	}
	lenFn := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(calls)
	}
	return fn, &calls, lenFn
}
