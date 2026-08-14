// Package httpapi 是网关对外暴露的HTTP协议入口层。
// 只认虚拟Key，不查Grix登录态/Session/JWT。
// 按调用方原生协议分组暴露入口（OpenAI /openai、Anthropic /anthropic），
// 鉴权→余额预检→转发→按真实用量结算这套"money-critical"主链路协议无关，收口在 serve()。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/askie/grix/backend/internal/gateway/credential"
	"github.com/askie/grix/backend/internal/gateway/modelroute"
	"github.com/askie/grix/backend/internal/gateway/pricing"
	"github.com/askie/grix/backend/internal/gateway/relay"
	"github.com/askie/grix/backend/internal/gateway/responsesbridge"
	"github.com/askie/grix/backend/internal/gateway/upstream"
	"github.com/askie/grix/backend/internal/gateway/wallet"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

type Handler struct {
	Wallets     *wallet.Service
	Pricing     *pricing.Service
	Credentials *credential.Service // 上游厂商官方Key由后台动态管理，转发时按厂商实时解密取用
	Relay       *relay.Service      // "Grix中转"的用户级模型设置：映射表 + 兜底模型
	HTTPClient  *http.Client        // 转发上游共享 client（复用连接池）；为空时适配器自建
}

// protocol 抽出各协议入口的差异点：如何取虚拟Key、如何回写错误体、按厂商构造哪种转发适配器。
// 其余（鉴权、余额、计价、结算）完全共用，保证钱账逻辑只有一份、不随协议分叉。
// inbound 是客户端入站请求头，各协议的 buildUpstream 自行按白名单决定透传哪些
// （如 Anthropic 只透传 anthropic-version/anthropic-beta），绝不整体转发。
type protocol struct {
	extractToken  func(c *gin.Context) string
	writeError    func(c *gin.Context, status int, code, message string)
	buildUpstream func(provider string, cred credential.Resolved, client *http.Client, inbound http.Header) (upstream.Upstream, error)
}

// 模型→厂商的路由知识收口在 modelroute 包：C端"可用模型清单"必须用同一份判定，
// 否则清单里会出现网关服务不了的模型。这里只留薄别名，少改调用点。
func resolveProvider(model string) string       { return modelroute.ResolveProvider(model) }
func canonicalModel(p, requested string) string { return modelroute.CanonicalModel(p, requested) }

var supportedProviders = modelroute.SupportedProviders

// resolveEffectiveModel 解析这次请求真正要调用（也是要计费）的模型，是"Grix中转"的核心：
//
//  1. 用户映射表命中            → 用映射的目标模型
//  2. 请求的模型本身后端就能服务  → 直接用它（用户可以绕过映射直接点名后端模型）
//  3. 都不是                    → 用该用户的兜底模型
//
// 第 3 条是链路能长期活着的保证：Claude/Codex 随时会发新模型名，网关的白名单必然不认，
// 没有兜底就等于上游一发版这边就 400。
func (h *Handler) resolveEffectiveModel(walletID int64, requested string) string {
	if h.Relay == nil {
		return requested
	}
	settings, err := h.Relay.Get(walletID)
	if err != nil {
		// 设置读不出来（库故障）：按请求原样走，该报错就报错。
		// 绝不拿一个猜出来的模型去花用户的钱。
		logger.L.Errorf("gateway: load relay settings failed, wallet=%d err=%v", walletID, err)
		return requested
	}
	if mapped, ok := settings.ModelMap[requested]; ok && mapped != "" {
		return mapped
	}
	if h.isServable(requested) {
		return requested
	}
	// 落兜底必须留痕：流水表只记计费模型，客户端实际发的名字不留档的话，
	// "用户配了 claude-sonnet-4-5 的映射、客户端发的却是带日期后缀的全名 → 静默走兜底"
	// 这类问题没人能排查，用户也无从知道该配哪个名字。
	logger.L.Infof("gateway: model fallback, wallet=%d requested=%q -> default=%q", walletID, requested, settings.DefaultModel)
	return settings.DefaultModel
}

