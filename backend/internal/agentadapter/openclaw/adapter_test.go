package openclaw

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestAdapter_Supports(t *testing.T) {
	a := NewAdapter()

	tests := []struct {
		name string
		meta agentadapter.AgentClientMeta
		want bool
	}{
		{
			name: "openclaw client type",
			meta: agentadapter.AgentClientMeta{ClientType: "openclaw"},
			want: true,
		},
		{
			name: "openclaw host type",
			meta: agentadapter.AgentClientMeta{HostType: "openclaw"},
			want: true,
		},
		{
			name: "claude client type",
			meta: agentadapter.AgentClientMeta{ClientType: "claude"},
			want: false,
		},
		{
			name: "empty",
			meta: agentadapter.AgentClientMeta{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.Supports(tt.meta); got != tt.want {
				t.Errorf("Supports(%+v) = %v, want %v", tt.meta, got, tt.want)
			}
		})
	}
}

func TestAdapter_NormalizeInbound(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	raw := []byte(`{
		"session_id":"session-1",
		"content":"审批文本",
		"extra":{
			"channel_data":{
				"execApproval":{
					"approvalId":"74569573",
					"approvalSlug":"74569573",
					"allowedDecisions":["allow-once","allow-always","deny"]
				},
				"grix":{
					"execApproval":{
						"approval_command_id":"74569573",
						"command":"echo hi",
						"host":"gateway",
						"cwd":"/tmp/demo",
						"expires_in_seconds":120
					}
				}
			}
		}
	}`)
	event, err := a.NormalizeInbound(ctx, raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if event.SessionID != "session-1" {
		t.Fatalf("session_id=%q want=session-1", event.SessionID)
	}
	if !strings.Contains(event.Content, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec approval card uri", event.Content)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	if _, ok := extra["channel_data"].(map[string]any); !ok {
		t.Fatalf("expected channel_data in extra, got %#v", extra)
	}
}

func TestAdapter_NormalizeInbound_ParsesRawOpenClawExecApprovalPending(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	raw := []byte(`{
		"session_id":"session-openclaw-pending",
		"content":"Approval required.",
		"extra":{
			"channel_data":{
				"openclaw":{
					"execApprovalPending":{
						"id":"approval_full_123",
						"createdAtMs":1700000000000,
						"expiresAtMs":1700000120000,
						"request":{
							"command":"npm run deploy",
							"commandArgv":["npm","run","deploy"],
							"envKeys":["DEBUG"],
							"cwd":"/srv/app",
							"nodeId":"node-9",
							"host":"gateway",
							"security":"allowlist",
							"ask":"always",
							"agentId":"main",
							"resolvedPath":"/srv/app/deploy.sh",
							"sessionKey":"agent:main:main",
							"turnSourceChannel":"grix",
							"turnSourceTo":"session-openclaw-pending",
							"turnSourceAccountId":"default",
							"turnSourceThreadId":"topic-1"
						}
					}
				}
			}
		}
	}`)

	event, err := a.NormalizeInbound(ctx, raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec approval card uri", event.Content)
	}

	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, ok := extra["channel_data"].(map[string]any)
	if !ok {
		t.Fatalf("expected channel_data in extra, got %#v", extra)
	}
	if _, ok := channelData["openclaw"].(map[string]any); !ok {
		t.Fatalf("expected openclaw namespace to be preserved, got %#v", channelData)
	}
	replyMeta, ok := channelData["execApproval"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized execApproval metadata, got %#v", channelData)
	}
	if got := replyMeta["approvalId"]; got != "approval_full_123" {
		t.Fatalf("approvalId=%v want=approval_full_123", got)
	}
	grixData, ok := channelData["grix"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized grix namespace, got %#v", channelData)
	}
	execApproval, ok := grixData["execApproval"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized grix execApproval payload, got %#v", grixData)
	}
	if got := execApproval["command"]; got != "npm run deploy" {
		t.Fatalf("command=%v want=npm run deploy", got)
	}
	if got := execApproval["host"]; got != "gateway" {
		t.Fatalf("host=%v want=gateway", got)
	}
}

func TestAdapter_NormalizeInbound_ParsesPlainTextExecApprovalFallback(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	raw, err := json.Marshal(map[string]any{
		"session_id": "session-plain-approval",
		"content": strings.Join([]string{
			"🔒 Exec approval required",
			"ID: approval_full_123",
			"Command:",
			"```",
			"npm run deploy",
			"echo done",
			"```",
			"CWD: /srv/app",
			"Node: node-9",
			"Host: gateway",
			"Expires in: 120s",
			"Mode: foreground (interactive approvals available in this chat).",
			"Background mode note: non-interactive runs cannot wait for chat approvals; use pre-approved policy (allow-always or ask=off).",
			"Reply with: /approve <id> allow-once|allow-always|deny",
		}, "\n"),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	event, err := a.NormalizeInbound(ctx, raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec approval card uri", event.Content)
	}

	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, ok := extra["channel_data"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized channel_data in extra, got %#v", extra)
	}
	replyMeta, ok := channelData["execApproval"].(map[string]any)
	if !ok {
		t.Fatalf("expected execApproval metadata, got %#v", channelData)
	}
	if got := replyMeta["approvalId"]; got != "approval_full_123" {
		t.Fatalf("approvalId=%v want=approval_full_123", got)
	}
	grixData, ok := channelData["grix"].(map[string]any)
	if !ok {
		t.Fatalf("expected grix namespace, got %#v", channelData)
	}
	execApproval, ok := grixData["execApproval"].(map[string]any)
	if !ok {
		t.Fatalf("expected grix execApproval payload, got %#v", grixData)
	}
	if got := execApproval["command"]; got != "npm run deploy\necho done" {
		t.Fatalf("command=%v want multiline command", got)
	}
	if got := execApproval["host"]; got != "gateway" {
		t.Fatalf("host=%v want=gateway", got)
	}
}

func TestAdapter_NormalizeInbound_ParsesPlainTextExecApprovalResolvedFallback(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	raw, err := json.Marshal(map[string]any{
		"session_id": "session-plain-status",
		"content":    "✅ Exec approval allowed always. Resolved by grix:user-1. ID: approval_full_123",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	event, err := a.NormalizeInbound(ctx, raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/exec_status") {
		t.Fatalf("content=%q should contain exec status card uri", event.Content)
	}

	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, ok := extra["channel_data"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized channel_data in extra, got %#v", extra)
	}
	grixData, ok := channelData["grix"].(map[string]any)
	if !ok {
		t.Fatalf("expected grix namespace, got %#v", channelData)
	}
	execStatus, ok := grixData["execStatus"].(map[string]any)
	if !ok {
		t.Fatalf("expected grix execStatus payload, got %#v", grixData)
	}
	if got := execStatus["status"]; got != "resolved-allow-always" {
		t.Fatalf("status=%v want=resolved-allow-always", got)
	}
	if got := execStatus["resolved_by_id"]; got != "grix:user-1" {
		t.Fatalf("resolved_by_id=%v want=grix:user-1", got)
	}
}

func TestAdapter_NormalizeInbound_ParsesRawOpenClawExecApprovalResolved(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	raw := []byte(`{
		"session_id":"session-openclaw-resolved",
		"content":"Exec approval allowed once.",
		"extra":{
			"channel_data":{
				"openclaw":{
					"execApprovalResolved":{
						"id":"approval_full_123",
						"decision":"allow-once",
						"resolvedBy":"grix:user-1",
						"ts":1700000000500,
						"request":{
							"command":"npm run deploy",
							"nodeId":"node-9",
							"host":"gateway"
						}
					}
				}
			}
		}
	}`)

	event, err := a.NormalizeInbound(ctx, raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/exec_status") {
		t.Fatalf("content=%q should contain exec status card uri", event.Content)
	}

	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, ok := extra["channel_data"].(map[string]any)
	if !ok {
		t.Fatalf("expected channel_data in extra, got %#v", extra)
	}
	if _, ok := channelData["openclaw"].(map[string]any); !ok {
		t.Fatalf("expected openclaw namespace to be preserved, got %#v", channelData)
	}
	grixData, ok := channelData["grix"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized grix namespace, got %#v", channelData)
	}
	execStatus, ok := grixData["execStatus"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized grix execStatus payload, got %#v", grixData)
	}
	if got := execStatus["status"]; got != "resolved-allow-once" {
		t.Fatalf("status=%v want=resolved-allow-once", got)
	}
	if got := execStatus["resolved_by_id"]; got != "grix:user-1" {
		t.Fatalf("resolved_by_id=%v want=grix:user-1", got)
	}
}

func TestAdapter_NormalizeInbound_ParsesPlainTextExecApprovalExpiredFallback(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	raw, err := json.Marshal(map[string]any{
		"session_id": "session-plain-expired",
		"content":    "⏱️ Exec approval expired. ID: approval_full_123",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	event, err := a.NormalizeInbound(ctx, raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/exec_status") {
		t.Fatalf("content=%q should contain exec status card uri", event.Content)
	}

	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, ok := extra["channel_data"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized channel_data in extra, got %#v", extra)
	}
	grixData, ok := channelData["grix"].(map[string]any)
	if !ok {
		t.Fatalf("expected grix namespace, got %#v", channelData)
	}
	execStatus, ok := grixData["execStatus"].(map[string]any)
	if !ok {
		t.Fatalf("expected grix execStatus payload, got %#v", grixData)
	}
	if got := execStatus["status"]; got != "approval-expired" {
		t.Fatalf("status=%v want=approval-expired", got)
	}
}

func TestAdapter_NormalizeInbound_NormalizesStructuredAgentQuestionCard(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	raw := []byte(`{
		"session_id":"session-agent-question",
		"content":"请确认部署环境",
		"extra":{
			"biz_card":{
				"version":1,
				"type":"agent_question",
				"payload":{
					"request_id":"question_env_1",
					"questions":[
						{
							"index":1,
							"header":"Environment",
							"prompt":"Choose the deployment target.",
							"options":["prod","staging"]
						}
					]
				}
			}
		}
	}`)

	event, err := a.NormalizeInbound(ctx, raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/agent_question") {
		t.Fatalf("content=%q should contain agent question card uri", event.Content)
	}

	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	if _, ok := extra["biz_card"].(map[string]any); !ok {
		t.Fatalf("expected biz_card in extra, got %#v", extra)
	}
}

func TestAdapter_NormalizeRevoke_AddsSystemEventHint(t *testing.T) {
	a := NewAdapter()

	packet, err := a.NormalizeRevoke(context.Background(), agentadapter.DomainRevokeEvent{
		EventID:     "9001:event_revoke:u_1001_u_2001:18889990099",
		SessionID:   "u_1001_u_2001",
		ThreadID:    "topic-openclaw",
		SessionType: 1,
		MsgID:       18889990099,
		SenderID:    9001,
		IsRevoked:   true,
	})
	if err != nil {
		t.Fatalf("NormalizeRevoke error: %v", err)
	}
	if packet == nil {
		t.Fatal("NormalizeRevoke returned nil packet")
	}
	if packet.Cmd != "event_revoke" {
		t.Fatalf("cmd=%s want=event_revoke", packet.Cmd)
	}

	var payload protocol.AgentRevokeEventPayload
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal revoke payload: %v", err)
	}
	if payload.EventID != "9001:event_revoke:u_1001_u_2001:18889990099" {
		t.Fatalf("event_id=%s want original event id", payload.EventID)
	}
	if payload.ThreadID != "topic-openclaw" {
		t.Fatalf("thread_id=%q want=topic-openclaw", payload.ThreadID)
	}
	if payload.SystemEvent == nil {
		t.Fatal("expected system_event hint")
	}
	if payload.SystemEvent.Text != "Grix direct message deleted [session_id=u_1001_u_2001 msg_id=18889990099 sender_id=9001]" {
		t.Fatalf("system_event.text=%q", payload.SystemEvent.Text)
	}
	if payload.SystemEvent.ContextKey != "grix:revoke:u_1001_u_2001:18889990099" {
		t.Fatalf("system_event.context_key=%q", payload.SystemEvent.ContextKey)
	}
}

func TestAdapter_NormalizeOutbound(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	event := agentadapter.DomainOutboundEvent{
		EventType: "delegate_reply",
		SessionID: "session-1",
		Content:   "response text",
	}

	pkt, err := a.NormalizeOutbound(ctx, event)
	if err != nil {
		t.Fatalf("NormalizeOutbound error: %v", err)
	}
	if pkt.Cmd != "event_msg" {
		t.Errorf("Cmd = %q, want event_msg", pkt.Cmd)
	}
}

func TestAdapter_FamilyAndID(t *testing.T) {
	a := NewAdapter()
	if a.Family() != "openclaw" {
		t.Errorf("Family() = %q, want openclaw", a.Family())
	}
	if a.AdapterID() != "openclaw/base" {
		t.Errorf("AdapterID() = %q, want openclaw/base", a.AdapterID())
	}
}

func TestAdapter_OptionalCapabilities(t *testing.T) {
	a := NewAdapter()
	got := a.OptionalCapabilities()
	want := []string{
		"stream_chunk",
		"session_route",
		"local_action_v1",
		"agent_invoke",
		"inbound_media_v1",
		"reaction_v1",
		"thread_v1",
		"tailnet_file_v1",
	}
	if len(got) != len(want) {
		t.Fatalf("OptionalCapabilities()=%v want=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("OptionalCapabilities()[%d]=%q want=%q", index, got[index], want[index])
		}
	}
}

func TestAdapter_NormalizeOutbound_NormalizesStructuredExecStatusCard(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	packet, err := a.NormalizeOutbound(ctx, agentadapter.DomainOutboundEvent{
		EventType: "delegate_reply",
		SessionID: "session-1",
		Content:   "状态更新",
		ChannelData: json.RawMessage(`{
			"grix":{
				"execStatus":{
					"status":"running",
					"summary":"Command is running"
				}
			}
		}`),
	})
	if err != nil {
		t.Fatalf("NormalizeOutbound error: %v", err)
	}
	if packet == nil {
		t.Fatal("NormalizeOutbound returned nil packet")
	}
	if packet.Cmd != "event_msg" {
		t.Fatalf("Cmd=%q want=event_msg", packet.Cmd)
	}

	var payload map[string]any
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal outbound payload: %v", err)
	}
	content, _ := payload["content"].(string)
	if !strings.Contains(content, "grix://card/exec_status") {
		t.Fatalf("content=%q should contain exec status card uri", content)
	}
	if _, ok := payload["channel_data"].(map[string]any); !ok {
		t.Fatalf("expected channel_data to be preserved, got %#v", payload["channel_data"])
	}
}

func TestAdapter_NormalizeOutbound_NormalizesConversationBizCard(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	packet, err := a.NormalizeOutbound(ctx, agentadapter.DomainOutboundEvent{
		EventType: "delegate_reply",
		SessionID: "session-2",
		Content:   "打开会话",
		BizCard: json.RawMessage(`{
			"type":"conversation",
			"payload":{
				"session_id":"g_2001",
				"session_type":"group",
				"title":"研发群"
			}
		}`),
	})
	if err != nil {
		t.Fatalf("NormalizeOutbound error: %v", err)
	}
	if packet == nil {
		t.Fatal("NormalizeOutbound returned nil packet")
	}

	var payload map[string]any
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal outbound payload: %v", err)
	}
	content, _ := payload["content"].(string)
	if !strings.Contains(content, "grix://card/conversation") {
		t.Fatalf("content=%q should contain conversation card uri", content)
	}
	if _, ok := payload["biz_card"].(map[string]any); !ok {
		t.Fatalf("expected biz_card to be preserved, got %#v", payload["biz_card"])
	}
}

func TestAdapter_NormalizeInbound_DropsRedundantApprovalText(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	content := "Approval required.\n\nRun:\n\n```txt\n/approve 9c25d898 allow-once\n```\n\nPending command:\n\n```sh\necho \"Hello from OpenClaw!\"\n```\n\nOther options:\n\n```txt\n/approve 9c25d898 deny\n```\n\nThe effective approval policy requires approval every time, so Allow Always is unavailable.\n\nHost: gateway\nCWD: /workspace/openclaw\nExpires in: 30m\nFull id: `9c25d898-431e-4332-a4a5-bb19da9fa555`user wants me to run the echo command. I need to ask the user to approve it.\n\n需要执行 echo 命令，请回复以下命令进行授权：\n\n/approve 9c25d898 allow-once"

	raw, err := json.Marshal(map[string]any{
		"session_id": "session-dup",
		"content":    content,
		"extra": map[string]any{
			"channel_data": map[string]any{
				"execApproval": map[string]any{
					"allowedDecisions": []string{"allow-once", "deny"},
					"approvalId":       "9c25d898-431e-4332-a4a5-bb19da9fa555",
					"approvalKind":     "exec",
					"approvalSlug":     "9c25d898",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	event, err := a.NormalizeInbound(ctx, raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !event.Drop {
		t.Fatal("expected redundant approval text to be dropped, but Drop=false")
	}
	if event.SessionID != "session-dup" {
		t.Fatalf("session_id=%q want=session-dup", event.SessionID)
	}
}

func TestAdapter_NormalizeInbound_DoesNotDropNonApprovalText(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	raw := []byte(`{
		"session_id":"session-normal",
		"content":"这是一条普通的聊天消息，不需要审批。"
	}`)

	event, err := a.NormalizeInbound(ctx, raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if event.Drop {
		t.Fatal("normal text should not be dropped")
	}
	if event.Content != "这是一条普通的聊天消息，不需要审批。" {
		t.Fatalf("content=%q want original text", event.Content)
	}
}

func TestAdapter_NormalizeInbound_DoesNotDropApprovalTextWithoutChannelData(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	raw, err := json.Marshal(map[string]any{
		"session_id": "session-no-cd",
		"content":    "Approval required.\n\nRun:\n\n```txt\n/approve abc12345 allow-once\n```",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	event, err := a.NormalizeInbound(ctx, raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if event.Drop {
		t.Fatal("approval text without channel_data should not be dropped")
	}
}
