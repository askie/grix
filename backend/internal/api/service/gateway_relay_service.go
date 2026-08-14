// Package service 的这个文件是"Grix中转"模型设置的 C端接口层：
// 可用模型清单（含单价，给用户看成本）、兜底模型与模型映射表的读写、
// 以及"我名下哪些托管Agent接了中转"。
//
// 映射与兜底只存在于后端、由网关在每次请求时解析（见 internal/gateway/relay），
// 所以这里改完设置不需要给 connector 下发任何东西，下一次请求就是新的。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/gateway/modelroute"
	"github.com/askie/grix/backend/internal/gateway/provisioning"
	"github.com/askie/grix/backend/internal/gateway/relay"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

func gatewayRelayService() *relay.Service {
	return relay.New(store.DB, store.RDB)
}

// 映射表的规模上限：它在计费主路径上每请求都读，不能放任用户塞任意大的 JSON。
// 值取得宽松（正常几条到十几条映射），够用且防滥用。
const (
	maxModelMapEntries = 50
	maxModelNameLen    = 128
)

// GatewayModel 是一个"后端支持的模型"：能路由到厂商、且价目表里有当前生效的基准价。
// 单价一并给出去，前端要把成本明明白白摆在用户面前——用户在 Claude 的壳子里干活、
// 花的却是别的模型的钱，看不见价格他不可能踏实。
type GatewayModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// 单价单位：USD / 每百万 token。
	InputPricePerM  decimal.Decimal `json:"input_price_per_m"`
	OutputPricePerM decimal.Decimal `json:"output_price_per_m"`
}

type GatewayListModelsResp struct {
	Items []GatewayModel `json:"items"`
}

// GatewayListModels 列出当前后端支持的模型。
// 数据源是价目表：**有当前生效的全天兜底价 = 可用**。migration 里没有预置任何价目，
// 全靠塘主后台录入——价目表为空时这里返回空清单，中转即不可用。
func GatewayListModels() (*GatewayListModelsResp, *errcode.ErrCode) {
	var rules []model.GatewayPricingRule
	if err := store.DB.
		Where("effective_to IS NULL").
		Where("daily_window_start_min IS NULL AND daily_window_end_min IS NULL").
		Where("input_tier_start_tokens IS NULL AND input_tier_end_tokens IS NULL").
		Order("provider ASC, model ASC").
		Find(&rules).Error; err != nil {
		return nil, &errcode.ErrInternal
	}

	items := make([]GatewayModel, 0, len(rules))
	for i := range rules {
		r := &rules[i]
		// 必须跟网关用同一份路由判定（modelroute）：塘主后台录价目时不校验 provider，
		// 若这里只看"有基准价"，一条 openai/gpt-5 的价目就会把网关根本服务不了的模型
		// 塞进用户的可选清单——用户选中当兜底，所有请求全 400。
		// 同时要求 provider 列与模型名前缀路由一致：录错档（provider=volcano_ark 配
		// model=deepseek-xxx）的规则也进不了清单——网关按名字前缀去 deepseek 找价目，
		// 找不到照样 400。
		if !modelroute.Routable(r.Model) || r.Provider != modelroute.ResolveProvider(r.Model) {
			continue
		}
		items = append(items, GatewayModel{
			Provider: r.Provider,
			Model:    r.Model,
			// 展示用未命中缓存的输入价（用户实际付的上限），不拿缓存命中价去美化账单。
			InputPricePerM:  r.UncachedInputPricePerM,
			OutputPricePerM: r.OutputPricePerM,
		})
	}
	return &GatewayListModelsResp{Items: items}, nil
}

// servableModels 返回当前可用模型名集合，供写入设置时校验。
func servableModels() (map[string]bool, *errcode.ErrCode) {
	resp, ec := GatewayListModels()
	if ec != nil {
		return nil, ec
	}
	set := make(map[string]bool, len(resp.Items))
	for _, m := range resp.Items {
		set[m.Model] = true
	}
	return set, nil
}

// GatewayRelaySettingsResp 是当前用户的中转设置。
type GatewayRelaySettingsResp struct {
	// DefaultModel 兜底模型：所有没被映射命中、本身又不是后端支持模型的请求都落到它。
	// 用户从没设置过时返回系统默认，保证任何时候都有兜底、链路不会因为"没配置"而中断。
	DefaultModel string `json:"default_model"`
	// ModelMap 是 {客户端侧模型名: 后端支持的模型名}。
	ModelMap map[string]string `json:"model_map"`
}

