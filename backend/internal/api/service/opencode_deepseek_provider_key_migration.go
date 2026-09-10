package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

// opencodeDeepseekProviderKeyOldBucket is the shared "unclassified ACP" bucket
// opencode and deepseek both fell into before round6 — see
// ws/handler/agent_session_bind.go normalizeAgentSessionProviderKey.
const opencodeDeepseekProviderKeyOldBucket = "acp"

// opencodeDeepseekProviderKeyTargets mirrors the exact mapping added to
// normalizeAgentSessionProviderKey (ws/handler/agent_session_bind.go) and
// dispatchProviderKey (ws/agentapi/agent_invoke_dispatch_agent.go): opencode's
// connector-side session-history reader is registered under "opencode";
// deepseek's connector adapterType and provider-open report are
// "deepseek-harness" ("deepseek" is a same-reader alias, not the primary key).
// Kept as a separate literal map here rather than imported from ws/handler
// because api/service sits below ws/handler in the import graph (ws/handler
// already imports api/service) — literal duplication across these two
// switches and this migration is the existing pattern for provider_key
// buckets (see also the deveco/omp entries in both switches).
var opencodeDeepseekProviderKeyTargets = map[string]string{
	model.AgentClientTypeOpenCode: "opencode",
	model.AgentClientTypeDeepSeek: "deepseek-harness",
}

// RunOpencodeDeepseekProviderKeyMigration backfills existing opencode/deepseek
// rows from the old "acp" catch-all bucket to their own provider_key buckets
// (see opencodeDeepseekProviderKeyTargets). Before this, sync_history for
// these two client types asked grix-connector for a session-history reader
// under "acp", which isn't registered, so history import failed outright.
//
// Four places store or key on provider_key, and all four need to move
// together or existing bindings/import state become unreachable or get
// re-imported as duplicates:
//  1. agent_session_bindings.provider_key — sync_history dispatch reads
//     provider_key from here and sends it to the connector; a stale "acp"
//     value keeps failing forever even after the code fix ships, since the
//     fix only changes what NEW bindings get.
//  2. agent_session_sync_states.provider_key — part of the
//     (agent_id, session_id, provider_key, binding_id) unique key for import
//     progress. Leaving it stale makes the next LoadState() call miss the
//     existing row and treat an in-progress/completed import as "never
//     imported."
//  3. agent_native_message_imports.provider_key — part of the
//     (agent_id, provider_key, binding_id, native_message_id) unique key used
//     to dedupe imported messages. Leaving it stale means a future resync
//     can't find the old dedupe rows and re-imports every message already
//     imported, duplicating the transcript.
//  4. sessions.direct_key — see SessionCreateForAgentBinding in
//     session_service_create.go: sha256(provider_key + ":" + agent_session_id),
//     hex-truncated to 32 chars, used so re-binding/re-importing the same
//     native session reuses the same aibot session instead of creating a new
//     (empty) one. A provider_key change without recomputing this splits the
//     conversation history into two sessions on the next import.
//
// Scope: only bindings with a non-empty binding_id (i.e. created by
// explicitly importing an existing native session via agent_session_bind with
// agent_session_id set) are touched. A binding with an empty binding_id is a
// live session with no corresponding agent_session_sync_states row (the
// import gate only fires when agent_session_id is set — see
// seedImportIntentIfAbsent), so it isn't affected by the provider_key split,
// and its direct_key suffix bakes in a creation-time nanosecond timestamp
// that can't be reconstructed anyway.
//
// Idempotent: every step only touches rows still on the old bucket ("acp") or
// whose direct_key still matches the old formula's output, so re-running
// after a partial or full success is a no-op.
//
// Rollback: this migration only relabels existing rows — no primary key,
// binding_id, or native_message_id is created, deleted, or renumbered, so a
// rollback is symmetric: swap opencodeDeepseekProviderKeyTargets's values
// back to opencodeDeepseekProviderKeyOldBucket ("acp") and rerun the same
// four steps (direct_key recomputed with the "acp" formula). No backup table
// is needed for that.
func RunOpencodeDeepseekProviderKeyMigration(ctx context.Context) error {
	db := store.DB.WithContext(ctx)
	for clientType, newProviderKey := range opencodeDeepseekProviderKeyTargets {
		if err := migrateOpencodeDeepseekProviderKeyBucket(db, clientType, newProviderKey); err != nil {
			return fmt.Errorf("migrate provider_key bucket for %s: %w", clientType, err)
		}
	}
	return nil
}

