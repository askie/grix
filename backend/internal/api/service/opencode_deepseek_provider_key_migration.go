package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

// opencodeDeepseekProviderKeyOldBucket is the shared "unclassified ACP"
// bucket opencode, deepseek, and (briefly, before round5) deveco all fell
// into — see ws/handler/agent_session_bind.go normalizeAgentSessionProviderKey.
const opencodeDeepseekProviderKeyOldBucket = "acp"

// providerKeyMigrationTarget names one client_type -> provider_key bucket
// move this migration performs.
type providerKeyMigrationTarget struct {
	clientType     string
	newProviderKey string
}

// opencodeDeepseekProviderKeyTargets mirrors the exact mapping added to
// normalizeAgentSessionProviderKey (ws/handler/agent_session_bind.go) and
// dispatchProviderKey (ws/agentapi/agent_invoke_dispatch_agent.go):
//   - opencode's connector-side session-history reader is registered under
//     "opencode".
//   - deepseek's connector adapterType and provider-open report are
//     "deepseek-harness" ("deepseek" is a same-reader alias, not the primary
//     key).
//   - deveco's backend bucket ("deveco") already shipped in round5, but the
//     connector's providerKeyForAdapter() had no case for the shared
//     "opencode" adapterType until round6, so its session_bind "active"
//     write kept getting overwritten back to "acp" (see
//     ws/handler/agent_session_bind.go's
//     `providerKey = firstTrimmed(bindResp.ProviderKey, providerKey)` — the
//     connector's reported value wins over the backend's own computation).
//     Any deveco binding created in that window is a three-way mismatch:
//     agent_session_bindings.provider_key="acp" (wrong, from the connector
//     override) while agent_session_sync_states.provider_key="deveco" and
//     sessions.direct_key are already correct (both are set from the
//     backend's own value before the connector's override ever happens).
//     Listing deveco here is safe regardless of whether that mismatch ever
//     actually occurred: step 1's old-formula check only touches a session
//     whose direct_key is still hashed with the OLD bucket, so a deveco row
//     that was never wrong is left alone — only its binding and
//     native-message-import rows get moved.
//
// Order is fixed (a slice, not a map) so migration runs and logs are
// reproducible instead of iterating in Go's randomized map order.
var opencodeDeepseekProviderKeyTargets = []providerKeyMigrationTarget{
	{model.AgentClientTypeOpenCode, "opencode"},
	{model.AgentClientTypeDeepSeek, "deepseek-harness"},
	{model.AgentClientTypeDeveco, "deveco"},
}

