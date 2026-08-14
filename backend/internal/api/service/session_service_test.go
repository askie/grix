package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/google/uuid"
)

func init() {
	_ = snowflake.Init(1)
}

func setupSessionTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	prevRDB := store.RDB
	mockRDB := testutil.NewMockRedis()
	store.RDB = mockRDB
	systemsetting.InvalidateGroupSettingsCache()
	return testDB, func() {
		systemsetting.InvalidateGroupSettingsCache()
		if mockRDB != nil {
			_ = mockRDB.Close()
		}
		store.RDB = prevRDB
		testDB.Close()
	}
}

func createTestSessionWithMembers(t *testing.T, db *testutil.TestDB, userID int64, sessionID string) {
	t.Helper()
	now := time.Now()

	// Create session
	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    1,
		LastMsgSummary: "Hello",
	}
	if err := db.DB.Create(&session).Error; err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create session member
	member := model.SessionMember{
		SessionID:    sessionID,
		MemberID:     userID,
		MemberType:   1,
		Role:         3,
		UnreadCount:  5,
		LastActiveAt: now,
		JoinedAt:     now,
	}
	if err := db.DB.Create(&member).Error; err != nil {
		t.Fatalf("failed to create session member: %v", err)
	}
}

func seedUser(t *testing.T, db *testutil.TestDB, userID int64) {
	t.Helper()
	u := model.User{
		ID:           userID,
		Username:     fmt.Sprintf("user_%d", userID),
		Email:        fmt.Sprintf("user_%d@example.com", userID),
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     fmt.Sprintf("User%d", userID),
	}
	if err := db.DB.Create(&u).Error; err != nil {
		t.Fatalf("seed user %d error: %v", userID, err)
	}
}

// friendRelationIDCounter guarantees unique friends.id values regardless of
// clock resolution. Coarse Windows UnixNano() granularity plus symmetric
// userID+friendID sums (e.g. 100+200 == 200+100) otherwise collide.
var friendRelationIDCounter atomic.Int64

func seedFriendRelation(t *testing.T, db *testutil.TestDB, userID int64, friendID int64) {
	t.Helper()
	rel := model.Friend{
		ID:       time.Now().UnixNano() + friendRelationIDCounter.Add(1),
		UserID:   userID,
		FriendID: friendID,
	}
	if err := db.DB.Create(&rel).Error; err != nil {
		t.Fatalf("seed friend relation %d->%d error: %v", userID, friendID, err)
	}
}

func seedFriendRelationWithRemark(t *testing.T, db *testutil.TestDB, userID int64, friendID int64, remarkName string) {
	t.Helper()
	rel := model.Friend{
		ID:         time.Now().UnixNano() + friendRelationIDCounter.Add(1),
		UserID:     userID,
		FriendID:   friendID,
		RemarkName: remarkName,
	}
	if err := db.DB.Create(&rel).Error; err != nil {
		t.Fatalf("seed friend relation with remark %d->%d error: %v", userID, friendID, err)
	}
}

func seedUserBlock(t *testing.T, db *testutil.TestDB, userID int64, blockedUserID int64) {
	t.Helper()
	block := model.UserBlock{
		ID:            time.Now().UnixNano() + friendRelationIDCounter.Add(1),
		UserID:        userID,
		BlockedUserID: blockedUserID,
	}
	if err := db.DB.Create(&block).Error; err != nil {
		t.Fatalf("seed user block %d->%d error: %v", userID, blockedUserID, err)
	}
}

func seedAgent(t *testing.T, db *testutil.TestDB, agentID int64, ownerID int64, status int16) {
	t.Helper()
	agent := model.Agent{
		ID:        agentID,
		OwnerID:   ownerID,
		AgentName: fmt.Sprintf("agent_%d", agentID),
		Status:    status,
	}
	if err := db.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed agent %d error: %v", agentID, err)
	}
}

func TestSessionCreateForAgentDispatch(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		ownerID = int64(52001)
		agentID = int64(52002)
	)
	seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)

	t.Run("每次新建独立会话不复用", func(t *testing.T) {
		r1, err := SessionCreateForAgentDispatch(ownerID, agentID, "任务一")
		if err != nil {
			t.Fatalf("first dispatch session: %v", err)
		}
		r2, err := SessionCreateForAgentDispatch(ownerID, agentID, "任务一")
		if err != nil {
			t.Fatalf("second dispatch session: %v", err)
		}
		if !r1.IsNew || !r2.IsNew {
			t.Fatalf("both should be new: r1=%v r2=%v", r1.IsNew, r2.IsNew)
		}
		if r1.SessionID == r2.SessionID {
			t.Fatalf("expected distinct sessions, both got %s", r1.SessionID)
		}
	})

	t.Run("title 写入会话标题与 owner 成员自定义标题", func(t *testing.T) {
		r, err := SessionCreateForAgentDispatch(ownerID, agentID, "实现登录功能")
		if err != nil {
			t.Fatalf("dispatch session: %v", err)
		}
		var s model.Session
		if err := store.DB.Select("last_msg_summary").
			Where("session_id = ?", r.SessionID).First(&s).Error; err != nil {
			t.Fatalf("load session: %v", err)
		}
		if s.LastMsgSummary != "实现登录功能" {
			t.Fatalf("session title=%q want 实现登录功能", s.LastMsgSummary)
		}
		var m model.SessionMember
		if err := store.DB.Select("custom_title").
			Where("session_id = ? AND member_id = ? AND member_type = 1", r.SessionID, ownerID).
			First(&m).Error; err != nil {
			t.Fatalf("load owner member: %v", err)
		}
		if m.CustomTitle != "实现登录功能" {
			t.Fatalf("owner custom_title=%q want 实现登录功能", m.CustomTitle)
		}
	})

	t.Run("空 title 允许且标题为空", func(t *testing.T) {
		r, err := SessionCreateForAgentDispatch(ownerID, agentID, "")
		if err != nil {
			t.Fatalf("dispatch session: %v", err)
		}
		var s model.Session
		if err := store.DB.Select("last_msg_summary").
			Where("session_id = ?", r.SessionID).First(&s).Error; err != nil {
			t.Fatalf("load session: %v", err)
		}
		if s.LastMsgSummary != "" {
			t.Fatalf("session title=%q want empty", s.LastMsgSummary)
		}
	})

	t.Run("拒绝非本人 agent", func(t *testing.T) {
		const foreignAgent = int64(52090)
		seedAgent(t, testDB, foreignAgent, ownerID+999, model.AgentStatusActive)
		if _, err := SessionCreateForAgentDispatch(ownerID, foreignAgent, "x"); err == nil {
			t.Fatalf("expected error dispatching to agent of another owner")
		}
	})
}

func TestSessionList(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(1001)

	t.Run("empty list", func(t *testing.T) {
		resp, err := SessionList(userID, 10, 0)
		if err != nil {
			t.Fatalf("SessionList() error = %v", err)
		}

		if resp.HasMore {
			t.Error("expected no hasMore for empty list")
		}
		if len(resp.List) != 0 {
			t.Errorf("expected empty list, got %d items", len(resp.List))
		}
	})

	t.Run("with sessions", func(t *testing.T) {
		createTestSessionWithMembers(t, testDB, userID, "session-1")
		createTestSessionWithMembers(t, testDB, userID, "session-2")

		resp, err := SessionList(userID, 10, 0)
		if err != nil {
			t.Fatalf("SessionList() error = %v", err)
		}

		if len(resp.List) != 2 {
			t.Errorf("expected 2 sessions, got %d", len(resp.List))
		}
	})

	t.Run("pagination", func(t *testing.T) {
		// Clear existing data
		testDB.Cleanup()

		// Create 15 sessions
		for i := 0; i < 15; i++ {
			createTestSessionWithMembers(t, testDB, userID, "session-p"+string(rune('0'+i)))
		}

		// Get first page
		resp, err := SessionList(userID, 10, 0)
		if err != nil {
			t.Fatalf("SessionList() error = %v", err)
		}

		if !resp.HasMore {
			t.Error("expected hasMore to be true")
		}
		if len(resp.List) != 10 {
			t.Errorf("expected 10 items, got %d", len(resp.List))
		}

		// Get second page
		resp2, err := SessionList(userID, 10, 10)
		if err != nil {
			t.Fatalf("SessionList() page 2 error = %v", err)
		}

		if resp2.HasMore {
			t.Error("expected hasMore to be false on last page")
		}
		if len(resp2.List) != 5 {
			t.Errorf("expected 5 items on page 2, got %d", len(resp2.List))
		}
	})

	t.Run("pinned sessions are sorted to top", func(t *testing.T) {
		testDB.Cleanup()
		now := time.Now()

		sessions := []model.Session{
			{
				SessionID:      "session-pinned",
				OwnerID:        userID,
				SessionType:    1,
				LastMsgSummary: "pinned",
				UpdatedAt:      now.Add(-2 * time.Minute),
			},
			{
				SessionID:      "session-normal",
				OwnerID:        userID,
				SessionType:    1,
				LastMsgSummary: "normal",
				UpdatedAt:      now,
			},
		}
		if err := testDB.DB.Create(&sessions).Error; err != nil {
			t.Fatalf("create sessions error: %v", err)
		}

		pinnedAt := now.Add(-1 * time.Minute)
		members := []model.SessionMember{
			{
				SessionID:    "session-pinned",
				MemberID:     userID,
				MemberType:   1,
				Role:         3,
				IsPinned:     true,
				PinnedAt:     &pinnedAt,
				LastActiveAt: now.Add(-2 * time.Minute),
				JoinedAt:     now.Add(-2 * time.Minute),
			},
			{
				SessionID:    "session-normal",
				MemberID:     userID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&members).Error; err != nil {
			t.Fatalf("create members error: %v", err)
		}

		resp, err := SessionList(userID, 10, 0)
		if err != nil {
			t.Fatalf("SessionList() error = %v", err)
		}
		if len(resp.List) != 2 {
			t.Fatalf("expected 2 sessions, got %d", len(resp.List))
		}
		if resp.List[0].SessionID != "session-pinned" {
			t.Fatalf("expected pinned session first, got %s", resp.List[0].SessionID)
		}
		if !resp.List[0].IsPinned {
			t.Fatalf("expected first session is_pinned=true")
		}
		if resp.List[0].PinnedAt <= 0 {
			t.Fatalf("expected first session pinned_at > 0")
		}
	})

	t.Run("ignores agent membership rows for human user list", func(t *testing.T) {
		testDB.Cleanup()
		createTestSessionWithMembers(t, testDB, userID, "session-human")

		agentSession := model.Session{
			SessionID:      "session-agent",
			OwnerID:        userID,
			SessionType:    2,
			LastMsgSummary: "agent only",
		}
		if err := testDB.DB.Create(&agentSession).Error; err != nil {
			t.Fatalf("create agent session error: %v", err)
		}
		now := time.Now()
		agentMember := model.SessionMember{
			SessionID:    agentSession.SessionID,
			MemberID:     userID,
			MemberType:   2,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		}
		if err := testDB.DB.Create(&agentMember).Error; err != nil {
			t.Fatalf("create agent member error: %v", err)
		}

		resp, err := SessionList(userID, 10, 0)
		if err != nil {
			t.Fatalf("SessionList() error = %v", err)
		}
		if len(resp.List) != 1 {
			t.Fatalf("expected 1 human session, got %d", len(resp.List))
		}
		if resp.List[0].SessionID != "session-human" {
			t.Fatalf("expected session-human, got %s", resp.List[0].SessionID)
		}
	})

	t.Run("returns server-side title for private and group sessions", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19001)
		peerID := int64(19002)
		now := time.Now()

		seedUser(t, testDB, ownerID)
		seedUser(t, testDB, peerID)
		seedFriendRelationWithRemark(t, testDB, ownerID, peerID, "备注用户")

		privateSession := model.Session{
			SessionID:      "session-private-title",
			OwnerID:        ownerID,
			SessionType:    1,
			LastMsgSummary: "private msg",
		}
		if err := testDB.DB.Create(&privateSession).Error; err != nil {
			t.Fatalf("create private session error: %v", err)
		}
		privateMembers := []model.SessionMember{
			{
				SessionID:    privateSession.SessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    privateSession.SessionID,
				MemberID:     peerID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&privateMembers).Error; err != nil {
			t.Fatalf("create private members error: %v", err)
		}

		groupSession := model.Session{
			SessionID:      "session-group-title",
			OwnerID:        ownerID,
			SessionType:    2,
			GroupName:      "Task Room A",
			LastMsgSummary: "group msg",
		}
		if err := testDB.DB.Create(&groupSession).Error; err != nil {
			t.Fatalf("create group session error: %v", err)
		}
		groupMembers := []model.SessionMember{
			{
				SessionID:    groupSession.SessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    groupSession.SessionID,
				MemberID:     peerID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&groupMembers).Error; err != nil {
			t.Fatalf("create group members error: %v", err)
		}

		resp, err := SessionList(ownerID, 20, 0)
		if err != nil {
			t.Fatalf("SessionList() error = %v", err)
		}
		got := map[string]SessionItem{}
		for _, item := range resp.List {
			got[item.SessionID] = item
		}

		privateItem, ok := got[privateSession.SessionID]
		if !ok {
			t.Fatalf("missing private session in list")
		}
		if privateItem.Title != "备注用户" {
			t.Fatalf("private title should prefer friend remark, got %q", privateItem.Title)
		}
		if privateItem.Peer == nil {
			t.Fatalf("private peer should be present")
		}
		if privateItem.Peer.Nickname != "备注用户" {
			t.Fatalf("private peer nickname should prefer friend remark, got %q", privateItem.Peer.Nickname)
		}
		if privateItem.Peer.Username != "user_19002" {
			t.Fatalf("private peer username should be user_19002, got %q", privateItem.Peer.Username)
		}

		groupItem, ok := got[groupSession.SessionID]
		if !ok {
			t.Fatalf("missing group session in list")
		}
		if groupItem.Title != "Task Room A" {
			t.Fatalf("group title should be group name, got %q", groupItem.Title)
		}
	})

	t.Run("uses first message snippet when custom title is empty", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19101)
		peerID := int64(19102)
		now := time.Now()

		seedUser(t, testDB, ownerID)
		seedUser(t, testDB, peerID)

		session := model.Session{
			SessionID:      "session-first-message-title",
			OwnerID:        ownerID,
			SessionType:    1,
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create session error: %v", err)
		}
		members := []model.SessionMember{
			{
				SessionID:    session.SessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    session.SessionID,
				MemberID:     peerID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&members).Error; err != nil {
			t.Fatalf("create members error: %v", err)
		}

		firstContent := "  This is   the first message used\nas session title  "
		messages := []model.Message{
			{
				MsgID:      1001,
				SessionID:  session.SessionID,
				SenderID:   ownerID,
				SenderType: 1,
				MsgType:    1,
				Content:    firstContent,
			},
			{
				MsgID:      1002,
				SessionID:  session.SessionID,
				SenderID:   peerID,
				SenderType: 1,
				MsgType:    1,
				Content:    "second message",
			},
		}
		if err := testDB.DB.Create(&messages).Error; err != nil {
			t.Fatalf("create messages error: %v", err)
		}

		resp, err := SessionList(ownerID, 20, 0)
		if err != nil {
			t.Fatalf("SessionList() error = %v", err)
		}
		if len(resp.List) != 1 {
			t.Fatalf("expected 1 session, got %d", len(resp.List))
		}

		// 私聊 custom_title 为空时回退到 peer 显示名
		expected := fmt.Sprintf("User%d", peerID)
		if resp.List[0].Title != expected {
			t.Fatalf("expected peer nickname %q, got %q", expected, resp.List[0].Title)
		}
	})

	t.Run("group title keeps group name instead of first message snippet", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19111)
		memberID := int64(19112)
		now := time.Now()

		seedUser(t, testDB, ownerID)
		seedUser(t, testDB, memberID)

		session := model.Session{
			SessionID:      "session-group-title-priority",
			OwnerID:        ownerID,
			SessionType:    2,
			GroupName:      "项目讨论组",
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create session error: %v", err)
		}
		members := []model.SessionMember{
			{
				SessionID:    session.SessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    session.SessionID,
				MemberID:     memberID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&members).Error; err != nil {
			t.Fatalf("create members error: %v", err)
		}

		firstContent := "这是一条很新的群消息，不应该变成群标题"
		message := model.Message{
			MsgID:      1101,
			SessionID:  session.SessionID,
			SenderID:   ownerID,
			SenderType: 1,
			MsgType:    1,
			Content:    firstContent,
		}
		if err := testDB.DB.Create(&message).Error; err != nil {
			t.Fatalf("create message error: %v", err)
		}

		resp, err := SessionList(ownerID, 20, 0)
		if err != nil {
			t.Fatalf("SessionList() error = %v", err)
		}
		if len(resp.List) != 1 {
			t.Fatalf("expected 1 session, got %d", len(resp.List))
		}
		if resp.List[0].Title != "项目讨论组" {
			t.Fatalf("expected group name title, got %q", resp.List[0].Title)
		}
	})

	t.Run("custom title overrides first message snippet", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19201)
		peerID := int64(19202)
		now := time.Now()

		seedUser(t, testDB, ownerID)
		seedUser(t, testDB, peerID)

		session := model.Session{
			SessionID:      "session-custom-title-override",
			OwnerID:        ownerID,
			SessionType:    1,
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create session error: %v", err)
		}
		members := []model.SessionMember{
			{
				SessionID:    session.SessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				CustomTitle:  "My Archived Topic",
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    session.SessionID,
				MemberID:     peerID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&members).Error; err != nil {
			t.Fatalf("create members error: %v", err)
		}

		msg := model.Message{
			MsgID:      2001,
			SessionID:  session.SessionID,
			SenderID:   ownerID,
			SenderType: 1,
			MsgType:    1,
			Content:    "first message fallback",
		}
		if err := testDB.DB.Create(&msg).Error; err != nil {
			t.Fatalf("create message error: %v", err)
		}

		resp, err := SessionList(ownerID, 20, 0)
		if err != nil {
			t.Fatalf("SessionList() error = %v", err)
		}
		if len(resp.List) != 1 {
			t.Fatalf("expected 1 session, got %d", len(resp.List))
		}
		if resp.List[0].Title != "My Archived Topic" {
			t.Fatalf("expected custom title, got %q", resp.List[0].Title)
		}
	})
}

