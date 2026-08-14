package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/agentstream"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestMergeAgentAPIExtraWithMentions(t *testing.T) {
	raw := json.RawMessage(`{"reply_mode":"mention","mention_user_ids":["1001","1001"]}`)
	merged := mergeAgentAPIExtra(raw, "@1002 hi", 9999, true)

	var payload map[string]any
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("unmarshal merged extra error: %v", err)
	}

	if payload["delegate_origin"] != true {
		t.Fatalf("delegate_origin missing or invalid: %v", payload["delegate_origin"])
	}
	if payload["agent_api_origin"] != true {
		t.Fatalf("agent_api_origin missing or invalid: %v", payload["agent_api_origin"])
	}
	if payload["agent_id"] != "9999" {
		t.Fatalf("agent_id not injected, got=%v", payload["agent_id"])
	}
	if payload["reply_mode"] != "mention" {
		t.Fatalf("reply_mode should be preserved, got=%v", payload["reply_mode"])
	}

	rawMentions, ok := payload["mention_user_ids"].([]any)
	if !ok {
		t.Fatalf("mention_user_ids should be []any, got=%T", payload["mention_user_ids"])
	}
	got := make([]int64, 0, len(rawMentions))
	for _, item := range rawMentions {
		switch v := item.(type) {
		case string:
			got = append(got, mustParseInt64(v))
		default:
			t.Fatalf("mention_user_ids contains unexpected type: %T", item)
		}
	}
	want := []int64{1001, 1002}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mention_user_ids=%v want=%v", got, want)
	}
}

func setupAgentAPIBridgeTest(t *testing.T) func() {
	t.Helper()

	logger.Init()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}

	return func() {
		// 先停内容审核 worker（跨包的全局常驻协程），再关 Redis/DB。
		service.StopContentModerationWorkers()
		_ = store.RDB.Close()
		testDB.Close()
	}
}

func seedAgentAPIBridgeAgent(t *testing.T, ownerID, agentID int64) {
	t.Helper()

	now := time.Now()
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      ownerID,
		AgentName:    "api-agent",
		ProviderType: model.AgentProviderAPI,
		Status:       1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
}

func splitTextIntoFixedChunkCount(text string, chunkCount int) []string {
	if chunkCount <= 0 {
		return nil
	}

	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	if chunkCount > len(runes) {
		chunkCount = len(runes)
	}

	baseSize := len(runes) / chunkCount
	extra := len(runes) % chunkCount
	chunks := make([]string, 0, chunkCount)
	offset := 0

	for i := 0; i < chunkCount; i++ {
		size := baseSize
		if i < extra {
			size++
		}
		next := offset + size
		chunks = append(chunks, string(runes[offset:next]))
		offset = next
	}

	return chunks
}

func openClawMixedTestContent() string {
	return `老郭，给你300字中英文混合测试数据：

---

今天是个好日子，天气晴朗，适合出去走走。The quick brown fox jumps over the lazy dog. 这句话包含了英文字母表的所有26个字母，是经典的排版测试语句。

在现代软件开发中，我们经常需要处理各种字符编码问题。UTF-8 is the most widely used character encoding on the web, supporting over 1 million unique code points. 中文字符在UTF-8中通常占用3个字节，而英文ASCII字符只占1个字节。

数字化转型正在改变各行各业。Artificial Intelligence and Machine Learning are reshaping how businesses operate globally. 从电商推荐系统到自动驾驶，AI的应用场景越来越广泛。特别是在自然语言处理NLP领域，大语言模型LLM的突破让人惊叹。

数据安全也不容忽视。Zero Trust Architecture has become a fundamental security framework for modern enterprises. 企业需要建立完善的数据治理体系，确保用户隐私得到充分保护。GDPR和CCPA等法规的实施，标志着全球对数据保护的重视。

技术迭代速度越来越快，Keep learning and stay curious！持续学习是应对变化的最好方式。💪`
}

func TestHandleAgentAPISendDelegatedSessionUsesOwnerIdentity(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-api-delegate"
		ownerID   = int64(1001)
		peerID    = int64(2002)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	for _, rel := range []model.Friend{
		{ID: time.Now().UnixNano() + ownerID + peerID, UserID: ownerID, FriendID: peerID},
		{ID: time.Now().UnixNano() + peerID + ownerID + 1, UserID: peerID, FriendID: ownerID},
	} {
		if err := store.DB.Create(&rel).Error; err != nil {
			t.Fatalf("create friend relation error: %v", err)
		}
	}
	seedAgentAPIBridgeAgent(t, ownerID, agentID)

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate error: %v", err)
	}

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	result, err := s.handleAgentAPISend(ctx, wsagentapi.SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ClientMsgID: "agent-api-send-1",
		MsgType:     1,
		Content:     "delegate reply",
	})
	if err != nil {
		t.Fatalf("handleAgentAPISend error: %v", err)
	}
	if result == nil || result.MsgID <= 0 {
		t.Fatalf("handleAgentAPISend should return valid ack, got=%#v", result)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	if msg.SenderID != ownerID {
		t.Fatalf("delegated send sender_id=%d want=%d", msg.SenderID, ownerID)
	}
	if msg.SenderType != 1 {
		t.Fatalf("delegated send sender_type=%d want=1", msg.SenderType)
	}

	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra error: %v", err)
	}
	if extra["delegate_origin"] != true {
		t.Fatalf("delegate_origin missing in extra: %#v", extra)
	}
	if extra["agent_api_origin"] != true {
		t.Fatalf("agent_api_origin missing in extra: %#v", extra)
	}
	if extra["agent_id"] != "9992" {
		t.Fatalf("agent_id=%v want=9992", extra["agent_id"])
	}
}

func TestHandleAgentAPISendTrustedDelegateModeDoesNotRequireTextDelegate(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-voice-call-segment"
		ownerID   = int64(1001)
		peerID    = int64(2002)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	for _, rel := range []model.Friend{
		{ID: time.Now().UnixNano() + ownerID + peerID, UserID: ownerID, FriendID: peerID},
		{ID: time.Now().UnixNano() + peerID + ownerID + 1, UserID: peerID, FriendID: ownerID},
	} {
		if err := store.DB.Create(&rel).Error; err != nil {
			t.Fatalf("create friend relation error: %v", err)
		}
	}

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	result, err := s.handleAgentAPISend(context.Background(), wsagentapi.SendMessageReq{
		AgentID:      agentID,
		OwnerID:      ownerID,
		IdentityMode: agentmsg.ModeDelegate,
		SessionID:    sessionID,
		ClientMsgID:  "voice-call-segment:123:1",
		MsgType:      model.MsgTypeCallSegment,
		Content:      "语音转写",
	})
	if err != nil {
		t.Fatalf("handleAgentAPISend error: %v", err)
	}
	if result == nil || result.MsgID <= 0 {
		t.Fatalf("handleAgentAPISend should return valid ack, got=%#v", result)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	if msg.SenderID != ownerID || msg.SenderType != 1 {
		t.Fatalf("sender=(%d,%d) want=(%d,1)", msg.SenderID, msg.SenderType, ownerID)
	}
	if msg.MsgType != model.MsgTypeCallSegment {
		t.Fatalf("msg_type=%d want=%d", msg.MsgType, model.MsgTypeCallSegment)
	}
}

func TestHandleAgentAPISendDirectAgentSessionUsesAgentIdentity(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-api-direct"
		ownerID   = int64(1001)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedAgentAPIBridgeAgent(t, ownerID, agentID)

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	result, err := s.handleAgentAPISend(context.Background(), wsagentapi.SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ThreadID:    "topic-a",
		ClientMsgID: "agent-api-send-2",
		MsgType:     1,
		Content:     "direct reply",
	})
	if err != nil {
		t.Fatalf("handleAgentAPISend error: %v", err)
	}
	if result == nil || result.MsgID <= 0 {
		t.Fatalf("handleAgentAPISend should return valid ack, got=%#v", result)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	if msg.SenderID != agentID {
		t.Fatalf("direct send sender_id=%d want=%d", msg.SenderID, agentID)
	}
	if msg.SenderType != 2 {
		t.Fatalf("direct send sender_type=%d want=2", msg.SenderType)
	}
	if msg.ThreadID != "topic-a" {
		t.Fatalf("direct send thread_id=%q want=topic-a", msg.ThreadID)
	}
	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra error: %v", err)
	}
	if extra["thread_id"] != "topic-a" {
		t.Fatalf("thread_id=%v want=topic-a", extra["thread_id"])
	}
}

func TestHandleAgentAPISendNoReplyStillRequiresAuthorizedIdentity(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-api-no-reply-unauthorized"
		ownerID   = int64(1001)
		peerID    = int64(2002)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedAgentAPIBridgeAgent(t, ownerID, agentID)

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime()
	result, err := s.handleAgentAPISend(context.Background(), wsagentapi.SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ClientMsgID: "agent-api-no-reply-unauthorized",
		MsgType:     1,
		Content:     "/no_reply",
	})
	if err == nil {
		t.Fatalf("expected permission error, got result=%#v", result)
	}
	var sendErr *wsagentapi.SendError
	if !errors.As(err, &sendErr) {
		t.Fatalf("error type=%T want *SendError", err)
	}
	if sendErr.Code != 4003 {
		t.Fatalf("send error code=%d want=4003", sendErr.Code)
	}
}

func TestHandleAgentAPISend_DoesNotClearExistingAgentAPIComposing(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-api-send-typing-keep"
		ownerID   = int64(1401)
		agentID   = int64(9492)
		refEvent  = "evt-send-typing-keep"
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedAgentAPIBridgeAgent(t, ownerID, agentID)

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := handler.SetSessionActivityFromAgentAPI(context.Background(), s.hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID:  sessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     true,
		RefEventID: refEvent,
	}); err != nil {
		t.Fatalf("seed composing activity error: %v", err)
	}

	before, err := handler.ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities before send error: %v", err)
	}
	if len(before) != 1 || !before[0].Active {
		t.Fatalf("expected one active composing activity before send, got=%+v", before)
	}

	result, err := s.handleAgentAPISend(context.Background(), wsagentapi.SendMessageReq{
		EventID:     refEvent,
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ClientMsgID: "agent-api-send-typing-keep",
		MsgType:     1,
		Content:     "send without ending run",
	})
	if err != nil {
		t.Fatalf("handleAgentAPISend error: %v", err)
	}
	if result == nil || result.MsgID <= 0 {
		t.Fatalf("handleAgentAPISend should return valid ack, got=%#v", result)
	}

	after, err := handler.ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities after send error: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("expected composing activity to remain after send_msg, got=%+v", after)
	}
	if !after[0].Active {
		t.Fatalf("expected composing activity to stay active after send_msg, got=%+v", after[0])
	}
	if after[0].RefEventID != refEvent {
		t.Fatalf("ref_event_id=%q want=%q", after[0].RefEventID, refEvent)
	}
}

