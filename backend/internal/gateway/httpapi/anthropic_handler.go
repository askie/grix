package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/askie/grix/backend/internal/gateway/credential"
	"github.com/askie/grix/backend/internal/gateway/upstream"
	"github.com/askie/grix/backend/internal/gateway/wallet"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

// deepseekAnthropicRoot 是 DeepSeek Anthropic 兼容端点的默认根（其下 /v1/messages 由适配器拼）。
const deepseekAnthropicRoot = "https://api.deepseek.com/anthropic"

// buildAnthropicUpstream 按厂商构造 Anthropic 协议转发适配器。
// 一期只接国内厂商的 Anthropic 兼容端点；DeepSeek 已验证可用。火山方舟的 Anthropic 端点待验证后补，
// 真·Anthropic 官方后续用同一适配器换 baseURL/Key 即可接入。
// inbound 是客户端入站请求头，只按白名单（FilterAnthropicPassthroughHeaders）透传
// anthropic-version / anthropic-beta；鉴权头由适配器注入真实厂商 Key，客户端的绝不转发。
func buildAnthropicUpstream(provider string, cred credential.Resolved, client *http.Client, inbound http.Header) (upstream.Upstream, error) {
	switch provider {
	case "deepseek":
		root := deepseekAnthropicRoot
		// cred.BaseURL 若配了 DeepSeek 根（OpenAI 口径），换算成其 Anthropic 子路径。
		if cred.BaseURL != "" {
			root = strings.TrimRight(cred.BaseURL, "/") + "/anthropic"
		}
		return upstream.NewAnthropic(cred.APIKey, root, client).
			WithPassthroughHeaders(upstream.FilterAnthropicPassthroughHeaders(inbound)), nil
	default:
		return nil, fmt.Errorf("provider %q not yet supported on anthropic protocol", provider)
	}
}

// writeAnthropicError 按 Anthropic 错误体格式回写：{"type":"error","error":{"type","message"}}。
func writeAnthropicError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"type": "error", "error": gin.H{"type": code, "message": message}})
}

// extractAnthropicToken 取虚拟Key：Anthropic 原生客户端用 x-api-key；也兼容 Authorization: Bearer。
func extractAnthropicToken(c *gin.Context) string {
	if k := strings.TrimSpace(c.GetHeader("x-api-key")); k != "" {
		return k
	}
	return extractBearerToken(c.GetHeader("Authorization"))
}

// anthropicProtocol 是 Anthropic 协议入口的差异注入。
func anthropicProtocol() protocol {
	return protocol{
		extractToken:  extractAnthropicToken,
		writeError:    writeAnthropicError,
		buildUpstream: buildAnthropicUpstream,
	}
}

// Messages 是 Anthropic 协议入口：POST /anthropic/v1/messages
// 服务 Claude Code 等 Anthropic 原生协议客户端；按 model 转发到真实厂商（一期国内厂商 Anthropic 兼容端点）。
func (h *Handler) Messages(c *gin.Context) {
	h.serve(c, anthropicProtocol())
}

// CountTokens 是 Anthropic count_tokens 入口：POST /anthropic/v1/messages/count_tokens。
// Claude Code 直连（direct relay）会在发消息前调它估算上下文占用。
// 复用虚拟 Key 鉴权与模型映射，但不产生生成用量、不计费、不做余额预检；
// 上游响应（含错误体）原样透传。请求失败的留痕走日志，不写计费失败流水。
func (h *Handler) CountTokens(c *gin.Context) {
	p := anthropicProtocol()
	token := p.extractToken(c)
	if token == "" {
		p.writeError(c, http.StatusUnauthorized, "missing_api_key", "missing api key")
		return
	}

	auth, err := h.Wallets.Authenticate(token)
	if err != nil {
		switch {
		case errors.Is(err, wallet.ErrKeyNotFound):
			p.writeError(c, http.StatusUnauthorized, "invalid_api_key", "virtual key not found")
		case errors.Is(err, wallet.ErrKeyRevoked):
			p.writeError(c, http.StatusUnauthorized, "revoked_api_key", "virtual key has been revoked")
		default:
			p.writeError(c, http.StatusInternalServerError, "internal_error", "authenticate failed")
		}
		return
	}

	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		p.writeError(c, http.StatusBadRequest, "invalid_request", "failed to read request body")
		return
	}

	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(rawBody, &probe); err != nil || probe.Model == "" {
		p.writeError(c, http.StatusBadRequest, "invalid_request", "request body is not valid JSON or model is missing")
		return
	}

	// 模型映射口径与 Messages 主链路一致：客户端怎么命名都行，按解析结果调上游。
	effectiveModel := h.resolveEffectiveModel(auth.Wallet.ID, probe.Model)
	provider := resolveProvider(effectiveModel)
	if provider == "" || !supportedProviders[provider] {
		p.writeError(c, http.StatusBadRequest, "unsupported_model", fmt.Sprintf("model %q is not routed to any upstream", effectiveModel))
		return
	}

	upstreamModel := canonicalModel(provider, effectiveModel)
	if upstreamModel != probe.Model {
		rewritten, err := rewriteBodyModel(rawBody, upstreamModel)
		if err != nil {
			p.writeError(c, http.StatusBadRequest, "invalid_request", "failed to rewrite model in request body")
			return
		}
		rawBody = rewritten
	}

	cred, err := h.Credentials.NextInference(provider)
	if err != nil {
		if errors.Is(err, credential.ErrNoCredential) {
			p.writeError(c, http.StatusServiceUnavailable, "no_upstream_credential", fmt.Sprintf("no enabled upstream credential for %s", provider))
		} else {
			p.writeError(c, http.StatusInternalServerError, "credential_error", "resolve upstream credential failed")
		}
		return
	}
	up, err := p.buildUpstream(provider, cred, h.HTTPClient, c.Request.Header)
	if err != nil {
		p.writeError(c, http.StatusBadRequest, "unsupported_model", err.Error())
		return
	}
	ct, ok := up.(upstream.CountTokensUpstream)
	if !ok {
		p.writeError(c, http.StatusBadRequest, "unsupported_model", fmt.Sprintf("provider %q does not support count_tokens", provider))
		return
	}

	requestID := strconv.FormatInt(snowflake.GenID(), 10)
	// 不涉及计费，客户端断开就随连接取消，无需 WithoutCancel 兜底。
	status, respBody, err := ct.CountTokens(c.Request.Context(), rawBody)
	if err != nil {
		logger.L.Warnf("gateway: count_tokens upstream failed, request_id=%s wallet=%d provider=%s model=%s err=%v",
			requestID, auth.Wallet.ID, provider, upstreamModel, err)
		p.writeError(c, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	c.Data(status, "application/json", respBody)
}
