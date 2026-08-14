package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

func setupWidgetVisitorServiceTest(t *testing.T) *testutil.TestDB {
	t.Helper()
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB
	store.RDB = nil
	jwtpkg.Init("widget-visitor-service-test-secret", 3600, 86400)
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}
	return tdb
}

func seedWidgetSite(t *testing.T, site model.WidgetSite) {
	t.Helper()
	if err := store.DB.Create(&site).Error; err != nil {
		t.Fatalf("seed widget site error: %v", err)
	}
}

func TestWidgetVisitorInitCreatesSessionAndToken(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	seedWidgetSite(t, model.WidgetSite{
		ID:             9101,
		OwnerUserID:    801,
		SiteKey:        "wk_shop",
		SiteSecretHash: "hash",
		SiteName:       "Shop",
		AllowedOrigins: datatypes.JSON([]byte(`["https://shop.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	resp, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:      "wk_shop",
		VisitorKey:   "vk_123",
		VisitorName:  "Alice",
		VisitorEmail: "alice@example.com",
		PageURL:      "https://shop.example.com/p/1",
		Origin:       "https://shop.example.com",
		WSURL:        "wss://api.example.com/v1/widget/ws",
	})
	if err != nil {
		t.Fatalf("WidgetVisitorInit() error = %v", err)
	}
	if strings.TrimSpace(resp.SessionID) == "" {
		t.Fatal("session_id should not be empty")
	}
	if resp.VisitorID <= 0 {
		t.Fatalf("visitor_id should be positive, got %d", resp.VisitorID)
	}
	if resp.VisitorKey != "vk_123" {
		t.Fatalf("visitor_key = %q, want %q", resp.VisitorKey, "vk_123")
	}
	if resp.WSURL != "wss://api.example.com/v1/widget/ws" {
		t.Fatalf("ws_url = %q", resp.WSURL)
	}
	if strings.TrimSpace(resp.WidgetToken) == "" {
		t.Fatal("widget_token should not be empty")
	}

	claims, err := jwtpkg.ValidateWidgetAccessToken(resp.WidgetToken)
	if err != nil {
		t.Fatalf("ValidateWidgetAccessToken() error = %v", err)
	}
	if err := jwtpkg.ValidateWidgetSessionBinding(claims, 9101, resp.SessionID, resp.VisitorID); err != nil {
		t.Fatalf("ValidateWidgetSessionBinding() error = %v", err)
	}
	if !jwtpkg.WidgetScopeAllowed(claims, "chat:send") {
		t.Fatalf("default scope chat:send should be allowed, claims=%+v", claims)
	}

	var ws model.WidgetSession
	if err := store.DB.Where("session_id = ?", resp.SessionID).First(&ws).Error; err != nil {
		t.Fatalf("widget session not persisted: %v", err)
	}
	if ws.SiteID != 9101 || ws.OwnerUserID != 801 || ws.VisitorKey != "vk_123" {
		t.Fatalf("unexpected widget session persisted: %+v", ws)
	}

	var ownerMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", resp.SessionID, 801).First(&ownerMember).Error; err != nil {
		t.Fatalf("owner member not found: %v", err)
	}
	if !strings.HasPrefix(ownerMember.CustomTitle, "访客 ") {
		t.Fatalf("owner member custom title = %q, want prefix '访客 '", ownerMember.CustomTitle)
	}
}

func TestWidgetVisitorInitReusesActiveSession(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	seedWidgetSite(t, model.WidgetSite{
		ID:             9201,
		OwnerUserID:    1801,
		SiteKey:        "wk_reuse",
		SiteSecretHash: "hash",
		SiteName:       "Reuse",
		AllowedOrigins: datatypes.JSON([]byte(`["https://reuse.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	first, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:    "wk_reuse",
		VisitorKey: "vk_reuse_1",
		PageURL:    "https://reuse.example.com/a",
		Origin:     "https://reuse.example.com",
		WSURL:      "wss://api.example.com/v1/widget/ws",
	})
	if err != nil {
		t.Fatalf("first WidgetVisitorInit() error = %v", err)
	}

	second, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:    "wk_reuse",
		VisitorKey: "vk_reuse_1",
		PageURL:    "https://reuse.example.com/b",
		Origin:     "https://reuse.example.com",
		WSURL:      "wss://api.example.com/v1/widget/ws",
	})
	if err != nil {
		t.Fatalf("second WidgetVisitorInit() error = %v", err)
	}
	if first.SessionID != second.SessionID || first.VisitorID != second.VisitorID {
		t.Fatalf("expected session reuse, first=%+v second=%+v", first, second)
	}

	var count int64
	if err := store.DB.Model(&model.WidgetSession{}).
		Where("site_id = ? AND visitor_key = ?", 9201, "vk_reuse_1").
		Count(&count).Error; err != nil {
		t.Fatalf("count widget sessions error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected single active widget session, got %d", count)
	}
}

func TestWidgetVisitorInitRejectsOriginAndStatus(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	seedWidgetSite(t, model.WidgetSite{
		ID:             9301,
		OwnerUserID:    2801,
		SiteKey:        "wk_disabled",
		SiteSecretHash: "hash",
		SiteName:       "Disabled",
		AllowedOrigins: datatypes.JSON([]byte(`["https://disabled.example.com"]`)),
		Status:         model.WidgetSiteStatusDisabled,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	seedWidgetSite(t, model.WidgetSite{
		ID:             9302,
		OwnerUserID:    2802,
		SiteKey:        "wk_origin",
		SiteSecretHash: "hash",
		SiteName:       "Origin",
		AllowedOrigins: datatypes.JSON([]byte(`["https://allowed.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	_, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey: "wk_disabled", Origin: "https://disabled.example.com", WSURL: "wss://api.example.com/v1/widget/ws",
	})
	if !errors.Is(err, ErrWidgetSiteDisabled) {
		t.Fatalf("expected ErrWidgetSiteDisabled, got %v", err)
	}

	_, err = WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey: "wk_origin", Origin: "https://blocked.example.com", WSURL: "wss://api.example.com/v1/widget/ws",
	})
	if !errors.Is(err, ErrWidgetOriginNotAllowed) {
		t.Fatalf("expected ErrWidgetOriginNotAllowed, got %v", err)
	}
}

func TestWidgetVisitorInitRejectsBannedVisitor(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	now := time.Now().UTC()
	seedWidgetSite(t, model.WidgetSite{
		ID:             9401,
		OwnerUserID:    3801,
		SiteKey:        "wk_banned",
		SiteSecretHash: "hash",
		SiteName:       "Banned",
		AllowedOrigins: datatypes.JSON([]byte(`["https://banned.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err := store.DB.Create(&model.WidgetSession{
		ID:           9401001,
		SiteID:       9401,
		OwnerUserID:  3801,
		VisitorID:    4801,
		VisitorKey:   "vk_banned",
		SessionID:    "banned-session",
		Status:       model.WidgetSessionStatusBanned,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("seed banned widget session error: %v", err)
	}

	_, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey: "wk_banned", VisitorKey: "vk_banned", Origin: "https://banned.example.com", WSURL: "wss://api.example.com/v1/widget/ws",
	})
	if !errors.Is(err, ErrWidgetVisitorBanned) {
		t.Fatalf("expected ErrWidgetVisitorBanned, got %v", err)
	}
}

func TestWidgetVisitorInitDerivesStableVisitorKey(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = nil
	}()

	seedWidgetSite(t, model.WidgetSite{
		ID:             9501,
		OwnerUserID:    4801,
		SiteKey:        "wk_fp",
		SiteSecretHash: "hash_fp",
		SiteName:       "FP",
		AllowedOrigins: datatypes.JSON([]byte(`["https://fp.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	first, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:      "wk_fp",
		VisitorName:  "Alice",
		VisitorEmail: "alice@example.com",
		PageURL:      "https://fp.example.com/p/1",
		Origin:       "https://fp.example.com",
		WSURL:        "wss://api.example.com/v1/widget/ws",
		ClientIP:     "203.0.113.12",
		UserAgent:    "Mozilla/5.0 Test",
	})
	if err != nil {
		t.Fatalf("first WidgetVisitorInit() error = %v", err)
	}
	if !strings.HasPrefix(first.VisitorKey, "vkf_") {
		t.Fatalf("visitor_key=%q want vkf_*", first.VisitorKey)
	}

	second, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:      "wk_fp",
		VisitorName:  "Alice",
		VisitorEmail: "alice@example.com",
		PageURL:      "https://fp.example.com/p/2",
		Origin:       "https://fp.example.com",
		WSURL:        "wss://api.example.com/v1/widget/ws",
		ClientIP:     "203.0.113.99",
		UserAgent:    "Mozilla/5.0 Test",
	})
	if err != nil {
		t.Fatalf("second WidgetVisitorInit() error = %v", err)
	}
	if first.VisitorKey != second.VisitorKey {
		t.Fatalf("visitor_key should be stable, first=%q second=%q", first.VisitorKey, second.VisitorKey)
	}
}

func TestWidgetVisitorInitRateLimitByFingerprint(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = nil
	}()

	seedWidgetSite(t, model.WidgetSite{
		ID:             9601,
		OwnerUserID:    5801,
		SiteKey:        "wk_rl",
		SiteSecretHash: "hash_rl",
		SiteName:       "RL",
		AllowedOrigins: datatypes.JSON([]byte(`["https://rl.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	var gotErr error
	for i := 0; i < 13; i++ {
		_, gotErr = WidgetVisitorInit(WidgetVisitorInitInput{
			SiteKey:      "wk_rl",
			VisitorName:  "spam",
			VisitorEmail: "spam@example.com",
			PageURL:      "https://rl.example.com",
			Origin:       "https://rl.example.com",
			WSURL:        "wss://api.example.com/v1/widget/ws",
			ClientIP:     "198.51.100.20",
			UserAgent:    "spam-bot",
		})
		if i < 12 && gotErr != nil {
			t.Fatalf("request %d unexpected error: %v", i+1, gotErr)
		}
	}
	if !errors.Is(gotErr, ErrWidgetVisitorRateLimit) {
		t.Fatalf("expected ErrWidgetVisitorRateLimit, got %v", gotErr)
	}
}

func TestWidgetVisitorInitReusesClearsHistoryReset(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	seedWidgetSite(t, model.WidgetSite{
		ID:             9601,
		OwnerUserID:    5801,
		SiteKey:        "wk_reset",
		SiteSecretHash: "hash",
		SiteName:       "Reset",
		AllowedOrigins: datatypes.JSON([]byte(`["https://reset.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	// 第一次来访，创建 session
	first, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:    "wk_reset",
		VisitorKey: "vk_reset_1",
		PageURL:    "https://reset.example.com/a",
		Origin:     "https://reset.example.com",
		WSURL:      "wss://api.example.com/v1/widget/ws",
	})
	if err != nil {
		t.Fatalf("first init error: %v", err)
	}

	// 模拟 owner 删除会话：写入 session_history_resets
	now := time.Now().UTC()
	if err := store.DB.Create(&model.SessionHistoryReset{
		SessionID:     first.SessionID,
		UserID:        5801,
		DeletedBefore: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create history reset error: %v", err)
	}

	// 验证记录存在
	var countBefore int64
	store.DB.Model(&model.SessionHistoryReset{}).
		Where("session_id = ? AND user_id = ?", first.SessionID, 5801).
		Count(&countBefore)
	if countBefore != 1 {
		t.Fatalf("expected 1 history reset record, got %d", countBefore)
	}

	// 同一 visitor 再次来访，复用旧 session
	second, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:    "wk_reset",
		VisitorKey: "vk_reset_1",
		PageURL:    "https://reset.example.com/b",
		Origin:     "https://reset.example.com",
		WSURL:      "wss://api.example.com/v1/widget/ws",
	})
	if err != nil {
		t.Fatalf("second init error: %v", err)
	}
	if second.SessionID != first.SessionID {
		t.Fatalf("expected session reuse, got different session_id")
	}

	// 验证 session_history_resets 记录已被清除
	var countAfter int64
	store.DB.Model(&model.SessionHistoryReset{}).
		Where("session_id = ? AND user_id = ?", first.SessionID, 5801).
		Count(&countAfter)
	if countAfter != 0 {
		t.Fatalf("expected history reset cleared after reuse, got %d", countAfter)
	}
}

func TestWidgetVisitorInitReusesReestablishesDelegate(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()
	rdb := testutil.NewMockRedis()
	store.RDB = rdb
	defer func() {
		_ = rdb.Close()
		store.RDB = nil
	}()

	const ownerID = 6801
	const agentID = 6901

	seedWidgetSite(t, model.WidgetSite{
		ID:             9701,
		OwnerUserID:    ownerID,
		SiteKey:        "wk_delegate",
		SiteSecretHash: "hash",
		SiteName:       "Delegate",
		AllowedOrigins: datatypes.JSON([]byte(`["https://delegate.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	if err := store.DB.Create(&model.UserSetting{
		UserID:              ownerID,
		AutoDelegateAgentID: ptrInt64(agentID),
	}).Error; err != nil {
		t.Fatalf("seed user setting error: %v", err)
	}
	if err := store.DB.Create(&model.Agent{
		ID:      agentID,
		OwnerID: ownerID,
		Status:  1,
	}).Error; err != nil {
		t.Fatalf("seed agent error: %v", err)
	}

	first, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:    "wk_delegate",
		VisitorKey: "vk_delegate_1",
		PageURL:    "https://delegate.example.com/a",
		Origin:     "https://delegate.example.com",
		WSURL:      "wss://api.example.com/v1/widget/ws",
	})
	if err != nil {
		t.Fatalf("first init error: %v", err)
	}

	delegateKey := fmt.Sprintf("im:delegate:%s:%d", first.SessionID, ownerID)
	val, err := rdb.HGet(context.Background(), delegateKey, "agent_id").Result()
	if err != nil {
		t.Fatalf("delegate key not set after first init: %v", err)
	}
	if val != fmt.Sprintf("%d", agentID) {
		t.Fatalf("delegate agent_id = %q, want %d", val, agentID)
	}

	// 模拟连续回复超限后托管被清除
	rdb.Del(context.Background(), delegateKey)

	second, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:    "wk_delegate",
		VisitorKey: "vk_delegate_1",
		PageURL:    "https://delegate.example.com/b",
		Origin:     "https://delegate.example.com",
		WSURL:      "wss://api.example.com/v1/widget/ws",
	})
	if err != nil {
		t.Fatalf("second init error: %v", err)
	}
	if second.SessionID != first.SessionID {
		t.Fatalf("expected session reuse")
	}

	val, err = rdb.HGet(context.Background(), delegateKey, "agent_id").Result()
	if err != nil {
		t.Fatalf("delegate key not re-established after reuse: %v", err)
	}
	if val != fmt.Sprintf("%d", agentID) {
		t.Fatalf("delegate agent_id = %q after reuse, want %d", val, agentID)
	}
}

func TestWidgetVisitorInitResolvesLocaleAndWelcome(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	displayCfg := WidgetDisplayConfig{
		Welcome: map[string]string{
			"en_US": "Hi, how can I help?",
			"zh_CN": "您好，请问有什么可以帮您？",
		},
	}
	raw, err := json.Marshal(displayCfg)
	if err != nil {
		t.Fatalf("marshal display config error: %v", err)
	}
	seedWidgetSite(t, model.WidgetSite{
		ID:             9801,
		OwnerUserID:    7801,
		SiteKey:        "wk_locale",
		SiteSecretHash: "hash",
		SiteName:       "Locale",
		AllowedOrigins: datatypes.JSON([]byte(`["https://locale.example.com"]`)),
		DisplayConfig:  datatypes.JSON(raw),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	resp, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey: "wk_locale",
		Origin:  "https://locale.example.com",
		WSURL:   "wss://api.example.com/v1/widget/ws",
		Locale:  "zh-CN",
	})
	if err != nil {
		t.Fatalf("WidgetVisitorInit() error = %v", err)
	}
	if resp.ResolvedLocale != "zh_CN" {
		t.Fatalf("resolved_locale = %q, want zh_CN", resp.ResolvedLocale)
	}
	if resp.DisplayConfig.Welcome != "您好，请问有什么可以帮您？" {
		t.Fatalf("welcome = %q, want zh_CN copy", resp.DisplayConfig.Welcome)
	}

	var ws model.WidgetSession
	if err := store.DB.Where("session_id = ?", resp.SessionID).First(&ws).Error; err != nil {
		t.Fatalf("widget session not persisted: %v", err)
	}
	if ws.Locale != "zh_CN" {
		t.Fatalf("persisted session locale = %q, want zh_CN", ws.Locale)
	}

	// 未知语言归一化回退 en_US 文案
	respUnknown, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:    "wk_locale",
		VisitorKey: "vk_unknown_locale",
		Origin:     "https://locale.example.com",
		WSURL:      "wss://api.example.com/v1/widget/ws",
		Locale:     "xx-YY",
	})
	if err != nil {
		t.Fatalf("WidgetVisitorInit() (unknown locale) error = %v", err)
	}
	if respUnknown.ResolvedLocale != "en_US" {
		t.Fatalf("resolved_locale = %q, want en_US fallback", respUnknown.ResolvedLocale)
	}
	if respUnknown.DisplayConfig.Welcome != "Hi, how can I help?" {
		t.Fatalf("welcome = %q, want en_US fallback copy", respUnknown.DisplayConfig.Welcome)
	}
}

func TestWidgetVisitorConfigResolvesLocale(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	displayCfg := WidgetDisplayConfig{
		Welcome: map[string]string{"en_US": "Hello!", "ja_JP": "こんにちは！"},
	}
	raw, err := json.Marshal(displayCfg)
	if err != nil {
		t.Fatalf("marshal display config error: %v", err)
	}
	seedWidgetSite(t, model.WidgetSite{
		ID:             9802,
		OwnerUserID:    7802,
		SiteKey:        "wk_cfg_locale",
		SiteSecretHash: "hash",
		SiteName:       "CfgLocale",
		AllowedOrigins: datatypes.JSON([]byte(`["https://cfg.example.com"]`)),
		DisplayConfig:  datatypes.JSON(raw),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	cfg, err := WidgetVisitorConfig("wk_cfg_locale", "https://cfg.example.com", "ja-JP")
	if err != nil {
		t.Fatalf("WidgetVisitorConfig() error = %v", err)
	}
	if cfg.Welcome != "こんにちは！" {
		t.Fatalf("welcome = %q, want ja_JP copy", cfg.Welcome)
	}
}

func ptrInt64(v int64) *int64 { return &v }
