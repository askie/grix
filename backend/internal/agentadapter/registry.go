package agentadapter

import (
	"fmt"
	"sort"
	"sync"
)

type specificityRank struct {
	boundCount         int
	minVersion         string
	maxVersion         string
	sharedPrefix       int
	precision          int
	requiredCapability int
	optionalCapability int
}

// Registry manages registered adapters and provides lookup by family and metadata.
// It is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	adapters []AgentAdapter
	byFamily map[string][]AgentAdapter // family → adapters sorted by specificity
	byID     map[string]AgentAdapter   // adapter_id → adapter
}

// NewRegistry creates an empty adapter registry.
func NewRegistry() *Registry {
	return &Registry{
		byFamily: make(map[string][]AgentAdapter),
		byID:     make(map[string]AgentAdapter),
	}
}

// Register adds an adapter to the registry. Panics if an adapter with the
// same AdapterID is already registered.
func (r *Registry) Register(a AgentAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := a.AdapterID()
	if _, exists := r.byID[id]; exists {
		panic(fmt.Sprintf("agentadapter: duplicate adapter_id %q", id))
	}

	r.adapters = append(r.adapters, a)
	r.byID[id] = a
	r.byFamily[a.Family()] = append(r.byFamily[a.Family()], a)

	// Sort each family's adapters so the most specific (narrowest version range)
	// come first. This ensures SelectByMeta tries the best match first.
	for family := range r.byFamily {
		sort.SliceStable(r.byFamily[family], func(i, j int) bool {
			return compareSpecificity(r.byFamily[family][i], r.byFamily[family][j]) > 0
		})
	}
}

// LookupByID returns the adapter with the given adapter_id, or nil.
func (r *Registry) LookupByID(adapterID string) AgentAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[adapterID]
}

// LookupByFamily returns all adapters for a given family, sorted by specificity.
func (r *Registry) LookupByFamily(family string) []AgentAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make([]AgentAdapter, len(r.byFamily[family]))
	copy(cp, r.byFamily[family])
	return cp
}

// All returns all registered adapters.
func (r *Registry) All() []AgentAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make([]AgentAdapter, len(r.adapters))
	copy(cp, r.adapters)
	return cp
}

// Families returns all registered family names.
func (r *Registry) Families() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byFamily))
	for f := range r.byFamily {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// compareSpecificity orders adapters from most specific to most generic.
// Narrower version windows and richer declared requirements win.
func compareSpecificity(a, b AgentAdapter) int {
	left := buildSpecificityRank(a)
	right := buildSpecificityRank(b)

	if left.boundCount != right.boundCount {
		return compareInt(left.boundCount, right.boundCount)
	}
	if left.sharedPrefix != right.sharedPrefix {
		return compareInt(left.sharedPrefix, right.sharedPrefix)
	}
	if left.boundCount == 2 {
		if cmp := compareVersions(left.minVersion, right.minVersion); cmp != 0 {
			return cmp
		}
		if cmp := compareVersions(right.maxVersion, left.maxVersion); cmp != 0 {
			return cmp
		}
	}
	if left.boundCount == 1 {
		if left.minVersion != "" || right.minVersion != "" {
			if left.minVersion == "" {
				return -1
			}
			if right.minVersion == "" {
				return 1
			}
			if cmp := compareVersions(left.minVersion, right.minVersion); cmp != 0 {
				return cmp
			}
		}
		if left.maxVersion != "" || right.maxVersion != "" {
			if left.maxVersion == "" {
				return -1
			}
			if right.maxVersion == "" {
				return 1
			}
			if cmp := compareVersions(right.maxVersion, left.maxVersion); cmp != 0 {
				return cmp
			}
		}
	}
	if left.precision != right.precision {
		return compareInt(left.precision, right.precision)
	}
	if left.requiredCapability != right.requiredCapability {
		return compareInt(left.requiredCapability, right.requiredCapability)
	}
	if left.optionalCapability != right.optionalCapability {
		return compareInt(left.optionalCapability, right.optionalCapability)
	}

	switch {
	case a.AdapterID() < b.AdapterID():
		return 1
	case a.AdapterID() > b.AdapterID():
		return -1
	default:
		return 0
	}
}

func buildSpecificityRank(a AgentAdapter) specificityRank {
	meta, ok := a.(AdapterMeta)
	if !ok {
		return specificityRank{}
	}

	rank := specificityRank{
		requiredCapability: len(meta.RequiredCapabilities()),
		optionalCapability: len(meta.OptionalCapabilities()),
	}

	versionRange, err := ParseVersionRange(meta.VersionRange())
	if err != nil {
		return rank
	}
	rank.minVersion = versionRange.Min
	rank.maxVersion = versionRange.Max
	if versionRange.Min != "" {
		rank.boundCount++
	}
	if versionRange.Max != "" {
		rank.boundCount++
	}
	rank.sharedPrefix = sharedVersionPrefix(versionRange.Min, versionRange.Max)
	rank.precision = versionPrecision(versionRange.Min) + versionPrecision(versionRange.Max)
	return rank
}

func compareInt(left, right int) int {
	switch {
	case left > right:
		return 1
	case left < right:
		return -1
	default:
		return 0
	}
}

func sharedVersionPrefix(left, right string) int {
	if left == "" || right == "" {
		return 0
	}
	leftParts := splitVersion(left)
	rightParts := splitVersion(right)
	limit := len(leftParts)
	if len(rightParts) < limit {
		limit = len(rightParts)
	}
	count := 0
	for i := 0; i < limit; i++ {
		if versionPart(leftParts, i) != versionPart(rightParts, i) {
			break
		}
		count++
	}
	return count
}

func versionPrecision(version string) int {
	if version == "" {
		return 0
	}
	count := 0
	for _, part := range splitVersion(version) {
		if part != "" {
			count++
		}
	}
	return count
}
