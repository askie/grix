package syncstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/syncstream/fold"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MaxBatchSize = 100

// NotifyDirty best-effort wakes every routed connection for the affected
// users. Correctness remains in PostgreSQL; Redis carries no cursor state.
func NotifyDirty(userIDs ...int64) {
	if !Enabled() || store.RDB == nil {
		return
	}
	ctx := context.Background()
	seenUsers := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		if _, exists := seenUsers[userID]; exists {
			continue
		}
		seenUsers[userID] = struct{}{}
		routes, err := store.RDB.HGetAll(ctx, fmt.Sprintf("im:ws:route:%d", userID)).Result()
		if err != nil {
			if logger.L != nil {
				logger.L.Warnf("syncstream: load routes user=%d: %v", userID, err)
			}
			continue
		}
		envelope, err := json.Marshal(map[string]any{"user_id": userID, "cmd": protocol.InternalCmdSyncV2Dirty, "payload": struct{}{}})
		if err != nil {
			continue
		}
		seenNodes := make(map[string]struct{}, len(routes))
		for _, nodeID := range routes {
			if nodeID == "" {
				continue
			}
			if _, exists := seenNodes[nodeID]; exists {
				continue
			}
			seenNodes[nodeID] = struct{}{}
			if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", nodeID), envelope).Err(); err != nil {
				if logger.L != nil {
					logger.L.Warnf("syncstream: publish dirty user=%d node=%s: %v", userID, nodeID, err)
				}
			}
		}
	}
}

func Enabled() bool {
	return strings.TrimSpace(os.Getenv("AIBOT_SYNC_V2_ENABLED")) == "1"
}

type Event struct {
	UserID        int64
	Kind          string
	EntityType    string
	EntityID      string
	EntityVersion int64
	Tombstone     bool
	CommandID     string
	Payload       any
	// Span is how many consecutive cursors the row reserves: one per part of
	// a compound message row. Zero means one.
	Span int
}

// maxSpan is a compound row's message, session and unread parts.
const maxSpan = 3

// ClaimCommandTx atomically claims an outbox command in the caller's business
// transaction. false,nil means an earlier attempt already committed.
func ClaimCommandTx(tx *gorm.DB, userID int64, kind, commandID string, response any) (bool, error) {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return true, nil
	}
	if tx == nil || userID <= 0 || strings.TrimSpace(kind) == "" || len(commandID) > 128 {
		return false, errors.New("syncstream: invalid command idempotency key")
	}
	raw, err := json.Marshal(response)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	receipt := model.SyncCommandReceipt{UserID: userID, CommandKind: kind, CommandID: commandID, Response: datatypes.JSON(raw), CreatedAt: now, UpdatedAt: now}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// AppendTx appends events and advances safe heads inside the caller's domain
// transaction. User head rows are locked in sorted order to prevent deadlocks
// and to make commit visibility follow cursor order. A row's stream_cursor is
// the last cursor of its span, so the head always lands on a row boundary.
func AppendTx(tx *gorm.DB, events []Event) ([]model.UserSyncEvent, error) {
	if tx == nil {
		return nil, errors.New("syncstream: nil transaction")
	}
	if len(events) == 0 {
		return nil, nil
	}

	userSet := make(map[int64]struct{}, len(events))
	for _, event := range events {
		if event.UserID <= 0 || strings.TrimSpace(event.Kind) == "" ||
			strings.TrimSpace(event.EntityType) == "" || strings.TrimSpace(event.EntityID) == "" {
			return nil, errors.New("syncstream: invalid event")
		}
		if event.Span < 0 || event.Span > maxSpan || (event.Span > 1 && event.Kind != "message.upsert") {
			return nil, errors.New("syncstream: invalid event span")
		}
		userSet[event.UserID] = struct{}{}
	}
	userIDs := make([]int64, 0, len(userSet))
	for userID := range userSet {
		userIDs = append(userIDs, userID)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })

	now := time.Now().UTC()
	seed := make([]model.UserSyncHead, 0, len(userIDs))
	for _, userID := range userIDs {
		seed = append(seed, model.UserSyncHead{UserID: userID, UpdatedAt: now})
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
		return nil, fmt.Errorf("syncstream: seed heads: %w", err)
	}

	var heads []model.UserSyncHead
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id IN ?", userIDs).
		Order("user_id ASC").
		Find(&heads).Error; err != nil {
		return nil, fmt.Errorf("syncstream: lock heads: %w", err)
	}
	if len(heads) != len(userIDs) {
		return nil, errors.New("syncstream: incomplete head lock")
	}
	next := make(map[int64]int64, len(heads))
	for _, head := range heads {
		next[head.UserID] = head.HeadCursor
	}

	rows := make([]model.UserSyncEvent, 0, len(events))
	for _, event := range events {
		payload, err := json.Marshal(event.Payload)
		if err != nil {
			return nil, fmt.Errorf("syncstream: marshal %s: %w", event.Kind, err)
		}
		span := max(event.Span, 1)
		// Readers derive a row's span from its payload; a disagreement would
		// shift every later cursor for clients without compound_v1.
		if event.Kind == "message.upsert" && event.EntityType == "message" {
			got, err := fold.Span(payload)
			if err != nil {
				return nil, fmt.Errorf("syncstream: compound message.upsert: %w", err)
			}
			if got != span {
				return nil, fmt.Errorf("syncstream: message.upsert payload spans %d cursors, event reserves %d", got, span)
			}
		}
		next[event.UserID] += int64(span)
		rows = append(rows, model.UserSyncEvent{
			UserID: event.UserID, StreamCursor: next[event.UserID],
			EventKind: event.Kind, EntityType: event.EntityType,
			EntityID: event.EntityID, EntityVersion: event.EntityVersion,
			Tombstone: event.Tombstone, CommandID: strings.TrimSpace(event.CommandID),
			Payload: datatypes.JSON(payload), CreatedAt: now,
		})
	}
	if err := tx.Create(&rows).Error; err != nil {
		return nil, fmt.Errorf("syncstream: append events: %w", err)
	}
	for _, userID := range userIDs {
		if err := tx.Model(&model.UserSyncHead{}).
			Where("user_id = ?", userID).
			Updates(map[string]any{"head_cursor": next[userID], "updated_at": now}).Error; err != nil {
			return nil, fmt.Errorf("syncstream: publish head user=%d: %w", userID, err)
		}
	}
	return rows, nil
}
