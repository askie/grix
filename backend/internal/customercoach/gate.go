package customercoach

import (
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/pkg/logger"
)

// FeatureGateKey is the admin feature-gates key for proactive customer coach.
// Status semantics follow the shared gate store:
//   - enabled: all users
//   - whitelist: only FeatureGateUser entries
//   - disabled / missing: nobody (fail-closed)
const FeatureGateKey = "customer_coach"

// IsCoachTriggerAllowed reports whether proactive customer-coach dispatch is
// enabled for this user via the admin feature gate. Fail-closed on errors or
// when the gate has not been created yet.
func IsCoachTriggerAllowed(userID int64) bool {
	if userID <= 0 {
		return false
	}
	features, err := featuregate.GetUserFeatures(userID)
	if err != nil {
		logger.L.Warnf("evaluate customer_coach gate failed user=%d err=%v (fail-closed)", userID, err)
		return false
	}
	for _, feature := range features {
		if feature == FeatureGateKey {
			return true
		}
	}
	return false
}

// Coach onboarding steps in guidance order. The ids are persisted in the
// dispatch gate state, so keep them stable.
const (
	coachStepAgent           = "agent"
	coachStepAgentMessage    = "agent_message"
	coachStepMultiAgentGroup = "multi_agent_group"
	coachStepVoice           = "voice"
)

// missingCoachSteps returns the onboarding steps the user has not completed
// yet, in stable guidance order. An empty result means the basic path is done.
//
// Criteria mirror training "阶段 10：基础路径都完成", with the "main agent"
// requirement relaxed to "has at least one agent": requiring a scope-complete
// main agent almost never holds in practice (even heavy users miss one scope),
// which made the skip gate dead code and pushed the silence decision onto the
// model. Online status is intentionally ignored — it is ephemeral and should
// not re-open coaching after the path is done.
func missingCoachSteps(snapshot Snapshot) []string {
	missing := make([]string, 0, 4)
	if snapshot.Overview.AgentTotal <= 0 {
		missing = append(missing, coachStepAgent)
	}
	if !snapshot.Usage.HasSentAgentMessage {
		missing = append(missing, coachStepAgentMessage)
	}
	if !snapshot.Overview.HasMultiAgentGroup && snapshot.Sessions.MultiAgentGroups <= 0 {
		missing = append(missing, coachStepMultiAgentGroup)
	}
	if !snapshot.Usage.HasVoiceCall {
		missing = append(missing, coachStepVoice)
	}
	return missing
}

// ShouldSkipCoachDispatch reports whether the user's snapshot already shows the
// full basic onboarding path is complete. When true, backend must not dispatch
// a coach snapshot to the support agent (do not rely on the agent staying silent).
func ShouldSkipCoachDispatch(snapshot Snapshot) bool {
	return len(missingCoachSteps(snapshot)) == 0
}
