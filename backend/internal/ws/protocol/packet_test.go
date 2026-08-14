package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPacketMarshal(t *testing.T) {
	payload := AuthPayload{
		Token:    "test-token",
		DeviceID: "device-123",
		Platform: "ios",
	}
	payloadBytes, _ := json.Marshal(payload)

	pkt := Packet{
		Cmd:     CmdAuth,
		Seq:     1,
		Payload: payloadBytes,
	}

	data, err := json.Marshal(pkt)
	if err != nil {
		t.Fatalf("failed to marshal packet: %v", err)
	}

	if string(data) == "" {
		t.Error("expected non-empty JSON")
	}
}

func TestPacketUnmarshal(t *testing.T) {
	jsonData := `{"cmd":"auth","seq":1,"payload":{"token":"test","device_id":"dev1","platform":"ios"}}`

	var pkt Packet
	err := json.Unmarshal([]byte(jsonData), &pkt)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if pkt.Cmd != "auth" {
		t.Errorf("expected cmd 'auth', got '%s'", pkt.Cmd)
	}
	if pkt.Seq != 1 {
		t.Errorf("expected seq 1, got %d", pkt.Seq)
	}
}

func TestAuthPayload(t *testing.T) {
	payload := AuthPayload{
		Token:    "my-token",
		DeviceID: "device-001",
		Platform: "android",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded AuthPayload
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Token != payload.Token {
		t.Errorf("token mismatch")
	}
	if decoded.DeviceID != payload.DeviceID {
		t.Errorf("device_id mismatch")
	}
	if decoded.Platform != payload.Platform {
		t.Errorf("platform mismatch")
	}
}

func TestSendMsgPayload(t *testing.T) {
	payload := SendMsgPayload{
		SessionID:   "session-123",
		ThreadID:    "topic-a",
		ClientMsgID: "client-msg-456",
		MsgType:     1,
		Content:     "Hello World",
	}

	data, _ := json.Marshal(payload)

	var decoded SendMsgPayload
	json.Unmarshal(data, &decoded)

	if decoded.SessionID != "session-123" {
		t.Errorf("session_id mismatch")
	}
	if decoded.ThreadID != "topic-a" {
		t.Errorf("thread_id mismatch")
	}
	if decoded.MsgType != 1 {
		t.Errorf("msg_type mismatch")
	}
}

func TestPushMsgPayload(t *testing.T) {
	payload := PushMsgPayload{
		InboxSeq:    1,
		MsgID:       100,
		SessionID:   "session-123",
		ThreadID:    "topic-push",
		SenderID:    1001,
		SenderType:  1,
		MsgType:     1,
		Content:     "Test message",
		IsRevoked:   false,
		IsStreaming: true,
		CreatedAt:   1700000000,
	}

	data, _ := json.Marshal(payload)

	var decoded PushMsgPayload
	json.Unmarshal(data, &decoded)

	if decoded.MsgID != 100 {
		t.Errorf("msg_id mismatch")
	}
	if decoded.ThreadID != "topic-push" {
		t.Errorf("thread_id mismatch")
	}
	if !decoded.IsStreaming {
		t.Error("is_streaming should be true")
	}
}

func TestStreamChunkPayload(t *testing.T) {
	payload := StreamChunkPayload{
		MsgID:        123,
		SessionID:    "session-456",
		ThreadID:     "topic-stream",
		SenderID:     1001,
		DeltaContent: "Hello",
		IsFinish:     false,
	}

	data, _ := json.Marshal(payload)

	var decoded StreamChunkPayload
	json.Unmarshal(data, &decoded)

	if decoded.DeltaContent != "Hello" {
		t.Errorf("delta_content mismatch")
	}
	if decoded.ThreadID != "topic-stream" {
		t.Errorf("thread_id mismatch")
	}
	if decoded.IsFinish {
		t.Error("is_finish should be false")
	}
}

func TestSessionActivityPayload(t *testing.T) {
	payload := SessionActivityPayload{
		SessionID:    "session-123",
		Kind:         SessionActivityKindComposing,
		Active:       true,
		ActorID:      1001,
		ActorType:    SessionActivityActorTypeHuman,
		ExecutorID:   1001,
		ExecutorType: SessionActivityActorTypeHuman,
		Source:       SessionActivitySourceHumanInput,
		UpdatedAt:    1700000000,
		ExpiresAt:    1700005000,
	}

	data, _ := json.Marshal(payload)

	var decoded SessionActivityPayload
	json.Unmarshal(data, &decoded)

	if decoded.Kind != SessionActivityKindComposing {
		t.Errorf("kind mismatch")
	}
	if decoded.ActorID != 1001 {
		t.Errorf("actor_id mismatch")
	}
	if decoded.Source != SessionActivitySourceHumanInput {
		t.Errorf("source mismatch")
	}
}

func TestCommandConstants(t *testing.T) {
	tests := []struct {
		name  string
		cmd   string
		value string
	}{
		{"auth", CmdAuth, "auth"},
		{"auth_ack", CmdAuthAck, "auth_ack"},
		{"ping", CmdPing, "ping"},
		{"pong", CmdPong, "pong"},
		{"send_msg", CmdSendMsg, "send_msg"},
		{"edit_msg", CmdEditMsg, "edit_msg"},
		{"update_binding_card", CmdUpdateBindingCard, "update_binding_card"},
		{"send_ack", CmdSendAck, "send_ack"},
		{"send_nack", CmdSendNack, "send_nack"},
		{"retry_msg", CmdRetryMsg, "retry_msg"},
		{"retry_msg_ack", CmdRetryMsgAck, "retry_msg_ack"},
		{"event_ack", CmdEventAck, "event_ack"},
		{"push_msg", CmdPushMsg, "push_msg"},
		{"push_edit", CmdPushEdit, "push_edit"},
		{"session_read", CmdSessionRead, "session_read"},
		{"session_read_ack", CmdSessionReadAck, "session_read_ack"},
		{"session_read_sync", CmdSessionReadSync, "session_read_sync"},
		{"session_member_changed", CmdSessionMemberChanged, "session_member_changed"},
		{"session_access_revoked", CmdSessionAccessRevoked, "session_access_revoked"},
		{"event_result", CmdEventResult, "event_result"},
		{"session_activity_set", CmdSessionActivitySet, "session_activity_set"},
		{"session_activity_sync", CmdSessionActivitySync, "session_activity_sync"},
		{"session_activity_list", CmdSessionActivityList, "session_activity_list"},
		{"session_activity_list_resp", CmdSessionActivityListResp, "session_activity_list_resp"},
		{"stream_chunk", CmdStreamChunk, "stream_chunk"},
		{"stream_finish", CmdStreamFinish, "stream_finish"},
		{"stream_stop", CmdStreamStop, "stream_stop"},
		{"stream_error", CmdStreamError, "stream_error"},
		{"agent_delivery_error", CmdAgentDeliveryError, "agent_delivery_error"},
		{"agent_delivery_status", CmdAgentDeliveryStatus, "agent_delivery_status"},
		{"agent_output_get", CmdAgentOutputGet, "agent_output_get"},
		{"agent_output_get_resp", CmdAgentOutputGetResp, "agent_output_get_resp"},
		{"agent_output_stop", CmdAgentOutputStop, "agent_output_stop"},
		{"agent_output_stop_ack", CmdAgentOutputStopAck, "agent_output_stop_ack"},
		{"agent_output_status", CmdAgentOutputStatus, "agent_output_status"},
		{"event_edit", CmdEventEdit, "event_edit"},
		{"event_stop", CmdEventStop, "event_stop"},
		{"event_stop_ack", CmdEventStopAck, "event_stop_ack"},
		{"event_stop_result", CmdEventStopResult, "event_stop_result"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cmd != tt.value {
				t.Errorf("expected '%s', got '%s'", tt.value, tt.cmd)
			}
		})
	}
}