func TestHandleAgentAPISendPersistsVisibleToInGroup(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-api-group-visible"
		ownerID   = int64(12001)
		peerID    = int64(12002)
		agentID   = int64(12999)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedAgentAPIBridgeAgent(t, ownerID, agentID)

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	_, err := s.handleAgentAPISend(context.Background(), wsagentapi.SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ClientMsgID: "agent-api-send-group-visible",
		MsgType:     1,
		Content:     "owner-visible message",
		VisibleTo:   []int64{ownerID},
	})
	if err != nil {
		t.Fatalf("handleAgentAPISend error: %v", err)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	if msg.VisibleTo == nil {
		t.Fatal("visible_to should be persisted for group message")
	}
	var visibleTo []int64
	if err := json.Unmarshal(msg.VisibleTo, &visibleTo); err != nil {
		t.Fatalf("unmarshal visible_to error: %v", err)
	}
	if len(visibleTo) != 1 || visibleTo[0] != ownerID {
		t.Fatalf("visible_to=%v want=[%d]", visibleTo, ownerID)
	}
}

func TestHandleAgentAPISendPreservesMediaURLInExtra(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-api-media"
		ownerID   = int64(1001)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedAgentAPIBridgeAgent(t, ownerID, agentID)

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	_, err := s.handleAgentAPISend(context.Background(), wsagentapi.SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ClientMsgID: "agent-api-send-media",
		MsgType:     2,
		Content:     "[media]",
		MediaURL:    "https://cdn.example.com/media/demo.png",
	})
	if err != nil {
		t.Fatalf("handleAgentAPISend error: %v", err)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra error: %v", err)
	}
	if extra["media_url"] != "https://cdn.example.com/media/demo.png" {
		t.Fatalf("extra media_url=%v want=%q", extra["media_url"], "https://cdn.example.com/media/demo.png")
	}
	attachments, ok := extra["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("attachments=%#v want=1 item", extra["attachments"])
	}
	firstAttachment, ok := attachments[0].(map[string]any)
	if !ok {
		t.Fatalf("attachment=%#v want object", attachments[0])
	}
	if firstAttachment["media_url"] != "https://cdn.example.com/media/demo.png" {
		t.Fatalf("attachment media_url=%v want=%q", firstAttachment["media_url"], "https://cdn.example.com/media/demo.png")
	}
}

func TestHandleAgentAPISendRepairsMarkdownContent(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-api-markdown-repair"
		ownerID   = int64(1101)
		agentID   = int64(9998)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedAgentAPIBridgeAgent(t, ownerID, agentID)

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	_, err := s.handleAgentAPISend(context.Background(), wsagentapi.SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ClientMsgID: "agent-api-send-markdown-repair",
		MsgType:     1,
		Content:     "```go\nfmt.Println(\"hi\")",
	})
	if err != nil {
		t.Fatalf("handleAgentAPISend error: %v", err)
	}

	var msg model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&msg).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	want := "```go\nfmt.Println(\"hi\")\n```"
	if msg.Content != want {
		t.Fatalf("message content=%q want=%q", msg.Content, want)
	}
}

func TestHandleAgentAPISendRejectsSessionWithoutDelegateOrAgentMember(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-api-forbidden"
		ownerID   = int64(1001)
		peerID    = int64(2002)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedAgentAPIBridgeAgent(t, ownerID, agentID)

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	_, err := s.handleAgentAPISend(context.Background(), wsagentapi.SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ClientMsgID: "agent-api-send-3",
		MsgType:     1,
		Content:     "should fail",
	})
	if err == nil {
		t.Fatal("handleAgentAPISend should reject session without delegate or agent membership")
	}

	sendErr, ok := err.(*wsagentapi.SendError)
	if !ok {
		t.Fatalf("expected SendError, got=%T", err)
	}
	if sendErr.Code != 4003 {
		t.Fatalf("send error code=%d want=4003", sendErr.Code)
	}
}

func TestHandleAgentAPIDeleteMsg_AllowsDirectAgentSender(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-api-delete"
		ownerID   = int64(1001)
		agentID   = int64(9992)
		msgID     = int64(18889990123)
	)

	now := time.Now().UTC()
	lastMsgID := msgID
	if err := store.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    1,
		LastMsgID:      &lastMsgID,
		LastMsgSummary: "delete me",
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		m := member
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member(%d,%d) error: %v", m.MemberID, m.MemberType, err)
		}
	}
	seedAgentAPIBridgeAgent(t, ownerID, agentID)
	if err := store.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   agentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "delete me",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIDeleteMsg(context.Background(), agentID, ownerID, wsagentapi.DeleteMsgPayload{
		SessionID: sessionID,
		MsgID:     msgID,
	}); err != nil {
		t.Fatalf("handleAgentAPIDeleteMsg error: %v", err)
	}

	var msg model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", msgID, sessionID).First(&msg).Error; err != nil {
		t.Fatalf("reload deleted message error: %v", err)
	}
	if !msg.IsDeleted {
		t.Fatal("expected direct agent message to be deleted")
	}
	if !msg.IsRevoked {
		t.Fatal("expected direct agent message to be revoked")
	}
}

func TestHandleAgentAPIReactMsg_RemoveDeletesReaction(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-api-react-remove"
		ownerID   = int64(1001)
		agentID   = int64(9992)
		msgID     = int64(18889990124)
	)

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		m := member
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedAgentAPIBridgeAgent(t, ownerID, agentID)
	if err := store.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   agentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "react to me",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}
	if err := store.DB.Create(&model.MessageReaction{
		MsgID:     msgID,
		SessionID: sessionID,
		UserID:    ownerID,
		Emoji:     "👍",
		CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create reaction error: %v", err)
	}

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIReactMsg(context.Background(), agentID, ownerID, wsagentapi.ReactMsgPayload{
		SessionID: sessionID,
		MsgID:     msgID,
		Emoji:     "👍",
		Op:        "remove",
	}); err != nil {
		t.Fatalf("handleAgentAPIReactMsg error: %v", err)
	}

	var count int64
	if err := store.DB.Model(&model.MessageReaction{}).
		Where("msg_id = ? AND session_id = ? AND user_id = ? AND emoji = ?", msgID, sessionID, ownerID, "👍").
		Count(&count).Error; err != nil {
		t.Fatalf("count reaction error: %v", err)
	}
	if count != 0 {
		t.Fatalf("reaction count=%d want=0", count)
	}
}

func TestHandleAgentAPIMediaUploadInit_UsesPresignResponse(t *testing.T) {
	original := presignAgentAPIMediaUpload
	defer func() {
		presignAgentAPIMediaUpload = original
	}()

	var gotUserID int64
	var gotFilename string
	var gotContentType string
	presignAgentAPIMediaUpload = func(userID int64, filename, contentType string) (*service.PresignResp, error) {
		gotUserID = userID
		gotFilename = filename
		gotContentType = contentType
		return &service.PresignResp{
			UploadURL:      "https://upload.example.com/demo.png",
			MediaAccessURL: "https://cdn.example.com/demo.png",
		}, nil
	}

	s := &Server{hub: NewHub("node-test")}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	result, err := s.handleAgentAPIMediaUploadInit(context.Background(), 9992, 1001, wsagentapi.MediaUploadInitPayload{
		UploadID:  "upload-1",
		Name:      "demo.png",
		SizeBytes: 123,
		Mime:      "image/png",
	})
	if err != nil {
		t.Fatalf("handleAgentAPIMediaUploadInit error: %v", err)
	}
	if gotUserID != 1001 || gotFilename != "demo.png" || gotContentType != "image/png" {
		t.Fatalf("presign args=(%d,%q,%q) want=(1001,%q,%q)", gotUserID, gotFilename, gotContentType, "demo.png", "image/png")
	}
	if result == nil {
		t.Fatal("expected upload init result")
	}
	if result.UploadID != "upload-1" || result.UploadURL != "https://upload.example.com/demo.png" || result.MediaURL != "https://cdn.example.com/demo.png" {
		t.Fatalf("result=%#v want upload response fields", result)
	}
	if result.Method != "PUT" {
		t.Fatalf("method=%q want=PUT", result.Method)
	}
}