// GatewayGetRelaySettings 取当前用户的兜底模型 + 模型映射表。
func GatewayGetRelaySettings(ownerID int64) (*GatewayRelaySettingsResp, *errcode.ErrCode) {
	w, ec := ensureGatewayWallet(ownerID)
	if ec != nil {
		return nil, ec
	}
	s, err := gatewayRelayService().Get(w.ID)
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	return &GatewayRelaySettingsResp{DefaultModel: s.DefaultModel, ModelMap: s.ModelMap}, nil
}

// GatewayPutRelaySettingsReq 是保存中转设置的入参。
type GatewayPutRelaySettingsReq struct {
	DefaultModel string            `json:"default_model"`
	ModelMap     map[string]string `json:"model_map"`
}

// GatewayPutRelaySettings 覆盖保存当前用户的中转设置。
//
// 校验口径：兜底模型和所有映射目标都必须是"后端当前支持的模型"，否则直接拒绝——
// 让一个指向不存在模型的映射落库，等于给用户埋一个只会在真正发请求时才炸的雷。
// 映射的 key（客户端侧模型名）不校验：用户爱写什么写什么，写错了也只会落到兜底模型，不会挂。
func GatewayPutRelaySettings(ownerID int64, req GatewayPutRelaySettingsReq) (*GatewayRelaySettingsResp, *errcode.ErrCode) {
	w, ec := ensureGatewayWallet(ownerID)
	if ec != nil {
		return nil, ec
	}

	// 映射表在计费主路径上每次请求都要读，必须有上限；上限取得宽松，正常使用碰不到。
	if len(req.ModelMap) > maxModelMapEntries {
		return nil, &errcode.ErrGatewayModelMapTooLarge
	}

	servable, ec := servableModels()
	if ec != nil {
		return nil, ec
	}

	defaultModel := strings.TrimSpace(req.DefaultModel)
	if defaultModel == "" || !servable[defaultModel] {
		return nil, &errcode.ErrGatewayModelNotServable
	}

	cleaned := make(map[string]string, len(req.ModelMap))
	for k, v := range req.ModelMap {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		if len(k) > maxModelNameLen || len(v) > maxModelNameLen {
			return nil, &errcode.ErrGatewayModelMapTooLarge
		}
		if !servable[v] {
			return nil, &errcode.ErrGatewayModelNotServable
		}
		cleaned[k] = v
	}

	if err := gatewayRelayService().Put(w.ID, relay.Settings{
		DefaultModel: defaultModel,
		ModelMap:     cleaned,
	}); err != nil {
		return nil, &errcode.ErrInternal
	}
	return &GatewayRelaySettingsResp{DefaultModel: defaultModel, ModelMap: cleaned}, nil
}

// GatewayAgentRelayState 是"我名下某个托管Agent跟中转的关系"。
type GatewayAgentRelayState struct {
	AgentID   int64  `json:"agent_id,string"`
	AgentName string `json:"agent_name"`
	// ClientType 是托管Agent的客户端类型（claude/codex/qwen/opencode/gemini/...）。
	ClientType string `json:"client_type"`
	// Supported=false 表示该类型接不了中转（绑定自己账号或BYOK、不支持自定义端点）。
	// 前端必须把原因显示出来，否则用户会疑惑"为什么 Gemini 不扣我的钱"。
	Supported bool `json:"supported"`
	// Configured=true 表示已经签过专属虚拟Key、流量正走中转。
	Configured bool `json:"configured"`
	// RelayModel：gateway.relay_state_enabled 开启时返回 desired（state 表的唯一权威期望模型；
	// state 表无行时回退该Agent最新活跃虚拟Key的 relay_model 快照）；flag 关闭时保持原语义
	// （最新活跃Key的 relay_model）。空=未指定，走网关模型映射/兜底。
	RelayModel string `json:"relay_model"`

	// —— 以下为 relay state（migration 111）扩展字段，仅 gateway.relay_state_enabled 开启时
	// 返回；flag 关闭时不读 state 表、这些字段整体缺席，前端按旧版展示（设计 §2.6）——
	// Enabled 是期望开关（desired；state 表无行视为 false——老 connector 未同步前期望态默认为关）。
	Enabled *bool `json:"enabled,omitempty"`
	// Applied 是 connector 最近一次有效回执的实际态（actual）。
	Applied *bool `json:"applied,omitempty"`
	// AppliedAt 是最近一次有效回执时间；null 表示从未收到回执。
	AppliedAt *time.Time `json:"applied_at,omitempty"`
	// StateKnown 表示服务端能否确知该 agent 的实际态（当前存在在线权威 WS 连接）。
	// false 时 applied 不可信，前端按"设备离线/旧版连接器"展示。
	StateKnown *bool `json:"state_known,omitempty"`
}