func TestPullSyncPayload(t *testing.T) {
	payload := PullSyncPayload{
		LastInboxSeq: 100,
	}

	data, _ := json.Marshal(payload)

	var decoded PullSyncPayload
	json.Unmarshal(data, &decoded)

	if decoded.LastInboxSeq != 100 {
		t.Errorf("last_inbox_seq mismatch")
	}
}

func TestSendNackPayload(t *testing.T) {
	payload := SendNackPayload{
		ClientMsgID: "cmsg-123",
		Code:        4003,
		Msg:         "permission denied",
	}

	data, _ := json.Marshal(payload)

	var decoded SendNackPayload
	json.Unmarshal(data, &decoded)

	if decoded.ClientMsgID != "cmsg-123" {
		t.Errorf("client_msg_id mismatch")
	}
	if decoded.Code != 4003 {
		t.Errorf("code mismatch")
	}
}

func TestPullSyncRespPayload(t *testing.T) {
	payload := PullSyncRespPayload{
		HasMore: true,
		Messages: []PushMsgPayload{
			{MsgID: 1, Content: "msg1"},
			{MsgID: 2, Content: "msg2"},
		},
		UnreadSnapshot: map[string]int{
			"9f0f7f26-8b1b-4d46-ac8b-5f6d0dd2d94d": 3,
		},
	}

	data, _ := json.Marshal(payload)

	var decoded PullSyncRespPayload
	json.Unmarshal(data, &decoded)

	if !decoded.HasMore {
		t.Error("has_more should be true")
	}
	if len(decoded.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(decoded.Messages))
	}
	if decoded.UnreadSnapshot["9f0f7f26-8b1b-4d46-ac8b-5f6d0dd2d94d"] != 3 {
		t.Errorf("unexpected unread snapshot value")
	}
}