func TestHandleAgentAPIStreamChunk_PersistsQuotedMessageID(t *testing.T) {
	logger.Init()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}
	defer func() {
		_ = store.RDB.Close()
		testDB.Close()
	}()

	const (
		sessionID             = "g_stream_reply"
		ownerID         int64 = 1001
		peerID          int64 = 2003
		agentID         int64 = 9992
		quotedMessageID int64 = 18889990222
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-group",
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:  sessionID,
			MemberID:   memberID,
			MemberType: 1,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:ws:route:2003", "dev-1", "node-test").Err(); err != nil {
		t.Fatalf("seed route redis error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-test")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	first := wsagentapi.AgentStreamChunkPayload{
		SessionID:       sessionID,
		ThreadID:        "topic-stream-a",
		ClientMsgID:     "stream-1",
		DeltaContent:    "hello",
		ChunkSeq:        1,
		QuotedMessageID: quotedMessageID,
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, first); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}
	second := wsagentapi.AgentStreamChunkPayload{
		SessionID:       sessionID,
		ClientMsgID:     "stream-1",
		DeltaContent:    " world",
		ChunkSeq:        2,
		QuotedMessageID: quotedMessageID,
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, second); err != nil {
		t.Fatalf("second stream chunk error: %v", err)
	}

	var placeholder model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&placeholder).Error; err != nil {
		t.Fatalf("load placeholder error: %v", err)
	}
	if placeholder.QuotedMessageID != quotedMessageID {
		t.Fatalf("placeholder quoted_message_id=%d want=%d", placeholder.QuotedMessageID, quotedMessageID)
	}
	if placeholder.ThreadID != "topic-stream-a" {
		t.Fatalf("placeholder thread_id=%q want=topic-stream-a", placeholder.ThreadID)
	}
	if placeholder.MsgType != 4 {
		t.Fatalf("placeholder msg_type=%d want=4", placeholder.MsgType)
	}

	finish := wsagentapi.AgentStreamChunkPayload{
		SessionID:       sessionID,
		ClientMsgID:     "stream-1",
		ChunkSeq:        3,
		IsFinish:        true,
		QuotedMessageID: quotedMessageID,
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, finish); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	var saved model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", placeholder.MsgID, sessionID).First(&saved).Error; err != nil {
		t.Fatalf("load final message error: %v", err)
	}
	if saved.QuotedMessageID != quotedMessageID {
		t.Fatalf("saved quoted_message_id=%d want=%d", saved.QuotedMessageID, quotedMessageID)
	}
	if saved.ThreadID != "topic-stream-a" {
		t.Fatalf("saved thread_id=%q want=topic-stream-a", saved.ThreadID)
	}
	if saved.MsgType != 1 {
		t.Fatalf("saved msg_type=%d want=1", saved.MsgType)
	}
	if saved.Content != "hello world" {
		t.Fatalf("saved content=%q want=%q", saved.Content, "hello world")
	}

	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				UserID  int64           `json:"user_id"`
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal redis envelope error: %v", err)
			}
			if envelope.Cmd != protocol.CmdStreamFinish {
				continue
			}
			var finishPayload protocol.StreamFinishPayload
			if err := json.Unmarshal(envelope.Payload, &finishPayload); err != nil {
				t.Fatalf("unmarshal stream_finish payload error: %v", err)
			}
			if finishPayload.QuotedMessageID != quotedMessageID {
				t.Fatalf("stream_finish quoted_message_id=%d want=%d", finishPayload.QuotedMessageID, quotedMessageID)
			}
			if finishPayload.ThreadID != "topic-stream-a" {
				t.Fatalf("stream_finish thread_id=%q want=topic-stream-a", finishPayload.ThreadID)
			}
			if finishPayload.MsgID != placeholder.MsgID {
				t.Fatalf("stream_finish msg_id=%d want=%d", finishPayload.MsgID, placeholder.MsgID)
			}
			return
		case <-timeout:
			t.Fatal("timed out waiting for stream_finish publication")
		}
	}
}

func TestHandleAgentAPIStreamChunkNoReplyDeletesExistingPlaceholder(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID   = "g_stream_no_reply_cleanup"
		ownerID     = int64(1001)
		peerID      = int64(2003)
		agentID     = int64(9992)
		clientMsgID = "stream-no-reply-cleanup"
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-no-reply-group",
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:  sessionID,
			MemberID:   memberID,
			MemberType: 1,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}

	s := &Server{agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime()
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  clientMsgID,
		DeltaContent: "partial",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}

	var beforeCount int64
	if err := store.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&beforeCount).Error; err != nil {
		t.Fatalf("count messages before no_reply error: %v", err)
	}
	if beforeCount != 1 {
		t.Fatalf("message count before no_reply=%d want=1", beforeCount)
	}

	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  clientMsgID,
		DeltaContent: "/no_reply",
		ChunkSeq:     2,
	}); err != nil {
		t.Fatalf("no_reply stream chunk error: %v", err)
	}

	var afterCount int64
	if err := store.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&afterCount).Error; err != nil {
		t.Fatalf("count messages after no_reply error: %v", err)
	}
	if afterCount != 0 {
		t.Fatalf("message count after no_reply=%d want=0", afterCount)
	}
	if exists, err := store.RDB.HExists(ctx, agentAPIStreamRegistryKey(agentID), clientMsgID).Result(); err != nil || exists {
		t.Fatalf("stream registry exists=%v err=%v want false", exists, err)
	}
	if exists, err := store.RDB.Exists(ctx, agentstream.StoppedFenceKey(agentID, clientMsgID)).Result(); err != nil || exists != 1 {
		t.Fatalf("stopped fence exists=%d err=%v want=1", exists, err)
	}

	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: clientMsgID,
		ChunkSeq:    3,
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("late finish stream chunk error: %v", err)
	}
	if err := store.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&afterCount).Error; err != nil {
		t.Fatalf("count messages after late finish error: %v", err)
	}
	if afterCount != 0 {
		t.Fatalf("message count after late finish=%d want=0", afterCount)
	}
}

func TestHandleAgentAPIStreamChunk_ThinkingFinalizedAsCard(t *testing.T) {
	logger.Init()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}
	defer func() {
		_ = store.RDB.Close()
		testDB.Close()
	}()

	const (
		sessionID                 = "g_stream_thinking_card"
		ownerID             int64 = 1001
		peerID              int64 = 2003
		agentID             int64 = 9992
		thinkingClientMsgID       = "evt_thinking_card_thinking"
	)
	const thinkingText = "first line\nsecond line"

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "thinking-group",
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:  sessionID,
			MemberID:   memberID,
			MemberType: 1,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:ws:route:2003", "dev-1", "node-test").Err(); err != nil {
		t.Fatalf("seed route redis error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-test")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  thinkingClientMsgID,
		DeltaContent: "first line\n",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first thinking stream chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  thinkingClientMsgID,
		DeltaContent: "second line",
		ChunkSeq:     2,
	}); err != nil {
		t.Fatalf("second thinking stream chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: thinkingClientMsgID,
		ChunkSeq:    3,
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish thinking stream chunk error: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	var saved model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&saved).Error; err != nil {
		t.Fatalf("load final thinking message error: %v", err)
	}
	if saved.MsgType != 1 {
		t.Fatalf("saved msg_type=%d want=1", saved.MsgType)
	}
	const markdownPrefix = "[Thinking]("
	if !strings.HasPrefix(saved.Content, markdownPrefix) || !strings.HasSuffix(saved.Content, ")") {
		t.Fatalf("thinking content should be markdown card link, got=%q", saved.Content)
	}
	href := strings.TrimSuffix(strings.TrimPrefix(saved.Content, markdownPrefix), ")")
	parsedCardURI, err := url.Parse(href)
	if err != nil {
		t.Fatalf("parse thinking card uri error: %v", err)
	}
	if parsedCardURI.Scheme != "grix" || parsedCardURI.Host != "card" || parsedCardURI.Path != "/thinking" {
		t.Fatalf("thinking card uri invalid: %s", parsedCardURI.String())
	}
	if got := parsedCardURI.Query().Get("content"); got != thinkingText {
		t.Fatalf("thinking card content=%q want=%q", got, thinkingText)
	}

	var extra map[string]any
	if err := json.Unmarshal(saved.Extra, &extra); err != nil {
		t.Fatalf("unmarshal saved extra error: %v", err)
	}
	channelData, _ := extra["channel_data"].(map[string]any)
	grixData, _ := channelData["grix"].(map[string]any)
	thinkingData, _ := grixData["thinking"].(map[string]any)
	gotThinkingContent, _ := thinkingData["content"].(string)
	if gotThinkingContent != thinkingText {
		t.Fatalf("saved extra thinking content=%q want=%q", gotThinkingContent, thinkingText)
	}

	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal redis envelope error: %v", err)
			}
			if envelope.Cmd != protocol.CmdStreamFinish {
				continue
			}
			var finishPayload protocol.StreamFinishPayload
			if err := json.Unmarshal(envelope.Payload, &finishPayload); err != nil {
				t.Fatalf("unmarshal stream_finish payload error: %v", err)
			}
			if finishPayload.MsgID != saved.MsgID {
				t.Fatalf("stream_finish msg_id=%d want=%d", finishPayload.MsgID, saved.MsgID)
			}
			if finishPayload.FinalContent != saved.Content {
				t.Fatalf("stream_finish final_content=%q want=%q", finishPayload.FinalContent, saved.Content)
			}
			return
		case <-timeout:
			t.Fatal("timed out waiting for thinking stream_finish publication")
		}
	}
}

func TestHandleAgentAPIStreamChunk_ExplicitIsThinkingWithoutSuffix(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID         = "g_stream_thinking_explicit"
		ownerID     int64 = 1601
		peerID      int64 = 2603
		agentID     int64 = 9692
		clientMsgID       = "evt_explicit_no_suffix" // 注意:不含 _thinking 后缀
	)
	const thinkingText = "explicit thinking"

	if err := store.DB.Create(&model.Session{
		SessionID: sessionID, OwnerID: ownerID, SessionType: 2, GroupName: "explicit-group",
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID: sessionID, MemberID: memberID, MemberType: 1,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1601", "agent_id", "9692").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:ws:route:2603", "dev-1", "node-test").Err(); err != nil {
		t.Fatalf("seed route redis error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-test")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	// 显式 IsThinking=true,但 client_msg_id 不含 _thinking 后缀。
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID: sessionID, ClientMsgID: clientMsgID, DeltaContent: thinkingText, ChunkSeq: 1, IsThinking: true,
	}); err != nil {
		t.Fatalf("thinking stream chunk error: %v", err)
	}

	// 流式期:广播的 stream_chunk 必须带 is_thinking=true。
	streamChunkSeen := false
	chunkTimeout := time.After(2 * time.Second)
	for !streamChunkSeen {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal envelope error: %v", err)
			}
			if envelope.Cmd != protocol.CmdStreamChunk {
				continue
			}
			var chunkPayload protocol.StreamChunkPayload
			if err := json.Unmarshal(envelope.Payload, &chunkPayload); err != nil {
				t.Fatalf("unmarshal stream_chunk error: %v", err)
			}
			if !chunkPayload.IsThinking {
				t.Fatalf("stream_chunk is_thinking=false want true (explicit flag lost)")
			}
			streamChunkSeen = true
		case <-chunkTimeout:
			t.Fatal("timed out waiting for stream_chunk publication")
		}
	}

	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID: sessionID, ClientMsgID: clientMsgID, ChunkSeq: 2, IsFinish: true, IsThinking: true,
	}); err != nil {
		t.Fatalf("finish thinking stream chunk error: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	// finalize:无 _thinking 后缀也应按持久化的显式标记包成思考卡片。
	var saved model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&saved).Error; err != nil {
		t.Fatalf("load final message error: %v", err)
	}
	if !strings.HasPrefix(saved.Content, "[Thinking](grix://card/thinking") {
		t.Fatalf("explicit thinking should finalize as thinking card, got=%q", saved.Content)
	}
}

