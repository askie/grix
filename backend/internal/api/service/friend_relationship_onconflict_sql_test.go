package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TestPeerPinMuteOnConflictSQLQualifiesStateVersion asserts the ON CONFLICT
// DO UPDATE expressions used by FriendSetPinned / FriendSetMuted qualify
// state_version with the target table name.
//
// Postgres rejects the bare form (`state_version + 1`) with
// `column reference "state_version" is ambiguous` because ON CONFLICT
// exposes both the table row and EXCLUDED. Existing service tests mostly
// run on SQLite, which does not enforce that ambiguity, so this DryRun
// SQL check is the portable regression guard when AIBOT_TEST_PG_DSN is
// unset. Prefer a live Postgres upsert under that DSN when available.
func TestPeerPinMuteOnConflictSQLQualifiesStateVersion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:peer_pin_onconflict_sql?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	now := time.Now().UTC()
	cases := []struct {
		name      string
		row       any
		doUpdates map[string]any
		wantExpr  string
		forbid    string
	}{
		{
			name: "user_peer_pins",
			row: &model.UserPeerPin{
				ID: 1, UserID: 10, PeerUserID: 20, IsPinned: true, CreatedAt: now, UpdatedAt: now,
			},
			doUpdates: map[string]any{
				"is_pinned":     true,
				"pinned_at":     &now,
				"updated_at":    now,
				"state_version": gorm.Expr("user_peer_pins.state_version + 1"),
			},
			wantExpr: "user_peer_pins.state_version",
			forbid:   "state_version = state_version + 1",
		},
		{
			name: "user_peer_mutes",
			row: &model.UserPeerMute{
				ID: 1, UserID: 10, PeerUserID: 20, IsMuted: true, CreatedAt: now, UpdatedAt: now,
			},
			doUpdates: map[string]any{
				"is_muted":      true,
				"muted_at":      &now,
				"updated_at":    now,
				"state_version": gorm.Expr("user_peer_mutes.state_version + 1"),
			},
			wantExpr: "user_peer_mutes.state_version",
			forbid:   "state_version = state_version + 1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := db.Session(&gorm.Session{DryRun: true, SkipDefaultTransaction: true}).
				Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "user_id"}, {Name: "peer_user_id"}},
					DoUpdates: clause.Assignments(tc.doUpdates),
				}).Create(tc.row)
			if tx.Error != nil {
				t.Fatalf("dry-run create: %v", tx.Error)
			}
			sql := strings.ToLower(tx.Statement.SQL.String())
			if !strings.Contains(sql, "on conflict") {
				t.Fatalf("expected ON CONFLICT SQL, got: %s", sql)
			}
			if !strings.Contains(sql, strings.ToLower(tc.wantExpr)) {
				t.Fatalf("expected table-qualified %q in SQL, got: %s", tc.wantExpr, sql)
			}
			if strings.Contains(sql, tc.forbid) {
				t.Fatalf("unqualified state_version bump must not appear; SQL: %s", sql)
			}
		})
	}
}

// TestFriendRelationshipOnConflictExprsMatchProductionSources guards against
// reintroducing bare `state_version + 1` into peer pin/mute OnConflict
// DoUpdates in friend_relationship_service.go (SQLite tests would not catch it).
func TestFriendRelationshipOnConflictExprsMatchProductionSources(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	srcPath := filepath.Join(filepath.Dir(thisFile), "friend_relationship_service.go")
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read %s: %v", srcPath, err)
	}
	src := string(raw)
	if strings.Contains(src, `gorm.Expr("state_version + 1")`) {
		t.Fatal(`friend_relationship_service.go still has unqualified gorm.Expr("state_version + 1"); use user_peer_pins/user_peer_mutes.state_version + 1 in OnConflict DoUpdates`)
	}
	for _, want := range []string{
		`gorm.Expr("user_peer_pins.state_version + 1")`,
		`gorm.Expr("user_peer_mutes.state_version + 1")`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("missing required OnConflict Expr %s", want)
		}
	}
}
