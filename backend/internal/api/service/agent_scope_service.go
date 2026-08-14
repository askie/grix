package service

import (
	"slices"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

type AgentScopeResp struct {
	AgentID             int64                  `json:"agent_id,string"`
	Scopes              []string               `json:"scopes"`
	AvailableScopes     []string               `json:"available_scopes"`
	AvailableScopeItems []agentscope.ScopeItem `json:"available_scope_items"`
}

func AgentScopeGet(ownerID, agentID int64, lang ...string) (*AgentScopeResp, *errcode.ErrCode) {
	if _, ec := getScopeTargetAgent(ownerID, agentID); ec != nil {
		return nil, ec
	}

	scopes, err := listAgentScopes(agentID)
	if err != nil {
		return nil, internalAgentScopeErr("查询 Agent scope 失败", err)
	}
	return &AgentScopeResp{
		AgentID:             agentID,
		Scopes:              scopes,
		AvailableScopes:     agentscope.AllowedScopes(),
		AvailableScopeItems: agentscope.AllowedScopeItems(firstLang(lang)),
	}, nil
}

func AgentScopeReplace(ownerID, agentID int64, rawScopes []string, lang ...string) (*AgentScopeResp, *errcode.ErrCode) {
	if _, ec := getScopeTargetAgent(ownerID, agentID); ec != nil {
		return nil, ec
	}

	beforeScopes, err := listAgentScopes(agentID)
	if err != nil {
		return nil, internalAgentScopeErr("查询 Agent scope 失败", err)
	}

	scopes, err := agentscope.Normalize(rawScopes)
	if err != nil {
		return nil, &errcode.ErrAgentScopeInvalid
	}

	if err := replaceAgentScopes(agentID, scopes); err != nil {
		return nil, internalAgentScopeErr("更新 Agent scope 失败", err)
	}

	hasCreateScope := slices.Contains(scopes, agentscope.ScopeAgentAPICreate)
	store.DB.Model(&model.Agent{}).Where("id = ?", agentID).
		Update("is_main", hasCreateScope)

	ownerIDRef := ownerID
	WriteAuditLog(WriteAuditLogReq{
		EventType: "agent_scope_replace",
		UserID:    &ownerIDRef,
		Detail: map[string]any{
			"target_agent_id": agentID,
			"old_scopes":      beforeScopes,
			"new_scopes":      scopes,
		},
	})

	return &AgentScopeResp{
		AgentID:             agentID,
		Scopes:              scopes,
		AvailableScopes:     agentscope.AllowedScopes(),
		AvailableScopeItems: agentscope.AllowedScopeItems(firstLang(lang)),
	}, nil
}

func firstLang(lang []string) string {
	if len(lang) == 0 {
		return "zh"
	}
	return lang[0]
}

func getScopeTargetAgent(ownerID, agentID int64) (*model.Agent, *errcode.ErrCode) {
	var agent model.Agent
	if err := store.DB.Select("id,owner_id,provider_type,status").First(&agent, agentID).Error; err != nil {
		return nil, &errcode.ErrAgentNotFound
	}
	if agent.OwnerID != ownerID {
		return nil, &errcode.ErrAgentForbidden
	}
	if agent.Status == model.AgentStatusDeleted {
		return nil, &errcode.ErrAgentNotFound
	}
	if agent.ProviderType != model.AgentProviderAPI {
		return nil, &errcode.ErrAgentScopeTargetInvalid
	}
	return &agent, nil
}

func listAgentScopes(agentID int64) ([]string, error) {
	var rows []model.AgentAPIScope
	if err := store.DB.Select("scope").
		Where("agent_id = ?", agentID).
		Order("scope ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]string, 0, len(rows))
	for _, row := range rows {
		if !agentscope.IsAllowed(row.Scope) {
			continue
		}
		result = append(result, row.Scope)
	}
	return result, nil
}

func replaceAgentScopes(agentID int64, scopes []string) error {
	return store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&model.AgentAPIScope{}).Error; err != nil {
			return err
		}

		if len(scopes) == 0 {
			return nil
		}

		now := time.Now()
		rows := make([]model.AgentAPIScope, 0, len(scopes))
		for _, scope := range scopes {
			rows = append(rows, model.AgentAPIScope{
				AgentID:   agentID,
				Scope:     scope,
				CreatedAt: now,
				UpdatedAt: now,
			})
		}
		return tx.Create(&rows).Error
	})
}

func internalAgentScopeErr(msg string, err error) *errcode.ErrCode {
	if logger.L != nil {
		logger.L.Errorf("%s: %v", msg, err)
	}
	return &errcode.ErrCode{
		HTTPStatus: 500,
		BizCode:    50001,
		Msg:        msg,
	}
}