func TestHandleAgentAPIStreamChunk_FirstVisibleKeepsComposingUntilFinalize(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-api-stream-typing-keep"
		ownerID   = int64(1501)
		agentID   = int64(9592)
		refEvent  = "evt-stream-typing-keep"
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedAgentAPIBridgeAgent(t, ownerID, agentID)

	s := &Server{
		hub:                       NewHub("node-test"),
		agentAPIStreamFinishGrace: 40 * time.Millisecond,
	}
	if err := handler.SetSessionActivityFromAgentAPI(context.Background(), s.hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID:  sessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     true,
		RefEventID: refEvent,
	}); err != nil {
		t.Fatalf("seed composing activity error: %v", err)
	}

	if err := s.handleAgentAPIStreamChunk(context.Background(), agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		EventID:      refEvent,
		SessionID:    sessionID,
		ClientMsgID:  "stream-typing-keep-1",
		DeltaContent: "hello",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}

	activitiesAfterFirstChunk, err := handler.ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities after first chunk error: %v", err)
	}
	if len(activitiesAfterFirstChunk) != 1 || !activitiesAfterFirstChunk[0].Active {
		t.Fatalf("expected composing activity to remain after first visible chunk, got=%+v", activitiesAfterFirstChunk)
	}

	if err := s.handleAgentAPIStreamChunk(context.Background(), agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		EventID:     refEvent,
		SessionID:   sessionID,
		ClientMsgID: "stream-typing-keep-1",
		ChunkSeq:    2,
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	activitiesAfterFinalize, err := handler.ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities after finalize error: %v", err)
	}
	if len(activitiesAfterFinalize) != 0 {
		t.Fatalf("expected composing activity to clear at stream finalize, got=%+v", activitiesAfterFinalize)
	}
}

func TestHandleAgentAPIStreamChunk_DropsLateChunksForStoppedStream(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "g_stream_stop_fence"
		ownerID   = int64(1001)
		peerID    = int64(2003)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-stop-fence-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}

	s := &Server{agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-stop-1",
		DeltaContent: "partial",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}

	var placeholder model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&placeholder).Error; err != nil {
		t.Fatalf("load placeholder error: %v", err)
	}

	if err := service.RevokeMessageForStop(ctx, sessionID, placeholder.MsgID); err != nil {
		t.Fatalf("RevokeMessageForStop error: %v", err)
	}
	if err := agentstream.FenceStreamsByMsgID(ctx, agentID, placeholder.MsgID, time.Minute); err != nil {
		t.Fatalf("FenceStreamsByMsgID error: %v", err)
	}

	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-stop-1",
		DeltaContent: " late chunk",
		ChunkSeq:     2,
	}); err != nil {
		t.Fatalf("late stream chunk error: %v", err)
	}

	var totalMessages int64
	if err := store.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&totalMessages).Error; err != nil {
		t.Fatalf("count messages error: %v", err)
	}
	if totalMessages != 1 {
		t.Fatalf("message count=%d want=1", totalMessages)
	}

	var liveMessages int64
	if err := store.DB.Model(&model.Message{}).Where("session_id = ? AND is_deleted = false", sessionID).Count(&liveMessages).Error; err != nil {
		t.Fatalf("count live messages error: %v", err)
	}
	if liveMessages != 0 {
		t.Fatalf("live message count=%d want=0", liveMessages)
	}

	exists, err := store.RDB.Exists(ctx, agentstream.StoppedFenceKey(agentID, "stream-stop-1")).Result()
	if err != nil {
		t.Fatalf("check stopped fence error: %v", err)
	}
	if exists != 1 {
		t.Fatalf("stopped fence exists=%d want=1", exists)
	}
}

func TestHandleAgentAPIStreamChunk_QuoteVisibleToUsesQuotedOwnerVisibility(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID             = "g_stream_quote_visible_to"
		quotedOwnerID   int64 = 3101
		replyOwnerID    int64 = 3102
		outsiderID      int64 = 3103
		agentID         int64 = 3999
		quotedMessageID int64 = 18889991234
	)

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     quotedOwnerID,
		SessionType: 2,
		GroupName:   "stream-quote-visible-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{quotedOwnerID, replyOwnerID, outsiderID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	seedAgentAPIBridgeAgent(t, replyOwnerID, agentID)

	quotedVisibleTo, _ := json.Marshal([]int64{replyOwnerID})
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMessageID,
		SessionID:  sessionID,
		SenderID:   quotedOwnerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "只给B看的消息",
		VisibleTo:  quotedVisibleTo,
		CreatedAt:  now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":3102", "agent_id", "3999").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}

	s := &Server{agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, replyOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:       sessionID,
		ClientMsgID:     "stream-quote-visible-1",
		DeltaContent:    "我引用回复你",
		ChunkSeq:        1,
		QuotedMessageID: quotedMessageID,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, replyOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:       sessionID,
		ClientMsgID:     "stream-quote-visible-1",
		ChunkSeq:        2,
		IsFinish:        true,
		QuotedMessageID: quotedMessageID,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	var reply model.Message
	if err := store.DB.
		Where("session_id = ? AND msg_id <> ?", sessionID, quotedMessageID).
		Order("msg_id DESC").
		First(&reply).Error; err != nil {
		t.Fatalf("load reply message error: %v", err)
	}
	if reply.SenderID != replyOwnerID || reply.SenderType != 1 {
		t.Fatalf("reply sender=(%d,%d) want=(%d,1)", reply.SenderID, reply.SenderType, replyOwnerID)
	}
	if reply.VisibleTo == nil {
		t.Fatal("reply visible_to should be persisted")
	}
	var visibleTo []int64
	if err := json.Unmarshal(reply.VisibleTo, &visibleTo); err != nil {
		t.Fatalf("unmarshal visible_to error: %v", err)
	}
	if !reflect.DeepEqual(visibleTo, []int64{quotedOwnerID}) {
		t.Fatalf("reply visible_to=%v want=[%d]", visibleTo, quotedOwnerID)
	}

	var inboxRows []model.UserInbox
	if err := store.DB.Where("session_id = ? AND msg_id = ?", sessionID, reply.MsgID).
		Order("user_id ASC").
		Find(&inboxRows).Error; err != nil {
		t.Fatalf("load inbox rows error: %v", err)
	}
	gotInboxUsers := make([]int64, 0, len(inboxRows))
	for _, row := range inboxRows {
		gotInboxUsers = append(gotInboxUsers, row.UserID)
	}
	wantInboxUsers := []int64{quotedOwnerID, replyOwnerID}
	if !reflect.DeepEqual(gotInboxUsers, wantInboxUsers) {
		t.Fatalf("inbox users=%v want=%v", gotInboxUsers, wantInboxUsers)
	}

	handler.BufferVisibleGroupMessage(ctx, sessionID, reply.SenderID, reply.SenderType, reply.MsgID, visibleTo)

	quotedOwnerBuffer, err := store.RDB.LRange(ctx, agentreceive.VisibleBufferKey(sessionID, 1, quotedOwnerID), 0, -1).Result()
	if err != nil {
		t.Fatalf("load quoted owner buffer error: %v", err)
	}
	if len(quotedOwnerBuffer) == 0 {
		t.Fatal("quoted owner should have buffered visible context")
	}
	outsiderBuffer, err := store.RDB.LRange(ctx, agentreceive.VisibleBufferKey(sessionID, 1, outsiderID), 0, -1).Result()
	if err != nil {
		t.Fatalf("load outsider buffer error: %v", err)
	}
	if len(outsiderBuffer) != 0 {
		t.Fatalf("outsider should not receive buffered visible context, got=%v", outsiderBuffer)
	}
}

func TestHandleAgentAPIStreamChunk_PreservesChunkSequenceAcrossResumes(t *testing.T) {
	logger.Init()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}
	defer func() {
		_ = store.RDB.Close()
		testDB.Close()
	}()

	const (
		sessionID       = "g_stream_seq"
		ownerID   int64 = 1001
		peerID    int64 = 2003
		agentID   int64 = 9992
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-seq-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:ws:route:2003", "dev-1", "node-test").Err(); err != nil {
		t.Fatalf("seed route redis error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-test")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-seq-1",
		DeltaContent: "hello",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-seq-1",
		DeltaContent: " world",
		ChunkSeq:     2,
	}); err != nil {
		t.Fatalf("second stream chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: "stream-seq-1",
		ChunkSeq:    3,
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}

	var chunkSeqs []int64
	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				UserID  int64           `json:"user_id"`
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal redis envelope error: %v", err)
			}
			switch envelope.Cmd {
			case protocol.CmdStreamChunk:
				var chunkPayload protocol.StreamChunkPayload
				if err := json.Unmarshal(envelope.Payload, &chunkPayload); err != nil {
					t.Fatalf("unmarshal stream_chunk payload error: %v", err)
				}
				chunkSeqs = append(chunkSeqs, chunkPayload.ChunkSeq)
			case protocol.CmdStreamFinish:
				var finishPayload protocol.StreamFinishPayload
				if err := json.Unmarshal(envelope.Payload, &finishPayload); err != nil {
					t.Fatalf("unmarshal stream_finish payload error: %v", err)
				}
				if !reflect.DeepEqual(chunkSeqs, []int64{1, 2}) {
					t.Fatalf("stream chunk seqs=%v want=[1 2]", chunkSeqs)
				}
				if finishPayload.LastChunkSeq != 2 {
					t.Fatalf("stream finish last_chunk_seq=%d want=2", finishPayload.LastChunkSeq)
				}
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for stream sequence publications")
		}
	}
}

