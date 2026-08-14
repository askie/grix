// Package handler 的这个文件是大模型计费网关的 C端（普通用户自助）HTTP入口：
// 建/查/吊销自己的虚拟Key、查自己的钱包余额与流水。认 Grix 登录态(JWT)，
// 不直连 cmd/gateway 那套只认虚拟Key本身的鉴权体系。
package handler

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
)

func gatewayPageParams(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	return page, pageSize
}

// resolveGatewayBaseURLs 按当前这次请求实际打到的域名(CN grix.dhf.pub / 全球区 gb.grix.im /
// 本地开发)拼出网关的 Anthropic、OpenAI 两个协议入口地址——网关跟API服务共用同一个公网域名，
// 只是路径前缀不同(/anthropic、/openai)，不需要额外配置就能自动适配当前所在区域。
// 地址必须带 /v1：网关的真实端点是 /anthropic/v1/messages、/openai/v1/chat/completions，
// 而 connector 把 agent 请求里的 /v1/... 后缀重新挂到这个 base 上时会去掉重复的 /v1 前缀，
// 少一截就会被拼成 /anthropic/messages 打到 nginx 404（agent 侧表现为"模型不存在"）。
// 注意：带 /v1 的这个地址是**专给 connector 的路由拼接逻辑**用的，不是标准 SDK base_url——
// Anthropic/OpenAI 官方 SDK 的 base 应是 /anthropic、/openai（SDK 自己补 /v1/messages 等）。
// 别把这里下发的地址直接拿去当 SDK 的 base_url，否则会重复 /v1。
// 这个地址会下发给 connector 作为虚拟Key+Agent全部模型流量的转发目标，所以 X-Forwarded-Host
// 必须命中 server.allowed_web_origins 白名单才采信，否则退回 c.Request.Host——避免入口没
// 强制覆盖该头时把下发地址拼错，或被客户端塞入的伪造Host劫持。
// 兜底到 c.Request.Host 时还会经 localDevGatewayHost 把端口换成网关自己的监听端口，见其注释。
func resolveGatewayBaseURLs(c *gin.Context) (anthropicBaseURL, openaiBaseURL string) {
	proto := "http"
	if c.Request.TLS != nil {
		proto = "https"
	}
	if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		proto = forwarded
	}
	host := strings.TrimSpace(c.Request.Host)
	if forwardedHost := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); forwardedHost != "" && isAllowedGatewayHost(forwardedHost) {
		host = forwardedHost
	} else {
		host = localDevGatewayHost(host)
	}
	base := proto + "://" + host
	return base + "/anthropic/v1", base + "/openai/v1"
}

// localDevGatewayHost 只处理"没走可信 X-Forwarded-Host"的兜底路径。生产环境网关和API服务
// 共用同一个公网域名，靠反代按路径转发到两个不同的后端进程，走到这条兜底路径时 Host 本身
// 不带端口（标准80/443）。本地开发没有反代合并端口——api和gateway是两个独立端口的进程，
// Host 会带着 api 自己的端口（如127.0.0.1:27180）。Host 一旦带着显式端口，说明走的不是
// 生产反代那一套，必须换成网关自己监听的端口，否则下发地址会指回调用方（通常是api服务）
// 自己，打到网关没注册的路由拿一个裸404（agent侧表现为"模型不存在"）。
func localDevGatewayHost(host string) string {
	hostname, port, err := net.SplitHostPort(host)
	if err != nil || port == "" {
		return host
	}
	return net.JoinHostPort(hostname, strconv.Itoa(config.C.Gateway.Port))
}

// isAllowedGatewayHost 判断 host（可能带端口）是否命中 server.allowed_web_origins 白名单里
// 任一域名（该配置已经维护着 CN/全球区的合法公网域名，如 "https://grix.dhf.pub,https://gb.grix.im"）。
func isAllowedGatewayHost(host string) bool {
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	for _, item := range strings.Split(config.C.Server.AllowedWebOrigins, ",") {
		origin := strings.TrimSpace(item)
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || u.Hostname() == "" {
			continue
		}
		if strings.EqualFold(u.Hostname(), hostname) {
			return true
		}
	}
	return false
}

