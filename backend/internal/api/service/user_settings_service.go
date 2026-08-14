package service

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrUserSettingsInvalidPayload       = errors.New("invalid user settings payload")
	ErrUserSettingsInvalidAgentID       = errors.New("invalid auto delegate agent id")
	ErrUserSettingsInvalidFriendAddMode = errors.New("invalid friend add setting")
	ErrUserSettingsInvalidLanguage      = errors.New("invalid preferred language")
	ErrUserSettingsAutoAgentNotFound    = errors.New("auto delegate agent not found")
	ErrUserSettingsAutoAgentNotOwned    = errors.New("auto delegate agent not owned by current user")
	ErrUserSettingsAutoAgentUnavailable = errors.New("auto delegate agent is unavailable")
	ErrUserSettingsVoiceAgentNotVoice   = errors.New("voice auto delegate agent must be a voice model")
)

type UserSettingsResp struct {
	PreferredLanguage string               `json:"preferred_language"`
	Chat              UserSettingsChatResp `json:"chat"`
}

type UserSettingsChatResp struct {
	AutoDelegateAgentID      *int64 `json:"auto_delegate_agent_id,string,omitempty"`
	VoiceAutoDelegateAgentID *int64 `json:"voice_auto_delegate_agent_id,string,omitempty"`
	VoiceBrainAgentID        *int64 `json:"voice_brain_agent_id,string,omitempty"`
	VoiceBrainRealtime       bool   `json:"voice_brain_realtime"`
	FriendAddSetting         int8   `json:"friend_add_setting"`
	AllowGroupInvite    bool   `json:"allow_group_invite"`
}

type UserSettingsUpdateReq struct {
	PreferredLanguage OptionalStringUpdate       `json:"preferred_language"`
	Chat              *UserSettingsChatUpdateReq `json:"chat"`
}

type UserSettingsChatUpdateReq struct {
	AutoDelegateAgentID      OptionalStringUpdate `json:"auto_delegate_agent_id"`
	VoiceAutoDelegateAgentID OptionalStringUpdate `json:"voice_auto_delegate_agent_id"`
	VoiceBrainAgentID        OptionalStringUpdate `json:"voice_brain_agent_id"`
	VoiceBrainRealtime       *bool                `json:"voice_brain_realtime"`
	FriendAddSetting         *int8                `json:"friend_add_setting"`
	AllowGroupInvite    *bool                `json:"allow_group_invite"`
}

// OptionalStringUpdate tracks whether a string field was present in JSON
// payload, while preserving null as an explicit clear signal.
type OptionalStringUpdate struct {
	Set   bool
	Value *string
}