func TestHandleAgentAPIStreamChunk_ReordersOutOfOrderChunksBeforeFinish(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "g_stream_reorder_before_finish"
		ownerID   = int64(1001)
		peerID    = int64(2003)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-reorder-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:ws:route:2003", "dev-1", "node-test").Err(); err != nil {
		t.Fatalf("seed route redis error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-test")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-reorder-1",
		DeltaContent: "B",
		ChunkSeq:     2,
	}); err != nil {
		t.Fatalf("second chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-reorder-1",
		DeltaContent: "A",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: "stream-reorder-1",
		ChunkSeq:    3,
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	var saved model.Message
	if err := store.DB.Where("session_id = ?", sessionID).Order("created_at DESC").First(&saved).Error; err != nil {
		t.Fatalf("load final message error: %v", err)
	}
	if saved.Content != "AB" {
		t.Fatalf("saved content=%q want=%q", saved.Content, "AB")
	}
	var totalMessages int64
	if err := store.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&totalMessages).Error; err != nil {
		t.Fatalf("count final messages error: %v", err)
	}
	if totalMessages != 1 {
		t.Fatalf("message count=%d want=1", totalMessages)
	}

	var deltas []string
	var chunkSeqs []int64
	var chunkMsgIDs []int64
	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal redis envelope error: %v", err)
			}
			switch envelope.Cmd {
			case protocol.CmdStreamChunk:
				var chunkPayload protocol.StreamChunkPayload
				if err := json.Unmarshal(envelope.Payload, &chunkPayload); err != nil {
					t.Fatalf("unmarshal stream_chunk payload error: %v", err)
				}
				deltas = append(deltas, chunkPayload.DeltaContent)
				chunkSeqs = append(chunkSeqs, chunkPayload.ChunkSeq)
				chunkMsgIDs = append(chunkMsgIDs, chunkPayload.MsgID)
			case protocol.CmdStreamFinish:
				var finishPayload protocol.StreamFinishPayload
				if err := json.Unmarshal(envelope.Payload, &finishPayload); err != nil {
					t.Fatalf("unmarshal stream_finish payload error: %v", err)
				}
				if !reflect.DeepEqual(deltas, []string{"A", "B"}) {
					t.Fatalf("stream chunk deltas=%v want=[A B]", deltas)
				}
				if !reflect.DeepEqual(chunkSeqs, []int64{1, 2}) {
					t.Fatalf("stream chunk seqs=%v want=[1 2]", chunkSeqs)
				}
				if !reflect.DeepEqual(chunkMsgIDs, []int64{saved.MsgID, saved.MsgID}) {
					t.Fatalf("stream chunk msg_ids=%v want=[%d %d]", chunkMsgIDs, saved.MsgID, saved.MsgID)
				}
				if finishPayload.FinalContent != "AB" {
					t.Fatalf("final content=%q want=%q", finishPayload.FinalContent, "AB")
				}
				if finishPayload.MsgID != saved.MsgID {
					t.Fatalf("stream_finish msg_id=%d want=%d", finishPayload.MsgID, saved.MsgID)
				}
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for reordered stream publications")
		}
	}
}

func TestHandleAgentAPIStreamChunk_ReplacesConflictingPendingChunkBeforeDrain(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "g_stream_conflicting_pending_chunk"
		ownerID   = int64(1001)
		peerID    = int64(2003)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-conflict-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:ws:route:2003", "dev-1", "node-test").Err(); err != nil {
		t.Fatalf("seed route redis error: %v", err)
	}

	s := &Server{agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-conflict-1",
		DeltaContent: " /or hea",
		ChunkSeq:     2,
	}); err != nil {
		t.Fatalf("first pending seq2 chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-conflict-1",
		DeltaContent: "or / hea",
		ChunkSeq:     2,
	}); err != nil {
		t.Fatalf("replacement seq2 chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-conflict-1",
		DeltaContent: "doct",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("seq1 chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-conflict-1",
		DeltaContent: "lth",
		ChunkSeq:     3,
	}); err != nil {
		t.Fatalf("seq3 chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: "stream-conflict-1",
		ChunkSeq:    4,
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish chunk error: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	var saved model.Message
	if err := store.DB.Where("session_id = ?", sessionID).Order("created_at DESC").First(&saved).Error; err != nil {
		t.Fatalf("load final message error: %v", err)
	}
	if saved.Content != "doctor / health" {
		t.Fatalf("saved content=%q want=%q", saved.Content, "doctor / health")
	}
}

func TestHandleAgentAPIStreamChunk_ConcurrentOutOfOrderChunksRemainOrdered(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "g_stream_reorder_concurrent"
		ownerID   = int64(1001)
		peerID    = int64(2003)
		agentID   = int64(9992)
		rounds    = 20
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-reorder-concurrent-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}

	s := &Server{agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	for i := 0; i < rounds; i++ {
		clientMsgID := fmt.Sprintf("stream-concurrent-%d", i)
		quotedMessageID := int64(18889996000 + i)

		start := make(chan struct{})
		errCh := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			errCh <- s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
				SessionID:       sessionID,
				ClientMsgID:     clientMsgID,
				QuotedMessageID: quotedMessageID,
				DeltaContent:    "B",
				ChunkSeq:        2,
			})
		}()

		go func() {
			defer wg.Done()
			<-start
			errCh <- s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
				SessionID:       sessionID,
				ClientMsgID:     clientMsgID,
				QuotedMessageID: quotedMessageID,
				DeltaContent:    "A",
				ChunkSeq:        1,
			})
		}()

		close(start)
		wg.Wait()
		close(errCh)
		for callErr := range errCh {
			if callErr != nil {
				t.Fatalf("stream chunk error: %v", callErr)
			}
		}

		if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
			SessionID:       sessionID,
			ClientMsgID:     clientMsgID,
			QuotedMessageID: quotedMessageID,
			ChunkSeq:        3,
			IsFinish:        true,
		}); err != nil {
			t.Fatalf("finish stream chunk error: %v", err)
		}
	}

	time.Sleep(120 * time.Millisecond)

	for i := 0; i < rounds; i++ {
		quotedMessageID := int64(18889996000 + i)
		var saved model.Message
		if err := store.DB.Where("session_id = ? AND quoted_message_id = ?", sessionID, quotedMessageID).
			Order("created_at DESC").
			First(&saved).Error; err != nil {
			t.Fatalf("load final message error quoted=%d: %v", quotedMessageID, err)
		}
		if saved.Content != "AB" {
			t.Fatalf("saved content=%q want=%q quoted=%d", saved.Content, "AB", quotedMessageID)
		}
	}
}

func TestHandleAgentAPIStreamChunk_OpenClawMixedFragmentsOutOfOrderRestoresContent(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "g_stream_openclaw_mixed_reorder"
		ownerID   = int64(1001)
		peerID    = int64(2003)
		agentID   = int64(9992)
		clientMsg = "stream-openclaw-mixed-1"
	)

	expectedContent := openClawMixedTestContent()

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-openclaw-mixed-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}

	chunks := splitTextIntoFixedChunkCount(expectedContent, 247)
	if len(chunks) != 247 {
		t.Fatalf("chunk count=%d want=247", len(chunks))
	}

	s := &Server{agentAPIStreamFinishGrace: 40 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例

	for seq := 2; seq <= len(chunks); seq += 2 {
		if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
			SessionID:    sessionID,
			ClientMsgID:  clientMsg,
			DeltaContent: chunks[seq-1],
			ChunkSeq:     int64(seq),
		}); err != nil {
			t.Fatalf("even chunk seq=%d error: %v", seq, err)
		}
	}
	for seq := 1; seq <= len(chunks); seq += 2 {
		if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
			SessionID:    sessionID,
			ClientMsgID:  clientMsg,
			DeltaContent: chunks[seq-1],
			ChunkSeq:     int64(seq),
		}); err != nil {
			t.Fatalf("odd chunk seq=%d error: %v", seq, err)
		}
	}

	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: clientMsg,
		ChunkSeq:    int64(len(chunks) + 1),
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}

	time.Sleep(120 * time.Millisecond)

	var saved model.Message
	if err := store.DB.Where("session_id = ?", sessionID).Order("created_at DESC").First(&saved).Error; err != nil {
		t.Fatalf("load final message error: %v", err)
	}
	if saved.Content != expectedContent {
		t.Fatalf("saved content mismatch\nwant:\n%s\n\ngot:\n%s", expectedContent, saved.Content)
	}
}

func TestHandleAgentAPIStreamChunk_OpenClawMixedPendingOverwriteBeforeDrainRestoresContent(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID      = "g_stream_openclaw_pending_overwrite"
		ownerID        = int64(1001)
		peerID         = int64(2003)
		agentID        = int64(9992)
		clientMsg      = "stream-openclaw-pending-overwrite-1"
		bufferStartSeq = 40
	)

	expectedContent := openClawMixedTestContent()

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-openclaw-overwrite-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}

	chunks := splitTextIntoFixedChunkCount(expectedContent, 247)
	if len(chunks) != 247 {
		t.Fatalf("chunk count=%d want=247", len(chunks))
	}

	s := &Server{agentAPIStreamFinishGrace: 40 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例

	for seq := bufferStartSeq; seq <= len(chunks); seq++ {
		delta := chunks[seq-1]
		if seq%23 == 0 {
			delta = "[bad-overwrite]"
		}
		if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
			SessionID:    sessionID,
			ClientMsgID:  clientMsg,
			DeltaContent: delta,
			ChunkSeq:     int64(seq),
		}); err != nil {
			t.Fatalf("buffer stage seq=%d error: %v", seq, err)
		}
	}
	for seq := bufferStartSeq; seq <= len(chunks); seq++ {
		if seq%23 != 0 {
			continue
		}
		if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
			SessionID:    sessionID,
			ClientMsgID:  clientMsg,
			DeltaContent: chunks[seq-1],
			ChunkSeq:     int64(seq),
		}); err != nil {
			t.Fatalf("overwrite seq=%d error: %v", seq, err)
		}
	}
	for seq := 1; seq < bufferStartSeq; seq++ {
		if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
			SessionID:    sessionID,
			ClientMsgID:  clientMsg,
			DeltaContent: chunks[seq-1],
			ChunkSeq:     int64(seq),
		}); err != nil {
			t.Fatalf("head seq=%d error: %v", seq, err)
		}
	}

	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: clientMsg,
		ChunkSeq:    int64(len(chunks) + 1),
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}

	time.Sleep(120 * time.Millisecond)

	var saved model.Message
	if err := store.DB.Where("session_id = ?", sessionID).Order("created_at DESC").First(&saved).Error; err != nil {
		t.Fatalf("load final message error: %v", err)
	}
	if saved.Content != expectedContent {
		t.Fatalf("saved content mismatch\nwant:\n%s\n\ngot:\n%s", expectedContent, saved.Content)
	}
}

