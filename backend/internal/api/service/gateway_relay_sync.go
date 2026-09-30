package service

// 中转开关服务端化（migration 111）的 WS 对齐协议业务层
// v5 使用事件驱动，无心跳：
// 路径 A——connector 上线发 relay_state_sync_request 主动对齐（可靠性主路径）；
// 回执——relay_state_report 与 apply_relay_state 的 local_action_result 写回 actual。
// agentapi 的 WS handler 只做协议适配（身份取自连接认证结果、限流、seq 关联），
// 业务规则全部收在这里，保持 Handler→Service→Store 边界。

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/gateway/provisioning"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
)

// GatewayRelayStateSyncResp 是 relay_state_sync_request 的应答内容。
type GatewayRelayStateSyncResp struct {
	// Enabled / Model / Revision 是服务端持有的 desired（唯一权威期望态）。
	Enabled  bool
	Model    string
	Revision int64
	// Credential 非空表示本次顺带重签了专属虚拟Key（enabled=true 且无有效 Key 或
	// desired model 与最新活跃 Key 的 relay_model 不一致，设计 §2.2 评审#9）。
	// 明文 Key 只出现在这一次 WS 应答里，不落日志、不落 state 表。
	Credential *GatewayIssueAgentRelayCredentialResp
	// ModelAutoFilled=true 表示 desired model 是服务端回填的（原生配置类型首报缺 model：
	// 先取最新活跃 Key 的 relay_model，无 Key 回退钱包 default_model，设计 §2.4 评审#3），
	// 桌面端据此提示"已自动选用默认模型，可点击更换"。
	ModelAutoFilled bool
}