func TestPullSyncRespPayloadIncludesEmptyUnreadSnapshot(t *testing.T) {
	payload := PullSyncRespPayload{
		HasMore:        false,
		Messages:       []PushMsgPayload{},
		UnreadSnapshot: map[string]int{},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := decoded["unread_snapshot"]; !ok {
		t.Fatal("expected unread_snapshot field to be present even when empty")
	}
}

func TestStreamErrorPayload(t *testing.T) {
	payload := StreamErrorPayload{
		MsgID:     123,
		SessionID: "session-err",
		ErrorCode: 500,
		ErrorMsg:  "Internal error",
	}

	data, _ := json.Marshal(payload)

	var decoded StreamErrorPayload
	json.Unmarshal(data, &decoded)

	if decoded.ErrorCode != 500 {
		t.Errorf("error_code mismatch")
	}
	if decoded.ErrorMsg != "Internal error" {
		t.Errorf("error_msg mismatch")
	}
}

func TestAgentEventResultPayload(t *testing.T) {
	payload := AgentEventResultPayload{
		EventID:   "evt-1",
		Status:    AgentEventResultFailed,
		Code:      AgentDeliveryCodeProcessingFailed,
		Msg:       "processing failed",
		UpdatedAt: 1704067204000,
	}

	data, _ := json.Marshal(payload)

	var decoded AgentEventResultPayload
	json.Unmarshal(data, &decoded)

	if decoded.EventID != "evt-1" {
		t.Errorf("event_id mismatch")
	}
	if decoded.Status != AgentEventResultFailed {
		t.Errorf("status mismatch")
	}
	if decoded.Code != AgentDeliveryCodeProcessingFailed {
		t.Errorf("code mismatch")
	}
}

func TestOverrideStreamPayload(t *testing.T) {
	payload := OverrideStreamPayload{
		SessionID:   "session-override",
		TargetMsgID: 999,
	}

	data, _ := json.Marshal(payload)

	var decoded OverrideStreamPayload
	json.Unmarshal(data, &decoded)

	if decoded.TargetMsgID != 999 {
		t.Errorf("target_msg_id mismatch")
	}
}

func TestReAuthPayload(t *testing.T) {
	payload := ReAuthPayload{
		Token: "refresh-token-xyz",
	}

	data, _ := json.Marshal(payload)

	var decoded ReAuthPayload
	json.Unmarshal(data, &decoded)

	if decoded.Token != "refresh-token-xyz" {
		t.Error("token mismatch")
	}
}

func TestClientStreamChunkPayload(t *testing.T) {
	payload := ClientStreamChunkPayload{
		SessionID:    "client-session",
		SenderID:     1001,
		DeltaContent: "chunk data",
		IsFinish:     true,
	}

	data, _ := json.Marshal(payload)

	var decoded ClientStreamChunkPayload
	json.Unmarshal(data, &decoded)

	if !decoded.IsFinish {
		t.Error("is_finish should be true")
	}
}

func TestStreamFinishPayloadQuotedMessageID(t *testing.T) {
	payload := StreamFinishPayload{
		MsgID:           9001,
		SessionID:       "group-session",
		ThreadID:        "topic-finish",
		SenderID:        1001,
		FinalContent:    "done",
		QuotedMessageID: 18889990222,
		IsFinish:        true,
	}

	data, _ := json.Marshal(payload)

	var decoded StreamFinishPayload
	json.Unmarshal(data, &decoded)

	if decoded.QuotedMessageID != 18889990222 {
		t.Fatalf("quoted_message_id mismatch: got=%d", decoded.QuotedMessageID)
	}
	if decoded.ThreadID != "topic-finish" {
		t.Fatalf("thread_id mismatch: got=%q", decoded.ThreadID)
	}
}

// TestStreamChunkPayloadIsThinkingRoundTrip 守卫:is_thinking 字段必须能正确序列化/反序列化,
// 且为 false 时省略(omitempty),避免后续重构静默丢失该协议字段。
func TestStreamChunkPayloadIsThinkingRoundTrip(t *testing.T) {
	thinking := StreamChunkPayload{SessionID: "s1", DeltaContent: "x", IsThinking: true}
	raw, err := json.Marshal(thinking)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !strings.Contains(string(raw), `"is_thinking":true`) {
		t.Fatalf("thinking payload must contain is_thinking:true, got %s", raw)
	}
	var decoded StreamChunkPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !decoded.IsThinking {
		t.Fatalf("is_thinking lost in round-trip")
	}

	normalRaw, _ := json.Marshal(StreamChunkPayload{SessionID: "s1", DeltaContent: "x"})
	if strings.Contains(string(normalRaw), "is_thinking") {
		t.Fatalf("non-thinking payload must omit is_thinking, got %s", normalRaw)
	}
}
