package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/syncstream"
	"github.com/askie/grix/backend/internal/syncstream/fold"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type syncV2Conn interface {
	ConnInterface
	SyncMode() string
	SetSyncV2Hooks(wake, cleanup func())
}

type syncV2State struct {
	mu         sync.Mutex
	generation string
	// compound is fixed per resume: the connection declared compound_v1.
	compound  bool
	cursor    int64
	pending   bool
	pendingTo int64
	draining  bool
	dirty     bool
	closed    bool
}

// syncCapabilityCompoundV1 lets a client receive compound message rows as one
// event, with first_cursor, and folded replay pages.
const syncCapabilityCompoundV1 = "compound_v1"

func hasSyncCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == want {
			return true
		}
	}
	return false
}

var syncV2States sync.Map

func HandleSyncResume(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	v2conn, ok := conn.(syncV2Conn)
	if !ok || v2conn.SyncMode() != "v2" || !syncstream.Enabled() {
		conn.SendPayload(protocol.CmdError, pkt.Seq, protocol.ErrorPayload{Code: 4001, Msg: "sync_v2 not negotiated"})
		return
	}
	var payload protocol.SyncResumePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil ||
		strings.TrimSpace(payload.Generation) == "" || len(strings.TrimSpace(payload.Generation)) > 64 || payload.CommittedCursor < 0 {
		conn.SendPayload(protocol.CmdError, pkt.Seq, protocol.ErrorPayload{Code: 4001, Msg: "invalid sync_resume"})
		return
	}
	generation := strings.TrimSpace(payload.Generation)
	if store.DB == nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, protocol.ErrorPayload{Code: 5001, Msg: "sync resume failed"})
		return
	}
	var head model.UserSyncHead
	err := store.DB.Where("user_id = ?", conn.GetUserID()).First(&head).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		conn.SendPayload(protocol.CmdError, pkt.Seq, protocol.ErrorPayload{Code: 5001, Msg: "sync resume failed"})
		return
	}
	if payload.CommittedCursor > head.HeadCursor {
		conn.SendPayload(protocol.CmdError, pkt.Seq, protocol.ErrorPayload{Code: 4091, Msg: "sync cursor exceeds server head"})
		return
	}
	state := &syncV2State{generation: generation, cursor: payload.CommittedCursor, dirty: true,
		compound: hasSyncCapability(payload.Capabilities, syncCapabilityCompoundV1)}
	if err := recordSyncResume(conn.GetUserID(), conn.GetDeviceID(), generation, payload.CommittedCursor); err != nil {
		logger.L.Warnf("sync_v2 resume state user=%d device=%s: %v", conn.GetUserID(), conn.GetDeviceID(), err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, protocol.ErrorPayload{Code: 5001, Msg: "sync resume failed"})
		return
	}
	syncV2States.Store(conn, state)
	v2conn.SetSyncV2Hooks(
		func() { markAndDrainSyncV2(conn, state) },
		func() { state.mu.Lock(); state.closed = true; state.mu.Unlock(); syncV2States.Delete(conn) },
	)
	drainSyncV2(conn, state)
}

func HandleSyncAck(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	value, ok := syncV2States.Load(conn)
	if !ok {
		conn.SendPayload(protocol.CmdError, pkt.Seq, protocol.ErrorPayload{Code: 4001, Msg: "sync_resume required"})
		return
	}
	state := value.(*syncV2State)
	var payload protocol.SyncAckPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, protocol.ErrorPayload{Code: 4001, Msg: "invalid sync_ack"})
		return
	}
	state.mu.Lock()
	if state.closed || payload.Generation != state.generation || !state.pending || payload.CommittedCursor != state.pendingTo {
		state.mu.Unlock()
		conn.SendPayload(protocol.CmdError, pkt.Seq, protocol.ErrorPayload{Code: 4091, Msg: "stale or unexpected sync_ack"})
		return
	}
	if err := recordSyncAck(conn.GetUserID(), conn.GetDeviceID(), state.generation, payload.CommittedCursor); err != nil {
		state.mu.Unlock()
		conn.SendPayload(protocol.CmdError, pkt.Seq, protocol.ErrorPayload{Code: 5001, Msg: "sync ack failed"})
		return
	}
	state.cursor = payload.CommittedCursor
	state.pending = false
	state.mu.Unlock()
	drainSyncV2(conn, state)
}

func markAndDrainSyncV2(conn ConnInterface, state *syncV2State) {
	state.mu.Lock()
	state.dirty = true
	state.mu.Unlock()
	drainSyncV2(conn, state)
}

// PollSyncV2 is the low-frequency safety net for a lost pub/sub wake-up. It is
// called by the existing client heartbeat and only drains when the primary
// head is ahead, so idle connections do not receive empty sync batches.
func PollSyncV2(conn ConnInterface) {
	value, ok := syncV2States.Load(conn)
	if !ok || store.DB == nil {
		return
	}
	state := value.(*syncV2State)
	state.mu.Lock()
	if state.closed || state.pending || state.draining {
		state.mu.Unlock()
		return
	}
	cursor := state.cursor
	state.mu.Unlock()

	var head model.UserSyncHead
	err := store.DB.Select("head_cursor").Where("user_id = ?", conn.GetUserID()).First(&head).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	if err != nil {
		logger.L.Warnf("sync_v2 heartbeat head user=%d: %v", conn.GetUserID(), err)
		return
	}
	if head.HeadCursor > cursor {
		markAndDrainSyncV2(conn, state)
	}
}

func drainSyncV2(conn ConnInterface, state *syncV2State) {
	drainSyncV2AfterHead(conn, state, nil)
}