// RunOpencodeDeepseekProviderKeyMigration backfills existing opencode/
// deepseek/deveco rows from the old "acp" catch-all bucket to their own
// provider_key buckets (see opencodeDeepseekProviderKeyTargets). Before this,
// sync_history for opencode/deepseek asked grix-connector for a
// session-history reader under "acp", which isn't registered, so history
// import failed outright; deveco had the narrower connector-override
// mismatch described above.
//
// *** Deployment order matters. *** Run this only after BOTH the backend
// code fix (ws/handler/agent_session_bind.go, normalizeAgentSessionProviderKey
// / ws/agentapi/agent_invoke_dispatch_agent.go, dispatchProviderKey) and the
// grix-connector fix (session-identity.ts providerKeyForAdapter) are live —
// connector first or simultaneously with the backend, never the backend
// alone first. Two broken orders to avoid:
//   - Running this migration before the code fix ships: any bind/dispatch
//     that happens between the migration and the code fix recomputes
//     provider_key with the OLD (unfixed) logic, writing "acp" — including
//     into a fresh direct_key — which no longer matches what the migration
//     already set, so the very next explicit re-import of that session
//     creates a duplicate instead of reusing it.
//   - Backend fixed but the user's grix-connector build predates the
//     providerKeyForAdapter fix: the connector keeps reporting "acp" for
//     opencode/deveco sessions in its open ack, and
//     ws/handler/agent_session_bind.go's
//     `firstTrimmed(bindResp.ProviderKey, providerKey)` still prefers that
//     reported value over the backend's own corrected computation — so
//     shipping only the backend has ZERO end-to-end effect for that user
//     until their connector also upgrades. deepseek is unaffected by this
//     specific ordering hazard (its providerKeyForAdapter case already
//     reported "deepseek-harness" before round6), but its
//     sync_states/native-message-imports rows still need the backend fix
//     plus this migration to move off the old bucket.
//
// Four places store or key on provider_key, and all four need to move
// together or existing bindings/import state become unreachable or get
// re-imported as duplicates:
//  1. agent_session_bindings.provider_key — sync_history dispatch reads
//     provider_key from here and sends it to the connector; a stale value
//     keeps failing forever even after the code fix ships, since the fix
//     only changes what NEW bindings get.
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
// Scope: steps 2-4 move every row for the target client type's agents that
// is still on the old bucket — no binding_id filter. A live binding with an
// empty binding_id simply has no agent_session_sync_states or
// agent_native_message_imports rows to begin with (the import gate only
// creates those when agent_session_id is set — see seedImportIntentIfAbsent
// in ws/handler/agent_session_bind.go), so steps 3-4 are naturally no-ops for
// it; step 2 has no reason to exclude it either. Step 1 (direct_key) is the
// one step that DOES require a non-empty binding_id: a live binding's direct_key
// suffix bakes in a creation-time nanosecond timestamp that can't be
// reconstructed, so there is nothing to recompute for it regardless.
//
// Idempotent: every step only touches rows still on the old bucket, or (for
// direct_key specifically) whose current value still matches the old
// formula's output, so re-running after a partial or full success is a
// no-op. Each client type's four steps run inside one transaction, so a
// failure partway through (e.g. step 2 succeeds, step 4 errors) rolls back
// that client type's work entirely instead of leaving bindings on the new
// bucket while the dedupe ledger is still on the old one.
//
// Rollback: this migration only relabels existing rows — no primary key,
// binding_id, or native_message_id is created, deleted, or renumbered, so a
// rollback is symmetric: swap opencodeDeepseekProviderKeyTargets's values
// back to opencodeDeepseekProviderKeyOldBucket ("acp") and rerun the same
// four steps (direct_key recomputed with the "acp" formula). No backup table
// is needed for that.
//
// Operational note: this function is intentionally NOT part of
// cmd/migrate/main.go's unconditional startup sequence, so it never runs on
// a routine deploy. It runs only when explicitly opted into via the
// -backfill-provider-keys flag (see cmd/migrate/main.go), which must be
// passed once — after confirming the deployment-order requirement above is
// satisfied — as e.g.
// `go run ./cmd/migrate -backfill-provider-keys config.yaml`.
func RunOpencodeDeepseekProviderKeyMigration(ctx context.Context) error {
	db := store.DB.WithContext(ctx)
	for _, target := range opencodeDeepseekProviderKeyTargets {
		if err := migrateOpencodeDeepseekProviderKeyBucket(db, target.clientType, target.newProviderKey); err != nil {
			return fmt.Errorf("migrate provider_key bucket for %s: %w", target.clientType, err)
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

	return db.Transaction(func(tx *gorm.DB) error {
		// 1) Recompute sessions.direct_key. Bindings are selected from BOTH
		// buckets: a binding can already show the new provider_key (the
		// connector, once fixed, can report it in its open ack before the
		// backend or this migration runs — see the deployment-order note
		// above) while direct_key is still hashed with the old formula. The
		// old-formula comparison below is what actually gates which sessions
		// get touched, independent of the binding's current provider_key.
		var bindings []model.AgentSessionBinding
		if err := tx.
			Where("agent_id IN ? AND provider_key IN ? AND binding_id <> ''",
				agentIDs, []string{opencodeDeepseekProviderKeyOldBucket, newProviderKey}).
			Find(&bindings).Error; err != nil {
			return err
		}
		directKeysUpdated := int64(0)
		for _, b := range bindings {
			var sess model.Session
			if err := tx.Select("session_id", "owner_id", "direct_key").
				Where("session_id = ?", b.SessionID).First(&sess).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			oldDirectKey := computeAgentSessionDirectKey(sess.OwnerID, b.AgentID, opencodeDeepseekProviderKeyOldBucket, b.BindingID)
			if sess.DirectKey == nil || *sess.DirectKey != oldDirectKey {
				// Not on the old formula's output: already migrated, never
				// wrong to begin with (e.g. deveco — see the comment on
				// opencodeDeepseekProviderKeyTargets), or changed by
				// something else. Leave it alone rather than guess.
				continue
			}
			newDirectKey := computeAgentSessionDirectKey(sess.OwnerID, b.AgentID, newProviderKey, b.BindingID)
			// UpdateColumn, not Update: skip GORM's hooks/auto-timestamps so
			// this relabeling doesn't bump sessions.updated_at and make a
			// batch of old sessions jump to the top of the chat list.
			result := tx.Model(&model.Session{}).Where("session_id = ?", sess.SessionID).
				UpdateColumn("direct_key", newDirectKey)
			if result.Error != nil {
				return result.Error
			}
			directKeysUpdated += result.RowsAffected
		}

		// 2) agent_session_bindings.provider_key
		bindingsResult := tx.Model(&model.AgentSessionBinding{}).
			Where("agent_id IN ? AND provider_key = ?", agentIDs, opencodeDeepseekProviderKeyOldBucket).
			UpdateColumn("provider_key", newProviderKey)
		if bindingsResult.Error != nil {
			return bindingsResult.Error
		}

		// 3) agent_session_sync_states.provider_key
		syncStatesResult := tx.Model(&model.AgentSessionSyncState{}).
			Where("agent_id IN ? AND provider_key = ?", agentIDs, opencodeDeepseekProviderKeyOldBucket).
			UpdateColumn("provider_key", newProviderKey)
		if syncStatesResult.Error != nil {
			return syncStatesResult.Error
		}

		// 4) agent_native_message_imports.provider_key
		nativeMsgResult := tx.Model(&model.AgentNativeMessageImport{}).
			Where("agent_id IN ? AND provider_key = ?", agentIDs, opencodeDeepseekProviderKeyOldBucket).
			UpdateColumn("provider_key", newProviderKey)
		if nativeMsgResult.Error != nil {
			return nativeMsgResult.Error
		}

		logger.L.Infof(
			"opencode_deepseek_provider_key_migration: client_type=%s bucket=%s direct_keys=%d bindings=%d sync_states=%d native_message_imports=%d",
			clientType, newProviderKey, directKeysUpdated, bindingsResult.RowsAffected, syncStatesResult.RowsAffected, nativeMsgResult.RowsAffected,
		)
		return nil
	})
}

// computeAgentSessionDirectKey mirrors SessionCreateForAgentBinding's formula
// in session_service_create.go exactly (same suffix shape, same hex prefix
// length) so migrated rows stay lookup-compatible with future binds.
func computeAgentSessionDirectKey(ownerID, agentID int64, providerKey, agentSessionID string) string {
	suffix := providerKey + ":" + agentSessionID
	sum := sha256.Sum256([]byte(suffix))
	return fmt.Sprintf("agent-session:%d:%d:%s", ownerID, agentID, hex.EncodeToString(sum[:])[:32])
}