// GatewayRelayStateSync 处理 connector 上线后的中转状态对齐。身份（ownerID/agentID）
// 必须取自连接认证结果，调用方不可冒充。
//
// 存量对账（多设备首次同步策略，设计 §2.4）：
//   - state 行不存在且上报了 local_enabled：以首个上报设备的本机名单落 initial desired
//     （并发首报用 expected_revision=0 的乐观锁竞速，落败方重读已存在的行，等同"首个获胜"）；
//   - state 行已存在：忽略 local_enabled/local_model，以服务端 desired 为准——
//     本机名单从此不再回写，避免多设备互相翻转。
func GatewayRelayStateSync(ownerID, agentID int64, localEnabled *bool, localModel, anthropicBaseURL, openaiBaseURL string) (*GatewayRelayStateSyncResp, *errcode.ErrCode) {
	if !config.C.Gateway.RelayStateEnabled {
		return nil, &errcode.ErrGatewayRelayStateDisabled
	}
	agent, ec := gatewayResolveOwnedRelayAgent(ownerID, agentID)
	if ec != nil {
		return nil, ec
	}
	w, ec := ensureGatewayWallet(ownerID)
	if ec != nil {
		return nil, ec
	}

	resp := &GatewayRelayStateSyncResp{}
	row, err := store.GetGatewayAgentRelayState(agentID)
	switch {
	case err == nil:
		// 行已存在：忽略本机名单，以服务端 desired 为准。
	case errors.Is(err, gorm.ErrRecordNotFound):
		if localEnabled == nil {
			// 未上报本机名单（新装 connector 还没有本地名单概念）：不建行，
			// desired 默认关（GET /agents 对无行同样视为 false，两端口径一致）。
			resp.Enabled = false
			resp.Model = ""
			resp.Revision = 0
			return resp, nil
		}
		model := strings.TrimSpace(localModel)
		if *localEnabled && model == "" && relayModelBackfillClientType(agent.AgentClientType) {
			// 缺 model 的回填：先取最新活跃 Key 的 relay_model，
			// 无活跃 Key 回退钱包级 default_model（必然在可用清单内，保证期望态合法）。
			filled, bErr := backfillRelayModelForAgent(w.ID, agentID)
			if bErr != nil {
				if gatewayNativeProviderClientTypes[agent.AgentClientType] {
					// 原生类型没 model 期望态非法，保持 fail loud
					return nil, &errcode.ErrInternal
				}
			} else {
				model = filled
			}
			resp.ModelAutoFilled = model != ""
		}
		zero := int64(0)
		row, err = store.UpsertGatewayAgentRelayStateDesired(agentID, w.ID, *localEnabled, model, &zero)
		if err != nil {
			if !errors.Is(err, store.ErrGatewayAgentRelayStateRevisionConflict) {
				return nil, &errcode.ErrInternal
			}
			// 另一台设备抢先落了 initial desired：以它的为准（忽略本机名单）。
			resp.ModelAutoFilled = false
			row, err = store.GetGatewayAgentRelayState(agentID)
			if err != nil {
				return nil, &errcode.ErrInternal
			}
		}
	default:
		return nil, &errcode.ErrInternal
	}

	// 存量行修复：claude/codex 历史期望态允许空 model（空 = 走网关映射兜底），但空 model
	// 会让 direct_relay capability 永远 supported=false（primary_model 凑不齐），这些 agent
	// 永远停在 MITM。enabled 且空 model 时按同一口径回填并落库；乐观锁冲突放弃，
	// 等下一轮 sync 再试。回填后 desired 与最新活跃 Key 不一致会触发下方顺带重签，
	// connector 一次拿到"期望态 + 带完整 capability 的凭证"。
	if row.Enabled && strings.TrimSpace(row.RelayModel) == "" && relayModelBackfillClientType(agent.AgentClientType) {
		if filled, bErr := backfillRelayModelForAgent(w.ID, agentID); bErr == nil && filled != "" {
			expected := row.Revision
			if newRow, upErr := store.UpsertGatewayAgentRelayStateDesired(agentID, w.ID, row.Enabled, filled, &expected); upErr == nil {
				row = newRow
				resp.ModelAutoFilled = true
			}
		}
	}

	resp.Enabled = row.Enabled
	resp.Model = row.RelayModel
	resp.Revision = row.Revision

	// enabled=true 且（无有效 Key 或 desired model ≠ 最新活跃 Key 的 relay_model）时
	// 顺带重签凭证随应答下发；connector 据此一次拿到"期望态 + 可用凭证"。
	if row.Enabled {
		keyModel, hasKey := gatewayLatestActiveKeyRelayModel(w.ID, agentID)
		if !hasKey || keyModel != row.RelayModel {
			// connector 从自己的 ws_url 推导并随 sync 上报两个入口；这样内联重签
			// 能构造完整的 Codex/Claude direct_relay capability，而不再被空地址降级。
			cred, ec := GatewayIssueAgentRelayCredential(ownerID, agentID, anthropicBaseURL, openaiBaseURL, row.RelayModel)
			if ec != nil {
				return nil, ec
			}
			resp.Credential = cred
		}
	}
	return resp, nil
}

// relayModelBackfillClientType 在原生配置类型之外补上 claude/codex：这两类历史上走 MITM，
// 期望态 model 可空（空 = 走网关映射兜底），但空 model 会让 direct_relay capability 永远
// supported=false，direct 永远轮不到它们。缺 model 时按原生类型同一口径回填。
func relayModelBackfillClientType(clientType string) bool {
	return gatewayNativeProviderClientTypes[clientType] ||
		clientType == model.AgentClientTypeClaude ||
		clientType == model.AgentClientTypeCodex
}

