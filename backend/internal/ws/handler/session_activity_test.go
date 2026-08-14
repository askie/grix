package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type sessionActivityMockConn struct {
	userID   int64
	deviceID string
	seq      int64
	sent     []sessionActivitySent
}

type sessionActivitySent struct {
	cmd     string
	seq     int64
	payload interface{}
}

func (c *sessionActivityMockConn) SendPayload(cmd string, seq int64, payload interface{}) {
	c.sent = append(c.sent, sessionActivitySent{cmd: cmd, seq: seq, payload: payload})
}

func (c *sessionActivityMockConn) SendPacket(pkt *protocol.Packet) {}
func (c *sessionActivityMockConn) AckPush(msgID int64)             {}
func (c *sessionActivityMockConn) Close()                          {}
func (c *sessionActivityMockConn) NextSeq() int64 {
	c.seq++
	return c.seq
}
func (c *sessionActivityMockConn) GetUserID() int64                                           { return c.userID }
func (c *sessionActivityMockConn) GetDeviceID() string                                        { return c.deviceID }
func (c *sessionActivityMockConn) GetPlatform() string                                        { return "" }
func (c *sessionActivityMockConn) SetAuth(userID int64, sessionID, deviceID, platform string) {}
func (c *sessionActivityMockConn) IsAuthed() bool                                             { return true }

type sessionActivityMockHub struct {
	nodeID string
	conns  map[int64][]ConnInterface
}

func (h *sessionActivityMockHub) Register(c ConnInterface)                  {}
func (h *sessionActivityMockHub) Unregister(c ConnInterface)                {}
func (h *sessionActivityMockHub) RefreshAlive(c ConnInterface)              {}
func (h *sessionActivityMockHub) GetNodeID() string                         { return h.nodeID }
func (h *sessionActivityMockHub) GetUserConns(userID int64) []ConnInterface { return h.conns[userID] }

func TestHandleSessionActivitySetListAndClear(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	sessionID := "session-activity-1"
	user1 := int64(1001)
	user2 := int64(1002)

	mustCreateSessionActivityFixture(t, sessionID, user1, user2, 0)

	conn1 := &sessionActivityMockConn{userID: user1, deviceID: "dev-1"}
	conn2 := &sessionActivityMockConn{userID: user2, deviceID: "dev-2"}
	hub := &sessionActivityMockHub{
		nodeID: "node-test",
		conns: map[int64][]ConnInterface{
			user1: {conn1},
			user2: {conn2},
		},
	}

	setPayload, _ := json.Marshal(protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    true,
		TTLMS:     5000,
	})
	HandleSessionActivitySet(hub, conn1, &protocol.Packet{
		Cmd:     protocol.CmdSessionActivitySet,
		Seq:     11,
		Payload: setPayload,
	})

	activities, err := ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities error: %v", err)
	}
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Kind != protocol.SessionActivityKindComposing {
		t.Fatalf("activity kind=%s", activities[0].Kind)
	}
	if activities[0].ActorID != user1 || activities[0].ActorType != protocol.SessionActivityActorTypeHuman {
		t.Fatalf("unexpected actor=%+v", activities[0])
	}

	if len(conn2.sent) == 0 {
		t.Fatal("expected broadcast to other session member")
	}
	lastSync, ok := conn2.sent[len(conn2.sent)-1].payload.(protocol.SessionActivityPayload)
	if !ok {
		t.Fatalf("unexpected payload type %T", conn2.sent[len(conn2.sent)-1].payload)
	}
	if !lastSync.Active {
		t.Fatalf("expected active sync, got=%+v", lastSync)
	}

	listPayload, _ := json.Marshal(protocol.SessionActivityListPayload{
		SessionID: sessionID,
	})
	HandleSessionActivityList(hub, conn2, &protocol.Packet{
		Cmd:     protocol.CmdSessionActivityList,
		Seq:     12,
		Payload: listPayload,
	})
	if len(conn2.sent) < 2 {
		t.Fatal("expected list response to be sent")
	}
	listResp, ok := conn2.sent[len(conn2.sent)-1].payload.(protocol.SessionActivityListRespPayload)
	if !ok {
		t.Fatalf("unexpected list response type %T", conn2.sent[len(conn2.sent)-1].payload)
	}
	if len(listResp.Activities) != 1 {
		t.Fatalf("expected 1 list activity, got %d", len(listResp.Activities))
	}

	clearPayload, _ := json.Marshal(protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    false,
	})
	HandleSessionActivitySet(hub, conn1, &protocol.Packet{
		Cmd:     protocol.CmdSessionActivitySet,
		Seq:     13,
		Payload: clearPayload,
	})

	activities, err = ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities after clear error: %v", err)
	}
	if len(activities) != 0 {
		t.Fatalf("expected 0 activities after clear, got %d", len(activities))
	}

	lastSync, ok = conn2.sent[len(conn2.sent)-1].payload.(protocol.SessionActivityPayload)
	if !ok {
		t.Fatalf("unexpected clear payload type %T", conn2.sent[len(conn2.sent)-1].payload)
	}
	if lastSync.Active {
		t.Fatalf("expected inactive clear sync, got=%+v", lastSync)
	}
}

