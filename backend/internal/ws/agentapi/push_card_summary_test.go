package agentapi

import (
	"encoding/json"
	"testing"
)

// buildQuestionCardContent renders a card the same way the producers do, so the
// tests exercise the real encoding (JSON "d" for nested payloads, flat query
// params otherwise).
func buildQuestionCardContent(payload map[string]any) string {
	return buildLocalGrixCardLink("[Agent Question] req-1", "agent_question", payload)
}

func TestQuestionPushSummaryFormCard(t *testing.T) {
	content := buildQuestionCardContent(map[string]any{
		"request_id": "question-1788582637914-c0zrjz",
		"mode":       "form",
		"questions": []map[string]any{
			{
				"index":   1,
				"header":  "部署环境",
				"prompt":  "这次改动要部署到哪个环境？",
				"options": []string{"预发", "生产"},
			},
		},
	})

	if got := questionPushSummary(content, nil); got != "部署环境：这次改动要部署到哪个环境？" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestQuestionPushSummaryPrefersMessage(t *testing.T) {
	content := buildQuestionCardContent(map[string]any{
		"request_id": "req-2",
		"mode":       "form",
		"message":    "需要确认发布范围",
		"questions": []map[string]any{
			{"index": 1, "header": "范围", "prompt": "只发后端吗？"},
		},
	})

	if got := questionPushSummary(content, nil); got != "需要确认发布范围" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestQuestionPushSummaryPromptWithoutHeader(t *testing.T) {
	content := buildQuestionCardContent(map[string]any{
		"request_id": "req-3",
		"mode":       "form",
		"questions": []map[string]any{
			{"index": 1, "prompt": "继续执行吗？"},
		},
	})

	if got := questionPushSummary(content, nil); got != "继续执行吗？" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestQuestionPushSummaryCollapsesMultilineText(t *testing.T) {
	content := buildQuestionCardContent(map[string]any{
		"request_id": "req-multiline",
		"mode":       "form",
		"questions": []map[string]any{
			{"index": 1, "header": "确认", "prompt": "改动包含：\n- 后端\n- 前端\n继续吗？"},
		},
	})

	if got := questionPushSummary(content, nil); got != "确认：改动包含： - 后端 - 前端 继续吗？" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestQuestionPushSummaryURLMode(t *testing.T) {
	// A url-mode card with no nested values is encoded as flat query params.
	content := buildQuestionCardContent(map[string]any{
		"request_id":     "req-4",
		"mode":           "url",
		"url":            "https://grix.im/guide",
		"open_url_label": "View Gemini authentication guide",
	})

	if got := questionPushSummary(content, nil); got != "View Gemini authentication guide" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestQuestionPushSummaryURLModeWithoutLabel(t *testing.T) {
	content := buildQuestionCardContent(map[string]any{
		"request_id": "req-5",
		"mode":       "url",
		"url":        "https://grix.im/guide",
	})

	if got := questionPushSummary(content, nil); got != "https://grix.im/guide" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestQuestionPushSummaryFromExtra(t *testing.T) {
	extra := json.RawMessage(`{"biz_card":{"version":1,"type":"agent_question","payload":{"request_id":"req-6","message":"来自 extra 的提问"}}}`)

	if got := questionPushSummary("请查看提问卡", extra); got != "来自 extra 的提问" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestQuestionPushSummaryNoCard(t *testing.T) {
	if got := questionPushSummary("普通消息，没有卡片链接", nil); got != "" {
		t.Fatalf("expected empty summary, got %q", got)
	}
	// A reply card must not be mistaken for a question card.
	reply := "[reply](grix://card/agent_question_reply?d=%7B%22request_id%22%3A%22req-7%22%7D)"
	if got := questionPushSummary(reply, nil); got != "" {
		t.Fatalf("expected empty summary for reply card, got %q", got)
	}
}

func TestApprovalPushSummaryCommand(t *testing.T) {
	// exec_approval payloads with nested values are encoded as JSON "d".
	content := buildLocalGrixCardLink("[Exec Approval] rm -rf build", "exec_approval", map[string]any{
		"approval_id":         "req_1",
		"approval_command_id": "req_1",
		"command":             "rm -rf build",
		"options":             []string{"allow-once", "deny"},
	})

	if got := approvalPushSummary(content, nil); got != "rm -rf build" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestApprovalPushSummaryFlatCommand(t *testing.T) {
	content := "[[Exec Approval] echo hi](grix://card/exec_approval?approval_command_id=req_2&approval_id=req_2&command=echo+hi)"

	if got := approvalPushSummary(content, nil); got != "echo hi" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestApprovalPushSummaryFallsBackToLinkText(t *testing.T) {
	content := "[[Exec Approval] git push origin main](grix://card/exec_approval?approval_id=req_3)"

	if got := approvalPushSummary(content, nil); got != "git push origin main" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestApprovalPushSummaryFromExtra(t *testing.T) {
	extra := json.RawMessage(`{"biz_card":{"version":1,"type":"exec_approval","payload":{"approval_id":"req_4","command":"kubectl apply -f k8s"}}}`)

	if got := approvalPushSummary("请审批", extra); got != "kubectl apply -f k8s" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestApprovalPushSummaryNoCard(t *testing.T) {
	if got := approvalPushSummary("普通消息，没有卡片链接", nil); got != "" {
		t.Fatalf("expected empty summary, got %q", got)
	}
}

// TestQuestionPushSummaryRealCard pins the behaviour against the exact card
// content that shipped the raw markdown to the lock screen.
func TestQuestionPushSummaryRealCard(t *testing.T) {
	content := "[[Agent Question] question-1788582637914-c0zrjz](grix://card/agent_question?d=%7B%22request_id%22%3A%22question-1788582637914-c0zrjz%22%2C%22mode%22%3A%22form%22%2C%22questions%22%3A%5B%7B%22index%22%3A1%2C%22header%22%3A%22%E5%8F%91%E5%B8%83%E8%8C%83%E5%9B%B4%22%2C%22prompt%22%3A%22%E8%BF%99%E6%AC%A1%E5%8F%AA%E5%8F%91%E5%90%8E%E7%AB%AF%E5%90%97%EF%BC%9F%22%7D%5D%7D)"

	if got := questionPushSummary(content, nil); got != "发布范围：这次只发后端吗？" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

// TestQuestionPushSummaryHeaderRepeatsPrompt covers agents that copy the prompt
// into the header, which used to push the same sentence twice.
func TestQuestionPushSummaryHeaderRepeatsPrompt(t *testing.T) {
	cases := []struct {
		name   string
		header string
		prompt string
		want   string
	}{
		{
			name:   "identical",
			header: "走哪条创建路径?(决定我下一步怎么执行)",
			prompt: "走哪条创建路径?(决定我下一步怎么执行)",
			want:   "走哪条创建路径?(决定我下一步怎么执行)",
		},
		{
			name:   "identical ignoring case and spacing",
			header: " Pick A Branch ",
			prompt: "pick a branch",
			want:   "pick a branch",
		},
		{
			name:   "prompt starts with header plus colon",
			header: "创建路径",
			prompt: "创建路径：走哪条？",
			want:   "创建路径：走哪条？",
		},
		{
			name:   "prompt starts with header without colon",
			header: "Deploy target",
			prompt: "Deploy target for this change?",
			want:   "Deploy target for this change?",
		},
		{
			name:   "distinct header still prefixes the prompt",
			header: "部署环境",
			prompt: "这次改动要部署到哪个环境？",
			want:   "部署环境：这次改动要部署到哪个环境？",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := buildQuestionCardContent(map[string]any{
				"request_id": "req-dup",
				"mode":       "form",
				"questions": []map[string]any{
					{"index": 1, "header": tc.header, "prompt": tc.prompt},
				},
			})

			if got := questionPushSummary(content, nil); got != tc.want {
				t.Fatalf("unexpected summary: %q, want %q", got, tc.want)
			}
		})
	}
}