// isServable 判断一个模型名后端能否直接服务：既能路由到厂商，价目表里又有基准价。
func (h *Handler) isServable(m string) bool {
	if !modelroute.Routable(m) {
		return false
	}
	provider := modelroute.ResolveProvider(m)
	_, err := h.Pricing.DefaultRule(provider, modelroute.CanonicalModel(provider, m))
	return err == nil
}

// rewriteBodyModel 只替换请求体的 model 字段，其余字段（messages/stream/tools/...）原样保留。
// 用 json.RawMessage 只解顶层：中转主路径几乎每次请求都要改写，长上下文时 body 是 MB 级的，
// 深度反序列化再重编码既费 CPU，还会把数字失真成 float64。顶层浅解 + 其余字段按原字节透传
// （Marshal 对 RawMessage 仍会做 HTML 转义压缩，但那只改表示、不改语义）。
func rewriteBodyModel(raw []byte, model string) ([]byte, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	quoted, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	body["model"] = quoted
	return json.Marshal(body)
}

func extractBearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

// serve 是所有协议入口共用的主链路：鉴权 → 余额预检 → 读体探模型 → 路由厂商 → 计价前置 →
// 取上游凭据 → 构造适配器转发 → 按真实用量结算。差异点全部经 protocol 注入。
func (h *Handler) serve(c *gin.Context, p protocol) {
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

	if err := h.Wallets.PreflightCheck(auth.Wallet.ID); err != nil {
		p.writeError(c, http.StatusPaymentRequired, "insufficient_balance", "wallet balance is insufficient")
		return
	}

	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		p.writeError(c, http.StatusBadRequest, "invalid_request", "failed to read request body")
		return
	}

	var probe struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(rawBody, &probe); err != nil {
		p.writeError(c, http.StatusBadRequest, "invalid_request", "request body is not valid JSON")
		return
	}

	// 解析这次请求真正要调用（也是要计费）的模型。客户端把模型叫什么名字后端不关心，
	// 一律按解析结果调用和结算。未知模型（含 Claude/Codex 未来发布的任何新模型）落到兜底模型，
	// 链路不会因为上游发版而中断。
	effectiveModel := h.resolveEffectiveModel(auth.Wallet.ID, probe.Model)

	provider := resolveProvider(effectiveModel)
	if provider == "" || !supportedProviders[provider] {
		// 兜底之后仍然路由不出去，只可能是兜底模型本身被下架或配错了——这是配置事故，
		// 必须吼出来，否则表现成"用户莫名其妙用不了"，没人知道该去改哪。
		logger.L.Errorf("gateway: fallback model unroutable, wallet=%d requested=%q effective=%q", auth.Wallet.ID, probe.Model, effectiveModel)
		p.writeError(c, http.StatusBadRequest, "unsupported_model", fmt.Sprintf("model %q is not routed to any upstream", effectiveModel))
		return
	}

	upstreamModel := canonicalModel(provider, effectiveModel)
	billingModel := upstreamModel
	// 前置只校验"有没有全天兜底价"（每个模型必须有一条基准价）；分时档只在特定时段覆盖它，
	// 具体这次用哪档价等请求做完、按完成时刻再选。
	if _, err := h.Pricing.DefaultRule(provider, billingModel); err != nil {
		p.writeError(c, http.StatusBadRequest, "unpriced_model", fmt.Sprintf("no active base pricing rule for %s/%s", provider, billingModel))
		return
	}

	// 把请求体里客户端侧的模型名换成解析后的真实模型再转发。
	// 必须发生在转发之前——上游收到的、我们计费的，必须是同一个模型。
	if upstreamModel != probe.Model {
		rewritten, err := rewriteBodyModel(rawBody, upstreamModel)
		if err != nil {
			p.writeError(c, http.StatusBadRequest, "invalid_request", "failed to rewrite model in request body")
			return
		}
		rawBody = rewritten
	}

	// 上游官方Key由后台动态管理，这里按厂商实时取一把启用中的凭据（多把则轮询分流）。
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

	requestID := strconv.FormatInt(snowflake.GenID(), 10)

	// 转发给厂商的请求必须跟"客户端到网关"这条连接的生命周期解耦：用户若在最后一块带usage的数据到达前断线，
	// 不能让厂商侧请求跟着被取消——否则读不到usage、判定不扣费，可厂商那边token已真实生成并计费，形成"卡点断线白嫖"。
	// WithoutCancel 让厂商响应无论客户端在不在线都完整读完拿到准确usage再结算；写回给已断开客户端的字节自然静默失败，不影响计费。
	// （厂商HTTP请求本身有 client.Timeout 兜底，不会无限挂起。）
	upstreamCtx := context.WithoutCancel(c.Request.Context())
	actualModel, usage, err := up.Forward(upstreamCtx, rawBody, probe.Stream, c.Writer)
	if err != nil {
		if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
			logger.L.Errorf("gateway: RecordFailure(upstream_error) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
		}
		// 上游adapter在能确定的错误场景下已经把响应写给客户端了；这里只兜底网络层面还没写任何响应的情况。
		if !c.Writer.Written() {
			p.writeError(c, http.StatusBadGateway, "upstream_error", err.Error())
		}
		return
	}
	if usage == nil {
		// 流结束却没拿到usage：厂商可能已扣费但我们无从计价，留痕待人工核对。
		logger.L.Warnf("gateway: upstream returned no usage, request_id=%s wallet=%d provider=%s model=%s", requestID, auth.Wallet.ID, provider, billingModel)
		if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
			logger.L.Errorf("gateway: RecordFailure(no_usage) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
		}
		return
	}

	// requestBillingModel 是按"我们发给上游的模型"收敛出的计价名——计价前置已校验过它必有基准价。
	// 结算优先按上游回显名（actualModel）计价；但厂商爱回显别的名字（豆包 Ark 常回带日期后缀的
	// 版本名），回显名查不到价目时**必须回落到 requestBillingModel 收钱**，绝不白嫖：
	// token 是真实消耗了的，宁可按我们发出去的那个模型的价目近似收，也不能记一笔失败流水放走。
	requestBillingModel := billingModel
	billingModel = canonicalModel(provider, actualModel)
	// 按"请求完成时刻"选分时价（DeepSeek错峰/高峰就是按完成时刻判定的），这里用 now 即完成时刻。
	cost, _, err := h.Pricing.Calculate(provider, billingModel, *usage, time.Now())
	if err != nil && billingModel != requestBillingModel {
		logger.L.Warnf("gateway: no pricing for upstream-echoed model, refalling to request model, request_id=%s wallet=%d provider=%s echoed=%q request=%q",
			requestID, auth.Wallet.ID, provider, billingModel, requestBillingModel)
		billingModel = requestBillingModel
		cost, _, err = h.Pricing.Calculate(provider, billingModel, *usage, time.Now())
	}
	if err != nil {
		// 连发出模型的价目都算不出来（极端：结算瞬间规则被退休/库故障）——
		// 已经把响应给了用户、真实费用也已经在厂商那边发生了，只是我们这边一时算不出该收多少。
		// 不能假装没发生，必须留痕以便人工介入核对损失，绝不能悄悄吞掉这笔账。
		logger.L.Errorf("gateway: cost calculation failed after upstream served, request_id=%s wallet=%d provider=%s model=%s err=%v",
			requestID, auth.Wallet.ID, provider, billingModel, err)
		if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
			logger.L.Errorf("gateway: RecordFailure(calc_failed) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
		}
		return
	}

	if _, err := h.Wallets.Settle(wallet.SettleRequest{
		WalletID:         auth.Wallet.ID,
		VirtualKeyID:     auth.VirtualKey.ID,
		RequestID:        requestID,
		Provider:         provider,
		Model:            billingModel,
		PromptTokens:     usage.CachedTokens + usage.UncachedTokens,
		CachedTokens:     usage.CachedTokens,
		CompletionTokens: usage.CompletionTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		Cost:             cost,
	}); err != nil {
		// 扣款事务失败：响应已经发给用户、厂商已真实扣费，但我们没扣到钱。这是实打实的钱损，
		// 必须高优先级留痕（含算出的应扣金额），供对账/人工追账，绝不能静默丢弃。
		logger.L.Errorf("gateway: SETTLE FAILED (money lost), request_id=%s wallet=%d provider=%s model=%s cost=%s err=%v",
			requestID, auth.Wallet.ID, provider, billingModel, cost.String(), err)
		if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
			logger.L.Errorf("gateway: RecordFailure(settle_failed) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
		}
	}
}

