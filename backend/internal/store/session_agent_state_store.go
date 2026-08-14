package store

import (
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// resolveSessionTitleForOwner returns the best available display title for a
// session from the owner's perspective: custom_title > group_name (groups) > "".
// Used to denormalise the title into session_agent_states at run-start time.
func resolveSessionTitleForOwner(sessionID string, ownerID int64) string {
	if DB == nil {
		return ""
	}
	type row struct {
		CustomTitle string
		GroupName   string
		SessionType int16
	}
	var r row
	err := DB.Raw(`
		SELECT COALESCE(sm.custom_title, '') AS custom_title,
		       COALESCE(s.group_name, '')    AS group_name,
		       s.session_type
		FROM sessions s
		LEFT JOIN session_members sm
		       ON sm.session_id = s.session_id
		      AND sm.member_id  = ?
		      AND sm.member_type = 1
		WHERE s.session_id = ? AND s.is_deleted = false
		LIMIT 1`, ownerID, sessionID).Scan(&r).Error
	if err != nil {
		return ""
	}
	if t := strings.TrimSpace(r.CustomTitle); t != "" {
		return t
	}
	if r.SessionType == 2 {
		return strings.TrimSpace(r.GroupName)
	}
	return ""
}

// UpsertSessionAgentStateRunning marks a session as running for a freshly
// registered run. It clears any terminal residue (completed_at / stop_reason)
// from a previous turn so that a long-running new turn never carries a stale
// completion timestamp, and the single state value is always self-consistent.
func UpsertSessionAgentStateRunning(sessionID string, ownerID, agentID int64, runID string, startedAt time.Time) {
	UpsertSessionAgentStateRunningWithGeneration(
		sessionID, ownerID, agentID, runID, startedAt, 0,
	)
}

func UpsertSessionAgentStateRunningWithGeneration(
	sessionID string,
	ownerID, agentID int64,
	runID string,
	startedAt time.Time,
	runGeneration int64,
) {
	if DB == nil {
		return
	}
	startedAt = startedAt.UTC()
	title := resolveSessionTitleForOwner(sessionID, ownerID)
	now := time.Now().UTC()
	s := model.SessionAgentState{
		SessionID:     sessionID,
		OwnerID:       ownerID,
		AgentID:       agentID,
		State:         model.SessionAgentStateRunning,
		TaskTitle:     title,
		LastRunID:     runID,
		RunGeneration: runGeneration,
		StartedAt:     &startedAt,
		UpdatedAt:     now,
	}
	result := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "session_id"}, {Name: "owner_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"agent_id":       agentID,
			"state":          model.SessionAgentStateRunning,
			"task_title":     title,
			"last_run_id":    runID,
			"run_generation": runGeneration,
			"started_at":     startedAt,
			"completed_at":   nil,
			"stop_reason":    "",
			"updated_at":     now,
		}),
		// Running writes are launched in the background. A terminal result or
		// a newer run can therefore reach the database first; an older/same-run
		// delayed write must never regress either state back to running.
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{
				SQL: `(chat_states.last_run_id = excluded.last_run_id
					AND chat_states.state = ?)
					OR (chat_states.last_run_id <> excluded.last_run_id
						AND ((excluded.run_generation > 0
								AND excluded.run_generation > chat_states.run_generation)
							OR (excluded.run_generation = 0
								AND chat_states.run_generation = 0
								AND excluded.started_at >
							COALESCE(
								chat_states.started_at,
								chat_states.updated_at,
								'1970-01-01'
							))))`,
				Vars: []any{model.SessionAgentStateRunning},
			},
		}},
	}).Create(&s)
	if result.Error != nil {
		logger.L.Warnf("upsert session_agent_state running session=%s owner=%d err=%v",
			sessionID, ownerID, result.Error)
	}
}

// UpdateSessionAgentStateTitleBySession updates task_title for all rows of a
// session. Called when a session is renamed so the cached title stays current.
func UpdateSessionAgentStateTitleBySession(sessionID, title string) {
	if DB == nil {
		return
	}
	if err := DB.Model(&model.SessionAgentState{}).
		Where("session_id = ?", sessionID).
		Update("task_title", strings.TrimSpace(title)).Error; err != nil {
		logger.L.Warnf("update session_agent_state task_title session=%s err=%v", sessionID, err)
	}
}

