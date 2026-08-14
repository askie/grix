package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/store"
)

const profileTTL = 24 * time.Hour

type SkillEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Trigger     string `json:"trigger,omitempty"`
	Source      string `json:"source,omitempty"`
	Managed     bool   `json:"managed,omitempty"`
	SyncState   string `json:"sync_state,omitempty"`
}

// LibrarySkillEnableScopes 是 connector 对某项技能库技能在两个作用域下当前状态的上报
// （技能库启用，方案 v2）。取值语义由 connector 决定，后端不解释、只透传给前端：
//   - global:  "none" | "link" | "unmanaged" | "conflict" | "broken" | "blocked"
//   - project: 同上 + "unavailable"（当前会话没有可用的项目目录）
type LibrarySkillEnableScopes struct {
	Global  string `json:"global,omitempty"`
	Project string `json:"project,omitempty"`
}

// LibrarySkillEntry 是 connector 上报的技能库中一项技能的完整状态（区别于 SkillEntry：
// SkillEntry 是"当前已生效、可被 agent 调用"的技能清单，LibrarySkillEntry 是"技能库里
// 存在的全部技能 + 在 global/project 两个作用域下的启用状态"，供前端渲染启用/停用 UI）。
type LibrarySkillEntry struct {
	Name         string                   `json:"name"`
	Description  string                   `json:"description,omitempty"`
	Digest       string                   `json:"digest,omitempty"`
	Dir          string                   `json:"dir,omitempty"`
	OwnerID      int64                    `json:"owner_id,string,omitempty"`
	System       bool                     `json:"system,omitempty"`
	EnableScopes LibrarySkillEnableScopes `json:"enable_scopes"`
}

type Profile struct {
	AgentID       int64               `json:"agent_id"`
	OwnerID       int64               `json:"owner_id"`
	AdapterID     string              `json:"adapter_id"`
	ClientType    string              `json:"client_type"`
	Capabilities  []string            `json:"capabilities"`
	LocalActions  []string            `json:"local_actions"`
	Skills        []SkillEntry        `json:"skills"`
	LibrarySkills []LibrarySkillEntry `json:"library_skills"`
	Online        bool                `json:"online"`
	LeaseUntil    int64               `json:"lease_until"`
	UpdatedAt     int64               `json:"updated_at"`
}

type RunState struct {
	HasActiveRun bool   `json:"has_active_run"`
	RunID        string `json:"run_id"`
	State        string `json:"state"`
	CanStop      bool   `json:"can_stop"`
	StreamMsgID  int64  `json:"stream_msg_id"`
	TriggerMsgID int64  `json:"trigger_msg_id"`
	AgentID      int64  `json:"agent_id"`
	UpdatedAt    int64  `json:"updated_at"`
}

func (p Profile) HasLocalAction(action string) bool {
	target := normalizeName(action)
	if target == "" {
		return false
	}
	for _, value := range p.LocalActions {
		if normalizeName(value) == target {
			return true
		}
	}
	return false
}

func Key(agentID int64) string {
	return fmt.Sprintf("im:agent_api:runtime:%d", agentID)
}

// KeyForOwner stores the runtime snapshot reported by one physical
// owner-scoped connector. Shared connectors can have a different project cwd
// and therefore different project skill enablement from the primary one.
func KeyForOwner(agentID, ownerID int64) string {
	return fmt.Sprintf("im:agent_api:runtime:%d:%d", agentID, ownerID)
}

func StoreProfile(ctx context.Context, profile Profile, ttl time.Duration) error {
	return storeProfile(ctx, Key(profile.AgentID), profile, ttl)
}

func StoreProfileForOwner(ctx context.Context, profile Profile, ttl time.Duration) error {
	if profile.OwnerID <= 0 {
		return nil
	}
	return storeProfile(ctx, KeyForOwner(profile.AgentID, profile.OwnerID), profile, ttl)
}

func storeProfile(ctx context.Context, key string, profile Profile, ttl time.Duration) error {
	if store.RDB == nil || profile.AgentID <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ttl <= 0 {
		ttl = profileTTL
	}
	profile.AdapterID = strings.TrimSpace(profile.AdapterID)
	profile.ClientType = normalizeName(profile.ClientType)
	profile.Capabilities = normalizeNames(profile.Capabilities)
	profile.LocalActions = normalizeNames(profile.LocalActions)
	if profile.UpdatedAt <= 0 {
		profile.UpdatedAt = time.Now().UnixMilli()
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	return store.RDB.Set(ctx, key, data, ttl).Err()
}

func LoadProfile(ctx context.Context, agentID int64) (Profile, bool, error) {
	return loadProfile(ctx, Key(agentID), agentID)
}

// LoadProfileForOwner prefers the caller's physical connector snapshot and
// falls back to the primary agent snapshot for legacy/offline connectors.
func LoadProfileForOwner(ctx context.Context, agentID, ownerID int64) (Profile, bool, error) {
	if ownerID > 0 {
		profile, ok, err := loadProfile(ctx, KeyForOwner(agentID, ownerID), agentID)
		if err != nil || ok {
			return profile, ok, err
		}
	}
	return LoadProfile(ctx, agentID)
}

func loadProfile(ctx context.Context, key string, agentID int64) (Profile, bool, error) {
	if store.RDB == nil || agentID <= 0 {
		return Profile{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	data, err := store.RDB.Get(ctx, key).Bytes()
	if err != nil {
		return Profile{}, false, nil
	}
	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return Profile{}, false, err
	}
	profile.ClientType = normalizeName(profile.ClientType)
	profile.Capabilities = normalizeNames(profile.Capabilities)
	profile.LocalActions = normalizeNames(profile.LocalActions)
	return profile, true, nil
}

func DeleteProfile(ctx context.Context, agentID int64) error {
	if store.RDB == nil || agentID <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return store.RDB.Del(ctx, Key(agentID)).Err()
}

func DeleteProfileForOwner(ctx context.Context, agentID, ownerID int64) error {
	if store.RDB == nil || agentID <= 0 || ownerID <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return store.RDB.Del(ctx, KeyForOwner(agentID, ownerID)).Err()
}

func normalizeNames(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeName(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func normalizeName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