func TestHandleAgentAPIStreamChunk_OpenClawMixedFinishBeforeHeadChunkStillRestoresContent(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "g_stream_openclaw_finish_before_head"
		ownerID   = int64(1001)
		peerID    = int64(2003)
		agentID   = int64(9992)
		clientMsg = "stream-openclaw-finish-before-head-1"
	)

	expectedContent := openClawMixedTestContent()

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-openclaw-finish-before-head-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}

	chunks := splitTextIntoFixedChunkCount(expectedContent, 247)
	if len(chunks) != 247 {
		t.Fatalf("chunk count=%d want=247", len(chunks))
	}

	s := &Server{agentAPIStreamFinishGrace: 80 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例

	for seq := 2; seq <= len(chunks); seq++ {
		if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
			SessionID:    sessionID,
			ClientMsgID:  clientMsg,
			DeltaContent: chunks[seq-1],
			ChunkSeq:     int64(seq),
		}); err != nil {
			t.Fatalf("body seq=%d error: %v", seq, err)
		}
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: clientMsg,
		ChunkSeq:    int64(len(chunks) + 1),
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  clientMsg,
		DeltaContent: chunks[0],
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("head seq=1 error: %v", err)
	}

	time.Sleep(160 * time.Millisecond)

	var saved model.Message
	if err := store.DB.Where("session_id = ?", sessionID).Order("created_at DESC").First(&saved).Error; err != nil {
		t.Fatalf("load final message error: %v", err)
	}
	if saved.Content != expectedContent {
		t.Fatalf("saved content mismatch\nwant:\n%s\n\ngot:\n%s", expectedContent, saved.Content)
	}
}

func TestHandleAgentAPIStreamChunk_FinishAbsorbsTrailingChunksBeforeFinalize(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "g_stream_finish_authoritative_final"
		ownerID   = int64(1001)
		peerID    = int64(2003)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-finish-authoritative-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:ws:route:2003", "dev-1", "node-test").Err(); err != nil {
		t.Fatalf("seed route redis error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-test")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{agentAPIStreamFinishGrace: 40 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-final-content-1",
		DeltaContent: "A",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: "stream-final-content-1",
		ChunkSeq:    3,
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-final-content-1",
		DeltaContent: "B",
		ChunkSeq:     2,
	}); err != nil {
		t.Fatalf("trailing chunk should be absorbed without error: %v", err)
	}

	if agentstream.HasStoppedFence(ctx, agentID, "stream-final-content-1") {
		t.Fatal("finish should not set stopped fence before grace finalization")
	}

	var deltas []string
	gotFinish := false
	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal redis envelope error: %v", err)
			}
			switch envelope.Cmd {
			case protocol.CmdStreamChunk:
				var chunkPayload protocol.StreamChunkPayload
				if err := json.Unmarshal(envelope.Payload, &chunkPayload); err != nil {
					t.Fatalf("unmarshal stream_chunk payload error: %v", err)
				}
				deltas = append(deltas, chunkPayload.DeltaContent)
			case protocol.CmdStreamFinish:
				var finishPayload protocol.StreamFinishPayload
				if err := json.Unmarshal(envelope.Payload, &finishPayload); err != nil {
					t.Fatalf("unmarshal stream_finish payload error: %v", err)
				}
				if !reflect.DeepEqual(deltas, []string{"A", "B"}) {
					t.Fatalf("stream chunk deltas=%v want=[A B]", deltas)
				}
				if finishPayload.FinalContent != "AB" {
					t.Fatalf("final content=%q want=%q", finishPayload.FinalContent, "AB")
				}
				gotFinish = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for finish after trailing chunk absorption")
		}
		if gotFinish {
			break
		}
	}

	var saved model.Message
	if err := store.DB.Where("session_id = ?", sessionID).Order("created_at DESC").First(&saved).Error; err != nil {
		t.Fatalf("load final message error: %v", err)
	}
	if saved.Content != "AB" {
		t.Fatalf("saved content=%q want=%q", saved.Content, "AB")
	}
	if !agentstream.HasStoppedFence(ctx, agentID, "stream-final-content-1") {
		t.Fatal("expected stopped fence after grace finalization")
	}
}

func TestHandleAgentAPIStreamChunk_FinishWithGapFinalizesBufferedContentAndDropsLateChunk(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "g_stream_finish_gap_partial"
		ownerID   = int64(1001)
		peerID    = int64(2004)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-finish-gap-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:ws:route:2004", "dev-1", "node-test").Err(); err != nil {
		t.Fatalf("seed route redis error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-test")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{agentAPIStreamFinishGrace: 40 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-gap-1",
		DeltaContent: "A",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: "stream-gap-1",
		ChunkSeq:    3,
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish chunk error: %v", err)
	}

	var deltas []string
	gotFinish := false
	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal redis envelope error: %v", err)
			}
			switch envelope.Cmd {
			case protocol.CmdStreamChunk:
				var chunkPayload protocol.StreamChunkPayload
				if err := json.Unmarshal(envelope.Payload, &chunkPayload); err != nil {
					t.Fatalf("unmarshal stream_chunk payload error: %v", err)
				}
				deltas = append(deltas, chunkPayload.DeltaContent)
			case protocol.CmdStreamFinish:
				var finishPayload protocol.StreamFinishPayload
				if err := json.Unmarshal(envelope.Payload, &finishPayload); err != nil {
					t.Fatalf("unmarshal stream_finish payload error: %v", err)
				}
				if !reflect.DeepEqual(deltas, []string{"A"}) {
					t.Fatalf("stream chunk deltas=%v want=[A]", deltas)
				}
				if finishPayload.FinalContent != "A" {
					t.Fatalf("final content=%q want=%q", finishPayload.FinalContent, "A")
				}
				gotFinish = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for finish after gap finalization")
		}
		if gotFinish {
			break
		}
	}

	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-gap-1",
		DeltaContent: "B",
		ChunkSeq:     2,
	}); err != nil {
		t.Fatalf("late chunk after finish should be dropped without error: %v", err)
	}

	var saved model.Message
	if err := store.DB.Where("session_id = ?", sessionID).Order("created_at DESC").First(&saved).Error; err != nil {
		t.Fatalf("load final message error: %v", err)
	}
	if saved.Content != "A" {
		t.Fatalf("saved content=%q want=%q", saved.Content, "A")
	}
	if !agentstream.HasStoppedFence(ctx, agentID, "stream-gap-1") {
		t.Fatal("expected stopped fence after partial finish finalization")
	}
}

func TestHandleAgentAPIStreamDisconnect_AbortsAndFinalizesPartialContent(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "g_stream_disconnect_abort"
		ownerID   = int64(1001)
		peerID    = int64(2003)
		agentID   = int64(9992)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "stream-disconnect-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}

	s := &Server{}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-disconnect-1",
		DeltaContent: "partial reply",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}

	var placeholder model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&placeholder).Error; err != nil {
		t.Fatalf("load placeholder error: %v", err)
	}
	if placeholder.MsgType != 4 {
		t.Fatalf("placeholder msg_type=%d want=4", placeholder.MsgType)
	}

	if exists, err := store.RDB.HExists(ctx, agentAPIStreamRegistryKey(agentID), "stream-disconnect-1").Result(); err != nil || !exists {
		t.Fatalf("stream registry missing before disconnect exists=%v err=%v", exists, err)
	}

	s.handleAgentAPIStreamDisconnect(ctx, agentID, ownerID)

	var saved model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", placeholder.MsgID, sessionID).First(&saved).Error; err != nil {
		t.Fatalf("load finalized message error: %v", err)
	}
	if saved.MsgType != 1 {
		t.Fatalf("saved msg_type=%d want=1", saved.MsgType)
	}
	if saved.Content != "partial reply" {
		t.Fatalf("saved content=%q want=%q", saved.Content, "partial reply")
	}

	if exists, err := store.RDB.HExists(ctx, agentAPIStreamRegistryKey(agentID), "stream-disconnect-1").Result(); err != nil {
		t.Fatalf("check stream registry cleanup error: %v", err)
	} else if exists {
		t.Fatal("stream registry entry should be removed after disconnect abort")
	}
}

func TestHandleAgentAPIStreamChunk_GroupMentionAliasDispatchesDelegateEvent(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID        = "g_stream_alias_mention_delegate"
		liuOwnerID int64 = 3101
		maOwnerID  int64 = 3102
		huaOwnerID int64 = 3103
		liuAgentID int64 = 9301
		maAgentID  int64 = 9302
	)

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     liuOwnerID,
		SessionType: 2,
		GroupName:   "api-agents-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, user := range []model.User{
		{ID: liuOwnerID, Username: "liu", Email: "liu@example.com", Nickname: "Liu", Status: model.UserStatusActive},
		{ID: maOwnerID, Username: "ma", Email: "ma@example.com", Nickname: "Ma", Status: model.UserStatusActive},
		{ID: huaOwnerID, Username: "hua", Email: "hua@example.com", Nickname: "Hua", Status: model.UserStatusActive},
	} {
		u := user
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user(%d) error: %v", u.ID, err)
		}
	}
	for _, memberID := range []int64{liuOwnerID, maOwnerID, huaOwnerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create member(%d) error: %v", memberID, err)
		}
	}

	seedAgentAPIBridgeAgent(t, liuOwnerID, liuAgentID)
	seedAgentAPIBridgeAgent(t, maOwnerID, maAgentID)

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":3101", "agent_id", "9301").Err(); err != nil {
		t.Fatalf("seed liu delegate error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":3102", "agent_id", "9302").Err(); err != nil {
		t.Fatalf("seed ma delegate error: %v", err)
	}
	if err := store.RDB.Set(ctx, "im:agent_api:route:9302", "node-target", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route error: %v", err)
	}

	mgr := wsagentapi.NewManager("", 30*time.Second, nil, nil, nil, nil)
	mgr.SetNodeID("node-origin")
	previous := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(mgr)
	defer wsagentapi.SetGlobal(previous)

	pubsub := store.RDB.Subscribe(ctx, "chan:node-target")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{hub: NewHub("node-origin"), agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, liuAgentID, liuOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-mention-1",
		DeltaContent: "请 @Ma 看一下这条信息",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, liuAgentID, liuOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: "stream-mention-1",
		ChunkSeq:    2,
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	var saved model.Message
	if err := store.DB.Where("session_id = ?", sessionID).
		Order("created_at DESC").
		First(&saved).Error; err != nil {
		t.Fatalf("load saved message error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(saved.Extra, &extra); err != nil {
		t.Fatalf("unmarshal saved extra error: %v", err)
	}
	rawMentions, ok := extra["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 1 {
		t.Fatalf("mention_user_ids invalid: %#v", extra["mention_user_ids"])
	}
	if mustParseInt64(rawMentions[0].(string)) != maOwnerID {
		t.Fatalf("mention_user_ids[0]=%v want=%d", rawMentions[0], maOwnerID)
	}

	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal forwarded envelope error: %v", err)
			}
			if envelope.Cmd != "_agent_api_delegate_event" {
				continue
			}

			var event wsagentapi.DelegateEventPayload
			if err := json.Unmarshal(envelope.Payload, &event); err != nil {
				t.Fatalf("unmarshal forwarded event payload error: %v", err)
			}
			if event.EventType != "group_mention" {
				t.Fatalf("event_type=%s want=group_mention", event.EventType)
			}
			if event.OwnerID != maOwnerID {
				t.Fatalf("owner_id=%d want=%d", event.OwnerID, maOwnerID)
			}
			if event.AgentID != maAgentID {
				t.Fatalf("agent_id=%d want=%d", event.AgentID, maAgentID)
			}
			if event.MsgID != saved.MsgID {
				t.Fatalf("msg_id=%d want=%d", event.MsgID, saved.MsgID)
			}
			if len(event.MentionUserIDs) != 1 || event.MentionUserIDs[0] != maOwnerID {
				t.Fatalf("mention_user_ids=%v want=[%d]", event.MentionUserIDs, maOwnerID)
			}
			return
		case <-timeout:
			t.Fatal("timed out waiting for forwarded delegate event")
		}
	}
}

