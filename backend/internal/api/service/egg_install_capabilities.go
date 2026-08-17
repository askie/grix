package service

import (
	"strings"

	"github.com/askie/grix/backend/internal/model"
)

type eggInstallCapabilities struct {
	CanCreateAgent           bool
	ExistingAgentClientTypes []string
}

func buildEggInstallCapabilities(version model.EggVersion) eggInstallCapabilities {
	hasPersonaPackage := strings.TrimSpace(version.PersonaZipURL) != ""
	hasSkillPackage := strings.TrimSpace(version.SkillZipURL) != ""

	targets := make([]string, 0, 8)
	if hasPersonaPackage {
		targets = append(targets, model.AgentClientTypeOpenClaw, model.AgentClientTypeHermes)
	}
	if hasSkillPackage {
		targets = append(targets, model.AgentClientTypeClaude, model.AgentClientTypeCodex, model.AgentClientTypeGemini, model.AgentClientTypeQwen, model.AgentClientTypeKimi, model.AgentClientTypeDeepSeek)
	}

	return eggInstallCapabilities{
		CanCreateAgent:           hasPersonaPackage,
		ExistingAgentClientTypes: targets,
	}
}

func supportsEggInstallClient(version model.EggVersion, clientType string) bool {
	normalizedClientType := model.NormalizeAgentClientType(clientType)
	if normalizedClientType == "" {
		return false
	}

	capabilities := buildEggInstallCapabilities(version)
	for _, supportedClientType := range capabilities.ExistingAgentClientTypes {
		if model.NormalizeAgentClientType(supportedClientType) == normalizedClientType {
			return true
		}
	}
	return false
}
