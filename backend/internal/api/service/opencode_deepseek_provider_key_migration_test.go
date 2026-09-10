package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// seedMigrationFixtureOpts lets each test pick the exact starting
// provider_key per table, instead of assuming everything starts on the old
// bucket — needed to reproduce mismatched states (a binding already on the
// new bucket while other tables are still on the old one, or a row that was
// always correct and must be left alone).
type seedMigrationFixtureOpts struct {
	agentID          int64
	ownerID          int64
	clientType       string
	sessionID        string
	bindingID        string
	bindingBucket    string
	sessionDirectKey string // full direct_key value, not just a bucket
	syncStateBucket  string
	nativeMsgBucket  string
	nativeMsgID      string
}

func seedMigrationFixture(t *testing.T, o seedMigrationFixtureOpts) {
	t.Helper()
	if o.nativeMsgID == "" {
		o.nativeMsgID = "native-msg-1"
	}
	if err := store.DB.Create(&model.Agent{
		ID:              o.agentID,
		AgentName:       "fixture-" + o.clientType,
		OwnerID:         o.ownerID,
		AgentClientType: o.clientType,
	}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   o.sessionID,
		DirectKey:   &o.sessionDirectKey,
		OwnerID:     o.ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := store.DB.Create(&model.AgentSessionBinding{
		AgentID:     o.agentID,
		SessionID:   o.sessionID,
		ProviderKey: o.bindingBucket,
		BindingID:   o.bindingID,
		Status:      "ready",
	}).Error; err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	if err := store.DB.Create(&model.AgentSessionSyncState{
		AgentID:     o.agentID,
		OwnerID:     o.ownerID,
		SessionID:   o.sessionID,
		ProviderKey: o.syncStateBucket,
		BindingID:   o.bindingID,
		Status:      model.AgentSessionSyncStatusCompleted,
		Imported:    3,
	}).Error; err != nil {
		t.Fatalf("seed sync state: %v", err)
	}
	if err := store.DB.Create(&model.AgentNativeMessageImport{
		AgentID:         o.agentID,
		ProviderKey:     o.nativeMsgBucket,
		BindingID:       o.bindingID,
		NativeMessageID: o.nativeMsgID,
		SessionID:       o.sessionID,
		MsgID:           9001,
		NativeCreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed native message import: %v", err)
	}
}

// seedOpencodeDeepseekMigrationFixture is the common case: every table starts
// on the old bucket with a direct_key hashed by the old formula.
func seedOpencodeDeepseekMigrationFixture(t *testing.T, agentID int64, ownerID int64, clientType, sessionID, bindingID string) {
	t.Helper()
	seedMigrationFixture(t, seedMigrationFixtureOpts{
		agentID:          agentID,
		ownerID:          ownerID,
		clientType:       clientType,
		sessionID:        sessionID,
		bindingID:        bindingID,
		bindingBucket:    opencodeDeepseekProviderKeyOldBucket,
		sessionDirectKey: computeAgentSessionDirectKey(ownerID, agentID, opencodeDeepseekProviderKeyOldBucket, bindingID),
		syncStateBucket:  opencodeDeepseekProviderKeyOldBucket,
		nativeMsgBucket:  opencodeDeepseekProviderKeyOldBucket,
	})
}

// TestRunOpencodeDeepseekProviderKeyMigration_MigratesAllFourTables covers the
// full backfill for opencode/deepseek/deveco in one pass and asserts every
// provider_key-keyed table moved together, plus sessions.direct_key was
// recomputed with the new bucket.
func TestRunOpencodeDeepseekProviderKeyMigration_MigratesAllFourTables(t *testing.T) {
	logger.Init()
	store.DB = testutil.NewTestDB().DB

	const ownerID = int64(5001)
	seedOpencodeDeepseekMigrationFixture(t, 6001, ownerID, model.AgentClientTypeOpenCode, "sess-opencode-1", "native-oc-1")
	seedOpencodeDeepseekMigrationFixture(t, 6002, ownerID, model.AgentClientTypeDeepSeek, "sess-deepseek-1", "native-ds-1")

	if err := RunOpencodeDeepseekProviderKeyMigration(context.Background()); err != nil {
		t.Fatalf("migration: %v", err)
	}

	cases := []struct {
		agentID    int64
		sessionID  string
		bindingID  string
		wantBucket string
	}{
		{6001, "sess-opencode-1", "native-oc-1", "opencode"},
		{6002, "sess-deepseek-1", "native-ds-1", "deepseek-harness"},
	}
	for _, c := range cases {
		var binding model.AgentSessionBinding
		if err := store.DB.Where("agent_id = ? AND session_id = ?", c.agentID, c.sessionID).First(&binding).Error; err != nil {
			t.Fatalf("load binding: %v", err)
		}
		if binding.ProviderKey != c.wantBucket {
			t.Fatalf("binding.provider_key = %q, want %q", binding.ProviderKey, c.wantBucket)
		}

		var syncState model.AgentSessionSyncState
		if err := store.DB.Where("agent_id = ? AND session_id = ? AND provider_key = ?", c.agentID, c.sessionID, c.wantBucket).First(&syncState).Error; err != nil {
			t.Fatalf("load sync state under new bucket: %v", err)
		}
		if syncState.Status != model.AgentSessionSyncStatusCompleted || syncState.Imported != 3 {
			t.Fatalf("sync state content changed: %+v", syncState)
		}

		var msgImport model.AgentNativeMessageImport
		if err := store.DB.Where("agent_id = ? AND provider_key = ? AND binding_id = ?", c.agentID, c.wantBucket, c.bindingID).First(&msgImport).Error; err != nil {
			t.Fatalf("load native message import under new bucket: %v", err)
		}

		var sess model.Session
		if err := store.DB.Where("session_id = ?", c.sessionID).First(&sess).Error; err != nil {
			t.Fatalf("load session: %v", err)
		}
		wantDirectKey := computeAgentSessionDirectKey(ownerID, c.agentID, c.wantBucket, c.bindingID)
		if sess.DirectKey == nil || *sess.DirectKey != wantDirectKey {
			t.Fatalf("session.direct_key = %v, want %q", sess.DirectKey, wantDirectKey)
		}
	}

	// Idempotent: rerunning must not error and must not change anything further.
	if err := RunOpencodeDeepseekProviderKeyMigration(context.Background()); err != nil {
		t.Fatalf("rerun migration: %v", err)
	}
	var recheck model.AgentSessionBinding
	if err := store.DB.Where("agent_id = ? AND session_id = ?", int64(6001), "sess-opencode-1").First(&recheck).Error; err != nil {
		t.Fatalf("reload binding: %v", err)
	}
	if recheck.ProviderKey != "opencode" {
		t.Fatalf("rerun changed provider_key to %q", recheck.ProviderKey)
	}
}

// TestRunOpencodeDeepseekProviderKeyMigration_DevecoOnlyMovesTheMismatchedRows
// covers deveco's narrower mismatch (see the comment on
// opencodeDeepseekProviderKeyTargets): round5 already fixed the backend
// bucket before round6 fixed the connector-side override, so a deveco
// binding created in that window has provider_key="acp" (wrong) while
// sync_state and direct_key are already correct ("deveco"). The migration
// must fix the binding and native-message-import rows without touching (or
// corrupting) the already-correct sync_state or direct_key.
func TestRunOpencodeDeepseekProviderKeyMigration_DevecoOnlyMovesTheMismatchedRows(t *testing.T) {
	logger.Init()
	store.DB = testutil.NewTestDB().DB

	const ownerID = int64(5004)
	const agentID = int64(6301)
	const bindingID = "native-deveco-1"
	correctDirectKey := computeAgentSessionDirectKey(ownerID, agentID, "deveco", bindingID)
	seedMigrationFixture(t, seedMigrationFixtureOpts{
		agentID:          agentID,
		ownerID:          ownerID,
		clientType:       model.AgentClientTypeDeveco,
		sessionID:        "sess-deveco-1",
		bindingID:        bindingID,
		bindingBucket:    opencodeDeepseekProviderKeyOldBucket, // wrong: connector override
		sessionDirectKey: correctDirectKey,                     // already correct
		syncStateBucket:  "deveco",                             // already correct
		nativeMsgBucket:  opencodeDeepseekProviderKeyOldBucket, // wrong
	})

	if err := RunOpencodeDeepseekProviderKeyMigration(context.Background()); err != nil {
		t.Fatalf("migration: %v", err)
	}

	var binding model.AgentSessionBinding
	if err := store.DB.Where("agent_id = ? AND session_id = ?", agentID, "sess-deveco-1").First(&binding).Error; err != nil {
		t.Fatalf("load binding: %v", err)
	}
	if binding.ProviderKey != "deveco" {
		t.Fatalf("binding.provider_key = %q, want deveco", binding.ProviderKey)
	}

	var msgImport model.AgentNativeMessageImport
	if err := store.DB.Where("agent_id = ? AND binding_id = ?", agentID, bindingID).First(&msgImport).Error; err != nil {
		t.Fatalf("load native message import: %v", err)
	}
	if msgImport.ProviderKey != "deveco" {
		t.Fatalf("native_message_import.provider_key = %q, want deveco", msgImport.ProviderKey)
	}

	var sess model.Session
	if err := store.DB.Where("session_id = ?", "sess-deveco-1").First(&sess).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if sess.DirectKey == nil || *sess.DirectKey != correctDirectKey {
		t.Fatalf("session.direct_key changed from the already-correct value: got %v, want %q", sess.DirectKey, correctDirectKey)
	}
}

// TestRunOpencodeDeepseekProviderKeyMigration_FixesDirectKeyWhenBindingAlreadyOnNewBucket
// covers the deployment-order case the review flagged: if grix-connector's
// fix reaches a user before the backend's, agent_session_bindings.provider_key
// can already read the NEW bucket (the connector's honest report wins via
// firstTrimmed) while agent_session_sync_states / native_message_imports /
// sessions.direct_key are still on the old one (computed by the not-yet-fixed
// backend at bind time). Selecting bindings only from the old bucket would
// permanently miss this row's direct_key.
func TestRunOpencodeDeepseekProviderKeyMigration_FixesDirectKeyWhenBindingAlreadyOnNewBucket(t *testing.T) {
	logger.Init()
	store.DB = testutil.NewTestDB().DB

	const ownerID = int64(5005)
	const agentID = int64(6401)
	const bindingID = "native-oc-early-connector-1"
	seedMigrationFixture(t, seedMigrationFixtureOpts{
		agentID:          agentID,
		ownerID:          ownerID,
		clientType:       model.AgentClientTypeOpenCode,
		sessionID:        "sess-opencode-early-1",
		bindingID:        bindingID,
		bindingBucket:    "opencode",                                                                                      // connector already fixed
		sessionDirectKey: computeAgentSessionDirectKey(ownerID, agentID, opencodeDeepseekProviderKeyOldBucket, bindingID), // backend not fixed yet
		syncStateBucket:  opencodeDeepseekProviderKeyOldBucket,
		nativeMsgBucket:  opencodeDeepseekProviderKeyOldBucket,
	})

	if err := RunOpencodeDeepseekProviderKeyMigration(context.Background()); err != nil {
		t.Fatalf("migration: %v", err)
	}

	var sess model.Session
	if err := store.DB.Where("session_id = ?", "sess-opencode-early-1").First(&sess).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	wantDirectKey := computeAgentSessionDirectKey(ownerID, agentID, "opencode", bindingID)
	if sess.DirectKey == nil || *sess.DirectKey != wantDirectKey {
		t.Fatalf("session.direct_key = %v, want %q (binding was already on the new bucket)", sess.DirectKey, wantDirectKey)
	}

	var syncState model.AgentSessionSyncState
	if err := store.DB.Where("agent_id = ? AND session_id = ?", agentID, "sess-opencode-early-1").First(&syncState).Error; err != nil {
		t.Fatalf("load sync state: %v", err)
	}
	if syncState.ProviderKey != "opencode" {
		t.Fatalf("sync_state.provider_key = %q, want opencode", syncState.ProviderKey)
	}
}

// TestRunOpencodeDeepseekProviderKeyMigration_RollsBackOnPartialFailure proves
// the four steps for one client type are transactional: if the last step
// fails (here, a native_message_imports row collides with the unique index
// once its provider_key moves), the earlier steps for that client type must
// not have taken effect either.
func TestRunOpencodeDeepseekProviderKeyMigration_RollsBackOnPartialFailure(t *testing.T) {
	logger.Init()
	store.DB = testutil.NewTestDB().DB

	const ownerID = int64(5006)
	const agentID = int64(6501)
	const bindingID = "native-oc-conflict-1"
	seedOpencodeDeepseekMigrationFixture(t, agentID, ownerID, model.AgentClientTypeOpenCode, "sess-opencode-conflict-1", bindingID)

	// A row already sitting on the new bucket with the same
	// (agent_id, provider_key, binding_id, native_message_id) that step 4's
	// bulk UPDATE will try to produce for the "acp" row seeded above —
	// moving it collides with this row's unique index.
	if err := store.DB.Create(&model.AgentNativeMessageImport{
		AgentID:         agentID,
		ProviderKey:     "opencode",
		BindingID:       bindingID,
		NativeMessageID: "native-msg-1",
		SessionID:       "sess-opencode-conflict-1",
		MsgID:           9002,
		NativeCreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed conflicting native message import: %v", err)
	}

	if err := RunOpencodeDeepseekProviderKeyMigration(context.Background()); err == nil {
		t.Fatal("migration: want an error from the unique-index collision, got nil")
	}

	var binding model.AgentSessionBinding
	if err := store.DB.Where("agent_id = ? AND session_id = ?", agentID, "sess-opencode-conflict-1").First(&binding).Error; err != nil {
		t.Fatalf("load binding: %v", err)
	}
	if binding.ProviderKey != opencodeDeepseekProviderKeyOldBucket {
		t.Fatalf("binding.provider_key = %q, want the transaction rolled back to %q", binding.ProviderKey, opencodeDeepseekProviderKeyOldBucket)
	}

	var sess model.Session
	if err := store.DB.Where("session_id = ?", "sess-opencode-conflict-1").First(&sess).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	wantOldDirectKey := computeAgentSessionDirectKey(ownerID, agentID, opencodeDeepseekProviderKeyOldBucket, bindingID)
	if sess.DirectKey == nil || *sess.DirectKey != wantOldDirectKey {
		t.Fatalf("session.direct_key = %v, want the rolled-back old value %q", sess.DirectKey, wantOldDirectKey)
	}
}

// TestRunOpencodeDeepseekProviderKeyMigration_ReimportReachesSameSession is the
// scenario the migration exists for: after the bucket moves, re-binding the
// same native session (same provider_key + agent_session_id going into
// SessionCreateForAgentBinding, exactly as agent_session_bind.go computes
// directSuffix) must resolve to the SAME aibot session, not create a new one.
func TestRunOpencodeDeepseekProviderKeyMigration_ReimportReachesSameSession(t *testing.T) {
	logger.Init()
	store.DB = testutil.NewTestDB().DB

	const ownerID = int64(5002)
	const agentID = int64(6101)
	const bindingID = "native-oc-reimport-1"
	seedOpencodeDeepseekMigrationFixture(t, agentID, ownerID, model.AgentClientTypeOpenCode, "sess-opencode-reimport-1", bindingID)

	if err := RunOpencodeDeepseekProviderKeyMigration(context.Background()); err != nil {
		t.Fatalf("migration: %v", err)
	}

	// Re-import: same directSuffix formula normalizeAgentSessionProviderKey +
	// agent_session_bind.go would build post-fix ("opencode:" + agentSessionID).
	resp, err := SessionCreateForAgentBinding(ownerID, agentID, "opencode:"+bindingID, "")
	if err != nil {
		t.Fatalf("re-bind: %v", err)
	}
	if resp.IsNew {
		t.Fatalf("re-import created a new session instead of reusing sess-opencode-reimport-1")
	}
	if resp.SessionID != "sess-opencode-reimport-1" {
		t.Fatalf("re-import resolved session_id = %q, want the original session", resp.SessionID)
	}
}

// TestRunOpencodeDeepseekProviderKeyMigration_LeavesOtherAcpAgentsAlone
// asserts the migration only reclassifies opencode/deepseek/deveco — a
// genuinely unclassified ACP client type (e.g. qodercli) that also defaults
// to "acp" must keep its rows untouched.
func TestRunOpencodeDeepseekProviderKeyMigration_LeavesOtherAcpAgentsAlone(t *testing.T) {
	logger.Init()
	store.DB = testutil.NewTestDB().DB

	const ownerID = int64(5003)
	seedOpencodeDeepseekMigrationFixture(t, 6201, ownerID, model.AgentClientTypeQoderCLI, "sess-qodercli-1", "native-qc-1")

	if err := RunOpencodeDeepseekProviderKeyMigration(context.Background()); err != nil {
		t.Fatalf("migration: %v", err)
	}

	var binding model.AgentSessionBinding
	if err := store.DB.Where("agent_id = ? AND session_id = ?", int64(6201), "sess-qodercli-1").First(&binding).Error; err != nil {
		t.Fatalf("load binding: %v", err)
	}
	if binding.ProviderKey != opencodeDeepseekProviderKeyOldBucket {
		t.Fatalf("unrelated acp agent's provider_key changed to %q", binding.ProviderKey)
	}
}
