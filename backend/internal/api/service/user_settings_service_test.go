package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

func TestGetUserSettings(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(9101)
	seedUser(t, testDB, userID)

	resp, err := GetUserSettings(userID)
	if err != nil {
		t.Fatalf("GetUserSettings() error = %v", err)
	}
	if resp.Chat.AutoDelegateAgentID != nil {
		t.Fatalf("expected empty auto delegate agent id, got %v", *resp.Chat.AutoDelegateAgentID)
	}
	if resp.PreferredLanguage != preferredLanguageZH {
		t.Fatalf("expected default preferred_language=%q, got %q", preferredLanguageZH, resp.PreferredLanguage)
	}
	if resp.Chat.FriendAddSetting != model.FriendAddSettingNeedApproval {
		t.Fatalf(
			"expected default friend_add_setting=%d, got %d",
			model.FriendAddSettingNeedApproval,
			resp.Chat.FriendAddSetting,
		)
	}
	if !resp.Chat.AllowGroupInvite {
		t.Fatal("expected default allow_group_invite=true")
	}
}

func TestUpdateUserSettings(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(9201)
	agentID := int64(9202)
	seedUser(t, testDB, userID)
	seedAgent(t, testDB, agentID, userID, 1)

	t.Run("set and clear auto delegate agent", func(t *testing.T) {
		rawAgentID := fmt.Sprintf("%d", agentID)
		resp, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AutoDelegateAgentID: OptionalStringUpdate{
					Set:   true,
					Value: &rawAgentID,
				},
			},
		})
		if err != nil {
			t.Fatalf("UpdateUserSettings(set) error = %v", err)
		}
		if resp.Chat.AutoDelegateAgentID == nil || *resp.Chat.AutoDelegateAgentID != agentID {
			t.Fatalf("expected auto_delegate_agent_id=%d, got %#v", agentID, resp.Chat.AutoDelegateAgentID)
		}

		blank := ""
		resp, err = UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AutoDelegateAgentID: OptionalStringUpdate{
					Set:   true,
					Value: &blank,
				},
			},
		})
		if err != nil {
			t.Fatalf("UpdateUserSettings(clear) error = %v", err)
		}
		if resp.Chat.AutoDelegateAgentID != nil {
			t.Fatalf("expected auto_delegate_agent_id cleared, got %v", *resp.Chat.AutoDelegateAgentID)
		}
		if resp.Chat.FriendAddSetting != model.FriendAddSettingNeedApproval {
			t.Fatalf(
				"expected friend_add_setting remain default=%d, got %d",
				model.FriendAddSettingNeedApproval,
				resp.Chat.FriendAddSetting,
			)
		}
		if !resp.Chat.AllowGroupInvite {
			t.Fatal("expected allow_group_invite remain true")
		}
		if resp.PreferredLanguage != preferredLanguageZH {
			t.Fatalf("expected preferred_language remain default=%q, got %q", preferredLanguageZH, resp.PreferredLanguage)
		}
	})

	t.Run("update preferred language independently", func(t *testing.T) {
		language := "en-US"
		resp, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			PreferredLanguage: OptionalStringUpdate{
				Set:   true,
				Value: &language,
			},
		})
		if err != nil {
			t.Fatalf("UpdateUserSettings(update preferred_language) error = %v", err)
		}
		if resp.PreferredLanguage != preferredLanguageEN {
			t.Fatalf("expected preferred_language=%q, got %q", preferredLanguageEN, resp.PreferredLanguage)
		}
		if resp.Chat.FriendAddSetting != model.FriendAddSettingNeedApproval {
			t.Fatalf("expected friend_add_setting remain default=%d, got %d", model.FriendAddSettingNeedApproval, resp.Chat.FriendAddSetting)
		}
		if !resp.Chat.AllowGroupInvite {
			t.Fatal("expected allow_group_invite remain true")
		}
	})

	t.Run("update preferred language to french", func(t *testing.T) {
		language := "fr-FR"
		resp, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			PreferredLanguage: OptionalStringUpdate{
				Set:   true,
				Value: &language,
			},
		})
		if err != nil {
			t.Fatalf("UpdateUserSettings(fr-FR) error = %v", err)
		}
		if resp.PreferredLanguage != "fr" {
			t.Fatalf("expected preferred_language=%q, got %q", "fr", resp.PreferredLanguage)
		}
	})

	t.Run("reject invalid preferred language", func(t *testing.T) {
		language := "xx-INVALID"
		_, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			PreferredLanguage: OptionalStringUpdate{
				Set:   true,
				Value: &language,
			},
		})
		if !errors.Is(err, ErrUserSettingsInvalidLanguage) {
			t.Fatalf("expected ErrUserSettingsInvalidLanguage, got %v", err)
		}
	})

	t.Run("update friend add mode without overriding auto delegate agent", func(t *testing.T) {
		rawAgentID := fmt.Sprintf("%d", agentID)
		if _, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AutoDelegateAgentID: OptionalStringUpdate{
					Set:   true,
					Value: &rawAgentID,
				},
			},
		}); err != nil {
			t.Fatalf("UpdateUserSettings(set auto delegate) error = %v", err)
		}

		mode := model.FriendAddSettingAutoApprove
		resp, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				FriendAddSetting: &mode,
			},
		})
		if err != nil {
			t.Fatalf("UpdateUserSettings(update friend_add_setting) error = %v", err)
		}
		if resp.Chat.AutoDelegateAgentID == nil || *resp.Chat.AutoDelegateAgentID != agentID {
			t.Fatalf("expected auto_delegate_agent_id=%d preserved, got %#v", agentID, resp.Chat.AutoDelegateAgentID)
		}
		if resp.Chat.FriendAddSetting != mode {
			t.Fatalf("expected friend_add_setting=%d, got %d", mode, resp.Chat.FriendAddSetting)
		}
	})

	t.Run("update allow group invite independently", func(t *testing.T) {
		allowGroupInvite := false
		resp, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AllowGroupInvite: &allowGroupInvite,
			},
		})
		if err != nil {
			t.Fatalf("UpdateUserSettings(update allow_group_invite) error = %v", err)
		}
		if resp.Chat.AllowGroupInvite != allowGroupInvite {
			t.Fatalf(
				"expected allow_group_invite=%t, got %t",
				allowGroupInvite,
				resp.Chat.AllowGroupInvite,
			)
		}
	})

	t.Run("reject invalid payload", func(t *testing.T) {
		_, err := UpdateUserSettings(userID, UserSettingsUpdateReq{})
		if !errors.Is(err, ErrUserSettingsInvalidPayload) {
			t.Fatalf("expected ErrUserSettingsInvalidPayload, got %v", err)
		}
	})

	t.Run("reject empty chat update", func(t *testing.T) {
		_, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{},
		})
		if !errors.Is(err, ErrUserSettingsInvalidPayload) {
			t.Fatalf("expected ErrUserSettingsInvalidPayload for empty chat update, got %v", err)
		}
	})

	t.Run("reject invalid agent id", func(t *testing.T) {
		bad := "abc"
		_, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AutoDelegateAgentID: OptionalStringUpdate{
					Set:   true,
					Value: &bad,
				},
			},
		})
		if !errors.Is(err, ErrUserSettingsInvalidAgentID) {
			t.Fatalf("expected ErrUserSettingsInvalidAgentID, got %v", err)
		}
	})

	t.Run("clear auto delegate agent with explicit null", func(t *testing.T) {
		rawAgentID := fmt.Sprintf("%d", agentID)
		if _, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AutoDelegateAgentID: OptionalStringUpdate{
					Set:   true,
					Value: &rawAgentID,
				},
			},
		}); err != nil {
			t.Fatalf("UpdateUserSettings(set auto delegate) error = %v", err)
		}

		resp, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AutoDelegateAgentID: OptionalStringUpdate{
					Set:   true,
					Value: nil,
				},
			},
		})
		if err != nil {
			t.Fatalf("UpdateUserSettings(clear by explicit null) error = %v", err)
		}
		if resp.Chat.AutoDelegateAgentID != nil {
			t.Fatalf("expected auto_delegate_agent_id cleared by explicit null, got %v", *resp.Chat.AutoDelegateAgentID)
		}
	})

	t.Run("reject invalid friend add mode", func(t *testing.T) {
		mode := int8(9)
		_, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				FriendAddSetting: &mode,
			},
		})
		if !errors.Is(err, ErrUserSettingsInvalidFriendAddMode) {
			t.Fatalf("expected ErrUserSettingsInvalidFriendAddMode, got %v", err)
		}
	})

	t.Run("reject foreign agent", func(t *testing.T) {
		otherUserID := int64(9203)
		foreignAgentID := int64(9204)
		seedUser(t, testDB, otherUserID)
		seedAgent(t, testDB, foreignAgentID, otherUserID, 1)

		rawForeignID := fmt.Sprintf("%d", foreignAgentID)
		_, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AutoDelegateAgentID: OptionalStringUpdate{
					Set:   true,
					Value: &rawForeignID,
				},
			},
		})
		if !errors.Is(err, ErrUserSettingsAutoAgentNotOwned) {
			t.Fatalf("expected ErrUserSettingsAutoAgentNotOwned, got %v", err)
		}
	})

	t.Run("reject unavailable agent", func(t *testing.T) {
		disabledAgentID := int64(9205)
		seedAgent(t, testDB, disabledAgentID, userID, 2)

		rawDisabledID := fmt.Sprintf("%d", disabledAgentID)
		_, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AutoDelegateAgentID: OptionalStringUpdate{
					Set:   true,
					Value: &rawDisabledID,
				},
			},
		})
		if !errors.Is(err, ErrUserSettingsAutoAgentUnavailable) {
			t.Fatalf("expected ErrUserSettingsAutoAgentUnavailable, got %v", err)
		}
	})
}

