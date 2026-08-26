package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func generateUnsubToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func EnsureReachSubscription(userID int64, region string) (*model.ReachSubscription, error) {
	var sub model.ReachSubscription
	err := store.DB.Where("user_id = ?", userID).First(&sub).Error
	if err == nil {
		return &sub, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	defaultSubscribed := region != "global"
	now := time.Now().UTC()
	token := generateUnsubToken()
	if err := store.DB.Exec(
		"INSERT INTO reach_subscriptions (user_id, subscribed, topics, unsub_token, updated_at) VALUES (?, ?, '[]', ?, ?)",
		userID, defaultSubscribed, token, now,
	).Error; err != nil {
		store.DB.Where("user_id = ?", userID).First(&sub)
		return &sub, nil
	}
	sub = model.ReachSubscription{
		UserID:     userID,
		Subscribed: defaultSubscribed,
		Topics:     []byte(`[]`),
		UnsubToken: token,
		UpdatedAt:  now,
	}
	return &sub, nil
}

func GetReachSubscription(userID int64) (*model.ReachSubscription, error) {
	var sub model.ReachSubscription
	if err := store.DB.Where("user_id = ?", userID).First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func UpdateReachSubscription(userID int64, subscribed bool) error {
	res := store.DB.Model(&model.ReachSubscription{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{"subscribed": subscribed, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("subscription not found")
	}
	return nil
}

func UnsubscribeByToken(token string) error {
	if token == "" {
		return errors.New("empty token")
	}
	res := store.DB.Model(&model.ReachSubscription{}).
		Where("unsub_token = ? AND subscribed = ?", token, true).
		Updates(map[string]any{"subscribed": false, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("invalid token or already unsubscribed")
	}
	return nil
}

func IsUserSubscribedForReach(userID int64) bool {
	var sub model.ReachSubscription
	if err := store.DB.Where("user_id = ?", userID).First(&sub).Error; err != nil {
		return true
	}
	return sub.Subscribed
}

// IsUserSubscribedForMarketing 判断营销触达能否发给这名用户。
//
// 与 IsUserSubscribedForReach 的唯一差别在「查不到订阅记录」时的兜底：海外（global）
// 用户必须显式 opt-in，缺记录一律按未订阅跳过；国内用户沿用 EnsureReachSubscription
// 里「注册即订阅」的默认值。查库出错时同样按不发处理——营销邮件宁可少发一封。
func IsUserSubscribedForMarketing(userID int64, region string) bool {
	var sub model.ReachSubscription
	if err := store.DB.Where("user_id = ?", userID).First(&sub).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return !strings.EqualFold(strings.TrimSpace(region), "global")
		}
		return false
	}
	return sub.Subscribed
}