func TestUpsertSessionActivityWithTTL_RespectsAndClampsAgentAPITTL(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		sessionID = "session-activity-agent-ttl"
		ownerID   = int64(1801)
		peerID    = int64(1802)
		agentID   = int64(9801)
	)
	mustCreateSessionActivityFixture(t, sessionID, ownerID, peerID, agentID)
	if err := store.RDB.HSet(
		context.Background(),
		"im:delegate:"+sessionID+":1801",
		"agent_id", "9801",
	).Err(); err != nil {
		t.Fatalf("seed delegate state error: %v", err)
	}

	hub := &sessionActivityMockHub{nodeID: "node-test", conns: map[int64][]ConnInterface{}}

	assertTTLDelta := func(expectedMs int64) {
		t.Helper()
		activities, err := ListSessionActivities(context.Background(), sessionID)
		if err != nil {
			t.Fatalf("ListSessionActivities error: %v", err)
		}
		if len(activities) != 1 {
			t.Fatalf("expected 1 activity, got %d", len(activities))
		}
		delta := activities[0].ExpiresAt - activities[0].UpdatedAt
		if delta != expectedMs {
			t.Fatalf("ttl delta=%dms want=%dms activity=%+v", delta, expectedMs, activities[0])
		}
	}

	if err := SetSessionActivityFromAgentAPI(context.Background(), hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID:  sessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     true,
		TTLMS:      120_000,
		RefEventID: "evt-agent-ttl",
	}); err != nil {
		t.Fatalf("upsert with explicit ttl error: %v", err)
	}
	assertTTLDelta(120_000)

	if err := SetSessionActivityFromAgentAPI(context.Background(), hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID:  sessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     true,
		RefEventID: "evt-agent-ttl",
	}); err != nil {
		t.Fatalf("upsert with default ttl error: %v", err)
	}
	assertTTLDelta(int64(nonHumanActivityTTL / time.Millisecond))

	if err := SetSessionActivityFromAgentAPI(context.Background(), hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID:  sessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     true,
		TTLMS:      1_000,
		RefEventID: "evt-agent-ttl",
	}); err != nil {
		t.Fatalf("upsert with low ttl error: %v", err)
	}
	assertTTLDelta(int64(nonHumanActivityMinTTL / time.Millisecond))

	if err := SetSessionActivityFromAgentAPI(context.Background(), hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID:  sessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     true,
		TTLMS:      30_000,
		RefEventID: "evt-agent-ttl",
	}); err != nil {
		t.Fatalf("upsert with connector ttl error: %v", err)
	}
	assertTTLDelta(30_000)

	if err := SetSessionActivityFromAgentAPI(context.Background(), hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID:  sessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     true,
		TTLMS:      300_000,
		RefEventID: "evt-agent-ttl",
	}); err != nil {
		t.Fatalf("upsert with high ttl error: %v", err)
	}
	assertTTLDelta(int64(nonHumanActivityMaxTTL / time.Millisecond))
}

