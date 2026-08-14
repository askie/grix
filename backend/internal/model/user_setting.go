package model

import "time"

const (
	FriendAddSettingNeedApproval int8 = 1
	FriendAddSettingAutoApprove  int8 = 2
	FriendAddSettingForbidden    int8 = 3
)

func IsValidFriendAddSetting(setting int8) bool {
	switch setting {
	case FriendAddSettingNeedApproval, FriendAddSettingAutoApprove, FriendAddSettingForbidden:
		return true
	default:
		return false
	}
}

type UserSetting struct {
	UserID              int64     `gorm:"primaryKey;autoIncrement:false" json:"user_id,string"`
	AutoDelegateAgentID *int64    `gorm:"column:auto_delegate_agent_id" json:"auto_delegate_agent_id,string,omitempty"`
	VoiceAutoDelegateAgentID *int64 `gorm:"column:voice_auto_delegate_agent_id" json:"voice_auto_delegate_agent_id,string,omitempty"`
	VoiceBrainAgentID        *int64 `gorm:"column:voice_brain_agent_id" json:"voice_brain_agent_id,string,omitempty"`
	// VoiceBrainRealtime 语音大脑工作模式：true=豆包实时互动(端到端+502背景注入)，
	// false=STT+TTS 念稿兜底。默认 true。仅作用于语音大脑(owner 主动呼出)，不影响客服。
	VoiceBrainRealtime  bool      `gorm:"column:voice_brain_realtime;not null;default:true" json:"voice_brain_realtime"`
	PreferredLanguage   string    `gorm:"column:preferred_language;size:8;not null;default:zh" json:"preferred_language"`
	FriendAddSetting    int8      `gorm:"column:friend_add_setting;not null;default:1" json:"friend_add_setting"`
	AllowGroupInvite    bool      `gorm:"column:allow_group_invite;not null;default:true" json:"allow_group_invite"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (UserSetting) TableName() string { return "user_settings" }
