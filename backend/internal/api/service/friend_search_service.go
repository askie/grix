package service

import (
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

type UserSearchResult struct {
	ID        int64  `json:"id,string"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
}

func SearchUsers(keyword string, currentUserID int64) ([]UserSearchResult, error) {
	var users []model.User
	query := store.DB.
		Where(
			"(LOWER(username) LIKE LOWER(?) OR LOWER(nickname) LIKE LOWER(?)) AND id != ? AND status = ?",
			"%"+keyword+"%",
			"%"+keyword+"%",
			currentUserID,
			model.UserStatusActive,
		)
	query = applyFriendSearchVisibilityFilter(query)
	if err := query.Limit(20).Find(&users).Error; err != nil {
		return nil, err
	}

	results := make([]UserSearchResult, 0, len(users))
	for _, u := range users {
		results = append(results, UserSearchResult{
			ID:        u.ID,
			Username:  u.Username,
			Nickname:  u.Nickname,
			AvatarURL: u.AvatarURL,
		})
	}
	return results, nil
}

func ResolveUserIDByUsername(username string) (int64, error) {
	name := strings.TrimSpace(username)
	if name == "" {
		return 0, errors.New("target username is required")
	}
	if isHiddenFriendSearchUsername(name) {
		return 0, errors.New("target user not found")
	}

	var user model.User
	if err := store.DB.Select("id").
		Where("username = ? AND status = ?", name, model.UserStatusActive).
		First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, errors.New("target user not found")
		}
		return 0, err
	}
	return user.ID, nil
}