// UpsertSessionAgentStateTerminal writes the single mutually-exclusive state
// when a run reaches a terminal value (completed / failed / idle). The terminal
// state owns the final verdict and overwrites all relevant fields.
func UpsertSessionAgentStateTerminal(s model.SessionAgentState) {
	if DB == nil {
		return
	}
	s.UpdatedAt = time.Now().UTC()
	result := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "session_id"}, {Name: "owner_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"agent_id":     s.AgentID,
			"state":        s.State,
			"last_run_id":  s.LastRunID,
			"stop_reason":  s.StopReason,
			"completed_at": s.CompletedAt,
			"updated_at":   s.UpdatedAt,
		}),
	}).Create(&s)
	if result.Error != nil {
		logger.L.Warnf("upsert session_agent_state terminal session=%s owner=%d state=%s err=%v",
			s.SessionID, s.OwnerID, s.State, result.Error)
	}
}

// FindNonTerminalSessionAgentStateByRun resolves compact run ownership after
// Redis/in-memory tracking has expired. last_run_id is checked together with
// owner and agent so an old or foreign terminal packet cannot target a newer
// session run.
func FindNonTerminalSessionAgentStateByRun(
	runID string,
	ownerID int64,
	agentID int64,
) (*model.SessionAgentState, error) {
	if DB == nil || strings.TrimSpace(runID) == "" || ownerID <= 0 || agentID <= 0 {
		return nil, nil
	}
	var state model.SessionAgentState
	result := DB.
		Where("last_run_id = ? AND owner_id = ? AND agent_id = ?", strings.TrimSpace(runID), ownerID, agentID).
		Where("state IN ?", nonTerminalSessionAgentStates()).
		First(&state)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, result.Error
	}
	return &state, nil
}

// SettleSessionAgentStateByRun applies a terminal state only while runID is
// still the current non-terminal run. It is the DB-side idempotency fence for
// delayed/duplicated connector results and never overwrites a newer run.
func SettleSessionAgentStateByRun(s model.SessionAgentState) (bool, error) {
	if DB == nil {
		return true, nil
	}
	return settleSessionAgentStateByRunDB(DB, s)
}

func settleSessionAgentStateByRunDB(db *gorm.DB, s model.SessionAgentState) (bool, error) {
	if db == nil {
		return false, errors.New("db is nil")
	}
	if strings.TrimSpace(s.SessionID) == "" || s.OwnerID <= 0 ||
		s.AgentID <= 0 || strings.TrimSpace(s.LastRunID) == "" {
		return false, nil
	}
	now := time.Now().UTC()
	s.LastRunID = strings.TrimSpace(s.LastRunID)
	// The stop_reason column is VARCHAR(255); bound by runes so an oversized
	// connector error text can never abort the settlement transaction (pg 22001).
	s.StopReason = textutil.TruncateRunes(strings.TrimSpace(s.StopReason), model.StopReasonMaxRunes)
	s.UpdatedAt = now
	// Insert handles the narrow race where the terminal packet reaches this
	// synchronous fence before registerActiveRun's background running write.
	// The conflict predicate makes an existing newer/terminal row immutable.
	result := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "session_id"}, {Name: "owner_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"state":          s.State,
			"run_generation": s.RunGeneration,
			"stop_reason":    s.StopReason,
			"completed_at":   s.CompletedAt,
			"updated_at":     now,
		}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{
				SQL: `chat_states.agent_id = excluded.agent_id
					AND chat_states.last_run_id = excluded.last_run_id
					AND (excluded.run_generation = 0
						OR chat_states.run_generation = excluded.run_generation)
					AND chat_states.state IN ?`,
				Vars: []any{nonTerminalSessionAgentStates()},
			},
		}},
	}).Create(&s)
	return result.RowsAffected == 1, result.Error
}

func nonTerminalSessionAgentStates() []string {
	return []string{
		model.SessionAgentStateRunning,
		model.SessionAgentStateWaitingApproval,
		model.SessionAgentStateWaitingQuestion,
	}
}

// SetSessionAgentStateWaiting transitions an active session into a waiting
// phase (waiting_approval / waiting_question) — the run is alive but blocked on
// the owner. It updates an existing active row only: a waiting phase presupposes
// a running row created by an owner-eligible run, so proxy/visitor runs (which
// have no row) are a no-op. It never resurrects a terminal row.
func SetSessionAgentStateWaiting(sessionID string, ownerID int64, state string) {
	if DB == nil {
		return
	}
	state = strings.TrimSpace(state)
	if state != model.SessionAgentStateWaitingApproval && state != model.SessionAgentStateWaitingQuestion {
		return
	}
	now := time.Now().UTC()
	result := DB.Model(&model.SessionAgentState{}).
		Where("session_id = ? AND owner_id = ?", strings.TrimSpace(sessionID), ownerID).
		Where("state IN ?", []string{
			model.SessionAgentStateRunning,
			model.SessionAgentStateWaitingApproval,
			model.SessionAgentStateWaitingQuestion,
		}).
		Updates(map[string]any{
			"state":      state,
			"updated_at": now,
		})
	if result.Error != nil {
		logger.L.Warnf("set session_agent_state waiting session=%s owner=%d state=%s err=%v",
			sessionID, ownerID, state, result.Error)
	}
}

