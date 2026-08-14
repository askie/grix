package doubao

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/voiceadapter"
	proto "github.com/askie/grix/backend/internal/voiceadapter/doubao/doubaoprotocol"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 协议测试 ---

func TestProtocol_MarshalUnmarshal_FullClient(t *testing.T) {
	msg := &proto.Message{
		Type:      proto.MsgTypeFullClient,
		TypeFlag:  proto.FlagWithEvent,
		Event:     proto.EventStartConnection,
		Payload:   []byte("{}"),
	}
	data, err := proto.Marshal(msg, proto.SerializationJSON)
	require.NoError(t, err)
	assert.True(t, len(data) > 4)

	parsed, err := proto.Unmarshal(data)
	require.NoError(t, err)
	assert.Equal(t, proto.MsgTypeFullClient, parsed.Type)
	assert.Equal(t, proto.EventStartConnection, parsed.Event)
	assert.Equal(t, []byte("{}"), parsed.Payload)
}

func TestProtocol_MarshalUnmarshal_AudioClient(t *testing.T) {
	pcm := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	msg := &proto.Message{
		Type:      proto.MsgTypeAudioOnlyClient,
		TypeFlag:  proto.FlagWithEvent,
		Event:     proto.EventAudioInput,
		SessionID: "sess-123",
		Payload:   pcm,
	}
	data, err := proto.Marshal(msg, proto.SerializationRaw)
	require.NoError(t, err)

	parsed, err := proto.Unmarshal(data)
	require.NoError(t, err)
	assert.Equal(t, proto.MsgTypeAudioOnlyClient, parsed.Type)
	assert.Equal(t, proto.EventAudioInput, parsed.Event)
	assert.Equal(t, "sess-123", parsed.SessionID)
	assert.Equal(t, pcm, parsed.Payload)
}

func TestProtocol_MarshalUnmarshal_WithSessionID(t *testing.T) {
	msg := &proto.Message{
		Type:      proto.MsgTypeFullClient,
		TypeFlag:  proto.FlagWithEvent,
		Event:     proto.EventStartSession,
		SessionID: "my-session",
		Payload:   []byte(`{"tts":{}}`),
	}
	data, err := proto.Marshal(msg, proto.SerializationJSON)
	require.NoError(t, err)

	parsed, err := proto.Unmarshal(data)
	require.NoError(t, err)
	assert.Equal(t, proto.EventStartSession, parsed.Event)
	assert.Equal(t, "my-session", parsed.SessionID)
}

// --- 适配器基础测试 ---

func TestAdapter_Identity(t *testing.T) {
	a := New()
	assert.Equal(t, "doubao_realtime", a.Family())
	assert.Equal(t, "doubao_realtime_v1", a.AdapterID())
}

func TestAdapter_StartMissingConfig(t *testing.T) {
	cases := []voiceadapter.VoiceAgentConfig{
		{AgentID: 1, Mode: voiceadapter.ModeDuplex},
		{AgentID: 1, Mode: voiceadapter.ModeDuplex, APIKey: "no-colon"},
		{AgentID: 1, Mode: voiceadapter.ModeDuplex, APIKey: ":token"},
		{AgentID: 1, Mode: voiceadapter.ModeDuplex, APIKey: "appid:"},
	}
	for i, cfg := range cases {
		a := New()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		audio := make(chan voiceadapter.PCMFrame)
		close(audio)
		_, _, err := a.Start(ctx, cfg, audio)
		cancel()
		assert.ErrorIs(t, err, ErrNotConfigured, "case %d", i)
	}
}

func TestAdapter_InterruptBeforeStart(t *testing.T) {
	assert.NoError(t, New().Interrupt(context.Background()))
}

func TestAdapter_CloseBeforeStart(t *testing.T) {
	assert.NoError(t, New().Close(context.Background()))
}

func TestAdapter_Registration(t *testing.T) {
	voiceadapter.ResetForTest()
	defer voiceadapter.ResetForTest()
	RegisterInRegistry()
	bridge, err := voiceadapter.New("doubao_realtime")
	require.NoError(t, err)
	assert.Equal(t, "doubao_realtime", bridge.Family())
}

func TestParseAPIKey(t *testing.T) {
	appID, token, err := parseAPIKey("myapp:mytoken123")
	require.NoError(t, err)
	assert.Equal(t, "myapp", appID)
	assert.Equal(t, "mytoken123", token)
}

func TestParseAPIKey_Invalid(t *testing.T) {
	_, _, err := parseAPIKey("")
	assert.ErrorIs(t, err, ErrNotConfigured)
	_, _, err = parseAPIKey("nocolon")
	assert.ErrorIs(t, err, ErrNotConfigured)
}

// --- Mock WebSocket 集成测试 ---

