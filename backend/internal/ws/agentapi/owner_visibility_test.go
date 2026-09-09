package agentapi

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/acp"
	"github.com/askie/grix/backend/internal/agentadapter/agy"
	"github.com/askie/grix/backend/internal/agentadapter/claude"
	"github.com/askie/grix/backend/internal/agentadapter/codebuddy"
	"github.com/askie/grix/backend/internal/agentadapter/codewhale"
	"github.com/askie/grix/backend/internal/agentadapter/codex"
	"github.com/askie/grix/backend/internal/agentadapter/copilot"
	"github.com/askie/grix/backend/internal/agentadapter/cursor"
	"github.com/askie/grix/backend/internal/agentadapter/deepseek"
	"github.com/askie/grix/backend/internal/agentadapter/dim"
	"github.com/askie/grix/backend/internal/agentadapter/gemini"
	"github.com/askie/grix/backend/internal/agentadapter/grok"
	"github.com/askie/grix/backend/internal/agentadapter/hermes"
	"github.com/askie/grix/backend/internal/agentadapter/kimi"
	"github.com/askie/grix/backend/internal/agentadapter/kiro"
	"github.com/askie/grix/backend/internal/agentadapter/mcode"
	"github.com/askie/grix/backend/internal/agentadapter/omp"
	"github.com/askie/grix/backend/internal/agentadapter/openclaw"
	"github.com/askie/grix/backend/internal/agentadapter/opencode"
	"github.com/askie/grix/backend/internal/agentadapter/openhuman"
	"github.com/askie/grix/backend/internal/agentadapter/pi"
	"github.com/askie/grix/backend/internal/agentadapter/qodercli"
	"github.com/askie/grix/backend/internal/agentadapter/qoderclicn"
	"github.com/askie/grix/backend/internal/agentadapter/qwen"
	"github.com/askie/grix/backend/internal/agentadapter/qwenpaw"
	"github.com/askie/grix/backend/internal/agentadapter/reasonix"
	"github.com/askie/grix/backend/internal/agentadapter/traecli"
	"github.com/askie/grix/backend/internal/agentadapter/zeroclaw"
)

// TestIsOwnerVisibilityAdapter_CoversEveryRegisteredAdapterFamily is a guard:
// isOwnerVisibilityAdapter used to be a hand-maintained enumeration that fell
// behind ws/server.go's adapter registration list three times in a row
// (round2a's qodercli/qoderclicn/mcode/dim all shipped without it) — an owner
// exec_approval/exec_status/agent_open_session card for an un-enumerated
// family silently broadcasts to every group member instead of just the
// owner, a privacy regression. This mirrors ws/server.go's exact adapter
// list (importing ws/server.go itself here would cycle back into this
// package) so adding a new adapter there without updating
// isOwnerVisibilityAdapter fails this test instead of shipping silently.
func TestIsOwnerVisibilityAdapter_CoversEveryRegisteredAdapterFamily(t *testing.T) {
	registered := []agentadapter.AgentAdapter{
		openclaw.NewAdapter(),
		acp.NewAdapter(),
		claude.NewAdapter(),
		codex.NewAdapter(),
		cursor.NewAdapter(),
		deepseek.NewAdapter(),
		pi.NewAdapter(),
		gemini.NewAdapter(),
		hermes.NewAdapter(),
		qwen.NewAdapter(),
		openhuman.NewAdapter(),
		reasonix.NewAdapter(),
		codewhale.NewAdapter(),
		opencode.NewAdapter(),
		kiro.NewAdapter(),
		copilot.NewAdapter(),
		agy.NewAdapter(),
		kimi.NewAdapter(),
		qodercli.NewAdapter(),
		qoderclicn.NewAdapter(),
		mcode.NewAdapter(),
		dim.NewAdapter(),
		traecli.NewAdapter(),
		omp.NewAdapter(),
		codebuddy.NewAdapter(),
		grok.NewAdapter(),
		qwenpaw.NewAdapter(),
		zeroclaw.NewAdapter(),
	}
	for _, a := range registered {
		family := a.Family()
		if !isOwnerVisibilityAdapter(family) {
			t.Errorf("isOwnerVisibilityAdapter(%q) = false, want true — this adapter is registered in ws/server.go but missing from isOwnerVisibilityAdapter's enumeration", family)
		}
		if !isOwnerVisibilityAdapter(a.AdapterID()) {
			t.Errorf("isOwnerVisibilityAdapter(%q) = false, want true (AdapterID form)", a.AdapterID())
		}
	}
}

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
			name:      "traecli exec approval card",
			adapterID: "traecli/base",
			content:   "[Approve](grix://card/exec_approval?approval_id=req_1)",
			ownerID:   1011,
			want:      []int64{1011},
		},
		{
			name:      "grok open session card",
			adapterID: "grok/base",
			content:   "[Open](grix://card/agent_open_session?summary_text=missing)",
			ownerID:   1006,
			want:      []int64{1006},
		},
		{
			name:      "qwenpaw approval card",
			adapterID: "qwenpaw/base",
			content:   "[Approval](grix://card/exec_approval?approval_id=req_1)",
			ownerID:   1007,
			want:      []int64{1007},
		},
		{
			name:      "zeroclaw status card",
			adapterID: "zeroclaw/base",
			content:   "[Status](grix://card/exec_status?status=resolved-allow-once)",
			ownerID:   1008,
			want:      []int64{1008},
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
