package agentadapter

import (
	"fmt"
	"strconv"
	"strings"
)

// VersionRange represents a range of supported host versions.
// Format: ">=minVersion <maxVersion" (e.g. ">=2026.1 <2027.0").
// Single-sided: ">=2026.1" or "<2027.0".
// Empty string matches all versions.
type VersionRange struct {
	Min string // inclusive lower bound, empty = no lower bound
	Max string // exclusive upper bound, empty = no upper bound
}

// ParseVersionRange parses a version range string.
func ParseVersionRange(s string) (VersionRange, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return VersionRange{}, nil
	}

	vr := VersionRange{}
	parts := strings.Fields(s)
	for _, part := range parts {
		if strings.HasPrefix(part, ">=") {
			vr.Min = strings.TrimPrefix(part, ">=")
		} else if strings.HasPrefix(part, ">") {
			vr.Min = strings.TrimPrefix(part, ">")
		} else if strings.HasPrefix(part, "<=") {
			vr.Max = strings.TrimPrefix(part, "<=")
		} else if strings.HasPrefix(part, "<") {
			vr.Max = strings.TrimPrefix(part, "<")
		} else {
			return VersionRange{}, fmt.Errorf("invalid version range segment: %q", part)
		}
	}
	return vr, nil
}

// Contains checks if a version string falls within this range.
// Uses simple dot-separated numeric comparison.
func (vr VersionRange) Contains(version string) bool {
	version = strings.TrimSpace(version)
	if version == "" {
		return true // no version info → assume compatible
	}

	if vr.Min != "" && compareVersions(version, vr.Min) < 0 {
		return false
	}
	if vr.Max != "" && compareVersions(version, vr.Max) >= 0 {
		return false
	}
	return true
}

// String returns the range in canonical format.
func (vr VersionRange) String() string {
	var parts []string
	if vr.Min != "" {
		parts = append(parts, ">="+vr.Min)
	}
	if vr.Max != "" {
		parts = append(parts, "<"+vr.Max)
	}
	return strings.Join(parts, " ")
}

// compareVersions compares two dot-separated version strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// Handles mixed formats like "2026.3.23-1" by splitting on both "." and "-".
func compareVersions(a, b string) int {
	aParts := splitVersion(a)
	bParts := splitVersion(b)
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := 0; i < maxLen; i++ {
		av := versionPart(aParts, i)
		bv := versionPart(bParts, i)
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func splitVersion(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "-", ".")
	return strings.Split(s, ".")
}

func versionPart(parts []string, idx int) int64 {
	if idx >= len(parts) {
		return 0
	}
	// Try numeric parse first
	if v, err := strconv.ParseInt(parts[idx], 10, 64); err == nil {
		return v
	}
	// Fallback: lexicographic comparison mapped to int
	var result int64
	for _, c := range parts[idx] {
		result = result*31 + int64(c)
	}
	return result
}

// CheckCapabilities verifies that the required capabilities are present
// in the client's declared capabilities. Returns missing capabilities.
func CheckCapabilities(required []string, clientCaps []string) []string {
	if len(required) == 0 {
		return nil
	}
	capSet := make(map[string]struct{}, len(clientCaps))
	for _, c := range clientCaps {
		capSet[c] = struct{}{}
	}
	var missing []string
	for _, r := range required {
		if _, ok := capSet[r]; !ok {
			missing = append(missing, r)
		}
	}
	return missing
}
