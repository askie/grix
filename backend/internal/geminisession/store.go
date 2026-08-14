package geminisession

import (
	"context"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Snapshot struct {
	AgentID   int64
	SessionID string
	ModeID    string
	ModelID   string
}

func Load(ctx context.Context, agentID int64, sessionID string) (Snapshot, bool, error) {
	if store.DB == nil || agentID <= 0 {
		return Snapshot{}, false, nil
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return Snapshot{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var record model.GeminiSessionContext
	err := store.DB.WithContext(ctx).
		Where("agent_id = ? AND session_id = ?", agentID, normalizedSessionID).
		First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return Snapshot{}, false, nil
		}
		return Snapshot{}, false, err
	}
	return Snapshot{
		AgentID:   record.AgentID,
		SessionID: record.SessionID,
		ModeID:    strings.TrimSpace(record.ModeID),
		ModelID:   strings.TrimSpace(record.ModelID),
	}, true, nil
}

func Upsert(ctx context.Context, snapshot Snapshot) error {
	if store.DB == nil {
		return nil
	}
	if snapshot.AgentID <= 0 {
		return nil
	}
	snapshot.SessionID = strings.TrimSpace(snapshot.SessionID)
	if snapshot.SessionID == "" {
		return nil
	}
	snapshot.ModeID = strings.TrimSpace(snapshot.ModeID)
	snapshot.ModelID = strings.TrimSpace(snapshot.ModelID)
	if ctx == nil {
		ctx = context.Background()
	}

	if existing, ok, err := Load(ctx, snapshot.AgentID, snapshot.SessionID); err == nil && ok {
		if snapshot.ModeID == "" {
			snapshot.ModeID = existing.ModeID
		}
		if snapshot.ModelID == "" {
			snapshot.ModelID = existing.ModelID
		}
	}
	if snapshot.ModeID == "" && snapshot.ModelID == "" {
		return nil
	}

	record := model.GeminiSessionContext{
		AgentID:   snapshot.AgentID,
		SessionID: snapshot.SessionID,
		ModeID:    snapshot.ModeID,
		ModelID:   snapshot.ModelID,
	}
	return store.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "agent_id"},
			{Name: "session_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"mode_id",
			"model_id",
			"updated_at",
		}),
	}).Create(&record).Error
}
