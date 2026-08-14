package agentapi

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestOwnerVisibleToForAdapterCard(t *testing.T) {
	testCases := []struct {
		name      string
		adapterID string
		content   string
		extra     json.RawMessage
		ownerID   int64
		want      []int64
	}{
		{
			name:      "claude open session card",
			adapterID: "claude/base",
			content:   "[Open](grix://card/agent_open_session?summary_text=missing)",
			ownerID:   1001,
			want:      []int64{1001},
		},
		{
			name:      "gemini approval card",
			adapterID: "gemini/base",
			content:   "[Approval](grix://card/exec_approval?approval_id=req_1)",
			ownerID:   1002,
			want:      []int64{1002},
		},
		{
			name:      "agy open session card",
			adapterID: "agy/base",
			content:   "[Open](grix://card/agent_open_session?summary_text=missing)",
			ownerID:   1003,
			want:      []int64{1003},
		},
		{
			name:      "kimi open session card",
			adapterID: "kimi/base",
			content:   "[Open](grix://card/agent_open_session?summary_text=missing)",
			ownerID:   1004,
			want:      []int64{1004},
		},
		{
			name:      "openhuman open session card",
			adapterID: "openhuman/base",
			content:   "[Open](grix://card/agent_open_session?summary_text=missing)",
			ownerID:   1005,
			want:      []int64{1005},
		},
		{
			name:      "deepseek open session card",
			adapterID: "deepseek/jsonrpc-v1",
			content:   "[Open](grix://card/agent_open_session?summary_text=missing)",
			ownerID:   1010,
			want:      []int64{1010},
		},
		{
			name:      "deepseek channel_data sessionBinding fallback",
			adapterID: "deepseek/jsonrpc-v1",
			content:   "Session binding missing.",
			extra: json.RawMessage(`{
				"channel_data": {
					"deepseek": {
						"sessionBinding": {
							"status": "missing",
							"reason": "binding_missing",
							"error_code": "session_binding_missing"
						}
					}
				}
			}`),
			ownerID: 1011,
			want:    []int64{1011},
		},
		{
			name:      "family-only adapter id works",
			adapterID: "gemini",
			content:   "[Approval](grix://card/exec_approval?approval_id=req_1)",
			ownerID:   1002,
			want:      []int64{1002},
		},
		{
			name:      "codex approval status card",
			adapterID: "codex/base",
			content:   "[Status](grix://card/exec_status?status=resolved-allow-once)",
			ownerID:   1003,
			want:      []int64{1003},
		},
		{
			name:      "qwen open session card",
			adapterID: "qwen/base",
			content:   "[Open](grix://card/agent_open_session?summary_text=missing)",
			ownerID:   1004,
			want:      []int64{1004},
		},
		{
			name:      "openclaw approval card",
			adapterID: "openclaw/base",
			content:   "[Approval](grix://card/exec_approval?approval_id=req_oc1)",
			ownerID:   1005,
			want:      []int64{1005},
		},
		{
			name:      "hermes status card",
			adapterID: "hermes/base",
			content:   "[Status](grix://card/exec_status?status=resolved-allow-once)",
			ownerID:   1006,
			want:      []int64{1006},
		},
		{
			name:      "openclaw family-only adapter id works",
			adapterID: "openclaw",
			content:   "[Approval](grix://card/exec_approval?approval_id=req_oc2)",
			ownerID:   1007,
			want:      []int64{1007},
		},
		{
			name:      "hermes family-only adapter id works",
			adapterID: "hermes",
			content:   "[Status](grix://card/exec_status?status=resolved-deny)",
			ownerID:   1008,
			want:      []int64{1008},
		},
		{
			name:      "non target adapter ignored",
			adapterID: "unknown/base",
			content:   "[Open](grix://card/agent_open_session?summary_text=missing)",
			ownerID:   1009,
			want:      nil,
		},
		{
			name:      "extra biz_card fallback",
			adapterID: "codex/base",
			content:   "plain text",
			extra: json.RawMessage(`{
				"biz_card": {
					"type": "exec_approval",
					"payload": {"approval_id":"req_1"}
				}
			}`),
			ownerID: 1006,
			want:    []int64{1006},
		},
		{
			name:      "extra channel_data sessionBinding fallback",
			adapterID: "qwen/base",
			content:   "plain text",
			extra: json.RawMessage(`{
				"channel_data": {
					"qwen": {
						"sessionBinding": {
							"status": "missing",
							"reason": "binding_missing"
						}
					}
				}
			}`),
			ownerID: 1007,
			want:    []int64{1007},
		},
		{
			name:      "non target card ignored",
			adapterID: "claude/base",
			content:   "[Question](grix://card/agent_question?request_id=req_2)",
			ownerID:   1008,
			want:      nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ownerVisibleToForAdapterCard(tc.adapterID, tc.content, tc.extra, tc.ownerID)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ownerVisibleToForAdapterCard()=%v want=%v", got, tc.want)
			}
		})
	}
}
