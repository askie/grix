package agentapi

import (
	"fmt"
	"strings"
)

// ensureAgentScope returns non-zero code/msg when scope is missing (and may notify owner).
func ensureAgentScope(agentID, ownerID int64, scope string) (int, string) {
	if err := checkAgentScope(agentID, scope); err == nil {
		return 0, ""
	}
	return agentScopeMissing(agentID, ownerID, scope)
}

// agentScopeMissing returns (4003, message) when the agent lacks scope.
func agentScopeMissing(agentID, ownerID int64, scope string) (int, string) {
	base := fmt.Sprintf("agent %d lacks scope %s", agentID, scope)
	if m := GetGlobal(); m != nil {
		if msg := m.maybeNotifyScopeApproval(agentID, ownerID, scope); msg != "" {
			return 4003, msg
		}
	}
	return 4003, base
}

// agentScopeMissingMessage is used where only an error string is needed (MCP path).
func agentScopeMissingMessage(agentID, ownerID int64, scope string) string {
	_, msg := agentScopeMissing(agentID, ownerID, scope)
	return msg
}

func lacksAgentScope(agentID int64, scope string) bool {
	return checkAgentScope(agentID, strings.TrimSpace(scope)) != nil
}
