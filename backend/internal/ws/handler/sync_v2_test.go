package handler

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/syncstream"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type syncV2TestConn struct {
	mu       sync.Mutex
	userID   int64
	deviceID string
	seq      int64
	mode     string
	sent     []*protocol.Packet
	wake     func()
	cleanup  func()
}

func (c *syncV2TestConn) SendPayload(cmd string, seq int64, payload interface{}) {
	raw, _ := json.Marshal(payload)
	c.SendPacket(&protocol.Packet{Cmd: cmd, Seq: seq, Payload: raw})
}
func (c *syncV2TestConn) SendPacket(pkt *protocol.Packet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, pkt)
}
func (c *syncV2TestConn) AckPush(int64)  {}
func (c *syncV2TestConn) NextSeq() int64 { c.seq++; return c.seq }
func (c *syncV2TestConn) Close() {
	if c.cleanup != nil {
		c.cleanup()
	}
}
func (c *syncV2TestConn) GetUserID() int64                      { return c.userID }
func (c *syncV2TestConn) GetDeviceID() string                   { return c.deviceID }
func (c *syncV2TestConn) GetPlatform() string                   { return "test" }
func (c *syncV2TestConn) SetAuth(int64, string, string, string) {}
func (c *syncV2TestConn) IsAuthed() bool                        { return true }
func (c *syncV2TestConn) SyncMode() string                      { return c.mode }
func (c *syncV2TestConn) SetSyncV2Hooks(wake, cleanup func())   { c.wake, c.cleanup = wake, cleanup }

func setupSyncV2DB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.UserSyncHead{}, &model.UserSyncEvent{}, &model.DeviceSyncCursor{}, &model.SessionMember{}); err != nil {
		t.Fatal(err)
	}
	old := store.DB
	oldRead := store.ReadDB
	store.DB = db
	store.ReadDB = db
	t.Cleanup(func() { store.DB, store.ReadDB = old, oldRead })
	t.Setenv("AIBOT_SYNC_V2_ENABLED", "1")
	return db
}

func syncPacket(t *testing.T, cmd string, payload any) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return &protocol.Packet{Cmd: cmd, Seq: 1, Payload: raw}
}

func latestBatch(t *testing.T, conn *syncV2TestConn) protocol.SyncBatchPayload {
	t.Helper()
	conn.mu.Lock()
	defer conn.mu.Unlock()
	for i := len(conn.sent) - 1; i >= 0; i-- {
		if conn.sent[i].Cmd == protocol.CmdSyncBatch {
			var b protocol.SyncBatchPayload
			if err := json.Unmarshal(conn.sent[i].Payload, &b); err != nil {
				t.Fatal(err)
			}
			return b
		}
	}
	t.Fatal("sync_batch not sent")
	return protocol.SyncBatchPayload{}
}

func TestSyncV2ResumeBatchAckAndReplay(t *testing.T) {
	db := setupSyncV2DB(t)
	userID := int64(41)
	if err := db.Create(&model.UserSyncHead{UserID: userID, HeadCursor: 3}).Error; err != nil {
		t.Fatal(err)
	}
	for _, cursor := range []int64{1, 3} {
		if err := db.Create(&model.UserSyncEvent{UserID: userID, StreamCursor: cursor, EventKind: "message.upsert", EntityType: "message", EntityID: fmt.Sprint(cursor), EntityVersion: cursor, Payload: datatypes.JSON([]byte(`{"ok":true}`))}).Error; err != nil {
			t.Fatal(err)
		}
	}

	first := &syncV2TestConn{userID: userID, deviceID: "device-a", mode: "v2"}
	HandleSyncResume(nil, first, syncPacket(t, protocol.CmdSyncResume, protocol.SyncResumePayload{Generation: "g1", CommittedCursor: 0}))
	batch := latestBatch(t, first)
	if batch.FromCursor != 0 || batch.NextCursor != 3 || batch.HeadCursor != 3 || len(batch.Events) != 2 || batch.HasMore {
		t.Fatalf("unexpected batch: %#v", batch)
	}
	first.wake()
	first.mu.Lock()
	sentBeforeAck := len(first.sent)
	first.mu.Unlock()
	if sentBeforeAck != 1 {
		t.Fatalf("more than one unacked batch: %d", sentBeforeAck)
	}

	// Disconnect-before-ACK replays from the client-owned committed cursor.
	replay := &syncV2TestConn{userID: userID, deviceID: "device-a", mode: "v2"}
	HandleSyncResume(nil, replay, syncPacket(t, protocol.CmdSyncResume, protocol.SyncResumePayload{Generation: "g2", CommittedCursor: 0}))
	if got := latestBatch(t, replay); got.NextCursor != 3 || len(got.Events) != 2 {
		t.Fatalf("replay mismatch: %#v", got)
	}
	HandleSyncAck(nil, replay, syncPacket(t, protocol.CmdSyncAck, protocol.SyncAckPayload{Generation: "g2", CommittedCursor: 3}))
	var cursor model.DeviceSyncCursor
	if err := db.First(&cursor, "user_id = ? AND device_id = ?", userID, "device-a").Error; err != nil {
		t.Fatal(err)
	}
	if cursor.CommittedCursor != 3 {
		t.Fatalf("committed cursor=%d", cursor.CommittedCursor)
	}
}