func TestSetSessionActivityFromAgentAPIDelegateIdentity(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	sessionID := "session-activity-delegate"
	ownerID := int64(2001)
	peerID := int64(2002)
	agentID := int64(9001)

	mustCreateSessionActivityFixture(t, sessionID, ownerID, peerID, agentID)
	if err := store.RDB.HSet(context.Background(),
		"im:delegate:"+sessionID+":2001",
		"agent_id", "9001",
	).Err(); err != nil {
		t.Fatalf("seed delegate state error: %v", err)
	}

	ownerConn := &sessionActivityMockConn{userID: ownerID, deviceID: "owner-dev"}
	peerConn := &sessionActivityMockConn{userID: peerID, deviceID: "peer-dev"}
	hub := &sessionActivityMockHub{
		nodeID: "node-test",
		conns: map[int64][]ConnInterface{
			ownerID: {ownerConn},
			peerID:  {peerConn},
		},
	}

	err := SetSessionActivityFromAgentAPI(context.Background(), hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID:  sessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     true,
		RefEventID: "evt-1",
		RefMsgID:   "123",
	})
	if err != nil {
		t.Fatalf("SetSessionActivityFromAgentAPI error: %v", err)
	}

	activities, err := ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities error: %v", err)
	}
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	activity := activities[0]
	if activity.ActorID != ownerID || activity.ActorType != protocol.SessionActivityActorTypeHuman {
		t.Fatalf("unexpected actor identity: %+v", activity)
	}
	if activity.ExecutorID != agentID || activity.ExecutorType != protocol.SessionActivityActorTypeAgent {
		t.Fatalf("unexpected executor identity: %+v", activity)
	}
	if activity.Source != protocol.SessionActivitySourceAgentAPI {
		t.Fatalf("unexpected source: %+v", activity)
	}
}

func TestSetSessionActivityFromAgentAPIAllowsCurrentRefEvent(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		sessionID = "session-activity-current-ref"
		ownerID   = int64(2151)
		peerID    = int64(2152)
		agentID   = int64(9151)
		refEvent  = "evt-current"
	)

	mustCreateSessionActivityFixture(t, sessionID, ownerID, peerID, agentID)
	if err := store.RDB.HSet(context.Background(),
		"im:delegate:"+sessionID+":2151",
		"agent_id", "9151",
	).Err(); err != nil {
		t.Fatalf("seed delegate state error: %v", err)
	}

	prevCheck := isCurrentAgentAPIRefEvent
	isCurrentAgentAPIRefEvent = func(owner int64, session string, event string) bool {
		return owner == ownerID && session == sessionID && event == refEvent
	}
	defer func() {
		isCurrentAgentAPIRefEvent = prevCheck
	}()

	hub := &sessionActivityMockHub{nodeID: "node-test", conns: map[int64][]ConnInterface{}}
	err := SetSessionActivityFromAgentAPI(context.Background(), hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID:  sessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     true,
		RefEventID: refEvent,
	})
	if err != nil {
		t.Fatalf("SetSessionActivityFromAgentAPI error: %v", err)
	}

	activities, err := ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities error: %v", err)
	}
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].RefEventID != refEvent {
		t.Fatalf("activity ref_event_id=%q want=%q", activities[0].RefEventID, refEvent)
	}
}

func TestSetSessionActivityFromAgentAPICurrentStopRefEventClearsActivity(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		sessionID  = "session-activity-current-stop-ref"
		ownerID    = int64(2171)
		peerID     = int64(2172)
		agentID    = int64(9171)
		currentRef = "evt-current-stop-clear"
	)

	mustCreateSessionActivityFixture(t, sessionID, ownerID, peerID, agentID)
	if err := store.RDB.HSet(context.Background(),
		"im:delegate:"+sessionID+":2171",
		"agent_id", "9171",
	).Err(); err != nil {
		t.Fatalf("seed delegate state error: %v", err)
	}

	prevCheck := isCurrentAgentAPIRefEvent
	isCurrentAgentAPIRefEvent = func(owner int64, session string, event string) bool {
		return owner == ownerID && session == sessionID && event == currentRef
	}
	defer func() {
		isCurrentAgentAPIRefEvent = prevCheck
	}()

	peerConn := &sessionActivityMockConn{userID: peerID, deviceID: "peer-dev"}
	hub := &sessionActivityMockHub{
		nodeID: "node-test",
		conns: map[int64][]ConnInterface{
			ownerID: {},
			peerID:  {peerConn},
		},
	}

	if err := UpsertSessionActivity(context.Background(), hub, protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         protocol.SessionActivityKindComposing,
		ActorID:      ownerID,
		ActorType:    protocol.SessionActivityActorTypeHuman,
		ExecutorID:   agentID,
		ExecutorType: protocol.SessionActivityActorTypeAgent,
		Source:       protocol.SessionActivitySourceAgentAPI,
		RefEventID:   currentRef,
	}); err != nil {
		t.Fatalf("seed activity error: %v", err)
	}
	peerConn.sent = nil

	err := SetSessionActivityFromAgentAPI(context.Background(), hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID:  sessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     false,
		RefEventID: currentRef,
	})
	if err != nil {
		t.Fatalf("SetSessionActivityFromAgentAPI error: %v", err)
	}

	activities, err := ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities error: %v", err)
	}
	if len(activities) != 0 {
		t.Fatalf("expected current composing activity to be cleared, got %d", len(activities))
	}
	if len(peerConn.sent) != 1 {
		t.Fatalf("expected one clear broadcast, got %d", len(peerConn.sent))
	}
	payload, ok := peerConn.sent[0].payload.(protocol.SessionActivityPayload)
	if !ok {
		t.Fatalf("unexpected payload type %T", peerConn.sent[0].payload)
	}
	if payload.Active {
		t.Fatalf("expected inactive payload, got %+v", payload)
	}
	if payload.RefEventID != currentRef {
		t.Fatalf("payload ref_event_id=%q want=%q", payload.RefEventID, currentRef)
	}
}

