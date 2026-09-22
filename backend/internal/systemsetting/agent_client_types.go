package systemsetting

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const agentClientTypesSettingKey = "agent_client_types"
const agentClientTypesSettingsCacheTTL = time.Minute

var agentClientTypesSettingsNow = time.Now

var agentClientTypesSettingsCache struct {
	mu        sync.RWMutex
	value     AgentClientTypesSettings
	expiresAt time.Time
	loaded    bool
}

// AgentClientTypesSettings stores the deployment-enabled agent client_type list.
// Empty Enabled (or missing system_settings row) means all known types are enabled.
type AgentClientTypesSettings struct {
	Enabled []string `json:"enabled"`
}

func DefaultAgentClientTypesSettings() AgentClientTypesSettings {
	return AgentClientTypesSettings{}
}

func GetAgentClientTypesSettings() (AgentClientTypesSettings, error) {
	now := agentClientTypesSettingsNow()
	if settings, ok := getAgentClientTypesSettingsFromCache(now); ok {
		return settings, nil
	}

	settings := DefaultAgentClientTypesSettings()
	if store.DB == nil {
		settings = NormalizeAgentClientTypesSettings(settings)
		setAgentClientTypesSettingsCache(settings, now)
		return settings, nil
	}
	var row model.SystemSetting
	if err := store.DB.First(&row, "key = ?", agentClientTypesSettingKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			settings = NormalizeAgentClientTypesSettings(settings)
			setAgentClientTypesSettingsCache(settings, now)
			return settings, nil
		}
		return AgentClientTypesSettings{}, err
	}
	if len(row.Value) > 0 {
		if err := json.Unmarshal(row.Value, &settings); err != nil {
			return AgentClientTypesSettings{}, err
		}
	}
	settings = NormalizeAgentClientTypesSettings(settings)
	setAgentClientTypesSettingsCache(settings, now)
	return settings, nil
}

func SaveAgentClientTypesSettings(settings AgentClientTypesSettings, updatedBy *int64) error {
	settings = NormalizeAgentClientTypesSettings(settings)

	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	row := model.SystemSetting{
		Key:       agentClientTypesSettingKey,
		Value:     datatypes.JSON(raw),
		UpdatedBy: updatedBy,
	}
	if err := store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}

	setAgentClientTypesSettingsCache(settings, agentClientTypesSettingsNow())
	return nil
}

func InvalidateAgentClientTypesSettingsCache() {
	agentClientTypesSettingsCache.mu.Lock()
	agentClientTypesSettingsCache.loaded = false
	agentClientTypesSettingsCache.expiresAt = time.Time{}
	agentClientTypesSettingsCache.value = AgentClientTypesSettings{}
	agentClientTypesSettingsCache.mu.Unlock()
}

func getAgentClientTypesSettingsFromCache(now time.Time) (AgentClientTypesSettings, bool) {
	agentClientTypesSettingsCache.mu.RLock()
	defer agentClientTypesSettingsCache.mu.RUnlock()

	if !agentClientTypesSettingsCache.loaded {
		return AgentClientTypesSettings{}, false
	}
	if now.After(agentClientTypesSettingsCache.expiresAt) {
		return AgentClientTypesSettings{}, false
	}
	return cloneAgentClientTypesSettings(agentClientTypesSettingsCache.value), true
}

func setAgentClientTypesSettingsCache(settings AgentClientTypesSettings, now time.Time) {
	agentClientTypesSettingsCache.mu.Lock()
	agentClientTypesSettingsCache.value = cloneAgentClientTypesSettings(settings)
	agentClientTypesSettingsCache.expiresAt = now.Add(agentClientTypesSettingsCacheTTL)
	agentClientTypesSettingsCache.loaded = true
	agentClientTypesSettingsCache.mu.Unlock()
}

func cloneAgentClientTypesSettings(settings AgentClientTypesSettings) AgentClientTypesSettings {
	out := AgentClientTypesSettings{}
	if len(settings.Enabled) == 0 {
		return out
	}
	out.Enabled = append([]string(nil), settings.Enabled...)
	return out
}

