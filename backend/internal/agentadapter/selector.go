package agentadapter

import (
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// SelectResult holds the outcome of adapter selection for a connection.
type SelectResult struct {
	Adapter                 AgentAdapter
	AdapterID               string
	Degraded                bool
	DegradePolicy           DegradePolicy
	MissingCapabilities     []string
	UnsupportedCapabilities []string
}

// SelectByMeta selects the best adapter for the given client metadata.
// Selection priority:
//  1. Honor adapter_hint when it points to a registered compatible adapter
//  2. Match by family (client_type / host_type)
//  3. Check version range (if adapter declares AdapterMeta)
//  4. Check required capabilities (if adapter declares AdapterMeta)
//  5. Find the most specific adapter whose Supports() returns true
//  6. Fall back to the family's most generic adapter with degradation
//  7. Return nil if no adapter exists for the family
func SelectByMeta(registry *Registry, meta AgentClientMeta) *SelectResult {
	if hinted := selectByHint(registry, meta); hinted != nil {
		return hinted
	}

	family := resolveFamily(meta)
	adapters := registry.LookupByFamily(family)
	if len(adapters) == 0 {
		logger.L.Warnf("agentadapter: no adapters registered for family=%s client_type=%s", family, meta.ClientType)
		return nil
	}

	// Phase 1: Try each adapter with full version+capability matching.
	var degradedCandidate *SelectResult
	for _, a := range adapters {
		if !a.Supports(meta) {
			continue
		}

		result := checkAdapterCompatibility(a, meta)
		if result == nil {
			continue
		}
		if !result.Degraded {
			return result
		}
		if degradedCandidate == nil {
			degradedCandidate = result
		}
	}

	if degradedCandidate != nil {
		logger.L.Warnf(
			"agentadapter: selecting degraded adapter_id=%s family=%s host_version=%s missing_capabilities=%v",
			degradedCandidate.AdapterID, family, meta.HostVersion, degradedCandidate.MissingCapabilities,
		)
		return degradedCandidate
	}

	// Phase 2: Fall back to the most generic adapter in the family.
	fallback := adapters[len(adapters)-1]
	policy := DegradeToBasic
	if meta, ok := fallback.(AdapterMeta); ok {
		policy = meta.DegradePolicy()
	}

	logger.L.Warnf(
		"agentadapter: no specific adapter matched for family=%s host_version=%s, falling back to adapter_id=%s degrade_policy=%v",
		family, meta.HostVersion, fallback.AdapterID(), policy,
	)
	return &SelectResult{
		Adapter:       fallback,
		AdapterID:     fallback.AdapterID(),
		Degraded:      true,
		DegradePolicy: policy,
	}
}

func selectByHint(registry *Registry, meta AgentClientMeta) *SelectResult {
	if registry == nil {
		return nil
	}
	adapterID := meta.AdapterHint
	if adapterID == "" {
		return nil
	}

	hinted := registry.LookupByID(adapterID)
	if hinted == nil {
		logger.L.Warnf("agentadapter: adapter_hint %q not registered", adapterID)
		return nil
	}

	result := checkAdapterCompatibility(hinted, meta)
	if result == nil {
		logger.L.Warnf(
			"agentadapter: adapter_hint %q incompatible with host_version=%s, falling back to family selection",
			adapterID,
			meta.HostVersion,
		)
		return nil
	}
	if result.Degraded {
		logger.L.Warnf(
			"agentadapter: selecting hinted adapter_id=%s with degraded capabilities=%v",
			result.AdapterID,
			result.MissingCapabilities,
		)
	} else {
		logger.L.Infof("agentadapter: selecting hinted adapter_id=%s", result.AdapterID)
	}
	return result
}

// checkAdapterCompatibility checks version range and capabilities for an adapter.
// Returns nil if the adapter should be skipped (version out of range).
func checkAdapterCompatibility(a AgentAdapter, meta AgentClientMeta) *SelectResult {
	// If adapter doesn't implement AdapterMeta, use basic Supports() result.
	adapterMeta, hasMeta := a.(AdapterMeta)
	if !hasMeta {
		return &SelectResult{
			Adapter:   a,
			AdapterID: a.AdapterID(),
		}
	}

	// Check version range.
	if adapterMeta.VersionRange() != "" && meta.HostVersion != "" {
		vr, err := ParseVersionRange(adapterMeta.VersionRange())
		if err != nil {
			logger.L.Warnf("agentadapter: invalid version range %q for adapter %s: %v", adapterMeta.VersionRange(), a.AdapterID(), err)
		} else if !vr.Contains(meta.HostVersion) {
			logger.L.Infof(
				"agentadapter: adapter %s version range %s excludes host_version=%s",
				a.AdapterID(), adapterMeta.VersionRange(), meta.HostVersion,
			)
			return nil // skip this adapter
		}
	}

	// Check required capabilities.
	missing := CheckCapabilities(adapterMeta.RequiredCapabilities(), meta.Capabilities)
	if len(missing) > 0 {
		logger.L.Infof(
			"agentadapter: adapter %s missing required capabilities: %v",
			a.AdapterID(), missing,
		)
		// Don't skip — degrade instead, since the connection may still work partially.
		return &SelectResult{
			Adapter:             a,
			AdapterID:           a.AdapterID(),
			Degraded:            true,
			DegradePolicy:       adapterMeta.DegradePolicy(),
			MissingCapabilities: missing,
		}
	}

	return &SelectResult{
		Adapter:       a,
		AdapterID:     a.AdapterID(),
		DegradePolicy: adapterMeta.DegradePolicy(),
	}
}

// resolveFamily determines the AI family from client metadata.
// Priority: host_type > client_type.
func resolveFamily(meta AgentClientMeta) string {
	if meta.HostType != "" {
		return meta.HostType
	}
	if meta.ClientType != "" {
		return meta.ClientType
	}
	return ""
}

// FormatAuthAckExt builds the extended auth_ack fields for the contract.
func FormatAuthAckExt(result *SelectResult, meta AgentClientMeta) map[string]interface{} {
	supportedCaps, degradedCaps, unsupportedCaps := classifyAuthAckCapabilities(result, meta)
	if result == nil {
		return map[string]interface{}{
			"contract_version":         meta.ContractVersion,
			"adapter_id":               "",
			"supported_capabilities":   supportedCaps,
			"degraded_capabilities":    degradedCaps,
			"unsupported_capabilities": unsupportedCaps,
		}
	}

	return map[string]interface{}{
		"contract_version":         meta.ContractVersion,
		"adapter_id":               result.AdapterID,
		"supported_capabilities":   supportedCaps,
		"degraded_capabilities":    degradedCaps,
		"unsupported_capabilities": unsupportedCaps,
	}
}

func classifyAuthAckCapabilities(result *SelectResult, meta AgentClientMeta) ([]string, []string, []string) {
	if len(meta.Capabilities) == 0 {
		return []string{}, []string{}, []string{}
	}
	if result == nil || result.Adapter == nil {
		return []string{}, []string{}, append([]string{}, meta.Capabilities...)
	}

	adapterMeta, hasMeta := result.Adapter.(AdapterMeta)
	if !hasMeta {
		return append([]string{}, meta.Capabilities...), []string{}, []string{}
	}

	supportedSet := make(map[string]struct{})
	// Platform-level capability: independent of the selected model adapter.
	supportedSet[protocol.AgentAPISessionSendQuoteCapability] = struct{}{}
	for _, capability := range adapterMeta.RequiredCapabilities() {
		supportedSet[capability] = struct{}{}
	}
	for _, capability := range adapterMeta.OptionalCapabilities() {
		supportedSet[capability] = struct{}{}
	}

	missingSet := make(map[string]struct{}, len(result.MissingCapabilities))
	for _, capability := range result.MissingCapabilities {
		missingSet[capability] = struct{}{}
	}

	supported := make([]string, 0, len(meta.Capabilities))
	degraded := make([]string, 0, len(result.MissingCapabilities))
	unsupported := make([]string, 0, len(meta.Capabilities))
	for _, capability := range meta.Capabilities {
		if _, missing := missingSet[capability]; missing {
			degraded = append(degraded, capability)
			continue
		}
		if _, ok := supportedSet[capability]; ok {
			supported = append(supported, capability)
			continue
		}
		unsupported = append(unsupported, capability)
	}
	for _, capability := range result.MissingCapabilities {
		if _, seen := missingSet[capability]; seen && !containsString(degraded, capability) {
			degraded = append(degraded, capability)
		}
	}
	unsupported = append(unsupported, result.UnsupportedCapabilities...)
	return supported, degraded, unsupported
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