func TestSessionListFiltersBannedGroup(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const userID = int64(19001)
	now := time.Now()

	active := model.Session{
		SessionID:         "group-active-list",
		OwnerID:           userID,
		SessionType:       model.SessionTypeGroup,
		GroupName:         "Active Group",
		ModerationStatus:  model.SessionModerationStatusActive,
		AllowMemberInvite: true,
	}
	banned := model.Session{
		SessionID:         "group-banned-list",
		OwnerID:           userID,
		SessionType:       model.SessionTypeGroup,
		GroupName:         "Banned Group",
		ModerationStatus:  model.SessionModerationStatusBanned,
		AllowMemberInvite: true,
	}
	if err := testDB.DB.Create(&[]model.Session{active, banned}).Error; err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: active.SessionID, MemberID: userID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
		{SessionID: banned.SessionID, MemberID: userID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
	}).Error; err != nil {
		t.Fatalf("create session members: %v", err)
	}

	resp, err := SessionList(userID, 10, 0)
	if err != nil {
		t.Fatalf("SessionList() error = %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected 1 visible session, got %d", len(resp.List))
	}
	if resp.List[0].SessionID != active.SessionID {
		t.Fatalf("expected active session %s, got %s", active.SessionID, resp.List[0].SessionID)
	}
}

func TestSessionConversationsFoldPrivateThreads(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const ownerID = int64(19101)
	const peerID = int64(19102)
	now := time.Now()
	pinnedAt := now.Add(time.Minute)

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, peerID)
	if err := testDB.DB.Create(&model.Friend{
		ID:       1910102,
		UserID:   ownerID,
		FriendID: peerID,
		IsPinned: true,
		PinnedAt: &pinnedAt,
	}).Error; err != nil {
		t.Fatalf("create friend pin: %v", err)
	}
	if err := testDB.DB.Create(&model.UserPeerPin{
		ID:         19101020,
		UserID:     ownerID,
		PeerUserID: peerID,
		IsPinned:   true,
		PinnedAt:   &pinnedAt,
	}).Error; err != nil {
		t.Fatalf("create user peer pin: %v", err)
	}

	sessions := []model.Session{
		{SessionID: "conv-private-old", OwnerID: ownerID, SessionType: model.SessionTypeDirect, LastMsgSummary: "old", UpdatedAt: now.Add(-2 * time.Hour)},
		{SessionID: "conv-private-new", OwnerID: ownerID, SessionType: model.SessionTypeDirect, LastMsgSummary: "new", UpdatedAt: now},
		{SessionID: "conv-group", OwnerID: ownerID, SessionType: model.SessionTypeGroup, GroupName: "Team", ModerationStatus: model.SessionModerationStatusActive, LastMsgSummary: "group", UpdatedAt: now.Add(-time.Hour)},
	}
	if err := testDB.DB.Create(&sessions).Error; err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: "conv-private-old", MemberID: ownerID, MemberType: 1, Role: 3, UnreadCount: 2, LastActiveAt: now.Add(-2 * time.Hour), JoinedAt: now.Add(-2 * time.Hour)},
		{SessionID: "conv-private-old", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now.Add(-2 * time.Hour), JoinedAt: now.Add(-2 * time.Hour)},
		{SessionID: "conv-private-new", MemberID: ownerID, MemberType: 1, Role: 3, UnreadCount: 3, IsMuted: true, LastActiveAt: now, JoinedAt: now},
		{SessionID: "conv-private-new", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: "conv-group", MemberID: ownerID, MemberType: 1, Role: 3, UnreadCount: 7, LastActiveAt: now.Add(-time.Hour), JoinedAt: now.Add(-time.Hour)},
		{SessionID: "conv-group", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now.Add(-time.Hour), JoinedAt: now.Add(-time.Hour)},
	}).Error; err != nil {
		t.Fatalf("create members: %v", err)
	}

	resp, err := SessionConversations(ownerID, 20, "")
	if err != nil {
		t.Fatalf("SessionConversations() error = %v", err)
	}
	if len(resp.List) != 2 {
		t.Fatalf("expected 2 folded conversations, got %#v", resp.List)
	}
	private := resp.List[0]
	if private.GroupKey != "private:1:19102" {
		t.Fatalf("expected private group first, got %#v", private)
	}
	if private.LatestSessionID != "conv-private-new" {
		t.Fatalf("expected latest private session, got %s", private.LatestSessionID)
	}
	if private.ThreadCount != 2 || !private.HasMoreThreads {
		t.Fatalf("expected folded thread_count=2, got count=%d has_more=%v", private.ThreadCount, private.HasMoreThreads)
	}
	if private.Unread != 5 {
		t.Fatalf("expected aggregate unread=5, got %d", private.Unread)
	}
	if private.BadgeUnread != 2 {
		t.Fatalf("expected muted thread excluded from badge unread, got %d", private.BadgeUnread)
	}
	if !private.IsPinned || private.PinnedAt != pinnedAt.Unix() {
		t.Fatalf("expected friend pin to drive private conversation pin, got pinned=%v pinned_at=%d", private.IsPinned, private.PinnedAt)
	}
}

func TestFriendSetPinnedAllowsNonFriendHumanPeer(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const ownerID = int64(19111)
	const peerID = int64(19112)

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, peerID)

	resp, err := FriendSetPinned(ownerID, peerID, true)
	if err != nil {
		t.Fatalf("FriendSetPinned() error = %v", err)
	}
	if !resp.IsPinned || resp.PinnedAt <= 0 {
		t.Fatalf("expected pinned response, got %#v", resp)
	}

	var relCount int64
	if err := testDB.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", ownerID, peerID).
		Count(&relCount).Error; err != nil {
		t.Fatalf("query friendship error: %v", err)
	}
	if relCount != 0 {
		t.Fatalf("pinning a non-friend must not create friendship, got %d", relCount)
	}

	var pin model.UserPeerPin
	if err := testDB.DB.
		Where("user_id = ? AND peer_user_id = ?", ownerID, peerID).
		First(&pin).Error; err != nil {
		t.Fatalf("query user peer pin error: %v", err)
	}
	if !pin.IsPinned || pin.PinnedAt == nil {
		t.Fatalf("expected persisted user peer pin, got %#v", pin)
	}
}

func TestFriendSetPinnedAllowsAgentPeer(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const ownerID = int64(19113)
	const agentID = int64(19114)

	seedUser(t, testDB, ownerID)
	seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)

	resp, err := FriendSetPinned(ownerID, agentID, true)
	if err != nil {
		t.Fatalf("FriendSetPinned() error = %v", err)
	}
	if !resp.IsPinned || resp.PinnedAt <= 0 {
		t.Fatalf("expected pinned response, got %#v", resp)
	}

	var pin model.UserPeerPin
	if err := testDB.DB.
		Where("user_id = ? AND peer_user_id = ?", ownerID, agentID).
		First(&pin).Error; err != nil {
		t.Fatalf("query user peer pin error: %v", err)
	}
	if !pin.IsPinned || pin.PinnedAt == nil {
		t.Fatalf("expected persisted agent peer pin, got %#v", pin)
	}
}

func TestSessionConversationsUsesUserPeerPinForNonFriendPeer(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const ownerID = int64(19121)
	const peerID = int64(19122)
	now := time.Now()

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, peerID)
	if _, err := FriendSetPinned(ownerID, peerID, true); err != nil {
		t.Fatalf("FriendSetPinned() error = %v", err)
	}

	if err := testDB.DB.Create(&model.Session{
		SessionID:      "conv-nonfriend-peer",
		OwnerID:        ownerID,
		SessionType:    model.SessionTypeDirect,
		LastMsgSummary: "visitor",
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: "conv-nonfriend-peer", MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: "conv-nonfriend-peer", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
	}).Error; err != nil {
		t.Fatalf("create members: %v", err)
	}

	resp, err := SessionConversations(ownerID, 20, "")
	if err != nil {
		t.Fatalf("SessionConversations() error = %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected 1 conversation, got %#v", resp.List)
	}
	item := resp.List[0]
	if item.GroupKey != "private:1:19122" {
		t.Fatalf("expected private peer group, got %#v", item)
	}
	if !item.IsPinned || item.PinnedAt <= 0 {
		t.Fatalf("expected user peer pin to drive conversation pin, got pinned=%v pinned_at=%d", item.IsPinned, item.PinnedAt)
	}
}

func TestSessionConversationThreadsReturnsFoldedPrivateSessions(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const ownerID = int64(19201)
	const peerID = int64(19202)
	now := time.Now()

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, peerID)
	if err := testDB.DB.Create(&[]model.Session{
		{SessionID: "thread-old", OwnerID: ownerID, SessionType: model.SessionTypeDirect, LastMsgSummary: "old", UpdatedAt: now.Add(-time.Hour)},
		{SessionID: "thread-new", OwnerID: ownerID, SessionType: model.SessionTypeDirect, LastMsgSummary: "new", UpdatedAt: now},
		{SessionID: "thread-other", OwnerID: ownerID, SessionType: model.SessionTypeGroup, GroupName: "Other", ModerationStatus: model.SessionModerationStatusActive, LastMsgSummary: "other", UpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: "thread-old", MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now.Add(-time.Hour), JoinedAt: now.Add(-time.Hour)},
		{SessionID: "thread-old", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now.Add(-time.Hour), JoinedAt: now.Add(-time.Hour)},
		{SessionID: "thread-new", MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: "thread-new", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: "thread-other", MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: "thread-other", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
	}).Error; err != nil {
		t.Fatalf("create members: %v", err)
	}

	resp, err := SessionConversationThreads(ownerID, "private:1:19202", 1, "")
	if err != nil {
		t.Fatalf("SessionConversationThreads() error = %v", err)
	}
	if len(resp.List) != 1 || resp.List[0].SessionID != "thread-new" {
		t.Fatalf("expected first page to contain latest private thread, got %#v", resp.List)
	}
	if !resp.HasMore || resp.NextCursor == "" {
		t.Fatalf("expected paged thread response, got has_more=%v cursor=%q", resp.HasMore, resp.NextCursor)
	}

	next, err := SessionConversationThreads(ownerID, "private:1:19202", 1, resp.NextCursor)
	if err != nil {
		t.Fatalf("SessionConversationThreads(next) error = %v", err)
	}
	if len(next.List) != 1 || next.List[0].SessionID != "thread-old" {
		t.Fatalf("expected second page to contain older private thread, got %#v", next.List)
	}
}