type GatewayListAgentsResp struct {
	Items []GatewayAgentRelayState `json:"items"`
}

// GatewayListAgents 列出当前用户名下的托管Agent，标出每个能否接中转、是否已接入。
// gateway.relay_state_enabled 开启时叠加 relay state（migration 111）的 desired/actual
// 扩展字段；flag 关闭时不读 state 表，响应完全回落到 configured 旧语义（设计 §2.6）。
func GatewayListAgents(ownerID int64) (*GatewayListAgentsResp, *errcode.ErrCode) {
	w, ec := ensureGatewayWallet(ownerID)
	if ec != nil {
		return nil, ec
	}

	var agents []model.Agent
	if err := store.DB.
		Where("owner_id = ? AND status != 3", ownerID).
		Order("created_at ASC").
		Find(&agents).Error; err != nil {
		return nil, &errcode.ErrInternal
	}

	keys, err := gatewayWalletService().ListVirtualKeys(w.ID)
	if err != nil {
		return nil, &errcode.ErrInternal
	}
	configured := make(map[int64]bool, len(keys))
	relayModelByAgent := make(map[int64]string, len(keys))
	// 罕见但存在：重签发时吊销旧 Key 失败会留下同 agent 的多把活跃 Key（签发方有意
	// 不因此中断）。回显必须确定地取"最新签发那把"的模型——snowflake ID 单调，按 ID 取最大。
	latestKeyIDByAgent := make(map[int64]int64, len(keys))
	for i := range keys {
		k := &keys[i]
		if k.AgentID == 0 || k.Status != model.GatewayVirtualKeyStatusActive {
			continue
		}
		configured[k.AgentID] = true
		if k.ID > latestKeyIDByAgent[k.AgentID] {
			latestKeyIDByAgent[k.AgentID] = k.ID
			relayModelByAgent[k.AgentID] = k.RelayModel
		}
	}

	// relay state 扩展（flag 门控）：desired 以 state 表为唯一权威，Key 快照只做无行时的回退。
	var stateByAgent map[int64]*model.GatewayAgentRelayState
	var stateKnown map[int64]bool
	if config.C.Gateway.RelayStateEnabled {
		states, err := store.ListGatewayAgentRelayStatesByWallet(w.ID)
		if err != nil {
			return nil, &errcode.ErrInternal
		}
		stateByAgent = make(map[int64]*model.GatewayAgentRelayState, len(states))
		agentIDs := make([]int64, 0, len(agents))
		for i := range states {
			stateByAgent[states[i].AgentID] = &states[i]
		}
		for i := range agents {
			agentIDs = append(agentIDs, agents[i].ID)
		}
		stateKnown = gatewayAgentStatesKnown(ownerID, agentIDs)
	}

	items := make([]GatewayAgentRelayState, 0, len(agents))
	for i := range agents {
		a := &agents[i]
		item := GatewayAgentRelayState{
			AgentID:    a.ID,
			AgentName:  a.AgentName,
			ClientType: a.AgentClientType,
			Supported:  gatewaySupportedAgentClientTypes[a.AgentClientType],
			Configured: configured[a.ID],
			RelayModel: relayModelByAgent[a.ID],
		}
		if stateByAgent != nil {
			enabled, applied := false, false
			if st, ok := stateByAgent[a.ID]; ok {
				enabled = st.Enabled
				applied = st.Applied
				item.AppliedAt = st.AppliedAt
				// relay_model 改返回 desired（设计 §2.2 评审#9）：state 表是唯一权威，
				// Key 上的只是最近一次签发快照。
				item.RelayModel = st.RelayModel
			}
			known := stateKnown[a.ID]
			item.Enabled = &enabled
			item.Applied = &applied
			item.StateKnown = &known
		}
		items = append(items, item)
	}
	return &GatewayListAgentsResp{Items: items}, nil
}