func TestHandleSessionActivitySetRejectsMutedComposing(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		sessionID = "session-activity-muted-composing"
		ownerID   = int64(2201)
		memberID  = int64(2202)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:       sessionID,
		OwnerID:         ownerID,
		SessionType:     model.SessionTypeGroup,
		AllMembersMuted: true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
	}
	for _, member := range members {
		member := member
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	conn := &sessionActivityMockConn{userID: memberID, deviceID: "member-dev"}
	peerConn := &sessionActivityMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sessionActivityMockHub{
		nodeID: "node-test",
		conns: map[int64][]ConnInterface{
			memberID: {conn},
			ownerID:  {peerConn},
		},
	}

	setPayload, _ := json.Marshal(protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    true,
	})
	HandleSessionActivitySet(hub, conn, &protocol.Packet{
		Cmd:     protocol.CmdSessionActivitySet,
		Seq:     21,
		Payload: setPayload,
	})

	activities, err := ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities error: %v", err)
	}
	if len(activities) != 0 {
		t.Fatalf("expected muted composing to be rejected, got=%d", len(activities))
	}
	if len(peerConn.sent) != 0 {
		t.Fatalf("muted composing should not broadcast, got=%#v", peerConn.sent)
	}
}

func TestSetSessionActivityFromAgentAPIRejectsMutedDelegateIdentity(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		sessionID = "session-activity-agent-muted"
		ownerID   = int64(2301)
		peerID    = int64(2302)
		agentID   = int64(9301)
	)

	now := time.Now()
	users := []model.User{
		{ID: ownerID, Username: "owner", Email: "owner@example.com", Nickname: "owner", Status: model.UserStatusActive},
		{ID: peerID, Username: "peer", Email: "peer@example.com", Nickname: "peer", Status: model.UserStatusActive},
	}
	for _, user := range users {
		user := user
		if err := store.DB.Create(&user).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      ownerID,
		AgentName:    "agent",
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:       sessionID,
		OwnerID:         ownerID,
		SessionType:     model.SessionTypeGroup,
		AllMembersMuted: true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
	}
	for _, member := range members {
		member := member
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.RDB.HSet(
		context.Background(),
		"im:delegate:"+sessionID+":2301",
		"agent_id", "9301",
	).Err(); err != nil {
		t.Fatalf("seed delegate state error: %v", err)
	}

	hub := &sessionActivityMockHub{nodeID: "node-test", conns: map[int64][]ConnInterface{}}
	err := SetSessionActivityFromAgentAPI(context.Background(), hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    true,
	})
	if !errors.Is(err, sessionguard.ErrGroupAllMembersMuted) {
		t.Fatalf("expected ErrGroupAllMembersMuted, got=%v", err)
	}

	activities, listErr := ListSessionActivities(context.Background(), sessionID)
	if listErr != nil {
		t.Fatalf("ListSessionActivities error: %v", listErr)
	}
	if len(activities) != 0 {
		t.Fatalf("expected no activity after rejected agent composing, got=%d", len(activities))
	}
}

