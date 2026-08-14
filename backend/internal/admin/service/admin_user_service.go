package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAdminUsernameRequired    = errors.New("管理员账号不能为空")
	ErrAdminNicknameRequired    = errors.New("管理员昵称不能为空")
	ErrAdminUsernameExists      = errors.New("管理员账号已存在")
	ErrAdminCurrentPassword     = errors.New("当前密码错误")
	ErrAdminPasswordSame        = errors.New("新密码不能与当前密码相同")
	ErrAdminDisableSelf         = errors.New("不能禁用当前登录管理员")
	ErrAdminDeleteSelf          = errors.New("不能删除当前登录管理员")
	ErrAdminLastActiveProtected = errors.New("至少保留一个启用状态的管理员")
)

type AdminListItem struct {
	ID          int64      `json:"id,string"`
	Username    string     `json:"username"`
	Nickname    string     `json:"nickname"`
	Role        int16      `json:"role"`
	RoleID      *int64     `json:"role_id,string,omitempty"`
	RoleName    string     `json:"role_name,omitempty"`
	Status      int16      `json:"status"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type CreateAdminInput struct {
	Username string
	Nickname string
	Password string
	// Role=1 超级管理员（默认），Role=2 自定义角色时需提供 RoleID
	Role   int16
	RoleID *int64
}

var ErrAdminRoleIDRequired = errors.New("自定义角色管理员必须指定角色")

func ListAdmins() ([]AdminListItem, error) {
	rows, err := store.DB.Raw(`
		SELECT u.id, u.username, u.nickname, u.role, u.role_id, u.status,
		       u.last_login_at, u.created_at,
		       COALESCE(r.name, '') AS role_name
		FROM admin_users u
		LEFT JOIN admin_roles r ON r.id = u.role_id
		ORDER BY u.created_at ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []AdminListItem
	for rows.Next() {
		var it AdminListItem
		if err := rows.Scan(&it.ID, &it.Username, &it.Nickname, &it.Role, &it.RoleID,
			&it.Status, &it.LastLoginAt, &it.CreatedAt, &it.RoleName); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if items == nil {
		items = []AdminListItem{}
	}
	return items, nil
}

func CreateAdmin(operatorID int64, input CreateAdminInput, clientIP, userAgent string) (*model.AdminUser, error) {
	username := strings.TrimSpace(input.Username)
	if username == "" {
		return nil, ErrAdminUsernameRequired
	}

	nickname := strings.TrimSpace(input.Nickname)
	if nickname == "" {
		return nil, ErrAdminNicknameRequired
	}

	role := input.Role
	if role != model.AdminRoleCustom {
		role = model.AdminRoleSuperAdmin
	}
	if role == model.AdminRoleCustom && input.RoleID == nil {
		return nil, ErrAdminRoleIDRequired
	}

	passwordHash, err := HashAdminPassword(input.Password)
	if err != nil {
		return nil, err
	}

	admin := &model.AdminUser{
		ID:           snowflake.GenID(),
		Username:     username,
		PasswordHash: passwordHash,
		Nickname:     nickname,
		Role:         role,
		RoleID:       input.RoleID,
		Status:       model.AdminStatusActive,
	}

	err = store.DB.Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&model.AdminUser{}).
			Where("username = ?", username).
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return ErrAdminUsernameExists
		}

		// 校验 RoleID 指向真实存在的角色
		if input.RoleID != nil {
			var roleCount int64
			if err := tx.Model(&model.AdminRole{}).
				Where("id = ?", *input.RoleID).
				Count(&roleCount).Error; err != nil {
				return err
			}
			if roleCount == 0 {
				return ErrAdminRoleIDRequired
			}
		}

		if err := tx.Create(admin).Error; err != nil {
			if isAdminUsernameConflict(err) {
				return ErrAdminUsernameExists
			}
			return err
		}

		return recordOperationTx(tx, operatorID, "admin_create", "admin_user", fmt.Sprintf("%d", admin.ID), map[string]any{
			"username": admin.Username,
			"nickname": admin.Nickname,
		}, clientIP, userAgent)
	})
	if err != nil {
		return nil, err
	}

	return admin, nil
}

