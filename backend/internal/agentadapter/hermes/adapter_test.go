package hermes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapter_Supports(t *testing.T) {
	a := NewAdapter()

	tests := []struct {
		name string
		meta agentadapter.AgentClientMeta
		want bool
	}{
		{
			name: "hermes client type",
			meta: agentadapter.AgentClientMeta{ClientType: Family},
			want: true,
		},
		{
			name: "hermes host type",
			meta: agentadapter.AgentClientMeta{HostType: Family},
			want: true,
		},
		{
			name: "other family",
			meta: agentadapter.AgentClientMeta{ClientType: "claude"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.Supports(tt.meta); got != tt.want {
				t.Fatalf("Supports(%+v)=%v want=%v", tt.meta, got, tt.want)
			}
		})
	}
}

func TestAdapter_OptionalCapabilities_MatchHermesProtocol(t *testing.T) {
	a := NewAdapter()
	got := a.OptionalCapabilities()
	want := []string{"stream_chunk", "session_route", "thread_v1", "inbound_media_v1", "local_action_v1", "audit_replay_v2"}
	if len(got) != len(want) {
		t.Fatalf("OptionalCapabilities()=%v want=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("OptionalCapabilities()[%d]=%q want=%q", index, got[index], want[index])
		}
	}
}

func TestAdapter_RequiredCapabilities_MatchHermesProtocol(t *testing.T) {
	a := NewAdapter()
	got := a.RequiredCapabilities()
	want := []string{"local_action_v1"}
	if len(got) != len(want) {
		t.Fatalf("RequiredCapabilities()=%v want=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("RequiredCapabilities()[%d]=%q want=%q", index, got[index], want[index])
		}
	}
}