func (f *OptionalStringUpdate) UnmarshalJSON(data []byte) error {
	f.Set = true
	if string(data) == "null" {
		f.Value = nil
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	f.Value = &value
	return nil
}

func GetUserSettings(userID int64) (*UserSettingsResp, error) {
	currentSetting, exists, err := loadUserSetting(userID)
	if err != nil {
		return nil, err
	}

	resp := &UserSettingsResp{
		PreferredLanguage: preferredLanguageZH,
		Chat: UserSettingsChatResp{
			FriendAddSetting:   model.FriendAddSettingNeedApproval,
			AllowGroupInvite:   true,
			VoiceBrainRealtime: true,
		},
	}
	if !exists {
		return resp, nil
	}
	if currentSetting.AutoDelegateAgentID != nil && *currentSetting.AutoDelegateAgentID > 0 {
		resp.Chat.AutoDelegateAgentID = cloneInt64Ptr(currentSetting.AutoDelegateAgentID)
	}
	if currentSetting.VoiceAutoDelegateAgentID != nil && *currentSetting.VoiceAutoDelegateAgentID > 0 {
		resp.Chat.VoiceAutoDelegateAgentID = cloneInt64Ptr(currentSetting.VoiceAutoDelegateAgentID)
	}
	if currentSetting.VoiceBrainAgentID != nil && *currentSetting.VoiceBrainAgentID > 0 {
		resp.Chat.VoiceBrainAgentID = cloneInt64Ptr(currentSetting.VoiceBrainAgentID)
	}
	if !model.IsValidFriendAddSetting(currentSetting.FriendAddSetting) {
		return nil, ErrUserSettingsInvalidFriendAddMode
	}
	resp.PreferredLanguage = normalizePreferredLanguage(currentSetting.PreferredLanguage)
	resp.Chat.FriendAddSetting = currentSetting.FriendAddSetting
	resp.Chat.AllowGroupInvite = currentSetting.AllowGroupInvite
	resp.Chat.VoiceBrainRealtime = currentSetting.VoiceBrainRealtime
	return resp, nil
}

func UpdateUserSettings(userID int64, req UserSettingsUpdateReq) (*UserSettingsResp, error) {
	hasPreferredLanguageUpdate := req.PreferredLanguage.Set
	if req.Chat == nil && !hasPreferredLanguageUpdate {
		return nil, ErrUserSettingsInvalidPayload
	}
	hasAutoDelegateUpdate := false
	hasVoiceAutoDelegateUpdate := false
	hasVoiceBrainUpdate := false
	hasFriendAddModeUpdate := false
	hasAllowGroupInviteUpdate := false
	hasVoiceBrainRealtimeUpdate := false
	if req.Chat != nil {
		hasAutoDelegateUpdate = req.Chat.AutoDelegateAgentID.Set
		hasVoiceAutoDelegateUpdate = req.Chat.VoiceAutoDelegateAgentID.Set
		hasVoiceBrainUpdate = req.Chat.VoiceBrainAgentID.Set
		hasFriendAddModeUpdate = req.Chat.FriendAddSetting != nil
		hasAllowGroupInviteUpdate = req.Chat.AllowGroupInvite != nil
		hasVoiceBrainRealtimeUpdate = req.Chat.VoiceBrainRealtime != nil
	}
	if !hasPreferredLanguageUpdate && !hasAutoDelegateUpdate && !hasVoiceAutoDelegateUpdate && !hasVoiceBrainUpdate && !hasFriendAddModeUpdate && !hasAllowGroupInviteUpdate && !hasVoiceBrainRealtimeUpdate {
		return nil, ErrUserSettingsInvalidPayload
	}

	currentSetting, exists, err := loadUserSetting(userID)
	if err != nil {
		return nil, err
	}

	nextAutoDelegateAgentID := (*int64)(nil)
	nextVoiceAutoDelegateAgentID := (*int64)(nil)
	nextVoiceBrainAgentID := (*int64)(nil)
	nextPreferredLanguage := preferredLanguageZH
	nextFriendAddMode := model.FriendAddSettingNeedApproval
	nextAllowGroupInvite := true
	nextVoiceBrainRealtime := true
	if exists {
		nextAutoDelegateAgentID = cloneInt64Ptr(currentSetting.AutoDelegateAgentID)
		nextVoiceAutoDelegateAgentID = cloneInt64Ptr(currentSetting.VoiceAutoDelegateAgentID)
		nextVoiceBrainAgentID = cloneInt64Ptr(currentSetting.VoiceBrainAgentID)
		nextPreferredLanguage = normalizePreferredLanguage(currentSetting.PreferredLanguage)
		if !model.IsValidFriendAddSetting(currentSetting.FriendAddSetting) {
			return nil, ErrUserSettingsInvalidFriendAddMode
		}
		nextFriendAddMode = currentSetting.FriendAddSetting
		nextAllowGroupInvite = currentSetting.AllowGroupInvite
		nextVoiceBrainRealtime = currentSetting.VoiceBrainRealtime
	}

	if hasAutoDelegateUpdate {
		nextAutoDelegateAgentID, err = parseAutoDelegateAgentID(req.Chat.AutoDelegateAgentID.Value)
		if err != nil {
			return nil, err
		}
		if nextAutoDelegateAgentID != nil {
			if err := validateAutoDelegateAgent(userID, *nextAutoDelegateAgentID); err != nil {
				return nil, err
			}
		}
	}
	if hasVoiceAutoDelegateUpdate {
		nextVoiceAutoDelegateAgentID, err = parseAutoDelegateAgentID(req.Chat.VoiceAutoDelegateAgentID.Value)
		if err != nil {
			return nil, err
		}
		if nextVoiceAutoDelegateAgentID != nil {
			if err := validateVoiceAutoDelegateAgent(userID, *nextVoiceAutoDelegateAgentID); err != nil {
				return nil, err
			}
		}
	}
	if hasVoiceBrainUpdate {
		nextVoiceBrainAgentID, err = parseAutoDelegateAgentID(req.Chat.VoiceBrainAgentID.Value)
		if err != nil {
			return nil, err
		}
		if nextVoiceBrainAgentID != nil {
			// 语音大脑必须是 owner 本人的 type=4 语音大模型（与语音托管同款校验）
			if err := validateVoiceAutoDelegateAgent(userID, *nextVoiceBrainAgentID); err != nil {
				return nil, err
			}
		}
	}
	if hasPreferredLanguageUpdate {
		nextPreferredLanguage, err = parsePreferredLanguage(req.PreferredLanguage.Value)
		if err != nil {
			return nil, err
		}
	}

	if hasFriendAddModeUpdate {
		if err := validateFriendAddMode(*req.Chat.FriendAddSetting); err != nil {
			return nil, err
		}
		nextFriendAddMode = *req.Chat.FriendAddSetting
	}
	if hasAllowGroupInviteUpdate {
		nextAllowGroupInvite = *req.Chat.AllowGroupInvite
	}
	if hasVoiceBrainRealtimeUpdate {
		nextVoiceBrainRealtime = *req.Chat.VoiceBrainRealtime
	}

	now := time.Now()
	nextSetting := map[string]any{
		"user_id":                userID,
		"auto_delegate_agent_id":       nextAutoDelegateAgentID,
		"voice_auto_delegate_agent_id": nextVoiceAutoDelegateAgentID,
		"voice_brain_agent_id":         nextVoiceBrainAgentID,
		"preferred_language":           nextPreferredLanguage,
		"friend_add_setting":           nextFriendAddMode,
		"allow_group_invite":           nextAllowGroupInvite,
		"voice_brain_realtime":         nextVoiceBrainRealtime,
		"created_at":                   now,
		"updated_at":             now,
	}
	if err := store.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"auto_delegate_agent_id":       nextAutoDelegateAgentID,
			"voice_auto_delegate_agent_id": nextVoiceAutoDelegateAgentID,
			"voice_brain_agent_id":         nextVoiceBrainAgentID,
			"preferred_language":           nextPreferredLanguage,
			"friend_add_setting":           nextFriendAddMode,
			"allow_group_invite":           nextAllowGroupInvite,
			"voice_brain_realtime":         nextVoiceBrainRealtime,
			"updated_at":                   now,
		}),
	}).Table(model.UserSetting{}.TableName()).Create(nextSetting).Error; err != nil {
		return nil, err
	}

	return GetUserSettings(userID)
}

