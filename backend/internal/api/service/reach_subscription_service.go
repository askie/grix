package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
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
