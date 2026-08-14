package service

import (
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

// AgentSortItem represents one agent's target category and sort position.
type AgentSortItem struct {
	AgentID    int64 `json:"agent_id,string" binding:"required"`
	CategoryID int64 `json:"category_id,string"`
	SortOrder  int   `json:"sort_order"`
}

// AgentBatchSort updates category and sort order for multiple agents in one transaction.
func AgentBatchSort(userID int64, items []AgentSortItem) *errcode.ErrCode {
	if len(items) == 0 {
		return nil
	}

	agentIDs := make([]int64, 0, len(items))
	categoryIDs := make(map[int64]bool)
	for _, it := range items {
		agentIDs = append(agentIDs, it.AgentID)
		if it.CategoryID != 0 {
			categoryIDs[it.CategoryID] = true
		}
	}

	for catID := range categoryIDs {
		if ec := CheckOwnerCategory(userID, catID); ec != nil {
			return ec
		}
	}

	var agents []model.Agent
	if err := store.DB.Where("id IN ? AND owner_id = ? AND status != ?", agentIDs, userID, model.AgentStatusDeleted).Find(&agents).Error; err != nil {
		return internalAgentErr("批量查询 Agent 失败", err)
	}
	if len(agents) != len(items) {
		return &errcode.ErrAgentNotFound
	}

	now := time.Now()
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		for _, it := range items {
			fields := map[string]any{
				"category_id": it.CategoryID,
				"sort_order":  it.SortOrder,
				"updated_at":  now,
			}
			if err := tx.Model(&model.Agent{}).Where("id = ? AND owner_id = ?", it.AgentID, userID).Updates(fields).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return internalAgentErr("批量更新 Agent 排序失败", err)
	}
	return nil
}
