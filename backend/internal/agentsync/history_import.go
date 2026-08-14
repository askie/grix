package agentsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/inboxseq"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const sessionSummaryMaxRunes = 60

const (
	snowflakeEpochMillis = int64(1288834974657)
	snowflakeTimeShift   = uint(22)
	snowflakeLowMask     = int64(1<<22 - 1)
)

type SyncIdentity struct {
	AgentID     int64
	OwnerID     int64
	SessionID   string
	ProviderKey string
	BindingID   string
	SyncRunID   string
}

type NativeMessage struct {
	NativeMessageID string          `json:"native_message_id"`
	NativeParentID  string          `json:"native_parent_id,omitempty"`
	Role            string          `json:"role"`
	Content         string          `json:"content"`
	CreatedAt       time.Time       `json:"created_at"`
	MsgType         string          `json:"msg_type,omitempty"`
	Extra           json.RawMessage `json:"extra,omitempty"`
}

type ImportPageParams struct {
	SyncIdentity
	Messages []NativeMessage
	Cursor   string
}

func NewSyncRunID() string {
	return fmt.Sprintf("sync_%d", snowflake.GenID())
}

func Queue(ctx context.Context, ident SyncIdentity) error {
	return QueueAtCursor(ctx, ident, "")
}

func QueueAtCursor(ctx context.Context, ident SyncIdentity, cursor string) error {
	if err := validateIdentity(ident); err != nil {
		return err
	}
	now := time.Now().UTC()
	cursor = strings.TrimSpace(cursor)
	return store.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "agent_id"},
			{Name: "session_id"},
			{Name: "provider_key"},
			{Name: "binding_id"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"owner_id":     ident.OwnerID,
			"sync_run_id":  ident.SyncRunID,
			"status":       model.AgentSessionSyncStatusQueued,
			"cursor":       cursor,
			"last_error":   "",
			"imported":     0,
			"started_at":   nil,
			"completed_at": nil,
			"updated_at":   now,
		}),
	}).Create(&model.AgentSessionSyncState{
		AgentID:     ident.AgentID,
		OwnerID:     ident.OwnerID,
		SessionID:   strings.TrimSpace(ident.SessionID),
		ProviderKey: strings.TrimSpace(ident.ProviderKey),
		BindingID:   strings.TrimSpace(ident.BindingID),
		SyncRunID:   strings.TrimSpace(ident.SyncRunID),
		Status:      model.AgentSessionSyncStatusQueued,
		Cursor:      cursor,
	}).Error
}

func LoadCursor(ctx context.Context, ident SyncIdentity) (string, error) {
	if err := validateIdentity(ident); err != nil {
		return "", err
	}
	var state model.AgentSessionSyncState
	result := store.DB.WithContext(ctx).
		Where("agent_id = ? AND session_id = ? AND provider_key = ? AND binding_id = ?",
			ident.AgentID, strings.TrimSpace(ident.SessionID), strings.TrimSpace(ident.ProviderKey), strings.TrimSpace(ident.BindingID)).
		Limit(1).
		Find(&state)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", nil
	}
	return strings.TrimSpace(state.Cursor), nil
}

func MarkRunning(ctx context.Context, ident SyncIdentity, cursor string) error {
	return updateState(ctx, ident, map[string]interface{}{
		"status":       model.AgentSessionSyncStatusRunning,
		"cursor":       strings.TrimSpace(cursor),
		"last_error":   "",
		"started_at":   time.Now().UTC(),
		"completed_at": nil,
	})
}

func MarkCompleted(ctx context.Context, ident SyncIdentity, cursor string, imported int) error {
	now := time.Now().UTC()
	return updateState(ctx, ident, map[string]interface{}{
		"status":         model.AgentSessionSyncStatusCompleted,
		"cursor":         strings.TrimSpace(cursor),
		"last_error":     "",
		"imported":       imported,
		"last_synced_at": now,
		"completed_at":   now,
	})
}

func MarkPartial(ctx context.Context, ident SyncIdentity, cursor string, imported int) error {
	now := time.Now().UTC()
	return updateState(ctx, ident, map[string]interface{}{
		"status":         model.AgentSessionSyncStatusPartial,
		"cursor":         strings.TrimSpace(cursor),
		"last_error":     "",
		"imported":       imported,
		"last_synced_at": now,
	})
}

func MarkFailed(ctx context.Context, ident SyncIdentity, cursor string, imported int, err error) error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if len(msg) > 1024 {
		msg = msg[:1024]
	}
	return updateState(ctx, ident, map[string]interface{}{
		"status":     model.AgentSessionSyncStatusFailed,
		"cursor":     strings.TrimSpace(cursor),
		"last_error": msg,
		"imported":   imported,
	})
}

