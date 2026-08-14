package agenttoolbar_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/cursor"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

type cursorRateLimitsHandshakeFixture struct {
	Contract              string         `json:"contract"`
	LocalActionResult     map[string]any `json:"local_action_result"`
	BindingMetaRateLimits map[string]any `json:"binding_meta_rate_limits"`
	Toolbar               struct {
		Items []struct {
			ItemID         string  `json:"itemId"`
			CenterText     string  `json:"centerText"`
			Percent        float64 `json:"percent"`
			LocalAction    string  `json:"localAction"`
			ProgressDetail string  `json:"progressDetail"`
		} `json:"items"`
	} `json:"toolbar"`
	SlotSemantics map[string]string `json:"slot_semantics"`
}

func loadCursorRateLimitsHandshakeFixture(t *testing.T) (cursorRateLimitsHandshakeFixture, []byte) {
	t.Helper()
	path := filepath.Join("testdata", "cursor-rate-limits-handshake.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx cursorRateLimitsHandshakeFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return fx, raw
}

func TestCursorRateLimitsHandshake_FixtureContract(t *testing.T) {
	fx, _ := loadCursorRateLimitsHandshakeFixture(t)
	if fx.Contract != "cursor-rate-limits-v1" {
		t.Fatalf("contract=%q", fx.Contract)
	}
	if fx.SlotSemantics["fiveHour"] == "" || fx.SlotSemantics["sevenDay"] == "" {
		t.Fatalf("slot_semantics incomplete: %+v", fx.SlotSemantics)
	}
	if len(fx.Toolbar.Items) != 2 {
		t.Fatalf("toolbar items=%d want 2", len(fx.Toolbar.Items))
	}
	if fx.Toolbar.Items[0].CenterText != "M" || fx.Toolbar.Items[1].CenterText != "API" {
		t.Fatalf("centerText=%q/%q want M/API", fx.Toolbar.Items[0].CenterText, fx.Toolbar.Items[1].CenterText)
	}
}

func TestCursorRateLimitsHandshake_MatchesConnectorFixtureBytes(t *testing.T) {
	_, localRaw := loadCursorRateLimitsHandshakeFixture(t)
	localSum := sha256.Sum256(localRaw)

	// 相对本包：.../aibot/backend/internal/agenttoolbar → ../../../../grix-connector/...
	connectorPath := filepath.Join("..", "..", "..", "..", "grix-connector", "tests", "fixtures", "cursor-rate-limits-handshake.json")
	remoteRaw, err := os.ReadFile(connectorPath)
	if err != nil {
		// CI / 单仓 checkout 没有并列 grix-connector；本机旁路检出时再做字节对齐校验。
		t.Skipf("skip connector fixture bytes check: %v", err)
	}
	remoteSum := sha256.Sum256(remoteRaw)
	if hex.EncodeToString(localSum[:]) != hex.EncodeToString(remoteSum[:]) {
		t.Fatalf("handshake fixture diverged from grix-connector\nlocal=%s\nremote=%s",
			hex.EncodeToString(localSum[:]), hex.EncodeToString(remoteSum[:]))
	}
}

func TestCursorRateLimitsHandshake_ToolbarConsumesFixtureMeta(t *testing.T) {
	fx, _ := loadCursorRateLimitsHandshakeFixture(t)
	pkg := cursor.New()
	snapshot, err := pkg.Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-hs"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypeCursor,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "set_model", "set_mode", "get_rate_limits"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/project",
			Meta: map[string]any{
				"model_id":    "auto",
				"rate_limits": fx.BindingMetaRateLimits,
			},
		},
		Run: toolruntime.RunState{},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	for _, want := range fx.Toolbar.Items {
		item, ok := snapshot.FindItem(want.ItemID)
		if !ok {
			t.Fatalf("missing item %q", want.ItemID)
		}
		if item.CenterText != want.CenterText {
			t.Fatalf("%s centerText=%q want %q", want.ItemID, item.CenterText, want.CenterText)
		}
		if item.Percent != want.Percent {
			t.Fatalf("%s percent=%v want %v", want.ItemID, item.Percent, want.Percent)
		}
		if item.LocalAction != want.LocalAction {
			t.Fatalf("%s localAction=%q want %q", want.ItemID, item.LocalAction, want.LocalAction)
		}
		if item.ProgressDetail != want.ProgressDetail {
			t.Fatalf("%s progressDetail=%q want %q", want.ItemID, item.ProgressDetail, want.ProgressDetail)
		}
	}
}
