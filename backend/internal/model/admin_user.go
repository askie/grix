package model

import "time"

const (
	AdminRoleSuperAdmin int16 = 1

	AdminStatusActive   int16 = 1
	AdminStatusDisabled int16 = 2
)

type AdminUser struct {
	ID           int64      `gorm:"primaryKey" json:"id,string"`
	Username     string     `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash string     `gorm:"size:255;not null" json:"-"`
	Nickname     string     `gorm:"size:64;not null" json:"nickname"`
	Role         int16      `gorm:"not null;default:1" json:"role"`
	RoleID       *int64     `gorm:"index" json:"role_id,string,omitempty"`
	Status       int16      `gorm:"not null;default:1;index" json:"status"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (AdminUser) TableName() string { return "admin_users" }
