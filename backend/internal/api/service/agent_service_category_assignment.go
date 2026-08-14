package service

import (
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
)

// AgentAssignCategory updates one owned agent's category binding.
// categoryID=0 clears the current category.
func AgentAssignCategory(ownerID, agentID, categoryID int64) (*AgentResp, *errcode.ErrCode) {
	if ec := CheckOwnerCategory(ownerID, categoryID); ec != nil {
		return nil, ec
	}

	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		return nil, &errcode.ErrAgentNotFound
	}
	if agent.OwnerID != ownerID {
		return nil, &errcode.ErrAgentForbidden
	}
	if agent.Status == model.AgentStatusDeleted {
		return nil, &errcode.ErrAgentNotFound
	}

	if err := store.DB.Model(&agent).Updates(map[string]any{
		"category_id": categoryID,
		"updated_at":  time.Now(),
	}).Error; err != nil {
		return nil, internalAgentErr("更新 Agent 分类失败", err)
	}

	store.DB.First(&agent, agentID)
	resp := agentToResp(&agent, ownerID)
	return &resp, nil
}
