package callsegment

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/nats-io/nats.go"
)

const (
	TranscriptSubject = "voicebridge.transcript.*"
	TranscriptQueue   = "callsegment.ws"

	// callerDebounce 是访客连续说话时的句子聚合窗口。
	// 豆包 ASR 已处理句内停顿，此处针对句间停顿：人类对话轮转的自然沉默阈值约 1.5s，
	// 超过此时长视为一段表达结束，将缓冲句子合并为一条消息写入 IM 并触发文字大脑。
	callerDebounce = 1500 * time.Millisecond
)

type transcriptEvent struct {
	CallID        string `json:"call_id"`
	SegmentSeq    int    `json:"segment_seq"`
	SpeakerRole   string `json:"speaker_role"`
	TranscriptRaw string `json:"transcript_raw"`
	Provider      string `json:"provider"`
	StartedAtMs   int64  `json:"started_at_ms"`
}

type TranscriptRoute struct {
	SessionID    string
	OwnerID      int64
	AgentID      int64 // 有权写入会话的 agent（权限用）；语音大脑场景为文字 agent
	VoiceAgentID int64 // 语音大脑专用：真实发声的语音 agent（ai_bot speaker 展示用）
	CallerID     int64
	CalleeID     int64
	DirectAICall bool // agent == callee: 用户调用自己的 agent
}

type CallRecordLookup func(ctx context.Context, callID int64) (route TranscriptRoute, err error)

// MemoryLookup resolves transcript route from in-memory active calls.
type MemoryLookup func(callID int64) (route TranscriptRoute, ok bool)

// DelegateTrigger 在访客(caller)转写写入后触发文字托管（接点A）。
// 由装配方注入 handler.TriggerDelegatesForMessage 的闭包，
// 使 callsegment 无需直接依赖 ws/handler（保持包隔离方向单一）。
// selfTrigger 为 true 时（直拨通话，说话人即托管 owner 本人），
// 触发侧应放行"跳过 sender 本人托管"的常规规则（架构文档 33）。
type DelegateTrigger func(ctx context.Context, sessionID string, senderID int64, triggerMsgID int64, content string, selfTrigger bool)

// callerBuf 缓存一通电话中访客连续说话的句子，等待 debounce 窗口结束后合并写入。
type callerBuf struct {
	route      TranscriptRoute
	speakerUID int64
	callIDInt  int64
	sentences  []string
	firstSeq   int
	startedAt  int64
	provider   string
	timer      *time.Timer
	version    int
}

// NATSConsumer subscribes to voicebridge.transcript.* and writes segments via Writer.
type NATSConsumer struct {
	nc       *nats.Conn
	writer   *Writer
	lookup   CallRecordLookup
	memLoc   MemoryLookup    // optional fallback for calls not in DB
	delegate DelegateTrigger // optional：caller 转写触发文字托管（接点A）
	subs     []*nats.Subscription

	mu         sync.Mutex
	callerBufs map[int64]*callerBuf // key: callID
}

func NewNATSConsumer(nc *nats.Conn, writer *Writer, lookup CallRecordLookup) *NATSConsumer {
	return &NATSConsumer{nc: nc, writer: writer, lookup: lookup, callerBufs: make(map[int64]*callerBuf)}
}

// SetMemoryLookup sets an optional in-memory lookup fallback.
func (c *NATSConsumer) SetMemoryLookup(fn MemoryLookup) {
	c.memLoc = fn
}

// SetDelegateTrigger 注入文字托管触发回调（接点A）。为 nil 时不触发。
func (c *NATSConsumer) SetDelegateTrigger(fn DelegateTrigger) {
	c.delegate = fn
}

func (c *NATSConsumer) Start() error {
	if c.nc == nil {
		logInfo("callsegment: NATS not configured, transcript consumer disabled")
		return nil
	}
	sub, err := c.nc.QueueSubscribe(TranscriptSubject, TranscriptQueue, c.handleMsg)
	if err != nil {
		return fmt.Errorf("callsegment subscribe: %w", err)
	}
	c.subs = append(c.subs, sub)
	logInfo("callsegment: NATS transcript consumer started")
	return nil
}

