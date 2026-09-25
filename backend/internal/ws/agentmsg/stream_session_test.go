package agentmsg

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/datatypes"
)

func TestNewStreamSessionPermissionChecks(t *testing.T) {
	t.Run("missing member", func(t *testing.T) {
		cleanup := setupAgentMsgTest(t)
		defer cleanup()

		const sessionID = "sess-stream-permission-missing"
		createGroupSession(t, sessionID, 9001)

		_, err := NewStreamSession(StreamSessionConfig{
			Ctx:        context.Background(),
			SessionID:  sessionID,
			Identity:   &SenderIdentity{SenderID: 9002, SenderType: 1},
			BuilderTTL: 30 * time.Second,
		})
		if err != sessionguard.ErrSpeakForbidden {
			t.Fatalf("err=%v want=%v", err, sessionguard.ErrSpeakForbidden)
		}
	})

	t.Run("muted member", func(t *testing.T) {
		cleanup := setupAgentMsgTest(t)
		defer cleanup()

		const sessionID = "sess-stream-permission-muted"
		createGroupSession(t, sessionID, 9003)
		createSessionMember(t, model.SessionMember{
			SessionID:    sessionID,
			MemberID:     9004,
			MemberType:   1,
			IsSpeakMuted: true,
		})

		_, err := NewStreamSession(StreamSessionConfig{
			Ctx:        context.Background(),
			SessionID:  sessionID,
			Identity:   &SenderIdentity{SenderID: 9004, SenderType: 1},
			BuilderTTL: 30 * time.Second,
		})
		if err != sessionguard.ErrMemberSpeakMuted {
			t.Fatalf("err=%v want=%v", err, sessionguard.ErrMemberSpeakMuted)
		}
	})

	t.Run("banned session", func(t *testing.T) {
		cleanup := setupAgentMsgTest(t)
		defer cleanup()

		const sessionID = "sess-stream-permission-banned"
		if err := store.DB.Create(&model.Session{
			SessionID:        sessionID,
			OwnerID:          9005,
			SessionType:      model.SessionTypeGroup,
			ModerationStatus: model.SessionModerationStatusBanned,
		}).Error; err != nil {
			t.Fatalf("create banned session error: %v", err)
		}
		createSessionMember(t, model.SessionMember{
			SessionID:  sessionID,
			MemberID:   9006,
			MemberType: 1,
		})

		_, err := NewStreamSession(StreamSessionConfig{
			Ctx:        context.Background(),
			SessionID:  sessionID,
			Identity:   &SenderIdentity{SenderID: 9006, SenderType: 1},
			BuilderTTL: 30 * time.Second,
		})
		if err != sessionguard.ErrSessionBanned {
			t.Fatalf("err=%v want=%v", err, sessionguard.ErrSessionBanned)
		}
	})
}

