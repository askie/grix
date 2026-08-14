package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIStreamChat_DefaultModelAndUsage(t *testing.T) {
	var gotRequest map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer test-openai-key", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotRequest))

		fmt.Fprint(w, "event: ping\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7}}\n")
		fmt.Fprint(w, "data: [DONE]\n")
	}))
	defer server.Close()

	p := NewOpenAI("test-openai-key", server.URL, "gpt-default")
	var chunks []StreamChunk
	err := p.StreamChat(t.Context(), &Request{
		Messages:    []Message{{Role: "user", Content: "hello"}},
		Temperature: 0.5,
	}, func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	require.NoError(t, err)
	require.Len(t, chunks, 3)

	require.Equal(t, "gpt-default", gotRequest["model"])
	require.Equal(t, true, gotRequest["stream"])
	require.Equal(t, float64(0.5), gotRequest["temperature"])

	streamOptions, ok := gotRequest["stream_options"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, streamOptions["include_usage"])

	require.Equal(t, "hello", chunks[0].DeltaContent)
	require.False(t, chunks[0].IsFinish)
	require.Equal(t, " world", chunks[1].DeltaContent)
	require.True(t, chunks[1].IsFinish)
	require.Equal(t, 11, chunks[1].PromptTokens)
	require.Equal(t, 7, chunks[1].CompletionTokens)
	require.True(t, chunks[2].IsFinish)
	require.Empty(t, chunks[2].DeltaContent)
}

func TestOpenAIStreamChat_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "openai down", http.StatusBadGateway)
	}))
	defer server.Close()

	p := NewOpenAI("test-openai-key", server.URL, "gpt-default")
	err := p.StreamChat(t.Context(), &Request{Messages: []Message{{Role: "user", Content: "hello"}}}, func(StreamChunk) {})
	require.Error(t, err)
	require.Contains(t, err.Error(), "openai API error 502")
	require.Contains(t, err.Error(), "openai down")
}

func TestClaudeStreamChat_DefaultModelAndUsage(t *testing.T) {
	var gotRequest map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/messages", r.URL.Path)
		require.Equal(t, "test-claude-key", r.Header.Get("x-api-key"))
		require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotRequest))

		fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":13}}}\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hello\"}}\n")
		fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":9}}\n")
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n")
	}))
	defer server.Close()

	p := NewClaude("test-claude-key", server.URL, "claude-default")
	var chunks []StreamChunk
	err := p.StreamChat(t.Context(), &Request{
		Messages: []Message{
			{Role: "system", Content: "be helpful"},
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 512,
	}, func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	require.Equal(t, "claude-default", gotRequest["model"])
	require.Equal(t, true, gotRequest["stream"])
	require.Equal(t, float64(512), gotRequest["max_tokens"])
	require.Equal(t, "be helpful", gotRequest["system"])

	rawMessages, ok := gotRequest["messages"].([]any)
	require.True(t, ok)
	require.Len(t, rawMessages, 1)

	message, ok := rawMessages[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "user", message["role"])
	require.Equal(t, "hello", message["content"])

	require.Equal(t, "hello", chunks[0].DeltaContent)
	require.False(t, chunks[0].IsFinish)
	require.True(t, chunks[1].IsFinish)
	require.Equal(t, 13, chunks[1].PromptTokens)
	require.Equal(t, 9, chunks[1].CompletionTokens)
}

func TestClaudeStreamChat_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "claude down", http.StatusInternalServerError)
	}))
	defer server.Close()

	p := NewClaude("test-claude-key", server.URL, "claude-default")
	err := p.StreamChat(t.Context(), &Request{Messages: []Message{{Role: "user", Content: "hello"}}}, func(StreamChunk) {})
	require.Error(t, err)
	require.Contains(t, err.Error(), "claude API error 500")
	require.Contains(t, err.Error(), "claude down")
}

func TestLocalStreamChat_DefaultModelAndFinishStats(t *testing.T) {
	var gotRequest map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/chat", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotRequest))

		fmt.Fprint(w, "{\"message\":{\"content\":\"hello\"},\"done\":false}\n")
		fmt.Fprint(w, "{\"message\":{\"content\":\" world\"},\"done\":true,\"prompt_eval_count\":21,\"eval_count\":8}\n")
	}))
	defer server.Close()

	p := NewLocalProvider(server.URL, "ollama-default")
	var chunks []StreamChunk
	err := p.StreamChat(t.Context(), &Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Stream:   false,
	}, func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	require.Equal(t, "ollama-default", gotRequest["model"])
	require.Equal(t, false, gotRequest["stream"])

	rawMessages, ok := gotRequest["messages"].([]any)
	require.True(t, ok)
	require.Len(t, rawMessages, 1)

	message, ok := rawMessages[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "user", message["role"])
	require.Equal(t, "hello", message["content"])

	require.Equal(t, "hello", chunks[0].DeltaContent)
	require.False(t, chunks[0].IsFinish)
	require.Equal(t, " world", chunks[1].DeltaContent)
	require.True(t, chunks[1].IsFinish)
	require.Equal(t, 21, chunks[1].PromptTokens)
	require.Equal(t, 8, chunks[1].CompletionTokens)
}

func TestLocalStreamChat_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ollama down", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	p := NewLocalProvider(server.URL, "ollama-default")
	err := p.StreamChat(t.Context(), &Request{Messages: []Message{{Role: "user", Content: "hello"}}, Stream: true}, func(StreamChunk) {})
	require.Error(t, err)
	require.Contains(t, err.Error(), "local LLM API error 503")
	require.Contains(t, err.Error(), "ollama down")
}

func TestOpenAIStreamChat_SkipsMalformedSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {not-json}\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n")
		fmt.Fprint(w, "data: [DONE]\n")
	}))
	defer server.Close()

	p := NewOpenAI("test-openai-key", server.URL, "gpt-default")
	var chunks []StreamChunk
	err := p.StreamChat(t.Context(), &Request{Messages: []Message{{Role: "user", Content: "hello"}}}, func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, "ok", chunks[0].DeltaContent)
	require.True(t, chunks[1].IsFinish)
}