func TestSessionConversationsKeepsGroupAsSingleItemWithMultipleMembers(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const ownerID = int64(19211)
	now := time.Now()

	seedUser(t, testDB, ownerID)
	for _, uid := range []int64{19212, 19213, 19214} {
		seedUser(t, testDB, uid)
	}
	if err := testDB.DB.Create(&model.Session{
		SessionID:        "conv-group-many-members",
		OwnerID:          ownerID,
		SessionType:      model.SessionTypeGroup,
		GroupName:        "Many Members",
		ModerationStatus: model.SessionModerationStatusActive,
		LastMsgSummary:   "group latest",
		UpdatedAt:        now,
	}).Error; err != nil {
		t.Fatalf("create group session: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: "conv-group-many-members", MemberID: ownerID, MemberType: 1, Role: 3, UnreadCount: 4, LastActiveAt: now, JoinedAt: now},
		{SessionID: "conv-group-many-members", MemberID: 19212, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: "conv-group-many-members", MemberID: 19213, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: "conv-group-many-members", MemberID: 19214, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
	}).Error; err != nil {
		t.Fatalf("create group members: %v", err)
	}

	resp, err := SessionConversations(ownerID, 20, "")
	if err != nil {
		t.Fatalf("SessionConversations() error = %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected 1 group conversation, got %#v", resp.List)
	}
	item := resp.List[0]
	if item.GroupKey != "session:conv-group-many-members" {
		t.Fatalf("expected group session key, got %q", item.GroupKey)
	}
	if item.ThreadCount != 1 || item.HasMoreThreads {
		t.Fatalf("group should not be inflated by members, count=%d has_more=%v", item.ThreadCount, item.HasMoreThreads)
	}
	if item.Unread != 4 || item.BadgeUnread != 4 {
		t.Fatalf("expected unread=4 badge=4, got unread=%d badge=%d", item.Unread, item.BadgeUnread)
	}
}

func TestSessionConversationsPaginatesByFoldedItems(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const ownerID = int64(19221)
	now := time.Now()
	seedUser(t, testDB, ownerID)
	for _, uid := range []int64{19222, 19223} {
		seedUser(t, testDB, uid)
	}

	if err := testDB.DB.Create(&[]model.Session{
		{SessionID: "page-peer-a-old", OwnerID: ownerID, SessionType: model.SessionTypeDirect, LastMsgSummary: "a old", UpdatedAt: now.Add(-3 * time.Hour)},
		{SessionID: "page-peer-a-new", OwnerID: ownerID, SessionType: model.SessionTypeDirect, LastMsgSummary: "a new", UpdatedAt: now.Add(-time.Hour)},
		{SessionID: "page-peer-b", OwnerID: ownerID, SessionType: model.SessionTypeDirect, LastMsgSummary: "b", UpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: "page-peer-a-old", MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now.Add(-3 * time.Hour), JoinedAt: now.Add(-3 * time.Hour)},
		{SessionID: "page-peer-a-old", MemberID: 19222, MemberType: 1, Role: 1, LastActiveAt: now.Add(-3 * time.Hour), JoinedAt: now.Add(-3 * time.Hour)},
		{SessionID: "page-peer-a-new", MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now.Add(-time.Hour), JoinedAt: now.Add(-time.Hour)},
		{SessionID: "page-peer-a-new", MemberID: 19222, MemberType: 1, Role: 1, LastActiveAt: now.Add(-time.Hour), JoinedAt: now.Add(-time.Hour)},
		{SessionID: "page-peer-b", MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: "page-peer-b", MemberID: 19223, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
	}).Error; err != nil {
		t.Fatalf("create members: %v", err)
	}

	first, err := SessionConversations(ownerID, 1, "")
	if err != nil {
		t.Fatalf("SessionConversations(first) error = %v", err)
	}
	if len(first.List) != 1 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("expected first folded page with cursor, got %#v", first)
	}
	if first.List[0].GroupKey != "private:1:19223" {
		t.Fatalf("expected newest peer on first page, got %#v", first.List)
	}

	second, err := SessionConversations(ownerID, 1, first.NextCursor)
	if err != nil {
		t.Fatalf("SessionConversations(second) error = %v", err)
	}
	if len(second.List) != 1 || second.HasMore {
		t.Fatalf("expected final folded page, got %#v", second)
	}
	if second.List[0].GroupKey != "private:1:19222" || second.List[0].ThreadCount != 2 {
		t.Fatalf("expected folded peer A on second page, got %#v", second.List)
	}
}

func TestSessionConversationsLatestByActivityIgnoresPin(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const ownerID = int64(19401)
	const peerID = int64(19402)
	now := time.Now()
	pinnedAt := now.Add(time.Minute)

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, peerID)
	if err := testDB.DB.Create(&model.Friend{
		ID:       1940102,
		UserID:   ownerID,
		FriendID: peerID,
		IsPinned: true,
		PinnedAt: &pinnedAt,
	}).Error; err != nil {
		t.Fatalf("create friend pin: %v", err)
	}
	if err := testDB.DB.Create(&model.UserPeerPin{
		ID:         19401020,
		UserID:     ownerID,
		PeerUserID: peerID,
		IsPinned:   true,
		PinnedAt:   &pinnedAt,
	}).Error; err != nil {
		t.Fatalf("create user peer pin: %v", err)
	}

	// Two sessions with the same peer. The older one is session-pinned;
	// the newer one is not session-pinned. The conversation group summary
	// must reflect the newer (more active) session regardless of pinning.
	if err := testDB.DB.Create(&[]model.Session{
		{SessionID: "pin-old-pinned", OwnerID: ownerID, SessionType: model.SessionTypeDirect, LastMsgSummary: "old pinned msg", UpdatedAt: now.Add(-2 * time.Hour)},
		{SessionID: "pin-new-unpinned", OwnerID: ownerID, SessionType: model.SessionTypeDirect, LastMsgSummary: "new unpinned msg", UpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: "pin-old-pinned", MemberID: ownerID, MemberType: 1, Role: 3, IsPinned: true, PinnedAt: &pinnedAt, UnreadCount: 5, LastActiveAt: now.Add(-2 * time.Hour), JoinedAt: now.Add(-2 * time.Hour)},
		{SessionID: "pin-old-pinned", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now.Add(-2 * time.Hour), JoinedAt: now.Add(-2 * time.Hour)},
		{SessionID: "pin-new-unpinned", MemberID: ownerID, MemberType: 1, Role: 3, UnreadCount: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: "pin-new-unpinned", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
	}).Error; err != nil {
		t.Fatalf("create members: %v", err)
	}

	resp, err := SessionConversations(ownerID, 20, "")
	if err != nil {
		t.Fatalf("SessionConversations() error = %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected 1 conversation group, got %d", len(resp.List))
	}
	item := resp.List[0]
	// The latest session must be the newer one, not the pinned one.
	if item.LatestSessionID != "pin-new-unpinned" {
		t.Fatalf("expected latest=pin-new-unpinned, got %s", item.LatestSessionID)
	}
	// LastMsg comes from the messages table, so it is empty in this
	// test which does not seed messages. Verify LatestSessionID only.
	// The group is still pinned because of the friend-level pin.
	if !item.IsPinned {
		t.Fatalf("expected conversation group to remain pinned")
	}
}

func TestLoadConversationCandidatesExcludesHistoryResetSessions(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const ownerID = int64(19350)
	const peerID = int64(19351)
	now := time.Now()

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, peerID)

	if err := testDB.DB.Create(&[]model.Session{
		{SessionID: "shr-active", OwnerID: ownerID, SessionType: model.SessionTypeDirect, LastMsgSummary: "active", UpdatedAt: now},
		{SessionID: "shr-deleted", OwnerID: ownerID, SessionType: model.SessionTypeDirect, LastMsgSummary: "deleted", UpdatedAt: now.Add(-time.Hour)},
	}).Error; err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: "shr-active", MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: "shr-active", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: "shr-deleted", MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now.Add(-time.Hour), JoinedAt: now.Add(-time.Hour)},
		{SessionID: "shr-deleted", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now.Add(-time.Hour), JoinedAt: now.Add(-time.Hour)},
	}).Error; err != nil {
		t.Fatalf("create members: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionHistoryReset{
		SessionID:     "shr-deleted",
		UserID:        ownerID,
		DeletedBefore: now,
	}).Error; err != nil {
		t.Fatalf("create history reset: %v", err)
	}

	// SessionConversations should fold the two private threads into one item,
	// excluding the one with a history reset.
	resp, err := SessionConversations(ownerID, 20, "")
	if err != nil {
		t.Fatalf("SessionConversations() error = %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected 1 conversation (history-reset session excluded), got %d items", len(resp.List))
	}
	if resp.List[0].ThreadCount != 1 {
		t.Fatalf("expected thread_count=1, got %d", resp.List[0].ThreadCount)
	}

	// SessionConversationThreads should also exclude the reset session.
	threads, err := SessionConversationThreads(ownerID, "private:1:19351", 20, "")
	if err != nil {
		t.Fatalf("SessionConversationThreads() error = %v", err)
	}
	if len(threads.List) != 1 || threads.List[0].SessionID != "shr-active" {
		t.Fatalf("expected only the active thread, got %#v", threads.List)
	}
}

// TestSessionConversationsReappearsAfterHistoryResetWithNewMessage 验证删除会话后
// 又收到新消息（cutoff 之后存在可见消息）时，会话应重新出现在列表，
// 与底部角标(pull_sync)口径一致。
func TestSessionConversationsReappearsAfterHistoryResetWithNewMessage(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const ownerID = int64(19360)
	const peerID = int64(19361)
	now := time.Now()

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, peerID)

	if err := testDB.DB.Create(&model.Session{
		SessionID: "shr-reappear", OwnerID: ownerID, SessionType: model.SessionTypeDirect,
		LastMsgSummary: "new after delete", UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: "shr-reappear", MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now.Add(-time.Hour)},
		{SessionID: "shr-reappear", MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now.Add(-time.Hour)},
	}).Error; err != nil {
		t.Fatalf("create members: %v", err)
	}
	// 删除发生在一小时前
	if err := testDB.DB.Create(&model.SessionHistoryReset{
		SessionID: "shr-reappear", UserID: ownerID, DeletedBefore: now.Add(-time.Hour),
	}).Error; err != nil {
		t.Fatalf("create history reset: %v", err)
	}
	// 删除点之后对方又发来一条可见消息
	if err := testDB.DB.Create(&model.Message{
		MsgID: 8401, SessionID: "shr-reappear", SenderID: peerID, SenderType: 1,
		MsgType: 1, Content: "are you there?", CreatedAt: now.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	resp, err := SessionConversations(ownerID, 20, "")
	if err != nil {
		t.Fatalf("SessionConversations() error = %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected reset session to reappear (1 item), got %d", len(resp.List))
	}
	if resp.List[0].LatestSessionID != "shr-reappear" {
		t.Fatalf("expected shr-reappear, got %s", resp.List[0].LatestSessionID)
	}
}

func TestSessionSearch(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	t.Run("matches displayed session titles", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19301)
		remarkPeerID := int64(19302)
		firstMessagePeerID := int64(19303)
		now := time.Now()

		seedUser(t, testDB, ownerID)
		seedUser(t, testDB, remarkPeerID)
		seedUser(t, testDB, firstMessagePeerID)
		seedFriendRelationWithRemark(t, testDB, ownerID, remarkPeerID, "Remark Alpha")

		remarkSession := model.Session{
			SessionID:      "session-search-remark",
			OwnerID:        ownerID,
			SessionType:    1,
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&remarkSession).Error; err != nil {
			t.Fatalf("create remark session error: %v", err)
		}
		remarkMembers := []model.SessionMember{
			{SessionID: remarkSession.SessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
			{SessionID: remarkSession.SessionID, MemberID: remarkPeerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := testDB.DB.Create(&remarkMembers).Error; err != nil {
			t.Fatalf("create remark members error: %v", err)
		}

		groupSession := model.Session{
			SessionID:      "session-search-group",
			OwnerID:        ownerID,
			SessionType:    2,
			GroupName:      "Project Atlas",
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&groupSession).Error; err != nil {
			t.Fatalf("create group session error: %v", err)
		}
		groupMembers := []model.SessionMember{
			{SessionID: groupSession.SessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now.Add(-time.Minute), JoinedAt: now.Add(-time.Minute)},
			{SessionID: groupSession.SessionID, MemberID: remarkPeerID, MemberType: 1, Role: 1, LastActiveAt: now.Add(-time.Minute), JoinedAt: now.Add(-time.Minute)},
		}
		if err := testDB.DB.Create(&groupMembers).Error; err != nil {
			t.Fatalf("create group members error: %v", err)
		}

		firstMessageSession := model.Session{
			SessionID:      "session-search-first-message",
			OwnerID:        ownerID,
			SessionType:    1,
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&firstMessageSession).Error; err != nil {
			t.Fatalf("create first-message session error: %v", err)
		}
		firstMessageMembers := []model.SessionMember{
			{SessionID: firstMessageSession.SessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now.Add(-2 * time.Minute), JoinedAt: now.Add(-2 * time.Minute)},
			{SessionID: firstMessageSession.SessionID, MemberID: firstMessagePeerID, MemberType: 1, Role: 1, LastActiveAt: now.Add(-2 * time.Minute), JoinedAt: now.Add(-2 * time.Minute)},
		}
		if err := testDB.DB.Create(&firstMessageMembers).Error; err != nil {
			t.Fatalf("create first-message members error: %v", err)
		}
		firstMessageContent := "Budget Review Kickoff"
		if err := testDB.DB.Create(&model.Message{
			MsgID:      3101,
			SessionID:  firstMessageSession.SessionID,
			SenderID:   ownerID,
			SenderType: 1,
			MsgType:    1,
			Content:    firstMessageContent,
		}).Error; err != nil {
			t.Fatalf("create first message error: %v", err)
		}

		remarkResp, err := SessionSearch(ownerID, "remark", 20, 0, 0)
		if err != nil {
			t.Fatalf("SessionSearch(remark) error = %v", err)
		}
		if len(remarkResp.List) != 1 || remarkResp.List[0].SessionID != remarkSession.SessionID {
			t.Fatalf("expected only remark session, got %#v", remarkResp.List)
		}
		if remarkResp.List[0].Title != "Remark Alpha" {
			t.Fatalf("expected remark title, got %q", remarkResp.List[0].Title)
		}

		groupResp, err := SessionSearch(ownerID, "ATLAS", 20, 0, 0)
		if err != nil {
			t.Fatalf("SessionSearch(ATLAS) error = %v", err)
		}
		if len(groupResp.List) != 1 || groupResp.List[0].SessionID != groupSession.SessionID {
			t.Fatalf("expected only group session, got %#v", groupResp.List)
		}
		if groupResp.List[0].Title != groupSession.GroupName {
			t.Fatalf("expected group title %q, got %q", groupSession.GroupName, groupResp.List[0].Title)
		}

		// 私聊优先使用对端昵称作为 title，搜索对端昵称可以匹配
		peerNicknameResp, err := SessionSearch(ownerID, fmt.Sprintf("User%d", firstMessagePeerID), 20, 0, 0)
		if err != nil {
			t.Fatalf("SessionSearch(peer nickname) error = %v", err)
		}
		if len(peerNicknameResp.List) != 1 || peerNicknameResp.List[0].SessionID != firstMessageSession.SessionID {
			t.Fatalf("expected only first-message session by peer nickname, got %#v", peerNicknameResp.List)
		}

		// 搜索第一条消息内容不再匹配（因为 title 已经是对端昵称）
		firstMessageResp, err := SessionSearch(ownerID, "review", 20, 0, 0)
		if err != nil {
			t.Fatalf("SessionSearch(review) error = %v", err)
		}
		if len(firstMessageResp.List) != 0 {
			t.Fatalf("expected no session matched by first message content, got %#v", firstMessageResp.List)
		}
	})

	t.Run("supports compact multi token and session id fuzzy matching", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19401)
		now := time.Now()

		seedUser(t, testDB, ownerID)

		compactSession := model.Session{
			SessionID:      "task_room_9083",
			OwnerID:        ownerID,
			SessionType:    2,
			GroupName:      "Task Room 9083",
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&compactSession).Error; err != nil {
			t.Fatalf("create compact session error: %v", err)
		}
		compactMember := model.SessionMember{
			SessionID:    compactSession.SessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		}
		if err := testDB.DB.Create(&compactMember).Error; err != nil {
			t.Fatalf("create compact member error: %v", err)
		}

		multiTokenSession := model.Session{
			SessionID:      "session-project-atlas-review",
			OwnerID:        ownerID,
			SessionType:    2,
			GroupName:      "Project Atlas Review",
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&multiTokenSession).Error; err != nil {
			t.Fatalf("create multi token session error: %v", err)
		}
		multiTokenMember := model.SessionMember{
			SessionID:    multiTokenSession.SessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now.Add(-time.Minute),
			JoinedAt:     now.Add(-time.Minute),
		}
		if err := testDB.DB.Create(&multiTokenMember).Error; err != nil {
			t.Fatalf("create multi token member error: %v", err)
		}

		compactResp, err := SessionSearch(ownerID, "taskroom9083", 20, 0, 0)
		if err != nil {
			t.Fatalf("SessionSearch(taskroom9083) error = %v", err)
		}
		if len(compactResp.List) != 1 || compactResp.List[0].SessionID != compactSession.SessionID {
			t.Fatalf("expected compact title match, got %#v", compactResp.List)
		}

		tokenResp, err := SessionSearch(ownerID, "atlas review", 20, 0, 0)
		if err != nil {
			t.Fatalf("SessionSearch(atlas review) error = %v", err)
		}
		if len(tokenResp.List) != 1 || tokenResp.List[0].SessionID != multiTokenSession.SessionID {
			t.Fatalf("expected multi token title match, got %#v", tokenResp.List)
		}

		sessionIDResp, err := SessionSearch(ownerID, "projectatlasreview", 20, 0, 0)
		if err != nil {
			t.Fatalf("SessionSearch(projectatlasreview) error = %v", err)
		}
		if len(sessionIDResp.List) != 1 || sessionIDResp.List[0].SessionID != multiTokenSession.SessionID {
			t.Fatalf("expected compact session id/title fuzzy match, got %#v", sessionIDResp.List)
		}
	})

	t.Run("paginates after relevance ordering", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19411)
		now := time.Now()

		seedUser(t, testDB, ownerID)

		sessions := []struct {
			id     string
			title  string
			active time.Time
		}{
			{id: "session-search-page-1", title: "Alpha", active: now.Add(-2 * time.Minute)},
			{id: "session-search-page-2", title: "Alpha Room", active: now.Add(-time.Minute)},
			{id: "session-search-page-3", title: "Project Alpha Review", active: now},
		}
		for _, entry := range sessions {
			session := model.Session{
				SessionID:      entry.id,
				OwnerID:        ownerID,
				SessionType:    2,
				GroupName:      entry.title,
				LastMsgSummary: "latest",
			}
			if err := testDB.DB.Create(&session).Error; err != nil {
				t.Fatalf("create paged session error: %v", err)
			}
			member := model.SessionMember{
				SessionID:    entry.id,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: entry.active,
				JoinedAt:     entry.active,
			}
			if err := testDB.DB.Create(&member).Error; err != nil {
				t.Fatalf("create paged member error: %v", err)
			}
		}

		page1, err := SessionSearch(ownerID, "alpha", 2, 0, 0)
		if err != nil {
			t.Fatalf("SessionSearch page1 error = %v", err)
		}
		if !page1.HasMore {
			t.Fatalf("expected page1 has_more=true")
		}
		if len(page1.List) != 2 {
			t.Fatalf("expected 2 items on page1, got %d", len(page1.List))
		}
		if page1.List[0].SessionID != "session-search-page-1" || page1.List[1].SessionID != "session-search-page-2" {
			t.Fatalf("unexpected page1 order: %#v", page1.List)
		}

		page2, err := SessionSearch(ownerID, "alpha", 2, 2, 0)
		if err != nil {
			t.Fatalf("SessionSearch page2 error = %v", err)
		}
		if page2.HasMore {
			t.Fatalf("expected page2 has_more=false")
		}
		if len(page2.List) != 1 {
			t.Fatalf("expected 1 item on page2, got %d", len(page2.List))
		}
		if page2.List[0].SessionID != "session-search-page-3" {
			t.Fatalf("unexpected page2 session_id: %#v", page2.List)
		}
	})
}

