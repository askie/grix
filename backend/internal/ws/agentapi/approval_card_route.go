package agentapi

import (
	"context"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

// resolveApprovalCardSessionID 决定审批卡的最终投递会话。
//
// 托管代答形态：1:1 私聊里除 agent 主人外还有其他真人成员（客服会话、widget
// 访客会话——主人把 agent 托管在该会话里代答）。审批卡若落进这种会话，对端
// 客户就能看到卡片并代为批准：visible_to 只在群聊分发层强制过滤，1:1 会话
// 不过滤（send_msg.go 仅对 sessionType==2 校验并裁剪 visible_to），这是真实
// 发生过的越权批准安全隐患。
//
// 因此这种形态下一律把审批卡改投到「主人 ↔ agent」的私聊会话（不存在则按
// SessionOpenLatest 语义创建），审批动作只发生在主人自己的对话里；审批回传
// 经 direct_session_route 抵达同一 agent，卡片 msg_id 索引也按新会话登记，
// 结果卡原地编辑不受影响。其余形态（主人↔agent 私聊、群聊）返回原会话：
// 群聊分发层已对 visible_to=[owner] 强制过滤，群成员看不到卡。
func resolveApprovalCardSessionID(ctx context.Context, sessionID string, agentID, ownerID int64) string {
	if strings.TrimSpace(sessionID) == "" || agentID <= 0 || ownerID <= 0 {
		return sessionID
	}
	if !directSessionHasNonOwnerHuman(sessionID, ownerID) {
		return sessionID
	}
	resp, err := service.SessionOpenLatest(ownerID, agentID, 2)
	if err != nil || resp == nil || strings.TrimSpace(resp.SessionID) == "" {
		// 私聊会话不可用（agent 停用等）：退回原会话，卡片仍带 visible_to=[owner]。
		logger.L.Warnf(
			"approval card reroute failed, keep original session: agent=%d owner=%d session=%s err=%v",
			agentID, ownerID, sessionID, err,
		)
		return sessionID
	}
	logger.L.Infof(
		"approval card rerouted to owner-agent private session: agent=%d owner=%d from=%s to=%s",
		agentID, ownerID, sessionID, resp.SessionID,
	)
	return resp.SessionID
}

// directSessionHasNonOwnerHuman 报告 sessionID 是否为「1:1 且除 ownerID 外还有
// 其他真人成员」的会话，即托管代答（LLM proxy）形态。群聊与主人↔agent 私聊
// 均返回 false。判定基于会话成员构成而非事件上下文，因此对所有发卡路径
// （connector send_msg、codex 审批事件、claude 权限 invoke）一致生效，
// 与 agent 类型无关。
func directSessionHasNonOwnerHuman(sessionID string, ownerID int64) bool {
	if store.DB == nil || strings.TrimSpace(sessionID) == "" || ownerID <= 0 {
		return false
	}
	var sessionType int16
	if err := store.DB.Model(&model.Session{}).
		Select("session_type").
		Where("session_id = ?", sessionID).
		Scan(&sessionType).Error; err != nil {
		return false
	}
	if sessionType != model.SessionTypeDirect {
		return false
	}
	var count int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_type = 1 AND member_id != ?", sessionID, ownerID).
		Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// isApprovalFamilyCard 识别审批族卡片（审批请求卡 + 审批结果状态卡），
// 供结果回卡在兜底新发路径上同样套用改投判定。
func isApprovalFamilyCard(content string) bool {
	return strings.Contains(content, "grix://card/exec_approval") ||
		strings.Contains(content, "grix://card/exec_status")
}
