package service

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInvalidSystemControl = errors.New("invalid system control")

// SystemControl is the public, ordered parameter description and current value.
// Storage and validation stay server-owned; this is not arbitrary remote config.
type SystemControl struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	ValueType   string `json:"value_type"`
	Value       any    `json:"value"`
}

type systemControlDefinition struct {
	SystemControl
	read     func(*gorm.DB) (any, error)
	validate func(json.RawMessage) (any, error)
	write    func(*gorm.DB, any) (any, error) // returns the locked previous value
}

// Add parameters here in display order, with their own typed validator and
// canonical storage implementation. Other types can use model.SystemSetting.
var systemControlDirectory = []systemControlDefinition{{
	SystemControl: SystemControl{Key: "registration_enabled", Label: "允许用户注册", Description: "允许创建新账号（邮箱、手机号、Google、Apple）；关闭后已有账号仍可登录、绑定邮箱和重置密码。", ValueType: "boolean"},
	read:          func(db *gorm.DB) (any, error) { return featuregate.ReadRegistrationEnabled(db) },
	validate: func(raw json.RawMessage) (any, error) {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, ErrInvalidSystemControl
		}
		b, ok := value.(bool)
		if !ok {
			return nil, ErrInvalidSystemControl
		}
		return b, nil
	},
	write: func(tx *gorm.DB, value any) (any, error) {
		// Atomic insert handles concurrent first saves. A new row starts disabled,
		// then is locked before reading the audit value and updating only its status.
		gate := model.FeatureGate{Key: featuregate.FeatureRegistration, DisplayName: "允许注册", Status: model.FeatureStatusDisabled}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&gate).Error; err != nil {
			return nil, err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("key = ?", gate.Key).Take(&gate).Error; err != nil {
			return nil, err
		}
		before := gate.Status == model.FeatureStatusEnabled
		status := model.FeatureStatusDisabled
		if value.(bool) {
			status = model.FeatureStatusEnabled
		}
		return before, tx.Model(&model.FeatureGate{}).Where("key = ?", gate.Key).Update("status", status).Error
	},
}}

func ListSystemControls() ([]SystemControl, error) {
	items := make([]SystemControl, 0, len(systemControlDirectory))
	for _, definition := range systemControlDirectory {
		item := definition.SystemControl
		value, err := definition.read(store.DB)
		if err != nil {
			return nil, err
		}
		item.Value = value
		items = append(items, item)
	}
	return items, nil
}

func UpdateSystemControl(adminID int64, key string, raw json.RawMessage, clientIP, userAgent string) (*SystemControl, error) {
	for _, definition := range systemControlDirectory {
		if definition.Key != key {
			continue
		}
		value, err := definition.validate(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %s requires %s value", ErrInvalidSystemControl, key, definition.ValueType)
		}
		err = store.DB.Transaction(func(tx *gorm.DB) error {
			before, err := definition.write(tx, value)
			if err != nil {
				return err
			}
			return recordOperationTx(tx, adminID, "system_control_update", "system_control", key, map[string]any{"key": key, "before": before, "after": value}, clientIP, userAgent)
		})
		if err != nil {
			return nil, err
		}
		featuregate.InvalidateCache() // UI convenience only; admission never uses it.
		item := definition.SystemControl
		item.Value = value
		return &item, nil
	}
	return nil, fmt.Errorf("%w: unknown key", ErrInvalidSystemControl)
}
