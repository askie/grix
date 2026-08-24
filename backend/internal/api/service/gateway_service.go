// Package service 的这个文件是大模型计费网关的 C端（普通用户自助）接口层。
// 只负责鉴权归属校验 + 请求参数整理，实际钱包/Key业务逻辑全部转调
// internal/gateway/wallet 现成的 Service，不重复实现一遍计费规则。
package service

import (
	"encoding/json"
	"errors"
	"log"
	"strings"

	"gorm.io/gorm"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/gateway/provisioning"
	"github.com/askie/grix/backend/internal/gateway/wallet"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
)

func gatewayWalletService() *wallet.Service {
	return wallet.New(store.DB)
}

// ensureGatewayWallet 取当前用户的网关钱包，不存在则自动开一个（余额0）。
func ensureGatewayWallet(ownerID int64) (*model.GatewayWallet, *errcode.ErrCode) {
	w, err := gatewayWalletService().CreateWallet(ownerID)
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	return w, nil
}

func normalizeGatewayPagination(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

// GatewayListKeysResp 是当前用户名下所有虚拟Key（不含明文）。
type GatewayListKeysResp struct {
	Items []model.GatewayVirtualKey `json:"items"`
}

// GatewayListKeys 列出当前用户名下所有虚拟Key。
func GatewayListKeys(ownerID int64) (*GatewayListKeysResp, *errcode.ErrCode) {
	w, ec := ensureGatewayWallet(ownerID)
	if ec != nil {
		return nil, ec
	}
	keys, err := gatewayWalletService().ListVirtualKeys(w.ID)
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	return &GatewayListKeysResp{Items: keys}, nil
}

// GatewayCreateKeyReq 是创建虚拟Key的请求。
type GatewayCreateKeyReq struct {
	Label string `json:"label"`
}

// GatewayCreateKeyResp 里的 VirtualKey 是明文，只在这一次返回，之后系统只存哈希。
type GatewayCreateKeyResp struct {
	VirtualKey string                  `json:"virtual_key"`
	Key        model.GatewayVirtualKey `json:"key"`
}

// GatewayCreateKey 给当前用户自助创建一把虚拟Key。
func GatewayCreateKey(ownerID int64, req GatewayCreateKeyReq) (*GatewayCreateKeyResp, *errcode.ErrCode) {
	w, ec := ensureGatewayWallet(ownerID)
	if ec != nil {
		return nil, ec
	}
	plain, key, err := gatewayWalletService().IssueVirtualKey(w.ID, req.Label)
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	return &GatewayCreateKeyResp{VirtualKey: plain, Key: *key}, nil
}

// GatewayRevokeKey 吊销当前用户自己名下的一把虚拟Key；不允许吊销别人的Key。
func GatewayRevokeKey(ownerID, keyID int64) *errcode.ErrCode {
	w, ec := ensureGatewayWallet(ownerID)
	if ec != nil {
		return ec
	}
	key, err := gatewayWalletService().GetVirtualKey(keyID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &errcode.ErrGatewayKeyNotFound
		}
		return &errcode.ErrInternal
	}
	if key.WalletID != w.ID {
		return &errcode.ErrGatewayKeyForbidden
	}
	if err := gatewayWalletService().RevokeVirtualKey(keyID); err != nil {
		return &errcode.ErrInternal
	}
	return nil
}

// GatewayGetWallet 查当前用户自己的网关钱包（不存在则自动开一个）。
func GatewayGetWallet(ownerID int64) (*model.GatewayWallet, *errcode.ErrCode) {
	return ensureGatewayWallet(ownerID)
}

// GatewayListLedgerResp 是分页的消费流水。
type GatewayListLedgerResp struct {
	Items    []model.GatewayLedgerEntry `json:"items"`
	Total    int64                      `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"page_size"`
}