func TestClearSessionActivityIsIdempotent(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	sessionID := "session-activity-idempotent"
	user1 := int64(3001)
	user2 := int64(3002)
	mustCreateSessionActivityFixture(t, sessionID, user1, user2, 0)

	conn1 := &sessionActivityMockConn{userID: user1, deviceID: "dev-1"}
	conn2 := &sessionActivityMockConn{userID: user2, deviceID: "dev-2"}
	hub := &sessionActivityMockHub{
		nodeID: "node-test",
		conns: map[int64][]ConnInterface{
			user1: {conn1},
			user2: {conn2},
		},
	}

	activity := protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         protocol.SessionActivityKindComposing,
		ActorID:      user1,
		ActorType:    protocol.SessionActivityActorTypeHuman,
		ExecutorID:   user1,
		ExecutorType: protocol.SessionActivityActorTypeHuman,
		Source:       protocol.SessionActivitySourceHumanInput,
	}
	if err := UpsertSessionActivity(context.Background(), hub, activity); err != nil {
		t.Fatalf("UpsertSessionActivity error: %v", err)
	}
	conn2.sent = nil

	if err := ClearSessionActivity(context.Background(), hub, activity); err != nil {
		t.Fatalf("first ClearSessionActivity error: %v", err)
	}
	if len(conn2.sent) != 1 {
		t.Fatalf("expected one clear sync after first clear, got %d", len(conn2.sent))
	}

	if err := ClearSessionActivity(context.Background(), hub, activity); err != nil {
		t.Fatalf("second ClearSessionActivity error: %v", err)
	}
	if len(conn2.sent) != 1 {
		t.Fatalf("second clear should not broadcast again, got %d payload(s)", len(conn2.sent))
	}
}

func TestClearSessionActivityByRefEventID(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	sessionID := "session-activity-by-ref"
	user1 := int64(3201)
	user2 := int64(3202)
	mustCreateSessionActivityFixture(t, sessionID, user1, user2, 0)

	conn2 := &sessionActivityMockConn{userID: user2, deviceID: "dev-2"}
	hub := &sessionActivityMockHub{
		nodeID: "node-test",
		conns: map[int64][]ConnInterface{
			user1: {},
			user2: {conn2},
		},
	}

	first := protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         protocol.SessionActivityKindComposing,
		ActorID:      user1,
		ActorType:    protocol.SessionActivityActorTypeHuman,
		ExecutorID:   user1,
		ExecutorType: protocol.SessionActivityActorTypeHuman,
		Source:       protocol.SessionActivitySourceHumanInput,
		RefEventID:   "evt-clear-me",
	}
	second := protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         protocol.SessionActivityKindComposing,
		ActorID:      user2,
		ActorType:    protocol.SessionActivityActorTypeHuman,
		ExecutorID:   user2,
		ExecutorType: protocol.SessionActivityActorTypeHuman,
		Source:       protocol.SessionActivitySourceHumanInput,
		RefEventID:   "evt-keep-me",
	}
	if err := UpsertSessionActivity(context.Background(), hub, first); err != nil {
		t.Fatalf("upsert first activity error: %v", err)
	}
	if err := UpsertSessionActivity(context.Background(), hub, second); err != nil {
		t.Fatalf("upsert second activity error: %v", err)
	}
	conn2.sent = nil

	if err := ClearSessionActivityByRef(context.Background(), hub, sessionID, "", "evt-clear-me"); err != nil {
		t.Fatalf("ClearSessionActivityByRef error: %v", err)
	}

	activities, err := ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities error: %v", err)
	}
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity after clear-by-ref, got %d", len(activities))
	}
	if activities[0].RefEventID != "evt-keep-me" {
		t.Fatalf("unexpected remaining activity=%+v", activities[0])
	}
	if len(conn2.sent) != 1 {
		t.Fatalf("expected one inactive sync for cleared ref, got %d", len(conn2.sent))
	}
	payload, ok := conn2.sent[0].payload.(protocol.SessionActivityPayload)
	if !ok {
		t.Fatalf("unexpected payload type %T", conn2.sent[0].payload)
	}
	if payload.Active {
		t.Fatalf("expected cleared activity payload to be inactive, got %+v", payload)
	}
	if payload.RefEventID != "evt-clear-me" {
		t.Fatalf("clear payload ref_event_id=%q want=%q", payload.RefEventID, "evt-clear-me")
	}
}

