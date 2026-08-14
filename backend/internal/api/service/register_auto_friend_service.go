package service

import (
	"errors"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrConfiguredCustomerInvalid  = errors.New("系统客户账户ID配置无效")
	ErrConfiguredCustomerNotFound = errors.New("系统客户账户不存在")
	ErrConfiguredCustomerDisabled = errors.New("系统客户账户已禁用")
)

func addConfiguredCustomerFriendTx(tx *gorm.DB, registerUserID, customerUserID int64) error {
	if tx == nil {
		return errors.New("系统繁忙，请稍后重试")
	}
	if customerUserID == 0 {
		return nil
	}
	if customerUserID < 0 {
		return ErrConfiguredCustomerInvalid
	}
	if registerUserID <= 0 {
		return errors.New("用户创建失败")
	}
	if registerUserID == customerUserID {
		return ErrConfiguredCustomerInvalid
	}

	var customer model.User
	if err := tx.Select("id", "status").First(&customer, customerUserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrConfiguredCustomerNotFound
		}
		return err
	}
	if customer.Status != model.UserStatusActive {
		return ErrConfiguredCustomerDisabled
	}

	now := time.Now().UTC()
	relations := []model.Friend{
		{
			ID:        nextFriendID(),
			UserID:    registerUserID,
			FriendID:  customerUserID,
			CreatedAt: now,
		},
		{
			ID:        nextFriendID(),
			UserID:    customerUserID,
			FriendID:  registerUserID,
			CreatedAt: now,
		},
	}

	for _, relation := range relations {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "friend_id"},
			},
			DoNothing: true,
		}).Create(&relation).Error; err != nil {
			return err
		}
	}
	return nil
}

func notifyConfiguredCustomerFriendAdded(registerUserID, customerUserID int64) {
	if registerUserID <= 0 || customerUserID <= 0 || registerUserID == customerUserID {
		return
	}
	notifyFriendAdded(registerUserID, customerUserID)
	notifyFriendAdded(customerUserID, registerUserID)
}
