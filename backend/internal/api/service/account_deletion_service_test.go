package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/api/service/identity"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
)

func TestDeleteAccount(t *testing.T) {
	testDB := setupServiceTest(t)
	defer testDB.Close()

	store.RDB = testutil.NewMockRedis()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.ID = 9001001
		u.Username = "delete_me"
		u.Email = "delete_me@example.com"
		u.Nickname = "Delete Me"
	})
	friend := fixture.CreateUser(func(u *model.User) {
		u.ID = 9001002
		u.Username = "friend_user"
		u.Email = "friend_user@example.com"
		u.Nickname = "Friend User"
	})

	groupSession := fixture.CreateSession(func(s *model.Session) {
		s.SessionID = "group-delete-account"
		s.OwnerID = user.ID
		s.SessionType = 2
		s.GroupName = "Delete Group"
	})
	privateSession := fixture.CreateSession(func(s *model.Session) {
		s.SessionID = "private-delete-account"
		s.OwnerID = user.ID
		s.SessionType = 1
	})

	agent := &model.Agent{
		ID:        9100001,
		AgentName: "Delete Agent",
		OwnerID:   user.ID,
		Status:    1,
	}
	if err := testDB.DB.Create(agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	seedRows := []any{
		&model.Device{UserID: user.ID, Platform: model.DevicePlatformIOS, PushEnv: model.DevicePushEnvAPNsProduction, DeviceToken: "token-1", DeviceID: "dev-1", IsActive: true},
		&model.OAuthAccount{ID: 9200001, UserID: user.ID, Provider: "google", ProviderUID: "uid-1"},
		&model.RefreshToken{JTI: "jti-1", UserID: user.ID, FamilyID: "family-1", Status: model.RefreshTokenStatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)},
		&model.Friend{ID: 9300001, UserID: user.ID, FriendID: friend.ID},
		&model.Friend{ID: 9300002, UserID: friend.ID, FriendID: user.ID},
		&model.FriendRequest{ID: 9300003, FromUserID: user.ID, ToUserID: friend.ID, Status: 0},
		&model.UserInbox{UserID: user.ID, InboxSeq: 1, MsgID: 1, SessionID: privateSession.SessionID},
		&model.SessionHistoryReset{SessionID: privateSession.SessionID, UserID: user.ID, DeletedBefore: time.Now().UTC()},
		&model.LLMUsageLog{ID: 9400001, UserID: user.ID, SessionID: privateSession.SessionID, AgentID: agent.ID},
		&model.DelegationLog{ID: 9500001, UserID: user.ID, SessionID: privateSession.SessionID, AgentID: agent.ID, Action: "start"},
		&model.KnowledgeDoc{ID: 9600001, AgentID: agent.ID, ChunkText: "chunk"},
		&model.SessionMember{SessionID: groupSession.SessionID, MemberID: user.ID, MemberType: 1, Role: 3, JoinedAt: time.Now().UTC(), LastActiveAt: time.Now().UTC()},
		&model.SessionMember{SessionID: groupSession.SessionID, MemberID: friend.ID, MemberType: 1, Role: 1, JoinedAt: time.Now().UTC(), LastActiveAt: time.Now().UTC()},
		&model.SessionMember{SessionID: privateSession.SessionID, MemberID: user.ID, MemberType: 1, Role: 3, JoinedAt: time.Now().UTC(), LastActiveAt: time.Now().UTC()},
		&model.SessionMember{SessionID: privateSession.SessionID, MemberID: friend.ID, MemberType: 1, Role: 3, JoinedAt: time.Now().UTC(), LastActiveAt: time.Now().UTC()},
	}
	for _, row := range seedRows {
		if err := testDB.DB.Create(row).Error; err != nil {
			t.Fatalf("seed row %T: %v", row, err)
		}
	}

	if err := DeleteAccount(user.ID); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	var deletedUser model.User
	if err := testDB.DB.First(&deletedUser, user.ID).Error; err != nil {
		t.Fatalf("load deleted user: %v", err)
	}
	if deletedUser.Status != model.UserStatusDeleted {
		t.Fatalf("expected deleted status, got %d", deletedUser.Status)
	}
	if deletedUser.Nickname != deletedUserNickname {
		t.Fatalf("expected nickname %q, got %q", deletedUserNickname, deletedUser.Nickname)
	}
	if deletedUser.AvatarURL != "" || deletedUser.PasswordHash != "" {
		t.Fatalf("expected credentials scrubbed, avatar=%q password=%q", deletedUser.AvatarURL, deletedUser.PasswordHash)
	}

	assertZeroCount := func(table string, query string, args ...any) {
		t.Helper()
		var count int64
		if err := testDB.DB.Table(table).Where(query, args...).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected %s count 0, got %d", table, count)
		}
	}

	assertZeroCount("devices", "user_id = ?", user.ID)
	assertZeroCount("oauth_accounts", "user_id = ?", user.ID)
	assertZeroCount("auth_refresh_tokens", "user_id = ?", user.ID)
	assertZeroCount("friend_requests", "from_user_id = ? OR to_user_id = ?", user.ID, user.ID)
	assertZeroCount("friends", "user_id = ? OR friend_id = ?", user.ID, user.ID)
	assertZeroCount("user_inbox", "user_id = ?", user.ID)
	assertZeroCount("session_history_resets", "user_id = ?", user.ID)
	assertZeroCount("llm_usage_logs", "user_id = ?", user.ID)
	assertZeroCount("delegation_logs", "user_id = ?", user.ID)
	assertZeroCount("knowledge_docs", "agent_id = ?", agent.ID)

	var deletedAgent model.Agent
	if err := testDB.DB.First(&deletedAgent, agent.ID).Error; err != nil {
		t.Fatalf("load deleted agent: %v", err)
	}
	if deletedAgent.Status != 3 {
		t.Fatalf("expected agent deleted status, got %d", deletedAgent.Status)
	}
	if deletedAgent.APIKeyHash != "" || deletedAgent.APIKeyHint != "" {
		t.Fatalf("expected agent secrets scrubbed")
	}

	var deletedGroup model.Session
	if err := testDB.DB.First(&deletedGroup, "session_id = ?", groupSession.SessionID).Error; err != nil {
		t.Fatalf("load deleted group: %v", err)
	}
	if !deletedGroup.IsDeleted {
		t.Fatalf("expected owned group session deleted")
	}

	var privateMembers int64
	if err := testDB.DB.Model(&model.SessionMember{}).
		Where("session_id = ?", privateSession.SessionID).
		Count(&privateMembers).Error; err != nil {
		t.Fatalf("count private members: %v", err)
	}
	if privateMembers != 2 {
		t.Fatalf("expected private session members kept, got %d", privateMembers)
	}
}

