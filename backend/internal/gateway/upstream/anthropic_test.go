package upstream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAnthropicUsage(t *testing.T) {
	// 缓存读命中：input_tokens 只是新输入，缓存读单列——校验不会重复扣、也不会漏。
	u := normalizeAnthropicUsage(anthropicUsage{
		InputTokens: 3, CacheReadInputTokens: 7, CacheCreationInputTokens: 0, OutputTokens: 12,
	})
	assert.Equal(t, 7, u.CachedTokens)
	assert.Equal(t, 3, u.UncachedTokens)
	assert.Equal(t, 12, u.CompletionTokens)
	assert.Equal(t, 0, u.ReasoningTokens)

	// 缓存写（真 Anthropic 才有）暂并入未命中桶。
	u2 := normalizeAnthropicUsage(anthropicUsage{InputTokens: 4, CacheCreationInputTokens: 5, OutputTokens: 1})
	assert.Equal(t, 9, u2.UncachedTokens)
}

// 用抓到的 DeepSeek Anthropic 端点真实非流式响应做基准。
func TestAnthropicForwardNonStream(t *testing.T) {
	const realResp = `{"id":"b344e045","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[{"type":"text","text":"Hello!"}],"stop_reason":"max_tokens","usage":{"input_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":8,"service_tier":"standard"}}`

	var gotPath, gotKey, gotVer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVer = r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(realResp))
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL+"/anthropic", nil)
	rec := httptest.NewRecorder()
	model, usage, err := a.Forward(context.Background(), []byte(`{"model":"deepseek-chat","max_tokens":8,"messages":[]}`), false, rec)
	require.NoError(t, err)

	assert.Equal(t, "/anthropic/v1/messages", gotPath)
	assert.Equal(t, "sk-test", gotKey)
	assert.Equal(t, anthropicVersion, gotVer)
	assert.Equal(t, "deepseek-v4-flash", model) // 以响应体 model 为准（规范名）
	require.NotNil(t, usage)
	assert.Equal(t, 0, usage.CachedTokens)
	assert.Equal(t, 5, usage.UncachedTokens)
	assert.Equal(t, 8, usage.CompletionTokens)
	// 响应体原样回写给客户端
	assert.Equal(t, realResp, rec.Body.String())
}

// 用抓到的 DeepSeek Anthropic 端点真实流式 SSE 做基准。
func TestAnthropicForwardStream(t *testing.T) {
	const realSSE = `event: message_start
data: {"type":"message_start","message":{"id":"a76","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[],"usage":{"input_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0,"service_tier":"standard"}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":6,"service_tier":"standard"}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, realSSE)
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL+"/anthropic", nil)
	rec := httptest.NewRecorder()
	model, usage, err := a.Forward(context.Background(), []byte(`{"model":"deepseek-chat","stream":true,"messages":[]}`), true, rec)
	require.NoError(t, err)

	assert.Equal(t, "deepseek-v4-flash", model)
	require.NotNil(t, usage)
	assert.Equal(t, 0, usage.CachedTokens)
	assert.Equal(t, 10, usage.UncachedTokens)  // 输入取 message_start
	assert.Equal(t, 6, usage.CompletionTokens) // 输出取最终 message_delta
	// 透传：event: 与 data: 行都要原样回给客户端
	assert.True(t, strings.Contains(rec.Body.String(), "event: message_start"))
	assert.True(t, strings.Contains(rec.Body.String(), "event: content_block_delta"))
	assert.True(t, strings.Contains(rec.Body.String(), "event: message_stop"))
}

func TestAnthropicUpstreamErrorPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`)
	}))
	defer srv.Close()

	a := NewAnthropic("sk-bad", srv.URL+"/anthropic", nil)
	rec := httptest.NewRecorder()
	_, usage, err := a.Forward(context.Background(), []byte(`{"model":"deepseek-chat"}`), false, rec)
	require.Error(t, err)
	assert.Nil(t, usage)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "authentication_error")
}
