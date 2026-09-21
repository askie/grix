package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/syncstream"
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

func FriendSetPinned(userID, friendID int64, isPinned bool, commandIDs ...string) (*FriendPinResp, error) {
	if friendID <= 0 || userID == friendID {
		return nil, errors.New("invalid peer user")
	}

	now := time.Now()
	pinnedAt := int64(0)
	var pinnedAtValue *time.Time
	commandID := optionalCommandID(commandIDs)
	mutated := false
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		claimed, err := syncstream.ClaimCommandTx(tx, userID, "peer.pin", commandID, map[string]any{"peer_user_id": friendID, "is_pinned": isPinned})
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
		mutated = true
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
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "user_id"},
					{Name: "peer_user_id"},
				},
				DoUpdates: clause.Assignments(map[string]any{
					"is_pinned":     true,
					"pinned_at":     pinnedAtValue,
					"updated_at":    now,
					"state_version": gorm.Expr("state_version + 1"),
				}),
			}).Create(&pin).Error; err != nil {
				return err
			}
		} else {
			// Unpin: only update an existing row — avoid creating a
			// meaningless is_pinned=false row for peers never pinned.
			pin := model.UserPeerPin{ID: snowflake.GenID(), UserID: userID, PeerUserID: friendID, IsPinned: false, CreatedAt: now, UpdatedAt: now}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "peer_user_id"}}, DoUpdates: clause.Assignments(map[string]any{"is_pinned": false, "pinned_at": nil, "updated_at": now, "state_version": gorm.Expr("state_version + 1")})}).Create(&pin).Error; err != nil {
				return err
			}
		}

		// Sync friends table if a friendship exists (silent no-op otherwise).
		if err := tx.Model(&model.Friend{}).
			Where("user_id = ? AND friend_id = ?", userID, friendID).
			Updates(map[string]any{
				"is_pinned": isPinned,
				"pinned_at": pinnedAtValue,
			}).Error; err != nil {
			return err
		}
		var currentPin model.UserPeerPin
		if err := tx.Where("user_id = ? AND peer_user_id = ?", userID, friendID).First(&currentPin).Error; err != nil {
			return err
		}
		_, err = syncstream.AppendTx(tx, []syncstream.Event{{UserID: userID, Kind: "session.pin_changed", EntityType: "peer", EntityID: fmt.Sprintf("%d", friendID), EntityVersion: currentPin.StateVersion, CommandID: commandID, Payload: map[string]any{"peer_user_id": friendID, "is_pinned": isPinned, "pinned_at": pinnedAt, "state_version": currentPin.StateVersion}}})
		return err
	}); err != nil {
		return nil, err
	}
	if mutated {
		notifySyncV2Dirty(userID)
	}
	var currentPin model.UserPeerPin
	if err := store.DB.Where("user_id = ? AND peer_user_id = ?", userID, friendID).First(&currentPin).Error; err != nil {
		return nil, err
	}
	pinnedAt = 0
	if currentPin.PinnedAt != nil {
		pinnedAt = currentPin.PinnedAt.Unix()
	}

	return &FriendPinResp{
		FriendUserID: friendID,
		IsPinned:     currentPin.IsPinned,
		PinnedAt:     pinnedAt,
	}, nil
}

type FriendMuteResp struct {
	FriendUserID int64 `json:"friend_user_id,string"`
	IsMuted      bool  `json:"is_muted"`
}

func FriendSetMuted(userID, friendID int64, isMuted bool, commandIDs ...string) (*FriendMuteResp, error) {
	if friendID <= 0 || userID == friendID {
		return nil, errors.New("invalid peer user")
	}

	now := time.Now()
	var mutedAtValue *time.Time
	commandID := optionalCommandID(commandIDs)
	mutated := false
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		claimed, err := syncstream.ClaimCommandTx(tx, userID, "peer.mute", commandID, map[string]any{"peer_user_id": friendID, "is_muted": isMuted})
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
		mutated = true
		if isMuted {
			mutedAtValue = &now
		}

		if isMuted {
			mute := model.UserPeerMute{
				ID:         snowflake.GenID(),
				UserID:     userID,
				PeerUserID: friendID,
				IsMuted:    true,
				MutedAt:    mutedAtValue,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "user_id"},
					{Name: "peer_user_id"},
				},
				DoUpdates: clause.Assignments(map[string]any{
					"is_muted":      true,
					"muted_at":      mutedAtValue,
					"updated_at":    now,
					"state_version": gorm.Expr("state_version + 1"),
				}),
			}).Create(&mute).Error; err != nil {
				return err
			}
		} else {
			mute := model.UserPeerMute{ID: snowflake.GenID(), UserID: userID, PeerUserID: friendID, IsMuted: false, CreatedAt: now, UpdatedAt: now}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "peer_user_id"}}, DoUpdates: clause.Assignments(map[string]any{"is_muted": false, "muted_at": nil, "updated_at": now, "state_version": gorm.Expr("state_version + 1")})}).Create(&mute).Error; err != nil {
				return err
			}
		}
		var currentMute model.UserPeerMute
		if err := tx.Where("user_id = ? AND peer_user_id = ?", userID, friendID).First(&currentMute).Error; err != nil {
			return err
		}
		_, err = syncstream.AppendTx(tx, []syncstream.Event{{UserID: userID, Kind: "session.mute_changed", EntityType: "peer", EntityID: fmt.Sprintf("%d", friendID), EntityVersion: currentMute.StateVersion, CommandID: commandID, Payload: map[string]any{"peer_user_id": friendID, "is_muted": isMuted, "state_version": currentMute.StateVersion}}})
		return err
	}); err != nil {
		return nil, err
	}
	if mutated {
		notifySyncV2Dirty(userID)
	}
	var currentMute model.UserPeerMute
	if err := store.DB.Where("user_id = ? AND peer_user_id = ?", userID, friendID).First(&currentMute).Error; err != nil {
		return nil, err
	}

	return &FriendMuteResp{
		FriendUserID: friendID,
		IsMuted:      currentMute.IsMuted,
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
