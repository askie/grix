package agentapi

import (
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const agentAPIProtocolVersion = protocol.AgentAPIProtocolVersion

func resolveRequestedFamily(payload AuthPayload) string {
	if family := model.NormalizeAgentClientType(payload.HostType); family != "" {
		return family
	}
	return model.NormalizeAgentClientType(payload.ClientType)
}

func isHermesFamilyName(value string) bool {
	return model.NormalizeAgentClientType(value) == model.AgentClientTypeHermes
}

func isHermesAuthPayload(payload AuthPayload) bool {
	return isHermesFamilyName(resolveRequestedFamily(payload))
}

func isHermesConn(conn *agentConn) bool {
	if conn == nil {
		return false
	}
	if conn.adapter != nil && isHermesFamilyName(conn.adapter.Family()) {
		return true
	}
	return isHermesFamilyName(conn.clientType)
}

func validateHermesAuthPayload(payload AuthPayload) string {
	if strings.TrimSpace(payload.ProtocolVersion) != agentAPIProtocolVersion {
		return "protocol_version must be aibot-agent-api-v1"
	}
	if payload.ContractVersion != protocol.AgentAPIContractVersion {
		return "contract_version must be 1"
	}
	declaredCapabilities := normalizeDeclaredNames(payload.Capabilities)
	for _, capability := range protocol.HermesRequiredCapabilities() {
		if !hasDeclaredName(declaredCapabilities, capability) {
			return capability + " capability required for hermes"
		}
	}
	return ""
}

func isHermesAllowedClientCommand(cmd string) bool {
	return protocol.HermesAllowsPostAuthClientCommand(cmd)
}

func isHermesAllowedEventResultStatus(status string) bool {
	return protocol.HermesAllowsEventResultStatus(status)
}

func isHermesAllowedLocalActionResultStatus(status string) bool {
	return protocol.HermesAllowsLocalActionResultStatus(status)
}
