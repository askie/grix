package model

import "time"

// AdminRoleCustom 表示自定义角色类型（role 字段值）。
const AdminRoleCustom int16 = 2

// AssignablePermissionKeys 是可通过角色授予的权限 key（admins 为超管专属，不可授予）。
var AssignablePermissionKeys = []string{
	"users",
	"eggs",
	"reports",
	"moderation",
	"visitor_bans",
	"link_blocklist",
	"connector",
	"app",
	"feature_gates",
	"settings",
	"gateway_billing",
}

// AdminRole 管理后台角色。
type AdminRole struct {
	ID          int64     `gorm:"primaryKey" json:"id,string"`
	Name        string    `gorm:"uniqueIndex;size:64;not null" json:"name"`
	Description string    `gorm:"size:255;not null;default:''" json:"description"`
	Permissions string    `gorm:"type:text;not null;default:'[]'" json:"permissions"` // JSON string array
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (AdminRole) TableName() string { return "admin_roles" }
