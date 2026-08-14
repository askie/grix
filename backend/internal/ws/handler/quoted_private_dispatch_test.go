package handler

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// 引用一条无文本正文的消息（如图片）时，不再整条丢弃，而是按 msg_type 给占位，
// 让 agent 至少看到"引用了一张图片"。
func TestResolveQuotedContextMessage_EmptyContentImagePlaceholder(t *testing.T) {
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB

	const (
		sessionID = "session-quoted-image"
		ownerID   = int64(7100)
		quotedID  = int64(880100)
	)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedID,
		SessionID:  sessionID,
		SenderID:   ownerID,
		SenderType: 1,
		MsgType:    model.MsgTypeImage,
		Content:    "",
		CreatedAt:  time.Now().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted image message: %v", err)
	}

	got := ResolveQuotedContextMessage(sessionID, quotedID)
	if got == nil {
		t.Fatalf("expected a context entry for a quoted image, got nil")
	}
	if got.Content != "[引用消息]\n[图片]" {
		t.Fatalf("content=%q want quoted image placeholder", got.Content)
	}
}

// 1:1 私聊也必须把被引用消息作为结构化条目放进 context_messages，
// 这样各 agent 插件能像群聊一样从 context_messages 组装出引用原文。
func TestBuildDispatchContextMessages_PrivateIncludesQuoted(t *testing.T) {
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB

	const (
		sessionID = "session-private-quoted"
		ownerID   = int64(7001)
		quotedID  = int64(880001)
		agentID   = int64(9001)
	)
	now := time.Now()
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedID,
		SessionID:  sessionID,
		SenderID:   ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "私聊里被引用的原文",
		CreatedAt:  now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message: %v", err)
	}

	got := buildDispatchContextMessages(
		context.Background(),
		1, // sessionType = private
		sessionID,
		2, // bufferMemberType = agent
		agentID,
		ownerID, // viewerUserID
		agentreceive.DefaultBacklogCount,
		quotedID,
		agentreceive.ModeNormal,
		nil,
	)

	if len(got) != 1 {
		t.Fatalf("context_messages len=%d want=1 (%+v)", len(got), got)
	}
	if got[0].MsgID != quotedID {
		t.Fatalf("context_messages[0].msg_id=%d want=%d", got[0].MsgID, quotedID)
	}
	if got[0].SenderID != ownerID {
		t.Fatalf("context_messages[0].sender_id=%d want=%d", got[0].SenderID, ownerID)
	}
	if got[0].Content != "[引用消息]\n私聊里被引用的原文" {
		t.Fatalf("context_messages[0].content=%q want quoted-prefixed original", got[0].Content)
	}
}