func parseAutoDelegateAgentID(raw *string) (*int64, error) {
	if raw == nil {
		return nil, nil
	}

	normalized := strings.TrimSpace(*raw)
	if normalized == "" {
		return nil, nil
	}

	id, err := strconv.ParseInt(normalized, 10, 64)
	if err != nil || id <= 0 {
		return nil, ErrUserSettingsInvalidAgentID
	}
	return &id, nil
}

func parsePreferredLanguage(raw *string) (string, error) {
	if raw == nil {
		return "", ErrUserSettingsInvalidLanguage
	}
	normalized := strings.ToLower(strings.TrimSpace(*raw))
	if normalized == "" {
		return "", ErrUserSettingsInvalidLanguage
	}
	lower := strings.ReplaceAll(normalized, "-", "_")
	for _, lang := range supportedLanguages {
		if lower == lang || strings.HasPrefix(lower, lang+"_") {
			return lang, nil
		}
	}
	return "", ErrUserSettingsInvalidLanguage
}

func validateFriendAddMode(mode int8) error {
	if model.IsValidFriendAddSetting(mode) {
		return nil
	}
	return ErrUserSettingsInvalidFriendAddMode
}

func validateAutoDelegateAgent(userID int64, agentID int64) error {
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id", "status").
		Where("id = ?", agentID).
		First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserSettingsAutoAgentNotFound
		}
		return err
	}
	if agent.Status != 1 {
		return ErrUserSettingsAutoAgentUnavailable
	}
	if agent.OwnerID == userID {
		return nil
	}
	shared, err := hasActiveAgentShare(agentID, userID)
	if err != nil {
		return err
	}
	if !shared {
		return ErrUserSettingsAutoAgentNotOwned
	}
	return nil
}

