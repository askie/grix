package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type hermesBaseline struct {
	ProtocolVersion      string                `json:"protocol_version"`
	ContractVersion      int                   `json:"contract_version"`
	PacketFields         []string              `json:"packet_fields"`
	PublicCommands       []hermesPublicCommand `json:"public_commands"`
	RequiredAuthFields   []string              `json:"required_auth_fields"`
	Capabilities         hermesCapabilities    `json:"capabilities"`
	LocalActions         []string              `json:"local_actions"`
	Statuses             hermesStatuses        `json:"statuses"`
	ErrorCodes           []string              `json:"error_codes"`
	ForbiddenFields      []string              `json:"forbidden_public_fields"`
	RecommendedFields    []string              `json:"recommended_public_fields"`
	MinimalPluginSurface []string              `json:"minimal_plugin_surface"`
}

type hermesPublicCommand struct {
	Cmd       string `json:"cmd"`
	Direction string `json:"direction"`
}

type hermesCapabilities struct {
	Required []string `json:"required"`
	Stable   []string `json:"stable"`
}

type hermesStatuses struct {
	EventResult       []string `json:"event_result"`
	EventStopResult   []string `json:"event_stop_result"`
	LocalActionResult []string `json:"local_action_result"`
}

func TestHermesProfileMatchesSnapshot(t *testing.T) {
	baseline := loadHermesBaseline(t)

	if baseline.ProtocolVersion != AgentAPIProtocolVersion {
		t.Fatalf("protocol_version=%q want=%q", baseline.ProtocolVersion, AgentAPIProtocolVersion)
	}
	if baseline.ContractVersion != AgentAPIContractVersion {
		t.Fatalf("contract_version=%d want=%d", baseline.ContractVersion, AgentAPIContractVersion)
	}
	assertStringSliceEqual(t, "packet_fields", hermesPacketFields, baseline.PacketFields)
	assertStringSliceEqual(t, "required_auth_fields", hermesRequiredAuthFields, baseline.RequiredAuthFields)
	assertStringSliceEqual(t, "capabilities.required", hermesRequiredCapabilities, baseline.Capabilities.Required)
	assertStringSliceEqual(t, "capabilities.stable", hermesStableCapabilities, baseline.Capabilities.Stable)
	assertStringSliceEqual(t, "public_client_commands", hermesPublicClientCommands, collectCommands(baseline.PublicCommands, "client_to_server", "bidirectional"))
	assertStringSliceEqual(t, "public_server_commands", hermesPublicServerCommands, collectCommands(baseline.PublicCommands, "server_to_client", "bidirectional"))
	assertStringSliceEqual(t, "local_actions", hermesSupportedLocalActions, baseline.LocalActions)
	assertStringSliceEqual(t, "statuses.event_result", hermesEventResultStatuses, baseline.Statuses.EventResult)
	assertStringSliceEqual(t, "statuses.event_stop_result", hermesEventStopResultStatuses, baseline.Statuses.EventStopResult)
	assertStringSliceEqual(t, "statuses.local_action_result", hermesLocalActionResultStatuses, baseline.Statuses.LocalActionResult)
	assertStringSliceEqual(t, "error_codes", hermesErrorCodes, baseline.ErrorCodes)
	assertStringSliceEqual(t, "forbidden_public_fields", hermesForbiddenPublicFields, baseline.ForbiddenFields)
	assertStringSliceEqual(t, "recommended_public_fields", hermesRecommendedPublicFields, baseline.RecommendedFields)
	assertStringSliceEqual(t, "minimal_plugin_surface", hermesMinimalPluginSurface, baseline.MinimalPluginSurface)
}

func loadHermesBaseline(t *testing.T) hermesBaseline {
	t.Helper()

	path := filepath.Join("testdata", "hermes_aibot_agent_api_v1_baseline.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var baseline hermesBaseline
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return baseline
}

func collectCommands(commands []hermesPublicCommand, directions ...string) []string {
	allowed := make(map[string]struct{}, len(directions))
	for _, direction := range directions {
		allowed[direction] = struct{}{}
	}

	result := make([]string, 0, len(commands))
	for _, command := range commands {
		if _, ok := allowed[command.Direction]; ok {
			result = append(result, command.Cmd)
		}
	}
	return result
}

func assertStringSliceEqual(t *testing.T, label string, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%s len=%d want=%d got=%v want=%v", label, len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s[%d]=%q want=%q got=%v want=%v", label, i, got[i], want[i], got, want)
		}
	}
}
