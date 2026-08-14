package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	AgentTerminalEffectsPending = "pending"
	AgentTerminalEffectsClaimed = "claimed"
	AgentTerminalEffectsDone    = "done"
)

const (
	AgentTerminalEffectGemini       = "gemini"
	AgentTerminalEffectDelivery     = "delivery"
	AgentTerminalEffectOutput       = "output"
	AgentTerminalEffectNotification = "notification"
	AgentTerminalEffectQuestionCard = "question_card"
)

// AgentEventTerminalLedger is the long-lived immutable verdict for one
// connector event. Redis keeps the short-lived coordination lease; this row
// survives that TTL so an outbox retry can still be acknowledged or rejected
// deterministically.
type AgentEventTerminalLedger struct {
	EventID      string `gorm:"primaryKey;size:192;index:idx_agent_event_terminal_owner_agent,priority:3" json:"event_id"`
	// TerminalCommitToken is the per-event rolling-upgrade fence. A connector
	// may delete a tokenized terminal outbox item only after the backend echoes
	// this exact value from a fully committed terminal pipeline.
	TerminalCommitToken string `gorm:"size:64;not null;default:''" json:"terminal_commit_token,omitempty"`
	OwnerID      int64  `gorm:"not null;index:idx_agent_event_terminal_owner_agent,priority:1" json:"owner_id,string"`
	AgentID      int64  `gorm:"not null;index:idx_agent_event_terminal_owner_agent,priority:2" json:"agent_id,string"`
	SessionID    string `gorm:"size:64;not null;default:''" json:"session_id"`
	SessionType  int16  `gorm:"not null;default:0" json:"session_type"`
	MirrorMode   string `gorm:"size:32;not null;default:''" json:"mirror_mode,omitempty"`
	RecordOnly   bool   `gorm:"not null;default:false" json:"record_only"`
	SenderID     int64  `gorm:"not null;default:0" json:"sender_id,string"`
	TriggerMsgID int64  `gorm:"not null;default:0" json:"trigger_msg_id,string"`
	// DelegateEvent is the canonical event snapshot used to resume effects
	// after the shorter-lived Redis coordination record expires. Keep the
	// searchable ownership columns above as immutable redundant indexes.
	DelegateEvent datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"delegate_event"`

	Status string `gorm:"size:32;not null" json:"status"`
	Code   string `gorm:"type:text;not null;default:''" json:"code"`
	Msg    string `gorm:"type:text;not null;default:''" json:"msg"`

	StartedAt  *time.Time `json:"started_at,omitempty"`
	ReceivedAt int64      `gorm:"not null;default:0" json:"received_at,omitempty"`
	CallTurn   bool       `gorm:"not null;default:false" json:"call_turn"`
	// DispatchGeneration is allocated by the database for one owner/session.
	// It is the ordering fence for asynchronously persisted running/terminal
	// state; application wall clocks are deliberately not used for ordering.
	DispatchGeneration int64      `gorm:"not null;default:0;index" json:"dispatch_generation"`
	TerminalAt         *time.Time `json:"terminal_at,omitempty"`

	EffectsState            string     `gorm:"size:16;not null;default:'pending'" json:"effects_state"`
	EffectsDoneAt           *time.Time `json:"effects_done_at,omitempty"`
	RedisCommittedAt        *time.Time `json:"redis_committed_at,omitempty"`
	TaskEligible            bool       `gorm:"not null;default:false" json:"task_eligible"`
	TaskNotificationAllowed bool       `gorm:"not null;default:false" json:"task_notification_allowed"`
	EffectsSuppressed       bool       `gorm:"not null;default:false" json:"effects_suppressed"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (AgentEventTerminalLedger) TableName() string {
	return "agent_event_terminal_ledgers"
}

// AgentEventTerminalEffect is the durable per-sink outbox for a terminal
// verdict. Workers may lose/reclaim a lease, but each sink is independently
// fenced and must use the immutable ledger payload as its idempotency key.
type AgentEventTerminalEffect struct {
	EventID      string     `gorm:"primaryKey;size:192" json:"event_id"`
	Effect       string     `gorm:"primaryKey;size:32" json:"effect"`
	State        string     `gorm:"size:16;not null;default:'pending';index" json:"state"`
	ClaimToken   string     `gorm:"size:64;not null;default:''" json:"claim_token,omitempty"`
	ClaimUntil   *time.Time `json:"claim_until,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	AttemptCount int        `gorm:"not null;default:0" json:"attempt_count"`
	LastError    string     `gorm:"type:text;not null;default:''" json:"last_error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (AgentEventTerminalEffect) TableName() string {
	return "agent_event_terminal_effects"
}

// AgentNotificationReceipt permanently de-duplicates a notification at the
// final dispatcher channel. JetStream's duplicate window is only transport
// protection and is not the correctness boundary.
type AgentNotificationReceipt struct {
	IdempotencyKey string    `gorm:"primaryKey;size:256" json:"idempotency_key"`
	Channel        string    `gorm:"primaryKey;size:32" json:"channel"`
	ClaimedAt      time.Time `gorm:"not null" json:"claimed_at"`
}

func (AgentNotificationReceipt) TableName() string {
	return "agent_notification_receipts"
}

// AgentRunSequence provides a database-serialized generation per
// owner/session. The row is updated with RETURNING so every backend node sees
// one monotonic order independent of its local clock.
type AgentRunSequence struct {
	ScopeKey string `gorm:"primaryKey;size:160" json:"scope_key"`
	Value    int64  `gorm:"not null;default:0" json:"value"`
}

func (AgentRunSequence) TableName() string {
	return "agent_run_sequences"
}
