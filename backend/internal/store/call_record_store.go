package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CallRecordStore 实现 call.Persister 接口。
type CallRecordStore struct {
	db *gorm.DB
}

func (s *CallRecordStore) UpdateHandover(ctx context.Context, callID int64, event model.CallHandoverEvent, state int16, delegationMode string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec model.CallRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "handover_events").
			Where("id = ?", callID).First(&rec).Error; err != nil {
			return err
		}
		events := make([]model.CallHandoverEvent, 0, 1)
		if len(rec.HandoverEvents) > 0 {
			_ = json.Unmarshal(rec.HandoverEvents, &events)
		}
		events = append(events, event)
		raw, err := json.Marshal(events)
		if err != nil {
			return err
		}
		return tx.Model(&model.CallRecord{}).Where("id = ?", callID).Updates(map[string]any{
			"state":           state,
			"delegation_mode": delegationMode,
			"handover_events": string(raw), // JSONB 列需要 string，[]byte 会被 pgx 当 bytea 处理
		}).Error
	})
}

// NewCallRecordStore 创建 CallRecordStore。
func NewCallRecordStore(db *gorm.DB) *CallRecordStore {
	return &CallRecordStore{db: db}
}

func (s *CallRecordStore) Create(ctx context.Context, r *model.CallRecord) error {
	return s.db.WithContext(ctx).Create(r).Error
}

func (s *CallRecordStore) UpdateAnswered(ctx context.Context, callID int64, answeredAt time.Time) error {
	return s.db.WithContext(ctx).Model(&model.CallRecord{}).
		Where("id = ?", callID).
		Updates(map[string]any{
			"state":       model.CallStateActive,
			"answered_at": answeredAt,
		}).Error
}

func (s *CallRecordStore) UpdateAnsweredWithAI(ctx context.Context, callID, agentID int64, answeredAt time.Time) error {
	return s.db.WithContext(ctx).Model(&model.CallRecord{}).
		Where("id = ?", callID).
		Updates(map[string]any{
			"state":              model.CallStateAIDelegated,
			"answered_at":        answeredAt,
			"delegation_mode":    model.CallDelegationAIDelegated,
			"delegated_agent_id": agentID,
		}).Error
}

func (s *CallRecordStore) UpdateEnd(ctx context.Context, callID int64, state int16, endReason string, endedAt time.Time, durationSec *int) error {
	updates := map[string]any{
		"state":      state,
		"ended_at":   endedAt,
		"end_reason": endReason,
	}
	if durationSec != nil {
		updates["duration_seconds"] = *durationSec
	}
	return s.db.WithContext(ctx).Model(&model.CallRecord{}).
		Where("id = ?", callID).
		Updates(updates).Error
}

// UpdateRecordingURLs 更新录音 URL（Phase 3: Egress 完成后调用）。
func (s *CallRecordStore) UpdateRecordingURLs(ctx context.Context, callID int64, callerURL, calleeURL, aiURL, mixedURL string) error {
	updates := map[string]any{}
	if callerURL != "" {
		updates["recording_caller_url"] = callerURL
	}
	if calleeURL != "" {
		updates["recording_callee_url"] = calleeURL
	}
	if aiURL != "" {
		updates["recording_ai_url"] = aiURL
	}
	if mixedURL != "" {
		updates["recording_mixed_url"] = mixedURL
	}
	if len(updates) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&model.CallRecord{}).
		Where("id = ?", callID).
		Updates(updates).Error
}

// UpdateSegmentCount 原子递增 segment_count（每写入一条片段消息调用一次）。
func (s *CallRecordStore) UpdateSegmentCount(ctx context.Context, callID int64) error {
	return s.db.WithContext(ctx).Model(&model.CallRecord{}).
		Where("id = ?", callID).
		UpdateColumn("segment_count", gorm.Expr("segment_count + 1")).Error
}

// GetByID retrieves a call record by ID.
func (s *CallRecordStore) GetByID(ctx context.Context, callID int64) (*model.CallRecord, error) {
	var r model.CallRecord
	if err := s.db.WithContext(ctx).Where("id = ?", callID).First(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

// ListInProgress 返回所有处于进行中状态（RINGING / AI_DELEGATED / HUMAN_ACTIVE / ACTIVE）的通话记录。
// 用于 ws 启动时清理孤立通话。
func (s *CallRecordStore) ListInProgress(ctx context.Context) ([]model.CallRecord, error) {
	var records []model.CallRecord
	err := s.db.WithContext(ctx).
		Where("state IN ?", []int16{
			model.CallStateRinging,
			model.CallStateActive,
			model.CallStateAIDelegated,
			model.CallStateHumanActive,
		}).
		Select("id", "caller_id", "callee_id", "delegated_agent_id", "state", "session_id").
		Find(&records).Error
	return records, err
}

// ListActiveAIDelegatedBySession 返回指定会话当前处于 AI_DELEGATED 状态的通话。
// 用于语音回灌两个钩子（接点A 触发守卫 / 接点B 下行注入）的跨节点反查：
// 通话内存态只存在于 call owner 节点，多副本下以 DB 状态为准。
// answeredWithin 限定接听时间窗（兜底通话异常结束未更新状态时的脏记录），调用方做歧义守卫。
func (s *CallRecordStore) ListActiveAIDelegatedBySession(ctx context.Context, sessionID string, answeredWithin time.Duration) ([]model.CallRecord, error) {
	var records []model.CallRecord
	err := s.db.WithContext(ctx).
		Where("session_id = ? AND state = ? AND answered_at > ?",
			sessionID, model.CallStateAIDelegated, time.Now().Add(-answeredWithin)).
		Select("id", "ai_provider", "session_id", "delegated_agent_id", "callee_id").
		Find(&records).Error
	return records, err
}

// ListActiveDelegatedByOwner 返回指定 owner(被叫) 名下当前仍由 AI 代接/已接管的通话。
// 用于 pull_sync 下发"语音中"全量快照,跨节点正确(以 DB 状态为准,不依赖任一节点内存态)。
// 仅取 AI_DELEGATED / HUMAN_ACTIVE: 这两态对应 owner 会话页/会话列表应展示的"语音中"徽标。
func (s *CallRecordStore) ListActiveDelegatedByOwner(ctx context.Context, ownerID int64) ([]model.CallRecord, error) {
	var records []model.CallRecord
	err := s.db.WithContext(ctx).
		Where("callee_id = ? AND state IN ?", ownerID, []int16{
			model.CallStateAIDelegated,
			model.CallStateHumanActive,
		}).
		Select("id", "caller_id", "callee_id", "session_id", "state").
		Find(&records).Error
	return records, err
}
