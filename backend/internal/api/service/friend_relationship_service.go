package service

import (
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FriendItem struct {
	ID         int64     `json:"id,string"`
	UserID     int64     `json:"user_id,string"`
	Username   string    `json:"username"`
	Nickname   string    `json:"nickname"`
	RemarkName string    `json:"remark_name"`
	AvatarURL  string    `json:"avatar_url"`
	CreatedAt  time.Time `json:"created_at"`
}

func GetFriendList(userID int64) ([]FriendItem, error) {
	var friends []model.Friend
	if err := store.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&friends).Error; err != nil {
		return nil, err
	}

	items := make([]FriendItem, 0, len(friends))
	for _, f := range friends {
		var user model.User
		store.DB.First(&user, f.FriendID)
		items = append(items, FriendItem{
			ID:         f.ID,
			UserID:     user.ID,
			Username:   user.Username,
			Nickname:   resolveFriendDisplayNickname(f.RemarkName, user.Nickname, user.Username),
			RemarkName: strings.TrimSpace(f.RemarkName),
			AvatarURL:  user.AvatarURL,
			CreatedAt:  f.CreatedAt,
		})
	}
	return items, nil
}

func UpdateFriendRemark(userID, friendID int64, rawRemarkName string) (*FriendItem, error) {
	if friendID <= 0 {
		return nil, errors.New("invalid friend user")
	}

	remarkName, err := normalizeFriendRemarkName(rawRemarkName)
	if err != nil {
		return nil, err
	}

	var rel model.Friend
	if err := store.DB.
		Where("user_id = ? AND friend_id = ?", userID, friendID).
		First(&rel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("friend not found")
		}
		return nil, err
	}

	if err := store.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", userID, friendID).
		Update("remark_name", remarkName).Error; err != nil {
		return nil, err
	}

	var user model.User
	if err := store.DB.Select("id", "username", "nickname", "avatar_url").
		First(&user, friendID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("friend not found")
		}
		return nil, err
	}

	item := &FriendItem{
		ID:         rel.ID,
		UserID:     user.ID,
		Username:   user.Username,
		Nickname:   resolveFriendDisplayNickname(remarkName, user.Nickname, user.Username),
		RemarkName: remarkName,
		AvatarURL:  user.AvatarURL,
		CreatedAt:  rel.CreatedAt,
	}
	notifyFriendRemarkUpdated(userID, item)
	return item, nil
}

type FriendPinResp struct {
	FriendUserID int64 `json:"friend_user_id,string"`
	IsPinned     bool  `json:"is_pinned"`
	PinnedAt     int64 `json:"pinned_at"`
}

func FriendSetPinned(userID, friendID int64, isPinned bool) (*FriendPinResp, error) {
	if friendID <= 0 || userID == friendID {
		return nil, errors.New("invalid peer user")
	}

	now := time.Now()
	pinnedAt := int64(0)
	var pinnedAtValue *time.Time
	if isPinned {
		pinnedAtValue = &now
		pinnedAt = now.Unix()
	}

	if isPinned {
		// Pin: upsert — create or update the row.
		pin := model.UserPeerPin{
			ID:         snowflake.GenID(),
			UserID:     userID,
			PeerUserID: friendID,
			IsPinned:   true,
			PinnedAt:   pinnedAtValue,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := store.DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "peer_user_id"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"is_pinned":  true,
				"pinned_at":  pinnedAtValue,
				"updated_at": now,
			}),
		}).Create(&pin).Error; err != nil {
			return nil, err
		}
	} else {
		// Unpin: only update an existing row — avoid creating a
		// meaningless is_pinned=false row for peers never pinned.
		store.DB.Model(&model.UserPeerPin{}).
			Where("user_id = ? AND peer_user_id = ? AND is_pinned = ?", userID, friendID, true).
			Updates(map[string]any{
				"is_pinned":  false,
				"pinned_at":  nil,
				"updated_at": now,
			})
	}

	// Sync friends table if a friendship exists (silent no-op otherwise).
	store.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", userID, friendID).
		Updates(map[string]any{
			"is_pinned": isPinned,
			"pinned_at": pinnedAtValue,
		})

	return &FriendPinResp{
		FriendUserID: friendID,
		IsPinned:     isPinned,
		PinnedAt:     pinnedAt,
	}, nil
}

func DeleteFriend(userID, friendID int64) error {
	var deletedForUser bool
	var deletedForFriend bool

	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("user_id = ? AND friend_id = ?", userID, friendID).Delete(&model.Friend{})
		if res.Error != nil {
			return res.Error
		}
		deletedForUser = res.RowsAffected > 0

		res = tx.Where("user_id = ? AND friend_id = ?", friendID, userID).Delete(&model.Friend{})
		if res.Error != nil {
			return res.Error
		}
		deletedForFriend = res.RowsAffected > 0
		return nil
	}); err != nil {
		return err
	}

	if deletedForUser {
		pushFriendDeletedEvent(userID, friendID)
	}
	if deletedForFriend {
		pushFriendDeletedEvent(friendID, userID)
	}
	return nil
}
