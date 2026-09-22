package handler

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// A connection string in the body must not be parsed as an explicit "@100"
// mention, otherwise it suppresses the implicit quoted-message-owner mention
// and the quoted agent never gets woken up.
func TestResolveGroupMentionNormalizationKeepsQuotedOwnerWithConnectionString(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID       = "session-group-mention-connection-string"
		ownerID         = int64(8201)
		senderID        = int64(8202)
		quotedOwnerID   = int64(8203)
		quotedMessageID = int64(920001)
	)

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "connection-string-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: quotedOwnerID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  sessionID,
		SenderID:   quotedOwnerID,
		SenderType: 2,
		MsgType:    1,
		Content:    "已经拿到库连接了",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	content := "[dispatch-result] 完成\npostgres://eshop:eshop@100.64.0.7:15432/eshop?sslmode=disable"
	got := resolveGroupMentionNormalization(sessionID, senderID, senderID, content, quotedMessageID, nil)

	want := []int64{quotedOwnerID}
	if !reflect.DeepEqual(got.ExplicitMentionUserIDs, want) {
		t.Fatalf("explicit_mention_user_ids = %v, want %v", got.ExplicitMentionUserIDs, want)
	}
	if !reflect.DeepEqual(got.MentionUserIDs, want) {
		t.Fatalf("mention_user_ids = %v, want %v", got.MentionUserIDs, want)
	}
}

func TestResolveGroupMentionNormalizationUnresolvedTextMentionUsesQuotedOwner(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID       = "session-group-unresolved-text-mention"
		senderID        = int64(8204)
		quotedOwnerID   = int64(8205)
		quotedMessageID = int64(920002)
	)
	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID: sessionID, OwnerID: senderID, SessionType: 2, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: quotedOwnerID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Message{
		MsgID: quotedMessageID, SessionID: sessionID, SenderID: quotedOwnerID, SenderType: 2,
		MsgType: 1, Content: "agent A 的消息", CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	got := resolveGroupMentionNormalization(
		sessionID,
		senderID,
		senderID,
		"@unknown-agent 请处理",
		quotedMessageID,
		nil,
	)
	if got.HasExplicitIndividualMentions {
		t.Fatal("unresolved text @mention must not be an explicit group target")
	}
	want := []int64{quotedOwnerID}
	if !reflect.DeepEqual(got.MentionUserIDs, want) {
		t.Fatalf("unresolved text @mention should use quoted owner: mentions=%v want=%v", got.MentionUserIDs, want)
	}
	var extra map[string]any
	if err := json.Unmarshal(got.ExtraRaw, &extra); err != nil {
		t.Fatalf("unmarshal normalized extra error: %v", err)
	}
	rawExplicit, ok := extra[explicitMentionExtraKey].([]any)
	if !ok || len(rawExplicit) != 1 || rawExplicit[0] != strconv.FormatInt(quotedOwnerID, 10) {
		t.Fatalf("explicit_mention_user_ids=%#v want quoted owner %d", extra[explicitMentionExtraKey], quotedOwnerID)
	}
}
