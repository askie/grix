package featuregate

import (
	"errors"
	"fmt"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

var validStatuses = map[string]bool{
	model.FeatureStatusDisabled:  true,
	model.FeatureStatusWhitelist: true,
	model.FeatureStatusEnabled:   true,
}

// publicOnlyKeys are checked by unauthenticated clients (login/register pages).
// Whitelist mode requires a user ID, which is unavailable pre-login, so these
// keys must only be globally enabled or disabled.
var publicOnlyKeys = map[string]bool{
	"region_select":     true,
	"auth_register":     true,
	"auth_google_login": true,
	"auth_apple_login":  true,
}

// FeatureAgentAccessGate 控制访问门禁对非 Claude API agent 的生效范围
// （enabled=全量，whitelist=按 agent 主人灰度，disabled/未创建=仅 Claude）。
const FeatureAgentAccessGate = "agent_access_gate"

// BuiltinFeature defines a feature gate that is hardcoded in the frontend/backend.
type BuiltinFeature struct {
	Key         string
	DisplayName string
}

// BuiltinFeatures is the canonical list of features that can be toggled.
// Keep this in sync with frontend FeatureFlagService usage.
var BuiltinFeatures = []BuiltinFeature{
	{Key: "voice_call", DisplayName: "语音通话"},
	{Key: "voice_delegate", DisplayName: "语音托管"},
	{Key: "voice_command", DisplayName: "语音对话"},
	{Key: "agent_voice_llm", DisplayName: "Agent 语音大模型"},
	{Key: "voice_brain", DisplayName: "语音大脑"},
	{Key: "region_select", DisplayName: "区域选择"},
	{Key: "agent_share", DisplayName: "Agent 共享"},
	{Key: "conversation_audit", DisplayName: "对话审计"},
	{Key: "customer_coach", DisplayName: "客服主动引导"},
	{Key: FeatureAgentAccessGate, DisplayName: "Agent 访问门禁(非Claude)"},
}

// AvailableFeatures returns builtin features that do not yet exist in the database.
func AvailableFeatures() ([]BuiltinFeature, error) {
	existing, err := ListGates()
	if err != nil {
		return nil, err
	}
	used := make(map[string]bool, len(existing))
	for _, g := range existing {
		used[g.Key] = true
	}
	var available []BuiltinFeature
	for _, f := range BuiltinFeatures {
		if !used[f.Key] {
			available = append(available, f)
		}
	}
	return available, nil
}

// IsValidStatus checks if a status string is a valid feature gate status.
func IsValidStatus(status string) bool {
	return validStatuses[status]
}

// IsPublicOnly reports whether a feature key must be globally enabled/disabled
// and cannot use whitelist mode (evaluated before user login; no user ID available).
func IsPublicOnly(key string) bool {
	return publicOnlyKeys[key]
}

// CreateGate creates a new feature gate.
func CreateGate(key, displayName, status string) (*model.FeatureGate, error) {
	if !validStatuses[status] {
		return nil, fmt.Errorf("invalid feature gate status: %s", status)
	}
	if status == model.FeatureStatusWhitelist && publicOnlyKeys[key] {
		return nil, fmt.Errorf("feature '%s' is checked before login; whitelist mode is not supported for it", key)
	}
	gate := &model.FeatureGate{
		Key:         key,
		DisplayName: displayName,
		Status:      status,
	}
	if err := store.DB.Create(gate).Error; err != nil {
		return nil, fmt.Errorf("create feature gate: %w", err)
	}
	InvalidateCache()
	return gate, nil
}

// GetGate retrieves a feature gate by key.
func GetGate(key string) (*model.FeatureGate, error) {
	var gate model.FeatureGate
	if err := store.DB.First(&gate, "key = ?", key).Error; err != nil {
		return nil, fmt.Errorf("get feature gate %s: %w", key, err)
	}
	return &gate, nil
}

// ListGates returns all feature gates.
func ListGates() ([]model.FeatureGate, error) {
	var gates []model.FeatureGate
	if err := store.DB.Order("key ASC").Find(&gates).Error; err != nil {
		return nil, fmt.Errorf("list feature gates: %w", err)
	}
	return gates, nil
}

// UpdateGateStatus updates the status of a feature gate.
func UpdateGateStatus(key, status string) error {
	if !validStatuses[status] {
		return fmt.Errorf("invalid feature gate status: %s", status)
	}
	if status == model.FeatureStatusWhitelist && publicOnlyKeys[key] {
		return fmt.Errorf("feature '%s' is checked before login; whitelist mode is not supported for it", key)
	}
	result := store.DB.Model(&model.FeatureGate{}).Where("key = ?", key).Update("status", status)
	if result.Error != nil {
		return fmt.Errorf("update feature gate %s: %w", key, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("feature gate %s not found", key)
	}
	return nil
}

// DeleteGate deletes a feature gate and its whitelist.
func DeleteGate(key string) error {
	return store.DB.Transaction(func(tx *gorm.DB) error {
		// Delete whitelist entries first (explicit, works on all DB drivers)
		if err := tx.Where("feature_key = ?", key).Delete(&model.FeatureGateUser{}).Error; err != nil {
			return fmt.Errorf("delete whitelist for %s: %w", key, err)
		}
		result := tx.Where("key = ?", key).Delete(&model.FeatureGate{})
		if result.Error != nil {
			return fmt.Errorf("delete feature gate %s: %w", key, result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("feature gate %s not found", key)
		}
		return nil
	})
}

// AddUsersToWhitelist adds users to a feature gate's whitelist.
// Idempotent — adding an existing user is a no-op.
func AddUsersToWhitelist(featureKey string, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	if publicOnlyKeys[featureKey] {
		return fmt.Errorf("feature '%s' is a public gate; whitelist management is not supported", featureKey)
	}
	var entries []model.FeatureGateUser
	for _, uid := range userIDs {
		entries = append(entries, model.FeatureGateUser{
			FeatureKey: featureKey,
			UserID:     uid,
		})
	}
	// Use clause.OnConflict to make it idempotent (do nothing on conflict)
	if err := store.DB.Create(&entries).Error; err != nil {
		// SQLite doesn't support ON CONFLICT on composite PK well;
		// fallback: create one by one, skip existing
		for _, entry := range entries {
			var existing model.FeatureGateUser
			err := store.DB.Where("feature_key = ? AND user_id = ?", entry.FeatureKey, entry.UserID).First(&existing).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if err := store.DB.Create(&entry).Error; err != nil {
					return fmt.Errorf("add user %d to whitelist %s: %w", entry.UserID, entry.FeatureKey, err)
				}
			} else if err != nil {
				return fmt.Errorf("check whitelist %s user %d: %w", entry.FeatureKey, entry.UserID, err)
			}
		}
	}
	return nil
}

// RemoveUsersFromWhitelist removes users from a feature gate's whitelist.
// Removing a non-existent user is a no-op.
func RemoveUsersFromWhitelist(featureKey string, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	return store.DB.Where("feature_key = ? AND user_id IN ?", featureKey, userIDs).
		Delete(&model.FeatureGateUser{}).Error
}

// GetWhitelistUsers returns all user IDs in a feature gate's whitelist.
func GetWhitelistUsers(featureKey string) ([]model.FeatureGateUser, error) {
	var users []model.FeatureGateUser
	if err := store.DB.Where("feature_key = ?", featureKey).Find(&users).Error; err != nil {
		return nil, fmt.Errorf("get whitelist for %s: %w", featureKey, err)
	}
	return users, nil
}

// CountWhitelistUsersByGate returns a map of feature_key -> user count in a single query.
func CountWhitelistUsersByGate() (map[string]int, error) {
	type countRow struct {
		FeatureKey string
		Count      int
	}
	var rows []countRow
	if err := store.DB.Model(&model.FeatureGateUser{}).
		Select("feature_key, count(*) as count").
		Group("feature_key").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("count whitelist users by gate: %w", err)
	}
	result := make(map[string]int, len(rows))
	for _, r := range rows {
		result[r.FeatureKey] = r.Count
	}
	return result, nil
}

// EvaluateUserFeatures returns the list of feature keys visible to a given user.
func EvaluateUserFeatures(userID int64) ([]string, error) {
	gates, err := ListGates()
	if err != nil {
		return nil, err
	}
	if len(gates) == 0 {
		return nil, nil
	}

	// Collect whitelist gates that need user check
	whitelistKeys := make([]string, 0)
	for _, g := range gates {
		if g.Status == model.FeatureStatusWhitelist {
			whitelistKeys = append(whitelistKeys, g.Key)
		}
	}

	// Batch query whitelist for this user
	whitelistedKeys := make(map[string]bool)
	if len(whitelistKeys) > 0 {
		var entries []model.FeatureGateUser
		if err := store.DB.Where("feature_key IN ? AND user_id = ?", whitelistKeys, userID).Find(&entries).Error; err != nil {
			return nil, fmt.Errorf("evaluate user features: %w", err)
		}
		for _, e := range entries {
			whitelistedKeys[e.FeatureKey] = true
		}
	}

	// Build result
	var features []string
	for _, g := range gates {
		switch g.Status {
		case model.FeatureStatusEnabled:
			features = append(features, g.Key)
		case model.FeatureStatusWhitelist:
			if whitelistedKeys[g.Key] {
				features = append(features, g.Key)
			}
			// disabled: skip
		}
	}
	return features, nil
}
