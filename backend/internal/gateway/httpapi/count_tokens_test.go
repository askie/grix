package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// count_tokens 完整链路：鉴权 → 模型映射 → 凭据解析 → 透传上游 → 原样回写，不计费。
// 同时钉住原生协议头白名单：anthropic-beta 透传、客户端虚拟 Key 与无关头剥离。
func TestIntegrationAnthropicCountTokens(t *testing.T) {
	var gotPath, gotKey, gotBeta, gotAuth, gotDebug, gotBody string
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotBeta = r.Header.Get("anthropic-beta")
		gotAuth = r.Header.Get("Authorization")
		gotDebug = r.Header.Get("x-debug-trace")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"input_tokens":42}`))
	})
	fx.router.POST("/anthropic/v1/messages/count_tokens", fx.handler.CountTokens)
	beforeBalance := fx.balance()

	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages/count_tokens",
		strings.NewReader(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("x-api-key", fx.plainKey)
	req.Header.Set("Authorization", "Bearer "+fx.plainKey)
	req.Header.Set("anthropic-beta", "token-counting-2024-11-01")
	req.Header.Set("x-debug-trace", "1")
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"input_tokens":42}`, rec.Body.String())
	assert.Equal(t, "/anthropic/v1/messages/count_tokens", gotPath)
	// 上游鉴权恒为真实厂商 Key，客户端虚拟 Key（x-api-key / Authorization）不得透传。
	assert.Equal(t, "sk-upstream-official-key", gotKey)
	assert.Empty(t, gotAuth)
	assert.NotContains(t, gotKey, fx.plainKey)
	// 白名单头透传，无关头剥离。
	assert.Equal(t, "token-counting-2024-11-01", gotBeta)
	assert.Empty(t, gotDebug)
	// 模型映射与 Messages 同口径：别名 deepseek-chat 规范成 deepseek-v4-flash 再发上游。
	assert.Contains(t, gotBody, `"model":"deepseek-v4-flash"`)
	// 不产生生成用量、不扣费。
	assert.True(t, fx.balance().Equal(beforeBalance), "count_tokens 不得扣费")
}

// 缺 Key / 无效 Key 走 Anthropic 错误体，不触达上游。
func TestAnthropicCountTokensAuth(t *testing.T) {
	called := false
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	fx.router.POST("/anthropic/v1/messages/count_tokens", fx.handler.CountTokens)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages/count_tokens",
		strings.NewReader(`{"model":"deepseek-chat"}`))
	fx.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"type":"missing_api_key"`)
	assert.False(t, called)

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages/count_tokens",
		strings.NewReader(`{"model":"deepseek-chat"}`))
	req2.Header.Set("x-api-key", "gvk_nonexistent")
	fx.router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusUnauthorized, rec2.Code)
	assert.Contains(t, rec2.Body.String(), `"type":"invalid_api_key"`)
	assert.False(t, called)
}

// 上游业务错误（状态码+错误体）原样透传给客户端，不包成网关 502。
func TestIntegrationAnthropicCountTokensUpstreamErrorPassthrough(t *testing.T) {
	fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"count_tokens not supported"}}`))
	})
	fx.router.POST("/anthropic/v1/messages/count_tokens", fx.handler.CountTokens)

	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages/count_tokens",
		strings.NewReader(`{"model":"deepseek-chat","messages":[]}`))
	req.Header.Set("x-api-key", fx.plainKey)
	rec := httptest.NewRecorder()
	fx.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "count_tokens not supported")
}