func TestDeleteAccountReleasesPhoneForBinding(t *testing.T) {
	_, cleanup := setupAuthTest(t)
	defer cleanup()
	writeSmsSettings(t, systemsetting.SmsSettings{
		PhoneLoginEnabledCN:    true,
		PhoneRegisterEnabledCN: true,
		AllowedCountryCodesCN:  []string{"+86"},
	})

	const phone = "+8613800138000"
	fixture := testutil.NewFixtureBuilder(store.DB)
	deleted := fixture.CreateUser(func(u *model.User) {
		u.ID = 9001101
		u.Username = "deleted_phone_owner"
		u.Email = "deleted_phone_owner@example.com"
	})
	recipient := fixture.CreateUser(func(u *model.User) {
		u.ID = 9001102
		u.Username = "next_phone_owner"
		u.Email = "next_phone_owner@example.com"
	})
	seedPhoneForAccountDeletion(t, deleted.ID, phone)

	if err := DeleteAccount(deleted.ID); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	var scrubbed model.User
	if err := store.DB.First(&scrubbed, deleted.ID).Error; err != nil {
		t.Fatalf("load deleted user: %v", err)
	}
	if scrubbed.PhoneE164 != "" || scrubbed.PhoneCipher != "" || scrubbed.PhoneLast4 != "" ||
		scrubbed.PhoneBlind != "" || scrubbed.PhoneCountry != "" {
		t.Fatalf("expected all phone fields cleared, got %+v", scrubbed)
	}
	var identityCount int64
	if err := store.DB.Model(&model.UserIdentity{}).
		Where("user_id = ? AND provider IN ?", deleted.ID, []string{model.IdentityProviderPhoneSmsCN, model.IdentityProviderPhoneSmsGlobal}).
		Count(&identityCount).Error; err != nil {
		t.Fatalf("count phone identities: %v", err)
	}
	if identityCount != 0 {
		t.Fatalf("expected phone identities deleted, got %d", identityCount)
	}

	if err := phoneSmsStore.StoreCode(context.Background(), string(identity.SmsSceneBind), phone, "654321"); err != nil {
		t.Fatalf("store bind code: %v", err)
	}
	if err := BindUserPhone(recipient.ID, phone, "654321", "127.0.0.1"); err != nil {
		t.Fatalf("BindUserPhone() after account deletion error = %v", err)
	}
}