func TestAdapter_NormalizeInbound_PreservesContentAndExtra(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_1001",
		"thread_id":"topic-a",
		"content":"hello hermes",
		"extra":{"channel_data":{"grix":{"debug":true}}}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if event.SessionID != "g_1001" {
		t.Fatalf("SessionID=%q want=g_1001", event.SessionID)
	}
	if event.ThreadID != "topic-a" {
		t.Fatalf("ThreadID=%q want=topic-a", event.ThreadID)
	}
	if event.Content != "hello hermes" {
		t.Fatalf("Content=%q want=hello hermes", event.Content)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	if _, ok := extra["channel_data"].(map[string]any); !ok {
		t.Fatalf("expected channel_data in extra, got %#v", extra)
	}
}

func TestAdapter_NormalizeInbound_NormalizesStructuredExecApprovalCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_2001",
		"content":"审批文本",
		"extra":{
			"biz_card":{
				"version":1,
				"type":"exec_approval",
				"payload":{
					"approval_id":"74569573",
					"approval_slug":"74569573",
					"approval_command_id":"74569573",
					"command":"echo hi",
					"host":"gateway",
					"cwd":"/tmp/demo",
					"allowed_decisions":["allow-once","allow-always","deny"]
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/exec_approval") {
		t.Fatalf("Content=%q should contain exec approval card uri", event.Content)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	if _, ok := extra["biz_card"].(map[string]any); !ok {
		t.Fatalf("expected biz_card in extra, got %#v", extra)
	}
}

func TestAdapter_NormalizeInbound_ParsesRawHermesExecApprovalPending(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_2001_raw",
		"content":"[Exec Approval] rm -rf /tmp/demo (hermes)\n/approve req_123 allow-once\nReason: dangerous deletion",
		"channel_data":{
			"hermes":{
				"execApprovalPending":{
					"approval_id":"req_123",
					"pattern_key":"dangerous deletion",
					"pattern_keys":["dangerous deletion","filesystem mutation"],
					"command":"rm -rf /tmp/demo",
					"description":"dangerous deletion",
					"host":"hermes",
					"expires_in_seconds":300,
					"allowed_decisions":["allow-once","allow-always","deny"],
					"decision_commands":{
						"allow-once":"/approve req_123 allow-once",
						"allow-always":"/approve req_123 allow-always",
						"deny":"/approve req_123 deny"
					}
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/exec_approval") {
		t.Fatalf("Content=%q should contain exec approval card uri", event.Content)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, _ := extra["channel_data"].(map[string]any)
	if channelData == nil {
		t.Fatalf("expected channel_data in extra, got %#v", extra)
	}
	hermesData, _ := channelData["hermes"].(map[string]any)
	if hermesData == nil {
		t.Fatalf("expected hermes raw channel data, got %#v", channelData)
	}
	if _, ok := hermesData["execApprovalPending"].(map[string]any); !ok {
		t.Fatalf("expected raw execApprovalPending to be preserved, got %#v", hermesData)
	}
	replyMeta, _ := channelData["execApproval"].(map[string]any)
	if replyMeta == nil {
		t.Fatalf("expected synthesized execApproval metadata, got %#v", channelData)
	}
	if got := replyMeta["approvalId"]; got != "req_123" {
		t.Fatalf("approvalId=%v want=req_123", got)
	}
	grixData, _ := channelData["grix"].(map[string]any)
	if grixData == nil {
		t.Fatalf("expected synthesized grix payload, got %#v", channelData)
	}
	execApproval, _ := grixData["execApproval"].(map[string]any)
	if execApproval == nil {
		t.Fatalf("expected synthesized execApproval payload, got %#v", grixData)
	}
	if got := execApproval["command"]; got != "rm -rf /tmp/demo" {
		t.Fatalf("command=%v want raw command", got)
	}
	if got := execApproval["host"]; got != "hermes" {
		t.Fatalf("host=%v want hermes", got)
	}
}

func TestAdapter_NormalizeInbound_RejectsLegacyStructuredExecApprovalCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_2002",
		"content":"审批文本",
		"extra":{
			"channel_data":{
				"execApproval":{"approvalId":"approval_full_123"},
				"grix":{
					"execApproval":{
						"approval_command_id":"approval_full_123",
						"command":"npm run deploy",
						"host":"gateway"
					}
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if event.Content != "审批文本" {
		t.Fatalf("Content=%q want 原始内容", event.Content)
	}
}

func TestAdapter_NormalizeInbound_NormalizesStructuredEggInstallCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_3001",
		"content":"安装状态",
		"extra":{
			"biz_card":{
				"version":1,
				"type":"egg_install_status",
				"payload":{
					"install_id":"inst_123",
					"status":"running",
					"step":"agent_created",
					"summary":"Installation in progress"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/egg_install_status") {
		t.Fatalf("Content=%q should contain egg install card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_NormalizesStructuredUserProfileCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_3002",
		"content":"资料卡",
		"extra":{
			"biz_card":{
				"version":1,
				"type":"user_profile",
				"payload":{
					"user_id":"2001",
					"peer_type":2,
					"nickname":"Hermes Agent",
					"avatar_url":"https://cdn.example.com/avatar.png"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/user_profile") {
		t.Fatalf("Content=%q should contain user profile card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_NormalizesTopLevelConversationBizCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_3006",
		"content":"会话入口",
		"biz_card":{
			"version":1,
			"type":"conversation",
			"payload":{
				"session_id":"session-100",
				"session_type":"group",
				"title":"研发群"
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/conversation") {
		t.Fatalf("Content=%q should contain conversation card uri", event.Content)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	if _, ok := extra["biz_card"].(map[string]any); !ok {
		t.Fatalf("expected synthesized biz_card in extra, got %#v", extra)
	}
}

func TestAdapter_NormalizeInbound_NormalizesTopLevelExecApprovalChannelData(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_3007",
		"content":"审批文本",
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
					"host":"gateway"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/exec_approval") {
		t.Fatalf("Content=%q should contain exec approval card uri", event.Content)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	if _, ok := extra["channel_data"].(map[string]any); !ok {
		t.Fatalf("expected synthesized channel_data in extra, got %#v", extra)
	}
}

func TestAdapter_NormalizeInbound_NormalizesStructuredAgentQuestionBizCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_3003",
		"content":"请确认环境",
		"extra":{
			"biz_card":{
				"version":1,
				"type":"agent_question",
				"payload":{
					"request_id":"req_env_1",
					"questions":[
						{
							"index":1,
							"header":"Environment",
							"prompt":"Choose an environment.",
							"options":["production","staging"]
						}
					]
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/agent_question") {
		t.Fatalf("Content=%q should contain agent question card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_NormalizesAgentOpenSessionBizCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_3004",
		"content":"请输入工作目录",
		"extra":{
			"biz_card":{
				"version":1,
				"type":"agent_open_session",
				"payload":{
					"summary_text":"open 缺少目录路径。",
					"detail_text":"请输入工作目录来继续。"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/agent_open_session") {
		t.Fatalf("Content=%q should contain agent open session card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_RejectsLegacyCardAlias(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_3005",
		"content":"请输入工作目录",
		"extra":{
			"biz_card":{
				"version":1,
				"type":"claude_open_session",
				"payload":{
					"summary_text":"open 缺少目录路径。"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if event.Content != "请输入工作目录" {
		t.Fatalf("Content=%q want 原始内容", event.Content)
	}
}

func TestAdapter_NormalizeOutbound_UsesEventMsg(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-1",
		EventType: "group_message",
		SessionID: "g_1001",
		ThreadID:  "topic-a",
		Content:   "hello group",
		Attachments: []agentadapter.AttachmentPayload{
			{
				AttachmentType: "image",
				MediaURL:       "https://cdn.example.com/demo.png",
				ContentType:    "image/png",
			},
		},
		BizCard:     json.RawMessage(`{"type":"exec_status"}`),
		ChannelData: json.RawMessage(`{"grix":{"execStatus":{"status":"running"}}}`),
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
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := payload["session_id"]; got != "g_1001" {
		t.Fatalf("session_id=%v want=g_1001", got)
	}
	if got := payload["content"]; got != "hello group" {
		t.Fatalf("content=%v want=hello group", got)
	}
	if got := payload["thread_id"]; got != "topic-a" {
		t.Fatalf("thread_id=%v want=topic-a", got)
	}
	if _, ok := payload["attachments"].([]any); !ok {
		t.Fatalf("expected attachments in payload, got %#v", payload)
	}
}

func TestAdapter_NormalizeRevoke_UsesEventRevoke(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeRevoke(context.Background(), agentadapter.DomainRevokeEvent{
		EventID:     "evt-revoke-1",
		SessionID:   "g_1001",
		ThreadID:    "topic-hermes",
		SessionType: 2,
		MsgID:       9001,
		SenderID:    2001,
		IsRevoked:   true,
	})
	if err != nil {
		t.Fatalf("NormalizeRevoke error: %v", err)
	}
	if packet == nil {
		t.Fatal("NormalizeRevoke returned nil packet")
	}
	if packet.Cmd != "event_revoke" {
		t.Fatalf("Cmd=%q want=event_revoke", packet.Cmd)
	}
	var payload map[string]any
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal revoke payload: %v", err)
	}
	if got := payload["session_id"]; got != "g_1001" {
		t.Fatalf("session_id=%v want=g_1001", got)
	}
	if got := payload["thread_id"]; got != "topic-hermes" {
		t.Fatalf("thread_id=%v want=topic-hermes", got)
	}
	if got := payload["is_revoked"]; got != true {
		t.Fatalf("is_revoked=%v want=true", got)
	}
}

func TestAdapter_NormalizeInbound_DangerousCommandApprovalFallback(t *testing.T) {
	a := NewAdapter()
	input := map[string]any{
		"session_id": "s_100",
		"content":    "⚠️ **Dangerous command requires approval:**\n```\nHOME=/test bash -lc 'echo hello'\n```\nReason: shell command via -c/-lc flag\n\nReply `/approve` to execute.",
	}
	raw, _ := json.Marshal(input)

	event, err := a.NormalizeInbound(context.Background(), raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if !strings.Contains(event.Content, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec_approval card", event.Content)
	}
	if event.SessionID != "s_100" {
		t.Fatalf("session_id=%q want=s_100", event.SessionID)
	}

	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	cd, _ := extra["channel_data"].(map[string]any)
	if cd == nil {
		t.Fatal("extra should contain channel_data")
	}
	grix, _ := cd["grix"].(map[string]any)
	exec, _ := grix["execApproval"].(map[string]any)
	if got := exec["command"]; got != "HOME=/test bash -lc 'echo hello'" {
		t.Fatalf("command=%v", got)
	}
}
