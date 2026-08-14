package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrRoleNameRequired    = errors.New("角色名称不能为空")
	ErrRoleNameExists      = errors.New("角色名称已存在")
	ErrRoleNotFound        = errors.New("角色不存在")
	ErrRoleInUse           = errors.New("该角色还有管理员绑定，无法删除")
	ErrRoleInvalidPermKeys = errors.New("包含无效的权限 key")
)

type RoleInput struct {
	Name        string
	Description string
	Permissions []string // 可赋值的 key 列表
}

// LoadAdminPermissions 根据管理员类型返回其权限列表，仅查一次 DB。
// 超级管理员返回全部 key，自定义角色从 admin_roles 查。
func LoadAdminPermissions(admin *model.AdminUser) []string {
	if admin.Role == model.AdminRoleSuperAdmin {
		return append([]string{"admins"}, model.AssignablePermissionKeys...)
	}
	if admin.RoleID == nil {
		return []string{}
	}
	var role model.AdminRole
	if err := store.DB.First(&role, *admin.RoleID).Error; err != nil {
		return []string{}
	}
	return ParseRolePermissions(role.Permissions)
}

// LoadRoleByID 按 ID 加载角色。
func LoadRoleByID(id int64, dest *model.AdminRole) error {
	return store.DB.First(dest, id).Error
}

func ListRoles() ([]model.AdminRole, error) {
	var roles []model.AdminRole
	err := store.DB.Order("created_at ASC").Find(&roles).Error
	return roles, err
}

func CreateRole(input RoleInput) (*model.AdminRole, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrRoleNameRequired
	}
	if err := validatePermKeys(input.Permissions); err != nil {
		return nil, err
	}
	permsJSON, _ := json.Marshal(normalizePermKeys(input.Permissions))
	role := &model.AdminRole{
		ID:          snowflake.GenID(),
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		Permissions: string(permsJSON),
	}
	if err := store.DB.Create(role).Error; err != nil {
		if isNameConflict(err) {
			return nil, ErrRoleNameExists
		}
		return nil, err
	}
	return role, nil
}

func UpdateRole(roleID int64, input RoleInput) (*model.AdminRole, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrRoleNameRequired
	}
	if err := validatePermKeys(input.Permissions); err != nil {
		return nil, err
	}
	permsJSON, _ := json.Marshal(normalizePermKeys(input.Permissions))

	var result model.AdminRole
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		var role model.AdminRole
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&role, roleID).Error; err != nil {
			return ErrRoleNotFound
		}
		now := time.Now().UTC()
		updates := map[string]any{
			"name":        name,
			"description": strings.TrimSpace(input.Description),
			"permissions": string(permsJSON),
			"updated_at":  now,
		}
		if err := tx.Model(&role).Updates(updates).Error; err != nil {
			if isNameConflict(err) {
				return ErrRoleNameExists
			}
			return err
		}
		// 读取更新后的数据
		if err := tx.First(&result, roleID).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func DeleteRole(roleID int64) error {
	return store.DB.Transaction(func(tx *gorm.DB) error {
		// 锁定角色行确保一致性
		var role model.AdminRole
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&role, roleID).Error; err != nil {
			return ErrRoleNotFound
		}
		var count int64
		if err := tx.Model(&model.AdminUser{}).
			Where("role_id = ?", roleID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrRoleInUse
		}
		return tx.Delete(&model.AdminRole{}, roleID).Error
	})
}

// ParseRolePermissions 将 JSON 字符串解析为 permissions 切片。
func ParseRolePermissions(permissionsJSON string) []string {
	var keys []string
	_ = json.Unmarshal([]byte(permissionsJSON), &keys)
	return keys
}

func validatePermKeys(keys []string) error {
	valid := make(map[string]bool, len(model.AssignablePermissionKeys))
	for _, k := range model.AssignablePermissionKeys {
		valid[k] = true
	}
	for _, k := range keys {
		if !valid[k] {
			return ErrRoleInvalidPermKeys
		}
	}
	return nil
}

func normalizePermKeys(keys []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(keys))
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			result = append(result, k)
		}
	}
	return result
}

func isNameConflict(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