func (c *NATSConsumer) Stop() {
	for _, sub := range c.subs {
		_ = sub.Unsubscribe()
	}
	logInfo("callsegment: NATS transcript consumer stopped")
}

// FlushCall 立即 flush 指定通话缓冲的访客句子。通话结束时调用，避免最后一段话延迟写入。
func (c *NATSConsumer) FlushCall(callID int64) {
	c.mu.Lock()
	buf, ok := c.callerBufs[callID]
	if !ok {
		c.mu.Unlock()
		return
	}
	if buf.timer != nil {
		buf.timer.Stop()
	}
	delete(c.callerBufs, callID)
	c.mu.Unlock()
	c.flushCallerBuf(buf)
}

func (c *NATSConsumer) handleMsg(msg *nats.Msg) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var ev transcriptEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		logWarn("callsegment: invalid transcript payload: %v", err)
		return
	}

	callIDInt, err := strconv.ParseInt(ev.CallID, 10, 64)
	if err != nil {
		logWarn("callsegment: invalid call_id=%q: %v", ev.CallID, err)
		return
	}

	route, err := c.lookup(ctx, callIDInt)
	if err != nil || route.SessionID == "" || route.OwnerID <= 0 || route.AgentID <= 0 {
		// DB lookup failed, try in-memory fallback
		if c.memLoc != nil {
			if r, ok := c.memLoc(callIDInt); ok && r.SessionID != "" && r.OwnerID > 0 && r.AgentID > 0 {
				route = r
				err = nil
			}
		}
	}
	if err != nil {
		logWarn("callsegment: lookup session_id for call=%d failed: %v", callIDInt, err)
		return
	}
	if route.SessionID == "" || route.OwnerID <= 0 || route.AgentID <= 0 {
		logWarn("callsegment: invalid route for call=%d, dropping", callIDInt)
		return
	}
	speakerUserID := route.OwnerID
	switch ev.SpeakerRole {
	case "caller":
		if route.CallerID > 0 {
			speakerUserID = route.CallerID
		}
	case "callee":
		if route.CalleeID > 0 {
			speakerUserID = route.CalleeID
		}
	case "ai_bot":
		if route.VoiceAgentID > 0 {
			// 语音大脑：豆包只是传话筒，ai_bot 转写不写入 IM 会话（只留语音侧记录）。
			logInfo("callsegment: skip ai_bot transcript (voice_brain) call=%d seq=%d", callIDInt, ev.SegmentSeq)
			return
		}
		if route.AgentID > 0 {
			speakerUserID = route.AgentID
		}
	}

	// 访客(caller)说话：debounce 聚合多句为一段后再写入 IM 并触发文字大脑。
	// 红线：ai_bot / callee 转写不走 debounce，直接写入，且不触发 delegate（防死循环）。
	if ev.SpeakerRole == "caller" {
		c.appendCallerSentence(callIDInt, route, speakerUserID, ev)
		return
	}

	req := WriteReq{
		SessionID:     route.SessionID,
		OwnerID:       route.OwnerID,
		AgentID:       route.AgentID,
		DirectAICall:  route.DirectAICall,
		SpeakerUserID: speakerUserID,
		CallID:        ev.CallID,
		SegmentSeq:    ev.SegmentSeq,
		SpeakerRole:   ev.SpeakerRole,
		TranscriptRaw: ev.TranscriptRaw,
		Provider:      ev.Provider,
		StartedAtMs:   ev.StartedAtMs,
	}

	if _, err := c.writer.Write(ctx, req); err != nil {
		logWarn("callsegment: write failed call=%d seq=%d err=%v", callIDInt, ev.SegmentSeq, err)
	} else {
		logInfo("callsegment: written call=%d seq=%d role=%s", callIDInt, ev.SegmentSeq, ev.SpeakerRole)
	}
}