// mockServer 模拟豆包服务端，处理 StartConnection + StartSession 握手
func mockServer(t *testing.T, afterHandshake func(conn *websocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证鉴权 Header
		assert.NotEmpty(t, r.Header.Get("X-Api-App-ID"))
		assert.NotEmpty(t, r.Header.Get("X-Api-Access-Key"))

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// 处理 StartConnection
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		msg, _ := proto.Unmarshal(data)
		assert.Equal(t, proto.EventStartConnection, msg.Event)

		// 回复 ConnectionStarted
		resp := &proto.Message{
			Type:      proto.MsgTypeFullServer,
			TypeFlag:  proto.FlagWithEvent,
			Event:     proto.EventConnectionStarted,
			ConnectID: "conn-123",
			Payload:   []byte("{}"),
		}
		frame, _ := proto.Marshal(resp, proto.SerializationJSON)
		conn.WriteMessage(websocket.BinaryMessage, frame)

		// 处理 StartSession
		_, data, err = conn.ReadMessage()
		if err != nil {
			return
		}
		msg, _ = proto.Unmarshal(data)
		assert.Equal(t, proto.EventStartSession, msg.Event)

		// 回复 SessionStarted
		resp = &proto.Message{
			Type:      proto.MsgTypeFullServer,
			TypeFlag:  proto.FlagWithEvent,
			Event:     proto.EventSessionStarted,
			SessionID: msg.SessionID,
			Payload:   []byte(`{"dialog_id":"dlg-001"}`),
		}
		frame, _ = proto.Marshal(resp, proto.SerializationJSON)
		conn.WriteMessage(websocket.BinaryMessage, frame)

		if afterHandshake != nil {
			afterHandshake(conn)
		}
	}))
}

func TestAdapter_FullHandshakeAndReceiveAudio(t *testing.T) {
	srv := mockServer(t, func(conn *websocket.Conn) {
		// 发送一帧音频
		audioMsg := &proto.Message{
			Type:      proto.MsgTypeAudioOnlyServer,
			TypeFlag:  proto.FlagWithEvent,
			Event:     proto.EventTTSSentenceStart,
			SessionID: "test",
			Payload:   []byte{0xAA, 0xBB, 0xCC},
		}
		frame, _ := proto.Marshal(audioMsg, proto.SerializationRaw)
		conn.WriteMessage(websocket.BinaryMessage, frame)
		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	a := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	callerAudio := make(chan voiceadapter.PCMFrame, 10)
	outAudio, _, err := a.Start(ctx, voiceadapter.VoiceAgentConfig{
		AgentID:  1,
		Mode:     voiceadapter.ModeDuplex,
		Endpoint: wsURL,
		APIKey:   "testapp:testtoken",
	}, callerAudio)
	require.NoError(t, err)

	select {
	case frame := <-outAudio:
		assert.Equal(t, []byte{0xAA, 0xBB, 0xCC}, frame.Data)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for audio")
	}
	a.Close(context.Background())
}

func TestAdapter_ReceiveTranscript(t *testing.T) {
	srv := mockServer(t, func(conn *websocket.Conn) {
		// 发送 ASR 识别完成事件
		payload, _ := json.Marshal(map[string]any{"text": "你好世界"})
		msg := &proto.Message{
			Type:      proto.MsgTypeFullServer,
			TypeFlag:  proto.FlagWithEvent,
			Event:     proto.EventASRSentenceDone,
			SessionID: "test",
			Payload:   payload,
		}
		frame, _ := proto.Marshal(msg, proto.SerializationJSON)
		conn.WriteMessage(websocket.BinaryMessage, frame)
		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	a := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	callerAudio := make(chan voiceadapter.PCMFrame, 10)
	_, events, err := a.Start(ctx, voiceadapter.VoiceAgentConfig{
		AgentID:  1,
		Mode:     voiceadapter.ModeDuplex,
		Endpoint: wsURL,
		APIKey:   "testapp:testtoken",
	}, callerAudio)
	require.NoError(t, err)

	select {
	case evt := <-events:
		assert.Equal(t, voiceadapter.EventKindTranscript, evt.Kind)
		assert.Equal(t, "你好世界", evt.Transcript)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for transcript")
	}
	a.Close(context.Background())
}

func TestAdapter_SendAudio(t *testing.T) {
	received := make(chan []byte, 10)
	srv := mockServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			msg, err := proto.Unmarshal(data)
			if err != nil {
				continue
			}
			if msg.Type == proto.MsgTypeAudioOnlyClient && msg.Event == proto.EventAudioInput {
				received <- msg.Payload
			}
		}
	})
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	a := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	callerAudio := make(chan voiceadapter.PCMFrame, 10)
	_, _, err := a.Start(ctx, voiceadapter.VoiceAgentConfig{
		AgentID:  1,
		Mode:     voiceadapter.ModeDuplex,
		Endpoint: wsURL,
		APIKey:   "testapp:testtoken",
	}, callerAudio)
	require.NoError(t, err)

	testPCM := []byte{0x01, 0x02, 0x03, 0x04}
	callerAudio <- voiceadapter.PCMFrame{Data: testPCM}

	select {
	case got := <-received:
		assert.Equal(t, testPCM, got)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for audio on server")
	}
	a.Close(context.Background())
}

func TestAdapter_ReceiveError(t *testing.T) {
	srv := mockServer(t, func(conn *websocket.Conn) {
		msg := &proto.Message{
			Type:      proto.MsgTypeError,
			TypeFlag:  proto.FlagWithEvent,
			Event:     0,
			ErrorCode: 40001,
			Payload:   []byte("rate limit exceeded"),
		}
		frame, _ := proto.Marshal(msg, proto.SerializationJSON)
		conn.WriteMessage(websocket.BinaryMessage, frame)
		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	a := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	callerAudio := make(chan voiceadapter.PCMFrame, 10)
	_, events, err := a.Start(ctx, voiceadapter.VoiceAgentConfig{
		AgentID:  1,
		Mode:     voiceadapter.ModeDuplex,
		Endpoint: wsURL,
		APIKey:   "testapp:testtoken",
	}, callerAudio)
	require.NoError(t, err)

	select {
	case evt := <-events:
		assert.Equal(t, voiceadapter.EventKindError, evt.Kind)
		assert.Contains(t, evt.Err.Error(), "40001")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for error event")
	}
	a.Close(context.Background())
}
