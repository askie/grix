// Package openai 实现 OpenAI Realtime API 的 VoiceAgentBridge 适配器。
//
// 协议参考：https://platform.openai.com/docs/guides/realtime
//
// 音频格式：PCM16, 16kHz, 单声道, little-endian
// WS URL：wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview
// 鉴权：Authorization: Bearer <api_key> + OpenAI-Beta: realtime=v1
package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/askie/grix/backend/internal/voiceadapter"
	"github.com/gorilla/websocket"
)

const (
	Family    = "openai_realtime"
	adapterID = "openai_realtime_v1"

	defaultVoice    = "alloy"
	realtimeWSURL   = "wss://api.openai.com/v1/realtime"
	// OpenAI Realtime API 音频格式：PCM16, 24kHz, 单声道
	// 每次发送的 PCM 帧大小：20ms @ 24kHz = 480 samples = 960 bytes
	pcmChunkBytes = 960
	pcmSampleRate = 24000
)

// ErrNotConfigured 表示缺少 BYOK 必填配置（api_key 或 model）。
var ErrNotConfigured = errors.New("openai realtime adapter: api_key or model not configured")

// Adapter 实现 VoiceAgentBridge 接口。
type Adapter struct {
	started  atomic.Bool
	mu       sync.Mutex
	conn     *websocket.Conn
	cancelFn context.CancelFunc
}

func New() *Adapter { return &Adapter{} }

func RegisterInRegistry() {
	voiceadapter.Register(Family, func() voiceadapter.VoiceAgentBridge {
		return New()
	})
}

func (a *Adapter) AdapterID() string { return adapterID }
func (a *Adapter) Family() string    { return Family }

// Start 建立 WS 连接，配置 session，启动双工音频流。
func (a *Adapter) Start(ctx context.Context, cfg voiceadapter.VoiceAgentConfig,
	callerAudio <-chan voiceadapter.PCMFrame) (<-chan voiceadapter.PCMFrame, <-chan voiceadapter.Event, error) {

	apiKey := cfg.APIKey
	if apiKey == "" {
		return nil, nil, ErrNotConfigured
	}
	model := cfg.Model
	if model == "" {
		return nil, nil, ErrNotConfigured
	}
	voice := cfg.Voice
	if voice == "" {
		voice = defaultVoice
	}
	base := cfg.Endpoint
	if base == "" {
		base = realtimeWSURL
	}

	wsURL := fmt.Sprintf("%s?model=%s", base, model)
	headers := http.Header{
		"Authorization": {"Bearer " + apiKey},
		"OpenAI-Beta":   {"realtime=v1"},
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return nil, nil, fmt.Errorf("dial openai realtime: %w", err)
	}

	bridgeCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.conn = conn
	a.cancelFn = cancel
	a.mu.Unlock()
	a.started.Store(true)

	outAudio := make(chan voiceadapter.PCMFrame, 128)
	events := make(chan voiceadapter.Event, 32)

	// 发送 session.update 配置
	if err := a.sendSessionUpdate(conn, cfg, voice); err != nil {
		cancel()
		conn.Close()
		return nil, nil, fmt.Errorf("session update: %w", err)
	}

	// 启动接收 goroutine
	go a.recvLoop(bridgeCtx, conn, outAudio, events)
	// 启动发送 goroutine
	go a.sendLoop(bridgeCtx, conn, callerAudio, events)

	return outAudio, events, nil
}

// Interrupt 发送 response.cancel 打断当前 AI 应答。
func (a *Adapter) Interrupt(_ context.Context) error {
	if !a.started.Load() {
		return nil
	}
	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.WriteJSON(map[string]string{"type": "response.cancel"})
}