// appendCallerSentence 将访客句子追加到 debounce 缓冲，并重置计时器。
func (c *NATSConsumer) appendCallerSentence(callIDInt int64, route TranscriptRoute, speakerUID int64, ev transcriptEvent) {
	c.mu.Lock()
	if c.callerBufs == nil {
		c.callerBufs = make(map[int64]*callerBuf)
	}
	buf, exists := c.callerBufs[callIDInt]
	if !exists {
		buf = &callerBuf{
			route:      route,
			speakerUID: speakerUID,
			callIDInt:  callIDInt,
			firstSeq:   ev.SegmentSeq,
			startedAt:  ev.StartedAtMs,
			provider:   ev.Provider,
		}
		c.callerBufs[callIDInt] = buf
	}
	buf.sentences = append(buf.sentences, ev.TranscriptRaw)
	if buf.timer != nil {
		buf.timer.Stop()
	}
	buf.version++
	v := buf.version
	buf.timer = time.AfterFunc(callerDebounce, func() {
		c.mu.Lock()
		current, ok := c.callerBufs[callIDInt]
		if !ok || current.version != v {
			c.mu.Unlock()
			return
		}
		delete(c.callerBufs, callIDInt)
		c.mu.Unlock()
		c.flushCallerBuf(current)
	})
	c.mu.Unlock()
}

// flushCallerBuf 将缓冲句子合并写入 IM，并触发文字大脑（接点A）。
func (c *NATSConsumer) flushCallerBuf(buf *callerBuf) {
	if len(buf.sentences) == 0 {
		return
	}
	combined := strings.Join(buf.sentences, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := WriteReq{
		SessionID:     buf.route.SessionID,
		OwnerID:       buf.route.OwnerID,
		AgentID:       buf.route.AgentID,
		DirectAICall:  buf.route.DirectAICall,
		SpeakerUserID: buf.speakerUID,
		CallID:        strconv.FormatInt(buf.callIDInt, 10),
		SegmentSeq:    buf.firstSeq,
		SpeakerRole:   "caller",
		TranscriptRaw: combined,
		Provider:      buf.provider,
		StartedAtMs:   buf.startedAt,
	}
	msgID, err := c.writer.Write(ctx, req)
	if err != nil {
		logWarn("callsegment: write caller buffer call=%d seq=%d sentences=%d err=%v",
			buf.callIDInt, buf.firstSeq, len(buf.sentences), err)
		return
	}
	logInfo("callsegment: written caller buffer call=%d seq=%d sentences=%d len=%d",
		buf.callIDInt, buf.firstSeq, len(buf.sentences), len(combined))

	if c.delegate != nil && msgID > 0 {
		c.delegate(ctx, buf.route.SessionID, buf.speakerUID, msgID, combined, buf.route.DirectAICall)
	}
}

func DBLookup(s *store.CallRecordStore) CallRecordLookup {
	return func(ctx context.Context, callID int64) (TranscriptRoute, error) {
		rec, err := s.GetByID(ctx, callID)
		if err != nil {
			return TranscriptRoute{}, err
		}
		route := TranscriptRoute{
			SessionID: rec.SessionID,
			OwnerID:   rec.CalleeID,
			CallerID:  rec.CallerID,
			CalleeID:  rec.CalleeID,
		}
		if rec.DelegatedAgentID != nil {
			route.AgentID = *rec.DelegatedAgentID
		}
		// DirectAICall: agent == CalleeID → owner = CallerID
		if route.AgentID > 0 && route.AgentID == rec.CalleeID {
			route.OwnerID = rec.CallerID
			route.DirectAICall = true
		}
		// 语音大脑通话：text_agent_id 是真正负责会话消息的文字 agent（有会话成员权限）；
		// 将原始 voice_agent_id 保存到 VoiceAgentID 用于 ai_bot 转写的 speaker 展示，
		// 再覆盖 AgentID = text_agent_id 用于权限检查和 delegate 触发。
		if rec.TextAgentID != nil && *rec.TextAgentID > 0 {
			route.VoiceAgentID = route.AgentID // 保留原始语音 agent ID（豆包等）
			route.AgentID = *rec.TextAgentID
			route.OwnerID = rec.CallerID
			route.DirectAICall = true
		}
		return route, nil
	}
}

func logInfo(format string, args ...any) {
	if logger.L != nil {
		logger.L.Infof(format, args...)
	}
}

func logWarn(format string, args ...any) {
	if logger.L != nil {
		logger.L.Warnf(format, args...)
	}
}
