package conversationaudit

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"go.uber.org/zap"
)

func setupPreferenceTestDB(t *testing.T) {
	t.Helper()
	tdb := testutil.NewTestDB()
	previous := store.DB
	store.DB = tdb.DB
	featuregate.InvalidateCache()
	t.Cleanup(func() {
		featuregate.InvalidateCache()
		store.DB = previous
		tdb.Close()
	})
}

func TestAuditEnabledDefaultsFalseAndUpserts(t *testing.T) {
	setupPreferenceTestDB(t)

	enabled, err := GetAuditEnabled(7001, 8001)
	if err != nil || enabled {
		t.Fatalf("missing row must default to disabled, enabled=%v err=%v", enabled, err)
	}

	if err := SetAuditEnabled(7001, 8001, true); err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	enabled, err = GetAuditEnabled(7001, 8001)
	if err != nil || !enabled {
		t.Fatalf("expected enabled after set, enabled=%v err=%v", enabled, err)
	}

	// Upsert must not duplicate rows and must flip the value back.
	if err := SetAuditEnabled(7001, 8001, false); err != nil {
		t.Fatalf("set disabled: %v", err)
	}
	enabled, err = GetAuditEnabled(7001, 8001)
	if err != nil || enabled {
		t.Fatalf("expected disabled after second set, enabled=%v err=%v", enabled, err)
	}
	var count int64
	if err := store.DB.Model(&model.ConversationAuditPref{}).
		Where("user_id = ? AND agent_id = ?", 7001, 8001).
		Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("upsert must keep a single row, count=%d err=%v", count, err)
	}

	// State is scoped per (user, agent).
	if err := SetAuditEnabled(7001, 8001, true); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	other, err := GetAuditEnabled(7002, 8001)
	if err != nil || other {
		t.Fatalf("other user must stay disabled, enabled=%v err=%v", other, err)
	}
	other, err = GetAuditEnabled(7001, 8002)
	if err != nil || other {
		t.Fatalf("other agent must stay disabled, enabled=%v err=%v", other, err)
	}

	if err := SetAuditEnabled(0, 8001, true); err == nil {
		t.Fatalf("invalid ids must be rejected")
	}
}

func TestSnapshotAuditEnabledVisibility(t *testing.T) {
	setupPreferenceTestDB(t)

	if got := SnapshotAuditEnabled(7001, 8001); got != nil {
		t.Fatalf("feature gate off must omit the field, got=%v", *got)
	}
	if _, err := featuregate.CreateGate(FeatureGateKey, "test audit", model.FeatureStatusEnabled); err != nil {
		t.Fatalf("enable audit gate: %v", err)
	}

	got := SnapshotAuditEnabled(7001, 8001)
	if got == nil || *got {
		t.Fatalf("gate on without pref must report disabled, got=%v", got)
	}
	if err := SetAuditEnabled(7001, 8001, true); err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	got = SnapshotAuditEnabled(7001, 8001)
	if got == nil || !*got {
		t.Fatalf("gate on with pref must report enabled, got=%v", got)
	}
	if got := SnapshotAuditEnabled(7001, 0); got != nil {
		t.Fatalf("unresolved agent must omit the field, got=%v", *got)
	}
}

func TestApplyTurnPreferenceServerAuthoritative(t *testing.T) {
	setupPreferenceTestDB(t)
	if _, err := featuregate.CreateGate(FeatureGateKey, "test audit", model.FeatureStatusEnabled); err != nil {
		t.Fatalf("enable audit gate: %v", err)
	}
	if err := SetAuditEnabled(7001, 8001, true); err != nil {
		t.Fatalf("seed audit pref: %v", err)
	}

	// 偏好开启：注入标记，同时保留其他 extra 键、覆盖客户端伪造的 audit 键。
	out := ApplyTurnPreference(json.RawMessage(`{"mention":{"uids":[1]},"audit":{"enabled":false}}`), 7001, []int64{8001})
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if _, ok := envelope["mention"]; !ok {
		t.Fatalf("other extra keys must be preserved: %s", out)
	}
	var audit struct {
		Enabled bool   `json:"enabled"`
		Scope   string `json:"scope"`
	}
	if err := json.Unmarshal(envelope["audit"], &audit); err != nil || !audit.Enabled || audit.Scope != "turn" {
		t.Fatalf("server must inject the turn mark: %s", envelope["audit"])
	}

	// 偏好关闭：客户端伪造的 enabled=true 被剥离。
	out = ApplyTurnPreference(json.RawMessage(`{"audit":{"enabled":true,"scope":"turn"}}`), 7001, []int64{8002})
	if string(out) != "" {
		var stripped map[string]json.RawMessage
		if err := json.Unmarshal(out, &stripped); err != nil {
			t.Fatalf("unmarshal stripped output: %v", err)
		}
		if _, ok := stripped["audit"]; ok {
			t.Fatalf("client-supplied audit key must be stripped: %s", out)
		}
	}

	// Gate 关闭：不写标记。
	if err := featuregate.UpdateGateStatus(FeatureGateKey, model.FeatureStatusDisabled); err != nil {
		t.Fatalf("disable audit gate: %v", err)
	}
	featuregate.InvalidateCache()
	out = ApplyTurnPreference(json.RawMessage(`{"audit":{"enabled":true,"scope":"turn"}}`), 7001, []int64{8001})
	var gated map[string]json.RawMessage
	if len(out) > 0 {
		if err := json.Unmarshal(out, &gated); err != nil {
			t.Fatalf("unmarshal gated output: %v", err)
		}
	}
	if _, ok := gated["audit"]; ok {
		t.Fatalf("gate off must not mark: %s", out)
	}

	// 无候选 agent / 空 extra：不产生标记也不报错。
	if out := ApplyTurnPreference(nil, 7001, nil); len(out) != 0 {
		t.Fatalf("empty input must stay empty: %s", out)
	}
}

func TestAnyAgentEnabled(t *testing.T) {
	setupPreferenceTestDB(t)
	if err := SetAuditEnabled(7001, 8001, true); err != nil {
		t.Fatalf("seed audit pref: %v", err)
	}
	enabled, err := AnyAgentEnabled(7001, []int64{8002, 8001})
	if err != nil || !enabled {
		t.Fatalf("expected enabled when any agent matches, enabled=%v err=%v", enabled, err)
	}
	enabled, err = AnyAgentEnabled(7001, []int64{8002})
	if err != nil || enabled {
		t.Fatalf("expected disabled when none matches, enabled=%v err=%v", enabled, err)
	}
	enabled, err = AnyAgentEnabled(7001, nil)
	if err != nil || enabled {
		t.Fatalf("empty agent list must be disabled, enabled=%v err=%v", enabled, err)
	}
}

func TestApplyTurnPreferenceDropsMalformedExtra(t *testing.T) {
	setupPreferenceTestDB(t)
	if logger.L == nil {
		logger.L = zap.NewNop().Sugar()
	}
	// 畸形 extra 必须整体丢弃，客户端藏在里面的 audit 键不得透传。
	out := ApplyTurnPreference(json.RawMessage(`{"audit":{"enabled":true,"scope":"turn"},broken`), 7001, []int64{8001})
	if len(out) != 0 {
		t.Fatalf("malformed extra must be dropped entirely, got %s", out)
	}
}
