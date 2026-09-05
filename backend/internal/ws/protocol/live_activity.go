package protocol

// CmdLiveActivity 是离线推送任务（im.push.offline.<user_id>）的 cmd，不是 ws 报文
// cmd：实时活动卡片只经 APNs 下发，从不走 WebSocket，所以它不在 ws cmd 稳定性清单里。
const CmdLiveActivity = "live_activity"

// Live activity 事件。对应 ActivityKit 的 aps.event。
const (
	LiveActivityEventStart  = "start"
	LiveActivityEventUpdate = "update"
	LiveActivityEventEnd    = "end"
)

// LiveActivityAttributes 是活动的静态部分（ActivityAttributes），只在 start 下发，
// 之后整张卡的生命周期内不变。
type LiveActivityAttributes struct {
	SessionID string `json:"session_id"`
	AgentID   int64  `json:"agent_id,string"`
	AgentName string `json:"agent_name"`
}

// LiveActivityContentState 是活动的动态部分（ActivityAttributes.ContentState）。
// Phase 取 chat_states 的状态名，外加 stopped（用户主动停止 / 僵死清理）。
type LiveActivityContentState struct {
	Phase       string `json:"phase"`
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	UpdatedAtMs int64  `json:"updated_at_ms"`
}

// LiveActivityAlert 让一次 update 在锁屏上出声/震动。只有转入等待主人的阶段才带。
type LiveActivityAlert struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// LiveActivityPayload 是 pushTask{cmd:"live_activity"} 的 payload。
type LiveActivityPayload struct {
	Event        string                   `json:"event"`
	SessionID    string                   `json:"session_id"`
	Attributes   LiveActivityAttributes   `json:"attributes"`
	ContentState LiveActivityContentState `json:"content_state"`
	Alert        *LiveActivityAlert       `json:"alert,omitempty"`
	// DismissalAtMs 只在 end 事件带：卡片展示终态后自动从锁屏消失的时刻。
	DismissalAtMs int64 `json:"dismissal_at_ms,omitempty"`
}

// LiveActivityPhases 是 ContentState.Phase 的全部取值。iOS 端 GrixRunAttributes
// 的 phase 枚举必须与之保持一致。
const (
	LiveActivityPhaseRunning         = "running"
	LiveActivityPhaseWaitingApproval = "waiting_approval"
	LiveActivityPhaseWaitingQuestion = "waiting_question"
	LiveActivityPhaseCompleted       = "completed"
	LiveActivityPhaseFailed          = "failed"
	LiveActivityPhaseStopped         = "stopped"
)

// IsLiveActivityTerminalPhase 判断阶段是否为终态（卡片该结束）。
func IsLiveActivityTerminalPhase(phase string) bool {
	switch phase {
	case LiveActivityPhaseCompleted, LiveActivityPhaseFailed, LiveActivityPhaseStopped:
		return true
	}
	return false
}
