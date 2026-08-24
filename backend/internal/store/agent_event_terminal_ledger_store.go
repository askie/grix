package store

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AgentTerminalLedgerDisposition int

const (
	AgentTerminalLedgerMissing AgentTerminalLedgerDisposition = iota
	AgentTerminalLedgerPending
	AgentTerminalLedgerCreated
	AgentTerminalLedgerSame
	AgentTerminalLedgerConflict
	AgentTerminalLedgerForeign
)

func sameAgentTerminalVerdict(
	row *model.AgentEventTerminalLedger,
	status string,
	code string,
	msg string,
) bool {
	return row != nil &&
		strings.TrimSpace(row.Status) == strings.TrimSpace(status) &&
		strings.TrimSpace(row.Code) == strings.TrimSpace(code) &&
		strings.TrimSpace(row.Msg) == strings.TrimSpace(msg)
}

func ResolveAgentEventTerminalLedger(
	eventID string,
	ownerID int64,
	agentID int64,
	status string,
	code string,
	msg string,
	terminalCommitToken ...string,
) (AgentTerminalLedgerDisposition, *model.AgentEventTerminalLedger, error) {
	if DB == nil || strings.TrimSpace(eventID) == "" {
		return AgentTerminalLedgerMissing, nil, nil
	}
	var row model.AgentEventTerminalLedger
	err := DB.Where("event_id = ?", strings.TrimSpace(eventID)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AgentTerminalLedgerMissing, nil, nil
	}
	if err != nil {
		return AgentTerminalLedgerMissing, nil, err
	}
	if row.OwnerID != ownerID || row.AgentID != agentID {
		return AgentTerminalLedgerForeign, &row, nil
	}
	incomingToken := ""
	if len(terminalCommitToken) > 0 {
		incomingToken = strings.TrimSpace(terminalCommitToken[0])
	}
	if strings.TrimSpace(row.TerminalCommitToken) != "" &&
		strings.TrimSpace(row.TerminalCommitToken) != incomingToken {
		return AgentTerminalLedgerForeign, &row, nil
	}
	if strings.TrimSpace(row.Status) == "" {
		return AgentTerminalLedgerPending, &row, nil
	}
	if !sameAgentTerminalVerdict(&row, status, code, msg) {
		return AgentTerminalLedgerConflict, &row, nil
	}
	return AgentTerminalLedgerSame, &row, nil
}

