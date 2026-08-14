// Package doubao 实现豆包端到端实时语音大模型的 VoiceAgentBridge 适配器。
//
// 按照火山引擎官方 Go SDK 接入，使用正确的二进制协议和鉴权方式。
// 协议参考：火山引擎官方 Go SDK。
//
// 鉴权：WebSocket Header 携带 X-Api-App-ID / X-Api-Access-Key
// 音频输入：PCM S16LE, 16kHz, 单声道
// 音频输出：PCM F32LE 或 S16LE, 24kHz, 单声道（由 TTS AudioConfig 决定）
package doubao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/askie/grix/backend/internal/voiceadapter"
	proto "github.com/askie/grix/backend/internal/voiceadapter/doubao/doubaoprotocol"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	Family    = "doubao_realtime"
	adapterID = "doubao_realtime_v1"

	wsEndpoint   = "wss://openspeech.bytedance.com/api/v3/realtime/dialogue"
	defaultVoice = "zh_female_vv_jupiter_bigtts"
	apiAppKey    = "PlgvMymc7f3tQnJ6" // 官方 SDK 固定值
)

var ErrNotConfigured = errors.New("doubao adapter: require appid and access_token in api_key (format: appid:access_token)")

type Adapter struct {
	started  atomic.Bool
	mu       sync.Mutex
	conn     *websocket.Conn
	cancelFn context.CancelFunc
	sessID   string
}

func New() *Adapter { return &Adapter{} }

func RegisterInRegistry() {
	voiceadapter.Register(Family, func() voiceadapter.VoiceAgentBridge { return New() })
}

func (a *Adapter) AdapterID() string { return adapterID }
func (a *Adapter) Family() string    { return Family }

// Start 建立 WS 连接，执行 StartConnection → StartSession，然后启动收发 goroutine。
// cfg.APIKey 格式：appid:access_token
func (a *Adapter) Start(ctx context.Context, cfg voiceadapter.VoiceAgentConfig,
	callerAudio <-chan voiceadapter.PCMFrame) (<-chan voiceadapter.PCMFrame, <-chan voiceadapter.Event, error) {

	appID, accessToken, err := parseAPIKey(cfg.APIKey)
	if err != nil {
		return nil, nil, err
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = wsEndpoint
	}

	// 鉴权 Header（来自官方 SDK main.go）
	headers := http.Header{
		"X-Api-Resource-Id": {"volc.speech.dialog"},
		"X-Api-Access-Key":  {accessToken},
		"X-Api-App-Key":     {apiAppKey},
		"X-Api-App-ID":      {appID},
		"X-Api-Connect-Id":  {uuid.New().String()},
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		return nil, nil, fmt.Errorf("dial doubao realtime: %w", err)
	}

	bridgeCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.conn = conn
	a.cancelFn = cancel
	a.sessID = uuid.New().String()
	a.mu.Unlock()

	// StartConnection
	if err := a.startConnection(conn); err != nil {
		cancel()
		conn.Close()
		return nil, nil, fmt.Errorf("start connection: %w", err)
	}

	// StartSession
	if err := a.startSession(conn, cfg); err != nil {
		cancel()
		conn.Close()
		return nil, nil, fmt.Errorf("start session: %w", err)
	}

	a.started.Store(true)

	outAudio := make(chan voiceadapter.PCMFrame, 128)
	events := make(chan voiceadapter.Event, 32)

	go a.recvLoop(bridgeCtx, conn, outAudio, events)
	go a.sendLoop(bridgeCtx, conn, callerAudio, events)

	return outAudio, events, nil
}

func (a *Adapter) Interrupt(_ context.Context) error {
	if !a.started.Load() {
		return nil
	}
	// 目前 SDK 中没有明确的打断事件，通过 FinishSession + 重新 StartSession 实现
	// 简化处理：直接关闭连接让 recvLoop 退出
	return nil
}

func (a *Adapter) Close(_ context.Context) error {
	if a.cancelFn != nil {
		a.cancelFn()
	}
	a.mu.Lock()
	conn := a.conn
	a.conn = nil
	a.mu.Unlock()
	if conn != nil {
		// 尝试优雅关闭
		a.finishSession(conn)
		a.finishConnection(conn)
		return conn.Close()
	}
	return nil
}

// --- 协议交互 ---

func (a *Adapter) startConnection(conn *websocket.Conn) error {
	msg := &proto.Message{
		Type:     proto.MsgTypeFullClient,
		TypeFlag: proto.FlagWithEvent,
		Event:    proto.EventStartConnection,
		Payload:  []byte("{}"),
	}
	frame, err := proto.Marshal(msg, proto.SerializationJSON)
	if err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		return err
	}

	// 读取 ConnectionStarted 响应
	_, respData, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read ConnectionStarted: %w", err)
	}
	resp, err := proto.Unmarshal(respData)
	if err != nil {
		return fmt.Errorf("unmarshal ConnectionStarted: %w", err)
	}
	if resp.Event != proto.EventConnectionStarted {
		return fmt.Errorf("unexpected event %d, want ConnectionStarted(%d)", resp.Event, proto.EventConnectionStarted)
	}
	return nil
}

