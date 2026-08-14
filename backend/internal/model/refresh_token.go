package model

import "time"

const (
	RefreshTokenStatusActive  int16 = 0
	RefreshTokenStatusUsed    int16 = 1
	RefreshTokenStatusRevoked int16 = 2
)

type RefreshToken struct {
	JTI           string     `gorm:"primaryKey;size:64" json:"jti"`
	UserID        int64      `gorm:"index:idx_refresh_user_family,priority:1;not null" json:"user_id,string"`
	FamilyID      string     `gorm:"size:64;index:idx_refresh_user_family,priority:2;index:idx_refresh_family_status,priority:1;not null" json:"family_id"`
	Status        int16      `gorm:"index:idx_refresh_family_status,priority:2;not null;default:0" json:"status"`
	ParentJTI     *string    `gorm:"size:64" json:"parent_jti,omitempty"`
	ReplacedByJTI *string    `gorm:"size:64" json:"replaced_by_jti,omitempty"`
	ExpiresAt     time.Time  `gorm:"index;not null" json:"expires_at"`
	UsedAt        *time.Time `json:"used_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (RefreshToken) TableName() string { return "auth_refresh_tokens" }