func TestSessionSearchByID(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	t.Run("returns exact visible session in search format", func(t *testing.T) {
		testDB.Cleanup()
		ownerID := int64(19451)
		peerID := int64(19452)
		now := time.Now()

		seedUser(t, testDB, ownerID)
		seedUser(t, testDB, peerID)
		seedFriendRelationWithRemark(t, testDB, ownerID, peerID, "Exact Atlas")

		session := model.Session{
			SessionID:      "session-search-by-id",
			OwnerID:        ownerID,
			SessionType:    1,
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create session error: %v", err)
		}
		members := []model.SessionMember{
			{SessionID: session.SessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
			{SessionID: session.SessionID, MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := testDB.DB.Create(&members).Error; err != nil {
			t.Fatalf("create session members error: %v", err)
		}

		resp, err := SessionSearchByID(ownerID, session.SessionID, 20, 0, 0)
		if err != nil {
			t.Fatalf("SessionSearchByID() error = %v", err)
		}
		if resp.HasMore {
			t.Fatalf("expected has_more=false")
		}
		if len(resp.List) != 1 {
			t.Fatalf("expected 1 result, got %d", len(resp.List))
		}
		if resp.List[0].SessionID != session.SessionID {
			t.Fatalf("expected session_id %q, got %q", session.SessionID, resp.List[0].SessionID)
		}
		if resp.List[0].Title != "Exact Atlas" {
			t.Fatalf("expected title Exact Atlas, got %q", resp.List[0].Title)
		}

		emptyResp, err := SessionSearchByID(ownerID, "missing-session", 20, 0, 0)
		if err != nil {
			t.Fatalf("SessionSearchByID(missing) error = %v", err)
		}
		if len(emptyResp.List) != 0 {
			t.Fatalf("expected no results for missing session, got %#v", emptyResp.List)
		}
	})
}

func TestSessionSync(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(2001)

	t.Run("sync since timestamp", func(t *testing.T) {
		// Create session with current time
		createTestSessionWithMembers(t, testDB, userID, "sync-session-1")

		// Sync from 1 hour ago
		since := time.Now().Add(-1 * time.Hour).Unix()
		resp, err := SessionSync(userID, since, 10)
		if err != nil {
			t.Fatalf("SessionSync() error = %v", err)
		}

		if len(resp.List) != 1 {
			t.Errorf("expected 1 session, got %d", len(resp.List))
		}
	})

	t.Run("sync with old timestamp", func(t *testing.T) {
		// Sync from 1 day ago - should include all
		since := time.Now().Add(-24 * time.Hour).Unix()
		resp, err := SessionSync(userID, since, 10)
		if err != nil {
			t.Fatalf("SessionSync() error = %v", err)
		}

		if len(resp.List) < 1 {
			t.Error("expected at least 1 session")
		}
	})

}

func TestSessionSyncDeletedTombstones(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(7001)
	since := time.Now().Add(-1 * time.Hour).Unix()
	deletedAt := time.Now()

	// tomb-left：用户已退出（有墓碑、当前无成员）→ 应出现在 deleted_session_ids。
	if err := testDB.DB.Create(&model.Session{
		SessionID:   "tomb-left",
		OwnerID:     userID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create left session: %v", err)
	}
	// tomb-rejoin：退出后又重新加入（有墓碑、当前仍是成员）→ 必须被 LEFT JOIN 排除。
	createTestSessionWithMembers(t, testDB, userID, "tomb-rejoin")

	if err := recordSessionTombstones(testDB.DB, "tomb-left", []int64{userID}, deletedAt); err != nil {
		t.Fatalf("record left tombstone: %v", err)
	}
	if err := recordSessionTombstones(testDB.DB, "tomb-rejoin", []int64{userID}, deletedAt); err != nil {
		t.Fatalf("record rejoin tombstone: %v", err)
	}

	resp, err := SessionSync(userID, since, 50)
	if err != nil {
		t.Fatalf("SessionSync() error = %v", err)
	}

	deleted := make(map[string]bool, len(resp.DeletedSessionIDs))
	for _, id := range resp.DeletedSessionIDs {
		deleted[id] = true
	}
	if !deleted["tomb-left"] {
		t.Errorf("expected tomb-left in deleted_session_ids, got %v", resp.DeletedSessionIDs)
	}
	if deleted["tomb-rejoin"] {
		t.Errorf("rejoined session must be excluded from deleted_session_ids, got %v", resp.DeletedSessionIDs)
	}
	if resp.Cursor <= 0 {
		t.Errorf("expected positive cursor, got %d", resp.Cursor)
	}
}

func TestSessionSyncIncrementalWindow(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(7101)
	now := time.Now()

	mkSession := func(sid string, lastActive time.Time) {
		if err := testDB.DB.Create(&model.Session{
			SessionID:   sid,
			OwnerID:     userID,
			SessionType: 2,
		}).Error; err != nil {
			t.Fatalf("create session %s: %v", sid, err)
		}
		if err := testDB.DB.Create(&model.SessionMember{
			SessionID:    sid,
			MemberID:     userID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: lastActive,
			JoinedAt:     lastActive,
		}).Error; err != nil {
			t.Fatalf("create member %s: %v", sid, err)
		}
	}
	mkSession("incr-old", now.Add(-2*time.Hour))
	mkSession("incr-new", now)

	t.Run("only returns sessions changed after since", func(t *testing.T) {
		resp, err := SessionSync(userID, now.Add(-1*time.Hour).Unix(), 50)
		if err != nil {
			t.Fatalf("SessionSync() error = %v", err)
		}
		if len(resp.List) != 1 || resp.List[0].SessionID != "incr-new" {
			t.Errorf("expected only incr-new, got %+v", resp.List)
		}
		if resp.Cursor <= 0 {
			t.Errorf("expected positive cursor, got %d", resp.Cursor)
		}
	})

	t.Run("future since returns empty incremental window", func(t *testing.T) {
		resp, err := SessionSync(userID, now.Add(1*time.Minute).Unix(), 50)
		if err != nil {
			t.Fatalf("SessionSync() error = %v", err)
		}
		if len(resp.List) != 0 {
			t.Errorf("expected empty list for future since, got %+v", resp.List)
		}
	})
}

func TestSessionCreate(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(3001)
	peerID := int64(3002)
	seedUser(t, testDB, userID)
	seedUser(t, testDB, peerID)
	seedFriendRelation(t, testDB, userID, peerID)

	t.Run("create new session", func(t *testing.T) {
		resp, err := SessionCreate(userID, peerID, 1)
		if err != nil {
			t.Fatalf("SessionCreate() error = %v", err)
		}

		if resp.SessionID == "" {
			t.Error("expected session ID")
		}
		if !resp.IsNew {
			t.Error("expected IsNew to be true")
		}
		if _, err := uuid.Parse(resp.SessionID); err != nil {
			t.Fatalf("expected UUID session ID, got %q: %v", resp.SessionID, err)
		}
	})

	t.Run("create existing session", func(t *testing.T) {
		// Create first time
		resp1, _ := SessionCreate(userID, peerID, 1)

		// Create again - should create a new record
		resp2, err := SessionCreate(userID, peerID, 1)
		if err != nil {
			t.Fatalf("SessionCreate() second call error = %v", err)
		}

		if !resp2.IsNew {
			t.Error("expected IsNew to be true for new record")
		}
		if resp2.SessionID == resp1.SessionID {
			t.Error("session IDs should be different")
		}

		var cnt int64
		directKey := buildDirectKey(userID, peerID, 1)
		if err := store.DB.Model(&model.Session{}).
			Where("direct_key = ?", directKey).
			Count(&cnt).Error; err != nil {
			t.Fatalf("count sessions by direct_key error: %v", err)
		}
		if cnt < 2 {
			t.Fatalf("expected at least 2 sessions with same direct_key, got %d", cnt)
		}
	})

	t.Run("create session with agent peer", func(t *testing.T) {
		agentID := int64(4001)
		seedAgent(t, testDB, agentID, userID, 1)
		resp, err := SessionCreate(userID, agentID, 2) // peerType 2 = agent
		if err != nil {
			t.Fatalf("SessionCreate() error = %v", err)
		}
		if _, err := uuid.Parse(resp.SessionID); err != nil {
			t.Fatalf("expected UUID session ID for agent chat, got %q: %v", resp.SessionID, err)
		}
	})

	t.Run("rejects foreign agent peer", func(t *testing.T) {
		foreignOwnerID := int64(4002)
		foreignAgentID := int64(4003)
		seedUser(t, testDB, foreignOwnerID)
		seedAgent(t, testDB, foreignAgentID, foreignOwnerID, 1)

		_, err := SessionCreate(userID, foreignAgentID, 2)
		if !errors.Is(err, ErrMemberAgentNotOwned) {
			t.Fatalf("expected ErrMemberAgentNotOwned, got %v", err)
		}
	})

	t.Run("rejects unavailable owned agent peer", func(t *testing.T) {
		disabledAgentID := int64(4004)
		seedAgent(t, testDB, disabledAgentID, userID, 2)

		_, err := SessionCreate(userID, disabledAgentID, 2)
		if !errors.Is(err, ErrMemberAgentUnavailable) {
			t.Fatalf("expected ErrMemberAgentUnavailable, got %v", err)
		}
	})

	t.Run("direct key is deterministic", func(t *testing.T) {
		seedUser(t, testDB, 100)
		seedUser(t, testDB, 200)
		seedFriendRelation(t, testDB, 100, 200)
		seedFriendRelation(t, testDB, 200, 100)

		// Order of user/peer shouldn't matter for direct_key
		resp1, err := SessionCreate(100, 200, 1)
		if err != nil {
			t.Fatalf("create 100->200 session error: %v", err)
		}
		resp2, err := SessionCreate(200, 100, 1)
		if err != nil {
			t.Fatalf("create 200->100 session error: %v", err)
		}

		if resp1.SessionID == resp2.SessionID {
			t.Fatalf("session IDs should be different across separate records")
		}

		var s1 model.Session
		if err := store.DB.Where("session_id = ?", resp1.SessionID).First(&s1).Error; err != nil {
			t.Fatalf("query session1 error: %v", err)
		}
		var s2 model.Session
		if err := store.DB.Where("session_id = ?", resp2.SessionID).First(&s2).Error; err != nil {
			t.Fatalf("query session2 error: %v", err)
		}

		if s1.DirectKey == nil || s2.DirectKey == nil {
			t.Fatalf("direct_key should not be nil for private sessions")
		}
		if *s1.DirectKey != *s2.DirectKey {
			t.Fatalf("direct_key should match regardless of order: %s vs %s", *s1.DirectKey, *s2.DirectKey)
		}
	})

	t.Run("rejects non-friend human peer", func(t *testing.T) {
		nonFriendID := int64(3011)
		seedUser(t, testDB, nonFriendID)

		_, err := SessionCreate(userID, nonFriendID, 1)
		if !errors.Is(err, ErrMemberNotFriend) {
			t.Fatalf("expected ErrMemberNotFriend, got %v", err)
		}
	})

	t.Run("rejects blocked human peer", func(t *testing.T) {
		blockedPeerID := int64(3012)
		seedUser(t, testDB, blockedPeerID)
		seedFriendRelation(t, testDB, userID, blockedPeerID)
		seedFriendRelation(t, testDB, blockedPeerID, userID)
		seedUserBlock(t, testDB, blockedPeerID, userID)

		_, err := SessionCreate(userID, blockedPeerID, 1)
		if !errors.Is(err, ErrMemberNotFriend) {
			t.Fatalf("expected ErrMemberNotFriend for blocked peer, got %v", err)
		}
	})

	t.Run("auto starts delegate for peer who set default agent", func(t *testing.T) {
		prevRDB := store.RDB
		mockRDB := testutil.NewMockRedis()
		store.RDB = mockRDB
		defer func() {
			store.RDB = prevRDB
			_ = mockRDB.Close()
		}()

		peerAutoAgentID := int64(4011)
		seedAgent(t, testDB, peerAutoAgentID, peerID, 1)
		rawAgentID := fmt.Sprintf("%d", peerAutoAgentID)
		if _, err := UpdateUserSettings(peerID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AutoDelegateAgentID: OptionalStringUpdate{
					Set:   true,
					Value: &rawAgentID,
				},
			},
		}); err != nil {
			t.Fatalf("set peer default auto delegate agent error: %v", err)
		}

		resp, err := SessionCreate(userID, peerID, 1)
		if err != nil {
			t.Fatalf("SessionCreate() error = %v", err)
		}

		ctx := context.Background()
		delegateKey := fmt.Sprintf("im:delegate:%s:%d", resp.SessionID, peerID)
		agentIDInRedis, err := store.RDB.HGet(ctx, delegateKey, "agent_id").Result()
		if err != nil {
			t.Fatalf("read auto delegate agent_id from redis error: %v", err)
		}
		if agentIDInRedis != fmt.Sprintf("%d", peerAutoAgentID) {
			t.Fatalf("unexpected auto delegate agent_id, got=%s want=%d", agentIDInRedis, peerAutoAgentID)
		}

		maxRounds, err := store.RDB.HGet(ctx, delegateKey, "max_consecutive_replies").Int()
		if err != nil {
			t.Fatalf("read auto delegate max_consecutive_replies from redis error: %v", err)
		}
		if maxRounds != autoDelegateDefaultMaxConsecutiveReplies {
			t.Fatalf(
				"unexpected auto delegate max_consecutive_replies, got=%d want=%d",
				maxRounds,
				autoDelegateDefaultMaxConsecutiveReplies,
			)
		}

		var log model.DelegationLog
		if err := store.DB.
			Where("session_id = ? AND user_id = ? AND action = ?", resp.SessionID, peerID, "auto_start").
			First(&log).Error; err != nil {
			t.Fatalf("query auto_start delegation log error: %v", err)
		}
		if log.AgentID != peerAutoAgentID {
			t.Fatalf("delegation log agent_id=%d want=%d", log.AgentID, peerAutoAgentID)
		}
	})
}

func TestSessionOpenLatest(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(3101)
	peerID := int64(3102)
	freshPeerID := int64(3999)
	seedUser(t, testDB, userID)
	seedUser(t, testDB, peerID)
	seedUser(t, testDB, freshPeerID)
	seedFriendRelation(t, testDB, userID, peerID)
	seedFriendRelation(t, testDB, userID, freshPeerID)

	t.Run("returns latest existing session", func(t *testing.T) {
		oldResp, err := SessionCreate(userID, peerID, 1)
		if err != nil {
			t.Fatalf("create old session error: %v", err)
		}
		newResp, err := SessionCreate(userID, peerID, 1)
		if err != nil {
			t.Fatalf("create new session error: %v", err)
		}
		if oldResp.SessionID == newResp.SessionID {
			t.Fatalf("expected different session IDs")
		}

		if err := store.DB.Model(&model.Session{}).
			Where("session_id = ?", oldResp.SessionID).
			Update("updated_at", time.Now().Add(-time.Hour)).Error; err != nil {
			t.Fatalf("downgrade old updated_at error: %v", err)
		}
		if err := store.DB.Model(&model.Session{}).
			Where("session_id = ?", newResp.SessionID).
			Update("updated_at", time.Now()).Error; err != nil {
			t.Fatalf("upgrade new updated_at error: %v", err)
		}

		resp, err := SessionOpenLatest(userID, peerID, 1)
		if err != nil {
			t.Fatalf("SessionOpenLatest() error = %v", err)
		}
		if resp.IsNew {
			t.Fatalf("expected open latest to return existing session")
		}
		if resp.SessionID != newResp.SessionID {
			t.Fatalf("expected latest session_id=%s got=%s", newResp.SessionID, resp.SessionID)
		}
	})

	t.Run("creates new session if no record exists", func(t *testing.T) {
		resp, err := SessionOpenLatest(userID, freshPeerID, 1)
		if err != nil {
			t.Fatalf("SessionOpenLatest() create error = %v", err)
		}
		if !resp.IsNew {
			t.Fatalf("expected IsNew=true when no existing session")
		}
		if _, err := uuid.Parse(resp.SessionID); err != nil {
			t.Fatalf("expected UUID session ID, got %q: %v", resp.SessionID, err)
		}
	})

	t.Run("rejects non-friend human peer", func(t *testing.T) {
		nonFriendID := int64(3998)
		seedUser(t, testDB, nonFriendID)

		_, err := SessionOpenLatest(userID, nonFriendID, 1)
		if !errors.Is(err, ErrMemberNotFriend) {
			t.Fatalf("expected ErrMemberNotFriend, got %v", err)
		}
	})

	t.Run("rejects foreign agent peer", func(t *testing.T) {
		foreignOwnerID := int64(3997)
		foreignAgentID := int64(3996)
		seedUser(t, testDB, foreignOwnerID)
		seedAgent(t, testDB, foreignAgentID, foreignOwnerID, 1)

		_, err := SessionOpenLatest(userID, foreignAgentID, 2)
		if !errors.Is(err, ErrMemberAgentNotOwned) {
			t.Fatalf("expected ErrMemberAgentNotOwned, got %v", err)
		}
	})
}

func TestSessionCreateGroup(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(5001)
	memberIDs := []int64{5002, 5003, 5004}
	memberTypes := []int16{1, 1, 2}

	seedUser(t, testDB, userID)
	seedUser(t, testDB, 5002)
	seedUser(t, testDB, 5003)
	seedFriendRelation(t, testDB, userID, 5002)
	seedFriendRelation(t, testDB, userID, 5003)
	seedAgent(t, testDB, 5004, userID, 1)

	t.Run("create group session", func(t *testing.T) {
		resp, err := SessionCreateGroup(userID, "Test Group", memberIDs, memberTypes)
		if err != nil {
			t.Fatalf("SessionCreateGroup() error = %v", err)
		}

		if resp.SessionID == "" {
			t.Error("expected session ID")
		}
		if !resp.IsNew {
			t.Error("expected IsNew to be true")
		}
		if _, err := uuid.Parse(resp.SessionID); err != nil {
			t.Fatalf("expected UUID group session ID, got %q: %v", resp.SessionID, err)
		}

		// Verify session type is group (2)
		var session model.Session
		store.DB.First(&session, "session_id = ?", resp.SessionID)
		if session.SessionType != 2 {
			t.Errorf("expected session type 2 (group), got %d", session.SessionType)
		}
	})

	t.Run("pushes offline notice for invited human members", func(t *testing.T) {
		prevOfflinePushRunner := sessionMemberAddedOfflinePushRunner
		t.Cleanup(func() {
			sessionMemberAddedOfflinePushRunner = prevOfflinePushRunner
		})

		var called bool
		var gotSessionID string
		var gotOperatorID int64
		var gotGroupName string
		var gotUserIDs []int64
		sessionMemberAddedOfflinePushRunner = func(sessionID string, operatorID int64, groupName string, userIDs []int64) {
			called = true
			gotSessionID = sessionID
			gotOperatorID = operatorID
			gotGroupName = groupName
			gotUserIDs = append([]int64(nil), userIDs...)
		}

		resp, err := SessionCreateGroup(userID, "Push Group", memberIDs, memberTypes)
		if err != nil {
			t.Fatalf("SessionCreateGroup() error = %v", err)
		}
		if !called {
			t.Fatal("expected offline push runner to be called")
		}
		if gotSessionID != resp.SessionID {
			t.Fatalf("offline push session_id=%s want=%s", gotSessionID, resp.SessionID)
		}
		if gotOperatorID != userID {
			t.Fatalf("offline push operator_id=%d want=%d", gotOperatorID, userID)
		}
		if gotGroupName != "Push Group" {
			t.Fatalf("offline push group_name=%q want=%q", gotGroupName, "Push Group")
		}
		if !reflect.DeepEqual(gotUserIDs, []int64{5002, 5003}) {
			t.Fatalf("offline push user_ids=%v want=[5002 5003]", gotUserIDs)
		}
	})

	t.Run("verify members created", func(t *testing.T) {
		resp, _ := SessionCreateGroup(userID, "Member Test", memberIDs, memberTypes)

		// Count members
		var count int64
		store.DB.Model(&model.SessionMember{}).Where("session_id = ?", resp.SessionID).Count(&count)

		// Should have creator + 3 members = 4
		expectedCount := int64(1 + len(memberIDs))
		if count != expectedCount {
			t.Errorf("expected %d members, got %d", expectedCount, count)
		}
	})

	t.Run("creator has owner role", func(t *testing.T) {
		resp, _ := SessionCreateGroup(userID, "Role Test", memberIDs, memberTypes)

		var member model.SessionMember
		store.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", resp.SessionID, userID).First(&member)

		if member.Role != 3 {
			t.Errorf("expected creator role 3, got %d", member.Role)
		}
	})

	t.Run("deduplicates member ids and filters owner id", func(t *testing.T) {
		resp, err := SessionCreateGroup(
			userID,
			"Dedup Test",
			[]int64{userID, 5002, 5002, 5003},
			[]int16{1, 1, 1, 1},
		)
		if err != nil {
			t.Fatalf("SessionCreateGroup() error = %v", err)
		}

		var rows []model.SessionMember
		if err := testDB.DB.Where("session_id = ?", resp.SessionID).Find(&rows).Error; err != nil {
			t.Fatalf("query members error: %v", err)
		}
		if len(rows) != 3 {
			t.Fatalf("expected 3 members (owner + 2 unique), got %d", len(rows))
		}
	})

	t.Run("rejects invalid member_types length", func(t *testing.T) {
		_, err := SessionCreateGroup(userID, "Mismatch", []int64{5002, 5003}, []int16{1})
		if !errors.Is(err, ErrMemberTypesMismatch) {
			t.Fatalf("expected ErrMemberTypesMismatch, got %v", err)
		}
	})

	t.Run("rejects non-friend human member", func(t *testing.T) {
		seedUser(t, testDB, 5005)
		_, err := SessionCreateGroup(userID, "Invalid Friend", []int64{5005}, []int16{1})
		if !errors.Is(err, ErrMemberNotFriend) {
			t.Fatalf("expected ErrMemberNotFriend, got %v", err)
		}
	})

	t.Run("rejects target user who disallows group invite", func(t *testing.T) {
		blockedUserID := int64(5006)
		seedUser(t, testDB, blockedUserID)
		seedFriendRelation(t, testDB, userID, blockedUserID)
		if err := testDB.DB.Create(&model.UserSetting{
			UserID:           blockedUserID,
			FriendAddSetting: model.FriendAddSettingNeedApproval,
			AllowGroupInvite: true,
		}).Error; err != nil {
			t.Fatalf("seed blocked user setting error: %v", err)
		}
		if err := testDB.DB.Model(&model.UserSetting{}).
			Where("user_id = ?", blockedUserID).
			Update("allow_group_invite", false).Error; err != nil {
			t.Fatalf("disable blocked user group invite error: %v", err)
		}

		_, err := SessionCreateGroup(userID, "Blocked Group", []int64{blockedUserID}, []int16{1})
		if !errors.Is(err, ErrSessionTargetGroupInviteRejected) {
			t.Fatalf("expected ErrSessionTargetGroupInviteRejected, got %v", err)
		}
	})

	t.Run("auto starts delegate for invited member with default agent", func(t *testing.T) {
		prevRDB := store.RDB
		mockRDB := testutil.NewMockRedis()
		store.RDB = mockRDB
		defer func() {
			store.RDB = prevRDB
			_ = mockRDB.Close()
		}()

		invitedUserID := int64(5002)
		invitedAutoAgentID := int64(5011)
		seedAgent(t, testDB, invitedAutoAgentID, invitedUserID, 1)
		rawAgentID := fmt.Sprintf("%d", invitedAutoAgentID)
		if _, err := UpdateUserSettings(invitedUserID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AutoDelegateAgentID: OptionalStringUpdate{
					Set:   true,
					Value: &rawAgentID,
				},
			},
		}); err != nil {
			t.Fatalf("set invited user default auto delegate agent error: %v", err)
		}

		resp, err := SessionCreateGroup(userID, "Auto Delegate Group", []int64{invitedUserID}, []int16{1})
		if err != nil {
			t.Fatalf("SessionCreateGroup() error = %v", err)
		}

		ctx := context.Background()
		delegateKey := fmt.Sprintf("im:delegate:%s:%d", resp.SessionID, invitedUserID)
		agentIDInRedis, err := store.RDB.HGet(ctx, delegateKey, "agent_id").Result()
		if err != nil {
			t.Fatalf("read auto delegate agent_id from redis error: %v", err)
		}
		if agentIDInRedis != fmt.Sprintf("%d", invitedAutoAgentID) {
			t.Fatalf("unexpected auto delegate agent_id, got=%s want=%d", agentIDInRedis, invitedAutoAgentID)
		}

		maxRounds, err := store.RDB.HGet(ctx, delegateKey, "max_consecutive_replies").Int()
		if err != nil {
			t.Fatalf("read auto delegate max_consecutive_replies from redis error: %v", err)
		}
		if maxRounds != autoDelegateDefaultMaxConsecutiveReplies {
			t.Fatalf(
				"unexpected auto delegate max_consecutive_replies, got=%d want=%d",
				maxRounds,
				autoDelegateDefaultMaxConsecutiveReplies,
			)
		}

		var log model.DelegationLog
		if err := store.DB.
			Where("session_id = ? AND user_id = ? AND action = ?", resp.SessionID, invitedUserID, "auto_start").
			First(&log).Error; err != nil {
			t.Fatalf("query auto_start delegation log error: %v", err)
		}
		if log.AgentID != invitedAutoAgentID {
			t.Fatalf("delegation log agent_id=%d want=%d", log.AgentID, invitedAutoAgentID)
		}
	})
}

