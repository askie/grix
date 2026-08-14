package model

import (
	"time"

	"gorm.io/datatypes"
)

// 通话状态
const (
	CallStateRinging     int16 = 0
	CallStateActive      int16 = 1 // 真人通话中（Phase 1 兼容，Phase 2 起语义等同 HumanActive）
	CallStateEnded       int16 = 2
	CallStateRejected    int16 = 3
	CallStateMissed      int16 = 4
	CallStateError       int16 = 5
	CallStateAIDelegated int16 = 6 // Phase 2: AI 托管中
	CallStateHumanActive int16 = 7 // Phase 2: 真人接管中（AI 静默旁听）
)

// 通话模式
const (
	CallModeVoice int16 = 1
)

// 委托模式
const (
	CallDelegationHuman       = "human"
	CallDelegationAIDelegated = "ai_delegated"
	CallDelegationMixed       = "mixed"
)

// CallRecord 记录一次通话的完整生命周期
type CallRecord struct {
	ID                 int64          `gorm:"primaryKey" json:"id,string"`
	SessionID          string         `gorm:"size:50;not null;index:idx_call_session,priority:1" json:"session_id"`
	CallerID           int64          `gorm:"not null;index:idx_call_caller,priority:1" json:"caller_id,string"`
	CalleeID           int64          `gorm:"not null;index:idx_call_callee,priority:1" json:"callee_id,string"`
	CallMode           int16          `gorm:"not null;default:1" json:"call_mode"`
	State              int16          `gorm:"not null;default:0" json:"state"`
	DelegationMode     string         `gorm:"not null;default:'human'" json:"delegation_mode"`
	DelegatedAgentID   *int64         `json:"delegated_agent_id,string,omitempty"`
	TextAgentID        *int64         `json:"text_agent_id,string,omitempty"`
	AIProvider         string         `gorm:"column:ai_provider" json:"ai_provider,omitempty"`
	HandoverEvents     datatypes.JSON `gorm:"type:text" json:"handover_events,omitempty"`
	StartedAt          *time.Time     `json:"started_at,omitempty"`
	AnsweredAt         *time.Time     `json:"answered_at,omitempty"`
	EndedAt            *time.Time     `json:"ended_at,omitempty"`
	DurationSeconds    *int           `json:"duration_seconds,omitempty"`
	EndReason          string         `json:"end_reason,omitempty"`
	RecordingCallerURL string         `json:"recording_caller_url,omitempty"`
	RecordingCalleeURL string         `json:"recording_callee_url,omitempty"`
	RecordingAIURL     string         `json:"recording_ai_url,omitempty"`
	RecordingMixedURL  string         `json:"recording_mixed_url,omitempty"`
	TranscriptFullURL  string         `json:"transcript_full_url,omitempty"`
	SegmentCount       int            `gorm:"not null;default:0" json:"segment_count"`
	CreatedAt          time.Time      `gorm:"not null;autoCreateTime" json:"created_at"`
}

type CallHandoverEvent struct {
	Action    string    `json:"action"`
	ActorID   int64     `json:"actor_id,string"`
	CreatedAt time.Time `json:"created_at"`
}

func (CallRecord) TableName() string { return "call_records" }