func LoadAgentEventTerminalLedger(
	eventID string,
) (*model.AgentEventTerminalLedger, error) {
	if DB == nil || strings.TrimSpace(eventID) == "" {
		return nil, nil
	}
	var row model.AgentEventTerminalLedger
	err := DB.First(&row, "event_id = ?", strings.TrimSpace(eventID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ListPendingRecordOnlyAgentEventDispatches returns durable record-only seeds
// for one connection-scoped session. Callers apply the protocol-specific
// internal-event predicate after decoding the immutable event snapshot.
func ListPendingRecordOnlyAgentEventDispatches(
	sessionID string,
	ownerID int64,
	agentID int64,
) ([]model.AgentEventTerminalLedger, error) {
	if DB == nil || strings.TrimSpace(sessionID) == "" || ownerID <= 0 || agentID <= 0 {
		return nil, nil
	}
	var rows []model.AgentEventTerminalLedger
	err := DB.Where(
		"session_id = ? AND owner_id = ? AND agent_id = ? AND status = '' AND record_only = ?",
		strings.TrimSpace(sessionID), ownerID, agentID, true,
	).Find(&rows).Error
	return rows, err
}

// ListExpiredPendingAgentEventDispatches returns DB seeds old enough to have
// outlived the Redis coordination lease. The caller must still verify that no
// Redis record exists before deleting each row with the generation CAS.
func ListExpiredPendingAgentEventDispatches(
	cutoff time.Time,
	limit int,
) ([]model.AgentEventTerminalLedger, error) {
	if DB == nil || cutoff.IsZero() || limit <= 0 {
		return nil, nil
	}
	var rows []model.AgentEventTerminalLedger
	err := DB.Where("status = '' AND updated_at <= ?", cutoff.UTC()).
		Order("updated_at ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// NextAgentRunGeneration allocates a database-serialized order for one
// owner/session. It is deliberately independent from every backend node's
// application clock.
func NextAgentRunGeneration(sessionID string, ownerID int64) (int64, error) {
	if DB == nil {
		return time.Now().UnixNano(), nil
	}
	scopeKey := fmt.Sprintf("%d:%s", ownerID, strings.TrimSpace(sessionID))
	if ownerID <= 0 || strings.TrimSpace(sessionID) == "" {
		return 0, fmt.Errorf("invalid run generation scope")
	}
	var value int64
	err := DB.Raw(`
		INSERT INTO agent_run_sequences (scope_key, value)
		VALUES (?, 1)
		ON CONFLICT (scope_key) DO UPDATE
		SET value = agent_run_sequences.value + 1
		RETURNING value
	`, scopeKey).Scan(&value).Error
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("invalid allocated run generation")
	}
	return value, nil
}

// SeedAgentEventDispatchLedger persists the complete immutable dispatch
// snapshot before the packet is exposed to the connector. Status remains empty
// until a terminal verdict atomically freezes the row.
func SeedAgentEventDispatchLedger(
	entry model.AgentEventTerminalLedger,
) (AgentTerminalLedgerDisposition, *model.AgentEventTerminalLedger, bool, error) {
	if DB == nil {
		return AgentTerminalLedgerCreated, &entry, true, nil
	}
	entry.EventID = strings.TrimSpace(entry.EventID)
	entry.Status = ""
	entry.Code = ""
	entry.Msg = ""
	entry.EffectsState = model.AgentTerminalEffectsPending
	if entry.EventID == "" || entry.OwnerID <= 0 || entry.AgentID <= 0 ||
		len(entry.DelegateEvent) == 0 || entry.DispatchGeneration <= 0 {
		return AgentTerminalLedgerMissing, nil, false, nil
	}
	now := time.Now().UTC()
	entry.CreatedAt = now
	entry.UpdatedAt = now
	result := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "event_id"}},
		DoNothing: true,
	}).Create(&entry)
	if result.Error != nil {
		return AgentTerminalLedgerMissing, nil, false, result.Error
	}
	var resolved model.AgentEventTerminalLedger
	if err := DB.First(&resolved, "event_id = ?", entry.EventID).Error; err != nil {
		return AgentTerminalLedgerMissing, nil, false, err
	}
	switch {
	case resolved.OwnerID != entry.OwnerID || resolved.AgentID != entry.AgentID:
		return AgentTerminalLedgerForeign, &resolved, false, nil
	case strings.TrimSpace(resolved.TerminalCommitToken) != "" &&
		strings.TrimSpace(resolved.TerminalCommitToken) !=
			strings.TrimSpace(entry.TerminalCommitToken):
		return AgentTerminalLedgerForeign, &resolved, false, nil
	case strings.TrimSpace(resolved.Status) == "":
		if result.RowsAffected == 1 {
			return AgentTerminalLedgerCreated, &resolved, true, nil
		}
		return AgentTerminalLedgerPending, &resolved, false, nil
	default:
		return AgentTerminalLedgerSame, &resolved, false, nil
	}
}

// DeleteAgentEventDispatchSeedIfPending rolls back only the exact seed created
// by a failed initial channel write. A newer generation or any terminal verdict
// wins the CAS and is left untouched.
func DeleteAgentEventDispatchSeedIfPending(
	eventID string,
	ownerID int64,
	agentID int64,
	generation int64,
) (bool, error) {
	if DB == nil {
		return true, nil
	}
	result := DB.Where(
		"event_id = ? AND owner_id = ? AND agent_id = ? AND dispatch_generation = ? AND status = ''",
		strings.TrimSpace(eventID), ownerID, agentID, generation,
	).Delete(&model.AgentEventTerminalLedger{})
	return result.RowsAffected == 1, result.Error
}

