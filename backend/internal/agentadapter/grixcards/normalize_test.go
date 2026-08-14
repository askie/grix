package grixcards

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestNormalize_EggInstallStatus(t *testing.T) {
	content, _, ok := Normalize(&agentadapter.InboundSendMsgPayload{
		Content: "安装状态",
		ChannelData: json.RawMessage(`{
			"grix":{
				"eggInstall":{
					"install_id":"inst_123",
					"status":"running",
					"step":"agent_created",
					"summary":"Installation in progress"
				}
			}
		}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize egg install status")
	}
	if !strings.Contains(content, "grix://card/egg_install_status") {
		t.Fatalf("content=%q should contain egg install card uri", content)
	}
}

func TestNormalize_UserProfile(t *testing.T) {
	content, _, ok := Normalize(&agentadapter.InboundSendMsgPayload{
		Content: "资料",
		ChannelData: json.RawMessage(`{
			"grix":{
				"userProfile":{
					"user_id":"2001",
					"peer_type":2,
					"nickname":"Hermes Agent",
					"avatar_url":"https://cdn.example.com/avatar.png"
				}
			}
		}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize user profile card")
	}
	if !strings.Contains(content, "grix://card/user_profile") {
		t.Fatalf("content=%q should contain user profile card uri", content)
	}
}

func TestNormalize_UserProfileBizCard(t *testing.T) {
	content, _, ok := NormalizeBizCard(&agentadapter.InboundSendMsgPayload{
		Content: "资料",
		BizCard: json.RawMessage(`{
			"version":1,
			"type":"user_profile",
			"payload":{
				"user_id":"2001",
				"peer_type":2,
				"nickname":"Hermes Agent"
			}
		}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize user profile biz_card")
	}
	if !strings.Contains(content, "grix://card/user_profile") {
		t.Fatalf("content=%q should contain user profile card uri", content)
	}
}

func TestNormalize_Conversation(t *testing.T) {
	content, _, ok := Normalize(&agentadapter.InboundSendMsgPayload{
		Content: "会话入口",
		ChannelData: json.RawMessage(`{
			"grix":{
				"conversation":{
					"session_id":"session-100",
					"session_type":"group",
					"title":"研发群"
				}
			}
		}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize conversation card")
	}
	if !strings.Contains(content, "grix://card/conversation") {
		t.Fatalf("content=%q should contain conversation card uri", content)
	}
}

func TestNormalize_ConversationBizCard(t *testing.T) {
	content, _, ok := NormalizeBizCard(&agentadapter.InboundSendMsgPayload{
		Content: "会话入口",
		BizCard: json.RawMessage(`{
			"version":1,
			"type":"conversation",
			"payload":{
				"session_id":"session-100",
				"session_type":"private",
				"title":"Alice",
				"peer_id":"1001",
				"peer_nickname":"Alice"
			}
		}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize conversation biz_card")
	}
	if !strings.Contains(content, "grix://card/conversation") {
		t.Fatalf("content=%q should contain conversation card uri", content)
	}
}

func TestNormalizeBizCard_RejectsLegacyChannelData(t *testing.T) {
	if _, _, ok := NormalizeBizCard(&agentadapter.InboundSendMsgPayload{
		Content: "资料",
		ChannelData: json.RawMessage(`{
			"grix":{
				"userProfile":{"user_id":"2001","nickname":"Hermes Agent"}
			}
		}`),
	}); ok {
		t.Fatal("NormalizeBizCard should reject legacy channel_data")
	}
}

func TestNormalize_ToolExecution(t *testing.T) {
	content, _, ok := Normalize(&agentadapter.InboundSendMsgPayload{
		Content: "工具摘要",
		ChannelData: json.RawMessage(`{
			"grix":{
				"toolExecution":{
					"summary_text":"Ran terminal command",
					"detail_text":"pwd"
				}
			}
		}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize tool execution card")
	}
	if !strings.Contains(content, "grix://card/tool_execution") {
		t.Fatalf("content=%q should contain tool execution card uri", content)
	}
}

func TestNormalize_EggInstallStatusBizCard(t *testing.T) {
	content, _, ok := NormalizeBizCard(&agentadapter.InboundSendMsgPayload{
		Content: "安装状态",
		BizCard: json.RawMessage(`{
			"version":1,
			"type":"egg_install_status",
			"payload":{
				"install_id":"inst_123",
				"status":"running",
				"summary":"Installation in progress"
			}
		}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize egg install status biz_card")
	}
	if !strings.Contains(content, "grix://card/egg_install_status") {
		t.Fatalf("content=%q should contain egg install card uri", content)
	}
}
