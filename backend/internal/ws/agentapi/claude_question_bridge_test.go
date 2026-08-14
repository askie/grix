package agentapi

import (
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/grixactions"
)

func TestParseClaudeQuestionCommandParsesStructuredAnswer(t *testing.T) {
	parsed := parseClaudeQuestionCommand(grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
		RequestID: "req-1",
		Response: map[string]any{
			"type": "map",
			"entries": []map[string]any{
				{"key": "1", "value": "prod"},
				{"key": "2", "value": "cn-hz"},
			},
		},
	}))
	if !parsed.matched || !parsed.ok {
		t.Fatalf("parsed=%#v", parsed)
	}
	if parsed.requestID != "req-1" {
		t.Fatalf("request_id=%q want=req-1", parsed.requestID)
	}
	if parsed.action != "" {
		t.Fatalf("action=%q want empty", parsed.action)
	}
	if parsed.response["type"] != "map" {
		t.Fatalf("response=%#v", parsed.response)
	}
}

func TestParseClaudeQuestionCommandParsesURLActions(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name: "accept",
			command: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
				RequestID: "req-url-1",
				Action:    "accept",
			}),
			want: "accept",
		},
		{
			name: "cancel",
			command: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
				RequestID: "req-url-1",
				Action:    "cancel",
			}),
			want: "cancel",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed := parseClaudeQuestionCommand(tc.command)
			if !parsed.matched || !parsed.ok {
				t.Fatalf("parsed=%#v", parsed)
			}
			if parsed.action != tc.want {
				t.Fatalf("action=%q want=%q", parsed.action, tc.want)
			}
			if parsed.response != nil {
				t.Fatalf("response=%#v want=nil", parsed.response)
			}
		})
	}
}

func TestBuildClaudeQuestionReplyFallbackContent(t *testing.T) {
	mapReply := parseClaudeQuestionCommand(grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
		RequestID: "question-abc",
		Response: map[string]any{
			"type": "map",
			"entries": []map[string]any{
				{"key": "学段", "value": "A-Level"},
				{"key": "演示内容", "value": "看几道题预览"},
			},
		},
	}))
	content := buildClaudeQuestionReplyFallbackContent(mapReply)
	if content == "" {
		t.Fatal("map reply fallback content is empty")
	}
	for _, want := range []string{"question-abc", "学段: A-Level", "演示内容: 看几道题预览"} {
		if !strings.Contains(content, want) {
			t.Fatalf("fallback content %q missing %q", content, want)
		}
	}

	singleReply := parseClaudeQuestionCommand(grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
		RequestID: "question-single",
		Response: map[string]any{
			"type":  "single",
			"value": "prod",
		},
	}))
	content = buildClaudeQuestionReplyFallbackContent(singleReply)
	if !strings.Contains(content, "prod") {
		t.Fatalf("single reply fallback content %q missing value", content)
	}

	cancelReply := parseClaudeQuestionCommand(grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
		RequestID: "question-cancel",
		Action:    "cancel",
	}))
	if got := buildClaudeQuestionReplyFallbackContent(cancelReply); got != "" {
		t.Fatalf("cancel action should have no fallback content, got %q", got)
	}
}

func TestBuildClaudeQuestionReplyFallbackEventKeepsRouting(t *testing.T) {
	evt := DelegateEventPayload{
		EventID:   "evt-1",
		AgentID:   7,
		OwnerID:   9,
		SessionID: "sess-1",
		MsgID:     123,
		Content:   "grix://card/agent_question_reply?...",
	}
	parsed := parseClaudeQuestionCommand(grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
		RequestID: "question-route",
		Response:  map[string]any{"type": "single", "value": "yes"},
	}))
	fallback := buildClaudeQuestionReplyFallbackEvent(evt, parsed)
	if fallback == nil {
		t.Fatal("fallback event is nil")
	}
	if fallback.EventID != evt.EventID || fallback.AgentID != evt.AgentID ||
		fallback.OwnerID != evt.OwnerID || fallback.SessionID != evt.SessionID ||
		fallback.MsgID != evt.MsgID {
		t.Fatalf("fallback routing fields changed: %#v", fallback)
	}
	if fallback.Content == evt.Content || fallback.Content == "" {
		t.Fatalf("fallback content not rewritten: %q", fallback.Content)
	}
}

func TestExtractAgentQuestionRequestID(t *testing.T) {
	content := "[[Agent Question] question-1783124555819-t59ont](grix://card/agent_question?d=%7B%22request_id%22%3A%22question-1783124555819-t59ont%22%2C%22mode%22%3A%22form%22%7D)"
	if got := extractAgentQuestionRequestID(content); got != "question-1783124555819-t59ont" {
		t.Fatalf("request_id=%q", got)
	}
	if got := extractAgentQuestionRequestID("plain text"); got != "" {
		t.Fatalf("expected empty for plain text, got %q", got)
	}
	if got := extractAgentQuestionRequestID("grix://card/agent_status?category=session"); got != "" {
		t.Fatalf("expected empty for status card, got %q", got)
	}
}
