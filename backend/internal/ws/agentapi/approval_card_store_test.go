package agentapi

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractApprovalIDFromCard_DirectParams(t *testing.T) {
	content := "[Exec Approval] test (Gemini ACP)\n/approve test-id allow-once](grix://card/exec_approval?approval_id=test-id&approval_command_id=test-id&command=test&host=gemini)"
	got := extractApprovalIDFromCard(content, nil)
	assert.Equal(t, "test-id", got)
}

func TestExtractApprovalIDFromCard_DParam(t *testing.T) {
	payload := map[string]any{
		"approval_id":         "gemini:bridge-1:dG9vbC1jYWxsLWlk",
		"approval_slug":       "gemini:bridge-1:dG9vbC1jYWxsLWlk",
		"approval_command_id": "gemini:bridge-1:dG9vbC1jYWxsLWlk",
		"command":             "node -e 'console.log(1)'",
		"host":                "Gemini ACP",
		"allowed_decisions":   []string{"allow-once", "allow-always", "deny"},
	}
	d, _ := json.Marshal(payload)
	values := url.Values{}
	values.Set("d", string(d))
	uri := "grix://card/exec_approval?" + values.Encode()
	content := "[Exec Approval] test (Gemini ACP)](" + uri + ")"
	got := extractApprovalIDFromCard(content, nil)
	assert.Equal(t, "gemini:bridge-1:dG9vbC1jYWxsLWlk", got)
}

func TestExtractApprovalIDFromCard_BizCard(t *testing.T) {
	extra, _ := json.Marshal(map[string]any{
		"biz_card": map[string]any{
			"type": "exec_approval",
			"payload": map[string]any{
				"approval_id":         "bizcard-id",
				"approval_command_id": "bizcard-cmd-id",
			},
		},
	})
	got := extractApprovalIDFromCard("[something](grix://card/exec_approval)", extra)
	assert.Equal(t, "bizcard-id", got)
}

func TestExtractApprovalIDFromCard_NotApprovalCard(t *testing.T) {
	got := extractApprovalIDFromCard("hello world", nil)
	assert.Equal(t, "", got)
}

func TestExtractApprovalCardType_ACPChannelData(t *testing.T) {
	for _, execApproval := range []map[string]any{
		{"approval_command_id": "tool-call-1", "approval_type": "permission"},
		{"approval_command_id": "tool-call-1", "host": "acp"},
	} {
		extra, _ := json.Marshal(map[string]any{
			"channel_data": map[string]any{
				"grix": map[string]any{
					"execApproval": execApproval,
				},
			},
		})
		assert.Equal(t, "permission", extractApprovalCardType(extra))
	}
}