// NormalizeAgentClientTypesSettings keeps only valid non-empty client types,
// dedupes, and sorts for stable storage.
func NormalizeAgentClientTypesSettings(settings AgentClientTypesSettings) AgentClientTypesSettings {
	if len(settings.Enabled) == 0 {
		return AgentClientTypesSettings{}
	}
	seen := make(map[string]struct{}, len(settings.Enabled))
	normalized := make([]string, 0, len(settings.Enabled))
	for _, raw := range settings.Enabled {
		value := model.NormalizeAgentClientType(raw)
		if value == "" || !model.IsValidAgentClientType(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return AgentClientTypesSettings{Enabled: normalized}
}

// IsAgentClientTypeEnabled reports whether clientType may be newly created /
// selected in this deployment. Empty clientType is treated as enabled (legacy).
// Missing/empty setting → all known types enabled.
func IsAgentClientTypeEnabled(clientType string) (bool, error) {
	normalized := model.NormalizeAgentClientType(clientType)
	if normalized == "" {
		return true, nil
	}
	settings, err := GetAgentClientTypesSettings()
	if err != nil {
		return false, err
	}
	if len(settings.Enabled) == 0 {
		return model.IsValidAgentClientType(normalized), nil
	}
	for _, enabled := range settings.Enabled {
		if enabled == normalized {
			return true, nil
		}
	}
	return false, nil
}

// EnabledAgentClientTypeSet returns the set of enabled client types.
// When the setting is empty/missing, every known non-empty type is returned.
func EnabledAgentClientTypeSet() (map[string]struct{}, error) {
	settings, err := GetAgentClientTypesSettings()
	if err != nil {
		return nil, err
	}
	known := model.KnownAgentClientTypes()
	if len(settings.Enabled) == 0 {
		out := make(map[string]struct{}, len(known))
		for _, t := range known {
			out[t] = struct{}{}
		}
		return out, nil
	}
	out := make(map[string]struct{}, len(settings.Enabled))
	for _, t := range settings.Enabled {
		out[t] = struct{}{}
	}
	return out, nil
}

// AgentClientTypeDisplayName returns a short UI label for admin/settings.
func AgentClientTypeDisplayName(clientType string) string {
	switch model.NormalizeAgentClientType(clientType) {
	case model.AgentClientTypeCodex:
		return "Codex"
	case model.AgentClientTypeClaude:
		return "Claude"
	case model.AgentClientTypeGemini:
		return "Gemini"
	case model.AgentClientTypeHermes:
		return "Hermes"
	case model.AgentClientTypeOpenClaw:
		return "OpenClaw"
	case model.AgentClientTypeQwen:
		return "Qwen"
	case model.AgentClientTypePi:
		return "Pi"
	case model.AgentClientTypeOpenHuman:
		return "OpenHuman"
	case model.AgentClientTypeCursor:
		return "Cursor"
	case model.AgentClientTypeReasonix:
		return "Reasonix"
	case model.AgentClientTypeCodeWhale:
		return "CodeWhale"
	case model.AgentClientTypeOpenCode:
		return "OpenCode"
	case model.AgentClientTypeDeveco:
		return "DevEco Code"
	case model.AgentClientTypeKiro:
		return "Kiro"
	case model.AgentClientTypeCopilot:
		return "GitHub Copilot"
	case model.AgentClientTypeAgy:
		return "Antigravity"
	case model.AgentClientTypeKimi:
		return "Kimi"
	case model.AgentClientTypeDeepSeek:
		return "DeepSeek Harness"
	case model.AgentClientTypeQoderCLI:
		return "Qoder CLI"
	case model.AgentClientTypeQoderCLICN:
		return "Qoder CLI CN"
	case model.AgentClientTypeMCode:
		return "MiniMax Code"
	case model.AgentClientTypeDim:
		return "DimAgent"
	case model.AgentClientTypeTraeCli:
		return "TraeCLI"
	case model.AgentClientTypeOmp:
		return "Oh-My-Pi"
	case model.AgentClientTypeCodeBuddy:
		return "CodeBuddy"
	case model.AgentClientTypeGrok:
		return "Grok"
	case model.AgentClientTypeQwenPaw:
		return "QwenPaw"
	case model.AgentClientTypeZeroClaw:
		return "ZeroClaw"
	case model.AgentClientTypeACP:
		return "ACP Agent"
	default:
		return strings.TrimSpace(clientType)
	}
}
