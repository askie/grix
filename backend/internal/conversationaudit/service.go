// Package conversationaudit owns authorization and metadata persistence for
// connector-backed, per-turn conversation audits.
package conversationaudit

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const FeatureGateKey = "conversation_audit"

var (
	ErrInvalidRequest = errors.New("invalid audit request")
	ErrNotFound       = errors.New("audit turn not found")
	ErrNotAudited     = errors.New("message is not an audited turn")
	ErrCorrelation    = errors.New("audit state correlation mismatch")
)

// StatePayload is the content-free lifecycle contract received from a
// connector's audit_state packet.
type StatePayload struct {
	EventID      string
	SessionID    string
	MsgID        int64
	AuditID      string
	TurnID       string
	State        string
	Revision     int
	Quality      string
	Truncated    bool
	ErrorCode    string
	ErrorMessage string
}

// TurnTarget is the safe, content-free choice shown when one user message was
// processed by more than one agent. The client may select only an ID returned
// from this list; audit coordinates stay server-resolved.
type TurnTarget struct {
	AgentID  int64
	State    string
	Revision int
}

// FeatureEnabled fails closed so an unavailable gate store never starts a
// sensitive audit capture.
func FeatureEnabled(userID int64) bool {
	if userID <= 0 {
		return false
	}
	features, err := featuregate.GetUserFeatures(userID)
	if err != nil {
		logger.L.Warnf("evaluate conversation_audit gate failed user=%d err=%v (fail-closed)", userID, err)
		return false
	}
	for _, feature := range features {
		if feature == FeatureGateKey {
			return true
		}
	}
	return false
}

// RequestedTurn validates the only client-selectable audit shape. Capture
// options deliberately stay connector-owned; this request only opts the next
// message into a single turn audit.
func RequestedTurn(extra json.RawMessage) (bool, error) {
	if len(extra) == 0 {
		return false, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(extra, &envelope); err != nil {
		return false, fmt.Errorf("%w: malformed extra", ErrInvalidRequest)
	}
	rawAudit, ok := envelope["audit"]
	if !ok {
		return false, nil
	}
	var audit struct {
		Enabled *bool  `json:"enabled"`
		Scope   string `json:"scope"`
	}
	if err := json.Unmarshal(rawAudit, &audit); err != nil || audit.Enabled == nil {
		return false, fmt.Errorf("%w: audit must contain boolean enabled", ErrInvalidRequest)
	}
	if !*audit.Enabled {
		return false, nil
	}
	if audit.Scope != "turn" {
		return false, fmt.Errorf("%w: audit scope must be turn", ErrInvalidRequest)
	}
	return true, nil
}

// RecordState persists a connector lifecycle update only when it belongs to a
// message originally marked as a user-requested audit turn and the owner still
// has the per-agent audit preference enabled.
func RecordState(agentID, ownerID int64, payload StatePayload) (*model.ConversationAuditTurn, error) {
	if agentID <= 0 || ownerID <= 0 || strings.TrimSpace(payload.EventID) == "" ||
		strings.TrimSpace(payload.SessionID) == "" || payload.MsgID <= 0 || !validState(payload.State) {
		return nil, ErrInvalidRequest
	}
	if payload.Revision < 0 {
		return nil, ErrInvalidRequest
	}
	payload.AuditID = strings.TrimSpace(payload.AuditID)
	payload.TurnID = strings.TrimSpace(payload.TurnID)
	payload.Quality = strings.TrimSpace(payload.Quality)
	payload.ErrorCode = strings.TrimSpace(payload.ErrorCode)
	payload.ErrorMessage = truncate(strings.TrimSpace(payload.ErrorMessage), 512)

	var result model.ConversationAuditTurn
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		var message model.Message
		if err := tx.Where("session_id = ? AND msg_id = ? AND sender_id = ? AND sender_type = 1", payload.SessionID, payload.MsgID, ownerID).
			First(&message).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCorrelation
			}
			return err
		}
		requested, err := RequestedTurn(json.RawMessage(message.Extra))
		if err != nil || !requested {
			return ErrNotAudited
		}
		// 审计标记是消息级的，捕获再按 (owner, agent) 持久化偏好逐个过滤：
		// 群聊里只有开关打开的 Agent 才会落 turn，伪造的 audit_state 同样被拒。
		enabled, err := auditEnabledTx(tx, ownerID, agentID)
		if err != nil {
			return err
		}
		if !enabled {
			return ErrNotAudited
		}

		var existing model.ConversationAuditTurn
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("agent_id = ? AND event_id = ?", agentID, payload.EventID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			result = model.ConversationAuditTurn{
				ID:      snowflake.GenID(),
				OwnerID: ownerID, AgentID: agentID, SessionID: payload.SessionID, MsgID: payload.MsgID,
				EventID: payload.EventID, AuditID: payload.AuditID, TurnID: payload.TurnID,
				State: payload.State, Revision: payload.Revision, Quality: payload.Quality,
				Truncated: payload.Truncated, ErrorCode: payload.ErrorCode, ErrorMessage: payload.ErrorMessage,
				CreatedAt: time.Now().UTC(),
			}
			return tx.Create(&result).Error
		}
		if err != nil {
			return err
		}
		if existing.OwnerID != ownerID || existing.SessionID != payload.SessionID || existing.MsgID != payload.MsgID ||
			(existing.AuditID != "" && payload.AuditID != "" && existing.AuditID != payload.AuditID) ||
			(existing.TurnID != "" && payload.TurnID != "" && existing.TurnID != payload.TurnID) {
			return ErrCorrelation
		}
		if !shouldApplyState(existing, payload) {
			result = existing
			return nil
		}
		updates := stateUpdates(existing, payload)
		if existing.AuditID == "" && payload.AuditID != "" {
			updates["audit_id"] = payload.AuditID
		}
		if existing.TurnID == "" && payload.TurnID != "" {
			updates["turn_id"] = payload.TurnID
		}
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", existing.ID).First(&result).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTurns returns every audited agent for a message, ordered deterministically.
// A user may only select one of these rows in a later replay query.
func ListTurns(ownerID int64, sessionID string, msgID int64) ([]model.ConversationAuditTurn, error) {
	if ownerID <= 0 || strings.TrimSpace(sessionID) == "" || msgID <= 0 {
		return nil, ErrInvalidRequest
	}
	var turns []model.ConversationAuditTurn
	if err := store.DB.Where("owner_id = ? AND session_id = ? AND msg_id = ?", ownerID, sessionID, msgID).
		Order("agent_id ASC").Find(&turns).Error; err != nil {
		return nil, err
	}
	if len(turns) == 0 {
		return nil, ErrNotFound
	}
	return turns, nil
}

