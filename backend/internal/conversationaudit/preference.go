package conversationaudit

import (
	"encoding/json"
	"errors"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetAuditEnabled returns the persisted per-(user, agent) audit toggle.
// A missing row means disabled.
func GetAuditEnabled(userID, agentID int64) (bool, error) {
	if userID <= 0 || agentID <= 0 {
		return false, nil
	}
	var pref model.ConversationAuditPref
	err := store.DB.Select("enabled").
		Where("user_id = ? AND agent_id = ?", userID, agentID).
		First(&pref).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return pref.Enabled, nil
}

// SetAuditEnabled upserts the per-(user, agent) audit toggle.
func SetAuditEnabled(userID, agentID int64, enabled bool) error {
	if userID <= 0 || agentID <= 0 {
		return ErrInvalidRequest
	}
	pref := model.ConversationAuditPref{
		UserID:  userID,
		AgentID: agentID,
		Enabled: enabled,
	}
	return store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "agent_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled", "updated_at"}),
	}).Create(&pref).Error
}

// SnapshotAuditEnabled resolves the toolbar-snapshot representation of the
// audit toggle: nil when the feature is unavailable for the user or the agent
// is unresolved, so the field stays absent from the wire payload (old-backend
// compatible). Lookup failures fail closed to nil as well — the toggle is
// display state, never a capture trigger.
func SnapshotAuditEnabled(userID, agentID int64) *bool {
	if userID <= 0 || agentID <= 0 || store.DB == nil || !FeatureEnabled(userID) {
		return nil
	}
	enabled, err := GetAuditEnabled(userID, agentID)
	if err != nil {
		logger.L.Warnf("load conversation audit pref failed user=%d agent=%d err=%v", userID, agentID, err)
		return nil
	}
	return &enabled
}

// AnyAgentEnabled reports whether the user has the audit toggle enabled for at
// least one of the given agents, in a single query.
func AnyAgentEnabled(userID int64, agentIDs []int64) (bool, error) {
	if userID <= 0 || len(agentIDs) == 0 {
		return false, nil
	}
	var count int64
	err := store.DB.Model(&model.ConversationAuditPref{}).
		Where("user_id = ? AND agent_id IN ? AND enabled = ?", userID, agentIDs, true).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ApplyTurnPreference rewrites an outbound message's extra so the audit-turn
// mark reflects only the server-persisted per-(user, agent) preference: any
// client-supplied audit key is discarded, and the mark is written only when
// the sender has the feature gate and at least one candidate agent enabled.
// All failures fail closed (mark absent) and never block the message.
func ApplyTurnPreference(extra json.RawMessage, userID int64, agentIDs []int64) json.RawMessage {
	envelope := map[string]json.RawMessage{}
	if len(extra) > 0 {
		if err := json.Unmarshal(extra, &envelope); err != nil {
			// 解析失败也必须保证客户端 audit 键不透传：整个 extra 已是畸形
			// JSON，无法可靠保留其他字段，直接丢弃（消息发送不受影响）。
			logger.L.Warnf("audit turn preference: malformed extra dropped user=%d err=%v", userID, err)
			return nil
		}
	}
	delete(envelope, "audit")
	if userID <= 0 || len(agentIDs) == 0 || store.DB == nil || !FeatureEnabled(userID) {
		return marshalTurnEnvelope(envelope)
	}
	enabled, err := AnyAgentEnabled(userID, agentIDs)
	if err != nil {
		logger.L.Warnf("audit turn preference lookup failed user=%d err=%v", userID, err)
		return marshalTurnEnvelope(envelope)
	}
	if enabled {
		envelope["audit"] = json.RawMessage(`{"enabled":true,"scope":"turn"}`)
	}
	return marshalTurnEnvelope(envelope)
}

func marshalTurnEnvelope(envelope map[string]json.RawMessage) json.RawMessage {
	if len(envelope) == 0 {
		return nil
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil
	}
	return raw
}

// auditEnabledTx reads the per-(user, agent) toggle inside an existing
// transaction; a missing row means disabled.
func auditEnabledTx(tx *gorm.DB, userID, agentID int64) (bool, error) {
	var pref model.ConversationAuditPref
	err := tx.Select("enabled").
		Where("user_id = ? AND agent_id = ?", userID, agentID).
		First(&pref).Error
	// no row => disabled by default
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return pref.Enabled, nil
}