func TestResolveSessionViewingUsers(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	sessionID := "session-viewing-1"
	user1 := int64(4101)
	user2 := int64(4102)
	mustCreateSessionActivityFixture(t, sessionID, user1, user2, 0)

	hub := &sessionActivityMockHub{nodeID: "node-test", conns: map[int64][]ConnInterface{}}
	viewing := protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         protocol.SessionActivityKindViewing,
		ActorID:      user1,
		ActorType:    protocol.SessionActivityActorTypeHuman,
		ExecutorID:   user1,
		ExecutorType: protocol.SessionActivityActorTypeHuman,
		Source:       protocol.SessionActivitySourceHumanInput,
	}
	if err := UpsertSessionActivity(context.Background(), hub, viewing); err != nil {
		t.Fatalf("UpsertSessionActivity viewing error: %v", err)
	}

	viewingMap := ResolveSessionViewingUsers(context.Background(), sessionID, []int64{user1, user2})
	if !viewingMap[user1] {
		t.Fatalf("expected user1 to be marked as viewing")
	}
	if viewingMap[user2] {
		t.Fatalf("expected user2 to not be marked as viewing")
	}
	if !IsSessionViewingActive(context.Background(), sessionID, user1) {
		t.Fatalf("IsSessionViewingActive should return true for user1")
	}
	if IsSessionViewingActive(context.Background(), sessionID, user2) {
		t.Fatalf("IsSessionViewingActive should return false for user2")
	}
}

func TestClearAgentComposingActivityBySession(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		sessionID = "session-activity-clear-agent-composing"
		ownerID   = int64(5001)
		peerID    = int64(5002)
		agentID   = int64(9501)
	)
	mustCreateSessionActivityFixture(t, sessionID, ownerID, peerID, agentID)

	hub := &sessionActivityMockHub{nodeID: "node-test", conns: map[int64][]ConnInterface{}}

	// 1. direct mode: agent API composing recorded under the agent identity.
	if err := SetSessionActivityFromAgentAPI(context.Background(), hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    true,
	}); err != nil {
		t.Fatalf("SetSessionActivityFromAgentAPI direct error: %v", err)
	}

	if err := ClearAgentComposingActivityBySession(context.Background(), nil, sessionID); err != nil {
		t.Fatalf("ClearAgentComposingActivityBySession direct error: %v", err)
	}
	activities, err := ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities direct error: %v", err)
	}
	if len(activities) != 0 {
		t.Fatalf("expected no activities after direct clear, got %d: %+v", len(activities), activities)
	}

	// 2. delegate mode: agent API composing recorded under the owner identity.
	if err := store.RDB.HSet(context.Background(),
		"im:delegate:"+sessionID+":5001",
		"agent_id", "9501",
	).Err(); err != nil {
		t.Fatalf("seed delegate state error: %v", err)
	}
	if err := SetSessionActivityFromAgentAPI(context.Background(), hub, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    true,
	}); err != nil {
		t.Fatalf("SetSessionActivityFromAgentAPI delegate error: %v", err)
	}

	activities, err = ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities before delegate clear error: %v", err)
	}
	if len(activities) != 1 {
		t.Fatalf("expected 1 delegate activity, got %d", len(activities))
	}
	if activities[0].ActorID != ownerID {
		t.Fatalf("expected delegate activity actor_id=%d, got %d", ownerID, activities[0].ActorID)
	}

	if err := ClearAgentComposingActivityBySession(context.Background(), nil, sessionID); err != nil {
		t.Fatalf("ClearAgentComposingActivityBySession delegate error: %v", err)
	}
	activities, err = ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities delegate error: %v", err)
	}
	if len(activities) != 0 {
		t.Fatalf("expected no activities after delegate clear, got %d: %+v", len(activities), activities)
	}

	// 3. non-agent-API composing should not be cleared.
	ownerConn := &sessionActivityMockConn{userID: ownerID, deviceID: "owner-dev"}
	humanHub := &sessionActivityMockHub{
		nodeID: "node-test",
		conns: map[int64][]ConnInterface{
			ownerID: {ownerConn},
		},
	}
	setPayload, _ := json.Marshal(protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    true,
	})
	HandleSessionActivitySet(humanHub, ownerConn, &protocol.Packet{
		Cmd:     protocol.CmdSessionActivitySet,
		Seq:     1,
		Payload: setPayload,
	})

	if err := ClearAgentComposingActivityBySession(context.Background(), nil, sessionID); err != nil {
		t.Fatalf("ClearAgentComposingActivityBySession human error: %v", err)
	}
	activities, err = ListSessionActivities(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListSessionActivities human error: %v", err)
	}
	if len(activities) != 1 {
		t.Fatalf("expected human composing to remain, got %d activities", len(activities))
	}
}

