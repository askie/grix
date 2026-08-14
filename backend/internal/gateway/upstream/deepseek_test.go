package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeepSeekForwardResponsesNonStream(t *testing.T) {
	var gotPath, gotKey string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_1","object":"response","model":"deepseek-v4-flash","status":"completed","output":[],"usage":{"input_tokens":1000,"input_tokens_details":{"cached_tokens":300},"output_tokens":500,"output_tokens_details":{"reasoning_tokens":100},"total_tokens":1500}}`)
	}))
	defer srv.Close()

	d := NewDeepSeek("sk-test", srv.URL+"/", nil)
	rec := httptest.NewRecorder()
	model, usage, err := d.ForwardResponses(context.Background(), []byte(`{"model":"deepseek-v4-flash","input":"ping","store":true}`), false, rec)
	require.NoError(t, err)

	assert.Equal(t, "/responses", gotPath)
	assert.Equal(t, "Bearer sk-test", gotKey)
	assert.Equal(t, "deepseek-v4-flash", gotBody["model"])
	assert.Equal(t, false, gotBody["store"])
	assert.Equal(t, "deepseek-v4-flash", model)
	require.NotNil(t, usage)
	assert.Equal(t, 300, usage.CachedTokens)
	assert.Equal(t, 700, usage.UncachedTokens)
	assert.Equal(t, 500, usage.CompletionTokens)
	assert.Equal(t, 100, usage.ReasoningTokens)
	assert.Contains(t, rec.Body.String(), `"object":"response"`)
}

func TestDeepSeekForwardResponsesNonStreamWithoutUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_1","object":"response","model":"deepseek-v4-flash","status":"failed","output":[]}`)
	}))
	defer srv.Close()

	d := NewDeepSeek("sk-test", srv.URL, nil)
	rec := httptest.NewRecorder()
	model, usage, err := d.ForwardResponses(context.Background(), []byte(`{"model":"deepseek-v4-flash","input":"ping"}`), false, rec)
	require.NoError(t, err)

	assert.Equal(t, "deepseek-v4-flash", model)
	assert.Nil(t, usage)
	assert.Contains(t, rec.Body.String(), `"status":"failed"`)
}

func TestDeepSeekForwardResponsesStream(t *testing.T) {
	const sse = `event: response.created
data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress","model":"deepseek-v4-flash"}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"Hi","item_id":"msg_1","output_index":0,"content_index":0}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","model":"deepseek-v4-flash","usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":4},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":16}}}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/responses", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer srv.Close()

	d := NewDeepSeek("sk-test", srv.URL, nil)
	rec := httptest.NewRecorder()
	model, usage, err := d.ForwardResponses(context.Background(), []byte(`{"model":"deepseek-v4-flash","input":"ping","stream":true}`), true, rec)
	require.NoError(t, err)

	assert.Equal(t, "deepseek-v4-flash", model)
	require.NotNil(t, usage)
	assert.Equal(t, 4, usage.CachedTokens)
	assert.Equal(t, 6, usage.UncachedTokens)
	assert.Equal(t, 6, usage.CompletionTokens)
	assert.Equal(t, 2, usage.ReasoningTokens)
	assert.Contains(t, rec.Body.String(), "event: response.completed")
	assert.NotContains(t, rec.Body.String(), "[DONE]")
}

func TestDeepSeekForwardResponsesStreamLargeTerminalEvent(t *testing.T) {
	largeText := strings.Repeat("x", 1024*1024+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\n")
		_, _ = io.WriteString(w, `data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","model":"deepseek-v4-flash","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"`)
		_, _ = io.WriteString(w, largeText)
		_, _ = io.WriteString(w, `","annotations":[]}]}],"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":4},"output_tokens":6,"total_tokens":16}}}`)
		_, _ = io.WriteString(w, "\n\n")
	}))
	defer srv.Close()

	d := NewDeepSeek("sk-test", srv.URL, nil)
	rec := httptest.NewRecorder()
	model, usage, err := d.ForwardResponses(context.Background(), []byte(`{"model":"deepseek-v4-flash","input":"ping","stream":true}`), true, rec)
	require.NoError(t, err)

	assert.Equal(t, "deepseek-v4-flash", model)
	require.NotNil(t, usage)
	assert.Equal(t, 4, usage.CachedTokens)
	assert.Equal(t, 6, usage.UncachedTokens)
	assert.Equal(t, 6, usage.CompletionTokens)
	assert.Greater(t, rec.Body.Len(), 1024*1024)
}