func TestSessionCreateGroupByAgent(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		ownerID    = int64(5101)
		currentID  = int64(5102)
		friendID   = int64(5103)
		teammateID = int64(5104)
	)

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, friendID)
	seedFriendRelation(t, testDB, ownerID, friendID)
	seedAgent(t, testDB, currentID, ownerID, 1)
	seedAgent(t, testDB, teammateID, ownerID, 1)

	t.Run("adds current agent when member types are omitted", func(t *testing.T) {
		resp, err := SessionCreateGroupByAgent(ownerID, currentID, "Agent Created Group", []int64{friendID}, nil)
		if err != nil {
			t.Fatalf("SessionCreateGroupByAgent() error = %v", err)
		}

		var count int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ?", resp.SessionID).
			Count(&count).Error; err != nil {
			t.Fatalf("count group members error: %v", err)
		}
		if count != 3 {
			t.Fatalf("expected 3 members (owner + friend + current agent), got %d", count)
		}

		var agentMember model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_id = ? AND member_type = 2",
			resp.SessionID,
			currentID,
		).First(&agentMember).Error; err != nil {
			t.Fatalf("expected current agent membership: %v", err)
		}
	})

	t.Run("deduplicates current agent if already present", func(t *testing.T) {
		resp, err := SessionCreateGroupByAgent(
			ownerID,
			currentID,
			"Explicit Agent Group",
			[]int64{friendID, currentID, teammateID},
			[]int16{1, 2, 2},
		)
		if err != nil {
			t.Fatalf("SessionCreateGroupByAgent() error = %v", err)
		}

		var rows []model.SessionMember
		if err := testDB.DB.Where("session_id = ?", resp.SessionID).Find(&rows).Error; err != nil {
			t.Fatalf("query group members error: %v", err)
		}
		if len(rows) != 4 {
			t.Fatalf("expected 4 unique members (owner + friend + 2 agents), got %d", len(rows))
		}

		var agentCount int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 2", resp.SessionID, currentID).
			Count(&agentCount).Error; err != nil {
			t.Fatalf("count current agent membership error: %v", err)
		}
		if agentCount != 1 {
			t.Fatalf("expected current agent to be inserted once, got %d", agentCount)
		}
	})
}