func ImportPage(ctx context.Context, params ImportPageParams) (int, error) {
	if err := validateIdentity(params.SyncIdentity); err != nil {
		return 0, err
	}
	if len(params.Messages) == 0 {
		return 0, nil
	}
	messages := normalizeMessages(params.Messages)
	if len(messages) == 0 {
		return 0, nil
	}

	imported := 0
	err := store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureSessionImportAllowed(tx, params.SyncIdentity); err != nil {
			return err
		}

		var humans []model.SessionMember
		if err := tx.Where("session_id = ? AND member_type = 1", params.SessionID).Find(&humans).Error; err != nil {
			return err
		}
		if len(humans) == 0 {
			return errors.New("session has no human members")
		}
		humanIDs := make([]int64, 0, len(humans))
		for _, member := range humans {
			humanIDs = append(humanIDs, member.MemberID)
		}

		var maxImportedMsg *model.Message
		for _, native := range messages {
			msgID, err := nextHistoricalMsgID(tx, strings.TrimSpace(params.SessionID), native)
			if err != nil {
				return err
			}
			insert := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "agent_id"},
					{Name: "provider_key"},
					{Name: "binding_id"},
					{Name: "native_message_id"},
				},
				DoNothing: true,
			}).Create(&model.AgentNativeMessageImport{
				AgentID:         params.AgentID,
				ProviderKey:     strings.TrimSpace(params.ProviderKey),
				BindingID:       strings.TrimSpace(params.BindingID),
				NativeMessageID: strings.TrimSpace(native.NativeMessageID),
				SessionID:       strings.TrimSpace(params.SessionID),
				MsgID:           msgID,
				NativeCreatedAt: native.CreatedAt.UTC(),
			})
			if insert.Error != nil {
				return insert.Error
			}
			if insert.RowsAffected == 0 {
				continue
			}

			extra, err := buildMessageExtra(params.SyncIdentity, native)
			if err != nil {
				return err
			}
			msg := model.Message{
				MsgID:      msgID,
				SessionID:  strings.TrimSpace(params.SessionID),
				SenderID:   senderIDForRole(params.OwnerID, params.AgentID, native.Role),
				SenderType: senderTypeForRole(native.Role),
				MsgType:    msgTypeForNative(native),
				Content:    strings.TrimSpace(native.Content),
				Extra:      datatypes.JSON(extra),
				CreatedAt:  native.CreatedAt.UTC(),
			}
			if err := tx.Create(&msg).Error; err != nil {
				return err
			}

			nextSeqByUser, err := inboxseq.AllocateNextBatchTx(ctx, tx, humanIDs)
			if err != nil {
				return fmt.Errorf("allocate import inbox_seq batch: %w", err)
			}
			for _, member := range humans {
				if err := tx.Create(&model.UserInbox{
					UserID:    member.MemberID,
					InboxSeq:  nextSeqByUser[member.MemberID],
					MsgID:     msgID,
					SessionID: strings.TrimSpace(params.SessionID),
					EventKind: model.UserInboxEventKindMessage,
				}).Error; err != nil {
					return err
				}
			}

			imported++
			if maxImportedMsg == nil || msg.MsgID > maxImportedMsg.MsgID {
				msgCopy := msg
				maxImportedMsg = &msgCopy
			}
		}

		if maxImportedMsg != nil {
			var session model.Session
			if err := tx.Where("session_id = ?", params.SessionID).First(&session).Error; err != nil {
				return err
			}
			if session.LastMsgID == nil || maxImportedMsg.MsgID > *session.LastMsgID {
				updates := map[string]interface{}{
					"last_msg_id": maxImportedMsg.MsgID,
					"updated_at":  time.Now().UTC(),
				}
				if summary := messageSummary(maxImportedMsg.Content); summary != "" {
					updates["last_msg_summary"] = summary
				}
				if err := tx.Model(&model.Session{}).
					Where("session_id = ?", params.SessionID).
					Updates(updates).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	return imported, err
}

func ValidateTarget(ctx context.Context, ident SyncIdentity) error {
	if err := validateIdentity(ident); err != nil {
		return err
	}
	return ensureSessionImportAllowed(store.DB.WithContext(ctx), ident)
}

func updateState(ctx context.Context, ident SyncIdentity, updates map[string]interface{}) error {
	if err := validateIdentity(ident); err != nil {
		return err
	}
	updates["updated_at"] = time.Now().UTC()
	result := store.DB.WithContext(ctx).Model(&model.AgentSessionSyncState{}).
		Where("agent_id = ? AND session_id = ? AND provider_key = ? AND binding_id = ?",
			ident.AgentID, strings.TrimSpace(ident.SessionID), strings.TrimSpace(ident.ProviderKey), strings.TrimSpace(ident.BindingID)).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		cursor, _ := updates["cursor"].(string)
		return QueueAtCursor(ctx, ident, cursor)
	}
	return nil
}

func validateIdentity(ident SyncIdentity) error {
	switch {
	case store.DB == nil:
		return errors.New("database unavailable")
	case ident.AgentID <= 0:
		return errors.New("agent_id required")
	case ident.OwnerID <= 0:
		return errors.New("owner_id required")
	case strings.TrimSpace(ident.SessionID) == "":
		return errors.New("session_id required")
	case strings.TrimSpace(ident.ProviderKey) == "":
		return errors.New("provider_key required")
	case strings.TrimSpace(ident.BindingID) == "":
		return errors.New("binding_id required")
	}
	return nil
}

