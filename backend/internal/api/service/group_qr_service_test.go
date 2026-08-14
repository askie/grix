package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/systemsetting"
)

func TestGetOrCreateGroupQRCode(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		sessionID = "group-qr-session-1"
		ownerID   = int64(930001)
		memberID  = int64(930002)
		adminID   = int64(930004)
		outsider  = int64(930003)
	)

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, memberID)
	seedUser(t, testDB, adminID)
	seedUser(t, testDB, outsider)

	now := time.Now()
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		GroupName:      "QR Team",
		LastMsgSummary: "QR Team",
	}).Error; err != nil {
		t.Fatalf("create group session error: %v", err)
	}
	if err := testDB.DB.Create([]model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: adminID, MemberType: 1, Role: 2, JoinedAt: now, LastActiveAt: now},
	}).Error; err != nil {
		t.Fatalf("create group members error: %v", err)
	}

	info, err := GetOrCreateGroupQRCode(ownerID, sessionID)
	if err != nil {
		t.Fatalf("GetOrCreateGroupQRCode error: %v", err)
	}
	if info.Code == "" {
		t.Fatal("expected non-empty code")
	}
	if !strings.Contains(info.ShareURL, "/"+info.Code) {
		t.Fatalf("expected share url contains code, got %s", info.ShareURL)
	}

	info2, err := GetOrCreateGroupQRCode(adminID, sessionID)
	if err != nil {
		t.Fatalf("GetOrCreateGroupQRCode second call error: %v", err)
	}
	if info2.Code != info.Code {
		t.Fatalf("expected stable group qr code, got %s want %s", info2.Code, info.Code)
	}

	_, err = GetOrCreateGroupQRCode(memberID, sessionID)
	if !errors.Is(err, ErrSessionRoleDenied) {
		t.Fatalf("expected ErrSessionRoleDenied, got %v", err)
	}

	_, err = GetOrCreateGroupQRCode(outsider, sessionID)
	if !errors.Is(err, ErrSessionPermissionDenied) {
		t.Fatalf("expected ErrSessionPermissionDenied, got %v", err)
	}

	privateSessionID := "group-qr-private-session"
	if err := testDB.DB.Create(&model.Session{
		SessionID:      privateSessionID,
		OwnerID:        ownerID,
		SessionType:    1,
		LastMsgSummary: "private",
	}).Error; err != nil {
		t.Fatalf("create private session error: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID: privateSessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("create private member error: %v", err)
	}
	_, err = GetOrCreateGroupQRCode(ownerID, privateSessionID)
	if !errors.Is(err, ErrSessionInvalidType) {
		t.Fatalf("expected ErrSessionInvalidType, got %v", err)
	}
}

