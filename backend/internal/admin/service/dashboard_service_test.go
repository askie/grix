package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestLoadDailyRegistrantsUsesShanghaiDayAndSkipsDeleted(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	createDashboardUserFixture(t, testDB, 101, "u101", model.UserStatusActive, time.Date(2026, 7, 5, 16, 30, 0, 0, time.UTC)) // 2026-07-06 00:30 CST
	createDashboardUserFixture(t, testDB, 102, "u102", model.UserStatusActive, time.Date(2026, 7, 6, 16, 30, 0, 0, time.UTC)) // 2026-07-07 00:30 CST
	createDashboardUserFixture(t, testDB, 103, "u103", model.UserStatusDeleted, time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC))
	createDashboardUserFixture(t, testDB, 104, "u104", model.UserStatusActive, time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC))

	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	stats, err := loadDailyRegistrants(2, now)
	if err != nil {
		t.Fatalf("loadDailyRegistrants() error = %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 days, got %d", len(stats))
	}
	if stats[0] != (DailyRegistrationStat{Date: "2026-07-06", Count: 1}) {
		t.Fatalf("unexpected first day: %#v", stats[0])
	}
	if stats[1] != (DailyRegistrationStat{Date: "2026-07-07", Count: 1}) {
		t.Fatalf("unexpected second day: %#v", stats[1])
	}
}

func TestDashboardOverviewSkipsDeletedTotalUsers(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = nil
	}()

	createDashboardUserFixture(t, testDB, 201, "u201", model.UserStatusActive, time.Now().UTC())
	createDashboardUserFixture(t, testDB, 202, "u202", model.UserStatusBanned, time.Now().UTC())
	createDashboardUserFixture(t, testDB, 203, "u203", model.UserStatusDeleted, time.Now().UTC())

	overview, err := DashboardOverview()
	if err != nil {
		t.Fatalf("DashboardOverview() error = %v", err)
	}
	if overview.TotalUsers != 2 {
		t.Fatalf("expected total users to exclude deleted, got %d", overview.TotalUsers)
	}
}

func TestDashboardOnlineCountsDeduplicateRedisPresence(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previous
	}()

	ctx := context.Background()
	for key, value := range map[string]string{
		"im:ws:alive:301:web":             "1",
		"im:ws:alive:301:ios":             "1",
		"im:ws:alive:302:web":             "1",
		"im:ws:alive:not-a-user:web":      "1",
		"im:agent_api:route:401":          "node-a",
		"im:agent_api:route:401:9001":     "node-a",
		"im:agent_api:route:402:9002":     "node-b",
		"im:agent_api:route:not-an-agent": "node-c",
	} {
		if err := store.RDB.Set(ctx, key, value, time.Minute).Err(); err != nil {
			t.Fatalf("seed redis key %s: %v", key, err)
		}
	}

	users, err := countOnlineUsersFromRedis(ctx)
	if err != nil {
		t.Fatalf("countOnlineUsersFromRedis() error = %v", err)
	}
	if users != 2 {
		t.Fatalf("expected 2 online users, got %d", users)
	}

	agents, err := countOnlineAgentsFromRedis(ctx)
	if err != nil {
		t.Fatalf("countOnlineAgentsFromRedis() error = %v", err)
	}
	if agents != 2 {
		t.Fatalf("expected 2 online agents, got %d", agents)
	}
}

func createDashboardUserFixture(t *testing.T, db *testutil.TestDB, id int64, username string, status int16, createdAt time.Time) {
	t.Helper()
	user := model.User{
		ID:           id,
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: "hash",
		Nickname:     username,
		Status:       status,
		CreatedAt:    createdAt,
		UpdatedAt:    createdAt,
	}
	if err := db.DB.Create(&user).Error; err != nil {
		t.Fatalf("create user fixture: %v", err)
	}
}
