package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

func TestBuildEggInstallCapabilitiesSkillOnlySupportsProprietaryTypes(t *testing.T) {
	capabilities := buildEggInstallCapabilities(model.EggVersion{
		SkillZipURL: "https://example.com/skill.zip",
	})

	if capabilities.CanCreateAgent {
		t.Fatal("expected CanCreateAgent=false for skill-only egg")
	}

	want := []string{
		model.AgentClientTypeClaude,
		model.AgentClientTypeCodex,
		model.AgentClientTypeGemini,
		model.AgentClientTypeQwen,
		model.AgentClientTypeKimi,
	}
	if len(capabilities.ExistingAgentClientTypes) != len(want) {
		t.Fatalf("ExistingAgentClientTypes=%v want=%v", capabilities.ExistingAgentClientTypes, want)
	}
	for i, w := range want {
		if capabilities.ExistingAgentClientTypes[i] != w {
			t.Fatalf("ExistingAgentClientTypes[%d]=%q want=%q", i, capabilities.ExistingAgentClientTypes[i], w)
		}
	}
}

func TestBuildEggInstallCapabilitiesDualPackageSupportsAllTypes(t *testing.T) {
	capabilities := buildEggInstallCapabilities(model.EggVersion{
		PersonaZipURL: "https://example.com/persona.zip",
		SkillZipURL:   "https://example.com/skill.zip",
	})

	if !capabilities.CanCreateAgent {
		t.Fatal("expected CanCreateAgent=true when persona package exists")
	}

	want := []string{
		model.AgentClientTypeOpenClaw,
		model.AgentClientTypeHermes,
		model.AgentClientTypeClaude,
		model.AgentClientTypeCodex,
		model.AgentClientTypeGemini,
		model.AgentClientTypeQwen,
		model.AgentClientTypeKimi,
	}
	if len(capabilities.ExistingAgentClientTypes) != len(want) {
		t.Fatalf("ExistingAgentClientTypes len=%d want=%d", len(capabilities.ExistingAgentClientTypes), len(want))
	}
	for i, w := range want {
		if capabilities.ExistingAgentClientTypes[i] != w {
			t.Fatalf("ExistingAgentClientTypes[%d]=%q want=%q", i, capabilities.ExistingAgentClientTypes[i], w)
		}
	}
}

func TestBuildEggInstallCapabilitiesPersonaOnlySupportsGenericTypes(t *testing.T) {
	capabilities := buildEggInstallCapabilities(model.EggVersion{
		PersonaZipURL: "https://example.com/persona.zip",
	})

	if !capabilities.CanCreateAgent {
		t.Fatal("expected CanCreateAgent=true when persona package exists")
	}

	want := []string{model.AgentClientTypeOpenClaw, model.AgentClientTypeHermes}
	if len(capabilities.ExistingAgentClientTypes) != len(want) {
		t.Fatalf("ExistingAgentClientTypes=%v want=%v", capabilities.ExistingAgentClientTypes, want)
	}
	for i, w := range want {
		if capabilities.ExistingAgentClientTypes[i] != w {
			t.Fatalf("ExistingAgentClientTypes[%d]=%q want=%q", i, capabilities.ExistingAgentClientTypes[i], w)
		}
	}
}