func TestSetSessionActivityFromAgentAPI_InactiveComposingClearsAcrossIdentityFlip(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		sessionID = "session-activity-inactive-identity-flip"
		ownerID   = int64(5101)
		peerID    = int64(5102)
		agentID   = int64(9601)
	)
	mustCreateSessionActivityFixture(t, sessionID, ownerID, peerID, agentID)

	if err := store.RDB.HSet(context.Background(),
		fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID),
		"agent_id", fmt.Sprintf("%d", agentID),
	).Err(); err != nil {
		t.Fatalf("seed delegate state error: %v", err)
	}
	if err := SetSessionActivityFromAgentAPI(context.Background(), nil, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    true,
	}); err != nil {
		t.Fatalf("set delegate composing: %v", err)
	}
	if !HasAgentComposingActivity(context.Background(), sessionID, agentID) {
		t.Fatal("expected delegate composing to be active")
	}

	// Delegate ends: subsequent ResolveIdentity would use the agent actor key,
	// which does not match the stored owner-actor composing entry.
	if err := store.RDB.Del(context.Background(), fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID)).Err(); err != nil {
		t.Fatalf("clear delegate state: %v", err)
	}

	if err := SetSessionActivityFromAgentAPI(context.Background(), nil, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    false,
	}); err != nil {
		t.Fatalf("inactive composing clear: %v", err)
	}
	if HasAgentComposingActivity(context.Background(), sessionID, agentID) {
		t.Fatal("inactive composing must clear agent_api composing even after identity flip")
	}
}

func TestSetSessionActivityFromAgentAPI_SuppressesComposingWhenQueueIdle(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		sessionID = "session-activity-queue-idle-suppress"
		ownerID   = int64(5201)
		peerID    = int64(5202)
		agentID   = int64(9701)
	)
	mustCreateSessionActivityFixture(t, sessionID, ownerID, peerID, agentID)

	if err := SetSessionActivityFromAgentAPI(context.Background(), nil, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    true,
	}); err != nil {
		t.Fatalf("seed composing: %v", err)
	}

	if err := store.RDB.Set(
		context.Background(),
		fmt.Sprintf("im:agent_api:queue_idle:%d:%s", ownerID, sessionID),
		"1",
		time.Hour,
	).Err(); err != nil {
		t.Fatalf("mark queue idle: %v", err)
	}
	if !wsagentapi.IsSessionQueueIdle(context.Background(), ownerID, sessionID) {
		t.Fatal("expected queue idle marker")
	}

	// Empty-queue clear path.
	if err := SetSessionActivityFromAgentAPI(context.Background(), nil, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    false,
	}); err != nil {
		t.Fatalf("clear composing: %v", err)
	}

	// Stale composing tick must not revive the indicator while queue is idle.
	if err := SetSessionActivityFromAgentAPI(context.Background(), nil, agentID, ownerID, protocol.SessionActivitySetPayload{
		SessionID: sessionID,
		Kind:      protocol.SessionActivityKindComposing,
		Active:    true,
		TTLMS:     30_000,
	}); err != nil {
		t.Fatalf("stale composing tick: %v", err)
	}
	if HasAgentComposingActivity(context.Background(), sessionID, agentID) {
		t.Fatal("queue-idle composing ticks must not recreate agent composing")
	}
}

func mustCreateSessionActivityFixture(t *testing.T, sessionID string, user1 int64, user2 int64, agentID int64) {
	t.Helper()

	users := []model.User{
		{ID: user1, Username: "user1", Email: "user1@example.com", Nickname: "user1", Status: model.UserStatusActive},
		{ID: user2, Username: "user2", Email: "user2@example.com", Nickname: "user2", Status: model.UserStatusActive},
	}
	for _, user := range users {
		if err := store.DB.Create(&user).Error; err != nil {
			t.Fatalf("create user error: %v", err)
		}
	}
	if agentID > 0 {
		agent := model.Agent{ID: agentID, OwnerID: user1, AgentName: "agent", ProviderType: model.AgentProviderAPI, Status: 1}
		if err := store.DB.Create(&agent).Error; err != nil {
			t.Fatalf("create agent error: %v", err)
		}
	}

	session := model.Session{SessionID: sessionID, OwnerID: user1, SessionType: 1}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: user1, MemberType: 1},
		{SessionID: sessionID, MemberID: user2, MemberType: 1},
	}
	if agentID > 0 {
		members = append(members, model.SessionMember{
			SessionID:  sessionID,
			MemberID:   agentID,
			MemberType: 2,
		})
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
}