func loadUserAutoDelegateAgentID(userID int64) (int64, bool, error) {
	setting, exists, err := loadUserSetting(userID)
	if err != nil {
		return 0, false, err
	}
	if !exists {
		return 0, false, nil
	}
	if setting.AutoDelegateAgentID == nil || *setting.AutoDelegateAgentID <= 0 {
		return 0, false, nil
	}
	return *setting.AutoDelegateAgentID, true, nil
}

func getUserFriendAddMode(userID int64) (int8, error) {
	setting, exists, err := loadUserSetting(userID)
	if err != nil {
		return 0, err
	}
	if !exists {
		return model.FriendAddSettingNeedApproval, nil
	}
	if !model.IsValidFriendAddSetting(setting.FriendAddSetting) {
		return 0, ErrUserSettingsInvalidFriendAddMode
	}
	return setting.FriendAddSetting, nil
}

func loadUserSetting(userID int64) (*model.UserSetting, bool, error) {
	return loadUserSettingWithDB(store.DB, userID)
}

func loadUserSettingWithDB(db *gorm.DB, userID int64) (*model.UserSetting, bool, error) {
	if db == nil {
		return nil, false, nil
	}

	var setting model.UserSetting
	if err := db.Select("auto_delegate_agent_id", "voice_auto_delegate_agent_id", "voice_brain_agent_id", "voice_brain_realtime", "preferred_language", "friend_add_setting", "allow_group_invite").
		Where("user_id = ?", userID).
		First(&setting).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &setting, true, nil
}

func validateVoiceAutoDelegateAgent(userID int64, agentID int64) error {
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id", "status", "provider_type").
		Where("id = ?", agentID).
		First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserSettingsAutoAgentNotFound
		}
		return err
	}
	if agent.Status != 1 {
		return ErrUserSettingsAutoAgentUnavailable
	}
	if agent.ProviderType != model.AgentProviderVoice {
		return ErrUserSettingsVoiceAgentNotVoice
	}
	if agent.OwnerID == userID {
		return nil
	}
	shared, err := hasActiveAgentShare(agentID, userID)
	if err != nil {
		return err
	}
	if !shared {
		return ErrUserSettingsAutoAgentNotOwned
	}
	return nil
}

// LoadUserVoiceAutoDelegateAgentID 返回用户级语音自动托管 agent（来电自动代接用）。
func LoadUserVoiceAutoDelegateAgentID(userID int64) (int64, bool) {
	setting, exists, err := loadUserSetting(userID)
	if err != nil || !exists {
		return 0, false
	}
	if setting.VoiceAutoDelegateAgentID == nil || *setting.VoiceAutoDelegateAgentID <= 0 {
		return 0, false
	}
	return *setting.VoiceAutoDelegateAgentID, true
}

// LoadUserVoiceBrainAgentID 返回用户级语音大脑 agent（owner 主动呼出的语音通道）。
func LoadUserVoiceBrainAgentID(userID int64) (int64, bool) {
	setting, exists, err := loadUserSetting(userID)
	if err != nil || !exists {
		return 0, false
	}
	if setting.VoiceBrainAgentID == nil || *setting.VoiceBrainAgentID <= 0 {
		return 0, false
	}
	return *setting.VoiceBrainAgentID, true
}

// LoadUserVoiceBrainRealtime 返回用户级语音大脑工作模式：
// true=豆包实时互动(端到端+502背景注入)，false=STT+TTS 念稿兜底。
// 无设置记录时默认 true（实时）。
func LoadUserVoiceBrainRealtime(userID int64) bool {
	setting, exists, err := loadUserSetting(userID)
	if err != nil || !exists {
		return true
	}
	return setting.VoiceBrainRealtime
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func hasAnyUserDisallowGroupInvite(userIDs []int64) (bool, error) {
	normalizedUserIDs := uniquePositiveInt64s(userIDs)
	if len(normalizedUserIDs) == 0 {
		return false, nil
	}

	var settings []model.UserSetting
	if err := store.DB.Select("user_id", "allow_group_invite").
		Where("user_id IN ?", normalizedUserIDs).
		Find(&settings).Error; err != nil {
		return false, err
	}
	for _, setting := range settings {
		if !setting.AllowGroupInvite {
			return true, nil
		}
	}
	return false, nil
}

func uniquePositiveInt64s(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