func ChangeOwnPassword(adminID int64, currentPassword, newPassword, clientIP, userAgent string) error {
	newHash, err := HashAdminPassword(newPassword)
	if err != nil {
		return err
	}

	return store.DB.Transaction(func(tx *gorm.DB) error {
		admin, err := loadAdminForUpdate(tx, adminID)
		if err != nil {
			return err
		}

		if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(currentPassword)); err != nil {
			return ErrAdminCurrentPassword
		}
		if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(newPassword)) == nil {
			return ErrAdminPasswordSame
		}

		now := time.Now().UTC()
		if err := tx.Model(&model.AdminUser{}).
			Where("id = ?", admin.ID).
			Updates(map[string]any{
				"password_hash": newHash,
				"updated_at":    now,
			}).Error; err != nil {
			return err
		}

		if err := revokeAdminSessionsTx(tx, admin.ID, now); err != nil {
			return err
		}

		if err := clearAdminLoginLock(context.Background(), admin); err != nil {
			return err
		}

		return recordOperationTx(tx, admin.ID, "admin_change_password", "admin_user", fmt.Sprintf("%d", admin.ID), map[string]any{}, clientIP, userAgent)
	})
}

func DisableAdmin(operatorID, targetAdminID int64, clientIP, userAgent string) error {
	if operatorID == targetAdminID {
		return ErrAdminDisableSelf
	}

	return store.DB.Transaction(func(tx *gorm.DB) error {
		admin, err := loadAdminForUpdate(tx, targetAdminID)
		if err != nil {
			return err
		}
		if admin.Status == model.AdminStatusDisabled {
			return nil
		}

		count, err := countActiveAdminsTx(tx)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrAdminLastActiveProtected
		}

		now := time.Now().UTC()
		if err := tx.Model(&model.AdminUser{}).
			Where("id = ?", admin.ID).
			Updates(map[string]any{
				"status":     model.AdminStatusDisabled,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}

		if err := revokeAdminSessionsTx(tx, admin.ID, now); err != nil {
			return err
		}

		return recordOperationTx(tx, operatorID, "admin_disable", "admin_user", fmt.Sprintf("%d", admin.ID), map[string]any{
			"username": admin.Username,
		}, clientIP, userAgent)
	})
}

func EnableAdmin(operatorID, targetAdminID int64, clientIP, userAgent string) error {
	return store.DB.Transaction(func(tx *gorm.DB) error {
		admin, err := loadAdminForUpdate(tx, targetAdminID)
		if err != nil {
			return err
		}
		if admin.Status == model.AdminStatusActive {
			return nil
		}

		now := time.Now().UTC()
		if err := tx.Model(&model.AdminUser{}).
			Where("id = ?", admin.ID).
			Updates(map[string]any{
				"status":     model.AdminStatusActive,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}

		return recordOperationTx(tx, operatorID, "admin_enable", "admin_user", fmt.Sprintf("%d", admin.ID), map[string]any{
			"username": admin.Username,
		}, clientIP, userAgent)
	})
}

func DeleteAdmin(operatorID, targetAdminID int64, clientIP, userAgent string) error {
	if operatorID == targetAdminID {
		return ErrAdminDeleteSelf
	}

	return store.DB.Transaction(func(tx *gorm.DB) error {
		admin, err := loadAdminForUpdate(tx, targetAdminID)
		if err != nil {
			return err
		}
		if admin.Status == model.AdminStatusActive {
			count, err := countActiveAdminsTx(tx)
			if err != nil {
				return err
			}
			if count <= 1 {
				return ErrAdminLastActiveProtected
			}
		}

		if err := recordOperationTx(tx, operatorID, "admin_delete", "admin_user", fmt.Sprintf("%d", admin.ID), map[string]any{
			"username": admin.Username,
			"nickname": admin.Nickname,
		}, clientIP, userAgent); err != nil {
			return err
		}

		if err := tx.Where("admin_id = ?", admin.ID).Delete(&model.AdminSession{}).Error; err != nil {
			return err
		}

		return tx.Delete(&model.AdminUser{}, admin.ID).Error
	})
}

func loadAdminForUpdate(tx *gorm.DB, adminID int64) (*model.AdminUser, error) {
	var admin model.AdminUser
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&admin, adminID).Error; err != nil {
		return nil, err
	}
	return &admin, nil
}

func countActiveAdminsTx(tx *gorm.DB) (int64, error) {
	var count int64
	err := tx.Model(&model.AdminUser{}).
		Where("status = ?", model.AdminStatusActive).
		Count(&count).Error
	return count, err
}

func revokeAdminSessionsTx(tx *gorm.DB, adminID int64, now time.Time) error {
	return tx.Model(&model.AdminSession{}).
		Where("admin_id = ? AND revoked_at IS NULL", adminID).
		Updates(map[string]any{
			"revoked_at": now,
			"updated_at": now,
		}).Error
}

func isAdminUsernameConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "duplicate")
}
