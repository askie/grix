package service

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const agentClientTypesSystemSettingKey = "agent_client_types"

// AgentClientTypeSettingItem is one row for the admin settings UI.
type AgentClientTypeSettingItem struct {
	Type    string `json:"type"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
}

// AgentClientTypesAdminView is the GET payload for admin settings.
type AgentClientTypesAdminView struct {
	Items           []AgentClientTypeSettingItem `json:"items"`
	AllEnabled      bool                         `json:"all_enabled"`
	ConfiguredTypes []string                     `json:"configured_types"`
}

func GetAgentClientTypesAdminView() (*AgentClientTypesAdminView, error) {
	settings, err := systemsetting.GetAgentClientTypesSettings()
	if err != nil {
		return nil, err
	}
	configured := append([]string(nil), settings.Enabled...)
	allEnabled := len(configured) == 0
	enabledSet := make(map[string]struct{}, len(configured))
	for _, t := range configured {
		enabledSet[t] = struct{}{}
	}

	known := model.KnownAgentClientTypes()
	items := make([]AgentClientTypeSettingItem, 0, len(known))
	for _, clientType := range known {
		enabled := allEnabled
		if !allEnabled {
			_, enabled = enabledSet[clientType]
		}
		items = append(items, AgentClientTypeSettingItem{
			Type:    clientType,
			Label:   systemsetting.AgentClientTypeDisplayName(clientType),
			Enabled: enabled,
		})
	}
	return &AgentClientTypesAdminView{
		Items:           items,
		AllEnabled:      allEnabled,
		ConfiguredTypes: configured,
	}, nil
}

// UpdateAgentClientTypesSettings validates and persists the enabled list.
// At least one valid client_type is required.
func UpdateAgentClientTypesSettings(
	adminID int64,
	enabled []string,
	clientIP, userAgent string,
) error {
	normalized, err := validateAgentClientTypesEnabled(enabled)
	if err != nil {
		return err
	}
	settings := systemsetting.AgentClientTypesSettings{Enabled: normalized}

	err = store.DB.Transaction(func(tx *gorm.DB) error {
		raw, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		updatedBy := adminID
		row := model.SystemSetting{
			Key:       agentClientTypesSystemSettingKey,
			Value:     datatypes.JSON(raw),
			UpdatedBy: &updatedBy,
		}
		if err := tx.Where("key = ?", row.Key).Assign(row).FirstOrCreate(&row).Error; err != nil {
			return err
		}
		return recordOperationTx(
			tx,
			adminID,
			"agent_client_types_settings_update",
			"system_setting",
			agentClientTypesSystemSettingKey,
			settings,
			clientIP,
			userAgent,
		)
	})
	if err != nil {
		return err
	}
	systemsetting.InvalidateAgentClientTypesSettingsCache()
	return nil
}

func validateAgentClientTypesEnabled(enabled []string) ([]string, error) {
	if len(enabled) == 0 {
		return nil, errors.New("至少启用一种智能体类型")
	}
	seen := make(map[string]struct{}, len(enabled))
	for _, raw := range enabled {
		value := model.NormalizeAgentClientType(raw)
		if value == "" {
			continue
		}
		if !model.IsValidAgentClientType(value) {
			return nil, fmt.Errorf("不支持的智能体类型: %s", raw)
		}
		seen[value] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, errors.New("至少启用一种智能体类型")
	}
	settings := systemsetting.NormalizeAgentClientTypesSettings(systemsetting.AgentClientTypesSettings{
		Enabled: enabled,
	})
	return settings.Enabled, nil
}