func TestHandleAgentAPIStreamChunk_GroupAgentMentionDispatchesDirectEvent(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID        = "g_stream_agent_to_agent_direct"
		liuOwnerID int64 = 4101
		maOwnerID  int64 = 4102
		liuAgentID int64 = 9401
		maAgentID  int64 = 9402
	)

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     liuOwnerID,
		SessionType: 2,
		GroupName:   "agents-direct-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	seedAgentAPIBridgeAgent(t, liuOwnerID, liuAgentID)
	seedAgentAPIBridgeAgent(t, maOwnerID, maAgentID)
	if err := store.DB.Model(&model.Agent{}).
		Where("id = ?", liuAgentID).
		Update("agent_name", "Liu").Error; err != nil {
		t.Fatalf("update liu agent_name error: %v", err)
	}
	if err := store.DB.Model(&model.Agent{}).
		Where("id = ?", maAgentID).
		Update("agent_name", "Ma").Error; err != nil {
		t.Fatalf("update ma agent_name error: %v", err)
	}

	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: liuAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: maAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		m := member
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create agent member(%d) error: %v", m.MemberID, err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.Set(ctx, "im:agent_api:route:9402", "node-target", time.Minute).Err(); err != nil {
		t.Fatalf("seed ma route error: %v", err)
	}

	mgr := wsagentapi.NewManager("", 30*time.Second, nil, nil, nil, nil)
	mgr.SetNodeID("node-origin")
	previous := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(mgr)
	defer wsagentapi.SetGlobal(previous)

	pubsub := store.RDB.Subscribe(ctx, "chan:node-target")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{hub: NewHub("node-origin"), agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, liuAgentID, liuOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-agent-direct-1",
		DeltaContent: "@Liu 我先接手，@Ma 你来补充",
		ChunkSeq:     1,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, liuAgentID, liuOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: "stream-agent-direct-1",
		ChunkSeq:    2,
		IsFinish:    true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal forwarded envelope error: %v", err)
			}
			if envelope.Cmd != "_agent_api_delegate_event" {
				continue
			}

			var event wsagentapi.DelegateEventPayload
			if err := json.Unmarshal(envelope.Payload, &event); err != nil {
				t.Fatalf("unmarshal forwarded event payload error: %v", err)
			}
			if event.EventType != "group_mention" {
				t.Fatalf("event_type=%s want=group_mention", event.EventType)
			}
			if event.AgentID != maAgentID {
				t.Fatalf("agent_id=%d want=%d", event.AgentID, maAgentID)
			}
			if event.SenderID != liuAgentID {
				t.Fatalf("sender_id=%d want=%d", event.SenderID, liuAgentID)
			}
			containsMa := false
			for _, id := range event.MentionUserIDs {
				if id == maAgentID {
					containsMa = true
					break
				}
			}
			if !containsMa {
				t.Fatalf("mention_user_ids=%v should contain ma agent id=%d", event.MentionUserIDs, maAgentID)
			}
			return
		case <-timeout:
			t.Fatal("timed out waiting for agent-to-agent direct event")
		}
	}
}

func TestHandleAgentAPIStreamChunk_QuotedAgentDispatchesDirectEvent(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID         = "g_stream_quoted_agent_direct"
		liuOwnerID  int64 = 4121
		maOwnerID   int64 = 4122
		liuAgentID  int64 = 9421
		maAgentID   int64 = 9422
		quotedMsgID int64 = 18889990502
	)

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     liuOwnerID,
		SessionType: 2,
		GroupName:   "agents-quoted-direct-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	seedAgentAPIBridgeAgent(t, liuOwnerID, liuAgentID)
	seedAgentAPIBridgeAgent(t, maOwnerID, maAgentID)
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: liuAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: maAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		m := member
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create agent member(%d) error: %v", m.MemberID, err)
		}
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMsgID,
		SessionID:  sessionID,
		SenderID:   maAgentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "我是被引用的 agent 消息",
		CreatedAt:  now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	ctx := context.Background()
	if err := store.RDB.Set(ctx, "im:agent_api:route:9422", "node-target", time.Minute).Err(); err != nil {
		t.Fatalf("seed ma route error: %v", err)
	}

	mgr := wsagentapi.NewManager("", 30*time.Second, nil, nil, nil, nil)
	mgr.SetNodeID("node-origin")
	previous := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(mgr)
	defer wsagentapi.SetGlobal(previous)

	pubsub := store.RDB.Subscribe(ctx, "chan:node-target")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{hub: NewHub("node-origin"), agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, liuAgentID, liuOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:       sessionID,
		ClientMsgID:     "stream-quoted-agent-direct-1",
		DeltaContent:    "我接着这条引用继续说",
		ChunkSeq:        1,
		QuotedMessageID: quotedMsgID,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, liuAgentID, liuOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:       sessionID,
		ClientMsgID:     "stream-quoted-agent-direct-1",
		ChunkSeq:        2,
		QuotedMessageID: quotedMsgID,
		IsFinish:        true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal forwarded envelope error: %v", err)
			}
			if envelope.Cmd != "_agent_api_delegate_event" {
				continue
			}

			var event wsagentapi.DelegateEventPayload
			if err := json.Unmarshal(envelope.Payload, &event); err != nil {
				t.Fatalf("unmarshal forwarded event payload error: %v", err)
			}
			if event.EventType != "group_mention" {
				t.Fatalf("event_type=%s want=group_mention", event.EventType)
			}
			if event.AgentID != maAgentID {
				t.Fatalf("agent_id=%d want=%d", event.AgentID, maAgentID)
			}
			if event.SenderID != liuAgentID {
				t.Fatalf("sender_id=%d want=%d", event.SenderID, liuAgentID)
			}
			if len(event.MentionUserIDs) != 1 || event.MentionUserIDs[0] != maAgentID {
				t.Fatalf("mention_user_ids=%v want=[%d]", event.MentionUserIDs, maAgentID)
			}
			return
		case <-timeout:
			t.Fatal("timed out waiting for quoted-agent direct event")
		}
	}
}

func TestHandleAgentAPIStreamChunk_QuotedOwnerDispatchesDelegateEvent(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID         = "g_stream_quoted_owner_delegate"
		liuOwnerID  int64 = 4111
		maOwnerID   int64 = 4112
		huaOwnerID  int64 = 4113
		liuAgentID  int64 = 9411
		maAgentID   int64 = 9412
		quotedMsgID int64 = 18889990501
	)

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     liuOwnerID,
		SessionType: 2,
		GroupName:   "api-agents-quoted-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, user := range []model.User{
		{ID: liuOwnerID, Username: "liu", Email: "liu@example.com", Nickname: "Liu", Status: model.UserStatusActive},
		{ID: maOwnerID, Username: "ma", Email: "ma@example.com", Nickname: "Ma", Status: model.UserStatusActive},
		{ID: huaOwnerID, Username: "hua", Email: "hua@example.com", Nickname: "Hua", Status: model.UserStatusActive},
	} {
		u := user
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user(%d) error: %v", u.ID, err)
		}
	}
	for _, memberID := range []int64{liuOwnerID, maOwnerID, huaOwnerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create member(%d) error: %v", memberID, err)
		}
	}

	seedAgentAPIBridgeAgent(t, liuOwnerID, liuAgentID)
	seedAgentAPIBridgeAgent(t, maOwnerID, maAgentID)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMsgID,
		SessionID:  sessionID,
		SenderID:   maOwnerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "请接着看这条旧消息",
		CreatedAt:  now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":4111", "agent_id", "9411").Err(); err != nil {
		t.Fatalf("seed liu delegate error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":4112", "agent_id", "9412").Err(); err != nil {
		t.Fatalf("seed ma delegate error: %v", err)
	}
	if err := store.RDB.Set(ctx, "im:agent_api:route:9412", "node-target", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route error: %v", err)
	}

	mgr := wsagentapi.NewManager("", 30*time.Second, nil, nil, nil, nil)
	mgr.SetNodeID("node-origin")
	previous := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(mgr)
	defer wsagentapi.SetGlobal(previous)

	pubsub := store.RDB.Subscribe(ctx, "chan:node-target")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{hub: NewHub("node-origin"), agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, liuAgentID, liuOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:       sessionID,
		ClientMsgID:     "stream-quoted-owner-1",
		DeltaContent:    "请继续处理这条引用",
		ChunkSeq:        1,
		QuotedMessageID: quotedMsgID,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, liuAgentID, liuOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:       sessionID,
		ClientMsgID:     "stream-quoted-owner-1",
		ChunkSeq:        2,
		QuotedMessageID: quotedMsgID,
		IsFinish:        true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}
	time.Sleep(40 * time.Millisecond)

	var saved model.Message
	if err := store.DB.Where("session_id = ? AND content = ?", sessionID, "请继续处理这条引用").
		First(&saved).Error; err != nil {
		t.Fatalf("load saved message error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(saved.Extra, &extra); err != nil {
		t.Fatalf("unmarshal saved extra error: %v", err)
	}
	rawMentions, ok := extra["mention_user_ids"].([]any)
	if !ok || len(rawMentions) != 1 {
		t.Fatalf("mention_user_ids invalid: %#v", extra["mention_user_ids"])
	}
	if mustParseInt64(rawMentions[0].(string)) != maOwnerID {
		t.Fatalf("mention_user_ids[0]=%v want=%d", rawMentions[0], maOwnerID)
	}

	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal forwarded envelope error: %v", err)
			}
			if envelope.Cmd != "_agent_api_delegate_event" {
				continue
			}

			var event wsagentapi.DelegateEventPayload
			if err := json.Unmarshal(envelope.Payload, &event); err != nil {
				t.Fatalf("unmarshal forwarded event payload error: %v", err)
			}
			if event.EventType != "group_mention" {
				t.Fatalf("event_type=%s want=group_mention", event.EventType)
			}
			if event.OwnerID != maOwnerID {
				t.Fatalf("owner_id=%d want=%d", event.OwnerID, maOwnerID)
			}
			if event.AgentID != maAgentID {
				t.Fatalf("agent_id=%d want=%d", event.AgentID, maAgentID)
			}
			if event.MsgID != saved.MsgID {
				t.Fatalf("msg_id=%d want=%d", event.MsgID, saved.MsgID)
			}
			if len(event.MentionUserIDs) != 1 || event.MentionUserIDs[0] != maOwnerID {
				t.Fatalf("mention_user_ids=%v want=[%d]", event.MentionUserIDs, maOwnerID)
			}
			return
		case <-timeout:
			t.Fatal("timed out waiting for forwarded delegate event")
		}
	}
}