func migrateOpencodeDeepseekProviderKeyBucket(db *gorm.DB, clientType, newProviderKey string) error {
	var agentIDs []int64
	if err := db.Model(&model.Agent{}).
		Where("agent_client_type = ?", clientType).
		Pluck("id", &agentIDs).Error; err != nil {
		return err
	}
	if len(agentIDs) == 0 {
		return nil
	}

	// 1) Recompute sessions.direct_key first, while agent_session_bindings
	// still holds the old provider_key — the old formula's output is what we
	// use to confirm we're only touching sessions this migration created,
	// not something that already moved on or was hand-edited.
	var bindings []model.AgentSessionBinding
	if err := db.
		Where("agent_id IN ? AND provider_key = ? AND binding_id <> ''", agentIDs, opencodeDeepseekProviderKeyOldBucket).
		Find(&bindings).Error; err != nil {
		return err
	}
	for _, b := range bindings {
		var sess model.Session
		if err := db.Select("session_id", "owner_id", "direct_key").
			Where("session_id = ?", b.SessionID).First(&sess).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		oldDirectKey := computeAgentSessionDirectKey(sess.OwnerID, b.AgentID, opencodeDeepseekProviderKeyOldBucket, b.BindingID)
		if sess.DirectKey == nil || *sess.DirectKey != oldDirectKey {
			// Not on the old formula's output: already migrated, or changed
			// by something else. Leave it alone rather than guess.
			continue
		}
		newDirectKey := computeAgentSessionDirectKey(sess.OwnerID, b.AgentID, newProviderKey, b.BindingID)
		if err := db.Model(&model.Session{}).Where("session_id = ?", sess.SessionID).
			Update("direct_key", newDirectKey).Error; err != nil {
			return err
		}
	}

	// 2) agent_session_bindings.provider_key
	if err := db.Model(&model.AgentSessionBinding{}).
		Where("agent_id IN ? AND provider_key = ?", agentIDs, opencodeDeepseekProviderKeyOldBucket).
		Update("provider_key", newProviderKey).Error; err != nil {
		return err
	}

	// 3) agent_session_sync_states.provider_key
	if err := db.Model(&model.AgentSessionSyncState{}).
		Where("agent_id IN ? AND provider_key = ?", agentIDs, opencodeDeepseekProviderKeyOldBucket).
		Update("provider_key", newProviderKey).Error; err != nil {
		return err
	}

	// 4) agent_native_message_imports.provider_key
	if err := db.Model(&model.AgentNativeMessageImport{}).
		Where("agent_id IN ? AND provider_key = ?", agentIDs, opencodeDeepseekProviderKeyOldBucket).
		Update("provider_key", newProviderKey).Error; err != nil {
		return err
	}

	return nil
}

// computeAgentSessionDirectKey mirrors SessionCreateForAgentBinding's formula
// in session_service_create.go exactly (same suffix shape, same hex prefix
// length) so migrated rows stay lookup-compatible with future binds.
func computeAgentSessionDirectKey(ownerID, agentID int64, providerKey, agentSessionID string) string {
	suffix := providerKey + ":" + agentSessionID
	sum := sha256.Sum256([]byte(suffix))
	return fmt.Sprintf("agent-session:%d:%d:%s", ownerID, agentID, hex.EncodeToString(sum[:])[:32])
}
