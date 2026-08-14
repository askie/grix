package featuregate

import (
	"fmt"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"golang.org/x/sync/singleflight"
)

const cacheTTL = time.Minute

// now is injectable for testing.
var now = time.Now

// sfGroup coalesces concurrent loadSnapshot calls to prevent DB stampede.
var sfGroup singleflight.Group

// cache holds the full feature gate data snapshot.
// stale field retains the last good snapshot for stale-while-error fallback.
var cache struct {
	mu        sync.RWMutex
	gates     []gateSnapshot
	stale     []gateSnapshot // last known good snapshot for DB failure fallback
	loaded    bool
	expiresAt time.Time
}

type gateSnapshot struct {
	Key         string
	Status      string
	WhitelistID map[int64]bool
}

// GetUserFeatures returns the list of feature keys visible to the given user.
// Uses a 1-minute cache of the full gate data.
func GetUserFeatures(userID int64) ([]string, error) {
	snapshot, err := loadSnapshot()
	if err != nil {
		return nil, err
	}
	return evaluateFromSnapshot(snapshot, userID), nil
}

// IsPublicFeatureEnabled returns true if the given feature is globally enabled.
// Suitable for pre-login checks (register, OAuth login).
func IsPublicFeatureEnabled(key string) (bool, error) {
	snapshot, err := loadSnapshot()
	if err != nil {
		return false, err
	}
	for _, g := range snapshot {
		if g.Key == key {
			return g.Status == model.FeatureStatusEnabled, nil
		}
	}
	return false, nil
}

// GetPublicFeatures returns the list of feature keys that are globally enabled.
// Used by unauthenticated clients (e.g. login/register pages).
func GetPublicFeatures() ([]string, error) {
	snapshot, err := loadSnapshot()
	if err != nil {
		return nil, err
	}
	var features []string
	for _, g := range snapshot {
		if g.Status == model.FeatureStatusEnabled {
			features = append(features, g.Key)
		}
	}
	return features, nil
}

// GetAllGates returns all feature gates with whitelist counts (admin use).
// Uses a single batch query for whitelist counts instead of N+1.
func GetAllGates() ([]FeatureGateInfo, error) {
	gates, err := ListGates()
	if err != nil {
		return nil, err
	}
	// Batch query: one query for all whitelist counts
	counts, err := CountWhitelistUsersByGate()
	if err != nil {
		return nil, err
	}
	result := make([]FeatureGateInfo, len(gates))
	for i, g := range gates {
		result[i] = FeatureGateInfo{
			Key:                g.Key,
			DisplayName:        g.DisplayName,
			Status:             g.Status,
			WhitelistUserCount: counts[g.Key],
			PublicOnly:         IsPublicOnly(g.Key),
		}
	}
	return result, nil
}

// FeatureGateInfo is the admin-facing gate representation.
type FeatureGateInfo struct {
	Key                string `json:"key"`
	DisplayName        string `json:"display_name"`
	Status             string `json:"status"`
	WhitelistUserCount int    `json:"whitelist_user_count"`
	// PublicOnly indicates that this gate is evaluated before user login.
	// When true, only "enabled"/"disabled" statuses are allowed and whitelist
	// management is not available.
	PublicOnly bool `json:"public_only"`
}

// SaveGate creates or updates a feature gate and invalidates cache.
// If status is empty, only displayName is updated (for existing gates).
func SaveGate(key, displayName, status string) error {
	existing, err := GetGate(key)
	if err != nil {
		// Gate doesn't exist — status is required for creation
		if status == "" {
			return fmt.Errorf("status is required when creating a new gate")
		}
		_, err = CreateGate(key, displayName, status)
	} else {
		// Gate exists — update only provided fields
		if status != "" && status != existing.Status {
			if e := UpdateGateStatus(key, status); e != nil {
				return e
			}
		}
		if displayName != "" && displayName != existing.DisplayName {
			if e := updateGateField(key, "display_name", displayName); e != nil {
				return e
			}
		}
	}
	if err != nil {
		return err
	}
	InvalidateCache()
	return nil
}

// updateGateField is a helper to update a single field on a gate.
func updateGateField(key, field, value string) error {
	return store.DB.Model(&model.FeatureGate{}).Where("key = ?", key).Update(field, value).Error
}

// InvalidateCache clears the feature gate cache.
func InvalidateCache() {
	cache.mu.Lock()
	cache.loaded = false
	cache.gates = nil
	cache.expiresAt = time.Time{}
	cache.mu.Unlock()
}

func loadSnapshot() ([]gateSnapshot, error) {
	// Fast path: read lock
	cache.mu.RLock()
	if cache.loaded && now().Before(cache.expiresAt) {
		snapshot := cache.gates
		cache.mu.RUnlock()
		return snapshot, nil
	}
	cache.mu.RUnlock()

	// Slow path: singleflight ensures only one goroutine loads from DB.
	result, err, _ := sfGroup.Do("loadSnapshot", func() (interface{}, error) {
		// Double-check after winning the singleflight race
		cache.mu.RLock()
		if cache.loaded && now().Before(cache.expiresAt) {
			snapshot := cache.gates
			cache.mu.RUnlock()
			return snapshot, nil
		}
		cache.mu.RUnlock()

		// Load from DB
		gates, err := ListGates()
		if err != nil {
			// Stale-while-error: return last known good snapshot if available
			cache.mu.RLock()
			if cache.stale != nil {
				stale := cache.stale
				cache.mu.RUnlock()
				return stale, nil
			}
			cache.mu.RUnlock()
			return nil, err
		}

		snapshot := make([]gateSnapshot, 0, len(gates))
		for _, g := range gates {
			gs := gateSnapshot{
				Key:         g.Key,
				Status:      g.Status,
				WhitelistID: nil,
			}
			if g.Status == "whitelist" {
				users, dbErr := GetWhitelistUsers(g.Key)
				if dbErr != nil {
					// Stale-while-error for partial failures too
					cache.mu.RLock()
					if cache.stale != nil {
						stale := cache.stale
						cache.mu.RUnlock()
						return stale, nil
					}
					cache.mu.RUnlock()
					return nil, dbErr
				}
				wl := make(map[int64]bool, len(users))
				for _, u := range users {
					wl[u.UserID] = true
				}
				gs.WhitelistID = wl
			}
			snapshot = append(snapshot, gs)
		}

		// Update cache + save stale copy
		cache.mu.Lock()
		cache.gates = snapshot
		cache.stale = snapshot
		cache.expiresAt = now().Add(cacheTTL)
		cache.loaded = true
		cache.mu.Unlock()

		return snapshot, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]gateSnapshot), nil
}

func evaluateFromSnapshot(snapshot []gateSnapshot, userID int64) []string {
	var features []string
	for _, g := range snapshot {
		switch g.Status {
		case "enabled":
			features = append(features, g.Key)
		case "whitelist":
			if g.WhitelistID != nil && g.WhitelistID[userID] {
				features = append(features, g.Key)
			}
		}
	}
	return features
}