func TestStreamSessionResumeAndForceFinishLifecycle(t *testing.T) {
	cleanup := setupAgentMsgTest(t)
	defer cleanup()

	const (
		sessionID       = "sess-stream-lifecycle"
		ownerID         = int64(9101)
		senderID        = int64(9102)
		recipientID     = int64(9103)
		quotedMessageID = int64(991001)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, ownerID, []int64{senderID, recipientID})

	identity := &SenderIdentity{
		SenderID:   senderID,
		SenderType: 1,
		ExtraFields: map[string]any{
			"agent_api_origin": true,
		},
	}

	ss, err := NewStreamSession(StreamSessionConfig{
		Ctx:             context.Background(),
		SessionID:       sessionID,
		Identity:        identity,
		QuotedMessageID: quotedMessageID,
		BuilderTTL:      30 * time.Second,
	})
	if err != nil {
		t.Fatalf("new stream session error: %v", err)
	}

	var placeholder model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", ss.MsgID(), sessionID).First(&placeholder).Error; err != nil {
		t.Fatalf("query placeholder message error: %v", err)
	}
	if placeholder.MsgType != 4 {
		t.Fatalf("placeholder msg_type=%d want=4", placeholder.MsgType)
	}
	if placeholder.Content != "" {
		t.Fatalf("placeholder content=%q want empty", placeholder.Content)
	}

	if got := ss.AppendChunkNoBC("hello"); got != 1 {
		t.Fatalf("chunk seq after first append=%d want=1", got)
	}

	resumed := ResumeStreamSession(StreamSessionConfig{
		Ctx:             context.Background(),
		SessionID:       sessionID,
		Identity:        identity,
		QuotedMessageID: quotedMessageID,
		BuilderTTL:      30 * time.Second,
		ChunkSeq:        ss.ChunkSeq(),
	}, ss.MsgID())
	if resumed.BuilderKey() != ss.BuilderKey() {
		t.Fatalf("builder key=%s want=%s", resumed.BuilderKey(), ss.BuilderKey())
	}
	if resumed.ChunkSeq() != 1 {
		t.Fatalf("resumed chunk seq=%d want=1", resumed.ChunkSeq())
	}
	if resumed.QuotedMessageID() != quotedMessageID {
		t.Fatalf("resumed quoted msg id=%d want=%d", resumed.QuotedMessageID(), quotedMessageID)
	}

	resumed.AppendChunkLua("deadbeef", " world")
	if resumed.ChunkSeq() != 2 {
		t.Fatalf("resumed chunk seq after lua append=%d want=2", resumed.ChunkSeq())
	}

	fullContent, err := resumed.ForceFinish(map[string]any{
		"extra": datatypes.JSON([]byte(`{"from":"test"}`)),
	})
	if err != nil {
		t.Fatalf("force finish error: %v", err)
	}
	if fullContent != "hello world" {
		t.Fatalf("full content=%q want=%q", fullContent, "hello world")
	}

	var finished model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", ss.MsgID(), sessionID).First(&finished).Error; err != nil {
		t.Fatalf("query finished message error: %v", err)
	}
	if finished.MsgType != 1 || finished.Content != "hello world" {
		t.Fatalf("finished message=%#v", finished)
	}
	if string(finished.Extra) != `{"from":"test"}` {
		t.Fatalf("finished extra=%s want=%s", string(finished.Extra), `{"from":"test"}`)
	}

	var session model.Session
	if err := store.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		t.Fatalf("query session error: %v", err)
	}
	if session.LastMsgID == nil || *session.LastMsgID != ss.MsgID() {
		t.Fatalf("session last_msg_id=%v want=%d", session.LastMsgID, ss.MsgID())
	}
	if session.LastMsgSummary != "hello world" {
		t.Fatalf("session summary=%q want=%q", session.LastMsgSummary, "hello world")
	}

	for _, userID := range []int64{senderID, recipientID} {
		var count int64
		if err := store.DB.Model(&model.UserInbox{}).
			Where("user_id = ? AND msg_id = ? AND session_id = ?", userID, ss.MsgID(), sessionID).
			Count(&count).Error; err != nil {
			t.Fatalf("query inbox count user=%d error: %v", userID, err)
		}
		if count != 1 {
			t.Fatalf("inbox count user=%d got=%d want=1", userID, count)
		}
	}

	var senderMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, senderID).
		First(&senderMember).Error; err != nil {
		t.Fatalf("query sender member error: %v", err)
	}
	if senderMember.UnreadCount != 0 {
		t.Fatalf("sender unread count=%d want=0", senderMember.UnreadCount)
	}

	var recipientMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, recipientID).
		First(&recipientMember).Error; err != nil {
		t.Fatalf("query recipient member error: %v", err)
	}
	if recipientMember.UnreadCount != 1 {
		t.Fatalf("recipient unread count=%d want=1", recipientMember.UnreadCount)
	}

	got, err := store.RDB.Get(context.Background(), ss.BuilderKey()).Result()
	if err == nil || got != "" {
		t.Fatalf("builder key should be deleted after finish, got=%q err=%v", got, err)
	}
}

