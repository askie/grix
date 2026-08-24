package store

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	appstore "github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm/clause"
)

type BindingRecord struct {
	AgentID      int64
	SessionID    string
	ProviderKey  string
	BindingID    string
	Cwd          string
	Status       string
	WorkerStatus string
	Meta         map[string]any
}

func LoadBinding(ctx context.Context, agentID int64, sessionID string) (BindingRecord, bool, error) {
	if appstore.DB == nil || agentID <= 0 || strings.TrimSpace(sessionID) == "" {
		return BindingRecord{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var records []model.AgentSessionBinding
	err := appstore.DB.WithContext(ctx).
		Where("agent_id = ? AND session_id = ?", agentID, strings.TrimSpace(sessionID)).
		Limit(1).
		Find(&records).Error
	if err != nil {
		return BindingRecord{}, false, err
	}
	if len(records) == 0 {
		return BindingRecord{}, false, nil
	}
	record := records[0]
	out := BindingRecord{
		AgentID:      record.AgentID,
		SessionID:    record.SessionID,
		ProviderKey:  strings.TrimSpace(record.ProviderKey),
		BindingID:    strings.TrimSpace(record.BindingID),
		Cwd:          strings.TrimSpace(record.Cwd),
		Status:       strings.TrimSpace(record.Status),
		WorkerStatus: strings.TrimSpace(record.WorkerStatus),
	}
	if len(record.MetaJSON) > 0 {
		_ = json.Unmarshal(record.MetaJSON, &out.Meta)
	}
	return out, true, nil
}

// ListSyncableBindingsBySession returns every binding of the session that
// carries a provider-native session identity, regardless of binding status.
// History import must not depend on the status string: toolbar local-action
// results overwrite it with arbitrary outcomes ("opened", "model_set", ...),
// which used to silently stop an in-flight import. The actual import gate is
// the agentsync state row, not the binding status.
func ListSyncableBindingsBySession(ctx context.Context, sessionID string) ([]BindingRecord, error) {
	if appstore.DB == nil || strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var records []model.AgentSessionBinding
	if err := appstore.DB.WithContext(ctx).
		Where("session_id = ?", strings.TrimSpace(sessionID)).
		Order("agent_id ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]BindingRecord, 0, len(records))
	for _, record := range records {
		binding := bindingRecordFromModel(record)
		if binding.BindingID == "" || binding.ProviderKey == "" {
			continue
		}
		out = append(out, binding)
	}
	return out, nil
}

func LoadBindingByProvider(ctx context.Context, agentID int64, providerKey string, bindingID string) (BindingRecord, bool, error) {
	if appstore.DB == nil || agentID <= 0 || strings.TrimSpace(providerKey) == "" || strings.TrimSpace(bindingID) == "" {
		return BindingRecord{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var records []model.AgentSessionBinding
	err := appstore.DB.WithContext(ctx).
		Where("agent_id = ? AND provider_key = ? AND binding_id = ? AND status <> ?", agentID, strings.TrimSpace(providerKey), strings.TrimSpace(bindingID), "deleted").
		Order("updated_at DESC").
		Limit(1).
		Find(&records).Error
	if err != nil {
		return BindingRecord{}, false, err
	}
	if len(records) == 0 {
		return BindingRecord{}, false, nil
	}
	return bindingRecordFromModel(records[0]), true, nil
}

func bindingRecordFromModel(record model.AgentSessionBinding) BindingRecord {
	out := BindingRecord{
		AgentID:      record.AgentID,
		SessionID:    record.SessionID,
		ProviderKey:  strings.TrimSpace(record.ProviderKey),
		BindingID:    strings.TrimSpace(record.BindingID),
		Cwd:          strings.TrimSpace(record.Cwd),
		Status:       strings.TrimSpace(record.Status),
		WorkerStatus: strings.TrimSpace(record.WorkerStatus),
	}
	if len(record.MetaJSON) > 0 {
		_ = json.Unmarshal(record.MetaJSON, &out.Meta)
	}
	return out
}

func UpsertBinding(ctx context.Context, record BindingRecord) error {
	if appstore.DB == nil || record.AgentID <= 0 || strings.TrimSpace(record.SessionID) == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	meta := record.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	dbRecord := model.AgentSessionBinding{
		AgentID:      record.AgentID,
		SessionID:    strings.TrimSpace(record.SessionID),
		ProviderKey:  strings.TrimSpace(record.ProviderKey),
		BindingID:    strings.TrimSpace(record.BindingID),
		Cwd:          strings.TrimSpace(record.Cwd),
		Status:       strings.TrimSpace(record.Status),
		WorkerStatus: strings.TrimSpace(record.WorkerStatus),
		MetaJSON:     metaJSON,
	}
	return appstore.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "agent_id"}, {Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"provider_key",
			"binding_id",
			"cwd",
			"status",
			"worker_status",
			"meta_json",
			"updated_at",
		}),
	}).Create(&dbRecord).Error
}
