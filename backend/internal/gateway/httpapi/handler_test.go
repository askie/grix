package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// router 按 cmd/gateway/main.go 的方式注册协议入口及其不带 /v1 的别名，
// 验证路由接线与各自错误体格式。
// 缺Key会在触达 Wallets 前返回，故此处用零依赖的 Handler{} 即可覆盖入口层。
func router() *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &Handler{}
	r := gin.New()
	r.POST("/openai/v1/chat/completions", h.ChatCompletions)
	r.POST("/anthropic/v1/messages", h.Messages)
	// 不带 /v1 的别名路由。
	r.POST("/openai/chat/completions", h.ChatCompletions)
	r.POST("/anthropic/messages", h.Messages)
	return r
}

func TestAnthropicEntryMissingKeyReturnsAnthropicError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", strings.NewReader(`{"model":"deepseek-chat"}`))
	router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	// Anthropic 错误体：{"type":"error","error":{"type","message"}}
	assert.Contains(t, rec.Body.String(), `"type":"error"`)
	assert.Contains(t, rec.Body.String(), `"type":"missing_api_key"`)
}

func TestOpenAIEntryMissingKeyReturnsOpenAIError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(`{"model":"deepseek-chat"}`))
	router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	// OpenAI 错误体：{"error":{"message","type"}}，不含顶层 "type":"error"
	assert.Contains(t, rec.Body.String(), `"error":{`)
	assert.Contains(t, rec.Body.String(), `"type":"missing_api_key"`)
	assert.NotContains(t, rec.Body.String(), `"type":"error"`)
}

// x-api-key 头能被 Anthropic 入口识别为 token 来源（有Key则不会走缺Key分支；
// 这里用无效Key会触达鉴权，故只断言 extractAnthropicToken 的取值优先级）。
// 不带 /v1 的别名路由应与带 /v1 的原路由返回同样的协议错误体。
func TestOpenAIEntryAliasWithoutV1(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/openai/chat/completions", strings.NewReader(`{"model":"deepseek-chat"}`))
	router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"error":{`)
	assert.Contains(t, rec.Body.String(), `"type":"missing_api_key"`)
	assert.NotContains(t, rec.Body.String(), `"type":"error"`)
}

func TestAnthropicEntryAliasWithoutV1(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/messages", strings.NewReader(`{"model":"deepseek-chat"}`))
	router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"type":"error"`)
	assert.Contains(t, rec.Body.String(), `"type":"missing_api_key"`)
}

func TestExtractAnthropicToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Request.Header.Set("x-api-key", "vk-from-xapikey")
	assert.Equal(t, "vk-from-xapikey", extractAnthropicToken(c))

	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c2.Request.Header.Set("Authorization", "Bearer vk-from-bearer")
	assert.Equal(t, "vk-from-bearer", extractAnthropicToken(c2))
}
