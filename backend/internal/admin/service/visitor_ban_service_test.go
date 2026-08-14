package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

func TestListVisitorBansAndUnban(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()
	config.C.JWT.Secret = "visitor-ban-admin-service-test-secret-0123456789"

	admin := createAdminFixture(t, testDB, 9101, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	owner := createUserFixture(t, testDB, 9102, "owner-user", "owner@example.com")
	now := time.Now().UTC()
	site := model.WidgetSite{
		ID:             9201,
		OwnerUserID:    owner.ID,
		SiteKey:        "site_key_1",
		SiteName:       "Demo Site",
		SiteSecretHash: "hash",
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	require.NoError(t, store.DB.Create(&site).Error)
	require.NoError(t, store.DB.Create(&model.WidgetSession{
		ID:               9301,
		SiteID:           site.ID,
		OwnerUserID:      owner.ID,
		VisitorID:        9401,
		VisitorKey:       "visitor-key-1",
		SessionID:        "widget-session-1",
		VisitorName:      "Alice",
		VisitorEmail:     "alice@example.com",
		LastPageURL:      "https://example.com/pricing",
		LastInitIPPrefix: "203.0.113.0/24",
		LastInitIP:       "203.0.113.9",
		Status:           model.WidgetSessionStatusBanned,
		CreatedAt:        now,
		UpdatedAt:        now,
		LastActiveAt:     now,
		LastInitAt:       now,
	}).Error)
	require.NoError(t, security.BanWidgetIP(owner.ID, "203.0.113.9", "session_ban", "widget-session-1", security.WidgetIPBanDefaultTTL))

	list, err := ListVisitorBans(VisitorBanListParams{Query: "Alice", Status: model.WidgetSessionStatusBanned, Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(1), list.Total)
	require.Len(t, list.Items, 1)
	require.Equal(t, "Demo Site", list.Items[0].SiteName)
	require.Equal(t, owner.ID, list.Items[0].OwnerUserID)
	require.True(t, list.Items[0].HasIPBan)

	require.NoError(t, UnbanWidgetVisitor(admin.ID, "widget-session-1", "127.0.0.1", "test-agent"))

	var session model.WidgetSession
	require.NoError(t, store.DB.Where("session_id = ?", "widget-session-1").First(&session).Error)
	require.Equal(t, model.WidgetSessionStatusClosed, session.Status)
	var ipBanCount int64
	require.NoError(t, store.DB.Model(&model.WidgetIPBan{}).Where("owner_user_id = ?", owner.ID).Count(&ipBanCount).Error)
	require.Equal(t, int64(0), ipBanCount)

	var log model.AdminOperationLog
	require.NoError(t, store.DB.Where("action = ? AND target_id = ?", "widget_visitor_unban", "widget-session-1").First(&log).Error)
	var detail map[string]any
	require.NoError(t, json.Unmarshal(log.Detail, &detail))
	require.Equal(t, "visitor-key-1", detail["visitor_key"])
	require.Equal(t, float64(1), detail["session_count"])
	require.Equal(t, float64(1), detail["deleted_ip_bans"])
}