func TestSyncV2MultipleBatchesAndIndependentDevices(t *testing.T) {
	db := setupSyncV2DB(t)
	userID := int64(52)
	if err := db.Create(&model.UserSyncHead{UserID: userID, HeadCursor: 101}).Error; err != nil {
		t.Fatal(err)
	}
	rows := make([]model.UserSyncEvent, 0, 101)
	for i := int64(1); i <= 101; i++ {
		rows = append(rows, model.UserSyncEvent{UserID: userID, StreamCursor: i, EventKind: "session.upsert", EntityType: "session", EntityID: "s", EntityVersion: i, Payload: datatypes.JSON([]byte(`{}`))})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	a := &syncV2TestConn{userID: userID, deviceID: "a", mode: "v2"}
	HandleSyncResume(nil, a, syncPacket(t, protocol.CmdSyncResume, protocol.SyncResumePayload{Generation: "a1", CommittedCursor: 0}))
	first := latestBatch(t, a)
	if len(first.Events) != 100 || first.NextCursor != 100 || !first.HasMore {
		t.Fatalf("first batch: %#v", first)
	}
	HandleSyncAck(nil, a, syncPacket(t, protocol.CmdSyncAck, protocol.SyncAckPayload{Generation: "a1", CommittedCursor: 100}))
	second := latestBatch(t, a)
	if len(second.Events) != 1 || second.NextCursor != 101 || second.HasMore {
		t.Fatalf("second batch: %#v", second)
	}
	HandleSyncAck(nil, a, syncPacket(t, protocol.CmdSyncAck, protocol.SyncAckPayload{Generation: "a1", CommittedCursor: 101}))

	b := &syncV2TestConn{userID: userID, deviceID: "b", mode: "v2"}
	HandleSyncResume(nil, b, syncPacket(t, protocol.CmdSyncResume, protocol.SyncResumePayload{Generation: "b1", CommittedCursor: 50}))
	bb := latestBatch(t, b)
	if bb.FromCursor != 50 || bb.NextCursor != 101 || len(bb.Events) != 51 {
		t.Fatalf("device b batch: %#v", bb)
	}
	HandleSyncAck(nil, b, syncPacket(t, protocol.CmdSyncAck, protocol.SyncAckPayload{Generation: "b1", CommittedCursor: 101}))
	var states []model.DeviceSyncCursor
	if err := db.Where("user_id = ?", userID).Order("device_id").Find(&states).Error; err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 || states[0].DeviceID != "a" || states[1].DeviceID != "b" {
		t.Fatalf("device states: %#v", states)
	}

	// A new client generation owns its locally committed cursor. Server-side
	// diagnostic state must not retain the previous generation's higher ACK.
	a2 := &syncV2TestConn{userID: userID, deviceID: "a", mode: "v2"}
	HandleSyncResume(nil, a2, syncPacket(t, protocol.CmdSyncResume, protocol.SyncResumePayload{Generation: "a2", CommittedCursor: 50}))
	var reset model.DeviceSyncCursor
	if err := db.First(&reset, "user_id = ? AND device_id = ?", userID, "a").Error; err != nil {
		t.Fatal(err)
	}
	if reset.Generation != "a2" || reset.LastResumeCursor != 50 || reset.CommittedCursor != 50 {
		t.Fatalf("new generation cursor state: %#v", reset)
	}
}

func TestSyncV2RejectsMismatchedAck(t *testing.T) {
	db := setupSyncV2DB(t)
	if err := db.Create(&model.UserSyncHead{UserID: 9, HeadCursor: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserSyncEvent{UserID: 9, StreamCursor: 1, EventKind: "message.upsert", EntityType: "message", EntityID: "1", Payload: datatypes.JSON([]byte(`{}`))}).Error; err != nil {
		t.Fatal(err)
	}
	c := &syncV2TestConn{userID: 9, deviceID: "d", mode: "v2"}
	HandleSyncResume(nil, c, syncPacket(t, protocol.CmdSyncResume, protocol.SyncResumePayload{Generation: "g", CommittedCursor: 0}))
	HandleSyncAck(nil, c, syncPacket(t, protocol.CmdSyncAck, protocol.SyncAckPayload{Generation: "stale", CommittedCursor: 1}))
	var state model.DeviceSyncCursor
	if err := db.First(&state, "user_id = ? AND device_id = ?", 9, "d").Error; err != nil {
		t.Fatal(err)
	}
	if state.CommittedCursor != 0 {
		t.Fatalf("stale ack advanced cursor=%d", state.CommittedCursor)
	}
}

func TestSyncV2RequiresNegotiationAndCleansUpOnClose(t *testing.T) {
	setupSyncV2DB(t)
	v1 := &syncV2TestConn{userID: 70, deviceID: "v1", mode: "v1"}
	HandleSyncResume(nil, v1, syncPacket(t, protocol.CmdSyncResume, protocol.SyncResumePayload{Generation: "g", CommittedCursor: 0}))
	if _, ok := syncV2States.Load(v1); ok {
		t.Fatal("v1 connection created sync_v2 state")
	}
	if len(v1.sent) != 1 || v1.sent[0].Cmd != protocol.CmdError {
		t.Fatalf("v1 resume response=%#v", v1.sent)
	}

	v2 := &syncV2TestConn{userID: 71, deviceID: "v2", mode: "v2"}
	HandleSyncResume(nil, v2, syncPacket(t, protocol.CmdSyncResume, protocol.SyncResumePayload{Generation: "g", CommittedCursor: 0}))
	if _, ok := syncV2States.Load(v2); !ok {
		t.Fatal("v2 resume did not create state")
	}
	v2.Close()
	if _, ok := syncV2States.Load(v2); ok {
		t.Fatal("closed connection retained sync_v2 state")
	}
}

func TestSyncV2RejectsResumeBeyondPublishedHead(t *testing.T) {
	setupSyncV2DB(t)
	c := &syncV2TestConn{userID: 73, deviceID: "ahead", mode: "v2"}
	HandleSyncResume(nil, c, syncPacket(t, protocol.CmdSyncResume, protocol.SyncResumePayload{Generation: "g", CommittedCursor: 1}))
	if _, ok := syncV2States.Load(c); ok {
		t.Fatal("invalid resume created sync state")
	}
	if len(c.sent) != 1 || c.sent[0].Cmd != protocol.CmdError {
		t.Fatalf("invalid resume response=%#v", c.sent)
	}
}

func TestSyncV2ReadsPrimaryWhenReplicaIsStale(t *testing.T) {
	db := setupSyncV2DB(t)
	stale, err := gorm.Open(sqlite.Open("file:"+t.Name()+"-stale?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := stale.AutoMigrate(&model.UserSyncHead{}, &model.UserSyncEvent{}, &model.DeviceSyncCursor{}, &model.SessionMember{}); err != nil {
		t.Fatal(err)
	}
	store.ReadDB = stale
	if err := db.Create(&model.UserSyncHead{UserID: 72, HeadCursor: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserSyncEvent{UserID: 72, StreamCursor: 1, EventKind: "message.upsert", EntityType: "message", EntityID: "primary", Payload: datatypes.JSON([]byte(`{}`))}).Error; err != nil {
		t.Fatal(err)
	}

	c := &syncV2TestConn{userID: 72, deviceID: "primary", mode: "v2"}
	HandleSyncResume(nil, c, syncPacket(t, protocol.CmdSyncResume, protocol.SyncResumePayload{Generation: "g", CommittedCursor: 0}))
	batch := latestBatch(t, c)
	if batch.HeadCursor != 1 || len(batch.Events) != 1 || batch.Events[0].EntityID != "primary" {
		t.Fatalf("batch did not come from primary: %#v", batch)
	}
}

func TestSyncV2CommitBetweenHeadAndRowsIsNotSkipped(t *testing.T) {
	db := setupSyncV2DB(t)
	const userID int64 = 74
	c := &syncV2TestConn{userID: userID, deviceID: "commit-race", mode: "v2"}
	state := &syncV2State{generation: "g", dirty: true}
	if err := recordSyncResume(userID, c.deviceID, state.generation, 0); err != nil {
		t.Fatal(err)
	}
	syncV2States.Store(c, state)
	t.Cleanup(func() { syncV2States.Delete(c) })

	drainSyncV2AfterHead(c, state, func() {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&model.UserSyncEvent{
				UserID: userID, StreamCursor: 1, EventKind: "message.upsert",
				EntityType: "message", EntityID: "committed-between-statements",
				EntityVersion: 1, Payload: datatypes.JSON([]byte(`{}`)),
			}).Error; err != nil {
				return err
			}
			return tx.Create(&model.UserSyncHead{UserID: userID, HeadCursor: 1}).Error
		}); err != nil {
			t.Fatal(err)
		}
	})

	first := latestBatch(t, c)
	if first.FromCursor != 0 || first.NextCursor != 0 || len(first.Events) != 0 {
		t.Fatalf("race batch advanced across unseen event: %#v", first)
	}
	HandleSyncAck(nil, c, syncPacket(t, protocol.CmdSyncAck, protocol.SyncAckPayload{
		Generation: "g", CommittedCursor: 0,
	}))
	second := latestBatch(t, c)
	if second.NextCursor != 1 || len(second.Events) != 1 || second.Events[0].EntityID != "committed-between-statements" {
		t.Fatalf("committed event was not replayed: %#v", second)
	}
}

// appendCompoundDeliveries writes one recipient delivery (message, session and
// unread) per message id through the production helper with compound rows on.
func appendCompoundDeliveries(t *testing.T, db *gorm.DB, userID int64, msgIDs ...int64) {
	t.Helper()
	t.Setenv("AIBOT_SYNC_COMPOUND_ENABLED", "1")
	deliveries := make([]syncstream.MessageDelivery, 0, len(msgIDs))
	for i, msgID := range msgIDs {
		version := int64(i + 1)
		session := model.Session{SessionID: "s1", SessionType: 1, StateVersion: version}
		member := model.SessionMember{SessionID: "s1", MemberID: userID, MemberType: 1, UnreadCount: i + 1, StateVersion: version}
		deliveries = append(deliveries, syncstream.MessageDelivery{UserID: userID, SessionID: "s1",
			Message: model.Message{MsgID: msgID, SessionID: "s1", Content: "hi", StateVersion: version}, Session: &session, Member: &member})
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := syncstream.AppendTx(tx, syncstream.MessageDeliveryEvents(deliveries...))
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func resumeBatch(t *testing.T, userID int64, device string, capabilities []string) (*syncV2TestConn, protocol.SyncBatchPayload) {
	t.Helper()
	conn := &syncV2TestConn{userID: userID, deviceID: device, mode: "v2"}
	HandleSyncResume(nil, conn, syncPacket(t, protocol.CmdSyncResume, protocol.SyncResumePayload{Generation: device, CommittedCursor: 0, Capabilities: capabilities}))
	return conn, latestBatch(t, conn)
}

func TestSyncV2ShapesBatchByCompoundCapability(t *testing.T) {
	db := setupSyncV2DB(t)
	userID := int64(61)
	// m1 is upserted again by the third delivery.
	appendCompoundDeliveries(t, db, userID, 1, 2, 1)
	compoundCaps := []string{"sync_v2", syncCapabilityCompoundV1}

	// Without compound_v1 a client gets the classic rows, one cursor each.
	_, classic := resumeBatch(t, userID, "classic", []string{"sync_v2"})
	if len(classic.Events) != 9 || classic.NextCursor != 9 || classic.HasMore {
		t.Fatalf("classic batch: %#v", classic)
	}
	for i, event := range classic.Events {
		if event.Cursor != int64(i+1) || event.FirstCursor != 0 {
			t.Fatalf("classic event %d: %+v", i, event)
		}
	}
	if classic.Events[0].Kind != "message.upsert" || classic.Events[1].Kind != "session.upsert" || classic.Events[2].Kind != "session.unread_set" {
		t.Fatalf("classic order: %+v", classic.Events[:3])
	}

	compoundConn, compound := resumeBatch(t, userID, "compound", compoundCaps)
	if len(compound.Events) != 3 || compound.NextCursor != 9 {
		t.Fatalf("compound batch: %#v", compound)
	}
	for i, event := range compound.Events {
		if event.FirstCursor != int64(i*3+1) || event.Cursor != int64(i*3+3) {
			t.Fatalf("compound event %d: %+v", i, event)
		}
	}
	HandleSyncAck(nil, compoundConn, syncPacket(t, protocol.CmdSyncAck, protocol.SyncAckPayload{Generation: "compound", CommittedCursor: 9}))

	// Folding applies only to compound_v1 connections; the page bound stays.
	t.Setenv("AIBOT_SYNC_REPLAY_FOLD_ENABLED", "1")
	_, foldedBatch := resumeBatch(t, userID, "folded", compoundCaps)
	if len(foldedBatch.Events) != 2 || foldedBatch.NextCursor != 9 || foldedBatch.HasMore {
		t.Fatalf("folded batch: %#v", foldedBatch)
	}
	if first := foldedBatch.Events[0]; first.EntityID != "2" || first.FirstCursor != 1 || first.Cursor != 4 {
		t.Fatalf("folded first event: %+v", first)
	}
	if last := foldedBatch.Events[1]; last.EntityID != "1" || last.FirstCursor != 5 || last.Cursor != 9 {
		t.Fatalf("folded last event: %+v", last)
	}
	_, classicAgain := resumeBatch(t, userID, "classic-again", nil)
	if len(classicAgain.Events) != 9 {
		t.Fatalf("classic batch must never be folded: %d events", len(classicAgain.Events))
	}
}

func TestSyncV2ClassicBatchCapsExpandedEvents(t *testing.T) {
	db := setupSyncV2DB(t)
	userID := int64(62)
	msgIDs := make([]int64, 0, 40)
	for i := int64(1); i <= 40; i++ {
		msgIDs = append(msgIDs, i)
	}
	appendCompoundDeliveries(t, db, userID, msgIDs...)

	conn, first := resumeBatch(t, userID, "classic", nil)
	if len(first.Events) != 99 || first.NextCursor != 99 || !first.HasMore {
		t.Fatalf("first classic batch events=%d next=%d more=%v", len(first.Events), first.NextCursor, first.HasMore)
	}
	HandleSyncAck(nil, conn, syncPacket(t, protocol.CmdSyncAck, protocol.SyncAckPayload{Generation: "classic", CommittedCursor: 99}))
	second := latestBatch(t, conn)
	if len(second.Events) != 21 || second.FromCursor != 99 || second.NextCursor != 120 || second.HasMore {
		t.Fatalf("second classic batch events=%d from=%d next=%d more=%v", len(second.Events), second.FromCursor, second.NextCursor, second.HasMore)
	}

	_, compound := resumeBatch(t, userID, "compound", []string{syncCapabilityCompoundV1})
	if len(compound.Events) != 40 || compound.NextCursor != 120 || compound.HasMore {
		t.Fatalf("compound batch events=%d next=%d more=%v", len(compound.Events), compound.NextCursor, compound.HasMore)
	}
}