func TestStreamSessionAppendChunkBufferedCoalesces(t *testing.T) {
	cleanup := setupAgentMsgTest(t)
	defer cleanup()

	const (
		sessionID = "sess-stream-coalesce"
		ownerID   = int64(9301)
		senderID  = int64(9302)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, ownerID, []int64{senderID})

	// 放大窗口，强制把首个之后的小增量都合并进缓冲
	origInterval, origBytes := StreamFlushInterval, StreamFlushBytes
	StreamFlushInterval = time.Hour
	StreamFlushBytes = 1 << 20
	defer func() { StreamFlushInterval, StreamFlushBytes = origInterval, origBytes }()

	ss, err := NewStreamSession(StreamSessionConfig{
		Ctx:        context.Background(),
		SessionID:  sessionID,
		Identity:   &SenderIdentity{SenderID: senderID, SenderType: 1},
		BuilderTTL: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("new stream session error: %v", err)
	}

	for _, d := range []string{"你", "好", "，", "世", "界"} {
		ss.AppendChunkBuffered("", d)
	}
	// 仅首个 chunk 立即广播（seq=1），其余 4 个仍在缓冲，未广播
	if ss.ChunkSeq() != 1 {
		t.Fatalf("chunk seq during coalescing=%d want=1 (only first chunk flushed)", ss.ChunkSeq())
	}

	full, err := ss.ForceFinish(nil)
	if err != nil {
		t.Fatalf("force finish error: %v", err)
	}
	if full != "你好，世界" {
		t.Fatalf("full content=%q want=%q", full, "你好，世界")
	}
	// finalize 必须 flush 残余缓冲 → 再广播一次，seq 变为 2，且尾部不丢
	if ss.ChunkSeq() != 2 {
		t.Fatalf("chunk seq after finish=%d want=2 (residual flushed)", ss.ChunkSeq())
	}

	var finished model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", ss.MsgID(), sessionID).First(&finished).Error; err != nil {
		t.Fatalf("query finished message error: %v", err)
	}
	if finished.MsgType != 1 || finished.Content != "你好，世界" {
		t.Fatalf("finished message=%#v", finished)
	}
}

