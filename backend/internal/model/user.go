package model

import (
	"encoding/json"
	"time"
)

const (
	UserStatusActive  int16 = 1
	UserStatusBanned  int16 = 2
	UserStatusDeleted int16 = 3
)

type User struct {
	ID               int64      `gorm:"primaryKey" json:"id,string"`
	Username         string     `gorm:"uniqueIndex;size:100;not null" json:"username"`
	Email            string     `gorm:"uniqueIndex;size:100" json:"email"`
	PasswordHash     string     `gorm:"size:255" json:"-"`
	UsernameModified bool       `gorm:"default:false" json:"username_modified"`
	AuthProvider     string     `gorm:"size:20;default:local" json:"auth_provider"`
	Nickname         string     `gorm:"size:50" json:"nickname"`
	Introduction     string     `gorm:"type:text;not null;default:''" json:"introduction"`
	AvatarURL        string     `gorm:"size:255" json:"avatar_url"`
	Status           int16      `gorm:"not null;default:1;index" json:"status"`
	BannedReason     string     `gorm:"size:255;not null;default:''" json:"banned_reason"`
	BannedAt         *time.Time `json:"banned_at,omitempty"`
	BannedBy         *int64     `json:"banned_by,omitempty"`
	Region           string     `gorm:"size:16;not null;default:'global'" json:"region"` // 注册区域：cn 或 global
	// PhoneE164 旧明文列：手机号加密改造（migration 083）后不再写入，存量数据迁移时置 NULL。
	// 真实号改由 PhoneCipher（密文）持有，权威绑定关系仍在 user_identities 表（external_id=盲索引）。
	PhoneE164    string    `gorm:"size:20" json:"phone_e164,omitempty"`
	PhoneCountry string    `gorm:"size:8" json:"phone_country,omitempty"`
	// PhoneCipher 手机号 AES-GCM 密文（secretcrypto.Encrypt），需要时解密取回真实号；绝不下发前端。
	PhoneCipher string `gorm:"column:phone_cipher;type:text" json:"-"`
	// PhoneLast4 手机号末 4 位明文：供塘主后台精确搜索与前端脱敏展示（****8000）。
	PhoneLast4 string `gorm:"column:phone_last4;size:8" json:"-"`
	// PhoneBlind 手机号盲索引（secretcrypto.BlindIndex，HMAC）：唯一约束与登录精确查号；绝不下发前端。
	PhoneBlind string `gorm:"column:phone_blind;size:64" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (User) TableName() string { return "users" }

// MarshalJSON 保证真实手机号永不随 JSON 出站：phone_e164 字段只输出末 4 位脱敏（****8000）。
// 真实号在 PhoneCipher（密文）/PhoneBlind（盲索引）里，两者 json:"-" 不下发；后端内部需要真实号
// 的路径解密 PhoneCipher，不经 JSON，故不受影响。
func (u User) MarshalJSON() ([]byte, error) {
	type alias User
	a := alias(u)
	a.PhoneE164 = maskedPhoneDisplay(u)
	return json.Marshal(a)
}

// maskedPhoneDisplay 取末 4 位拼成 ****8000；优先用 PhoneLast4，迁移前的存量明文行兜底取 PhoneE164 末 4 位。
func maskedPhoneDisplay(u User) string {
	last4 := u.PhoneLast4
	if last4 == "" && u.PhoneE164 != "" {
		r := []rune(u.PhoneE164)
		if len(r) > 4 {
			last4 = string(r[len(r)-4:])
		} else {
			last4 = u.PhoneE164
		}
	}
	if last4 == "" {
		return ""
	}
	return "****" + last4
}
