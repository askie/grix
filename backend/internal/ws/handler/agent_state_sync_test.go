package handler

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
)

type agentStateMockConn struct {
	userID int64
	seq    int64
	sent   []protocol.AgentStateSyncPayload
}

func (c *agentStateMockConn) SendPayload(cmd string, seq int64, payload interface{}) {
	if cmd != protocol.CmdAgentStateSync {
		return
	}
	if typed, ok := payload.(protocol.AgentStateSyncPayload); ok {
		c.sent = append(c.sent, typed)
	}
}

func (c *agentStateMockConn) SendPacket(pkt *protocol.Packet) {}
func (c *agentStateMockConn) AckPush(msgID int64)             {}
func (c *agentStateMockConn) Close()                          {}
func (c *agentStateMockConn) NextSeq() int64 {
	c.seq++
	return c.seq
}
func (c *agentStateMockConn) GetUserID() int64                                           { return c.userID }
func (c *agentStateMockConn) GetDeviceID() string                                        { return "dev-agent-state" }
func (c *agentStateMockConn) GetPlatform() string                                        { return "" }
func (c *agentStateMockConn) SetAuth(userID int64, sessionID, deviceID, platform string) {}
func (c *agentStateMockConn) IsAuthed() bool                                             { return true }

type afterHGetAllHook struct {
	once sync.Once
	fn   func()
}

func (h *afterHGetAllHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (h *afterHGetAllHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && cmd.Name() == "hgetall" {
			h.once.Do(h.fn)
		}
		return err
	}
}

func (h *afterHGetAllHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		return next(ctx, cmds)
	}
}

func TestLoadAgentStatesNormalizesExpiredLease(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		store.RDB.Close()
		store.RDB = previous
	}()

	nowMs := time.Now().UnixMilli()
	RecordAgentState(context.Background(), 1001, BuildAgentStatePayload(2001, protocol.AgentStateOnline, true, nowMs-1000))
	RecordAgentState(context.Background(), 1001, BuildAgentStatePayload(2002, protocol.AgentStateOnline, true, nowMs+30_000))

	states := LoadAgentStates(context.Background(), 1001)
	if len(states) != 2 {
		t.Fatalf("expected 2 states, got=%d", len(states))
	}
	if states[0].AgentID != 2001 || states[0].State != protocol.AgentStateOffline {
		t.Fatalf("expected expired agent to be offline, got=%#v", states[0])
	}
	if states[1].AgentID != 2002 || states[1].State != protocol.AgentStateOnline {
		t.Fatalf("expected live agent to stay online, got=%#v", states[1])
	}
}

func TestPushStoredAgentStates(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		store.RDB.Close()
		store.RDB = previous
	}()

	nowMs := time.Now().UnixMilli()
	RecordAgentState(context.Background(), 3001, BuildAgentStatePayload(4001, protocol.AgentStateOnline, true, nowMs+60_000))

	conn := &agentStateMockConn{userID: 3001}
	PushStoredAgentStates(conn)

	if len(conn.sent) != 1 {
		t.Fatalf("expected 1 pushed state, got=%d", len(conn.sent))
	}
	if conn.sent[0].AgentID != 4001 || conn.sent[0].State != protocol.AgentStateOnline {
		t.Fatalf("unexpected pushed state=%#v", conn.sent[0])
	}
}

func TestRecordAgentStateRejectsOlderConnectionEpoch(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		store.RDB.Close()
		store.RDB = previous
	}()

	const (
		ownerID   = int64(5001)
		agentID   = int64(6001)
		oldEpoch  = int64(1_700_000_000_000_000)
		newEpoch  = int64(1_700_000_000_000_100)
		leaseTill = int64(1_900_000_000_000)
	)

	if !RecordAgentState(
		context.Background(),
		ownerID,
		BuildAgentStatePayloadWithEpoch(agentID, protocol.AgentStateOnline, true, leaseTill, newEpoch),
	) {
		t.Fatal("new connection online state should be accepted")
	}
	if RecordAgentState(
		context.Background(),
		ownerID,
		BuildAgentStatePayloadWithEpoch(agentID, protocol.AgentStateOffline, false, 0, oldEpoch),
	) {
		t.Fatal("older connection offline state must be rejected")
	}

	states := LoadAgentStates(context.Background(), ownerID)
	if len(states) != 1 || states[0].State != protocol.AgentStateOnline {
		t.Fatalf("older disconnect overwrote newer online state: %#v", states)
	}
	var extra agentStateExtra
	if err := json.Unmarshal(states[0].Extra, &extra); err != nil {
		t.Fatalf("decode state extra: %v", err)
	}
	if extra.ConnectionEpoch != newEpoch {
		t.Fatalf("connection_epoch=%d want=%d", extra.ConnectionEpoch, newEpoch)
	}

	if !RecordAgentState(
		context.Background(),
		ownerID,
		BuildAgentStatePayloadWithEpoch(agentID, protocol.AgentStateOffline, false, 0, newEpoch),
	) {
		t.Fatal("same connection's final offline state should be accepted")
	}
	states = LoadAgentStates(context.Background(), ownerID)
	if len(states) != 1 || states[0].State != protocol.AgentStateOffline {
		t.Fatalf("same-epoch offline state not persisted: %#v", states)
	}
}