// GatewaySetAgentRelay 设置用户名下一个托管Agent的中转开关（desired）：
// POST /v1/gateway/agents/:agent_id/relay 的业务实现。
//
// 校验顺序：feature flag → 归属（不属返回 404，不泄露他人 agent 存在性）→ 模型
// （非空必须在该钱包可用模型清单内，复用 servableModels 口径；开启 + 原生配置类型时
// model 必填，对应前端 need_model 文案）→ 写前 ensureGatewayWallet 隐式建钱包
// （state 表 wallet_id 非空，设计 §2.2 评审#6）。
//
// expectedRevision 非 nil 时走乐观锁：与服务端当前 revision 不一致返回
// ErrGatewayRelayStateConflict（HTTP 409）并带回最新 state，前端刷新后重试；
// 不传则 last-write-wins。写成功返回最新 state（含新 revision）。
func GatewaySetAgentRelay(ctx context.Context, ownerID, agentID int64, enabled bool, relayModel string, expectedRevision *int64) (*GatewayAgentRelayStateResp, *errcode.ErrCode) {
	if !config.C.Gateway.RelayStateEnabled {
		return nil, &errcode.ErrGatewayRelayStateDisabled
	}

	agent, ec := gatewayResolveOwnedRelayAgent(ownerID, agentID)
	if ec != nil {
		return nil, ec
	}

	// 模型校验口径与中转设置/凭证签发一致：选了就必须是"后端当前支持的模型"。
	// 原生配置类型的 CLI 配置结构里模型名必填，开启时不带 model 在源头拦下
	// （与 GatewayIssueAgentRelayCredential 同一约定，对应前端 need_model 文案）。
	relayModel = strings.TrimSpace(relayModel)
	if relayModel == "" {
		if enabled && gatewayNativeProviderClientTypes[agent.AgentClientType] {
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

	row, err := store.UpsertGatewayAgentRelayStateDesired(agentID, w.ID, enabled, relayModel, expectedRevision)
	if err != nil {
		if errors.Is(err, store.ErrGatewayAgentRelayStateRevisionConflict) {
			latest, getErr := store.GetGatewayAgentRelayState(agentID)
			if getErr != nil {
				return nil, &errcode.ErrInternal
			}
			return gatewayAgentRelayStateRespOf(latest), &errcode.ErrGatewayRelayStateConflict
		}
		return nil, &errcode.ErrInternal
	}

	// 路径 B（设计 §2.4）：目标 connector 在线时经 Redis 广播走 local_action
	// action_type=apply_relay_state 即时下发；agent 离线时 pub/sub 天然静默丢弃，
	// 只落 desired，connector 上线后由路径 A（relay_state_sync_request）对齐兜底。
	// 广播失败不阻塞写路径（理由同上，sync 兜底）。
	if err := provisioning.PublishApplyRelayState(provisioning.RelayStateApplyConfig{
		AgentID:  agentID,
		Enabled:  row.Enabled,
		Model:    row.RelayModel,
		Revision: row.Revision,
	}); err != nil {
		logger.L.Warnf("GatewaySetAgentRelay: publish apply_relay_state failed agent=%d err=%v", agentID, err)
	}
	_ = ctx
	return gatewayAgentRelayStateRespOf(row), nil
}

// GatewayAgentRelayStateResp 是 relay state 写操作返回的最新 state；409 冲突时也随响应带回。
type GatewayAgentRelayStateResp struct {
	AgentID    int64      `json:"agent_id,string"`
	Enabled    bool       `json:"enabled"`
	RelayModel string     `json:"relay_model"`
	Revision   int64      `json:"revision"`
	Applied    bool       `json:"applied"`
	AppliedAt  *time.Time `json:"applied_at,omitempty"`
}

func gatewayAgentRelayStateRespOf(row *model.GatewayAgentRelayState) *GatewayAgentRelayStateResp {
	return &GatewayAgentRelayStateResp{
		AgentID:    row.AgentID,
		Enabled:    row.Enabled,
		RelayModel: row.RelayModel,
		Revision:   row.Revision,
		Applied:    row.Applied,
		AppliedAt:  row.AppliedAt,
	}
}

// gatewayResolveOwnedRelayAgent 校验 agentID 属于 ownerID 且是"Grix中转"接得通的托管Agent类型。
// 与 gatewayResolveOwnedSupportedAgent 的唯一差别：归属失败返回 404 而非 403——
// 中转开关接口不向调用方泄露他人 agent 的存在性（设计 §2.3/§2.6）。
func gatewayResolveOwnedRelayAgent(ownerID, agentID int64) (*model.Agent, *errcode.ErrCode) {
	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &errcode.ErrAgentNotFound
		}
		return nil, &errcode.ErrInternal
	}
	if agent.OwnerID != ownerID || agent.Status == 3 {
		return nil, &errcode.ErrAgentNotFound
	}
	if !gatewaySupportedAgentClientTypes[agent.AgentClientType] {
		return nil, &errcode.ErrGatewayUnsupportedClientType
	}
	return &agent, nil
}