func ensureSessionImportAllowed(tx *gorm.DB, ident SyncIdentity) error {
	var session model.Session
	if err := tx.Where("session_id = ? AND owner_id = ? AND is_deleted = false", ident.SessionID, ident.OwnerID).
		First(&session).Error; err != nil {
		return err
	}
	var agentMember model.SessionMember
	if err := tx.Where("session_id = ? AND member_id = ? AND member_type = 2", ident.SessionID, ident.AgentID).
		First(&agentMember).Error; err != nil {
		return err
	}
	return nil
}

func normalizeMessages(input []NativeMessage) []NativeMessage {
	out := make([]NativeMessage, 0, len(input))
	now := time.Now().UTC()
	for _, msg := range input {
		msg.NativeMessageID = strings.TrimSpace(msg.NativeMessageID)
		msg.Role = normalizeRole(msg.Role)
		msg.Content = strings.TrimSpace(msg.Content)
		if msg.NativeMessageID == "" || msg.Content == "" {
			continue
		}
		if msg.CreatedAt.IsZero() {
			msg.CreatedAt = now
			now = now.Add(time.Millisecond)
		}
		out = append(out, msg)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func nextHistoricalMsgID(tx *gorm.DB, sessionID string, msg NativeMessage) (int64, error) {
	base := historicalMsgIDBase(msg.CreatedAt, msg.NativeMessageID)
	for attempt := int64(0); attempt <= snowflakeLowMask; attempt++ {
		msgID := (base &^ snowflakeLowMask) | ((base + attempt) & snowflakeLowMask)
		var count int64
		if err := tx.Model(&model.Message{}).
			Where("session_id = ? AND msg_id = ?", sessionID, msgID).
			Count(&count).Error; err != nil {
			return 0, err
		}
		if count == 0 {
			return msgID, nil
		}
	}
	return 0, errors.New("unable to allocate historical message id")
}

func historicalMsgIDBase(createdAt time.Time, nativeMessageID string) int64 {
	createdMs := createdAt.UTC().UnixMilli()
	maxMs := time.Now().UTC().Add(-time.Millisecond).UnixMilli()
	if createdMs <= 0 || createdMs > maxMs {
		createdMs = maxMs
	}
	if createdMs < snowflakeEpochMillis {
		createdMs = snowflakeEpochMillis
	}
	timePart := (createdMs - snowflakeEpochMillis) << snowflakeTimeShift
	return timePart | (stableLowBits(nativeMessageID) & snowflakeLowMask)
}

func stableLowBits(value string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.TrimSpace(value)))
	return int64(h.Sum32()) & snowflakeLowMask
}

func buildMessageExtra(ident SyncIdentity, msg NativeMessage) ([]byte, error) {
	extra := map[string]interface{}{}
	if len(msg.Extra) > 0 {
		if err := json.Unmarshal(msg.Extra, &extra); err != nil {
			return nil, err
		}
	}
	extra["imported"] = true
	extra["import_source"] = "agent_session_history_sync"
	extra["provider_key"] = strings.TrimSpace(ident.ProviderKey)
	extra["binding_id"] = strings.TrimSpace(ident.BindingID)
	extra["native_message_id"] = strings.TrimSpace(msg.NativeMessageID)
	extra["native_parent_id"] = strings.TrimSpace(msg.NativeParentID)
	extra["native_role"] = normalizeRole(msg.Role)
	extra["native_created_at"] = msg.CreatedAt.UTC().Format(time.RFC3339Nano)
	return json.Marshal(extra)
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "human":
		return "user"
	case "assistant", "agent", "ai":
		return "assistant"
	case "system":
		return "system"
	case "tool":
		return "tool"
	default:
		return "assistant"
	}
}

func senderTypeForRole(role string) int16 {
	switch normalizeRole(role) {
	case "user":
		return 1
	case "assistant":
		return 2
	default:
		return 3
	}
}

func senderIDForRole(ownerID, agentID int64, role string) int64 {
	switch normalizeRole(role) {
	case "user":
		return ownerID
	case "assistant":
		return agentID
	default:
		return 0
	}
}

func msgTypeForNative(msg NativeMessage) int16 {
	switch strings.ToLower(strings.TrimSpace(msg.MsgType)) {
	case "system":
		return model.MsgTypeSystem
	case "image":
		return model.MsgTypeImage
	default:
		if normalizeRole(msg.Role) == "system" {
			return model.MsgTypeSystem
		}
		return model.MsgTypeText
	}
}

func messageSummary(content string) string {
	if textutil.IsStandaloneCardMessage(content) {
		return ""
	}
	return textutil.TruncateRunes(content, sessionSummaryMaxRunes)
}