func TestUserSettingsUpdateReqJSONDecoding(t *testing.T) {
	t.Run("missing auto_delegate_agent_id field", func(t *testing.T) {
		var req UserSettingsUpdateReq
		if err := json.Unmarshal([]byte(`{"chat":{"friend_add_setting":2}}`), &req); err != nil {
			t.Fatalf("json unmarshal error = %v", err)
		}
		if req.Chat == nil {
			t.Fatalf("expected chat to be non-nil")
		}
		if req.Chat.AutoDelegateAgentID.Set {
			t.Fatalf("expected auto_delegate_agent_id set=false when field is absent")
		}
	})

	t.Run("null auto_delegate_agent_id field", func(t *testing.T) {
		var req UserSettingsUpdateReq
		if err := json.Unmarshal([]byte(`{"chat":{"auto_delegate_agent_id":null}}`), &req); err != nil {
			t.Fatalf("json unmarshal error = %v", err)
		}
		if req.Chat == nil {
			t.Fatalf("expected chat to be non-nil")
		}
		if !req.Chat.AutoDelegateAgentID.Set {
			t.Fatalf("expected auto_delegate_agent_id set=true when field is null")
		}
		if req.Chat.AutoDelegateAgentID.Value != nil {
			t.Fatalf("expected auto_delegate_agent_id value=nil when field is null")
		}
	})

	t.Run("string auto_delegate_agent_id field", func(t *testing.T) {
		var req UserSettingsUpdateReq
		if err := json.Unmarshal([]byte(`{"chat":{"auto_delegate_agent_id":"9202"}}`), &req); err != nil {
			t.Fatalf("json unmarshal error = %v", err)
		}
		if req.Chat == nil {
			t.Fatalf("expected chat to be non-nil")
		}
		if !req.Chat.AutoDelegateAgentID.Set {
			t.Fatalf("expected auto_delegate_agent_id set=true when field is present")
		}
		if req.Chat.AutoDelegateAgentID.Value == nil || *req.Chat.AutoDelegateAgentID.Value != "9202" {
			t.Fatalf("expected auto_delegate_agent_id value=9202, got %#v", req.Chat.AutoDelegateAgentID.Value)
		}
	})

	t.Run("string preferred_language field", func(t *testing.T) {
		var req UserSettingsUpdateReq
		if err := json.Unmarshal([]byte(`{"preferred_language":"en-US"}`), &req); err != nil {
			t.Fatalf("json unmarshal error = %v", err)
		}
		if !req.PreferredLanguage.Set {
			t.Fatalf("expected preferred_language set=true when field is present")
		}
		if req.PreferredLanguage.Value == nil || *req.PreferredLanguage.Value != "en-US" {
			t.Fatalf("expected preferred_language value=en-US, got %#v", req.PreferredLanguage.Value)
		}
	})
}
