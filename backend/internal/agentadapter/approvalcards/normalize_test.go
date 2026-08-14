package approvalcards

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestNormalize_StructuredExecApproval(t *testing.T) {
	content, _, ok := Normalize(&agentadapter.InboundSendMsgPayload{
		Content: "审批文本",
		ChannelData: json.RawMessage(`{
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
						"cwd":"/tmp/demo"
					}
				}
			}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize structured exec approval")
	}
	if !strings.Contains(content, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec approval card uri", content)
	}
}

func TestNormalize_PlainTextExecApproval(t *testing.T) {
	content, extra, ok := Normalize(&agentadapter.InboundSendMsgPayload{
		Content: strings.Join([]string{
			"🔒 Exec approval required",
			"ID: approval_full_123",
			"Command: `npm run deploy`",
			"CWD: /srv/app",
			"Host: gateway",
			"Expires in: 120s",
			"Reply with: /approve <id> allow-once|allow-always|deny",
		}, "\n"),
	})
	if !ok {
		t.Fatal("Normalize should recognize plain text exec approval")
	}
	if !strings.Contains(content, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec approval card uri", content)
	}
	var decoded map[string]any
	if err := json.Unmarshal(extra, &decoded); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, ok := decoded["channel_data"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized channel_data, got %#v", decoded)
	}
	if _, ok := channelData["execApproval"].(map[string]any); !ok {
		t.Fatalf("expected execApproval metadata, got %#v", channelData)
	}
}

func TestNormalize_ExecApprovalBizCard(t *testing.T) {
	content, _, ok := NormalizeBizCard(&agentadapter.InboundSendMsgPayload{
		Content: "审批文本",
		BizCard: json.RawMessage(`{
				"version":1,
				"type":"exec_approval",
				"payload":{
					"approval_id":"req_123",
					"approval_slug":"req_123",
					"approval_command_id":"req_123",
					"command":"npm run deploy",
					"host":"gateway",
					"allowed_decisions":["allow-once","deny"]
				}
			}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize exec approval biz_card")
	}
	if !strings.Contains(content, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec approval card uri", content)
	}
}

func TestNormalizeBizCard_RejectsLegacyChannelData(t *testing.T) {
	if _, _, ok := NormalizeBizCard(&agentadapter.InboundSendMsgPayload{
		Content: "审批文本",
		ChannelData: json.RawMessage(`{
				"execApproval":{"approvalId":"74569573"}
			}`),
	}); ok {
		t.Fatal("NormalizeBizCard should reject legacy channel_data")
	}
}

func TestNormalize_PlainTextExecStatus(t *testing.T) {
	content, extra, ok := Normalize(&agentadapter.InboundSendMsgPayload{
		Content: "✅ Exec approval allowed always. Resolved by grix:user-1. ID: approval_full_123",
	})
	if !ok {
		t.Fatal("Normalize should recognize plain text exec status")
	}
	if !strings.Contains(content, "grix://card/exec_status") {
		t.Fatalf("content=%q should contain exec status card uri", content)
	}
	var decoded map[string]any
	if err := json.Unmarshal(extra, &decoded); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, ok := decoded["channel_data"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized channel_data, got %#v", decoded)
	}
	grixData, ok := channelData["grix"].(map[string]any)
	if !ok {
		t.Fatalf("expected grix namespace, got %#v", channelData)
	}
	statusData, ok := grixData["execStatus"].(map[string]any)
	if !ok {
		t.Fatalf("expected execStatus payload, got %#v", grixData)
	}
	if got := statusData["status"]; got != "resolved-allow-always" {
		t.Fatalf("status=%v want=resolved-allow-always", got)
	}
}

func TestNormalize_ExecStatusBizCard(t *testing.T) {
	content, _, ok := NormalizeBizCard(&agentadapter.InboundSendMsgPayload{
		Content: "审批状态",
		BizCard: json.RawMessage(`{
				"version":1,
				"type":"exec_status",
				"payload":{
					"status":"resolved-deny",
					"summary":"Exec approval denied.",
					"approval_id":"req_123"
				}
			}`),
	})
	if !ok {
		t.Fatal("Normalize should recognize exec status biz_card")
	}
	if !strings.Contains(content, "grix://card/exec_status") {
		t.Fatalf("content=%q should contain exec status card uri", content)
	}
}

func TestNormalizeDangerousCommandText(t *testing.T) {
	input := "⚠️ **Dangerous command requires approval:**\n```\nHOME=/Users/example bash -lc 'INSTALL_ID=\"egg-smoke-20260503\"'\n```\nReason: shell command via -c/-lc flag\n\nReply `/approve` to execute, `/approve session` to approve this pattern for the session, `/approve always` to approve permanently, or `/deny` to cancel."

	cardContent, channelData, ok := NormalizeDangerousCommandText(input)
	if !ok {
		t.Fatal("NormalizeDangerousCommandText should match")
	}
	if !strings.Contains(cardContent, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec_approval card uri", cardContent)
	}
	if channelData == nil {
		t.Fatal("channelData should not be nil")
	}
	execApproval, _ := channelData["execApproval"].(map[string]any)
	if execApproval == nil {
		t.Fatal("channelData should contain execApproval")
	}
	if got := execApproval["approvalId"].(string); !strings.HasPrefix(got, "hd_") {
		t.Fatalf("approvalId=%q should start with hd_", got)
	}
	grix, _ := channelData["grix"].(map[string]any)
	exec, _ := grix["execApproval"].(map[string]any)
	if got := exec["command"].(string); got != `HOME=/Users/example bash -lc 'INSTALL_ID="egg-smoke-20260503"'` {
		t.Fatalf("command=%q", got)
	}
	if got := exec["host"].(string); got != "hermes" {
		t.Fatalf("host=%q want=hermes", got)
	}
	if got := exec["warning_text"].(string); got != "shell command via -c/-lc flag" {
		t.Fatalf("warning_text=%q", got)
	}
}

func TestNormalizeDangerousCommandText_UniqueID(t *testing.T) {
	input := "⚠️ **Dangerous command requires approval:**\n```\necho hello\n```\nReason: test"
	_, cd1, _ := NormalizeDangerousCommandText(input)
	_, cd2, _ := NormalizeDangerousCommandText(input)
	id1 := cd1["execApproval"].(map[string]any)["approvalId"].(string)
	id2 := cd2["execApproval"].(map[string]any)["approvalId"].(string)
	if id1 == id2 {
		t.Fatalf("same input should produce unique IDs, got identical: %q", id1)
	}
	if !strings.HasPrefix(id1, "hd_") || !strings.HasPrefix(id2, "hd_") {
		t.Fatalf("IDs should start with hd_: %q %q", id1, id2)
	}
}

func TestNormalizeDangerousCommandText_RejectsNonMatching(t *testing.T) {
	_, _, ok := NormalizeDangerousCommandText("just some random text")
	if ok {
		t.Fatal("should not match random text")
	}
}

func TestNormalizeDangerousCommandText_RejectsMissingCodeBlock(t *testing.T) {
	input := "⚠️ **Dangerous command requires approval:**\nno code block here\nReason: test"
	_, _, ok := NormalizeDangerousCommandText(input)
	if ok {
		t.Fatal("should not match without code block")
	}
}

func TestNormalizeDangerousCommandText_NoReason(t *testing.T) {
	input := "⚠️ **Dangerous command requires approval:**\n```\necho hello\n```"
	cardContent, channelData, ok := NormalizeDangerousCommandText(input)
	if !ok {
		t.Fatal("should match even without Reason line")
	}
	if !strings.Contains(cardContent, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec_approval card uri", cardContent)
	}
	grix := channelData["grix"].(map[string]any)
	exec := grix["execApproval"].(map[string]any)
	if _, has := exec["warning_text"]; has {
		t.Fatal("should not have warning_text without Reason")
	}
}


func TestSanitizeMarkdownLinkText(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{input: `echo "=== $dir ==="`, expected: `echo "=== dir ==="`},
		{input: "ls $HOME", expected: "ls HOME"},
		{input: "no special chars", expected: "no special chars"},
		{input: "$1 $2 $3", expected: "1 2 3"},
		{input: "", expected: ""},
	}
	for _, tc := range tests {
		got := sanitizeMarkdownLinkText(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeMarkdownLinkText(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestBuildGrixCardLink_StripsDollarFromFallback(t *testing.T) {
	payload := map[string]any{
		"approval_id":         "test_id",
		"approval_slug":       "test_slug",
		"approval_command_id": "test_cmd",
		"command":             `echo "$HOME"`,
		"host":                "hermes",
		"allowed_decisions":   []string{"allow-once", "deny"},
	}
	link := buildGrixCardLink(
		`[Exec Approval] echo "$HOME" (hermes)`+"\n"+"/approve test_cmd allow-once",
		"exec_approval",
		payload,
	)
	if strings.Contains(link, "$") {
		t.Errorf("card link should not contain $, got: %s", link)
	}
	if !strings.Contains(link, "grix://card/exec_approval") {
		t.Errorf("card link should contain grix://card URI, got: %s", link)
	}
}
