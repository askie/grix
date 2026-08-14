package service

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"encoding/json"
)

var voiceModelIDPattern = regexp.MustCompile(`[^a-z0-9_]+`)

// GetVoiceModelsSettings 读取语音模型清单（含停用项，供塘主管理）。
func GetVoiceModelsSettings() (systemsetting.VoiceModelsSettings, error) {
	return systemsetting.GetVoiceModelsSettings()
}

// UpdateVoiceModelsSettings 校验并保存语音模型清单，写操作审计 + 失效缓存。
func UpdateVoiceModelsSettings(adminID int64, settings systemsetting.VoiceModelsSettings, clientIP, userAgent string) error {
	normalized, err := normalizeVoiceModelsSettings(settings)
	if err != nil {
		return err
	}

	err = store.DB.Transaction(func(tx *gorm.DB) error {
		raw, err := json.Marshal(normalized)
		if err != nil {
			return err
		}
		updatedBy := adminID
		row := model.SystemSetting{
			Key:       "voice_models",
			Value:     datatypes.JSON(raw),
			UpdatedBy: &updatedBy,
		}
		if err := tx.Where("key = ?", row.Key).Assign(row).FirstOrCreate(&row).Error; err != nil {
			return err
		}
		return recordOperationTx(tx, adminID, "voice_models_update", "system_setting", "voice_models", normalized, clientIP, userAgent)
	})
	if err != nil {
		return err
	}
	systemsetting.InvalidateVoiceModelsCache()
	return nil
}

// normalizeVoiceModelsSettings 裁剪并逐项校验清单；ID 缺失时由 provider+model 生成。
func normalizeVoiceModelsSettings(settings systemsetting.VoiceModelsSettings) (systemsetting.VoiceModelsSettings, error) {
	out := systemsetting.VoiceModelsSettings{Options: make([]systemsetting.VoiceModelOption, 0, len(settings.Options))}
	seenID := map[string]bool{}
	for i := range settings.Options {
		opt := settings.Options[i]
		opt.Label = strings.TrimSpace(opt.Label)
		opt.Provider = strings.TrimSpace(opt.Provider)
		opt.Model = strings.TrimSpace(opt.Model)
		opt.Endpoint = strings.TrimSpace(opt.Endpoint)
		opt.ID = strings.TrimSpace(opt.ID)

		if opt.Label == "" {
			return settings, fmt.Errorf("第 %d 项缺少显示名", i+1)
		}
		if !systemsetting.IsSupportedVoiceProvider(opt.Provider) {
			return settings, fmt.Errorf("第 %d 项供应商不受支持（当前支持 %s）", i+1, strings.Join(systemsetting.SupportedVoiceProviders(), " / "))
		}
		if opt.Model == "" {
			return settings, fmt.Errorf("第 %d 项缺少模型", i+1)
		}
		if err := validateVoiceModelEndpoint(opt.Endpoint); err != nil {
			return settings, fmt.Errorf("第 %d 项接入地址非法：%w", i+1, err)
		}
		if opt.ID == "" {
			opt.ID = makeVoiceModelID(opt.Provider, opt.Model)
		}
		if seenID[opt.ID] {
			return settings, fmt.Errorf("条目 ID 重复：%s", opt.ID)
		}
		seenID[opt.ID] = true
		opt.Sort = i
		out.Options = append(out.Options, opt)
	}
	return out, nil
}

// validateVoiceModelEndpoint 接入地址必填，且必须是 ws/wss。
func validateVoiceModelEndpoint(endpoint string) error {
	if endpoint == "" {
		return errors.New("不能为空")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return errors.New("格式错误")
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return errors.New("必须以 ws:// 或 wss:// 开头")
	}
	if u.Hostname() == "" {
		return errors.New("缺少主机名")
	}
	return nil
}

func makeVoiceModelID(provider, modelName string) string {
	base := strings.ToLower(provider + "_" + modelName)
	return strings.Trim(voiceModelIDPattern.ReplaceAllString(base, "_"), "_")
}
