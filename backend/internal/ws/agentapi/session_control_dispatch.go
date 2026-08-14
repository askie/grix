package agentapi

const sessionControlUsage = "Usage: /grix open <cwd>\n/grix status\n/grix where\n/grix stop"

func (m *Manager) tryHandleSessionControlCommand(evt DelegateEventPayload) bool {
	return m.handleSessionControlCommand(evt, sessionControlBridgeConfig{
		actionType: "session_control",
		usage:      sessionControlUsage,
		logLabel:   "session",
	})
}