func TestGroupQRCodeRejectsBannedGroup(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		sessionID = "group-qr-banned"
		ownerID   = int64(939001)
		joinerID  = int64(939002)
	)

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, joinerID)

	now := time.Now()
	if err := testDB.DB.Create(&model.Session{
		SessionID:        sessionID,
		OwnerID:          ownerID,
		SessionType:      model.SessionTypeGroup,
		GroupName:        "Banned QR Group",
		ModerationStatus: model.SessionModerationStatusBanned,
	}).Error; err != nil {
		t.Fatalf("create banned group session error: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("create owner membership error: %v", err)
	}
	if err := testDB.DB.Create(&model.GroupQRCode{
		SessionID:     sessionID,
		Code:          "banned-group-qr",
		CreatorUserID: ownerID,
		ExpiresAt:     now.Add(24 * time.Hour),
		RotatedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create qr code error: %v", err)
	}

	_, err := GetOrCreateGroupQRCode(ownerID, sessionID)
	if !errors.Is(err, ErrSessionGroupBanned) {
		t.Fatalf("expected ErrSessionGroupBanned from GetOrCreateGroupQRCode, got %v", err)
	}

	_, err = ResolveGroupQRCode(joinerID, "banned-group-qr")
	if !errors.Is(err, ErrSessionGroupBanned) {
		t.Fatalf("expected ErrSessionGroupBanned from ResolveGroupQRCode, got %v", err)
	}

	_, err = JoinGroupByQRCode(joinerID, "banned-group-qr")
	if !errors.Is(err, ErrSessionGroupBanned) {
		t.Fatalf("expected ErrSessionGroupBanned from JoinGroupByQRCode, got %v", err)
	}
}

func TestResolveAndJoinGroupByQRCode(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		sessionID = "group-qr-session-join-1"
		ownerID   = int64(931001)
		memberID  = int64(931002)
		joinerID  = int64(931003)
	)

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, memberID)
	seedUser(t, testDB, joinerID)

	now := time.Now()
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		GroupName:      "Join Team",
		LastMsgSummary: "Join Team",
	}).Error; err != nil {
		t.Fatalf("create group session error: %v", err)
	}
	if err := testDB.DB.Create([]model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, JoinedAt: now, LastActiveAt: now},
	}).Error; err != nil {
		t.Fatalf("create group members error: %v", err)
	}

	qrInfo, err := GetOrCreateGroupQRCode(ownerID, sessionID)
	if err != nil {
		t.Fatalf("GetOrCreateGroupQRCode error: %v", err)
	}

	resolvedBeforeJoin, err := ResolveGroupQRCode(joinerID, qrInfo.Code)
	if err != nil {
		t.Fatalf("ResolveGroupQRCode before join error: %v", err)
	}
	if resolvedBeforeJoin.IsMember {
		t.Fatal("expected joiner is not member before join")
	}
	if resolvedBeforeJoin.MemberCount != 2 {
		t.Fatalf("expected member_count=2, got %d", resolvedBeforeJoin.MemberCount)
	}

	joinResp, err := JoinGroupByQRCode(joinerID, qrInfo.Code)
	if err != nil {
		t.Fatalf("JoinGroupByQRCode error: %v", err)
	}
	if !joinResp.Joined {
		t.Fatal("expected joined=true on first join")
	}
	if joinResp.SessionID != sessionID {
		t.Fatalf("unexpected session_id %s", joinResp.SessionID)
	}

	var memberCount int64
	if err := testDB.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, joinerID).
		Count(&memberCount).Error; err != nil {
		t.Fatalf("count joiner membership error: %v", err)
	}
	if memberCount != 1 {
		t.Fatalf("expected joiner membership count=1, got %d", memberCount)
	}

	joinResp2, err := JoinGroupByQRCode(joinerID, qrInfo.Code)
	if err != nil {
		t.Fatalf("JoinGroupByQRCode second call error: %v", err)
	}
	if joinResp2.Joined {
		t.Fatal("expected joined=false on duplicate join")
	}

	resolvedAfterJoin, err := ResolveGroupQRCode(joinerID, qrInfo.Code)
	if err != nil {
		t.Fatalf("ResolveGroupQRCode after join error: %v", err)
	}
	if !resolvedAfterJoin.IsMember {
		t.Fatal("expected joiner is member after join")
	}
	if resolvedAfterJoin.MemberCount != 3 {
		t.Fatalf("expected member_count=3, got %d", resolvedAfterJoin.MemberCount)
	}

	_, err = ResolveGroupQRCode(joinerID, "not-exists")
	if !IsGroupQRCodeNotFound(err) {
		t.Fatalf("expected group qr not found for resolve, got %v", err)
	}

	_, err = JoinGroupByQRCode(joinerID, "not-exists")
	if !IsGroupQRCodeNotFound(err) {
		t.Fatalf("expected group qr not found for join, got %v", err)
	}

	if err := testDB.DB.Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		Update("is_deleted", true).Error; err != nil {
		t.Fatalf("soft delete group session error: %v", err)
	}
	_, err = JoinGroupByQRCode(ownerID, qrInfo.Code)
	if !IsGroupQRCodeNotFound(err) {
		t.Fatalf("expected deleted group qr not found, got %v", err)
	}
}

func TestJoinGroupByQRCodeHonorsInviteRestrictions(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		ownerID     = int64(933001)
		joinerID    = int64(933002)
		memberID    = int64(933003)
		adminID     = int64(933004)
		anotherUser = int64(933005)
	)

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, joinerID)
	seedUser(t, testDB, memberID)
	seedUser(t, testDB, adminID)
	seedUser(t, testDB, anotherUser)

	now := time.Now()

	t.Run("rejects join when group invite is disabled", func(t *testing.T) {
		const sessionID = "group-qr-session-disabled-1"
		if err := testDB.DB.Create(&model.Session{
			SessionID:      sessionID,
			OwnerID:        ownerID,
			SessionType:    2,
			GroupName:      "Disabled Invite Team",
			LastMsgSummary: "Disabled Invite Team",
		}).Error; err != nil {
			t.Fatalf("create disabled session error: %v", err)
		}
		if err := testDB.DB.Model(&model.Session{}).
			Where("session_id = ?", sessionID).
			Update("allow_member_invite", false).Error; err != nil {
			t.Fatalf("disable group invite error: %v", err)
		}
		if err := testDB.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create disabled session owner error: %v", err)
		}

		info, err := GetOrCreateGroupQRCode(ownerID, sessionID)
		if err != nil {
			t.Fatalf("GetOrCreateGroupQRCode for disabled session error: %v", err)
		}

		_, err = JoinGroupByQRCode(joinerID, info.Code)
		if !errors.Is(err, ErrSessionMemberInviteDisabled) {
			t.Fatalf("expected ErrSessionMemberInviteDisabled, got %v", err)
		}
	})

	t.Run("rejects join when threshold reached", func(t *testing.T) {
		if err := systemsetting.SaveGroupSettings(systemsetting.GroupSettings{
			MemberInviteThreshold: 2,
		}, nil); err != nil {
			t.Fatalf("save group settings error: %v", err)
		}
		t.Cleanup(func() {
			systemsetting.InvalidateGroupSettingsCache()
			if err := systemsetting.SaveGroupSettings(systemsetting.DefaultGroupSettings(), nil); err != nil {
				t.Fatalf("restore group settings error: %v", err)
			}
		})

		const sessionID = "group-qr-session-threshold-1"
		if err := testDB.DB.Create(&model.Session{
			SessionID:         sessionID,
			OwnerID:           ownerID,
			SessionType:       2,
			GroupName:         "Threshold Team",
			AllowMemberInvite: true,
			LastMsgSummary:    "Threshold Team",
		}).Error; err != nil {
			t.Fatalf("create threshold session error: %v", err)
		}
		if err := testDB.DB.Create([]model.SessionMember{
			{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
			{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, JoinedAt: now, LastActiveAt: now},
			{SessionID: sessionID, MemberID: adminID, MemberType: 1, Role: 1, JoinedAt: now, LastActiveAt: now},
		}).Error; err != nil {
			t.Fatalf("create threshold session members error: %v", err)
		}

		info, err := GetOrCreateGroupQRCode(ownerID, sessionID)
		if err != nil {
			t.Fatalf("GetOrCreateGroupQRCode for threshold session error: %v", err)
		}

		_, err = JoinGroupByQRCode(anotherUser, info.Code)
		if !errors.Is(err, ErrSessionMemberInviteThresholdReached) {
			t.Fatalf("expected ErrSessionMemberInviteThresholdReached, got %v", err)
		}
	})
}