func TestRecordAgentStateLegacyEpochCompatibility(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		store.RDB.Close()
		store.RDB = previous
	}()

	const (
		ownerID = int64(5002)
		agentID = int64(6002)
	)
	if !RecordAgentState(
		context.Background(),
		ownerID,
		BuildAgentStatePayload(agentID, protocol.AgentStateOnline, true, time.Now().Add(time.Minute).UnixMilli()),
	) {
		t.Fatal("legacy online state should be accepted when no epoch is stored")
	}
	if !RecordAgentState(
		context.Background(),
		ownerID,
		BuildAgentStatePayload(agentID, protocol.AgentStateOffline, false, 0),
	) {
		t.Fatal("legacy offline state should remain last-write-wins")
	}

	states := LoadAgentStates(context.Background(), ownerID)
	if len(states) != 1 || states[0].State != protocol.AgentStateOffline {
		t.Fatalf("legacy state update not preserved: %#v", states)
	}
}

func TestReserveAgentConnectionEpochConcurrentUnique(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		store.RDB.Close()
		store.RDB = previous
	}()

	const (
		ownerID = int64(5010)
		agentID = int64(6010)
		workers = 32
	)
	start := make(chan struct{})
	results := make(chan int64, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			epoch, err := ReserveAgentConnectionEpoch(context.Background(), ownerID, agentID)
			if err != nil {
				errs <- err
				return
			}
			results <- epoch
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("reserve concurrent epoch: %v", err)
	}
	seen := make(map[int64]struct{}, workers)
	for epoch := range results {
		if _, duplicated := seen[epoch]; duplicated {
			t.Fatalf("duplicate connection epoch=%d", epoch)
		}
		seen[epoch] = struct{}{}
	}
	if len(seen) != workers {
		t.Fatalf("reserved epochs=%d want=%d", len(seen), workers)
	}
	for epoch := int64(1); epoch <= workers; epoch++ {
		if _, ok := seen[epoch]; !ok {
			t.Fatalf("missing monotonic connection epoch=%d; got=%v", epoch, seen)
		}
	}
}

func TestReserveAgentConnectionEpochDoesNotAdvanceAcceptedEpoch(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		store.RDB.Close()
		store.RDB = previous
	}()

	const (
		ownerID      = int64(5011)
		agentID      = int64(6011)
		accepted     = int64(1_700_000_000_000_123)
		expectedNext = accepted + 1
	)
	ctx := context.Background()
	livePayload := BuildAgentStatePayloadWithEpoch(
		agentID,
		protocol.AgentStateOnline,
		true,
		time.Now().Add(time.Minute).UnixMilli(),
		accepted,
	)
	if !RecordAgentState(ctx, ownerID, livePayload) {
		t.Fatal("seed accepted live generation")
	}

	reserved, err := ReserveAgentConnectionEpoch(ctx, ownerID, agentID)
	if err != nil {
		t.Fatalf("reserve connection epoch: %v", err)
	}
	if reserved != expectedNext {
		t.Fatalf("reserved epoch=%d want=%d", reserved, expectedNext)
	}

	field := strconv.FormatInt(agentID, 10)
	storedAccepted, err := store.RDB.HGet(ctx, agentStateEpochHashKey(ownerID), field).Int64()
	if err != nil {
		t.Fatalf("read accepted epoch: %v", err)
	}
	if storedAccepted != accepted {
		t.Fatalf("reservation advanced accepted epoch=%d want=%d", storedAccepted, accepted)
	}
	storedSequence, err := store.RDB.HGet(ctx, agentStateEpochSequenceHashKey(ownerID), field).Int64()
	if err != nil {
		t.Fatalf("read reserved sequence: %v", err)
	}
	if storedSequence != expectedNext {
		t.Fatalf("stored sequence=%d want=%d", storedSequence, expectedNext)
	}

	// Simulate the reserving process crashing before online publication. The
	// old live connection must still be allowed to renew its accepted epoch.
	livePayload.Extra = BuildAgentStatePayloadWithEpoch(
		agentID,
		protocol.AgentStateOnline,
		true,
		time.Now().Add(2*time.Minute).UnixMilli(),
		accepted,
	).Extra
	if !RecordAgentState(ctx, ownerID, livePayload) {
		t.Fatal("reservation must not suppress the previous live connection")
	}

	next, err := ReserveAgentConnectionEpoch(ctx, ownerID, agentID)
	if err != nil {
		t.Fatalf("reserve second connection epoch: %v", err)
	}
	if next != expectedNext+1 {
		t.Fatalf("second reserved epoch=%d want=%d", next, expectedNext+1)
	}
}

