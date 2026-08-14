package upstream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 缺 message_start 时必须报错、不落 usage —— 防止把"缓存命中0+新输入0+输出6"当成免费账结算。
func TestAnthropicStreamMissingMessageStartFails(t *testing.T) {
	const sse = `event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":0,"output_tokens":6}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL+"/anthropic", nil)
	rec := httptest.NewRecorder()
	_, usage, err := a.Forward(context.Background(), []byte(`{"model":"deepseek-chat","stream":true}`), true, rec)
	require.Error(t, err, "缺 message_start 必须报错")
	assert.Nil(t, usage, "不能返回 usage 触发结算")
	assert.Contains(t, err.Error(), "message_start")
}

// 缺 message_delta：Anthropic 官方约定输出量在 message_delta 里，若上游只有 message_start 就断流，
// 输出 tokens 会取自 message_start.usage.output_tokens（通常为 0）。此时应仍然结算（有 message_start），
// 但输出可能为 0 —— 校验行为符合契约（不 panic、不遗漏 usage）。
func TestAnthropicStreamOnlyMessageStartStillSettles(t *testing.T) {
	const sse = `event: message_start
data: {"type":"message_start","message":{"id":"x","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[],"usage":{"input_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":3,"output_tokens":0}}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL+"/anthropic", nil)
	rec := httptest.NewRecorder()
	model, usage, err := a.Forward(context.Background(), []byte(`{"model":"deepseek-chat","stream":true}`), true, rec)
	require.NoError(t, err)
	assert.Equal(t, "deepseek-v4-flash", model)
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.CachedTokens)
	assert.Equal(t, 10, usage.UncachedTokens)
	assert.Equal(t, 0, usage.CompletionTokens, "缺 message_delta 时输出取 message_start 的 0")
}

// 纯缓存命中场景：input_tokens=0（Claude风格纯缓存）+ cache_read_input_tokens 大量 + 输出。
// 校验 CachedTokens/UncachedTokens 桶分得对，UncachedTokens=0 不会被误算成缓存量。
func TestAnthropicStreamPureCacheHit(t *testing.T) {
	const sse = `event: message_start
data: {"type":"message_start","message":{"id":"x","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[],"usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":128,"output_tokens":0}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL+"/anthropic", nil)
	rec := httptest.NewRecorder()
	_, usage, err := a.Forward(context.Background(), []byte(`{"model":"deepseek-chat","stream":true}`), true, rec)
	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 128, usage.CachedTokens)
	assert.Equal(t, 0, usage.UncachedTokens)
	assert.Equal(t, 42, usage.CompletionTokens)
}

// 缓存写场景（真 Anthropic 才有 cache_creation_input_tokens>0）：一期按代码注释合入未命中桶。
// 校验：cache_creation 计入 UncachedTokens；未来接真 Anthropic 时改口径这里也会自然报错提醒。
func TestAnthropicStreamCacheCreation(t *testing.T) {
	const sse = `event: message_start
data: {"type":"message_start","message":{"id":"x","type":"message","role":"assistant","model":"claude-3-opus","content":[],"usage":{"input_tokens":100,"cache_creation_input_tokens":50,"cache_read_input_tokens":20,"output_tokens":0}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":30}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL+"/anthropic", nil)
	rec := httptest.NewRecorder()
	_, usage, err := a.Forward(context.Background(), []byte(`{"model":"claude-3-opus","stream":true}`), true, rec)
	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 20, usage.CachedTokens)
	assert.Equal(t, 150, usage.UncachedTokens, "缓存写(50)+新输入(100)合入未命中桶")
	assert.Equal(t, 30, usage.CompletionTokens)
}

// 非流式响应缺 usage 字段（厂商兜底返 usage=null 极端场景）：
// 与流式端 gotStart 门槛对称——零值 usage 返回 nil，让上层走 no_usage 失败留痕，
// 而不是归一化成全 0 桶静默记 0 元账、逃过对账。
func TestAnthropicNonStreamZeroUsage(t *testing.T) {
	const resp = `{"id":"x","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, resp)
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL+"/anthropic", nil)
	rec := httptest.NewRecorder()
	_, usage, err := a.Forward(context.Background(), []byte(`{"model":"deepseek-chat"}`), false, rec)
	require.NoError(t, err)
	assert.Nil(t, usage, "零 usage 必须返回 nil 走 no_usage 留痕，不得当 0 元账结算")
	assert.Equal(t, resp, rec.Body.String(), "响应体仍需原样透传给客户端")
}

// 断线不白嫖：客户端上下文提前 Cancel，但 upstream context 用 WithoutCancel 与之解耦，
// 上游请求仍能完整读到 usage 再结算。此测试模拟客户端断线，验证上游仍读完流拿到 usage。
func TestAnthropicClientCancelDoesNotAbortUpstream(t *testing.T) {
	var upstreamCompleted int32
	sseChunks := []string{
		`event: message_start
data: {"type":"message_start","message":{"id":"x","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[],"usage":{"input_tokens":50,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0}}}

`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}

`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":99}}

event: message_stop
data: {"type":"message_stop"}

`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		for _, chunk := range sseChunks {
			_, _ = io.WriteString(w, chunk)
			f.Flush()
			time.Sleep(20 * time.Millisecond)
		}
		atomic.StoreInt32(&upstreamCompleted, 1)
	}))
	defer srv.Close()

	// clientCtx 模拟客户端连接 —— 触发时立即 cancel
	clientCtx, cancel := context.WithCancel(context.Background())
	cancel() // 已经断线

	// 网关 serve() 层用 WithoutCancel 隔离——直接给适配器传"去 cancel 化"的 ctx
	upstreamCtx := context.WithoutCancel(clientCtx)

	a := NewAnthropic("sk-test", srv.URL+"/anthropic", nil)
	rec := httptest.NewRecorder()
	_, usage, err := a.Forward(upstreamCtx, []byte(`{"model":"deepseek-chat","stream":true}`), true, rec)
	require.NoError(t, err)
	require.NotNil(t, usage, "断线也要拿到 usage 才能防白嫖")
	assert.Equal(t, 50, usage.UncachedTokens)
	assert.Equal(t, 99, usage.CompletionTokens)
	assert.Equal(t, int32(1), atomic.LoadInt32(&upstreamCompleted), "上游 handler 必须跑完")
}

// 上游 5xx：错误体应被透传给客户端、Forward 返错、usage 为 nil。
func TestAnthropicUpstream5xxPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"upstream down"}}`)
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL+"/anthropic", nil)
	rec := httptest.NewRecorder()
	_, usage, err := a.Forward(context.Background(), []byte(`{"model":"deepseek-chat"}`), false, rec)
	require.Error(t, err)
	assert.Nil(t, usage)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "api_error")
}

// 长行 SSE：bufio.Scanner buffer 上限 1MB —— 校验小体量正常，防止将来有人误改 buffer 上限。
func TestAnthropicStreamLongLineHandled(t *testing.T) {
	longText := strings.Repeat("A", 200*1024) // 200KB 单行 delta，远小于 1MB 上限
	sse := "event: message_start\n" +
		`data: {"type":"message_start","message":{"id":"x","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + longText + `"}}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","delta":{},"usage":{"output_tokens":100000}}` + "\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer srv.Close()

	a := NewAnthropic("sk-test", srv.URL+"/anthropic", nil)
	rec := httptest.NewRecorder()
	_, usage, err := a.Forward(context.Background(), []byte(`{"model":"deepseek-chat","stream":true}`), true, rec)
	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 100000, usage.CompletionTokens)
}
