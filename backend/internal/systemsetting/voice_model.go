package systemsetting

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const voiceModelsSettingKey = "voice_models"
const voiceModelsSettingsCacheTTL = time.Minute

var voiceModelsSettingsNow = time.Now

var voiceModelsSettingsCache struct {
	mu        sync.RWMutex
	value     VoiceModelsSettings
	expiresAt time.Time
	loaded    bool
}

// supportedVoiceProviders 是语音通话链路端到端已实现的 provider 集合。
// 塘主只能在这些已开发好的供应商里挑，不能新增任意 provider。
var supportedVoiceProviders = []string{"openai_realtime", "doubao_realtime"}

// SupportedVoiceProviders 返回已支持的语音 provider 列表（供塘主下拉选择）。
func SupportedVoiceProviders() []string {
	out := make([]string, len(supportedVoiceProviders))
	copy(out, supportedVoiceProviders)
	return out
}

// IsSupportedVoiceProvider 判断 provider 是否在已开发集合内。
func IsSupportedVoiceProvider(p string) bool {
	for _, v := range supportedVoiceProviders {
		if v == p {
			return true
		}
	}
	return false
}

// VoicePreset 是一条预定义音色。
type VoicePreset struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// VoiceModelOption 是一条可供 C 端选择的语音模型条目。
// 用户在 C 端只看到 Label，选中后由 Provider/Model/Endpoint 共同决定通话链路。
type VoiceModelOption struct {
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	Provider string         `json:"provider"`
	Model    string         `json:"model"`
	Endpoint string         `json:"endpoint"`
	Enabled  bool           `json:"enabled"`
	Sort     int            `json:"sort"`
	Voices   []VoicePreset  `json:"voices,omitempty"`
}

// DefaultVoicePresets 返回指定 provider 的内置音色列表。
func DefaultVoicePresets(provider string) []VoicePreset {
	switch provider {
	case "openai_realtime":
		return []VoicePreset{
			{ID: "alloy", Label: "Alloy"},
			{ID: "ash", Label: "Ash"},
			{ID: "ballad", Label: "Ballad"},
			{ID: "coral", Label: "Coral"},
			{ID: "echo", Label: "Echo"},
			{ID: "fable", Label: "Fable"},
			{ID: "nova", Label: "Nova"},
			{ID: "onyx", Label: "Onyx"},
			{ID: "sage", Label: "Sage"},
			{ID: "shimmer", Label: "Shimmer"},
			{ID: "verse", Label: "Verse"},
		}
	case "doubao_realtime":
		return []VoicePreset{
			{ID: "cancan", Label: "灿灿（通用女声）"},
		}
	default:
		return nil
	}
}

// VoiceModelsSettings 是语音模型清单整体配置。
type VoiceModelsSettings struct {
	Options []VoiceModelOption `json:"options"`
}

// DefaultVoiceModelsSettings 返回默认清单，等价于历史写死在前端的两项，
// 保证塘主未配置时 C 端仍可正常选择（向后兼容兜底）。
func DefaultVoiceModelsSettings() VoiceModelsSettings {
	return VoiceModelsSettings{Options: []VoiceModelOption{
		{
			ID:       "openai_gpt4o_realtime",
			Label:    "OpenAI GPT Realtime",
			Provider: "openai_realtime",
			Model:    "gpt-4o-realtime-preview",
			Endpoint: "wss://api.openai.com/v1/realtime",
			Enabled:  true,
			Sort:     0,
			Voices:   DefaultVoicePresets("openai_realtime"),
		},
		{
			ID:       "doubao_realtime",
			Label:    "豆包语音大模型",
			Provider: "doubao_realtime",
			Model:    "doubao-realtime",
			Endpoint: "wss://openspeech.bytedance.com/api/v3/realtime",
			Enabled:  true,
			Sort:     1,
			Voices:   DefaultVoicePresets("doubao_realtime"),
		},
	}}
}

// GetVoiceModelsSettings 读取语音模型清单（带分钟级缓存）。
func GetVoiceModelsSettings() (VoiceModelsSettings, error) {
	now := voiceModelsSettingsNow()
	if settings, ok := getVoiceModelsSettingsFromCache(now); ok {
		return settings, nil
	}

	settings := DefaultVoiceModelsSettings()
	var row model.SystemSetting
	if err := store.DB.First(&row, "key = ?", voiceModelsSettingKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			setVoiceModelsSettingsCache(settings, now)
			return settings, nil
		}
		return VoiceModelsSettings{}, err
	}
	if len(row.Value) == 0 {
		setVoiceModelsSettingsCache(settings, now)
		return settings, nil
	}
	if err := json.Unmarshal(row.Value, &settings); err != nil {
		return VoiceModelsSettings{}, err
	}
	setVoiceModelsSettingsCache(settings, now)
	return settings, nil
}

// SaveVoiceModelsSettings 持久化语音模型清单并刷新缓存。
func SaveVoiceModelsSettings(settings VoiceModelsSettings, updatedBy *int64) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	row := model.SystemSetting{
		Key:       voiceModelsSettingKey,
		Value:     datatypes.JSON(raw),
		UpdatedBy: updatedBy,
	}
	if err := store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	setVoiceModelsSettingsCache(settings, voiceModelsSettingsNow())
	return nil
}

// EnabledVoiceModelOptions 返回启用的条目，按 Sort 升序，供 C 端展示。
// 已有部署可能在 voices 字段引入前就保存了清单，此时按 provider 补上内置默认值。
func EnabledVoiceModelOptions() ([]VoiceModelOption, error) {
	settings, err := GetVoiceModelsSettings()
	if err != nil {
		return nil, err
	}
	out := make([]VoiceModelOption, 0, len(settings.Options))
	for _, opt := range settings.Options {
		if opt.Enabled {
			if len(opt.Voices) == 0 {
				opt.Voices = DefaultVoicePresets(opt.Provider)
			}
			out = append(out, opt)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort < out[j].Sort })
	return out, nil
}

// InvalidateVoiceModelsCache 失效缓存（保存后由 admin 侧调用）。
func InvalidateVoiceModelsCache() {
	voiceModelsSettingsCache.mu.Lock()
	voiceModelsSettingsCache.loaded = false
	voiceModelsSettingsCache.expiresAt = time.Time{}
	voiceModelsSettingsCache.value = VoiceModelsSettings{}
	voiceModelsSettingsCache.mu.Unlock()
}

func getVoiceModelsSettingsFromCache(now time.Time) (VoiceModelsSettings, bool) {
	voiceModelsSettingsCache.mu.RLock()
	defer voiceModelsSettingsCache.mu.RUnlock()

	if !voiceModelsSettingsCache.loaded {
		return VoiceModelsSettings{}, false
	}
	if now.After(voiceModelsSettingsCache.expiresAt) {
		return VoiceModelsSettings{}, false
	}
	return voiceModelsSettingsCache.value, true
}

func setVoiceModelsSettingsCache(settings VoiceModelsSettings, now time.Time) {
	voiceModelsSettingsCache.mu.Lock()
	voiceModelsSettingsCache.value = settings
	voiceModelsSettingsCache.expiresAt = now.Add(voiceModelsSettingsCacheTTL)
	voiceModelsSettingsCache.loaded = true
	voiceModelsSettingsCache.mu.Unlock()
}
