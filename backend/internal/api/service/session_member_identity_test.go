package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupSessionMemberIdentityTest(t *testing.T) {
	t.Helper()
	testDB := testutil.NewTestDB()
	originalDB := store.DB
	originalReadDB := store.ReadDB
	store.DB = testDB.DB
	store.ReadDB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
		store.ReadDB = originalReadDB
		testDB.Close()
	})
}

func seedMembers(t *testing.T, sessionID string, members ...model.SessionMember) {
	t.Helper()
	for _, member := range members {
		member.SessionID = sessionID
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
}

func TestPrivateSessionMemberIdentitiesReturnsBothSides(t *testing.T) {
	setupSessionMemberIdentityTest(t)
	seedMembers(t, "s-private",
		model.SessionMember{MemberID: 4001, MemberType: 1},
		model.SessionMember{MemberID: 4002, MemberType: 2},
	)

	got := PrivateSessionMemberIdentities("s-private", model.SessionTypeDirect)
	if len(got) != 2 {
		t.Fatalf("expected 2 members, got=%d (%+v)", len(got), got)
	}
	if got[0].MemberID != 4001 || got[0].MemberType != 1 {
		t.Fatalf("unexpected human member: %+v", got[0])
	}
	if got[1].MemberID != 4002 || got[1].MemberType != 2 {
		t.Fatalf("unexpected agent member: %+v", got[1])
	}
}

func TestPrivateSessionMemberIdentitiesSkipsGroupAndEmptyInput(t *testing.T) {
	setupSessionMemberIdentityTest(t)
	seedMembers(t, "s-group",
		model.SessionMember{MemberID: 4101, MemberType: 1},
		model.SessionMember{MemberID: 4102, MemberType: 1},
	)

	if got := PrivateSessionMemberIdentities("s-group", 2); got != nil {
		t.Fatalf("group session must not resolve members, got=%+v", got)
	}
	if got := PrivateSessionMemberIdentities("  ", model.SessionTypeDirect); got != nil {
		t.Fatalf("blank session id must resolve to nil, got=%+v", got)
	}
	if got := PrivateSessionMemberIdentities("s-missing", model.SessionTypeDirect); got != nil {
		t.Fatalf("unknown session must resolve to nil, got=%+v", got)
	}
}

// 网站访客会话不得随消息下发成员身份：访客端本来就把访客会话渲染成合成的
// 「访客」行、不按对端归组，而接收端是匿名访客——下发等于把站点主人的用户 ID
// 告诉一个未认证的公网访客。
func TestPrivateSessionMemberIdentitiesSkipsWidgetSession(t *testing.T) {
	setupSessionMemberIdentityTest(t)
	seedMembers(t, "s-widget",
		model.SessionMember{MemberID: 4301, MemberType: 1},
		model.SessionMember{MemberID: 4302, MemberType: 1},
	)
	if err := store.DB.Create(&model.WidgetSession{
		ID:        9301,
		SessionID: "s-widget",
	}).Error; err != nil {
		t.Fatalf("create widget session error: %v", err)
	}

	if got := PrivateSessionMemberIdentities("s-widget", model.SessionTypeDirect); got != nil {
		t.Fatalf("widget session must not disclose members, got=%+v", got)
	}
	if got := PrivateSessionMemberIdentitiesBatch([]string{"s-widget"}); len(got) != 0 {
		t.Fatalf("widget session must not disclose members in batch, got=%+v", got)
	}
}

func TestPrivateSessionMemberIdentitiesBatchGroupsBySession(t *testing.T) {
	setupSessionMemberIdentityTest(t)
	seedMembers(t, "s-a",
		model.SessionMember{MemberID: 4201, MemberType: 1},
		model.SessionMember{MemberID: 4202, MemberType: 2},
	)
	seedMembers(t, "s-b",
		model.SessionMember{MemberID: 4201, MemberType: 1},
		model.SessionMember{MemberID: 4203, MemberType: 2},
	)

	// 重复与空白会话 ID 会被去重/丢弃，只发一次查询。
	got := PrivateSessionMemberIdentitiesBatch([]string{"s-a", "s-b", "s-a", " "})
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions in result, got=%d (%+v)", len(got), got)
	}
	if len(got["s-a"]) != 2 || got["s-a"][1].MemberID != 4202 {
		t.Fatalf("unexpected members for s-a: %+v", got["s-a"])
	}
	if len(got["s-b"]) != 2 || got["s-b"][1].MemberID != 4203 {
		t.Fatalf("unexpected members for s-b: %+v", got["s-b"])
	}
	if got := PrivateSessionMemberIdentitiesBatch(nil); got != nil {
		t.Fatalf("empty input must resolve to nil, got=%+v", got)
	}
}
