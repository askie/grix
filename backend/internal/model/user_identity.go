package model

import "time"

// 身份提供商类型常量，与 internal/api/service/identity 的注册名一一对应。
const (
	IdentityProviderPhoneSmsCN     = "phone_sms_cn"
	IdentityProviderPhoneSmsGlobal = "phone_sms_global"
	IdentityProviderEmailCode      = "email_code"
	IdentityProviderApple          = "apple"
	IdentityProviderGoogle         = "google"
)

// UserIdentity 用户绑定的某个身份提供商账号。
// 一个 user 可对应多行（多 provider）；同 (provider, external_id) 全局唯一。
type UserIdentity struct {
	ID          int64      `gorm:"primaryKey" json:"id,string"`
	UserID      int64      `gorm:"not null;index" json:"user_id,string"`
	Provider    string     `gorm:"size:32;not null;uniqueIndex:uq_user_identities_provider_extid,priority:1" json:"provider"`
	ExternalID  string     `gorm:"size:255;not null;uniqueIndex:uq_user_identities_provider_extid,priority:2" json:"external_id"`
	CountryCode string     `gorm:"size:8" json:"country_code,omitempty"`
	PrimaryFlag bool       `gorm:"not null;default:false" json:"primary_flag"`
	VerifiedAt  *time.Time `json:"verified_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (UserIdentity) TableName() string { return "user_identities" }