// agentWSRouteKey / agentWSRouteKeyForOwner 与 internal/ws/agentapi/route_registry.go 的
// agentRouteKey / agentRouteKeyForOwner 保持同一 key 格式。agentapi 反向依赖本包
// （import cycle）无法直接复用其函数，key 格式改动时需两边同步。
func agentWSRouteKey(agentID int64) string {
	return fmt.Sprintf("im:agent_api:route:%d", agentID)
}

func agentWSRouteKeyForOwner(agentID, ownerID int64) string {
	return fmt.Sprintf("im:agent_api:route:%d:%d", agentID, ownerID)
}

// agentWSCapabilitiesKey / agentWSCapabilitiesKeyForOwner 与 internal/ws/agentapi/route_registry.go
// 的 agentCapabilitiesKey / agentCapabilitiesKeyForOwner 保持同一 key 格式（import cycle
// 无法直接复用，同 agentWSRouteKey 的约定）。
func agentWSCapabilitiesKey(agentID int64) string {
	return fmt.Sprintf("im:agent_api:capabilities:%d", agentID)
}

func agentWSCapabilitiesKeyForOwner(agentID, ownerID int64) string {
	return fmt.Sprintf("im:agent_api:capabilities:%d:%d", agentID, ownerID)
}

// gatewayAgentStatesKnown 批量判断各 agent 的 state_known（设计 §2.3，规则写死）：
//
//	state_known = 该 agent 当前存在至少一条在线、通过权威校验、且能力位声明支持
//	              apply_relay_state 的 WS 连接
//
// 判据全部来自 Redis（跨节点可读）：路由 key 只给通过权威校验的连接登记、随心跳续期、
// 断连即删或过期（"有路由"≈"在线权威连接"）；能力 key 由 refreshAgentCapabilities 按
// 连接声明的 localActions 持久化，同样随连接消亡。两处 key 同 scope 配套判定：
// owner 路由配 owner 能力、主路由配主能力；老 connector 未声明 apply_relay_state
// （能力 key 不存在或清单不含该值）一律 false，其 applied 对前端无意义。
func gatewayAgentStatesKnown(ownerID int64, agentIDs []int64) map[int64]bool {
	known := make(map[int64]bool, len(agentIDs))
	if store.RDB == nil || len(agentIDs) == 0 {
		return known
	}
	ctx := context.Background()
	pipe := store.RDB.Pipeline()
	type probe struct {
		ownerRoute *redis.StringCmd
		mainRoute  *redis.StringCmd
		ownerCaps  *redis.StringCmd
		mainCaps   *redis.StringCmd
	}
	probes := make(map[int64]probe, len(agentIDs))
	for _, id := range agentIDs {
		if id <= 0 {
			continue
		}
		probes[id] = probe{
			ownerRoute: pipe.Get(ctx, agentWSRouteKeyForOwner(id, ownerID)),
			mainRoute:  pipe.Get(ctx, agentWSRouteKey(id)),
			ownerCaps:  pipe.Get(ctx, agentWSCapabilitiesKeyForOwner(id, ownerID)),
			mainCaps:   pipe.Get(ctx, agentWSCapabilitiesKey(id)),
		}
	}
	_, _ = pipe.Exec(ctx)
	for id, p := range probes {
		// StringCmd.Val() 在 key 不存在/出错时返回 ""，正好即"未知=离线/无能力"。
		ownerOnline := strings.TrimSpace(p.ownerRoute.Val()) != ""
		mainOnline := strings.TrimSpace(p.mainRoute.Val()) != ""
		known[id] = (ownerOnline && gatewayRelayStateCapable(p.ownerCaps.Val())) ||
			(mainOnline && gatewayRelayStateCapable(p.mainCaps.Val()))
	}
	return known
}

// gatewayRelayStateCapable 判断能力清单（refreshAgentCapabilities 持久化的 localActions
// JSON 数组）是否声明了 apply_relay_state；解析失败按未声明处理。
func gatewayRelayStateCapable(capsJSON string) bool {
	capsJSON = strings.TrimSpace(capsJSON)
	if capsJSON == "" {
		return false
	}
	var actions []string
	if err := json.Unmarshal([]byte(capsJSON), &actions); err != nil {
		return false
	}
	for _, a := range actions {
		if a == applyRelayStateActionType {
			return true
		}
	}
	return false
}
