package agentapi

import "testing"

func TestBuildClaudeQuestionCardPayloadIncludesFormMessageAndStandardQuestions(t *testing.T) {
	payload, ok := buildClaudeQuestionCardPayload("req-form-1", map[string]interface{}{
		"message": "Choose the deployment target before continuing.",
		"requested_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"environment": map[string]interface{}{
					"type":        "string",
					"title":       "Environment",
					"description": "Choose an environment.",
					"enum":        []interface{}{"production", "staging"},
				},
			},
			"required": []interface{}{"environment"},
		},
	})
	if !ok {
		t.Fatal("expected question payload to build")
	}

	if got := payload["request_id"]; got != "req-form-1" {
		t.Fatalf("request_id=%v want=req-form-1", got)
	}
	if got := payload["mode"]; got != "form" {
		t.Fatalf("mode=%v want=form", got)
	}
	if got := payload["message"]; got != "Choose the deployment target before continuing." {
		t.Fatalf("message=%v", got)
	}
	if got := payload["footer_text"]; got != claudeQuestionFooterText {
		t.Fatalf("footer_text=%v", got)
	}
	questions, ok := payload["questions"].([]map[string]any)
	if !ok || len(questions) != 1 {
		t.Fatalf("questions=%#v", payload["questions"])
	}
	if got := questions[0]["index"]; got != 1 {
		t.Fatalf("index=%v want=1", got)
	}
	if got := questions[0]["header"]; got != "Environment" {
		t.Fatalf("header=%v want=Environment", got)
	}
	if got := questions[0]["field_key"]; got != "environment" {
		t.Fatalf("field_key=%v want=environment", got)
	}
	if got := questions[0]["prompt"]; got != "Choose an environment. Choose one of the listed options." {
		t.Fatalf("prompt=%v", got)
	}
}

func TestBuildClaudeURLQuestionCardPayloadUsesStableInputs(t *testing.T) {
	payload, ok := buildClaudeQuestionCardPayload("req-url-1", map[string]interface{}{
		"mode":    "url",
		"url":     "https://auth.example.com/login",
		"message": "Open the login page.",
	})
	if !ok {
		t.Fatal("expected url payload to build")
	}

	if got := payload["mode"]; got != "url" {
		t.Fatalf("mode=%v want=url", got)
	}
	if got := payload["message"]; got != "Open the login page." {
		t.Fatalf("message=%v want=Open the login page.", got)
	}
	if got := payload["open_url_label"]; got != "Open authentication page" {
		t.Fatalf("open_url_label=%v want=Open authentication page", got)
	}
	if got := payload["footer_text"]; got != "Open the page, finish the flow, then tap Complete. Cancel if you do not want to continue." {
		t.Fatalf("footer_text=%v", got)
	}
	if got := payload["submitted_accept_text"]; got != "Authentication completed." {
		t.Fatalf("submitted_accept_text=%v", got)
	}
	if got := payload["submitted_cancel_text"]; got != "Authentication canceled." {
		t.Fatalf("submitted_cancel_text=%v", got)
	}
}
