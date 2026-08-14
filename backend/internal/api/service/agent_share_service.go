package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
)

// agentShareGateKey 与前端、admin 内置目录保持一致。
const agentShareGateKey = "agent_share"

// userHasAgentShareGate 校验「Agent 共享」feature gate 是否对该用户开启。
// 后端最终把关:即便绕过前端按钮直调 API(curl/老版/其他客户端),没开 gate 也无法创建/撤销共享。
// 兜底:gate 评估失败时按「关闭」处理(fail-closed,符合"默认关"语义)。
func userHasAgentShareGate(userID int64) bool {
	if userID <= 0 {
		return false
	}
	features, err := featuregate.GetUserFeatures(userID)
	if err != nil {
		// fail-closed 但记 warn 日志，便于运维发现 gate 评估系统故障；
		// 否则共享接口在 gate 评估失败时全部静默 403，问题难排查。
		logger.L.Warnf("evaluate agent_share gate failed user=%d err=%v (fail-closed)", userID, err)
		return false
	}
	for _, k := range features {
		if k == agentShareGateKey {
			return true
		}
	}
	return false
}

// notifyAgentShareChanged 把共享变更广播到该 agent 当前所有 owner 连接所在的 ws 节点。
// 撤销共享时,被踢的连接可能在与主连接不同的节点(多 ws 节点 + LB 散开),
// 只发主路由节点会让另一节点上失授权的连接残留;故扫 owner 集合并去重后发到每一个相关节点。
// agent 不在线时静默跳过——主连接重连时会全量下发。
func notifyAgentShareChanged(agentID int64) {
	if store.RDB == nil || agentID <= 0 {
		return
	}
	ctx := context.Background()
	nodes := loadAgentRouteAllNodesForShareSync(ctx, agentID)
	if len(nodes) == 0 {
		return
	}
	payload, _ := json.Marshal(protocol.AgentShareSyncPayload{AgentID: agentID})
	envelope, _ := json.Marshal(map[string]interface{}{
		"cmd":     protocol.RedisCmdAgentShareSync,
		"payload": json.RawMessage(payload),
	})
	for _, node := range nodes {
		if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", node), envelope).Err(); err != nil {
			logger.L.Warnf("publish agent share sync failed agent=%d node=%s err=%v", agentID, node, err)
		}
	}
}

// loadAgentRouteAllNodesForShareSync 复刻 agentapi.loadAgentRouteAllNodes 的语义(service 层无 import agentapi)。
// 优先扫 owner 集合(im:agent_api:route_owners:{agentID})拿到每个 owner 的所在节点,再补主路由节点兜底。
func loadAgentRouteAllNodesForShareSync(ctx context.Context, agentID int64) []string {
	if agentID <= 0 || store.RDB == nil {
		return nil
	}
	seen := make(map[string]struct{}, 4)
	out := make([]string, 0, 4)
	add := func(node string) {
		node = strings.TrimSpace(node)
		if node == "" {
			return
		}
		if _, dup := seen[node]; dup {
			return
		}
		seen[node] = struct{}{}
		out = append(out, node)
	}
	// 主路由(老路径,所有 agent 都会写)
	if main, err := store.RDB.Get(ctx, fmt.Sprintf("im:agent_api:route:%d", agentID)).Result(); err == nil {
		add(main)
	}
	// owner 维度路由(共享场景的关键)
	if owners, err := store.RDB.SMembers(ctx, fmt.Sprintf("im:agent_api:route_owners:%d", agentID)).Result(); err == nil {
		for _, ownerStr := range owners {
			node, gerr := store.RDB.Get(ctx, fmt.Sprintf("im:agent_api:route:%d:%s", agentID, ownerStr)).Result()
			if gerr != nil {
				continue
			}
			add(node)
		}
	}
	return out
}

// CanUseAgent 是 canUseAgent 的导出版，供 ws 层在消息发送/托管链路做运行时授权校验
// （主人或有效被共享者）。撤销共享后此处即时返回 false，无需等连接断开。
func CanUseAgent(userID, agentID int64) (bool, error) {
	return canUseAgent(userID, agentID)
}