// serveResponses 是 /openai/v1/responses 专用主链路：鉴权/计价与 serve() 同口径。
// 原生支持 Responses 的厂商直接转发；其他厂商额外做 Responses↔Chat Completions 协议转接。
func (h *Handler) serveResponses(c *gin.Context) {
	p := openaiProtocol()
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

	if err := h.Wallets.PreflightCheck(auth.Wallet.ID); err != nil {
		p.writeError(c, http.StatusPaymentRequired, "insufficient_balance", "wallet balance is insufficient")
		return
	}

	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		p.writeError(c, http.StatusBadRequest, "invalid_request", "failed to read request body")
		return
	}

	requestedModel, stream, err := probeResponsesRequest(rawBody)
	if err != nil {
		p.writeError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	effectiveModel := h.resolveEffectiveModel(auth.Wallet.ID, requestedModel)
	provider := resolveProvider(effectiveModel)
	if provider == "" || !supportedProviders[provider] {
		logger.L.Errorf("gateway: responses fallback model unroutable, wallet=%d requested=%q effective=%q", auth.Wallet.ID, requestedModel, effectiveModel)
		p.writeError(c, http.StatusBadRequest, "unsupported_model", fmt.Sprintf("model %q is not routed to any upstream", effectiveModel))
		return
	}

	upstreamModel := canonicalModel(provider, effectiveModel)
	billingModel := upstreamModel
	if _, err := h.Pricing.DefaultRule(provider, billingModel); err != nil {
		p.writeError(c, http.StatusBadRequest, "unpriced_model", fmt.Sprintf("no active base pricing rule for %s/%s", provider, billingModel))
		return
	}

	if upstreamModel != requestedModel {
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

	requestID := strconv.FormatInt(snowflake.GenID(), 10)
	responseID := "resp_" + requestID
	upstreamCtx := context.WithoutCancel(c.Request.Context())

	if responsesUpstream, ok := up.(upstream.ResponsesUpstream); ok {
		actualModel, usage, err := responsesUpstream.ForwardResponses(upstreamCtx, rawBody, stream, c.Writer)
		if err != nil {
			if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
				logger.L.Errorf("gateway: RecordFailure(responses_upstream_error) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
			}
			if !c.Writer.Written() {
				p.writeError(c, http.StatusBadGateway, "upstream_error", err.Error())
			}
			return
		}
		if usage == nil {
			logger.L.Warnf("gateway: native responses upstream returned no usage, request_id=%s wallet=%d provider=%s model=%s", requestID, auth.Wallet.ID, provider, billingModel)
			if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
				logger.L.Errorf("gateway: RecordFailure(no_usage) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
			}
			return
		}

		h.settleAfterUpstream(auth, requestID, provider, billingModel, actualModel, usage)
		return
	}

	chatBody, _, stream, err := responsesbridge.ConvertRequest(rawBody)
	if err != nil {
		p.writeError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	var (
		actualModel string
		usage       *pricing.Usage
	)

	if stream {
		bridge := responsesbridge.NewStreamWriter(c.Writer, responseID, effectiveModel)
		actualModel, usage, err = up.Forward(upstreamCtx, chatBody, true, bridge)
		// Forward 成功才补尾；Finish/写客户端失败只打日志，绝不覆盖 err、绝不挡结算
		// （与 serve()「客户端断线不挡扣费」同口径；Responses SSE 是 Codex 主路径）。
		if err == nil {
			if ferr := bridge.Finish(); ferr != nil {
				logger.L.Warnf("gateway: responses stream finish failed, request_id=%s wallet=%d err=%v",
					requestID, auth.Wallet.ID, ferr)
			}
		}
	} else {
		buf := responsesbridge.NewBufferWriter()
		actualModel, usage, err = up.Forward(upstreamCtx, chatBody, false, buf)
		if err != nil {
			// 上游适配器已把错误 JSON 写进 buffer；转成 OpenAI error 体回给客户端。
			if buf.Body.Len() > 0 {
				c.Data(buf.Code, "application/json", buf.Body.Bytes())
			} else if !c.Writer.Written() {
				p.writeError(c, http.StatusBadGateway, "upstream_error", err.Error())
			}
			if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
				logger.L.Errorf("gateway: RecordFailure(upstream_error) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
			}
			return
		}
		converted, convErr := responsesbridge.ConvertResponse(buf.Body.Bytes(), responseID, effectiveModel)
		if convErr != nil {
			p.writeError(c, http.StatusBadGateway, "upstream_error", convErr.Error())
			if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
				logger.L.Errorf("gateway: RecordFailure(convert_failed) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
			}
			return
		}
		c.Data(http.StatusOK, "application/json", converted)
	}

	if err != nil {
		if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
			logger.L.Errorf("gateway: RecordFailure(upstream_error) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
		}
		if !c.Writer.Written() {
			p.writeError(c, http.StatusBadGateway, "upstream_error", err.Error())
		}
		return
	}
	if usage == nil {
		logger.L.Warnf("gateway: responses upstream returned no usage, request_id=%s wallet=%d provider=%s model=%s", requestID, auth.Wallet.ID, provider, billingModel)
		if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
			logger.L.Errorf("gateway: RecordFailure(no_usage) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
		}
		return
	}

	h.settleAfterUpstream(auth, requestID, provider, billingModel, actualModel, usage)
}

func probeResponsesRequest(raw []byte) (model string, stream bool, err error) {
	var probe struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", false, fmt.Errorf("decode responses request: %w", err)
	}
	if probe.Model == "" {
		return "", false, fmt.Errorf("model is required")
	}
	return probe.Model, probe.Stream, nil
}

func (h *Handler) settleAfterUpstream(auth *wallet.AuthResult, requestID, provider, billingModel, actualModel string, usage *pricing.Usage) {
	requestBillingModel := billingModel
	billingModel = canonicalModel(provider, actualModel)
	cost, _, err := h.Pricing.Calculate(provider, billingModel, *usage, time.Now())
	if err != nil && billingModel != requestBillingModel {
		logger.L.Warnf("gateway: no pricing for upstream-echoed model, refalling to request model, request_id=%s wallet=%d provider=%s echoed=%q request=%q",
			requestID, auth.Wallet.ID, provider, billingModel, requestBillingModel)
		billingModel = requestBillingModel
		cost, _, err = h.Pricing.Calculate(provider, billingModel, *usage, time.Now())
	}
	if err != nil {
		logger.L.Errorf("gateway: cost calculation failed after upstream served, request_id=%s wallet=%d provider=%s model=%s err=%v",
			requestID, auth.Wallet.ID, provider, billingModel, err)
		if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
			logger.L.Errorf("gateway: RecordFailure(calc_failed) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
		}
		return
	}

	if _, err := h.Wallets.Settle(wallet.SettleRequest{
		WalletID:         auth.Wallet.ID,
		VirtualKeyID:     auth.VirtualKey.ID,
		RequestID:        requestID,
		Provider:         provider,
		Model:            billingModel,
		PromptTokens:     usage.CachedTokens + usage.UncachedTokens,
		CachedTokens:     usage.CachedTokens,
		CompletionTokens: usage.CompletionTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		Cost:             cost,
	}); err != nil {
		logger.L.Errorf("gateway: SETTLE FAILED (money lost), request_id=%s wallet=%d provider=%s model=%s cost=%s err=%v",
			requestID, auth.Wallet.ID, provider, billingModel, cost.String(), err)
		if ferr := h.Wallets.RecordFailure(auth.Wallet.ID, auth.VirtualKey.ID, requestID, provider, billingModel); ferr != nil {
			logger.L.Errorf("gateway: RecordFailure(settle_failed) write failed, request_id=%s wallet=%d err=%v", requestID, auth.Wallet.ID, ferr)
		}
	}
}
