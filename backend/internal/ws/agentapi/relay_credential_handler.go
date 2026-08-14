package agentapi

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// RelayCredentialRequestPayload 是 connector 主动申请"Grix中转"凭证的上行请求。
// 与桌面端 HTTP 直签（POST /v1/gateway/agents/:id/relay-credential）不同：这里 agent 身份
// 直接取自本连接的认证结果（conn.ownerID/conn.agentID），调用方无法冒充别人的 agent；
// 明文 Key 只走"服务端 →(WSS)→ connector"这一段，不再经桌面端内存中转。
type RelayCredentialRequestPayload struct {
	// Model 原生配置类型（qwen/pi/hermes 等）必填，MITM 类型（claude/codex）可空，
	// 校验口径与 HTTP 直签完全一致（服务端 GatewayIssueAgentRelayCredential 强校验）。
	Model string `json:"model,omitempty"`
	// 两个网关协议入口地址由 connector 按自己的 ws_url 推导（ws 域名与 api 域名可能不同，
	// 服务端无法从 WS 升级请求推出正确的网关地址），服务端只做格式校验并原样带回。
	AnthropicBaseURL string `json:"anthropic_base_url,omitempty"`
	OpenAIBaseURL    string `json:"openai_base_url,omitempty"`
}

// RelayCredentialResultPayload 是 relay_credential_request 的应答（seq 关联）。
// status=ok 时携带一次性明文凭证；status=failed 时 error_code 为后端业务错误码（与 HTTP
// 直签同一套 errcode），error_msg 为可直接展示的中文原因。明文 Key 不落日志。
type RelayCredentialResultPayload struct {
	Status           string `json:"status"` // ok / failed
	ErrorCode        string `json:"error_code,omitempty"`
	ErrorMsg         string `json:"error_msg,omitempty"`
	APIKey           string `json:"api_key,omitempty"`
	AnthropicBaseURL string `json:"anthropic_base_url,omitempty"`
	OpenAIBaseURL    string `json:"openai_base_url,omitempty"`
	Model            string `json:"model,omitempty"`
}

// handleRelayCredentialRequest 处理 connector 的中转凭证申请：复用 HTTP 直签的同一套
// 签发逻辑（每次签发新 Key 并吊销该 Agent 旧 Key），应答走本连接 seq 关联——
// 天然避开旧 WS 广播"跨节点投递、发了可能收不到"的不可靠问题。
func (m *Manager) handleRelayCredentialRequest(conn *agentConn, pkt *protocol.Packet) {
	if conn == nil || pkt == nil {
		return
	}
	m.refreshAgentLease(conn)

	fail := func(code, msg string) {
		conn.sendPayload(protocol.CmdRelayCredentialResult, pkt.Seq, RelayCredentialResultPayload{
			Status:    "failed",
			ErrorCode: code,
			ErrorMsg:  msg,
		})
	}

	var payload RelayCredentialRequestPayload
	if len(pkt.Payload) > 0 {
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			conn.recordViolation()
			fail(strconv.Itoa(protocol.CodeInvalidPayload), "invalid payload")
			return
		}
	}
	if ec := validateRelayBaseURL(payload.AnthropicBaseURL); ec != "" {
		fail(strconv.Itoa(protocol.CodeInvalidPayload), "anthropic_base_url "+ec)
		return
	}
	if ec := validateRelayBaseURL(payload.OpenAIBaseURL); ec != "" {
		fail(strconv.Itoa(protocol.CodeInvalidPayload), "openai_base_url "+ec)
		return
	}

	resp, ec := service.GatewayIssueAgentRelayCredential(
		conn.ownerID, conn.agentID,
		payload.AnthropicBaseURL, payload.OpenAIBaseURL, payload.Model,
	)
	if ec != nil {
		logger.L.Warnf("relay_credential_request issue failed agent=%d owner=%d biz=%d msg=%s",
			conn.agentID, conn.ownerID, ec.BizCode, ec.Msg)
		fail(strconv.Itoa(ec.BizCode), ec.Msg)
		return
	}

	conn.sendPayload(protocol.CmdRelayCredentialResult, pkt.Seq, RelayCredentialResultPayload{
		Status:           "ok",
		APIKey:           resp.VirtualKey,
		AnthropicBaseURL: resp.AnthropicBaseURL,
		OpenAIBaseURL:    resp.OpenAIBaseURL,
		Model:            resp.RelayModel,
	})
}

// validateRelayBaseURL 校验 connector 自报的网关入口地址：允许空（该协议入口用不到），
// 非空必须是合法的 http/https 绝对 URL——它会原样写回 connector 的本地路由配置，
// 在源头拦住畸形值比让 connector 的 MITM 代理运行时炸要清楚。
func validateRelayBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "must be a valid http(s) URL"
	}
	return ""
}
