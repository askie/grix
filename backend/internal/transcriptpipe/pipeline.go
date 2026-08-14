// Package transcriptpipe 订阅 NATS transcript 事件，经 Refiner 改写后写入 IM 消息流。
package transcriptpipe

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/callsegment"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/nats-io/nats.go"
)

// TranscriptEvent 是 Python voicebridge 发布的 transcript 事件结构。
type TranscriptEvent struct {
	CallID        string `json:"call_id"`
	SegmentSeq    int    `json:"segment_seq"`
	SpeakerRole   string `json:"speaker_role"`
	TranscriptRaw string `json:"transcript_raw"`
	Provider      string `json:"provider"`
	StartedAtMs   int64  `json:"started_at_ms"`
}

// SessionResolver 根据 callID 查询对应的 sessionID。
type SessionResolver func(callID string) (sessionID string, speakerUserID int64, ok bool)

// Pipeline 订阅 NATS transcript 事件并写入 IM 消息。
type Pipeline struct {
	nc       *nats.Conn
	writer   *callsegment.Writer
	resolver SessionResolver
	sub      *nats.Subscription
}

// New 创建 Pipeline。
func New(nc *nats.Conn, writer *callsegment.Writer, resolver SessionResolver) *Pipeline {
	return &Pipeline{nc: nc, writer: writer, resolver: resolver}
}

// Start 订阅 voicebridge.transcript.> 主题。
func (p *Pipeline) Start() error {
	if p.nc == nil {
		logger.L.Warn("transcriptpipe: NATS not configured, skipping")
		return nil
	}
	sub, err := p.nc.Subscribe("voicebridge.transcript.>", p.handle)
	if err != nil {
		return fmt.Errorf("subscribe transcript: %w", err)
	}
	p.sub = sub
	logger.L.Info("transcriptpipe: subscribed to voicebridge.transcript.>")
	return nil
}

// Stop 取消订阅。
func (p *Pipeline) Stop() {
	if p.sub != nil {
		_ = p.sub.Unsubscribe()
	}
}

func (p *Pipeline) handle(msg *nats.Msg) {
	var ev TranscriptEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		logger.L.Warnf("transcriptpipe: invalid event: %v", err)
		return
	}
	if ev.TranscriptRaw == "" {
		return // 空转写跳过
	}

	sessionID, speakerUserID, ok := p.resolver(ev.CallID)
	if !ok {
		logger.L.Warnf("transcriptpipe: session not found call_id=%s", ev.CallID)
		return
	}

	// 异步写入，避免阻塞 NATS 消息分发（LLM Refine 可能耗时数秒）
	go func() {
		_, err := p.writer.Write(context.Background(), callsegment.WriteReq{
			SessionID:     sessionID,
			CallID:        ev.CallID,
			SegmentSeq:    ev.SegmentSeq,
			SpeakerRole:   ev.SpeakerRole,
			SpeakerUserID: speakerUserID,
			TranscriptRaw: ev.TranscriptRaw,
			StartedAtMs:   ev.StartedAtMs,
			Provider:      ev.Provider,
		})
		if err != nil {
			logger.L.Errorf("transcriptpipe: write segment call_id=%s seq=%d err=%v",
				ev.CallID, ev.SegmentSeq, err)
		}
	}()
}