// drainSyncV2AfterHead keeps the statement-boundary hook explicit so the
// READ COMMITTED commit-order race can be regression-tested without a global
// mutable test hook in production code.
func drainSyncV2AfterHead(conn ConnInterface, state *syncV2State, afterHead func()) {
	state.mu.Lock()
	if state.closed || state.pending || state.draining || !state.dirty || store.DB == nil {
		state.mu.Unlock()
		return
	}
	state.draining = true
	state.dirty = false
	from := state.cursor
	generation := state.generation
	compound := state.compound
	state.mu.Unlock()

	// Freeze the publication boundary before reading rows. PostgreSQL READ
	// COMMITTED gives each statement its own snapshot, so reading rows first and
	// head second can observe a commit only in the second statement and must
	// never advance across the newly committed event.
	var head model.UserSyncHead
	err := store.DB.Where("user_id = ?", conn.GetUserID()).First(&head).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		head = model.UserSyncHead{UserID: conn.GetUserID()}
	} else if err != nil {
		state.mu.Lock()
		state.draining = false
		state.dirty = true
		state.mu.Unlock()
		logger.L.Warnf("sync_v2 load head user=%d: %v", conn.GetUserID(), err)
		return
	}
	if afterHead != nil {
		afterHead()
	}
	var rows []model.UserSyncEvent
	if err := store.DB.Where("user_id = ? AND stream_cursor > ? AND stream_cursor <= ?", conn.GetUserID(), from, head.HeadCursor).
		Order("stream_cursor ASC").Limit(syncstream.MaxBatchSize).Find(&rows).Error; err != nil {
		state.mu.Lock()
		state.draining = false
		state.dirty = true
		state.mu.Unlock()
		logger.L.Warnf("sync_v2 load batch user=%d cursor=%d head=%d: %v", conn.GetUserID(), from, head.HeadCursor, err)
		return
	}
	page, err := fold.Page(rows, from, fold.Options{Compound: compound,
		Fold: compound && syncstream.ReplayFoldEnabled(), MaxEvents: syncstream.MaxBatchSize})
	if err != nil {
		state.mu.Lock()
		state.draining = false
		state.dirty = true
		state.mu.Unlock()
		logger.L.Warnf("sync_v2 shape batch user=%d cursor=%d: %v", conn.GetUserID(), from, err)
		return
	}
	next := page.NextCursor
	events := page.Events
	// A writer may commit after the frozen head was read. Rechecking the head is
	// only a wake-up hint: it may mark the connection dirty, but it never changes
	// this batch's cursor. That preserves the no-skip invariant even if the
	// pub/sub notification is lost.
	latestHead := head.HeadCursor
	var latest model.UserSyncHead
	if err := store.DB.Where("user_id = ?", conn.GetUserID()).First(&latest).Error; err == nil {
		latestHead = latest.HeadCursor
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.L.Warnf("sync_v2 recheck head user=%d: %v", conn.GetUserID(), err)
	}
	hasMore := next < latestHead
	batch := protocol.SyncBatchPayload{Generation: generation, FromCursor: from,
		NextCursor: next, HeadCursor: latestHead, HasMore: hasMore, Events: events}
	if !hasMore {
		snapshot, err := buildSyncFinalSnapshot(conn.GetUserID())
		if err != nil {
			state.mu.Lock()
			state.draining = false
			state.dirty = true
			state.mu.Unlock()
			logger.L.Warnf("sync_v2 load final snapshot user=%d: %v", conn.GetUserID(), err)
			return
		}
		batch.FinalStateSnapshot = snapshot
	}
	state.mu.Lock()
	state.draining = false
	if state.closed || state.generation != generation || state.cursor != from || state.pending {
		state.mu.Unlock()
		return
	}
	state.pending = true
	state.pendingTo = next
	state.dirty = state.dirty || hasMore || latestHead > next
	state.mu.Unlock()
	conn.SendPayload(protocol.CmdSyncBatch, conn.NextSeq(), batch)
}

func buildSyncFinalSnapshot(userID int64) (*protocol.SyncFinalStateSnapshot, error) {
	type row struct {
		SessionID   string
		UnreadCount int
	}
	var rows []row
	if err := store.DB.Model(&model.SessionMember{}).Select("session_id, unread_count").
		Where("member_id = ? AND member_type = 1 AND unread_count > 0 AND is_tombstone = false", userID).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := &protocol.SyncFinalStateSnapshot{UnreadBySession: make(map[string]int, len(rows))}
	for _, item := range rows {
		result.UnreadBySession[item.SessionID] = item.UnreadCount
	}
	return result, nil
}

func recordSyncResume(userID int64, deviceID, generation string, cursor int64) error {
	now := time.Now().UTC()
	row := model.DeviceSyncCursor{UserID: userID, DeviceID: deviceID, StreamName: model.UserSyncStreamName,
		Generation: generation, LastResumeCursor: cursor, CommittedCursor: cursor, UpdatedAt: now}
	return store.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "device_id"}, {Name: "stream_name"}},
		DoUpdates: clause.Assignments(map[string]any{"generation": generation, "last_resume_cursor": cursor, "committed_cursor": cursor, "updated_at": now})}).Create(&row).Error
}

func recordSyncAck(userID int64, deviceID, generation string, cursor int64) error {
	now := time.Now().UTC()
	return store.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.DeviceSyncCursor{}).
			Where("user_id = ? AND device_id = ? AND stream_name = ? AND generation = ?", userID, deviceID, model.UserSyncStreamName, generation).
			Updates(map[string]any{"committed_cursor": gorm.Expr("CASE WHEN committed_cursor < ? THEN ? ELSE committed_cursor END", cursor, cursor), "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("sync cursor generation is no longer active")
		}
		return nil
	})
}
