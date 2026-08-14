package agentcards

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestNormalize_AgentQuestionBizCard(t *testing.T) {
	content, _, ok := Normalize(&agentadapter.InboundSendMsgPayload{
		Content: "需要确认环境",
		BizCard: json.RawMessage(`{
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
		}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize agent question biz_card")
	}
	if !strings.Contains(content, "grix://card/agent_question") {
		t.Fatalf("content=%q should contain agent question card uri", content)
	}
}

func TestNormalize_AgentOpenSessionBizCard(t *testing.T) {
	content, _, ok := Normalize(&agentadapter.InboundSendMsgPayload{
		Content: "请选择工作目录",
		BizCard: json.RawMessage(`{
			"version":1,
			"type":"agent_open_session",
			"payload":{
				"summary_text":"open 缺少目录路径。",
				"detail_text":"请输入工作目录。"
			}
		}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize agent open session biz_card")
	}
	if !strings.Contains(content, "grix://card/agent_open_session") {
		t.Fatalf("content=%q should contain agent open session card uri", content)
	}
}

func TestNormalize_RejectsLegacyCardAlias(t *testing.T) {
	if _, _, ok := Normalize(&agentadapter.InboundSendMsgPayload{
		Content: "请选择工作目录",
		BizCard: json.RawMessage(`{
			"version":1,
			"type":"claude_open_session",
			"payload":{
				"summary_text":"open 缺少目录路径。"
			}
		}`),
	}); ok {
		t.Fatal("Normalize should reject legacy card aliases")
	}
}

func TestNormalize_AgentStatusBizCard(t *testing.T) {
	content, _, ok := Normalize(&agentadapter.InboundSendMsgPayload{
		Content: "状态更新",
		BizCard: json.RawMessage(`{
			"version":1,
			"type":"agent_status",
			"payload":{
				"category":"access",
				"status":"warning",
				"summary":"Pairing is pending.",
				"reference_id":"pair_123"
			}
		}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize agent status biz_card")
	}
	if !strings.Contains(content, "grix://card/agent_status") {
		t.Fatalf("content=%q should contain agent status card uri", content)
	}
}
