package service

// 工具栏"中转开关"按钮的只读投影（agenttoolbar resolver 在构建快照时调用）。
//
// 全程只读：不隐式建钱包（ensureGatewayWallet 有建行副作用）、不写 state 表。
// 任何存储故障都返回 error，由调用方决定"不注入按钮"——绝不能把查询失败
// 解释成"官方模式"或拖垮整个工具栏。

import (
	"errors"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// GatewayAgentRelayToolbarProjection 是某个托管 Agent 的中转开关在工具栏上的展示原料。
type GatewayAgentRelayToolbarProjection struct {
	// Enabled 是 desired 期望开关（state 表无行 = false）。
	Enabled bool
	// RelayModel 是解析后的展示/开启用模型，兜底链：state 表已选 → 最新活跃 Key
	// 快照 → 钱包兜底模型。只在服务端已保存的值里取，读不到设置页未保存的本地选择。
	RelayModel string
	// Revision 是 state 行乐观锁版本；0 = 无行（可作为 expectedRevision 新建）。
	Revision int64
	// Applied 是 connector 最近一次有效回执的实际态；只用于"待生效"提示，不作开关值。
	Applied bool
	// AppliedRevision 是 applied 值对应的 desired revision：只有通过 revision
	// 门槛的回执才推进它；desired 写入与断连清理清 0。"当前 desired 已获回执
	// 确认" ⇔ AppliedRevision >= Revision（applied_at 区分不了回执确认与
	// 失败/超时/断连，三者都写时间戳）。
	AppliedRevision int64
	// StateKnown 表示服务端能否确知实际态（存在在线权威且声明 apply_relay_state 的
	// WS 连接）；false 时 applied 不可信。
	StateKnown bool
	// BalancePositive 表示钱包余额 > 0；无钱包 = false（无钱包即从未充值）。
	BalancePositive bool
}

// GatewayAgentRelayToolbarProjectionForAgent 返回工具栏投影；返回 (nil, nil) 表示
// "不该出现按钮"：flag 关闭、agent 不存在/已删除、请求人不是主人（中转是主人管理
// 能力，共享使用者看不到）、client type 不支持。返回 error 表示存储故障，
// 调用方同样不注入按钮但要记日志。
func GatewayAgentRelayToolbarProjectionForAgent(ownerID, agentID int64) (*GatewayAgentRelayToolbarProjection, error) {
	if !config.C.Gateway.RelayStateEnabled {
		return nil, nil
	}
	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if agent.OwnerID != ownerID || agent.Status == 3 {
		return nil, nil
	}
	if !gatewaySupportedAgentClientTypes[agent.AgentClientType] {
		return nil, nil
	}

	p := &GatewayAgentRelayToolbarProjection{}

	// 钱包只读：无钱包 = 从未充值 = 无余额，与"查询失败"严格区分（后者返回 error）。
	var w model.GatewayWallet
	walletErr := store.DB.Where("owner_id = ?", ownerID).First(&w).Error
	switch {
	case walletErr == nil:
		p.BalancePositive = w.Balance.GreaterThan(decimal.Zero)
	case errors.Is(walletErr, gorm.ErrRecordNotFound):
	default:
		return nil, walletErr
	}
	hasWallet := walletErr == nil

	row, err := store.GetGatewayAgentRelayState(agentID)
	switch {
	case err == nil:
		p.Enabled = row.Enabled
		p.Applied = row.Applied
		p.AppliedRevision = row.AppliedRevision
		p.Revision = row.Revision
		p.RelayModel = strings.TrimSpace(row.RelayModel)
	case errors.Is(err, gorm.ErrRecordNotFound):
	default:
		return nil, err
	}

	// 模型兜底只在"有行但模型为空"或"无行"时触发，且需要钱包才能查 Key/设置。
	// 查询失败必须传播为 error（调用方不注入按钮）：把故障折叠成"没有值"会让
	// 本应沿用 Key 模型的按钮错显示成钱包兜底模型，二者都不是用户的选择。
	if p.RelayModel == "" && hasWallet {
		keys, kErr := gatewayWalletService().ListVirtualKeys(w.ID)
		if kErr != nil {
			return nil, kErr
		}
		// 与 gatewayLatestActiveKeyRelayModel 同口径：取最新签发（ID 最大）的
		// 活跃专属 Key 的 relay_model；这里不能复用它，因为它把查询错误折叠成 false。
		latestKeyID := int64(0)
		for i := range keys {
			k := &keys[i]
			if k.AgentID != agentID || k.Status != model.GatewayVirtualKeyStatusActive {
				continue
			}
			if k.ID > latestKeyID {
				latestKeyID = k.ID
				p.RelayModel = strings.TrimSpace(k.RelayModel)
			}
		}
		if p.RelayModel == "" {
			// 严格读钱包设置行，不走 relay.Get：它的"无行 → 系统默认"链路里的
			// SystemDefaultModel 会把 system_settings 查询故障折叠成内置常量，
			// 展示路径必须区分"没配"和"查失败"（计费主链路保留原宽松语义）。
			var srow model.GatewayRelaySetting
			sErr := store.DB.Where("wallet_id = ?", w.ID).First(&srow).Error
			switch {
			case sErr == nil:
				p.RelayModel = strings.TrimSpace(srow.DefaultModel)
			case errors.Is(sErr, gorm.ErrRecordNotFound):
				def, dErr := gatewayRelayService().SystemDefaultModelStrict()
				if dErr != nil {
					return nil, dErr
				}
				p.RelayModel = strings.TrimSpace(def)
			default:
				return nil, sErr
			}
		}
	}

	p.StateKnown = gatewayAgentStatesKnown(ownerID, []int64{agentID})[agentID]
	return p, nil
}
