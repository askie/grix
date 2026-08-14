package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

func TestFriendSearchAndRequestFlow(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(30101)
	visibleID := int64(30102)
	hiddenID := int64(30103)
	pendingTargetID := int64(30104)
	autoApproveTargetID := int64(30105)

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, visibleID)
	seedUser(t, testDB, hiddenID)
	seedUser(t, testDB, pendingTargetID)
	seedUser(t, testDB, autoApproveTargetID)

	if err := testDB.DB.Model(&model.User{}).
		Where("id = ?", ownerID).
		Updates(map[string]any{
			"username": "delegate_worker_owner",
			"nickname": "Owner",
		}).Error; err != nil {
		t.Fatalf("update owner error: %v", err)
	}
	if err := testDB.DB.Model(&model.User{}).
		Where("id = ?", visibleID).
		Updates(map[string]any{
			"username": "delegate_worker_visible",
			"nickname": "Visible",
		}).Error; err != nil {
		t.Fatalf("update visible user error: %v", err)
	}
	if err := testDB.DB.Model(&model.User{}).
		Where("id = ?", hiddenID).
		Updates(map[string]any{
			"username": "delegate_owner_hidden",
			"nickname": "Hidden",
		}).Error; err != nil {
		t.Fatalf("update hidden user error: %v", err)
	}
	if err := testDB.DB.Create(&model.UserSetting{
		UserID:           autoApproveTargetID,
		FriendAddSetting: model.FriendAddSettingAutoApprove,
	}).Error; err != nil {
		t.Fatalf("seed auto approve setting error: %v", err)
	}

	results, err := SearchUsers("delegate", ownerID)
	if err != nil {
		t.Fatalf("SearchUsers() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 visible result, got %#v", results)
	}
	if results[0].ID != visibleID {
		t.Fatalf("expected visible user %d, got %#v", visibleID, results[0])
	}

	t.Run("pending request can be rejected", func(t *testing.T) {
		if err := SendFriendRequest(ownerID, pendingTargetID, "hello"); err != nil {
			t.Fatalf("SendFriendRequest() error = %v", err)
		}

		var req model.FriendRequest
		if err := testDB.DB.
			Where("from_user_id = ? AND to_user_id = ?", ownerID, pendingTargetID).
			First(&req).Error; err != nil {
			t.Fatalf("query pending request error: %v", err)
		}
		if req.Status != friendRequestStatusPending {
			t.Fatalf("expected pending status %d, got %d", friendRequestStatusPending, req.Status)
		}

		if err := HandleFriendRequest(req.ID, pendingTargetID, false); err != nil {
			t.Fatalf("HandleFriendRequest(reject) error = %v", err)
		}
		if err := testDB.DB.First(&req, req.ID).Error; err != nil {
			t.Fatalf("reload rejected request error: %v", err)
		}
		if req.Status != friendRequestStatusRejected {
			t.Fatalf("expected rejected status %d, got %d", friendRequestStatusRejected, req.Status)
		}
	})

	t.Run("auto approve creates friendship immediately", func(t *testing.T) {
		if err := SendFriendRequest(ownerID, autoApproveTargetID, "auto"); err != nil {
			t.Fatalf("SendFriendRequest(auto approve) error = %v", err)
		}

		var req model.FriendRequest
		if err := testDB.DB.
			Where("from_user_id = ? AND to_user_id = ?", ownerID, autoApproveTargetID).
			First(&req).Error; err != nil {
			t.Fatalf("query auto-approved request error: %v", err)
		}
		if req.Status != friendRequestStatusAccepted {
			t.Fatalf("expected accepted status %d, got %d", friendRequestStatusAccepted, req.Status)
		}

		var relCount int64
		if err := testDB.DB.Model(&model.Friend{}).
			Where("user_id = ? AND friend_id = ?", ownerID, autoApproveTargetID).
			Count(&relCount).Error; err != nil {
			t.Fatalf("query owner friendship error: %v", err)
		}
		if relCount != 1 {
			t.Fatalf("expected owner->target friendship, got %d", relCount)
		}
		if err := testDB.DB.Model(&model.Friend{}).
			Where("user_id = ? AND friend_id = ?", autoApproveTargetID, ownerID).
			Count(&relCount).Error; err != nil {
			t.Fatalf("query target friendship error: %v", err)
		}
		if relCount != 1 {
			t.Fatalf("expected target->owner friendship, got %d", relCount)
		}
	})
}

func TestResolveUserIDByUsernameRejectsHiddenUser(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	hiddenID := int64(30201)
	seedUser(t, testDB, hiddenID)
	if err := testDB.DB.Model(&model.User{}).
		Where("id = ?", hiddenID).
		Update("username", "delegate_sender_hidden").Error; err != nil {
		t.Fatalf("update hidden user error: %v", err)
	}

	_, err := ResolveUserIDByUsername("delegate_sender_hidden")
	if err == nil {
		t.Fatal("expected hidden username lookup to fail")
	}
	if err.Error() != "target user not found" {
		t.Fatalf("expected target user not found error, got %v", err)
	}
}