func TestReserveAgentConnectionEpochFailsWithoutRedis(t *testing.T) {
	previous := store.RDB
	store.RDB = nil
	defer func() {
		store.RDB = previous
	}()

	if epoch, err := ReserveAgentConnectionEpoch(context.Background(), 5012, 6012); err == nil {
		t.Fatalf("expected allocator failure without Redis, got epoch=%d", epoch)
	}
}

func TestLoadAgentStatesReadsBackAfterNormalizedStateIsRejected(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		store.RDB.Close()
		store.RDB = previous
	}()

	const (
		ownerID  = int64(5003)
		agentID  = int64(6003)
		oldEpoch = int64(1_700_000_000_000_000)
		newEpoch = int64(1_700_000_000_000_100)
	)
	ctx := context.Background()
	oldPayload := BuildAgentStatePayloadWithEpoch(
		agentID,
		protocol.AgentStateOnline,
		true,
		time.Now().Add(-time.Minute).UnixMilli(),
		oldEpoch,
	)
	if !RecordAgentState(ctx, ownerID, oldPayload) {
		t.Fatal("seed old online state")
	}

	writer := redis.NewClient(&redis.Options{Addr: store.RDB.Options().Addr})
	defer writer.Close()
	newPayload := BuildAgentStatePayloadWithEpoch(
		agentID,
		protocol.AgentStateOnline,
		true,
		time.Now().Add(time.Minute).UnixMilli(),
		newEpoch,
	)
	newRaw, err := json.Marshal(newPayload)
	if err != nil {
		t.Fatalf("marshal new state: %v", err)
	}
	store.RDB.AddHook(&afterHGetAllHook{fn: func() {
		pipe := writer.TxPipeline()
		pipe.HSet(ctx, agentStateHashKey(ownerID), "6003", newRaw)
		pipe.HSet(ctx, agentStateEpochHashKey(ownerID), "6003", newEpoch)
		if _, hookErr := pipe.Exec(ctx); hookErr != nil {
			panic(hookErr)
		}
	}})

	states := LoadAgentStates(ctx, ownerID)
	if len(states) != 1 {
		t.Fatalf("expected one state, got=%d", len(states))
	}
	if states[0].State != protocol.AgentStateOnline {
		t.Fatalf("stale normalized offline leaked to caller: %#v", states[0])
	}
	var extra agentStateExtra
	if err := json.Unmarshal(states[0].Extra, &extra); err != nil {
		t.Fatalf("decode returned state: %v", err)
	}
	if extra.ConnectionEpoch != newEpoch {
		t.Fatalf("returned epoch=%d want latest=%d", extra.ConnectionEpoch, newEpoch)
	}
}

func TestLoadAgentStatesDoesNotOverwriteSameEpochLeaseRefresh(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		store.RDB.Close()
		store.RDB = previous
	}()

	const (
		ownerID = int64(5004)
		agentID = int64(6004)
		epoch   = int64(1_700_000_000_000_200)
	)
	ctx := context.Background()
	expiredPayload := BuildAgentStatePayloadWithEpoch(
		agentID,
		protocol.AgentStateOnline,
		true,
		time.Now().Add(-time.Minute).UnixMilli(),
		epoch,
	)
	if !RecordAgentState(ctx, ownerID, expiredPayload) {
		t.Fatal("seed expired online state")
	}

	writer := redis.NewClient(&redis.Options{Addr: store.RDB.Options().Addr})
	defer writer.Close()
	refreshedPayload := BuildAgentStatePayloadWithEpoch(
		agentID,
		protocol.AgentStateOnline,
		true,
		time.Now().Add(time.Minute).UnixMilli(),
		epoch,
	)
	refreshedRaw, err := json.Marshal(refreshedPayload)
	if err != nil {
		t.Fatalf("marshal refreshed state: %v", err)
	}
	store.RDB.AddHook(&afterHGetAllHook{fn: func() {
		if hookErr := writer.HSet(
			ctx,
			agentStateHashKey(ownerID),
			"6004",
			refreshedRaw,
		).Err(); hookErr != nil {
			panic(hookErr)
		}
	}})

	states := LoadAgentStates(ctx, ownerID)
	if len(states) != 1 {
		t.Fatalf("expected one state, got=%d", len(states))
	}
	if states[0].State != protocol.AgentStateOnline {
		t.Fatalf("same-epoch refreshed lease was overwritten: %#v", states[0])
	}
	var extra agentStateExtra
	if err := json.Unmarshal(states[0].Extra, &extra); err != nil {
		t.Fatalf("decode returned state: %v", err)
	}
	if extra.ConnectionEpoch != epoch {
		t.Fatalf("returned epoch=%d want=%d", extra.ConnectionEpoch, epoch)
	}
	if extra.LeaseUntil <= time.Now().UnixMilli() {
		t.Fatalf("returned stale lease_until=%d", extra.LeaseUntil)
	}
}