func TestDeleteAccountAllowsNewPhoneRegistration(t *testing.T) {
	_, cleanup := setupAuthTest(t)
	defer cleanup()
	writeSmsSettings(t, systemsetting.SmsSettings{
		PhoneLoginEnabledCN:    true,
		PhoneRegisterEnabledCN: true,
		AllowedCountryCodesCN:  []string{"+86"},
	})

	const phone = "+8613800138001"
	fixture := testutil.NewFixtureBuilder(store.DB)
	deleted := fixture.CreateUser(func(u *model.User) {
		u.ID = 9001103
		u.Username = "deleted_phone_login"
		u.Email = "deleted_phone_login@example.com"
	})
	seedPhoneForAccountDeletion(t, deleted.ID, phone)
	if err := DeleteAccount(deleted.ID); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	if err := phoneSmsStore.StoreCode(context.Background(), string(identity.SmsSceneLogin), phone, "654321"); err != nil {
		t.Fatalf("store login code: %v", err)
	}
	resp, err := PhoneLoginWithCode(phone, "654321", testAuthDeviceID, testAuthPlatform, testAuthLanguage, "127.0.0.1")
	if err != nil {
		t.Fatalf("PhoneLoginWithCode() after account deletion error = %v", err)
	}
	if resp.User.ID == deleted.ID {
		t.Fatalf("expected a new account, got deleted user %d", deleted.ID)
	}
	var phoneIdentity model.UserIdentity
	if err := store.DB.Where("user_id = ? AND provider = ?", resp.User.ID, model.IdentityProviderPhoneSmsCN).
		First(&phoneIdentity).Error; err != nil {
		t.Fatalf("load new phone identity: %v", err)
	}
}

func seedPhoneForAccountDeletion(t *testing.T, userID int64, phone string) {
	t.Helper()
	cipher, last4, blind, err := phoneStorageFields(phone)
	if err != nil {
		t.Fatalf("build phone fields: %v", err)
	}
	if err := store.DB.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"phone_e164":    phone,
		"phone_cipher":  cipher,
		"phone_last4":   last4,
		"phone_blind":   blind,
		"phone_country": "+86",
	}).Error; err != nil {
		t.Fatalf("seed user phone: %v", err)
	}
	if err := store.DB.Create(&model.UserIdentity{
		ID:          userID + 100000,
		UserID:      userID,
		Provider:    model.IdentityProviderPhoneSmsCN,
		ExternalID:  blind,
		CountryCode: "+86",
		PrimaryFlag: true,
	}).Error; err != nil {
		t.Fatalf("seed phone identity: %v", err)
	}
}
