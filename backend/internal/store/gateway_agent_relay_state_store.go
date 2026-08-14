package store

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/askie/grix/backend/internal/model"
)

// ErrGatewayAgentRelayStateRevisionConflict 是 relay state 乐观锁冲突：
// 调用方带的 expected_revision 与服务端当前 revision 不一致（或预期新建的行已存在），
// 写被拒绝。service 层把它映射成 HTTP 409 + 最新 state，前端刷新后重试。
var ErrGatewayAgentRelayStateRevisionConflict = errors.New("gateway agent relay state revision conflict")

// GetGatewayAgentRelayState 取某个 Agent 的中转开关状态；无行返回 gorm.ErrRecordNotFound。
func GetGatewayAgentRelayState(agentID int64) (*model.GatewayAgentRelayState, error) {
	var row model.GatewayAgentRelayState
	if err := DB.Where("agent_id = ?", agentID).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListGatewayAgentRelayStatesByWallet 按钱包批量取回该用户所有 Agent 的中转开关状态，
// 供 GET /v1/gateway/agents 一次装配整页，避免逐 Agent 查库。
func ListGatewayAgentRelayStatesByWallet(walletID int64) ([]model.GatewayAgentRelayState, error) {
	var rows []model.GatewayAgentRelayState
	if err := DB.Where("wallet_id = ?", walletID).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// UpsertGatewayAgentRelayStateDesired 写入期望态（enabled/relay_model），revision 乐观锁：
//   - expectedRevision 非 nil：仅当当前 revision 一致时才写（行不存在视为可新建，
//     前端拿不到 revision 就不会传），revision+1；不一致返回
//     ErrGatewayAgentRelayStateRevisionConflict，调用方读最新 state 回给前端。
//   - expectedRevision 为 nil：last-write-wins，多端同时操作以版本号兜底。
//
// 写成功后返回库里的最新行。
func UpsertGatewayAgentRelayStateDesired(agentID, walletID int64, enabled bool, relayModel string, expectedRevision *int64) (*model.GatewayAgentRelayState, error) {
	now := time.Now().UTC()

	if expectedRevision != nil {
		// 原子 UPDATE 把"比对 revision"和"写入"合成一条语句，避免读-改-写竞态。
		res := DB.Model(&model.GatewayAgentRelayState{}).
			Where("agent_id = ? AND revision = ?", agentID, *expectedRevision).
			Updates(map[string]any{
				"wallet_id":   walletID,
				"enabled":     enabled,
				"relay_model": relayModel,
				"revision":    gorm.Expr("revision + 1"),
				// desired 一变，旧 actual 即失效：先回到"待生效"，等 connector 回执再写回。
				"applied":    false,
				"applied_at": nil,
				"updated_at": now,
			})
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 0 {
			var exists int64
			if err := DB.Model(&model.GatewayAgentRelayState{}).
				Where("agent_id = ?", agentID).Count(&exists).Error; err != nil {
				return nil, err
			}
			if exists > 0 {
				return nil, ErrGatewayAgentRelayStateRevisionConflict
			}
			// 行不存在：按新建设（首次从旧端迁过来的写没有可冲突的版本）。
			row := model.GatewayAgentRelayState{
				AgentID: agentID, WalletID: walletID, Enabled: enabled,
				RelayModel: relayModel, Revision: 1, CreatedAt: now, UpdatedAt: now,
			}
			if err := DB.Create(&row).Error; err != nil {
				// 并发下另一请求抢先建了同一 agent 的行：按冲突处理，让调用方重读。
				return nil, ErrGatewayAgentRelayStateRevisionConflict
			}
			return &row, nil
		}
		return GetGatewayAgentRelayState(agentID)
	}

	// last-write-wins：insert-or-update，已存在时 revision 在库内自增。
	row := model.GatewayAgentRelayState{
		AgentID: agentID, WalletID: walletID, Enabled: enabled,
		RelayModel: relayModel, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "agent_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"wallet_id":   walletID,
			"enabled":     enabled,
			"relay_model": relayModel,
			"revision":    gorm.Expr("gateway_agent_relay_state.revision + 1"),
			// desired 一变，旧 actual 即失效：先回到"待生效"，等 connector 回执再写回。
			"applied":    false,
			"applied_at": nil,
			"updated_at": now,
		}),
	}).Create(&row).Error; err != nil {
		return nil, err
	}
	return GetGatewayAgentRelayState(agentID)
}

// SetGatewayAgentRelayStateApplied 写回 actual：connector 回执（M2 的 relay_state_report /
// local_action_result）携带的实际态与回执时间。只更新已有行——没有 desired 的 agent
// 不存在可展示的"已生效"，回执直接丢弃。
func SetGatewayAgentRelayStateApplied(agentID int64, applied bool, appliedAt time.Time) error {
	res := DB.Model(&model.GatewayAgentRelayState{}).
		Where("agent_id = ?", agentID).
		Updates(map[string]any{
			"applied":    applied,
			"applied_at": appliedAt.UTC(),
			"updated_at": time.Now().UTC(),
		})
	return res.Error
}