// GatewayListLedger 分页查当前用户自己的消费流水。
func GatewayListLedger(ownerID int64, page, pageSize int) (*GatewayListLedgerResp, *errcode.ErrCode) {
	w, ec := ensureGatewayWallet(ownerID)
	if ec != nil {
		return nil, ec
	}
	page, pageSize = normalizeGatewayPagination(page, pageSize)
	items, total, err := gatewayWalletService().ListLedgerEntries(w.ID, page, pageSize)
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	return &GatewayListLedgerResp{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// GatewayListTopupsResp 是分页的充值记录。
type GatewayListTopupsResp struct {
	Items    []model.GatewayTopupRecord `json:"items"`
	Total    int64                      `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"page_size"`
}

// GatewayListTopups 分页查当前用户自己的充值记录（充值本身目前仍由管理员在塘主后台代充）。
func GatewayListTopups(ownerID int64, page, pageSize int) (*GatewayListTopupsResp, *errcode.ErrCode) {
	w, ec := ensureGatewayWallet(ownerID)
	if ec != nil {
		return nil, ec
	}
	page, pageSize = normalizeGatewayPagination(page, pageSize)
	items, total, err := gatewayWalletService().ListTopupRecords(w.ID, page, pageSize)
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	return &GatewayListTopupsResp{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// gatewaySupportedAgentClientTypes 是目前"Grix中转"接得通的托管Agent类型，逐项与
// grix-connector 的支持清单对齐：
//   - Claude/Codex：MITM 接管官方域名（relay-hosts.ts 的 RELAY_HOSTS_BY_CLIENT_TYPE）；
//   - Qwen/Kimi/Reasonix/DeepSeek/CodeWhale/Hermes/Pi/OpenCode：原生配置直连网关
//     （provider-env.ts 的 DIRECT_PROVIDER_CLIENT_TYPES）；
//   - Kiro：本地 CW↔Anthropic 协议代理 + 端点覆盖（KIRO_CW_RELAY_CLIENT_TYPES）；
//   - OpenClaw：连接器以插件身份跑在 OpenClaw 宿主里，没有 spawn 注入这一步，改由
//     插件收 configure_gateway_provider local action 后 patch 宿主配置写 grix provider
//     （openclaw/client.ts 注册、openclaw/local-actions.ts 承接）。注意这条路只存在于
//     插件宿主模式；若某台 connector 反过来把 openclaw 当被托管 CLI 拉起，它既不在
//     MITM 名单也不在 supportsNonMitmRelayConfig 里，开关会以 UNSUPPORTED_CLIENT_TYPE
//     被连接器当场拒绝。那是 fail loud 的明确失败，不会留下半配置，因此这里按"插件
//     宿主可用"登记，不为少数托管场景把绝大多数用户挡在门外。
// 其余类型（Gemini/Cursor/OpenHuman/Copilot 等）绑定自己账号或不支持自定义端点，
// connector 侧同样没有接管实现，接不了。
// 后端少登记一个类型，用户在模型设置里就看不到该 Agent（前端只渲染 supported 项），
// connector 支持也用不上；因此改这张表必须与 connector 清单同步核对。
var gatewaySupportedAgentClientTypes = map[string]bool{
	model.AgentClientTypeClaude:    true,
	model.AgentClientTypeCodex:     true,
	model.AgentClientTypeQwen:      true,
	model.AgentClientTypeKimi:      true,
	model.AgentClientTypeReasonix:  true,
	model.AgentClientTypeDeepSeek:  true,
	model.AgentClientTypeOpenCode:  true,
	model.AgentClientTypeCodeWhale: true,
	model.AgentClientTypePi:        true,
	model.AgentClientTypeHermes:    true,
	model.AgentClientTypeKiro:      true,
	model.AgentClientTypeOpenClaw:  true,
}

// gatewayNativeProviderClientTypes 里的类型不走 MITM 接管，而是把网关端点写进 CLI 自己的
// 原生配置（env / 进程配置 / 协议代理），配置结构里模型名是必填字段——启用中转（签发凭证）
// 时必须带 model，否则 connector 直接以 MISSING_MODEL 拒绝，留下"接口成功、聊天必失败"的
// 静默半配置。口径与 grix-connector 一致：supportsNonMitmRelayConfig
// （DIRECT_PROVIDER_CLIENT_TYPES ∪ KIRO_CW_RELAY_CLIENT_TYPES）再加 OpenClaw——
// 它的 local action 同样以 missing_model 拒绝不带模型的开启请求。
var gatewayNativeProviderClientTypes = map[string]bool{
	model.AgentClientTypeQwen:      true,
	model.AgentClientTypeKimi:      true,
	model.AgentClientTypeReasonix:  true,
	model.AgentClientTypeDeepSeek:  true,
	model.AgentClientTypeOpenCode:  true,
	model.AgentClientTypeCodeWhale: true,
	model.AgentClientTypePi:        true,
	model.AgentClientTypeHermes:    true,
	model.AgentClientTypeKiro:      true,
	model.AgentClientTypeOpenClaw:  true,
}

// GatewayConfigureAgentProviderResp 是"给托管Agent配置Grix中转"的结果。
type GatewayConfigureAgentProviderResp struct {
	// AlreadyConfigured=true 表示这个Agent之前已经配过一把专属虚拟Key，本次是幂等跳过
	// （虚拟Key明文只有创建那一刻能拿到，找不回旧的，所以不重复发一把新的）。
	// resend=true 时即使已经配过也不会短路，见 GatewayConfigureAgentProvider 的 resend 说明。
	AlreadyConfigured bool `json:"already_configured"`
}

// GatewayConfigureAgentProvider 把用户名下的一个托管Agent接入"Grix中转"：
// 给该Agent开一把专属虚拟Key（已配过就幂等跳过，不重复发），再通过 grix-connector
// 已有的WS通道下发配置，connector按该Agent类型自动完成MITM路由或原生配置写入，
// 用户不需要手动填任何网址和Key。anthropicBaseURL/openaiBaseURL 由调用方按当前请求
// 所在的域名(CN/全球区)拼好传入，本函数不关心具体是哪个区域。
//
// resend=true 用于客户端已经问过 connector、确认它本地没有这把Key的重试场景
// （下发是 WS 广播、不保证送达：如果上次发Key那一刻这个Agent恰好没有活跃连接，
// 消息会静默丢失，但Key在库里已经是active，普通幂等跳过会导致永远无法补发）。
// 这种情况下即使库里已有active Key也要作废重发一把，重新走一次下发。
func GatewayConfigureAgentProvider(ownerID, agentID int64, anthropicBaseURL, openaiBaseURL string, resend bool) (*GatewayConfigureAgentProviderResp, *errcode.ErrCode) {
	agent, ec := gatewayResolveOwnedSupportedAgent(ownerID, agentID)
	if ec != nil {
		return nil, ec
	}

	w, ec := ensureGatewayWallet(ownerID)
	if ec != nil {
		return nil, ec
	}

	svc := gatewayWalletService()
	existing, err := svc.GetActiveVirtualKeyByAgent(w.ID, agentID)
	hasExisting := err == nil
	if !hasExisting && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &errcode.ErrInternal
	}

	// 旧的 provider 广播接口没有 model 入参。这里取用户已保存的默认模型（或 state
	// 已明确指定的 model），把它作为这把 Key 的快照并用于 direct_relay capability。
	// 这不是让连接器猜模型；默认模型是钱包设置中唯一的用户配置来源。
	relayModel, ec := gatewayConfiguredRelayModel(w.ID, agentID)
	if ec != nil {
		return nil, ec
	}
	directRelay, err := marshalDirectRelayCapability(
		buildDirectRelayCapability(agent.AgentClientType, relayModel, anthropicBaseURL, openaiBaseURL),
	)
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	// 已经由旧实现下发的 Key 没有 relay_model，也就不可能带有 direct_relay。
	// direct capability 已可用时，将这类存量 Key 视为一次兼容升级并重签；否则
	// 幂等短路会让用户即使再次点击“启用中转”也永远留在 cc-switch/MITM 路径。
	upgradeLegacyDirectRelay := hasExisting && existing.RelayModel == "" && directRelaySupported(directRelay)
	if hasExisting && !resend && !upgradeLegacyDirectRelay {
		return &GatewayConfigureAgentProviderResp{AlreadyConfigured: true}, nil
	}

	plainKey, key, err := svc.IssueVirtualKeyForAgent(w.ID, agentID, agent.AgentName, relayModel)
	if err != nil {
		return nil, &errcode.ErrInternal
	}

	if err := provisioning.PublishConfigureGatewayProvider(provisioning.GatewayProviderConfig{
		AgentID:          agentID,
		APIKey:           plainKey,
		AnthropicBaseURL: anthropicBaseURL,
		OpenAIBaseURL:    openaiBaseURL,
		Model:            relayModel,
		DirectRelay:      directRelay,
	}); err != nil {
		// 广播失败说明 connector 没真正拿到这把明文Key，必须把刚发的Key吊销掉，
		// 否则下次重试会因为查到"已有active Key"而幂等跳过，永远无法重新广播。
		// 旧Key（resend场景下的那把）此时还没动，原样保留——resend失败不能把仍在
		// 正常工作的旧Key也搭进去，让agent落到"一把Key都没有"的更差状态。
		if revokeErr := svc.RevokeVirtualKey(key.ID); revokeErr != nil {
			log.Printf("GatewayConfigureAgentProvider: rollback virtual key %d after publish failure also failed: %v", key.ID, revokeErr)
		}
		return nil, &errcode.ErrInternal
	}

	// 广播确认成功后再吊销旧Key（显式 resend 或存量 direct 升级）：保证"发新Key"到
	// "广播出去"这段时间里旧Key全程有效，失败时不影响原先可用的 Key。
	if hasExisting {
		if revokeErr := svc.RevokeVirtualKey(existing.ID); revokeErr != nil {
			log.Printf("GatewayConfigureAgentProvider: revoke old virtual key %d after resend also failed: %v", existing.ID, revokeErr)
		}
	}
	return &GatewayConfigureAgentProviderResp{AlreadyConfigured: false}, nil
}

// gatewayConfiguredRelayModel resolves the model snapshot for the legacy
// provider-broadcast path. A state row wins when it is enabled and explicit;
// otherwise the wallet's persisted default model is the established fallback.
func gatewayConfiguredRelayModel(walletID, agentID int64) (string, *errcode.ErrCode) {
	if config.C.Gateway.RelayStateEnabled {
		row, err := store.GetGatewayAgentRelayState(agentID)
		if err == nil && row.Enabled && strings.TrimSpace(row.RelayModel) != "" {
			return strings.TrimSpace(row.RelayModel), nil
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", &errcode.ErrInternal
		}
	}
	settings, err := gatewayRelayService().Get(walletID)
	if err != nil {
		return "", &errcode.ErrInternal
	}
	return strings.TrimSpace(settings.DefaultModel), nil
}

func marshalDirectRelayCapability(capability *DirectRelayCapability) (json.RawMessage, error) {
	if capability == nil {
		return nil, nil
	}
	raw, err := json.Marshal(capability)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func directRelaySupported(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var capability DirectRelayCapability
	if err := json.Unmarshal(raw, &capability); err != nil {
		return false
	}
	return (capability.Claude != nil && capability.Claude.Supported) ||
		(capability.Codex != nil && capability.Codex.Supported)
}

// gatewayResolveOwnedSupportedAgent 校验 agentID 属于 ownerID 且是"Grix中转"接得通的托管Agent类型，
// 返回该Agent记录。GatewayConfigureAgentProvider（旧版WS广播下发）与
// GatewayIssueAgentRelayCredential（新版HTTP直接签发）共用同一套校验，避免两条路径的越权/类型
// 检查逐渐漂移不一致。
func gatewayResolveOwnedSupportedAgent(ownerID, agentID int64) (*model.Agent, *errcode.ErrCode) {
	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrAgentNotFound
		}
		return nil, &errcode.ErrInternal
	}
	if agent.OwnerID != ownerID {
		return nil, &errcode.ErrAgentForbidden
	}
	if agent.Status == 3 {
		return nil, &errcode.ErrAgentNotFound
	}
	if !gatewaySupportedAgentClientTypes[agent.AgentClientType] {
		return nil, &errcode.ErrGatewayUnsupportedClientType
	}
	return &agent, nil
}

// GatewayIssueAgentRelayCredentialResp 是"直接签发中转凭证"的结果：VirtualKey 明文只在这一次
// HTTP响应里返回，服务端此后只能查到哈希查不到明文。调用方（桌面端后端代理）必须立即转交给
// 本地 Connector 的凭证应用接口，用完即弃，不得记录日志或落盘。
type GatewayIssueAgentRelayCredentialResp struct {
	VirtualKey       string `json:"virtual_key"`
	AnthropicBaseURL string `json:"anthropic_base_url"`
	OpenAIBaseURL    string `json:"openai_base_url"`
	// RelayModel 是本次签发选定并落库的模型（可能为空=未指定），调用方原样转给连接器。
	RelayModel string `json:"relay_model"`
	// DirectRelay 是 versioned 直连能力声明（plan-direct-relay-first-refactor §6.1）：
	// 可选追加对象，旧连接器安全忽略；只在 gateway.direct_relay_enabled 开启且
	// agent 是 claude/codex 类型时返回。绝不含虚拟 Key 或真实厂商 Key。
	DirectRelay *DirectRelayCapability `json:"direct_relay,omitempty"`
}

// GatewayIssueAgentRelayCredential 给用户名下一个托管Agent直接签发一把专属中转凭证，明文Key连同
// 两个协议入口地址一起通过HTTP响应返回给调用方——不再经 Redis/WS local_action 向 connector 广播，
// 服务端只管鉴权归属与签发，是否应用、何时应用完全交给调用方（桌面端后端代理）自己去调本地
// Connector 的凭证应用接口。旧的 GatewayConfigureAgentProvider（WS广播路径）保留给还没升级的
// 旧客户端用，两者可以并存，因为各自签发的是互不影响的独立虚拟Key。
//
// 每次调用都会签发一把全新的Key，并吊销该Agent之前的活跃Key（如果有）：跟旧接口不同，这里没有
// "已配置过就跳过"的幂等短路——HTTP响应本身就是可靠交付，不存在广播那种"发了但可能收不到"的
// 问题，调用方决定要不要重新申请。
func GatewayIssueAgentRelayCredential(ownerID, agentID int64, anthropicBaseURL, openaiBaseURL, relayModel string) (*GatewayIssueAgentRelayCredentialResp, *errcode.ErrCode) {
	agent, ec := gatewayResolveOwnedSupportedAgent(ownerID, agentID)
	if ec != nil {
		return nil, ec
	}
	// 模型校验口径与中转设置一致：选了就必须是"后端当前支持的模型"，
	// 放一个不可用模型落库等于埋一个发请求时才炸的雷。
	// 原生配置类型的CLI配置结构里模型名必填（connector ≥3.6.0 侧同样强校验），
	// 在源头拦住比让桌面端拿着凭证去连接器那里吃 MISSING_MODEL 清楚得多。
	relayModel = strings.TrimSpace(relayModel)
	if relayModel == "" {
		if gatewayNativeProviderClientTypes[agent.AgentClientType] {
			return nil, &errcode.ErrGatewayRelayModelRequired
		}
	} else {
		servable, ec := servableModels()
		if ec != nil {
			return nil, ec
		}
		if !servable[relayModel] {
			return nil, &errcode.ErrGatewayModelNotServable
		}
	}

	w, ec := ensureGatewayWallet(ownerID)
	if ec != nil {
		return nil, ec
	}

	svc := gatewayWalletService()
	existing, err := svc.GetActiveVirtualKeyByAgent(w.ID, agentID)
	hasExisting := err == nil
	if !hasExisting && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &errcode.ErrInternal
	}

	plainKey, _, err := svc.IssueVirtualKeyForAgent(w.ID, agentID, agent.AgentName, relayModel)
	if err != nil {
		return nil, &errcode.ErrInternal
	}

	// 新Key已经落库生效之后再吊销旧Key：保证这段时间窗口里至少有一把Key可用，
	// 吊销失败也只是留下一把多余的旧Key，不影响本次签发结果。
	if hasExisting {
		if revokeErr := svc.RevokeVirtualKey(existing.ID); revokeErr != nil {
			log.Printf("GatewayIssueAgentRelayCredential: revoke old virtual key %d failed: %v", existing.ID, revokeErr)
		}
	}

	return &GatewayIssueAgentRelayCredentialResp{
		VirtualKey:       plainKey,
		AnthropicBaseURL: anthropicBaseURL,
		OpenAIBaseURL:    openaiBaseURL,
		RelayModel:       relayModel,
		DirectRelay:      buildDirectRelayCapability(agent.AgentClientType, relayModel, anthropicBaseURL, openaiBaseURL),
	}, nil
}