func TestHandleAgentAPIStreamChunk_ExplicitMentionSkipsQuotedOwnerDelegateEvent(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID         = "g_stream_quoted_owner_explicit_target"
		liuOwnerID  int64 = 4131
		maOwnerID   int64 = 4132
		huaOwnerID  int64 = 4133
		liuAgentID  int64 = 9431
		maAgentID   int64 = 9432
		huaAgentID  int64 = 9433
		quotedMsgID int64 = 18889990503
	)

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     liuOwnerID,
		SessionType: 2,
		GroupName:   "api-agents-quoted-explicit-group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, user := range []model.User{
		{ID: liuOwnerID, Username: "liu_explicit", Email: "liu_explicit@example.com", Nickname: "Liu", Status: model.UserStatusActive},
		{ID: maOwnerID, Username: "ma_explicit", Email: "ma_explicit@example.com", Nickname: "Ma", Status: model.UserStatusActive},
		{ID: huaOwnerID, Username: "hua_explicit", Email: "hua_explicit@example.com", Nickname: "Hua", Status: model.UserStatusActive},
	} {
		u := user
		if err := store.DB.Create(&u).Error; err != nil {
			t.Fatalf("create user(%d) error: %v", u.ID, err)
		}
	}
	for _, memberID := range []int64{liuOwnerID, maOwnerID, huaOwnerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create member(%d) error: %v", memberID, err)
		}
	}

	seedAgentAPIBridgeAgent(t, liuOwnerID, liuAgentID)
	seedAgentAPIBridgeAgent(t, maOwnerID, maAgentID)
	seedAgentAPIBridgeAgent(t, huaOwnerID, huaAgentID)
	if err := store.DB.Create(&model.Message{
		MsgID:      quotedMsgID,
		SessionID:  sessionID,
		SenderID:   maOwnerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "请接着看这条旧消息",
		CreatedAt:  now.Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create quoted message error: %v", err)
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":4131", "agent_id", "9431").Err(); err != nil {
		t.Fatalf("seed liu delegate error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":4132", "agent_id", "9432").Err(); err != nil {
		t.Fatalf("seed ma delegate error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":4133", "agent_id", "9433").Err(); err != nil {
		t.Fatalf("seed hua delegate error: %v", err)
	}
	if err := store.RDB.Set(ctx, "im:agent_api:route:9432", "node-target", time.Minute).Err(); err != nil {
		t.Fatalf("seed ma route error: %v", err)
	}
	if err := store.RDB.Set(ctx, "im:agent_api:route:9433", "node-target", time.Minute).Err(); err != nil {
		t.Fatalf("seed hua route error: %v", err)
	}

	mgr := wsagentapi.NewManager("", 30*time.Second, nil, nil, nil, nil)
	mgr.SetNodeID("node-origin")
	previous := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(mgr)
	defer wsagentapi.SetGlobal(previous)

	pubsub := store.RDB.Subscribe(ctx, "chan:node-target")
	defer pubsub.Close()
	ch := pubsub.Channel()

	s := &Server{hub: NewHub("node-origin"), agentAPIStreamFinishGrace: 20 * time.Millisecond}
	defer s.cleanupRuntime() // 关停：停流式收尾定时器并等在跑的回调退出，别让它活过用例
	if err := s.handleAgentAPIStreamChunk(ctx, liuAgentID, liuOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:       sessionID,
		ClientMsgID:     "stream-quoted-owner-explicit-target-1",
		DeltaContent:    "@Hua 这句请你来接手",
		ChunkSeq:        1,
		QuotedMessageID: quotedMsgID,
	}); err != nil {
		t.Fatalf("first stream chunk error: %v", err)
	}
	if err := s.handleAgentAPIStreamChunk(ctx, liuAgentID, liuOwnerID, wsagentapi.AgentStreamChunkPayload{
		SessionID:       sessionID,
		ClientMsgID:     "stream-quoted-owner-explicit-target-1",
		ChunkSeq:        2,
		QuotedMessageID: quotedMsgID,
		IsFinish:        true,
	}); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}
	events := make([]wsagentapi.DelegateEventPayload, 0, 2)
	deadline := time.After(2 * time.Second)
	idle := (<-chan time.Time)(nil)
	for {
		select {
		case msg := <-ch:
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				t.Fatalf("unmarshal forwarded envelope error: %v", err)
			}
			if envelope.Cmd != "_agent_api_delegate_event" {
				continue
			}

			var event wsagentapi.DelegateEventPayload
			if err := json.Unmarshal(envelope.Payload, &event); err != nil {
				t.Fatalf("unmarshal forwarded event payload error: %v", err)
			}
			events = append(events, event)
			idle = time.After(200 * time.Millisecond)
		case <-idle:
			goto ASSERT
		case <-deadline:
			if len(events) == 0 {
				t.Fatal("timed out waiting for forwarded delegate event")
			}
			goto ASSERT
		}
	}

ASSERT:
	if len(events) != 1 {
		t.Fatalf("forwarded delegate events=%d want=1 payload=%#v", len(events), events)
	}

	var explicitTarget *wsagentapi.DelegateEventPayload
	for i := range events {
		event := &events[i]
		if event.OwnerID == huaOwnerID {
			explicitTarget = event
		}
	}
	if explicitTarget == nil {
		t.Fatalf("forwarded delegate events missing expected owners payload=%#v", events)
	}

	if explicitTarget.EventType != "group_mention" {
		t.Fatalf("explicit target event_type=%s want=group_mention", explicitTarget.EventType)
	}
	if explicitTarget.MirrorMode != wsagentapi.MirrorModeRecordAndProcess {
		t.Fatalf("explicit target mirror mode=%q want=%q", explicitTarget.MirrorMode, wsagentapi.MirrorModeRecordAndProcess)
	}
	if explicitTarget.AgentID != huaAgentID {
		t.Fatalf("explicit target agent_id=%d want=%d", explicitTarget.AgentID, huaAgentID)
	}
	if explicitTarget.MsgID <= 0 {
		t.Fatalf("explicit target msg_id=%d want positive", explicitTarget.MsgID)
	}
	if explicitTarget.QuotedMessageID != quotedMsgID {
		t.Fatalf("explicit target quoted_message_id=%d want=%d", explicitTarget.QuotedMessageID, quotedMsgID)
	}
	if len(explicitTarget.MentionUserIDs) != 1 || explicitTarget.MentionUserIDs[0] != huaOwnerID {
		t.Fatalf("explicit target mention_user_ids=%v want=[%d]", explicitTarget.MentionUserIDs, huaOwnerID)
	}
}

func mustParseInt64(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("parse int64 %q: %v", s, err))
	}
	return v
}

// TestHandleAgentAPIStreamChunk_ReanchorsAfterStateLoss 复现 chunk_seq 失步导致的
// 空白气泡：重排状态丢失后，Agent 从远高于 1 的 seq 恢复时，重锚逻辑应把该分片立即
// 落地为可见内容，而不是当作 future chunk 永久缓存成空白气泡。
func TestHandleAgentAPIStreamChunk_ReanchorsAfterStateLoss(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID       = "g_stream_reanchor"
		ownerID   int64 = 1001
		peerID    int64 = 2003
		agentID   int64 = 9992
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "reanchor-group",
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:  sessionID,
			MemberID:   memberID,
			MemberType: 1,
		}).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:delegate:"+sessionID+":1001", "agent_id", "9992").Err(); err != nil {
		t.Fatalf("seed delegate redis error: %v", err)
	}

	s := &Server{
		agentAPIStreamFinishGrace:   20 * time.Millisecond,
		agentAPIStreamStallReanchor: 5 * time.Millisecond,
	}
	// 模拟"长停顿后恢复"：全新 client_msg_id 的首个分片 seq 已是 16（>1），
	// 等价于线上状态被清后 Agent 继续递增 seq 的场景。
	resumed := wsagentapi.AgentStreamChunkPayload{
		SessionID:    sessionID,
		ClientMsgID:  "stream-reanchor",
		DeltaContent: "resumed-content",
		ChunkSeq:     16,
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, resumed); err != nil {
		t.Fatalf("resumed stream chunk error: %v", err)
	}
	// 等待超过卡顿阈值，使下一分片触发重锚。
	time.Sleep(15 * time.Millisecond)
	finish := wsagentapi.AgentStreamChunkPayload{
		SessionID:   sessionID,
		ClientMsgID: "stream-reanchor",
		ChunkSeq:    17,
		IsFinish:    true,
	}
	if err := s.handleAgentAPIStreamChunk(ctx, agentID, ownerID, finish); err != nil {
		t.Fatalf("finish stream chunk error: %v", err)
	}
	time.Sleep(60 * time.Millisecond)

	var saved model.Message
	if err := store.DB.Where("session_id = ?", sessionID).First(&saved).Error; err != nil {
		t.Fatalf("load message error: %v", err)
	}
	if saved.Content != "resumed-content" {
		t.Fatalf("content=%q want=%q (re-anchor should flush instead of buffering forever)", saved.Content, "resumed-content")
	}
}