// LookupTurn returns a requested agent's own audit metadata. It deliberately
// requires an agent ID rather than using updated_at ordering, because one
// message can legitimately produce independent audits in a multi-agent room.
func LookupTurn(ownerID int64, sessionID string, msgID, agentID int64) (*model.ConversationAuditTurn, error) {
	if ownerID <= 0 || strings.TrimSpace(sessionID) == "" || msgID <= 0 || agentID <= 0 {
		return nil, ErrInvalidRequest
	}
	var turn model.ConversationAuditTurn
	if err := store.DB.Where("owner_id = ? AND session_id = ? AND msg_id = ? AND agent_id = ?", ownerID, sessionID, msgID, agentID).
		First(&turn).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &turn, nil
}

// LookupTurnByAuditID returns audit metadata by immutable audit ID within one
// owner. Scoped Agent API callers under the same owner may query each other's
// audits; the stored AgentID identifies the connector that must serve replay.
func LookupTurnByAuditID(ownerID int64, auditID string) (*model.ConversationAuditTurn, error) {
	if ownerID <= 0 || strings.TrimSpace(auditID) == "" {
		return nil, ErrInvalidRequest
	}
	var turn model.ConversationAuditTurn
	if err := store.DB.Where("owner_id = ? AND audit_id = ?", ownerID, strings.TrimSpace(auditID)).
		First(&turn).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &turn, nil
}

// Targets maps persistence rows to the restricted UI selection surface.
func Targets(turns []model.ConversationAuditTurn) []TurnTarget {
	result := make([]TurnTarget, 0, len(turns))
	for _, turn := range turns {
		result = append(result, TurnTarget{AgentID: turn.AgentID, State: turn.State, Revision: turn.Revision})
	}
	return result
}

func validState(state string) bool {
	switch state {
	case "accepted", "recording", "finalizing", "ready", "partial", "failed":
		return true
	default:
		return false
	}
}

func stateRank(state string) int {
	switch state {
	case "accepted":
		return 1
	case "recording":
		return 2
	case "finalizing":
		return 3
	case "ready", "partial", "failed":
		return 4
	default:
		return 0
	}
}

func isTerminalState(state string) bool {
	return state == "ready" || state == "partial" || state == "failed"
}

// shouldApplyState makes delivery idempotent even when connector WebSocket
// packets are duplicated or reordered. Revisions are authoritative; for the
// same revision lifecycle order is monotonic and the first terminal result
// wins. A later revision may replace a prior terminal result.
func shouldApplyState(existing model.ConversationAuditTurn, incoming StatePayload) bool {
	if incoming.Revision < existing.Revision {
		return false
	}
	incomingRank := stateRank(incoming.State)
	existingRank := stateRank(existing.State)
	if incoming.Revision > existing.Revision {
		return incomingRank >= existingRank
	}
	if incomingRank < existingRank {
		return false
	}
	if isTerminalState(existing.State) && isTerminalState(incoming.State) && existing.State != incoming.State {
		return false
	}
	return true
}

func stateUpdates(existing model.ConversationAuditTurn, payload StatePayload) map[string]any {
	quality := payload.Quality
	truncated := payload.Truncated
	errorCode := payload.ErrorCode
	errorMessage := payload.ErrorMessage
	if payload.Revision == existing.Revision {
		if quality == "" {
			quality = existing.Quality
		}
		truncated = existing.Truncated || truncated
		if errorCode == "" {
			errorCode = existing.ErrorCode
		}
		if errorMessage == "" {
			errorMessage = existing.ErrorMessage
		}
	}
	return map[string]any{
		"state": payload.State, "revision": payload.Revision,
		"quality": quality, "truncated": truncated,
		"error_code": errorCode, "error_message": errorMessage,
	}
}

func truncate(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen]
}
