package core

import (
	"context"
	"testing"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/conversationaudit"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// 快照注入对话审计开关状态：feature gate 开启时下发实际值，否则字段缺席（nil）。
func TestGetSnapshotInjectsAuditEnabled(t *testing.T) {
	tdb := testutil.NewTestDB()
	previous := store.DB
	store.DB = tdb.DB
	featuregate.InvalidateCache()
	t.Cleanup(func() {
		featuregate.InvalidateCache()
		store.DB = previous
		tdb.Close()
	})

	const ownerID int64 = 7101
	const agentID int64 = 8101
	if _, err := featuregate.CreateGate(conversationaudit.FeatureGateKey, "test audit", model.FeatureStatusEnabled); err != nil {
		t.Fatalf("enable audit gate: %v", err)
	}
	if err := conversationaudit.SetAuditEnabled(ownerID, agentID, true); err != nil {
		t.Fatalf("set audit pref: %v", err)
	}

	newSvc := func() *Service {
		return NewService(
			testResolver{buildInput: BuildInput{
				OwnerID: ownerID,
				Session: SessionInfo{SessionID: "s1", SessionType: model.SessionTypeDirect},
				Agent:   AgentInfo{AgentID: agentID},
			}},
			testRegistry{pkg: testPackage{snapshot: toolprotocol.Snapshot{Visible: true}}},
			&testCache{},
			noopNotifier{},
			noopExecutor{},
		)
	}

	snapshot, err := newSvc().GetSnapshot(context.Background(), ownerID, "s1", 0)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if snapshot.AuditEnabled == nil || !*snapshot.AuditEnabled {
		t.Fatalf("snapshot must carry enabled audit state, got=%v", snapshot.AuditEnabled)
	}

	// Gate off → 字段缺席，前端按旧后端回退。
	if err := featuregate.UpdateGateStatus(conversationaudit.FeatureGateKey, model.FeatureStatusDisabled); err != nil {
		t.Fatalf("disable audit gate: %v", err)
	}
	featuregate.InvalidateCache()
	snapshot, err = newSvc().GetSnapshot(context.Background(), ownerID, "s1", 0)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if snapshot.AuditEnabled != nil {
		t.Fatalf("gate off must omit audit state, got=%v", *snapshot.AuditEnabled)
	}
}
