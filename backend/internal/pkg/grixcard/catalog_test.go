package grixcard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKnownTypesRegistry_EveryTypeHasOwnerOnlyAndPushBody(t *testing.T) {
	if len(KnownTypes) == 0 {
		t.Fatal("KnownTypes must not be empty")
	}
	seen := make(map[string]struct{}, len(KnownTypes))
	for _, cardType := range KnownTypes {
		if cardType == "" {
			t.Fatal("KnownTypes contains empty type")
		}
		if _, dup := seen[cardType]; dup {
			t.Fatalf("duplicate KnownTypes entry %q", cardType)
		}
		seen[cardType] = struct{}{}

		if !IsOwnerOnlyInGroup(cardType) {
			t.Errorf("card type %q must be owner-only in groups (public allowlist is empty by default)", cardType)
		}
		body := PushBody(cardType)
		if body == "" {
			t.Errorf("PushBody(%q) returned empty", cardType)
		}
		if strings.Contains(body, "grix://") {
			t.Errorf("PushBody(%q)=%q must not contain grix://", cardType, body)
		}

		// Content path
		content := "[card](grix://card/" + cardType + "?d=%7B%7D)"
		if got := DetectType(content, nil); got != cardType {
			t.Errorf("DetectType(content %q)=%q want %q", cardType, got, cardType)
		}
		// biz_card.type path
		extra, _ := json.Marshal(map[string]any{
			"biz_card": map[string]any{"type": cardType, "payload": map[string]any{}},
		})
		if got := DetectType("plain text", extra); got != cardType {
			t.Errorf("DetectType(biz_card %q)=%q want %q", cardType, got, cardType)
		}
	}
}

func TestDetectTypeLongestPrefixWins(t *testing.T) {
	if got := DetectType("grix://card/agent_question_reply?d=1", nil); got != TypeAgentQuestionReply {
		t.Fatalf("got=%q want=%q", got, TypeAgentQuestionReply)
	}
	if got := DetectType("grix://card/tool_execution_group?d=1", nil); got != TypeToolExecutionGroup {
		t.Fatalf("got=%q want=%q", got, TypeToolExecutionGroup)
	}
	if got := DetectType("grix://card/agent_open_session_submit?d=1", nil); got != TypeAgentOpenSessionSubmit {
		t.Fatalf("got=%q want=%q", got, TypeAgentOpenSessionSubmit)
	}
}

func TestPushBodyUnknownNeverLeaksURI(t *testing.T) {
	body := PushBody("future_widget")
	if strings.Contains(body, "grix://") || body == "" {
		t.Fatalf("PushBody(unknown)=%q", body)
	}
}
