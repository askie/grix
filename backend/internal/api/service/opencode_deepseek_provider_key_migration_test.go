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

func seedOpencodeDeepseekMigrationFixture(t *testing.T, agentID int64, ownerID int64, clientType, sessionID, bindingID string) {
	t.Helper()
	if err := store.DB.Create(&model.Agent{
		ID:              agentID,
		AgentName:       "fixture-" + clientType,
		OwnerID:         ownerID,
		AgentClientType: clientType,
	}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	oldDirectKey := computeAgentSessionDirectKey(ownerID, agentID, opencodeDeepseekProviderKeyOldBucket, bindingID)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		DirectKey:   &oldDirectKey,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := store.DB.Create(&model.AgentSessionBinding{
		AgentID:     agentID,
		SessionID:   sessionID,
		ProviderKey: opencodeDeepseekProviderKeyOldBucket,
		BindingID:   bindingID,
		Status:      "ready",
	}).Error; err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	if err := store.DB.Create(&model.AgentSessionSyncState{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ProviderKey: opencodeDeepseekProviderKeyOldBucket,
		BindingID:   bindingID,
		Status:      model.AgentSessionSyncStatusCompleted,
		Imported:    3,
	}).Error; err != nil {
		t.Fatalf("seed sync state: %v", err)
	}
	if err := store.DB.Create(&model.AgentNativeMessageImport{
		AgentID:         agentID,
		ProviderKey:     opencodeDeepseekProviderKeyOldBucket,
		BindingID:       bindingID,
		NativeMessageID: "native-msg-1",
		SessionID:       sessionID,
		MsgID:           9001,
		NativeCreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed native message import: %v", err)
	}
}

// TestRunOpencodeDeepseekProviderKeyMigration_MigratesAllFourTables covers the
// full backfill for both client types in one pass and asserts every
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
// asserts the migration only reclassifies opencode/deepseek — a genuinely
// unclassified ACP client type (e.g. qodercli) that also defaults to "acp"
// must keep its rows untouched.
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