func TestStreamSessionAppendChunkBufferedKillSwitch(t *testing.T) {
	cleanup := setupAgentMsgTest(t)
	defer cleanup()

	const (
		sessionID = "sess-stream-coalesce-off"
		ownerID   = int64(9311)
		senderID  = int64(9312)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, ownerID, []int64{senderID})

	orig := StreamFlushInterval
	StreamFlushInterval = 0 // 关闭合并 → 退回逐 chunk 行为
	defer func() { StreamFlushInterval = orig }()

	ss, err := NewStreamSession(StreamSessionConfig{
		Ctx:        context.Background(),
		SessionID:  sessionID,
		Identity:   &SenderIdentity{SenderID: senderID, SenderType: 1},
		BuilderTTL: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("new stream session error: %v", err)
	}

	for _, d := range []string{"a", "b", "c"} {
		ss.AppendChunkBuffered("", d)
	}
	// 关闭合并时每个 chunk 立即广播，seq 应等于 chunk 数
	if ss.ChunkSeq() != 3 {
		t.Fatalf("chunk seq with coalescing off=%d want=3 (per-chunk)", ss.ChunkSeq())
	}

	full, err := ss.ForceFinish(nil)
	if err != nil {
		t.Fatalf("force finish error: %v", err)
	}
	if full != "abc" {
		t.Fatalf("full content=%q want=%q", full, "abc")
	}
}

func TestStreamSessionBroadcastsIsThinkingFlag(t *testing.T) {
	cleanup := setupAgentMsgTest(t)
	defer cleanup()

	const (
		sessionID = "sess-stream-thinking-flag"
		ownerID   = int64(9401)
		senderID  = int64(9402)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, ownerID, []int64{senderID})
	seedRoute(t, senderID, map[string]string{"dev-a": "node-think"})

	t.Run("thinking stream carries is_thinking=true", func(t *testing.T) {
		sub := subscribeChannel(t, "chan:node-think")
		defer sub.Close()

		ss, err := NewStreamSession(StreamSessionConfig{
			Ctx:        context.Background(),
			SessionID:  sessionID,
			Identity:   &SenderIdentity{SenderID: senderID, SenderType: 1},
			BuilderTTL: 30 * time.Second,
			IsThinking: true,
		})
		if err != nil {
			t.Fatalf("new stream session error: %v", err)
		}
		ss.AppendChunk("正在思考")

		payload := readEnvelopeMessage(t, sub)["payload"].(map[string]any)
		if payload["is_thinking"] != true {
			t.Fatalf("is_thinking=%v want=true (payload=%v)", payload["is_thinking"], payload)
		}
	})

	t.Run("normal stream omits is_thinking", func(t *testing.T) {
		sub := subscribeChannel(t, "chan:node-think")
		defer sub.Close()

		ss, err := NewStreamSession(StreamSessionConfig{
			Ctx:        context.Background(),
			SessionID:  sessionID,
			Identity:   &SenderIdentity{SenderID: senderID, SenderType: 1},
			BuilderTTL: 30 * time.Second,
		})
		if err != nil {
			t.Fatalf("new stream session error: %v", err)
		}
		ss.AppendChunk("普通正文")

		payload := readEnvelopeMessage(t, sub)["payload"].(map[string]any)
		if _, ok := payload["is_thinking"]; ok {
			t.Fatalf("normal stream should omit is_thinking, got payload=%v", payload)
		}
	})
}

func TestStreamSessionBroadcastsQuotedMessageID(t *testing.T) {
	cleanup := setupAgentMsgTest(t)
	defer cleanup()

	const (
		sessionID = "sess-stream-quote-flag"
		ownerID   = int64(9501)
		senderID  = int64(9502)
		quotedID  = int64(991002)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, ownerID, []int64{senderID})
	seedRoute(t, senderID, map[string]string{"dev-a": "node-quote"})

	t.Run("stream with quote carries quoted_message_id", func(t *testing.T) {
		sub := subscribeChannel(t, "chan:node-quote")
		defer sub.Close()

		ss, err := NewStreamSession(StreamSessionConfig{
			Ctx:             context.Background(),
			SessionID:       sessionID,
			Identity:        &SenderIdentity{SenderID: senderID, SenderType: 1},
			BuilderTTL:      30 * time.Second,
			QuotedMessageID: quotedID,
		})
		if err != nil {
			t.Fatalf("new stream session error: %v", err)
		}
		ss.AppendChunk("引用回复正文")

		payload := readEnvelopeMessage(t, sub)["payload"].(map[string]any)
		// QuotedMessageID 以 json:",string" 序列化，前端按字符串接收。
		if got := payload["quoted_message_id"]; got != "991002" {
			t.Fatalf("quoted_message_id=%v want=\"991002\" (payload=%v)", got, payload)
		}
	})

	t.Run("stream without quote omits quoted_message_id", func(t *testing.T) {
		sub := subscribeChannel(t, "chan:node-quote")
		defer sub.Close()

		ss, err := NewStreamSession(StreamSessionConfig{
			Ctx:        context.Background(),
			SessionID:  sessionID,
			Identity:   &SenderIdentity{SenderID: senderID, SenderType: 1},
			BuilderTTL: 30 * time.Second,
		})
		if err != nil {
			t.Fatalf("new stream session error: %v", err)
		}
		ss.AppendChunk("无引用正文")

		payload := readEnvelopeMessage(t, sub)["payload"].(map[string]any)
		if _, ok := payload["quoted_message_id"]; ok {
			t.Fatalf("stream without quote should omit quoted_message_id, got payload=%v", payload)
		}
	})
}

func TestStreamSessionFinishRepairsMarkdownBeforePersisting(t *testing.T) {
	cleanup := setupAgentMsgTest(t)
	defer cleanup()

	const (
		sessionID   = "sess-stream-markdown-repair"
		ownerID     = int64(9201)
		senderID    = int64(9202)
		recipientID = int64(9203)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, ownerID, []int64{senderID, recipientID})

	ss, err := NewStreamSession(StreamSessionConfig{
		Ctx:        context.Background(),
		SessionID:  sessionID,
		Identity:   &SenderIdentity{SenderID: senderID, SenderType: 1},
		BuilderTTL: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("new stream session error: %v", err)
	}

	ss.AppendChunkNoBC("```go\nfmt.Println(\"hi\")")

	fullContent, err := ss.ForceFinish(nil)
	if err != nil {
		t.Fatalf("force finish error: %v", err)
	}
	want := "```go\nfmt.Println(\"hi\")\n```"
	if fullContent != want {
		t.Fatalf("full content=%q want=%q", fullContent, want)
	}

	var finished model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", ss.MsgID(), sessionID).First(&finished).Error; err != nil {
		t.Fatalf("query finished message error: %v", err)
	}
	if finished.Content != want {
		t.Fatalf("finished content=%q want=%q", finished.Content, want)
	}
}

func TestStreamSessionDeletePlaceholderBroadcastsStreamDelete(t *testing.T) {
	cleanup := setupAgentMsgTest(t)
	defer cleanup()

	const (
		sessionID = "sess-stream-delete-placeholder"
		ownerID   = int64(9601)
		senderID  = int64(9602)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, ownerID, []int64{senderID})
	seedRoute(t, senderID, map[string]string{"dev-a": "node-delete"})

	newStream := func(t *testing.T) *StreamSession {
		t.Helper()
		ss, err := NewStreamSession(StreamSessionConfig{
			Ctx:        context.Background(),
			SessionID:  sessionID,
			Identity:   &SenderIdentity{SenderID: senderID, SenderType: 1},
			BuilderTTL: 30 * time.Second,
		})
		if err != nil {
			t.Fatalf("new stream session error: %v", err)
		}
		return ss
	}

	assertPlaceholderDeleted := func(t *testing.T, msgID int64) {
		t.Helper()
		var count int64
		if err := store.DB.Model(&model.Message{}).
			Where("msg_id = ? AND session_id = ?", msgID, sessionID).
			Count(&count).Error; err != nil {
			t.Fatalf("count placeholder error: %v", err)
		}
		if count != 0 {
			t.Fatalf("placeholder still exists msg_id=%d", msgID)
		}
	}

	assertStreamDeleteEnvelope := func(t *testing.T, sub *redis.PubSub, msgID int64) {
		t.Helper()
		envelope := readEnvelopeMessage(t, sub)
		if envelope["cmd"] != "stream_delete" {
			t.Fatalf("cmd=%v want=stream_delete", envelope["cmd"])
		}
		payload, ok := envelope["payload"].(map[string]any)
		if !ok {
			t.Fatalf("payload missing or wrong type: %v", envelope["payload"])
		}
		if got := payload["msg_id"]; got != fmt.Sprintf("%d", msgID) {
			t.Fatalf("msg_id=%v want=%d", got, msgID)
		}
		if payload["session_id"] != sessionID {
			t.Fatalf("session_id=%v want=%s", payload["session_id"], sessionID)
		}
	}

	t.Run("DeletePlaceholder broadcasts stream_delete", func(t *testing.T) {
		sub := subscribeChannel(t, "chan:node-delete")
		defer sub.Close()

		ss := newStream(t)
		ss.AppendChunk(" ") // 纯空白 chunk：客户端已据此渲染占位气泡
		_ = readEnvelopeMessage(t, sub)

		ss.DeletePlaceholder()

		assertStreamDeleteEnvelope(t, sub, ss.MsgID())
		assertPlaceholderDeleted(t, ss.MsgID())
	})

	t.Run("Abort with empty content broadcasts stream_delete", func(t *testing.T) {
		sub := subscribeChannel(t, "chan:node-delete")
		defer sub.Close()

		ss := newStream(t)
		// 不写任何 chunk：builder 为空，Abort 走空内容删除分支。
		ss.Abort()

		assertStreamDeleteEnvelope(t, sub, ss.MsgID())
		assertPlaceholderDeleted(t, ss.MsgID())
	})
}
