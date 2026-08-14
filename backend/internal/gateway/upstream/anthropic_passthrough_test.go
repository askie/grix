package upstream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 白名单只放行 anthropic-version / anthropic-beta；客户端的虚拟 Key、Authorization、
// 代理/调试头一律剥离（plan-direct-relay-first-refactor §6.4）。
func TestFilterAnthropicPassthroughHeaders(t *testing.T) {
	inbound := http.Header{
		"Anthropic-Version":   []string{"2024-10-01"},
		"Anthropic-Beta":      []string{"prompt-caching-2024-07-31"},
		"X-Api-Key":           []string{"gvk_client-virtual-key"},
		"Authorization":       []string{"Bearer gvk_client-virtual-key"},
		"X-Debug-Trace":       []string{"1"},
		"Proxy-Authorization": []string{"Basic xxx"},
	}

	filtered := FilterAnthropicPassthroughHeaders(inbound)
	assert.Equal(t, "2024-10-01", filtered.Get("anthropic-version"))
	assert.Equal(t, "prompt-caching-2024-07-31", filtered.Get("anthropic-beta"))
	assert.Empty(t, filtered.Get("x-api-key"))
	assert.Empty(t, filtered.Get("authorization"))
	assert.Empty(t, filtered.Get("x-debug-trace"))
	assert.Len(t, filtered, 2)
}

// 透传头应用到上行请求时：客户端的 anthropic-version 覆盖默认值；即使透传集合里混进了
// 鉴权头（调用方没走过滤器），真实厂商 Key 也恒由服务端权威覆写，客户端注不进来。
func TestAnthropicApplyHeadersOverridesAuth(t *testing.T) {
	var gotKey, gotVersion, gotBeta, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotBeta = r.Header.Get("anthropic-beta")
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"model":"deepseek-v4-flash","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	// 故意构造一个含鉴权头的 passthrough（模拟调用方绕过过滤器），验证双保险。
	dirty := http.Header{
		"Anthropic-Version": []string{"2024-10-01"},
		"Anthropic-Beta":    []string{"fine-grained-tool-streaming-2025-05-14"},
		"X-Api-Key":         []string{"gvk_client-virtual-key"},
		"Authorization":     []string{"Bearer gvk_client-virtual-key"},
	}
	a := NewAnthropic("sk-real-upstream-key", srv.URL, nil).WithPassthroughHeaders(dirty)
	rec := httptest.NewRecorder()
	_, _, err := a.Forward(context.Background(), []byte(`{"model":"deepseek-chat","messages":[]}`), false, rec)
	require.NoError(t, err)

	assert.Equal(t, "sk-real-upstream-key", gotKey, "上行必须用真实厂商 Key，客户端虚拟 Key 不得透传")
	assert.Empty(t, gotAuth, "客户端 Authorization 不得透传到上游")
	assert.Equal(t, "2024-10-01", gotVersion, "客户端的 anthropic-version 应覆盖默认值")
	assert.Equal(t, "fine-grained-tool-streaming-2025-05-14", gotBeta)
}

// count_tokens：路径、方法、请求体透传、状态码与响应体（含上游错误体）原样返回。
func TestAnthropicCountTokens(t *testing.T) {
	var gotPath, gotMethod, gotBody, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		gotKey = r.Header.Get("x-api-key")
		w.Write([]byte(`{"input_tokens":42}`))
	}))
	defer srv.Close()

	a := NewAnthropic("sk-real-upstream-key", srv.URL+"/anthropic", nil)
	status, body, err := a.CountTokens(context.Background(), []byte(`{"model":"deepseek-v4-flash","messages":[]}`))
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{"input_tokens":42}`, string(body))
	assert.Equal(t, "/anthropic/v1/messages/count_tokens", gotPath)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.JSONEq(t, `{"model":"deepseek-v4-flash","messages":[]}`, gotBody)
	assert.Equal(t, "sk-real-upstream-key", gotKey)
}

// 上游业务错误（如模型不支持 count_tokens）不是传输错误：status+body 原样交给网关透传。
func TestAnthropicCountTokensUpstreamErrorPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"count_tokens not supported"}}`))
	}))
	defer srv.Close()

	a := NewAnthropic("sk-real-upstream-key", srv.URL, nil)
	status, body, err := a.CountTokens(context.Background(), []byte(`{"model":"deepseek-v4-flash"}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, string(body), "count_tokens not supported")
}