func TestSessionAddMembers(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(7001)
	sessionID := "session-add-members-1"
	now := time.Now()

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, 7002)
	seedFriendRelation(t, testDB, ownerID, 7002)
	seedAgent(t, testDB, 7003, ownerID, 1)

	session := model.Session{
		SessionID:         sessionID,
		OwnerID:           ownerID,
		SessionType:       2,
		AllowMemberInvite: true,
		LastMsgSummary:    "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	ownerMember := model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		Role:         3,
		LastActiveAt: now,
		JoinedAt:     now,
	}
	if err := testDB.DB.Create(&ownerMember).Error; err != nil {
		t.Fatalf("create owner member error: %v", err)
	}

	t.Run("adds members idempotently", func(t *testing.T) {
		prevOfflinePushRunner := sessionMemberAddedOfflinePushRunner
		t.Cleanup(func() {
			sessionMemberAddedOfflinePushRunner = prevOfflinePushRunner
		})
		var offlinePushCalls int
		var offlinePushSessionID string
		var offlinePushOperatorID int64
		var offlinePushGroupName string
		var offlinePushUserIDs []int64
		sessionMemberAddedOfflinePushRunner = func(sessionID string, operatorID int64, groupName string, userIDs []int64) {
			offlinePushCalls++
			offlinePushSessionID = sessionID
			offlinePushOperatorID = operatorID
			offlinePushGroupName = groupName
			offlinePushUserIDs = append([]int64(nil), userIDs...)
		}

		resp, err := SessionAddMembers(
			ownerID,
			sessionID,
			[]int64{7002, 7002, ownerID, 7003},
			[]int16{1, 1, 1, 2},
		)
		if err != nil {
			t.Fatalf("SessionAddMembers() error = %v", err)
		}
		if resp.AddedCount != 2 {
			t.Fatalf("expected added_count=2, got %d", resp.AddedCount)
		}
		if resp.MemberCount != 3 {
			t.Fatalf("expected member_count=3, got %d", resp.MemberCount)
		}
		if offlinePushCalls != 1 {
			t.Fatalf("expected offline push call=1, got %d", offlinePushCalls)
		}
		if offlinePushSessionID != sessionID {
			t.Fatalf("offline push session_id=%s want=%s", offlinePushSessionID, sessionID)
		}
		if offlinePushOperatorID != ownerID {
			t.Fatalf("offline push operator_id=%d want=%d", offlinePushOperatorID, ownerID)
		}
		if offlinePushGroupName != "" {
			t.Fatalf("offline push group_name should be empty, got=%q", offlinePushGroupName)
		}
		if !reflect.DeepEqual(offlinePushUserIDs, []int64{7002}) {
			t.Fatalf("offline push user_ids=%v want=[7002]", offlinePushUserIDs)
		}

		resp2, err := SessionAddMembers(ownerID, sessionID, []int64{7002, 7003}, []int16{1, 2})
		if err != nil {
			t.Fatalf("SessionAddMembers() second call error = %v", err)
		}
		if resp2.AddedCount != 0 {
			t.Fatalf("expected added_count=0 on duplicate add, got %d", resp2.AddedCount)
		}
		if offlinePushCalls != 1 {
			t.Fatalf("duplicate add should not trigger extra offline push, got calls=%d", offlinePushCalls)
		}
	})

	t.Run("denies non-member", func(t *testing.T) {
		_, err := SessionAddMembers(7999, sessionID, []int64{7005}, []int16{1})
		if !errors.Is(err, ErrSessionPermissionDenied) {
			t.Fatalf("expected ErrSessionPermissionDenied, got %v", err)
		}
	})

	t.Run("allows normal member role", func(t *testing.T) {
		memberUserID := int64(7008)
		newFriendID := int64(7009)
		seedUser(t, testDB, memberUserID)
		seedUser(t, testDB, newFriendID)
		seedFriendRelation(t, testDB, memberUserID, newFriendID)
		member := model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberUserID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		}
		if err := testDB.DB.Create(&member).Error; err != nil {
			t.Fatalf("create normal member error: %v", err)
		}

		resp, err := SessionAddMembers(memberUserID, sessionID, []int64{newFriendID}, []int16{1})
		if err != nil {
			t.Fatalf("SessionAddMembers() normal member error = %v", err)
		}
		if resp.AddedCount != 1 {
			t.Fatalf("expected added_count=1, got %d", resp.AddedCount)
		}
		if resp.MemberCount != 5 {
			t.Fatalf("expected member_count=5, got %d", resp.MemberCount)
		}
	})

	t.Run("rejects normal member when member invite is disabled", func(t *testing.T) {
		disabledSessionID := "session-add-members-disabled"
		memberUserID := int64(7015)
		targetUserID := int64(7016)
		seedUser(t, testDB, memberUserID)
		seedUser(t, testDB, targetUserID)
		seedFriendRelation(t, testDB, memberUserID, targetUserID)

		disabledSession := model.Session{
			SessionID:         disabledSessionID,
			OwnerID:           ownerID,
			SessionType:       2,
			AllowMemberInvite: true,
			LastMsgSummary:    "group",
		}
		if err := testDB.DB.Create(&disabledSession).Error; err != nil {
			t.Fatalf("create disabled session error: %v", err)
		}
		if err := testDB.DB.Model(&model.Session{}).
			Where("session_id = ?", disabledSessionID).
			Update("allow_member_invite", false).Error; err != nil {
			t.Fatalf("disable member invite error: %v", err)
		}
		disabledMembers := []model.SessionMember{
			{
				SessionID:    disabledSessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    disabledSessionID,
				MemberID:     memberUserID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&disabledMembers).Error; err != nil {
			t.Fatalf("create disabled session members error: %v", err)
		}

		_, err := SessionAddMembers(memberUserID, disabledSessionID, []int64{targetUserID}, []int16{1})
		if !errors.Is(err, ErrSessionMemberInviteDisabled) {
			t.Fatalf("expected ErrSessionMemberInviteDisabled, got %v", err)
		}
	})

	t.Run("rejects normal member when group size exceeds threshold", func(t *testing.T) {
		thresholdSessionID := "session-add-members-threshold"
		memberUserID := int64(7017)
		targetUserID := int64(7018)
		seedUser(t, testDB, memberUserID)
		seedUser(t, testDB, targetUserID)
		seedUser(t, testDB, 7019)
		seedFriendRelation(t, testDB, memberUserID, targetUserID)
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

		thresholdSession := model.Session{
			SessionID:         thresholdSessionID,
			OwnerID:           ownerID,
			SessionType:       2,
			AllowMemberInvite: true,
			LastMsgSummary:    "group",
		}
		if err := testDB.DB.Create(&thresholdSession).Error; err != nil {
			t.Fatalf("create threshold session error: %v", err)
		}
		thresholdMembers := []model.SessionMember{
			{
				SessionID:    thresholdSessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    thresholdSessionID,
				MemberID:     memberUserID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    thresholdSessionID,
				MemberID:     7019,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&thresholdMembers).Error; err != nil {
			t.Fatalf("create threshold session members error: %v", err)
		}

		_, err := SessionAddMembers(memberUserID, thresholdSessionID, []int64{targetUserID}, []int16{1})
		if !errors.Is(err, ErrSessionMemberInviteThresholdReached) {
			t.Fatalf("expected ErrSessionMemberInviteThresholdReached, got %v", err)
		}
	})

	t.Run("rejects non-friend member target", func(t *testing.T) {
		seedUser(t, testDB, 7011)
		_, err := SessionAddMembers(ownerID, sessionID, []int64{7011}, []int16{1})
		if !errors.Is(err, ErrMemberNotFriend) {
			t.Fatalf("expected ErrMemberNotFriend, got %v", err)
		}
	})

	t.Run("rejects foreign agent target", func(t *testing.T) {
		seedAgent(t, testDB, 7012, 7999, 1)
		_, err := SessionAddMembers(ownerID, sessionID, []int64{7012}, []int16{2})
		if !errors.Is(err, ErrMemberAgentNotOwned) {
			t.Fatalf("expected ErrMemberAgentNotOwned, got %v", err)
		}
	})

	t.Run("rejects voice agent target", func(t *testing.T) {
		voiceAgentID := int64(7050)
		seedAgent(t, testDB, voiceAgentID, ownerID, 1)
		if err := testDB.DB.Model(&model.Agent{}).
			Where("id = ?", voiceAgentID).
			Update("media_capability", model.AgentMediaCapabilityVoice).Error; err != nil {
			t.Fatalf("mark voice agent error: %v", err)
		}
		_, err := SessionAddMembers(ownerID, sessionID, []int64{voiceAgentID}, []int16{2})
		if !errors.Is(err, ErrMemberAgentVoiceNotAllowed) {
			t.Fatalf("expected ErrMemberAgentVoiceNotAllowed, got %v", err)
		}
	})

	t.Run("rejects target user who disallows group invite", func(t *testing.T) {
		blockedUserID := int64(7022)
		seedUser(t, testDB, blockedUserID)
		seedFriendRelation(t, testDB, ownerID, blockedUserID)
		if err := testDB.DB.Create(&model.UserSetting{
			UserID:           blockedUserID,
			FriendAddSetting: model.FriendAddSettingNeedApproval,
			AllowGroupInvite: true,
		}).Error; err != nil {
			t.Fatalf("seed blocked user setting error: %v", err)
		}
		if err := testDB.DB.Model(&model.UserSetting{}).
			Where("user_id = ?", blockedUserID).
			Update("allow_group_invite", false).Error; err != nil {
			t.Fatalf("disable blocked user group invite error: %v", err)
		}

		_, err := SessionAddMembers(ownerID, sessionID, []int64{blockedUserID}, []int16{1})
		if !errors.Is(err, ErrSessionTargetGroupInviteRejected) {
			t.Fatalf("expected ErrSessionTargetGroupInviteRejected, got %v", err)
		}
	})

	t.Run("ignores existing member who later disallows group invite", func(t *testing.T) {
		existingUserID := int64(7023)
		seedUser(t, testDB, existingUserID)
		seedFriendRelation(t, testDB, ownerID, existingUserID)
		existingMember := model.SessionMember{
			SessionID:    sessionID,
			MemberID:     existingUserID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		}
		if err := testDB.DB.Create(&existingMember).Error; err != nil {
			t.Fatalf("create existing member error: %v", err)
		}
		if err := testDB.DB.Create(&model.UserSetting{
			UserID:           existingUserID,
			FriendAddSetting: model.FriendAddSettingNeedApproval,
			AllowGroupInvite: true,
		}).Error; err != nil {
			t.Fatalf("seed existing member setting error: %v", err)
		}
		if err := testDB.DB.Model(&model.UserSetting{}).
			Where("user_id = ?", existingUserID).
			Update("allow_group_invite", false).Error; err != nil {
			t.Fatalf("disable existing member group invite error: %v", err)
		}

		resp, err := SessionAddMembers(ownerID, sessionID, []int64{existingUserID}, []int16{1})
		if err != nil {
			t.Fatalf("expected duplicate existing member add to be ignored, got %v", err)
		}
		if resp.AddedCount != 0 {
			t.Fatalf("expected added_count=0, got %d", resp.AddedCount)
		}
	})

	t.Run("rejects non-group session", func(t *testing.T) {
		privateSession := model.Session{
			SessionID:      "session-add-members-private",
			OwnerID:        ownerID,
			SessionType:    1,
			LastMsgSummary: "private",
		}
		if err := testDB.DB.Create(&privateSession).Error; err != nil {
			t.Fatalf("create private session error: %v", err)
		}
		privateMember := model.SessionMember{
			SessionID:    privateSession.SessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		}
		if err := testDB.DB.Create(&privateMember).Error; err != nil {
			t.Fatalf("create private session member error: %v", err)
		}

		_, err := SessionAddMembers(ownerID, privateSession.SessionID, []int64{7010}, []int16{1})
		if !errors.Is(err, ErrSessionInvalidType) {
			t.Fatalf("expected ErrSessionInvalidType, got %v", err)
		}
	})

	t.Run("allows same numeric id for human and agent members", func(t *testing.T) {
		sameID := int64(7099)
		seedUser(t, testDB, sameID)
		seedFriendRelation(t, testDB, ownerID, sameID)
		seedAgent(t, testDB, sameID, ownerID, 1)

		resp, err := SessionAddMembers(
			ownerID,
			sessionID,
			[]int64{sameID, sameID},
			[]int16{1, 2},
		)
		if err != nil {
			t.Fatalf("SessionAddMembers() error = %v", err)
		}
		if resp.AddedCount != 2 {
			t.Fatalf("expected added_count=2, got %d", resp.AddedCount)
		}

		var count int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ?", sessionID, sameID).
			Count(&count).Error; err != nil {
			t.Fatalf("count same-id members error: %v", err)
		}
		if count != 2 {
			t.Fatalf("expected 2 rows for same member_id with different member_type, got %d", count)
		}
	})

	t.Run("auto starts delegate for newly added member with default agent", func(t *testing.T) {
		prevRDB := store.RDB
		mockRDB := testutil.NewMockRedis()
		store.RDB = mockRDB
		defer func() {
			store.RDB = prevRDB
			_ = mockRDB.Close()
		}()

		newMemberID := int64(7020)
		newMemberAutoAgentID := int64(7021)
		seedUser(t, testDB, newMemberID)
		seedFriendRelation(t, testDB, ownerID, newMemberID)
		seedAgent(t, testDB, newMemberAutoAgentID, newMemberID, 1)
		rawAgentID := fmt.Sprintf("%d", newMemberAutoAgentID)
		if _, err := UpdateUserSettings(newMemberID, UserSettingsUpdateReq{
			Chat: &UserSettingsChatUpdateReq{
				AutoDelegateAgentID: OptionalStringUpdate{
					Set:   true,
					Value: &rawAgentID,
				},
			},
		}); err != nil {
			t.Fatalf("set new member default auto delegate agent error: %v", err)
		}

		resp, err := SessionAddMembers(ownerID, sessionID, []int64{newMemberID}, []int16{1})
		if err != nil {
			t.Fatalf("SessionAddMembers() error = %v", err)
		}
		if resp.AddedCount != 1 {
			t.Fatalf("expected added_count=1, got %d", resp.AddedCount)
		}

		ctx := context.Background()
		delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, newMemberID)
		agentIDInRedis, err := store.RDB.HGet(ctx, delegateKey, "agent_id").Result()
		if err != nil {
			t.Fatalf("read auto delegate agent_id from redis error: %v", err)
		}
		if agentIDInRedis != fmt.Sprintf("%d", newMemberAutoAgentID) {
			t.Fatalf("unexpected auto delegate agent_id, got=%s want=%d", agentIDInRedis, newMemberAutoAgentID)
		}

		maxRounds, err := store.RDB.HGet(ctx, delegateKey, "max_consecutive_replies").Int()
		if err != nil {
			t.Fatalf("read auto delegate max_consecutive_replies from redis error: %v", err)
		}
		if maxRounds != autoDelegateDefaultMaxConsecutiveReplies {
			t.Fatalf(
				"unexpected auto delegate max_consecutive_replies, got=%d want=%d",
				maxRounds,
				autoDelegateDefaultMaxConsecutiveReplies,
			)
		}

		var log model.DelegationLog
		if err := store.DB.
			Where("session_id = ? AND user_id = ? AND action = ?", sessionID, newMemberID, "auto_start").
			First(&log).Error; err != nil {
			t.Fatalf("query auto_start delegation log error: %v", err)
		}
		if log.AgentID != newMemberAutoAgentID {
			t.Fatalf("delegation log agent_id=%d want=%d", log.AgentID, newMemberAutoAgentID)
		}
	})
}

func TestSessionRemoveMembers(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(7101)
	adminID := int64(7102)
	normalID := int64(7103)
	agentID := int64(7104)
	sessionID := "session-remove-members-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     adminID,
			MemberType:   1,
			Role:         2,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     normalID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     agentID,
			MemberType:   2,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("owner can remove normal member and agent", func(t *testing.T) {
		resp, err := SessionRemoveMembers(
			ownerID,
			sessionID,
			[]int64{normalID, agentID},
			[]int16{1, 2},
		)
		if err != nil {
			t.Fatalf("SessionRemoveMembers() error = %v", err)
		}
		if resp.RemovedCount != 2 {
			t.Fatalf("expected removed_count=2, got %d", resp.RemovedCount)
		}
		if resp.MemberCount != 2 {
			t.Fatalf("expected member_count=2, got %d", resp.MemberCount)
		}
	})

	t.Run("admin cannot remove owner", func(t *testing.T) {
		_, err := SessionRemoveMembers(adminID, sessionID, []int64{ownerID}, []int16{1})
		if !errors.Is(err, ErrSessionCannotRemoveOwner) {
			t.Fatalf("expected ErrSessionCannotRemoveOwner, got %v", err)
		}
	})

	t.Run("admin cannot remove admin", func(t *testing.T) {
		_, err := SessionRemoveMembers(adminID, sessionID, []int64{adminID}, []int16{1})
		if !errors.Is(err, ErrSessionCannotOperateSelf) {
			t.Fatalf("expected ErrSessionCannotOperateSelf, got %v", err)
		}

		// add another admin and verify admin-to-admin is denied
		admin2 := model.SessionMember{
			SessionID:    sessionID,
			MemberID:     7199,
			MemberType:   1,
			Role:         2,
			LastActiveAt: now,
			JoinedAt:     now,
		}
		if err := testDB.DB.Create(&admin2).Error; err != nil {
			t.Fatalf("create admin2 error: %v", err)
		}
		_, err = SessionRemoveMembers(adminID, sessionID, []int64{7199}, []int16{1})
		if !errors.Is(err, ErrSessionRemoveDenied) {
			t.Fatalf("expected ErrSessionRemoveDenied, got %v", err)
		}
	})
}

func TestSessionLeave(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(7141)
	adminID := int64(7142)
	memberID := int64(7143)
	sessionID := "session-leave-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     adminID,
			MemberType:   1,
			Role:         2,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, memberID)
	streakKey := fmt.Sprintf("im:delegate:streak:%s:%d", sessionID, memberID)
	if err := store.RDB.HSet(context.Background(), delegateKey, "agent_id", 9201).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}
	if err := store.RDB.Set(context.Background(), streakKey, "3", time.Minute).Err(); err != nil {
		t.Fatalf("seed delegate streak error: %v", err)
	}

	t.Run("normal member can leave group", func(t *testing.T) {
		resp, err := SessionLeave(memberID, sessionID)
		if err != nil {
			t.Fatalf("SessionLeave() error = %v", err)
		}
		if !resp.Left {
			t.Fatalf("expected left=true, got false")
		}

		var count int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, memberID).
			Count(&count).Error; err != nil {
			t.Fatalf("count left member error: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected member to be removed, got count=%d", count)
		}

		exists, err := store.RDB.Exists(context.Background(), delegateKey, streakKey).Result()
		if err != nil {
			t.Fatalf("check delegate state error: %v", err)
		}
		if exists != 0 {
			t.Fatalf("expected delegate state cleared, exists=%d", exists)
		}
	})

	t.Run("repeat leave returns false", func(t *testing.T) {
		resp, err := SessionLeave(memberID, sessionID)
		if err != nil {
			t.Fatalf("SessionLeave() repeat error = %v", err)
		}
		if resp.Left {
			t.Fatalf("expected left=false on repeat leave")
		}
	})

	t.Run("admin can leave group", func(t *testing.T) {
		resp, err := SessionLeave(adminID, sessionID)
		if err != nil {
			t.Fatalf("SessionLeave() admin error = %v", err)
		}
		if !resp.Left {
			t.Fatalf("expected admin leave success")
		}
	})

	t.Run("owner cannot leave group", func(t *testing.T) {
		_, err := SessionLeave(ownerID, sessionID)
		if !errors.Is(err, ErrSessionOwnerCannotLeave) {
			t.Fatalf("expected ErrSessionOwnerCannotLeave, got %v", err)
		}
	})
}