// ListSessionAgentStatesByOwner returns paginated session states for the given
// owner. sessionID narrows to a single session when non-empty; empty returns
// all sessions. state filters to a specific state when non-empty.
// page and pageSize are 1-based; pageSize is clamped to [1, 100].
// Returns the rows, total count, and any error.
func ListSessionAgentStatesByOwner(ownerID int64, sessionID, state string, page, pageSize int) ([]model.SessionAgentState, int64, error) {
	if DB == nil {
		return nil, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	} else if pageSize > 100 {
		pageSize = 100
	}
	q := DB.Model(&model.SessionAgentState{}).Where("owner_id = ?", ownerID)
	if sid := strings.TrimSpace(sessionID); sid != "" {
		q = q.Where("session_id = ?", sid)
	}
	if s := strings.TrimSpace(state); s != "" {
		q = q.Where("state = ?", s)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.SessionAgentState
	offset := (page - 1) * pageSize
	if err := q.Order("updated_at DESC").Limit(pageSize).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ManualUpdateChatState overwrites the state (and optionally stop_reason) of
// an existing chat_states row. Only updates rows that already exist for the
// given (session_id, owner_id) pair — never inserts. Returns false when no
// matching row is found.
func ManualUpdateChatState(sessionID string, ownerID int64, state, reason string) (bool, error) {
	if DB == nil {
		return false, nil
	}
	now := time.Now().UTC()
	fields := map[string]any{
		"state":       state,
		"stop_reason": strings.TrimSpace(reason),
		"updated_at":  now,
	}
	result := DB.Model(&model.SessionAgentState{}).
		Where("session_id = ? AND owner_id = ?", strings.TrimSpace(sessionID), ownerID).
		Updates(fields)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ListStaleRunningSessionAgentStates scans chat_states for zombie candidates:
// rows still in state=running whose updated_at is older than cutoff. A healthy
// run refreshes the row at registration and settles it at terminal time, so a
// running row untouched past the sweeper's conservative threshold means the
// terminal result was most likely lost (connector restart/crash between task
// completion and result reporting). Read-only: whether to settle is the
// caller's decision, guarded by SettleStaleRunningSessionAgentState.
func ListStaleRunningSessionAgentStates(cutoff time.Time, limit int) ([]model.SessionAgentState, error) {
	if DB == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	var rows []model.SessionAgentState
	if err := DB.
		Where("state = ?", model.SessionAgentStateRunning).
		Where("updated_at < ?", cutoff.UTC()).
		Order("updated_at ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// SettleStaleRunningSessionAgentState settles a chat_states row that the stale
// running sweeper has judged to be a zombie (terminal result never arrived).
// The guard is identical to SettleSessionAgentStateByRun: the update only
// lands while the row still carries the scanned last_run_id / run_generation /
// agent_id and is still non-terminal, so a row settled normally in the
// meantime — or taken over by a newer run — is never overwritten.
func SettleStaleRunningSessionAgentState(row model.SessionAgentState, stopReason string) (bool, error) {
	if DB == nil {
		return false, nil
	}
	completedAt := time.Now().UTC()
	return SettleSessionAgentStateByRun(model.SessionAgentState{
		SessionID:     row.SessionID,
		OwnerID:       row.OwnerID,
		AgentID:       row.AgentID,
		State:         model.SessionAgentStateIdle,
		LastRunID:     row.LastRunID,
		RunGeneration: row.RunGeneration,
		StopReason:    stopReason,
		StartedAt:     row.StartedAt,
		CompletedAt:   &completedAt,
	})
}

// ListSessionAgentStatesByState returns all rows currently in any of the given
// states. Used by the startup self-heal scan to find non-terminal rows whose
// run may have leaked (e.g. ws restart lost the in-memory run).
func ListSessionAgentStatesByState(states ...string) ([]model.SessionAgentState, error) {
	if DB == nil || len(states) == 0 {
		return nil, nil
	}
	var rows []model.SessionAgentState
	if err := DB.Where("state IN ?", states).Order("updated_at DESC").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
