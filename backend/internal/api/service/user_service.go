package service

import (
	"errors"
	"regexp"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

const maxUserIntroductionRunes = 300

func GetUserProfile(userID int64) (*model.User, error) {
	var user model.User
	if err := store.DB.First(&user, userID).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

type PublicProfile struct {
	ID           int64  `json:"id,string"`
	Username     string `json:"username"`
	Nickname     string `json:"nickname"`
	Introduction string `json:"introduction"`
	AvatarURL    string `json:"avatar_url"`
	// IsVisitor 标记该资料属于网站挂件访客（不在 users 表），而非正式注册用户。
	// 前端据此在昵称为空时兜底展示“访客”文案。
	IsVisitor bool `json:"is_visitor"`
}

// GetPublicProfile 返回某个 ID 的公开展示资料。
// 先查正式用户；查不到时回退查该请求者名下的挂件访客（widget 访客不在 users 表，
// 但作为会话对方/消息发送者会被前端拿去解析昵称）。二者都查不到返回 gorm.ErrRecordNotFound，
// 由调用方按“查无此人”处理。requesterID 用于把访客回退限定在归属自己的会话，避免越权读取他人访客。
func GetPublicProfile(requesterID, targetID int64) (*PublicProfile, error) {
	var user model.User
	err := store.DB.First(&user, targetID).Error
	if err == nil {
		return &PublicProfile{
			ID:           user.ID,
			Username:     user.Username,
			Nickname:     user.Nickname,
			Introduction: user.Introduction,
			AvatarURL:    user.AvatarURL,
		}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var visitor model.WidgetSession
	verr := store.DB.
		Where("visitor_id = ? AND owner_user_id = ?", targetID, requesterID).
		Order("updated_at DESC").
		First(&visitor).Error
	if verr == nil {
		return &PublicProfile{
			ID:        targetID,
			Nickname:  strings.TrimSpace(visitor.VisitorName),
			IsVisitor: true,
		}, nil
	}
	if !errors.Is(verr, gorm.ErrRecordNotFound) {
		return nil, verr
	}
	return nil, gorm.ErrRecordNotFound
}

func normalizeUserIntroduction(raw string) (string, error) {
	normalized, err := normalizeIntroductionText(raw, maxUserIntroductionRunes)
	if err == nil {
		return normalized, nil
	}
	switch {
	case errors.Is(err, errIntroductionTooLong):
		return "", errors.New("个人介绍长度不能超过 300 个字符")
	case errors.Is(err, errIntroductionInvalidControl):
		return "", errors.New("个人介绍包含非法控制字符")
	default:
		return "", err
	}
}

func UpdateUserProfile(userID int64, nickname, avatarURL, introduction *string) error {
	updates := map[string]interface{}{}
	if nickname != nil {
		updates["nickname"] = *nickname
	}
	if avatarURL != nil {
		updates["avatar_url"] = *avatarURL
	}
	if introduction != nil {
		normalizedIntroduction, err := normalizeUserIntroduction(*introduction)
		if err != nil {
			return err
		}
		updates["introduction"] = normalizedIntroduction
	}
	if len(updates) == 0 {
		return nil
	}
	return store.DB.Model(&model.User{}).Where("id = ?", userID).Updates(updates).Error
}

func UpdateUsername(userID int64, newUsername string) error {
	var user model.User
	if err := store.DB.First(&user, userID).Error; err != nil {
		return errors.New("用户不存在")
	}

	if user.UsernameModified {
		return errors.New("用户名已经被修改过，无法再次修改")
	}

	// Rule: only alphanumerics and underscores, 4-20 chars
	matched, _ := regexp.MatchString("^[a-zA-Z0-9_]{4,20}$", newUsername)
	if !matched {
		return errors.New("用户名必须为4-20位字母、数字或下划线")
	}

	var count int64
	store.DB.Model(&model.User{}).Where("username = ?", newUsername).Count(&count)
	if count > 0 {
		return errors.New("用户名已存在")
	}

	return store.DB.Model(&user).Updates(map[string]interface{}{
		"username":          newUsername,
		"username_modified": true,
	}).Error
}