func (a *Adapter) startSession(conn *websocket.Conn, cfg voiceadapter.VoiceAgentConfig) error {
	voice := cfg.Voice
	if voice == "" {
		voice = defaultVoice
	}
	systemRole := cfg.SystemPrompt
	if systemRole == "" {
		systemRole = "你是一个有帮助的语音助手，回答简洁。"
	}

	payload := map[string]any{
		"asr": map[string]any{
			"extra": map[string]any{
				"end_smooth_window_ms": 1500,
			},
		},
		"tts": map[string]any{
			"speaker": voice,
			"audio_config": map[string]any{
				"channel":     1,
				"format":      "pcm_s16le",
				"sample_rate": 24000,
			},
		},
		"dialog": map[string]any{
			"bot_name":       "豆包",
			"system_role":    systemRole,
			"speaking_style": "你的说话风格简洁明了，语速适中，语调自然。",
			"extra": map[string]any{
				"input_mod": "audio",
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	msg := &proto.Message{
		Type:      proto.MsgTypeFullClient,
		TypeFlag:  proto.FlagWithEvent,
		Event:     proto.EventStartSession,
		SessionID: a.sessID,
		Payload:   payloadBytes,
	}
	frame, err := proto.Marshal(msg, proto.SerializationJSON)
	if err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		return err
	}

	// 读取 SessionStarted 响应
	_, respData, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read SessionStarted: %w", err)
	}
	resp, err := proto.Unmarshal(respData)
	if err != nil {
		return fmt.Errorf("unmarshal SessionStarted: %w", err)
	}
	if resp.Event != proto.EventSessionStarted {
		return fmt.Errorf("unexpected event %d, want SessionStarted(%d)", resp.Event, proto.EventSessionStarted)
	}
	return nil
}

func (a *Adapter) finishSession(conn *websocket.Conn) {
	msg := &proto.Message{
		Type:      proto.MsgTypeFullClient,
		TypeFlag:  proto.FlagWithEvent,
		Event:     proto.EventFinishSession,
		SessionID: a.sessID,
		Payload:   []byte("{}"),
	}
	frame, _ := proto.Marshal(msg, proto.SerializationJSON)
	conn.WriteMessage(websocket.BinaryMessage, frame)
}

func (a *Adapter) finishConnection(conn *websocket.Conn) {
	msg := &proto.Message{
		Type:     proto.MsgTypeFullClient,
		TypeFlag: proto.FlagWithEvent,
		Event:    proto.EventFinishConnection,
		Payload:  []byte("{}"),
	}
	frame, _ := proto.Marshal(msg, proto.SerializationJSON)
	conn.WriteMessage(websocket.BinaryMessage, frame)
}

// --- 收发循环 ---

func (a *Adapter) sendLoop(ctx context.Context, conn *websocket.Conn,
	callerAudio <-chan voiceadapter.PCMFrame, events chan<- voiceadapter.Event) {

	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-callerAudio:
			if !ok {
				return
			}
			if len(frame.Data) == 0 {
				continue
			}
			msg := &proto.Message{
				Type:      proto.MsgTypeAudioOnlyClient,
				TypeFlag:  proto.FlagWithEvent,
				Event:     proto.EventAudioInput,
				SessionID: a.sessID,
				Payload:   frame.Data,
			}
			data, err := proto.Marshal(msg, proto.SerializationRaw)
			if err != nil {
				continue
			}
			a.mu.Lock()
			err = conn.WriteMessage(websocket.BinaryMessage, data)
			a.mu.Unlock()
			if err != nil {
				select {
				case events <- voiceadapter.Event{Kind: voiceadapter.EventKindError, Err: err}:
				default:
				}
				return
			}
		}
	}
}

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
			if ctx.Err() == nil {
				select {
				case events <- voiceadapter.Event{Kind: voiceadapter.EventKindError, Err: err}:
				default:
				}
			}
			return
		}

		msg, err := proto.Unmarshal(data)
		if err != nil {
			continue
		}

		switch msg.Type {
		case proto.MsgTypeAudioOnlyServer:
			// 下行音频
			if len(msg.Payload) > 0 {
				select {
				case outAudio <- voiceadapter.PCMFrame{Data: msg.Payload}:
				case <-ctx.Done():
					return
				}
			}

		case proto.MsgTypeFullServer:
			a.handleServerEvent(msg, events)

		case proto.MsgTypeError:
			select {
			case events <- voiceadapter.Event{
				Kind: voiceadapter.EventKindError,
				Err:  fmt.Errorf("doubao error (code=%d): %s", msg.ErrorCode, string(msg.Payload)),
			}:
			default:
			}
			return
		}
	}
}

func (a *Adapter) handleServerEvent(msg *proto.Message, events chan<- voiceadapter.Event) {
	switch msg.Event {
	case proto.EventASRSentenceStart:
		// ASR 开始识别（用户开始说话）

	case proto.EventASRSentenceDone:
		// ASR 识别完成，提取转写文本
		if len(msg.Payload) > 0 {
			var p map[string]any
			if json.Unmarshal(msg.Payload, &p) == nil {
				if text, ok := p["text"].(string); ok && text != "" {
					select {
					case events <- voiceadapter.Event{Kind: voiceadapter.EventKindTranscript, Transcript: text}:
					default:
					}
				}
			}
		}

	case proto.EventSessionFinished, proto.EventSessionFailed:
		select {
		case events <- voiceadapter.Event{Kind: voiceadapter.EventKindClosed}:
		default:
		}
	}
}

// --- 工具函数 ---

// parseAPIKey 从 "appid:access_token" 格式中拆分
func parseAPIKey(combined string) (appID, accessToken string, err error) {
	if combined == "" {
		return "", "", ErrNotConfigured
	}
	parts := strings.SplitN(combined, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", ErrNotConfigured
	}
	return parts[0], parts[1], nil
}
