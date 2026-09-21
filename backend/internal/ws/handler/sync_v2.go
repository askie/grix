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
	cursor     int64
	pending    bool
	pendingTo  int64
	draining   bool
	dirty      bool
	closed     bool
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
	state := &syncV2State{generation: generation, cursor: payload.CommittedCursor, dirty: true}
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

func drainSyncV2(conn ConnInterface, state *syncV2State) {
	state.mu.Lock()
	if state.closed || state.pending || state.draining || !state.dirty || store.DB == nil {
		state.mu.Unlock()
		return
	}
	state.draining = true
	state.dirty = false
	from := state.cursor
	generation := state.generation
	state.mu.Unlock()

	var rows []model.UserSyncEvent
	if err := store.DB.Where("user_id = ? AND stream_cursor > ?", conn.GetUserID(), from).
		Order("stream_cursor ASC").Limit(syncstream.MaxBatchSize).Find(&rows).Error; err != nil {
		state.mu.Lock()
		state.draining = false
		state.dirty = true
		state.mu.Unlock()
		logger.L.Warnf("sync_v2 load batch user=%d cursor=%d: %v", conn.GetUserID(), from, err)
		return
	}
	// Read the publication head after the rows. Under PostgreSQL READ COMMITTED,
	// this ordering guarantees that every row visible to the first statement has
	// a visible head in the second; a writer that commits between the statements
	// merely makes has_more true and is drained after the ACK.
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
	next := from
	events := make([]protocol.SyncEventPayload, 0, len(rows))
	for _, row := range rows {
		next = row.StreamCursor
		events = append(events, protocol.SyncEventPayload{Cursor: row.StreamCursor, Kind: row.EventKind,
			EntityType: row.EntityType, EntityID: row.EntityID, EntityVersion: row.EntityVersion,
			Tombstone: row.Tombstone, CommandID: row.CommandID, Payload: json.RawMessage(row.Payload)})
	}
	if len(rows) == 0 && head.HeadCursor > next {
		// Sparse/filtered positions are committed by the server-provided scan
		// watermark, never inferred from event count.
		next = head.HeadCursor
	}
	hasMore := next < head.HeadCursor
	batch := protocol.SyncBatchPayload{Generation: generation, FromCursor: from,
		NextCursor: next, HeadCursor: head.HeadCursor, HasMore: hasMore, Events: events}
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
	state.dirty = state.dirty || hasMore
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