// backfillRelayModelForAgent 给出回填模型：最新活跃 Key 的 relay_model → 钱包 default_model；
// 都不可用或读钱包失败时返回 ("", err)/""，调用方按类型决定 fail loud 还是维持空 model。
func backfillRelayModelForAgent(walletID, agentID int64) (string, error) {
	if keyModel, ok := gatewayLatestActiveKeyRelayModel(walletID, agentID); ok && strings.TrimSpace(keyModel) != "" {
		return strings.TrimSpace(keyModel), nil
	}
	settings, err := gatewayRelayService().Get(walletID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(settings.DefaultModel), nil
}

// gatewayLatestActiveKeyRelayModel 取该 agent 最新签发（snowflake ID 最大）的活跃专属 Key
// 的 relay_model 快照；无活跃 Key 返回 ("", false)。口径与 GatewayListAgents 的回显一致——
// 重签时吊销旧 Key 失败可能残留多把活跃 Key，必须确定地取最新那把。
func gatewayLatestActiveKeyRelayModel(walletID, agentID int64) (string, bool) {
	keys, err := gatewayWalletService().ListVirtualKeys(walletID)
	if err != nil {
		return "", false
	}
	latestID := int64(0)
	latestModel := ""
	for i := range keys {
		k := &keys[i]
		if k.AgentID != agentID || k.Status != model.GatewayVirtualKeyStatusActive {
			continue
		}
		if k.ID > latestID {
			latestID = k.ID
			latestModel = k.RelayModel
		}
	}
	return latestModel, latestID > 0
}

// GatewayRelayStateReport 处理 connector 的事件驱动回执（relay_state_report）：
// 回执必须携带其对应的 desired revision，只接受 revision >= 当前 desired revision 的
// 报告写回 applied/applied_at，过期一律丢弃（设计 §2.4 回执幂等——既防多设备重复下发
// 写回旧值，也消除报告在 desired 变更窗口期内延迟到达写回旧 applied 的竞态）。
// 返回 written 表示本次是否落库（仅用于调用方日志）。
func GatewayRelayStateReport(ownerID, agentID int64, applied bool, revision int64) (written bool, ec *errcode.ErrCode) {
	return gatewayRelayStateWriteApplied(ownerID, agentID, applied, revision)
}

// GatewayRelayStateApplyResult 处理 apply_relay_state 的 local_action_result 回执：
// ok=true 才能把 applied 置 true；失败（ok=false）表示"期望未达成"，applied 置 false
// （applied 始终等于最近一次有效回执携带的实际态值，设计 §2.4 评审#5）。
// 与 report 同一套 revision 新鲜度门槛。
func GatewayRelayStateApplyResult(ownerID, agentID int64, revision int64, ok bool) (written bool, ec *errcode.ErrCode) {
	return gatewayRelayStateWriteApplied(ownerID, agentID, ok, revision)
}

func gatewayRelayStateWriteApplied(ownerID, agentID int64, applied bool, revision int64) (bool, *errcode.ErrCode) {
	// flag 关闭时下发/回执链路整体停用（设计 §2.6）：静默丢弃，不当错误处理。
	if !config.C.Gateway.RelayStateEnabled {
		return false, nil
	}
	if _, ec := gatewayResolveOwnedRelayAgent(ownerID, agentID); ec != nil {
		return false, ec
	}
	row, err := store.GetGatewayAgentRelayState(agentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 没有 desired 的 agent 不存在可展示的"已生效"，回执直接丢弃。
		return false, nil
	}
	if err != nil {
		return false, &errcode.ErrInternal
	}
	if revision < row.Revision {
		return false, nil
	}
	if err := store.SetGatewayAgentRelayStateApplied(agentID, applied, time.Now()); err != nil {
		return false, &errcode.ErrInternal
	}
	return true, nil
}

// GatewayRelayStateDisconnected 在该 agent 的最后一条权威 WS 连接断开时，把 applied
// 置为 false——实际态随离线不可知，保守标未生效（设计 §2.4 applied 生命周期）。
// "是否还有其他权威连接在线"由调用方（agentapi 断连钩子，查本机连接表 + Redis 路由表）
// 判定；flag 关闭时停用。
func GatewayRelayStateDisconnected(agentID int64) {
	if !config.C.Gateway.RelayStateEnabled || agentID <= 0 {
		return
	}
	// SetApplied 只更新已有行：无 desired 的 agent 本就无可展示的"已生效"。
	if err := store.SetGatewayAgentRelayStateApplied(agentID, false, time.Now()); err != nil {
		// 断连清理失败不阻塞注销主路径，下次 sync/report 会自愈。
		_ = err
	}
}

// applyRelayStateActionType 转发 provisioning 的契约常量，避免本包各处散写字面量。
const applyRelayStateActionType = provisioning.ApplyRelayStateActionType