// canUseAgent 判断 userID 是否有权「使用」该 agent（建私聊/拉群/托管）：
// agent 主人本人，或被该 agent 有效共享的活跃账户。仅含「使用」，不含「管理」（改配置/删除仍只主人）。
// 被共享者一旦封号/注销，共享即时失效（无需 agent 主人手动撤销）。
func canUseAgent(userID, agentID int64) (bool, error) {
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id", "status").First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if agent.Status != model.AgentStatusActive {
		return false, nil
	}
	if agent.OwnerID == userID {
		return true, nil
	}
	return hasActiveAgentShare(agentID, userID)
}

// hasActiveAgentShare 判断 (agentID, sharedTo) 是否存在有效共享。
// 「有效」=共享行 status=1 且未过期，且 sharedTo 本人是活跃账户。
// 用户封号/注销时不依赖任何下线任务，下一次校验即返 false。
func hasActiveAgentShare(agentID, sharedTo int64) (bool, error) {
	var share model.AgentShare
	err := store.DB.Where(
		"agent_id = ? AND shared_to = ? AND status = ?",
		agentID, sharedTo, model.AgentShareStatusActive,
	).First(&share).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if share.ExpiresAt != nil && share.ExpiresAt.Before(time.Now()) {
		return false, nil
	}
	return sharedToUserActive(sharedTo)
}

// sharedToUserActive 校验被共享者是活跃账户（封号/注销视为共享自动失效）。
func sharedToUserActive(userID int64) (bool, error) {
	var u model.User
	if err := store.DB.Select("id", "status").Where("id = ?", userID).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return u.Status == model.UserStatusActive, nil
}

// loadOwnedActiveAgent 校验 agentID 属于 ownerID 且有效，返回 agent。
func loadOwnedActiveAgent(ownerID, agentID int64) (*model.Agent, *errcode.ErrCode) {
	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		return nil, &errcode.ErrAgentNotFound
	}
	if agent.OwnerID != ownerID {
		return nil, &errcode.ErrAgentForbidden
	}
	if agent.Status != model.AgentStatusActive {
		return nil, &errcode.ErrAgentNotFound
	}
	return &agent, nil
}

// AgentShareCreate 由 agent 主人把 agent 共享给某账户（幂等：已有有效共享则直接返回）。
// 被共享者后续由其设备/connector 用「主人 api_key + shared_owner_id」建立独立连接，无需单独凭据。
func AgentShareCreate(ownerID, agentID, sharedTo int64) *errcode.ErrCode {
	// feature gate 终极把关:绕过前端按钮直调 API(curl/老版/其他客户端)也无法越过 gate。
	if !userHasAgentShareGate(ownerID) {
		return &errcode.ErrAgentForbidden
	}
	if sharedTo <= 0 || sharedTo == ownerID {
		return &errcode.ErrBadRequest
	}
	if _, e := loadOwnedActiveAgent(ownerID, agentID); e != nil {
		return e
	}
	// 被共享者必须是有效账户：防止把 agent 共享给不存在/已封禁/已注销的 user_id，
	// 否则 connector 会为孤儿 user_id 维护一条永远没人用的 shared 实例，白白占资源。
	var target model.User
	if err := store.DB.Select("id", "status").
		Where("id = ? AND status = ?", sharedTo, model.UserStatusActive).
		First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &errcode.ErrBadRequest
		}
		return &errcode.ErrInternal
	}
	exists, err := hasActiveAgentShare(agentID, sharedTo)
	if err != nil {
		return &errcode.ErrInternal
	}
	if exists {
		return nil
	}
	now := time.Now()
	// 优先复活已撤销的老行，避免「共享→撤销→再共享」反复留下 status=2 孤儿行污染表。
	// (agent_id, shared_to) 的 status=1 partial unique index 已保证活跃行唯一；
	// status=2 行可能有多条历史孤儿，取最新一条复活即可，其余无害留底。
	var stale model.AgentShare
	staleErr := store.DB.
		Where("agent_id = ? AND shared_to = ? AND status = ?", agentID, sharedTo, model.AgentShareStatusRevoked).
		Order("id DESC").
		First(&stale).Error
	if staleErr == nil {
		if err := store.DB.Model(&model.AgentShare{}).
			Where("id = ?", stale.ID).
			Updates(map[string]interface{}{
				"status":     model.AgentShareStatusActive,
				"owner_id":   ownerID,
				"expires_at": nil,
				"updated_at": now,
			}).Error; err != nil {
			return &errcode.ErrInternal
		}
		notifyAgentShareChanged(agentID)
		return nil
	}
	if !errors.Is(staleErr, gorm.ErrRecordNotFound) {
		return &errcode.ErrInternal
	}
	share := model.AgentShare{
		ID:        snowflake.GenID(),
		AgentID:   agentID,
		OwnerID:   ownerID,
		SharedTo:  sharedTo,
		Status:    model.AgentShareStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.DB.Create(&share).Error; err != nil {
		return &errcode.ErrInternal
	}
	notifyAgentShareChanged(agentID)
	return nil
}