// GatewayConfigureAgentProvider handles POST /v1/gateway/agents/:agent_id/provider。
// 给用户名下一个托管Agent接入"Grix中转"：自动开一把专属虚拟Key + 通过connector现有
// WS通道自动配好路由/原生配置，用户不用手动填任何网址和Key。
func GatewayConfigureAgentProvider(c *gin.Context) {
	agentID, err := strconv.ParseInt(c.Param("agent_id"), 10, 64)
	if err != nil || agentID <= 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	anthropicBaseURL, openaiBaseURL := resolveGatewayBaseURLs(c)
	resend := c.Query("resend") == "true"
	data, ec := service.GatewayConfigureAgentProvider(userID, agentID, anthropicBaseURL, openaiBaseURL, resend)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// GatewayIssueAgentRelayCredential handles POST /v1/gateway/agents/:agent_id/relay-credential。
// 给用户名下一个托管Agent直接签发一把专属中转凭证，明文virtual_key连同两个协议入口地址一起
// 通过HTTP响应直接返回，不经 Redis/WS local_action 向 connector 广播——服务端只管鉴权和签发，
// 调用方（桌面端后端代理）自己负责拿着这把明文Key去调本地 Connector 的凭证应用接口。
// 桌面端“直连本地Connector”改造的新入口；旧的 GatewayConfigureAgentProvider（/provider）
// 原样保留给还没升级的旧客户端用。
func GatewayIssueAgentRelayCredential(c *gin.Context) {
	agentID, err := strconv.ParseInt(c.Param("agent_id"), 10, 64)
	if err != nil || agentID <= 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	anthropicBaseURL, openaiBaseURL := resolveGatewayBaseURLs(c)
	// body 可选：{"model":"..."}。旧客户端不带 body，等价于不指定模型。
	var req struct {
		Model string `json:"model"`
	}
	_ = c.ShouldBindJSON(&req)
	data, ec := service.GatewayIssueAgentRelayCredential(userID, agentID, anthropicBaseURL, openaiBaseURL, req.Model)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// GatewayListKeys handles GET /v1/gateway/keys.
func GatewayListKeys(c *gin.Context) {
	userID := middleware.GetUserID(c)
	data, ec := service.GatewayListKeys(userID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

type gatewayCreateKeyReq struct {
	Label string `json:"label"`
}

// GatewayCreateKey handles POST /v1/gateway/keys.
func GatewayCreateKey(c *gin.Context) {
	var req gatewayCreateKeyReq
	_ = c.ShouldBindJSON(&req)
	userID := middleware.GetUserID(c)
	data, ec := service.GatewayCreateKey(userID, service.GatewayCreateKeyReq{Label: req.Label})
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// GatewayRevokeKey handles POST /v1/gateway/keys/:id/revoke.
func GatewayRevokeKey(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || keyID <= 0 {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	if ec := service.GatewayRevokeKey(userID, keyID); ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// GatewayGetWallet handles GET /v1/gateway/wallet.
func GatewayGetWallet(c *gin.Context) {
	userID := middleware.GetUserID(c)
	data, ec := service.GatewayGetWallet(userID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{"wallet": data})
}

// GatewayListLedger handles GET /v1/gateway/wallet/ledger.
func GatewayListLedger(c *gin.Context) {
	userID := middleware.GetUserID(c)
	page, pageSize := gatewayPageParams(c)
	data, ec := service.GatewayListLedger(userID, page, pageSize)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// GatewayCreateTopup handles POST /v1/gateway/wallet/topup。
// 用户自助发起充值，返回收银台跳转；支付成功后自动入账到本人钱包。
func GatewayCreateTopup(c *gin.Context) {
	var req service.GatewayCreateTopupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, 10003, "参数错误")
		return
	}
	userID := middleware.GetUserID(c)
	data, ec := service.GatewayCreateTopup(userID, req)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}

// GatewayListTopups handles GET /v1/gateway/wallet/topups.
func GatewayListTopups(c *gin.Context) {
	userID := middleware.GetUserID(c)
	page, pageSize := gatewayPageParams(c)
	data, ec := service.GatewayListTopups(userID, page, pageSize)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, data)
}