func TestGroupQRCodeExpiresAndCreatorRoleGuard(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		sessionID = "group-qr-session-expire-1"
		ownerID   = int64(932001)
		joinerID  = int64(932002)
		adminID   = int64(932003)
	)

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, joinerID)
	seedUser(t, testDB, adminID)

	now := time.Now()
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		GroupName:      "Expire Team",
		LastMsgSummary: "Expire Team",
	}).Error; err != nil {
		t.Fatalf("create group session error: %v", err)
	}
	if err := testDB.DB.Create([]model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: joinerID, MemberType: 1, Role: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: adminID, MemberType: 1, Role: 2, JoinedAt: now, LastActiveAt: now},
	}).Error; err != nil {
		t.Fatalf("create group members error: %v", err)
	}

	info, err := GetOrCreateGroupQRCode(ownerID, sessionID)
	if err != nil {
		t.Fatalf("GetOrCreateGroupQRCode error: %v", err)
	}

	if err := testDB.DB.Model(&model.GroupQRCode{}).
		Where("session_id = ?", sessionID).
		Update("expires_at", now.Add(-1*time.Minute)).Error; err != nil {
		t.Fatalf("expire group qr error: %v", err)
	}

	_, err = ResolveGroupQRCode(joinerID, info.Code)
	if !IsGroupQRCodeNotFound(err) {
		t.Fatalf("expected expired code not found on resolve, got %v", err)
	}
	_, err = JoinGroupByQRCode(joinerID, info.Code)
	if !IsGroupQRCodeNotFound(err) {
		t.Fatalf("expected expired code not found on join, got %v", err)
	}

	refreshedInfo, err := GetOrCreateGroupQRCode(ownerID, sessionID)
	if err != nil {
		t.Fatalf("GetOrCreateGroupQRCode refresh error: %v", err)
	}
	if refreshedInfo.Code == info.Code {
		t.Fatalf("expected refreshed code differs after expiration, got same=%s", refreshedInfo.Code)
	}

	if err := testDB.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, ownerID).
		Update("role", 1).Error; err != nil {
		t.Fatalf("downgrade creator role error: %v", err)
	}

	_, err = ResolveGroupQRCode(joinerID, refreshedInfo.Code)
	if !IsGroupQRCodeNotFound(err) {
		t.Fatalf("expected creator role downgraded code invalid, got %v", err)
	}
	_, err = JoinGroupByQRCode(joinerID, refreshedInfo.Code)
	if !IsGroupQRCodeNotFound(err) {
		t.Fatalf("expected creator role downgraded code invalid on join, got %v", err)
	}

	reissuedByAdmin, err := GetOrCreateGroupQRCode(adminID, sessionID)
	if err != nil {
		t.Fatalf("GetOrCreateGroupQRCode by admin after creator downgrade error: %v", err)
	}
	if reissuedByAdmin.Code == refreshedInfo.Code {
		t.Fatalf("expected reissued code differs after creator downgrade, got same=%s", reissuedByAdmin.Code)
	}

	if _, err := ResolveGroupQRCode(joinerID, reissuedByAdmin.Code); err != nil {
		t.Fatalf("expected new admin-issued code resolvable, got %v", err)
	}
}