// AgentShareRevoke 撤销 agent 主人对某账户的共享。
func AgentShareRevoke(ownerID, agentID, sharedTo int64) *errcode.ErrCode {
	if _, e := loadOwnedActiveAgent(ownerID, agentID); e != nil {
		return e
	}
	if err := store.DB.Model(&model.AgentShare{}).
		Where("agent_id = ? AND shared_to = ? AND status = ?", agentID, sharedTo, model.AgentShareStatusActive).
		Update("status", model.AgentShareStatusRevoked).Error; err != nil {
		return &errcode.ErrInternal
	}
	// 通知 ws 重推 control_share_set（不含已撤销者），connector 据此断开其连接；
	// 即便被共享者抢先重连，握手时共享已失效会被拒。
	notifyAgentShareChanged(agentID)
	return nil
}

// AgentShareList 列出某 agent 当前有效的共享对象（供主人查看）。
func AgentShareList(ownerID, agentID int64) ([]model.AgentShare, *errcode.ErrCode) {
	if _, e := loadOwnedActiveAgent(ownerID, agentID); e != nil {
		return nil, e
	}
	var shares []model.AgentShare
	if err := store.DB.
		Where("agent_id = ? AND status = ?", agentID, model.AgentShareStatusActive).
		Order("created_at DESC").
		Find(&shares).Error; err != nil {
		return nil, &errcode.ErrInternal
	}
	return shares, nil
}

// AgentSharedWithMe 列出共享给 userID 的所有有效 agent（被共享者侧的「分享给我的」）。
func AgentSharedWithMe(userID int64) ([]AgentResp, error) {
	var shares []model.AgentShare
	if err := store.DB.
		Where("shared_to = ? AND status = ?", userID, model.AgentShareStatusActive).
		Order("created_at DESC").
		Find(&shares).Error; err != nil {
		return nil, err
	}
	if len(shares) == 0 {
		return []AgentResp{}, nil
	}
	agentIDs := make([]int64, 0, len(shares))
	for i := range shares {
		agentIDs = append(agentIDs, shares[i].AgentID)
	}
	var agents []model.Agent
	if err := store.DB.
		Where("id IN ? AND status = ?", agentIDs, model.AgentStatusActive).
		Find(&agents).Error; err != nil {
		return nil, err
	}
	onlineByID := loadAgentOnlineMap(context.Background(), userID)
	list := make([]AgentResp, 0, len(agents))
	for i := range agents {
		resp := agentToRespWithSecretAndOnline(&agents[i], userID, "", onlineByID[agents[i].ID])
		// 共享视角：不向被共享者暴露主人的 user_id，
		// 前端按本接口来源即可识别为「分享给我的」，不需要 owner_id 字段。
		resp.OwnerID = 0
		list = append(list, resp)
	}
	return list, nil
}
