package provider

import (
	"testing"
)

func TestStreamChunk(t *testing.T) {
	chunk := StreamChunk{
		DeltaContent:     "Hello",
		IsFinish:         false,
		PromptTokens:     10,
		CompletionTokens: 5,
		Error:            nil,
	}

	if chunk.DeltaContent != "Hello" {
		t.Error("delta content mismatch")
	}
	if chunk.IsFinish {
		t.Error("is_finish should be false")
	}
	if chunk.PromptTokens != 10 {
		t.Errorf("expected 10 prompt tokens, got %d", chunk.PromptTokens)
	}
}

func TestStreamChunkWithError(t *testing.T) {
	chunk := StreamChunk{
		DeltaContent: "",
		IsFinish:     true,
		Error:        ErrProviderError("test error"),
	}

	if chunk.Error == nil {
		t.Error("expected error")
	}
}

func TestMessage(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "Hello, world!",
	}

	if msg.Role != "user" {
		t.Error("role mismatch")
	}
	if msg.Content != "Hello, world!" {
		t.Error("content mismatch")
	}
}

func TestRequest(t *testing.T) {
	req := Request{
		Model:       "gpt-4",
		Messages:    []Message{{Role: "user", Content: "test"}},
		MaxTokens:   1000,
		Temperature: 0.7,
		Stream:      true,
	}

	if req.Model != "gpt-4" {
		t.Error("model mismatch")
	}
	if len(req.Messages) != 1 {
		t.Error("expected 1 message")
	}
	if !req.Stream {
		t.Error("stream should be true")
	}
}

func TestRequestDefaults(t *testing.T) {
	req := Request{
		Model:    "test",
		Messages: []Message{},
	}

	// Verify zero values
	if req.MaxTokens != 0 {
		t.Error("max_tokens should default to 0")
	}
	if req.Temperature != 0 {
		t.Error("temperature should default to 0")
	}
	if req.Stream {
		t.Error("stream should default to false")
	}
}

func TestMessageRoles(t *testing.T) {
	roles := []string{"system", "user", "assistant"}

	for _, role := range roles {
		msg := Message{Role: role, Content: "test"}
		if msg.Role != role {
			t.Errorf("role mismatch for %s", role)
		}
	}
}

func TestStreamChunkFinish(t *testing.T) {
	chunk := StreamChunk{
		DeltaContent:     "",
		IsFinish:         true,
		PromptTokens:     100,
		CompletionTokens: 50,
	}

	if !chunk.IsFinish {
		t.Error("is_finish should be true")
	}
	if chunk.DeltaContent != "" {
		t.Error("delta_content should be empty for finish chunk")
	}
}

func TestRequestMultipleMessages(t *testing.T) {
	req := Request{
		Model: "test-model",
		Messages: []Message{
			{Role: "system", Content: "You are helpful"},
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi!"},
			{Role: "user", Content: "How are you?"},
		},
		Stream: true,
	}

	if len(req.Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(req.Messages))
	}

	// Verify order
	if req.Messages[0].Role != "system" {
		t.Error("first message should be system")
	}
	if req.Messages[len(req.Messages)-1].Role != "user" {
		t.Error("last message should be user")
	}
}

func TestStreamCallback(t *testing.T) {
	// Test that StreamCallback type works correctly
	var callback StreamCallback

	receivedChunks := []StreamChunk{}
	callback = func(chunk StreamChunk) {
		receivedChunks = append(receivedChunks, chunk)
	}

	// Simulate streaming
	callback(StreamChunk{DeltaContent: "Hello", IsFinish: false})
	callback(StreamChunk{DeltaContent: " World", IsFinish: false})
	callback(StreamChunk{DeltaContent: "", IsFinish: true})

	if len(receivedChunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(receivedChunks))
	}
	if receivedChunks[2].IsFinish != true {
		t.Error("last chunk should have is_finish=true")
	}
}