// CommitAgentEventTerminalLedger atomically freezes the immutable verdict and
// optional canonical chat-state transition. The event_id primary key also
// makes a verdict from another owner/agent observable as foreign.
func CommitAgentEventTerminalLedger(
	entry model.AgentEventTerminalLedger,
	chatState *model.SessionAgentState,
) (AgentTerminalLedgerDisposition, *model.AgentEventTerminalLedger, bool, error) {
	if DB == nil {
		chatChanged := chatState != nil
		entry.TaskNotificationAllowed = entry.TaskEligible && chatChanged
		return AgentTerminalLedgerCreated, &entry, chatChanged, nil
	}
	entry.EventID = strings.TrimSpace(entry.EventID)
	entry.Status = strings.TrimSpace(entry.Status)
	entry.Code = strings.TrimSpace(entry.Code)
	entry.Msg = strings.TrimSpace(entry.Msg)
	if entry.EventID == "" || entry.OwnerID <= 0 || entry.AgentID <= 0 || entry.Status == "" {
		return AgentTerminalLedgerMissing, nil, false, nil
	}
	if strings.TrimSpace(entry.EffectsState) == "" {
		entry.EffectsState = model.AgentTerminalEffectsPending
	}
	now := time.Now().UTC()
	entry.CreatedAt = now
	entry.UpdatedAt = now

	var (
		disposition AgentTerminalLedgerDisposition
		resolved    *model.AgentEventTerminalLedger
		chatChanged bool
	)
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Legacy/rolling-upgrade events may not have a dispatch seed.
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "event_id"}},
			DoNothing: true,
		}).Create(&entry)
		if result.Error != nil {
			return result.Error
		}
		wonVerdict := result.RowsAffected == 1
		if result.RowsAffected == 0 {
			var existing model.AgentEventTerminalLedger
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("event_id = ?", entry.EventID).
				First(&existing).Error; err != nil {
				return err
			}
			switch {
			case existing.OwnerID != entry.OwnerID || existing.AgentID != entry.AgentID:
				disposition = AgentTerminalLedgerForeign
				resolved = &existing
				return nil
			case strings.TrimSpace(existing.TerminalCommitToken) != "" &&
				strings.TrimSpace(existing.TerminalCommitToken) !=
					strings.TrimSpace(entry.TerminalCommitToken):
				disposition = AgentTerminalLedgerForeign
				resolved = &existing
				return nil
			case strings.TrimSpace(existing.Status) == "" &&
				existing.DispatchGeneration > 0 &&
				entry.DispatchGeneration > 0 &&
				existing.DispatchGeneration != entry.DispatchGeneration:
				disposition = AgentTerminalLedgerConflict
				resolved = &existing
				return nil
			case strings.TrimSpace(existing.Status) == "":
				updates := map[string]any{
					"status":                    entry.Status,
					"code":                      entry.Code,
					"msg":                       entry.Msg,
					"received_at":               entry.ReceivedAt,
					"delegate_event":            entry.DelegateEvent,
					"terminal_at":               now,
					"effects_state":             entry.EffectsState,
					"effects_done_at":           entry.EffectsDoneAt,
					"effects_suppressed":        entry.EffectsSuppressed,
					"redis_committed_at":        entry.RedisCommittedAt,
					"task_eligible":             entry.TaskEligible,
					"task_notification_allowed": false,
					"updated_at":                now,
				}
				transition := tx.Model(&model.AgentEventTerminalLedger{}).
					Where("event_id = ? AND status = ''", entry.EventID).
					Updates(updates)
				if transition.Error != nil {
					return transition.Error
				}
				wonVerdict = transition.RowsAffected == 1
				if !wonVerdict {
					if err := tx.Where("event_id = ?", entry.EventID).First(&existing).Error; err != nil {
						return err
					}
				}
			}
			if !wonVerdict {
				resolved = &existing
				if sameAgentTerminalVerdict(&existing, entry.Status, entry.Code, entry.Msg) {
					disposition = AgentTerminalLedgerSame
				} else {
					disposition = AgentTerminalLedgerConflict
				}
				return nil
			}
		}

		disposition = AgentTerminalLedgerCreated
		if chatState != nil {
			chatState.RunGeneration = entry.DispatchGeneration
			changed, err := settleSessionAgentStateByRunDB(tx, *chatState)
			if err != nil {
				return err
			}
			chatChanged = changed
		}
		entry.TaskNotificationAllowed = entry.TaskEligible && chatChanged
		if err := tx.Model(&model.AgentEventTerminalLedger{}).
			Where("event_id = ?", entry.EventID).
			Updates(map[string]any{
				"task_notification_allowed": entry.TaskNotificationAllowed,
				"terminal_at":               now,
			}).Error; err != nil {
			return err
		}
		for _, effect := range []string{
			model.AgentTerminalEffectGemini,
			model.AgentTerminalEffectDelivery,
			model.AgentTerminalEffectOutput,
			model.AgentTerminalEffectNotification,
			model.AgentTerminalEffectQuestionCard,
		} {
			row := model.AgentEventTerminalEffect{
				EventID: entry.EventID,
				Effect:  effect,
				State:   model.AgentTerminalEffectsPending,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
		if err := tx.First(&resolved, "event_id = ?", entry.EventID).Error; err != nil {
			return err
		}
		return nil
	})
	return disposition, resolved, chatChanged, err
}

type AgentTerminalEffectClaim struct {
	Effect *model.AgentEventTerminalEffect
	Token  string
	Won    bool
	Done   bool
}