func TestSessionLeaveByAgent(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(7151)
	agentID := int64(7152)
	sessionID := "session-leave-agent-group"
	now := time.Now()

	groupSession := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&groupSession).Error; err != nil {
		t.Fatalf("create group session error: %v", err)
	}
	groupMembers := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     agentID,
			MemberType:   2,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&groupMembers).Error; err != nil {
		t.Fatalf("create group members error: %v", err)
	}

	directSession := model.Session{
		SessionID:      "session-leave-agent-direct",
		OwnerID:        ownerID,
		SessionType:    1,
		LastMsgSummary: "direct",
	}
	if err := testDB.DB.Create(&directSession).Error; err != nil {
		t.Fatalf("create direct session error: %v", err)
	}
	directMembers := []model.SessionMember{
		{
			SessionID:    directSession.SessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    directSession.SessionID,
			MemberID:     agentID,
			MemberType:   2,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&directMembers).Error; err != nil {
		t.Fatalf("create direct members error: %v", err)
	}

	t.Run("agent member can leave group", func(t *testing.T) {
		resp, err := SessionLeaveByAgent(agentID, ownerID, sessionID)
		if err != nil {
			t.Fatalf("SessionLeaveByAgent() error = %v", err)
		}
		if !resp.Left {
			t.Fatalf("expected left=true")
		}
		if resp.SessionID != sessionID {
			t.Fatalf("expected session_id=%s, got %s", sessionID, resp.SessionID)
		}

		var memberCount int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 2", sessionID, agentID).
			Count(&memberCount).Error; err != nil {
			t.Fatalf("count leave membership error: %v", err)
		}
		if memberCount != 0 {
			t.Fatalf("expected agent membership removed, got %d", memberCount)
		}
	})

	t.Run("repeat leave returns false", func(t *testing.T) {
		resp, err := SessionLeaveByAgent(agentID, ownerID, sessionID)
		if err != nil {
			t.Fatalf("SessionLeaveByAgent() repeat error = %v", err)
		}
		if resp.Left {
			t.Fatalf("expected left=false on repeated leave")
		}
	})

	t.Run("owner's agent can leave group", func(t *testing.T) {
		ownerAgentID := int64(7153)
		ownerSessionID := "session-leave-agent-owner-owned"
		seedUser(t, testDB, ownerID)
		seedAgent(t, testDB, ownerAgentID, ownerID, 1)

		ownerSession := model.Session{
			SessionID:      ownerSessionID,
			OwnerID:        ownerID,
			SessionType:    2,
			LastMsgSummary: "group",
		}
		if err := testDB.DB.Create(&ownerSession).Error; err != nil {
			t.Fatalf("create owner group session error: %v", err)
		}
		ownerMembers := []model.SessionMember{
			{
				SessionID:    ownerSessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    ownerSessionID,
				MemberID:     ownerAgentID,
				MemberType:   2,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&ownerMembers).Error; err != nil {
			t.Fatalf("create owner group members error: %v", err)
		}

		resp, err := SessionLeaveByAgent(ownerAgentID, ownerID, ownerSessionID)
		if err != nil {
			t.Fatalf("SessionLeaveByAgent() owner agent leave error = %v", err)
		}
		if !resp.Left {
			t.Fatalf("expected left=true for owner's agent")
		}
	})

	t.Run("delegated agent leave removes delegated user and agent member", func(t *testing.T) {
		groupOwnerID := int64(7154)
		delegatedUserID := int64(7155)
		delegatedAgentID := int64(7156)
		delegatedSessionID := "session-leave-agent-delegated-user"

		seedAgent(t, testDB, delegatedAgentID, delegatedUserID, 1)

		delegatedSession := model.Session{
			SessionID:      delegatedSessionID,
			OwnerID:        groupOwnerID,
			SessionType:    2,
			LastMsgSummary: "group",
		}
		if err := testDB.DB.Create(&delegatedSession).Error; err != nil {
			t.Fatalf("create delegated group session error: %v", err)
		}
		delegatedMembers := []model.SessionMember{
			{
				SessionID:    delegatedSessionID,
				MemberID:     groupOwnerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    delegatedSessionID,
				MemberID:     delegatedUserID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    delegatedSessionID,
				MemberID:     delegatedAgentID,
				MemberType:   2,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&delegatedMembers).Error; err != nil {
			t.Fatalf("create delegated group members error: %v", err)
		}

		delegateKey := fmt.Sprintf("im:delegate:%s:%d", delegatedSessionID, delegatedUserID)
		if err := store.RDB.HSet(context.Background(), delegateKey, "agent_id", delegatedAgentID).Err(); err != nil {
			t.Fatalf("seed delegated leave key error: %v", err)
		}

		resp, err := SessionLeaveByAgent(delegatedAgentID, delegatedUserID, delegatedSessionID)
		if err != nil {
			t.Fatalf("SessionLeaveByAgent() delegated leave error = %v", err)
		}
		if !resp.Left {
			t.Fatalf("expected left=true for delegated user leave")
		}

		var delegatedUserCount int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 1", delegatedSessionID, delegatedUserID).
			Count(&delegatedUserCount).Error; err != nil {
			t.Fatalf("count delegated user membership error: %v", err)
		}
		if delegatedUserCount != 0 {
			t.Fatalf("expected delegated user removed, got %d", delegatedUserCount)
		}

		var delegatedAgentCount int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 2", delegatedSessionID, delegatedAgentID).
			Count(&delegatedAgentCount).Error; err != nil {
			t.Fatalf("count delegated agent membership error: %v", err)
		}
		if delegatedAgentCount != 0 {
			t.Fatalf("expected delegated agent removed, got %d", delegatedAgentCount)
		}

		if exists, err := store.RDB.Exists(context.Background(), delegateKey).Result(); err != nil {
			t.Fatalf("check delegated leave key error: %v", err)
		} else if exists != 0 {
			t.Fatalf("expected delegated leave key removed, got exists=%d", exists)
		}
	})

	t.Run("non-member leave returns false", func(t *testing.T) {
		anotherSession := model.Session{
			SessionID:      "session-leave-agent-not-member",
			OwnerID:        ownerID,
			SessionType:    2,
			LastMsgSummary: "group",
		}
		if err := testDB.DB.Create(&anotherSession).Error; err != nil {
			t.Fatalf("create another group session error: %v", err)
		}
		anotherMembers := []model.SessionMember{
			{
				SessionID:    anotherSession.SessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&anotherMembers).Error; err != nil {
			t.Fatalf("create another group members error: %v", err)
		}

		resp, err := SessionLeaveByAgent(agentID, ownerID, anotherSession.SessionID)
		if err != nil {
			t.Fatalf("SessionLeaveByAgent() non-member error = %v", err)
		}
		if resp.Left {
			t.Fatalf("expected left=false for non-member leave")
		}
	})

	t.Run("direct session is rejected", func(t *testing.T) {
		_, err := SessionLeaveByAgent(agentID, ownerID, directSession.SessionID)
		if !errors.Is(err, ErrSessionInvalidType) {
			t.Fatalf("expected ErrSessionInvalidType, got %v", err)
		}
	})
}

func TestSessionUpdateMemberRole(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(7201)
	adminID := int64(7202)
	memberID := int64(7203)
	sessionID := "session-update-role-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     adminID,
			MemberType:   1,
			Role:         2,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("owner can promote normal member to admin", func(t *testing.T) {
		resp, err := SessionUpdateMemberRole(ownerID, sessionID, memberID, 1, 2)
		if err != nil {
			t.Fatalf("SessionUpdateMemberRole() error = %v", err)
		}
		if resp.Role != 2 {
			t.Fatalf("expected role=2, got %d", resp.Role)
		}
	})

	t.Run("owner can demote admin to normal", func(t *testing.T) {
		resp, err := SessionUpdateMemberRole(ownerID, sessionID, adminID, 1, 1)
		if err != nil {
			t.Fatalf("SessionUpdateMemberRole() error = %v", err)
		}
		if resp.Role != 1 {
			t.Fatalf("expected role=1, got %d", resp.Role)
		}
	})

	t.Run("admin cannot update role", func(t *testing.T) {
		_, err := SessionUpdateMemberRole(adminID, sessionID, memberID, 1, 2)
		if !errors.Is(err, ErrSessionOwnerRequired) {
			t.Fatalf("expected ErrSessionOwnerRequired, got %v", err)
		}
	})

	t.Run("cannot update self role", func(t *testing.T) {
		_, err := SessionUpdateMemberRole(ownerID, sessionID, ownerID, 1, 2)
		if !errors.Is(err, ErrSessionCannotOperateSelf) {
			t.Fatalf("expected ErrSessionCannotOperateSelf, got %v", err)
		}
	})
}

func TestSessionTransferOwner(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(7301)
	adminID := int64(7302)
	memberID := int64(7303)
	sessionID := "session-transfer-owner-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     adminID,
			MemberType:   1,
			Role:         2,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("owner can transfer to member", func(t *testing.T) {
		resp, err := SessionTransferOwner(ownerID, sessionID, memberID)
		if err != nil {
			t.Fatalf("SessionTransferOwner() error = %v", err)
		}
		if resp.OwnerID != memberID {
			t.Fatalf("expected new owner_id=%d, got %d", memberID, resp.OwnerID)
		}

		var oldOwner model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			sessionID,
			ownerID,
		).First(&oldOwner).Error; err != nil {
			t.Fatalf("query old owner error: %v", err)
		}
		if oldOwner.Role != 2 {
			t.Fatalf("expected old owner role=2, got %d", oldOwner.Role)
		}

		var newOwner model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			sessionID,
			memberID,
		).First(&newOwner).Error; err != nil {
			t.Fatalf("query new owner error: %v", err)
		}
		if newOwner.Role != 3 {
			t.Fatalf("expected new owner role=3, got %d", newOwner.Role)
		}
	})

	t.Run("admin cannot transfer owner", func(t *testing.T) {
		_, err := SessionTransferOwner(adminID, sessionID, ownerID)
		if !errors.Is(err, ErrSessionOwnerRequired) {
			t.Fatalf("expected ErrSessionOwnerRequired, got %v", err)
		}
	})

	t.Run("cannot transfer to self", func(t *testing.T) {
		_, err := SessionTransferOwner(memberID, sessionID, memberID)
		if !errors.Is(err, ErrSessionCannotOperateSelf) {
			t.Fatalf("expected ErrSessionCannotOperateSelf, got %v", err)
		}
	})
}

func TestSessionDissolve(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(7401)
	adminID := int64(7402)
	memberID := int64(7403)
	sessionID := "session-dissolve-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     adminID,
			MemberType:   1,
			Role:         2,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("owner can dissolve group", func(t *testing.T) {
		resp, err := SessionDissolve(ownerID, sessionID)
		if err != nil {
			t.Fatalf("SessionDissolve() error = %v", err)
		}
		if resp.SessionID != sessionID {
			t.Fatalf("expected session_id=%s, got %s", sessionID, resp.SessionID)
		}

		var sess model.Session
		if err := testDB.DB.Where("session_id = ?", sessionID).First(&sess).Error; err != nil {
			t.Fatalf("query session error: %v", err)
		}
		if !sess.IsDeleted {
			t.Fatalf("expected session is_deleted=true")
		}

		var count int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ?", sessionID).
			Count(&count).Error; err != nil {
			t.Fatalf("count members error: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected no session members after dissolve, got %d", count)
		}

		var sysMsg model.Message
		if err := testDB.DB.Where("session_id = ?", sessionID).
			Order("created_at DESC").
			First(&sysMsg).Error; err != nil {
			t.Fatalf("query dissolve system message error: %v", err)
		}
		if sysMsg.MsgType != 3 {
			t.Fatalf("expected msg_type=3, got %d", sysMsg.MsgType)
		}
		if sysMsg.SenderType != 3 {
			t.Fatalf("expected sender_type=3, got %d", sysMsg.SenderType)
		}
		if sysMsg.Content == "" {
			t.Fatalf("expected non-empty dissolve system message content")
		}
		if sess.LastMsgID == nil {
			t.Fatalf("expected session last_msg_id not nil")
		}
		if *sess.LastMsgID != sysMsg.MsgID {
			t.Fatalf("expected session last_msg_id=%d, got %d", sysMsg.MsgID, *sess.LastMsgID)
		}
		if sess.LastMsgSummary == "" {
			t.Fatalf("expected non-empty session last_msg_summary")
		}
	})

	t.Run("non-owner cannot dissolve group", func(t *testing.T) {
		session2 := model.Session{
			SessionID:      "session-dissolve-2",
			OwnerID:        ownerID,
			SessionType:    2,
			LastMsgSummary: "group2",
		}
		if err := testDB.DB.Create(&session2).Error; err != nil {
			t.Fatalf("create session2 error: %v", err)
		}
		members2 := []model.SessionMember{
			{
				SessionID:    session2.SessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    session2.SessionID,
				MemberID:     adminID,
				MemberType:   1,
				Role:         2,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&members2).Error; err != nil {
			t.Fatalf("create members2 error: %v", err)
		}

		_, err := SessionDissolve(adminID, session2.SessionID)
		if !errors.Is(err, ErrSessionDissolveDenied) {
			t.Fatalf("expected ErrSessionDissolveDenied, got %v", err)
		}
	})
}

func TestSessionDetail(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(6001)
	memberID := int64(6002)
	sessionID := "session-detail-1"
	now := time.Now()

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, memberID)
	seedFriendRelationWithRemark(t, testDB, ownerID, memberID, "详情备注")

	session := model.Session{
		SessionID:         sessionID,
		OwnerID:           ownerID,
		SessionType:       2,
		GroupName:         "详情测试群",
		AllowMemberInvite: true,
		LastMsgSummary:    "detail",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := testDB.DB.Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		Update("allow_member_invite", false).Error; err != nil {
		t.Fatalf("disable member invite error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:     sessionID,
			MemberID:      ownerID,
			MemberType:    1,
			GroupNickname: "OwnerGroupNick",
			Role:          3,
			LastReadMsgID: 501,
			LastActiveAt:  now,
			JoinedAt:      now,
		},
		{
			SessionID:     sessionID,
			MemberID:      memberID,
			MemberType:    1,
			GroupNickname: "MemberGroupNick",
			Role:          1,
			LastReadMsgID: 499,
			LastActiveAt:  now,
			JoinedAt:      now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("returns detail for session member", func(t *testing.T) {
		resp, err := SessionDetail(ownerID, sessionID)
		if err != nil {
			t.Fatalf("SessionDetail() error = %v", err)
		}
		if resp.SessionID != sessionID {
			t.Fatalf("expected session_id %s, got %s", sessionID, resp.SessionID)
		}
		if resp.SessionType != 2 {
			t.Fatalf("expected session_type=2, got %d", resp.SessionType)
		}
		if resp.GroupName != "详情测试群" {
			t.Fatalf("expected group_name=%q, got %q", "详情测试群", resp.GroupName)
		}
		if resp.MemberCount != 2 {
			t.Fatalf("expected member_count=2, got %d", resp.MemberCount)
		}
		if resp.AllowMemberInvite {
			t.Fatal("expected allow_member_invite=false")
		}
		if resp.MemberInviteThreshold != 20 {
			t.Fatalf("expected member_invite_threshold=20, got %d", resp.MemberInviteThreshold)
		}
		if resp.Members[0].LastReadMsgID != 501 {
			t.Fatalf("expected owner last_read_msg_id=501, got %d", resp.Members[0].LastReadMsgID)
		}
		if resp.Members[1].LastReadMsgID != 499 {
			t.Fatalf("expected member last_read_msg_id=499, got %d", resp.Members[1].LastReadMsgID)
		}
		nicknameByMemberID := make(map[int64]string, len(resp.Members))
		for _, item := range resp.Members {
			nicknameByMemberID[item.MemberID] = item.Nickname
		}
		if nicknameByMemberID[memberID] != "详情备注" {
			t.Fatalf("expected member nickname to use friend remark, got %q", nicknameByMemberID[memberID])
		}
		if nicknameByMemberID[ownerID] != "OwnerGroupNick" {
			t.Fatalf("expected owner nickname to use group nickname, got %q", nicknameByMemberID[ownerID])
		}
	})

	t.Run("denies non-member", func(t *testing.T) {
		_, err := SessionDetail(9999, sessionID)
		if !errors.Is(err, ErrSessionPermissionDenied) {
			t.Fatalf("expected ErrSessionPermissionDenied, got %v", err)
		}
	})
}

func TestSessionGroupDetailByAgent(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		ownerID      = int64(6101)
		agentID      = int64(6102)
		groupOwnerID = int64(6103)
		memberID     = int64(6104)
	)
	now := time.Now()

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, groupOwnerID)
	seedUser(t, testDB, memberID)
	seedFriendRelationWithRemark(t, testDB, ownerID, memberID, "Agent看到的备注")
	seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)

	t.Run("allows agent member when owner is not in group", func(t *testing.T) {
		const sessionID = "session-group-detail-agent-member"
		session := model.Session{
			SessionID:         sessionID,
			OwnerID:           groupOwnerID,
			SessionType:       model.SessionTypeGroup,
			GroupName:         "agent-member-group",
			AllowMemberInvite: true,
			LastMsgSummary:    "detail",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create agent-member session error: %v", err)
		}
		members := []model.SessionMember{
			{SessionID: sessionID, MemberID: groupOwnerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
			{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, GroupNickname: "GroupNick", LastActiveAt: now, JoinedAt: now},
			{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := testDB.DB.Create(&members).Error; err != nil {
			t.Fatalf("create agent-member members error: %v", err)
		}

		resp, err := SessionGroupDetail(agentID, ownerID, sessionID)
		if err != nil {
			t.Fatalf("SessionGroupDetail(agent member) error = %v", err)
		}
		if resp.GroupName != "agent-member-group" {
			t.Fatalf("expected group_name=%q, got %q", "agent-member-group", resp.GroupName)
		}
		if resp.MemberCount != 3 {
			t.Fatalf("expected member_count=3, got %d", resp.MemberCount)
		}
		nicknameByMemberID := make(map[int64]string, len(resp.Members))
		for _, item := range resp.Members {
			nicknameByMemberID[item.MemberID] = item.Nickname
		}
		if nicknameByMemberID[memberID] != "Agent看到的备注" {
			t.Fatalf("expected friend remark for member, got %q", nicknameByMemberID[memberID])
		}
	})

	t.Run("allows delegated owner in group when agent itself is absent", func(t *testing.T) {
		const sessionID = "session-group-detail-delegated-owner"
		session := model.Session{
			SessionID:         sessionID,
			OwnerID:           groupOwnerID,
			SessionType:       model.SessionTypeGroup,
			GroupName:         "delegated-owner-group",
			AllowMemberInvite: true,
			LastMsgSummary:    "detail",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create delegated-owner session error: %v", err)
		}
		members := []model.SessionMember{
			{SessionID: sessionID, MemberID: groupOwnerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
			{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := testDB.DB.Create(&members).Error; err != nil {
			t.Fatalf("create delegated-owner members error: %v", err)
		}
		if err := store.RDB.HSet(context.Background(), "im:delegate:"+sessionID+":6101", "agent_id", agentID).Err(); err != nil {
			t.Fatalf("seed delegated owner detail key error: %v", err)
		}

		resp, err := SessionGroupDetail(agentID, ownerID, sessionID)
		if err != nil {
			t.Fatalf("SessionGroupDetail(delegated owner) error = %v", err)
		}
		if resp.GroupName != "delegated-owner-group" {
			t.Fatalf("expected group_name=%q, got %q", "delegated-owner-group", resp.GroupName)
		}
		if resp.MemberCount != 2 {
			t.Fatalf("expected member_count=2, got %d", resp.MemberCount)
		}
	})

	t.Run("denies when neither agent nor delegated owner is in group", func(t *testing.T) {
		const sessionID = "session-group-detail-denied"
		session := model.Session{
			SessionID:         sessionID,
			OwnerID:           groupOwnerID,
			SessionType:       model.SessionTypeGroup,
			GroupName:         "denied-group",
			AllowMemberInvite: true,
			LastMsgSummary:    "detail",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create denied detail session error: %v", err)
		}
		members := []model.SessionMember{
			{SessionID: sessionID, MemberID: groupOwnerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		}
		if err := testDB.DB.Create(&members).Error; err != nil {
			t.Fatalf("create denied detail members error: %v", err)
		}

		_, err := SessionGroupDetail(agentID, ownerID, sessionID)
		if !errors.Is(err, ErrSessionPermissionDenied) {
			t.Fatalf("expected ErrSessionPermissionDenied, got %v", err)
		}
	})
}

func TestSessionDetailRejectsBannedGroup(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	const (
		userID    = int64(19501)
		sessionID = "group-detail-banned"
	)
	now := time.Now()
	if err := testDB.DB.Create(&model.Session{
		SessionID:         sessionID,
		OwnerID:           userID,
		SessionType:       model.SessionTypeGroup,
		GroupName:         "Banned Group",
		ModerationStatus:  model.SessionModerationStatusBanned,
		AllowMemberInvite: true,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID: sessionID, MemberID: userID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("create session member: %v", err)
	}

	_, err := SessionDetail(userID, sessionID)
	if !errors.Is(err, ErrSessionGroupBanned) {
		t.Fatalf("expected ErrSessionGroupBanned, got %v", err)
	}
}

func TestSessionRename(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(8601)
	peerID := int64(8602)
	sessionID := "session-rename-1"
	now := time.Now()

	seedUser(t, testDB, userID)
	seedUser(t, testDB, peerID)

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    1,
		LastMsgSummary: "rename",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     userID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     peerID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("rename success and trim whitespace", func(t *testing.T) {
		resp, err := SessionRename(userID, sessionID, "  Topic   Alpha  ")
		if err != nil {
			t.Fatalf("SessionRename() error = %v", err)
		}
		if resp.SessionID != sessionID {
			t.Fatalf("expected session_id=%s, got %s", sessionID, resp.SessionID)
		}
		if resp.Title != "Topic Alpha" {
			t.Fatalf("expected normalized title, got %q", resp.Title)
		}

		var rows []model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_type = 1",
			sessionID,
		).Order("member_id ASC").Find(&rows).Error; err != nil {
			t.Fatalf("query members error: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("expected 2 human members, got %d", len(rows))
		}
		for _, row := range rows {
			if row.CustomTitle != "Topic Alpha" {
				t.Fatalf("expected custom_title persisted for all users, got member=%d title=%q", row.MemberID, row.CustomTitle)
			}
		}
	})

	t.Run("clear title allowed", func(t *testing.T) {
		resp, err := SessionRename(userID, sessionID, "   ")
		if err != nil {
			t.Fatalf("SessionRename() clear error = %v", err)
		}
		if resp.Title != "" {
			t.Fatalf("expected empty title after clear, got %q", resp.Title)
		}

		var rows []model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_type = 1",
			sessionID,
		).Order("member_id ASC").Find(&rows).Error; err != nil {
			t.Fatalf("query members error: %v", err)
		}
		for _, row := range rows {
			if row.CustomTitle != "" {
				t.Fatalf("expected cleared custom_title for all users, got member=%d title=%q", row.MemberID, row.CustomTitle)
			}
		}
	})

	t.Run("rejects overly long title", func(t *testing.T) {
		tooLong := strings.Repeat("a", sessionCustomTitleMax+1)
		_, err := SessionRename(userID, sessionID, tooLong)
		if !errors.Is(err, ErrSessionTitleTooLong) {
			t.Fatalf("expected ErrSessionTitleTooLong, got %v", err)
		}
	})

	t.Run("denies non-member", func(t *testing.T) {
		_, err := SessionRename(9999, sessionID, "x")
		if !errors.Is(err, ErrSessionPermissionDenied) {
			t.Fatalf("expected ErrSessionPermissionDenied, got %v", err)
		}
	})

	t.Run("not found for deleted or missing session", func(t *testing.T) {
		_, err := SessionRename(userID, "session-missing", "x")
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound, got %v", err)
		}
	})
}

func TestSessionSetPinned(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(8601)
	peerID := int64(8602)
	sessionID := "session-pin-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    1,
		LastMsgSummary: "pin test",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     userID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     peerID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("set pinned true", func(t *testing.T) {
		resp, err := SessionSetPinned(userID, sessionID, true)
		if err != nil {
			t.Fatalf("SessionSetPinned() error = %v", err)
		}
		if resp.SessionID != sessionID {
			t.Fatalf("expected session_id=%s, got %s", sessionID, resp.SessionID)
		}
		if !resp.IsPinned {
			t.Fatalf("expected is_pinned=true")
		}
		if resp.PinnedAt <= 0 {
			t.Fatalf("expected pinned_at > 0")
		}

		var member model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			sessionID,
			userID,
		).First(&member).Error; err != nil {
			t.Fatalf("query member error: %v", err)
		}
		if !member.IsPinned {
			t.Fatalf("expected member.is_pinned=true")
		}
		if member.PinnedAt == nil || member.PinnedAt.IsZero() {
			t.Fatalf("expected member.pinned_at not nil")
		}
	})

	t.Run("set pinned false", func(t *testing.T) {
		resp, err := SessionSetPinned(userID, sessionID, false)
		if err != nil {
			t.Fatalf("SessionSetPinned() unpin error = %v", err)
		}
		if resp.IsPinned {
			t.Fatalf("expected is_pinned=false")
		}
		if resp.PinnedAt != 0 {
			t.Fatalf("expected pinned_at=0, got %d", resp.PinnedAt)
		}

		var member model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			sessionID,
			userID,
		).First(&member).Error; err != nil {
			t.Fatalf("query member error: %v", err)
		}
		if member.IsPinned {
			t.Fatalf("expected member.is_pinned=false")
		}
		if member.PinnedAt != nil {
			t.Fatalf("expected member.pinned_at=nil")
		}
	})

	t.Run("denies non-member", func(t *testing.T) {
		_, err := SessionSetPinned(9999, sessionID, true)
		if !errors.Is(err, ErrSessionPermissionDenied) {
			t.Fatalf("expected ErrSessionPermissionDenied, got %v", err)
		}
	})

	t.Run("returns not found for missing session", func(t *testing.T) {
		_, err := SessionSetPinned(userID, "session-missing", true)
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound, got %v", err)
		}
	})
}

func TestSessionSetMuted(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(8611)
	peerID := int64(8612)
	sessionID := "session-mute-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    1,
		LastMsgSummary: "mute test",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     userID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     peerID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("set muted true", func(t *testing.T) {
		resp, err := SessionSetMuted(userID, sessionID, true)
		if err != nil {
			t.Fatalf("SessionSetMuted() error = %v", err)
		}
		if resp.SessionID != sessionID {
			t.Fatalf("expected session_id=%s, got %s", sessionID, resp.SessionID)
		}
		if !resp.IsMuted {
			t.Fatalf("expected is_muted=true")
		}

		var member model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			sessionID,
			userID,
		).First(&member).Error; err != nil {
			t.Fatalf("query member error: %v", err)
		}
		if !member.IsMuted {
			t.Fatalf("expected member.is_muted=true")
		}
	})

	t.Run("set muted false", func(t *testing.T) {
		resp, err := SessionSetMuted(userID, sessionID, false)
		if err != nil {
			t.Fatalf("SessionSetMuted() error = %v", err)
		}
		if resp.IsMuted {
			t.Fatalf("expected is_muted=false")
		}

		var member model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			sessionID,
			userID,
		).First(&member).Error; err != nil {
			t.Fatalf("query member error: %v", err)
		}
		if member.IsMuted {
			t.Fatalf("expected member.is_muted=false")
		}
	})

	t.Run("denies non-member", func(t *testing.T) {
		_, err := SessionSetMuted(9999, sessionID, true)
		if !errors.Is(err, ErrSessionPermissionDenied) {
			t.Fatalf("expected ErrSessionPermissionDenied, got %v", err)
		}
	})

	t.Run("returns not found for missing session", func(t *testing.T) {
		_, err := SessionSetMuted(userID, "session-missing", true)
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound, got %v", err)
		}
	})
}

func TestSessionSetGroupNickname(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(8701)
	peerID := int64(8702)
	sessionID := "session-group-nickname-1"
	now := time.Now()

	seedUser(t, testDB, userID)
	seedUser(t, testDB, peerID)

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    2,
		LastMsgSummary: "group nickname",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:     sessionID,
			MemberID:      userID,
			MemberType:    1,
			GroupNickname: "OldMe",
			Role:          3,
			LastActiveAt:  now,
			JoinedAt:      now,
		},
		{
			SessionID:     sessionID,
			MemberID:      peerID,
			MemberType:    1,
			GroupNickname: "PeerKeep",
			Role:          1,
			LastActiveAt:  now,
			JoinedAt:      now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("set own nickname and trim whitespace", func(t *testing.T) {
		resp, err := SessionSetGroupNickname(userID, sessionID, "  Team   Lead  ")
		if err != nil {
			t.Fatalf("SessionSetGroupNickname() error = %v", err)
		}
		if resp.SessionID != sessionID {
			t.Fatalf("expected session_id=%s, got %s", sessionID, resp.SessionID)
		}
		if resp.GroupNickname != "Team Lead" {
			t.Fatalf("expected normalized group nickname Team Lead, got %q", resp.GroupNickname)
		}

		var me model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			sessionID,
			userID,
		).First(&me).Error; err != nil {
			t.Fatalf("query me error: %v", err)
		}
		if me.GroupNickname != "Team Lead" {
			t.Fatalf("expected own group_nickname updated, got %q", me.GroupNickname)
		}

		var peer model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			sessionID,
			peerID,
		).First(&peer).Error; err != nil {
			t.Fatalf("query peer error: %v", err)
		}
		if peer.GroupNickname != "PeerKeep" {
			t.Fatalf("expected peer group_nickname unchanged, got %q", peer.GroupNickname)
		}
	})

	t.Run("clear nickname allowed", func(t *testing.T) {
		resp, err := SessionSetGroupNickname(userID, sessionID, "   ")
		if err != nil {
			t.Fatalf("SessionSetGroupNickname() clear error = %v", err)
		}
		if resp.GroupNickname != "" {
			t.Fatalf("expected empty group nickname after clear, got %q", resp.GroupNickname)
		}

		var me model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_id = ? AND member_type = 1",
			sessionID,
			userID,
		).First(&me).Error; err != nil {
			t.Fatalf("query me error: %v", err)
		}
		if me.GroupNickname != "" {
			t.Fatalf("expected cleared group_nickname, got %q", me.GroupNickname)
		}
	})

	t.Run("rejects overly long nickname", func(t *testing.T) {
		tooLong := strings.Repeat("a", sessionGroupNicknameMax+1)
		_, err := SessionSetGroupNickname(userID, sessionID, tooLong)
		if !errors.Is(err, ErrSessionGroupNicknameTooLong) {
			t.Fatalf("expected ErrSessionGroupNicknameTooLong, got %v", err)
		}
	})

	t.Run("denies non-member", func(t *testing.T) {
		_, err := SessionSetGroupNickname(9999, sessionID, "x")
		if !errors.Is(err, ErrSessionPermissionDenied) {
			t.Fatalf("expected ErrSessionPermissionDenied, got %v", err)
		}
	})

	t.Run("rejects non-group session", func(t *testing.T) {
		privateSessionID := "session-group-nickname-private"
		privateSession := model.Session{
			SessionID:      privateSessionID,
			OwnerID:        userID,
			SessionType:    1,
			LastMsgSummary: "private",
		}
		if err := testDB.DB.Create(&privateSession).Error; err != nil {
			t.Fatalf("create private session error: %v", err)
		}
		privateMembers := []model.SessionMember{
			{
				SessionID:    privateSessionID,
				MemberID:     userID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    privateSessionID,
				MemberID:     peerID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&privateMembers).Error; err != nil {
			t.Fatalf("create private members error: %v", err)
		}

		_, err := SessionSetGroupNickname(userID, privateSessionID, "x")
		if !errors.Is(err, ErrSessionInvalidType) {
			t.Fatalf("expected ErrSessionInvalidType, got %v", err)
		}
	})
}
