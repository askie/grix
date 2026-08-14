package llmclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateJSONResponsesStyleAppliesExtraOptions(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedHeader string
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedHeader = r.Header.Get("X-Trace")
		w.Header().Set("Content-Type", "application/json")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "resp_123",
			"created_at": 1710000000,
			"model":      "test-model",
			"object":     "response",
			"output": []map[string]any{
				{
					"id":     "msg_1",
					"role":   "assistant",
					"status": "completed",
					"type":   "message",
					"content": []map[string]any{
						{
							"type":        "output_text",
							"text":        `{"translated":"你好"}`,
							"annotations": []any{},
						},
					},
				},
			},
			"status": "completed",
			"usage": map[string]any{
				"input_tokens": 12,
				"input_tokens_details": map[string]any{
					"cached_tokens": 0,
				},
				"output_tokens": 8,
				"output_tokens_details": map[string]any{
					"reasoning_tokens": 0,
				},
				"total_tokens": 20,
			},
		})
	}))
	defer server.Close()

	client, err := New(Config{
		APIKey:           "test-key",
		BaseURL:          server.URL,
		APIStyle:         "responses",
		DefaultModel:     "test-model",
		ReasoningEffort:  "minimal",
		ExtraBodyJSON:    `{"thinking":{"enabled":false}}`,
		ExtraHeadersJSON: `{"X-Trace":"translation"}`,
	})
	if err != nil {
		t.Fatalf("new llm client: %v", err)
	}

	temperature := 0.1
	var dest struct {
		Translated string `json:"translated"`
	}
	resp, err := client.GenerateJSON(context.Background(), JSONRequest{
		Instructions:    "translate to zh-CN",
		Input:           `{"name":"shrimp"}`,
		SchemaName:      "egg_translation",
		Schema:          map[string]any{"type": "object"},
		Temperature:     &temperature,
		MaxOutputTokens: 1200,
	}, &dest)
	if err != nil {
		t.Fatalf("generate json: %v", err)
	}

	if capturedPath != "/responses" {
		t.Fatalf("expected /responses path, got %q", capturedPath)
	}
	if capturedHeader != "translation" {
		t.Fatalf("expected extra header, got %q", capturedHeader)
	}
	if effort := getNestedString(capturedBody, "reasoning", "effort"); effort != "minimal" {
		t.Fatalf("expected reasoning.effort=minimal, got %q", effort)
	}
	if enabled, ok := getNestedBool(capturedBody, "thinking", "enabled"); !ok || enabled {
		t.Fatalf("expected thinking.enabled=false, got %v (ok=%v)", enabled, ok)
	}
	if dest.Translated != "你好" {
		t.Fatalf("expected translated output, got %q", dest.Translated)
	}
	if resp.OutputTokens != 8 || resp.TotalTokens != 20 {
		t.Fatalf("expected usage to be parsed, got %+v", resp)
	}
}

func TestGenerateJSONChatCompletionsStyleAppliesExtraOptions(t *testing.T) {
	t.Parallel()

	var capturedPath string
	var capturedHeader string
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedHeader = r.Header.Get("X-Trace")
		w.Header().Set("Content-Type", "application/json")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_123",
			"object":  "chat.completion",
			"created": 1710000001,
			"model":   "doubao-seed-2-0-mini-260215",
			"choices": []map[string]any{
				{
					"index":         0,
					"finish_reason": "stop",
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"translated":"こんにちは"}`,
						"refusal": "",
					},
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     18,
				"completion_tokens": 9,
				"total_tokens":      27,
			},
		})
	}))
	defer server.Close()

	client, err := New(Config{
		APIKey:           "test-key",
		BaseURL:          server.URL,
		APIStyle:         "chat_completions",
		DefaultModel:     "doubao-seed-2-0-mini-260215",
		ReasoningEffort:  "minimal",
		ExtraBodyJSON:    `{"thinking":{"enabled":false}}`,
		ExtraHeadersJSON: `{"X-Trace":"translation"}`,
	})
	if err != nil {
		t.Fatalf("new llm client: %v", err)
	}

	temperature := 0.1
	var dest struct {
		Translated string `json:"translated"`
	}
	resp, err := client.GenerateJSON(context.Background(), JSONRequest{
		Instructions:    "translate to ja-JP",
		Input:           `{"name":"shrimp"}`,
		SchemaName:      "egg_translation",
		Schema:          map[string]any{"type": "object"},
		Temperature:     &temperature,
		MaxOutputTokens: 1200,
	}, &dest)
	if err != nil {
		t.Fatalf("generate json: %v", err)
	}

	if capturedPath != "/chat/completions" {
		t.Fatalf("expected /chat/completions path, got %q", capturedPath)
	}
	if capturedHeader != "translation" {
		t.Fatalf("expected extra header, got %q", capturedHeader)
	}
	if value, ok := capturedBody["reasoning_effort"].(string); !ok || value != "minimal" {
		t.Fatalf("expected reasoning_effort=minimal, got %#v", capturedBody["reasoning_effort"])
	}
	messages, ok := capturedBody["messages"].([]any)
	if !ok || len(messages) < 1 {
		t.Fatalf("expected chat completion messages, got %#v", capturedBody["messages"])
	}
	firstMessage, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first message object, got %#v", messages[0])
	}
	if role, ok := firstMessage["role"].(string); !ok || role != "system" {
		t.Fatalf("expected first message role=system, got %#v", firstMessage["role"])
	}
	if enabled, ok := getNestedBool(capturedBody, "thinking", "enabled"); !ok || enabled {
		t.Fatalf("expected thinking.enabled=false, got %v (ok=%v)", enabled, ok)
	}
	if value, ok := capturedBody["max_tokens"].(float64); !ok || int64(value) != 1200 {
		t.Fatalf("expected max_tokens=1200, got %#v", capturedBody["max_tokens"])
	}
	if dest.Translated != "こんにちは" {
		t.Fatalf("expected translated output, got %q", dest.Translated)
	}
	if resp.InputTokens != 18 || resp.OutputTokens != 9 || resp.TotalTokens != 27 {
		t.Fatalf("expected usage to be parsed, got %+v", resp)
	}
}

func TestNewRejectsInvalidExtraHeadersJSON(t *testing.T) {
	t.Parallel()

	_, err := New(Config{
		APIKey:           "test-key",
		ExtraHeadersJSON: `{"X-Trace":1}`,
	})
	if err == nil {
		t.Fatal("expected invalid extra headers json to fail")
	}
}

func getNestedString(root map[string]any, keys ...string) string {
	current := root
	for idx, key := range keys {
		value, ok := current[key]
		if !ok {
			return ""
		}
		if idx == len(keys)-1 {
			strValue, _ := value.(string)
			return strValue
		}
		next, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}

func getNestedBool(root map[string]any, keys ...string) (bool, bool) {
	current := root
	for idx, key := range keys {
		value, ok := current[key]
		if !ok {
			return false, false
		}
		if idx == len(keys)-1 {
			boolValue, ok := value.(bool)
			return boolValue, ok
		}
		next, ok := value.(map[string]any)
		if !ok {
			return false, false
		}
		current = next
	}
	return false, false
}