func ClaimAgentEventTerminalEffect(
	eventID string,
	effect string,
	lease time.Duration,
) (AgentTerminalEffectClaim, error) {
	var claim AgentTerminalEffectClaim
	if DB == nil {
		claim.Won = true
		claim.Token = uuid.NewString()
		return claim, nil
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	token := uuid.NewString()
	result := DB.Model(&model.AgentEventTerminalEffect{}).
		Where("event_id = ? AND effect = ?", strings.TrimSpace(eventID), strings.TrimSpace(effect)).
		Where(
			"state = ? OR (state = ? AND (claim_until IS NULL OR claim_until <= CURRENT_TIMESTAMP))",
			model.AgentTerminalEffectsPending,
			model.AgentTerminalEffectsClaimed,
		).
		Updates(map[string]any{
			"state":         model.AgentTerminalEffectsClaimed,
			"claim_token":   token,
			"claim_until":   agentTerminalLeaseUntilExpr(DB, lease),
			"attempt_count": gorm.Expr("attempt_count + 1"),
			"last_error":    "",
			"updated_at":    gorm.Expr("CURRENT_TIMESTAMP"),
		})
	if result.Error != nil {
		return claim, result.Error
	}
	var row model.AgentEventTerminalEffect
	if err := DB.Where("event_id = ? AND effect = ?", strings.TrimSpace(eventID), strings.TrimSpace(effect)).
		First(&row).Error; err != nil {
		return claim, err
	}
	claim.Effect = &row
	claim.Token = token
	claim.Won = result.RowsAffected == 1
	claim.Done = row.State == model.AgentTerminalEffectsDone
	return claim, nil
}

func EnsureAgentEventTerminalEffects(eventID string) error {
	if DB == nil {
		return nil
	}
	for _, effect := range []string{
		model.AgentTerminalEffectGemini,
		model.AgentTerminalEffectDelivery,
		model.AgentTerminalEffectOutput,
		model.AgentTerminalEffectNotification,
		model.AgentTerminalEffectQuestionCard,
	} {
		row := model.AgentEventTerminalEffect{
			EventID: strings.TrimSpace(eventID),
			Effect:  effect,
			State:   model.AgentTerminalEffectsPending,
		}
		if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func CompleteAgentEventTerminalEffect(eventID, effect, token string) (bool, error) {
	if DB == nil {
		return true, nil
	}
	result := DB.Model(&model.AgentEventTerminalEffect{}).
		Where(
			"event_id = ? AND effect = ? AND state = ? AND claim_token = ?",
			strings.TrimSpace(eventID),
			strings.TrimSpace(effect),
			model.AgentTerminalEffectsClaimed,
			strings.TrimSpace(token),
		).
		Updates(map[string]any{
			"state":        model.AgentTerminalEffectsDone,
			"claim_token":  "",
			"claim_until":  nil,
			"completed_at": gorm.Expr("CURRENT_TIMESTAMP"),
			"updated_at":   gorm.Expr("CURRENT_TIMESTAMP"),
		})
	return result.RowsAffected == 1, result.Error
}

func FailAgentEventTerminalEffect(eventID, effect, token string, effectErr error) error {
	if DB == nil {
		return nil
	}
	msg := ""
	if effectErr != nil {
		msg = effectErr.Error()
	}
	return DB.Model(&model.AgentEventTerminalEffect{}).
		Where(
			"event_id = ? AND effect = ? AND state = ? AND claim_token = ?",
			strings.TrimSpace(eventID),
			strings.TrimSpace(effect),
			model.AgentTerminalEffectsClaimed,
			strings.TrimSpace(token),
		).
		Updates(map[string]any{
			"state":       model.AgentTerminalEffectsPending,
			"claim_token": "",
			"claim_until": nil,
			"last_error":  msg,
			"updated_at":  gorm.Expr("CURRENT_TIMESTAMP"),
		}).Error
}

func AllAgentEventTerminalEffectsDone(eventID string) (bool, error) {
	if DB == nil {
		return true, nil
	}
	var count int64
	err := DB.Model(&model.AgentEventTerminalEffect{}).
		Where("event_id = ? AND state <> ?", strings.TrimSpace(eventID), model.AgentTerminalEffectsDone).
		Count(&count).Error
	return count == 0, err
}

func FinalizeAgentEventTerminalEffects(eventID string) (bool, error) {
	if DB == nil {
		return true, nil
	}
	done, err := AllAgentEventTerminalEffectsDone(eventID)
	if err != nil || !done {
		return done, err
	}
	result := DB.Model(&model.AgentEventTerminalLedger{}).
		Where("event_id = ? AND effects_state <> ?", strings.TrimSpace(eventID), model.AgentTerminalEffectsDone).
		Updates(map[string]any{
			"effects_state":   model.AgentTerminalEffectsDone,
			"effects_done_at": gorm.Expr("CURRENT_TIMESTAMP"),
			"delegate_event":  datatypes.JSON([]byte("{}")),
			"updated_at":      gorm.Expr("CURRENT_TIMESTAMP"),
		})
	return result.Error == nil, result.Error
}

// ClaimAgentNotificationReceipt is a permanent mark-before-sink fence. The
// dispatcher already ACKs provider failures, so this preserves its existing
// at-most-once channel contract while extending de-duplication beyond NATS.
func ClaimAgentNotificationReceipt(idempotencyKey, channel string) (bool, error) {
	if DB == nil || strings.TrimSpace(idempotencyKey) == "" {
		return true, nil
	}
	row := model.AgentNotificationReceipt{
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
		Channel:        strings.TrimSpace(channel),
		ClaimedAt:      time.Now().UTC(),
	}
	result := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	return result.RowsAffected == 1, result.Error
}

// HasNewerPendingAgentEventDispatch is the cross-node composing fence. Every
// turn kind (task/proxy/call/record-only) is seeded in this table, so unlike
// chat_states it does not lose non-owner turns. A positive generation makes
// the comparison independent of backend clocks.
//
// record_only 镜像行（event_id 带 :mirror 后缀）只是队列镜像台账：只会被
// ACK、永远等不到终态，status 永远停在 ”。若把它们算作"更新的 pending
// dispatch"，任何遗留镜像行都会永久顶住栅栏——composing 终态清理与 stale
// run 清扫都会被卡死。这里只统计真实派发行。
func HasNewerPendingAgentEventDispatch(
	eventID string,
	sessionID string,
	ownerID int64,
	agentID int64,
) (bool, error) {
	if DB == nil || strings.TrimSpace(eventID) == "" ||
		strings.TrimSpace(sessionID) == "" || ownerID <= 0 || agentID <= 0 {
		return false, nil
	}
	var terminal model.AgentEventTerminalLedger
	err := DB.Select("dispatch_generation").
		First(&terminal, "event_id = ?", strings.TrimSpace(eventID)).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	query := DB.Model(&model.AgentEventTerminalLedger{}).
		Where(
			"session_id = ? AND owner_id = ? AND agent_id = ? AND event_id <> ? AND status = '' AND record_only = ?",
			strings.TrimSpace(sessionID),
			ownerID,
			agentID,
			strings.TrimSpace(eventID),
			false,
		)
	if terminal.DispatchGeneration > 0 {
		query = query.Where("dispatch_generation > ?", terminal.DispatchGeneration)
	}
	var count int64
	if err := query.Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func MarkAgentEventTerminalRedisCommitted(
	eventID string,
	ownerID int64,
	agentID int64,
	status string,
	code string,
	msg string,
	terminalCommitToken ...string,
) (bool, error) {
	if DB == nil {
		return true, nil
	}
	result := DB.Model(&model.AgentEventTerminalLedger{}).
		Where(
			"event_id = ? AND owner_id = ? AND agent_id = ? AND status = ? AND code = ? AND msg = ?",
			strings.TrimSpace(eventID),
			ownerID,
			agentID,
			strings.TrimSpace(status),
			strings.TrimSpace(code),
			strings.TrimSpace(msg),
		).
		Where("redis_committed_at IS NULL").
		Updates(map[string]any{
			"redis_committed_at": gorm.Expr("CURRENT_TIMESTAMP"),
			"updated_at":         gorm.Expr("CURRENT_TIMESTAMP"),
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	disposition, row, err := ResolveAgentEventTerminalLedger(
		eventID, ownerID, agentID, status, code, msg, terminalCommitToken...,
	)
	return err == nil && disposition == AgentTerminalLedgerSame &&
		row != nil && row.RedisCommittedAt != nil, err
}

// agentTerminalLeaseUntilExpr deliberately derives claim expiry from the
// database clock. Comparing one node's wall clock with a lease written by
// another node can otherwise let a fast clock steal an active claim.
func agentTerminalLeaseUntilExpr(db *gorm.DB, lease time.Duration) clause.Expr {
	if lease <= 0 {
		lease = 30 * time.Second
	}
	if db != nil && db.Dialector.Name() == "sqlite" {
		seconds := int64(math.Ceil(lease.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		return gorm.Expr(
			"DATETIME(CURRENT_TIMESTAMP, ?)",
			fmt.Sprintf("+%d seconds", seconds),
		)
	}
	return gorm.Expr(
		"CURRENT_TIMESTAMP + (? * INTERVAL '1 millisecond')",
		lease.Milliseconds(),
	)
}
