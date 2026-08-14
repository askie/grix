package httpapi

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/askie/grix/backend/internal/gateway/credential"
	"github.com/askie/grix/backend/internal/gateway/upstream"
)

// buildOpenAIUpstream 按厂商构造 OpenAI 协议转发适配器（服务 Codex 及一切 OpenAI 兼容客户端）。
// inbound 目前不透传任何客户端头（OpenAI 侧暂无经测试确认需要透传的 feature header）。
func buildOpenAIUpstream(provider string, cred credential.Resolved, client *http.Client, _ http.Header) (upstream.Upstream, error) {
	switch provider {
	case "deepseek":
		return upstream.NewDeepSeek(cred.APIKey, cred.BaseURL, client), nil
	case "volcano_ark":
		return upstream.NewVolcanoArk(cred.APIKey, cred.BaseURL, client), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", provider)
	}
}

// writeOpenAIError 按 OpenAI 错误体格式回写：{"error":{"message","type"}}。
func writeOpenAIError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"message": message, "type": code}})
}

// openaiProtocol 是 OpenAI 协议入口的差异注入：Bearer 取Key、OpenAI 错误体、OpenAI 转发适配器。
func openaiProtocol() protocol {
	return protocol{
		extractToken:  func(c *gin.Context) string { return extractBearerToken(c.GetHeader("Authorization")) },
		writeError:    writeOpenAIError,
		buildUpstream: buildOpenAIUpstream,
	}
}

// ChatCompletions 是 OpenAI 协议入口：POST /openai/v1/chat/completions
// 服务 Codex、以及一切 OpenAI 兼容客户端；按 model 转发到真实厂商。
func (h *Handler) ChatCompletions(c *gin.Context) {
	h.serve(c, openaiProtocol())
}

// Responses 是 OpenAI Responses API 入口：POST /openai/v1/responses。
// Codex / 新版 OpenAI SDK 走这条；DeepSeek 走原生 Responses，
// 其他上游按能力回落到 Chat Completions 桥接。
func (h *Handler) Responses(c *gin.Context) {
	h.serveResponses(c)
}