// Close 关闭 WS 连接，释放资源。
func (a *Adapter) Close(_ context.Context) error {
	if a.cancelFn != nil {
		a.cancelFn()
	}
	a.mu.Lock()
	conn := a.conn
	a.conn = nil
	a.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// --- 内部实现 ---

// sendSessionUpdate 发送 session.update 配置消息。
func (a *Adapter) sendSessionUpdate(conn *websocket.Conn, cfg voiceadapter.VoiceAgentConfig, voice string) error {
	modalities := []string{"audio", "text"}
	if cfg.Mode == voiceadapter.ModeTranscriptionOnly {
		modalities = []string{"text"} // 仅转写模式不需要音频输出
	}

	instructions := cfg.SystemPrompt
	if instructions == "" {
		instructions = "You are a helpful voice assistant. Respond concisely."
	}

	msg := map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"modalities":              modalities,
			"instructions":            instructions,
			"voice":                   voice,
			"input_audio_format":      "pcm16",  // PCM16, 24kHz, 单声道
			"output_audio_format":     "pcm16",
			"input_audio_transcription": map[string]any{
				"model": "whisper-1",
			},
			"turn_detection": map[string]any{
				"type":                "server_vad",
				"threshold":           0.5,
				"prefix_padding_ms":   300,
				"silence_duration_ms": 500,
			},
		},
	}
	return conn.WriteJSON(msg)
}

// sendLoop 从 callerAudio channel 读取 PCM 帧，base64 编码后发送给 OpenAI。
func (a *Adapter) sendLoop(ctx context.Context, conn *websocket.Conn,
	callerAudio <-chan voiceadapter.PCMFrame, events chan<- voiceadapter.Event) {

	buf := make([]byte, 0, pcmChunkBytes*4)
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-callerAudio:
			if !ok {
				return
			}
			buf = append(buf, frame.Data...)
			// 积累到足够大小再发送，减少消息数量
			for len(buf) >= pcmChunkBytes {
				chunk := buf[:pcmChunkBytes]
				buf = buf[pcmChunkBytes:]
				encoded := base64.StdEncoding.EncodeToString(chunk)
				msg := map[string]string{
					"type":  "input_audio_buffer.append",
					"audio": encoded,
				}
				if err := conn.WriteJSON(msg); err != nil {
					select {
					case events <- voiceadapter.Event{Kind: voiceadapter.EventKindError, Err: err}:
					default:
					}
					return
				}
			}
		}
	}
}

// recvLoop 接收 OpenAI 返回的消息，解析音频帧和 transcript 事件。
func (a *Adapter) recvLoop(ctx context.Context, conn *websocket.Conn,
	outAudio chan<- voiceadapter.PCMFrame, events chan<- voiceadapter.Event) {

	defer func() {
		select {
		case events <- voiceadapter.Event{Kind: voiceadapter.EventKindClosed}:
		default:
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if !errors.Is(err, websocket.ErrCloseSent) && ctx.Err() == nil {
				select {
				case events <- voiceadapter.Event{Kind: voiceadapter.EventKindError, Err: err}:
				default:
				}
			}
			return
		}

		var msg map[string]json.RawMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		var msgType string
		if err := json.Unmarshal(msg["type"], &msgType); err != nil {
			continue
		}

		switch msgType {
		case "response.output_audio.delta":
			// 音频增量：base64 PCM16 数据（24kHz）
			var delta string
			if err := json.Unmarshal(msg["delta"], &delta); err != nil {
				continue
			}
			pcm, err := base64.StdEncoding.DecodeString(delta)
			if err != nil {
				continue
			}
			select {
			case outAudio <- voiceadapter.PCMFrame{Data: pcm}:
			case <-ctx.Done():
				return
			}

		case "response.output_audio_transcript.done":
			// 完整转写文本（final）
			var transcript string
			if err := json.Unmarshal(msg["transcript"], &transcript); err != nil {
				continue
			}
			if transcript != "" {
				select {
				case events <- voiceadapter.Event{Kind: voiceadapter.EventKindTranscript, Transcript: transcript}:
				default:
				}
			}

		case "error":
			// OpenAI 返回错误
			var errMsg struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(data, &errMsg); err == nil {
				select {
				case events <- voiceadapter.Event{
					Kind: voiceadapter.EventKindError,
					Err:  fmt.Errorf("openai realtime error: %s", errMsg.Error.Message),
				}:
				default:
				}
			}
		}
	}
}
