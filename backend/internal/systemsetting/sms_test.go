package systemsetting

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
)

// writeSmsRowDirect 模拟"另一个 pod 保存了配置"：只落库，不动本进程的缓存和 hook。
func writeSmsRowDirect(t *testing.T, settings SmsSettings) {
	t.Helper()
	encrypted := settings
	if err := encryptSmsSecrets(&encrypted); err != nil {
		t.Fatalf("encryptSmsSecrets() error = %v", err)
	}
	raw, err := json.Marshal(encrypted)
	if err != nil {
		t.Fatalf("marshal settings error = %v", err)
	}
	row := model.SystemSetting{Key: smsSettingKey, Value: datatypes.JSON(raw)}
	if err := store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&row).Error; err != nil {
		t.Fatalf("write sms row error = %v", err)
	}
}

// 多副本下配置只在处理保存请求的那个 pod 上触发过 reload hook；其余 pod 必须在缓存过期
// 重读时发现内容变化并补触发，否则新模板号要等重启才生效（表现为间歇性 not_configured）。
func TestGetSmsSettingsTriggersReloadHookOnRefreshedChange(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	InvalidateSmsSettingsCache()
	defer InvalidateSmsSettingsCache()

	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	smsSettingsNow = func() time.Time { return now }
	defer func() { smsSettingsNow = time.Now }()

	var reloads []SmsSettings
	RegisterSmsReloadHook(func(s SmsSettings) { reloads = append(reloads, s) })
	defer RegisterSmsReloadHook(nil)

	v1 := DefaultSmsSettings()
	v1.Aliyun.SignName = "grix"
	writeSmsRowDirect(t, v1)

	if _, err := GetSmsSettings(); err != nil {
		t.Fatalf("GetSmsSettings() error = %v", err)
	}
	if len(reloads) != 0 {
		t.Fatalf("首次加载不该触发 reload，got %d", len(reloads))
	}

	// 另一个 pod 配好了通知模板号。
	v2 := v1
	v2.Aliyun.TemplateCodeNotify = "SMS_NOTIFY"
	writeSmsRowDirect(t, v2)

	// 缓存还没过期：本 pod 读到的仍是旧值，也不触发 reload。
	got, err := GetSmsSettings()
	if err != nil {
		t.Fatalf("GetSmsSettings() error = %v", err)
	}
	if got.Aliyun.TemplateCodeNotify != "" || len(reloads) != 0 {
		t.Fatalf("TTL 内不该重读或触发 reload，got template=%q reloads=%d", got.Aliyun.TemplateCodeNotify, len(reloads))
	}

	// 过 TTL：重读发现内容变了，补触发 reload hook。
	now = now.Add(smsSettingsCacheTTL + time.Second)
	got, err = GetSmsSettings()
	if err != nil {
		t.Fatalf("GetSmsSettings() error = %v", err)
	}
	if got.Aliyun.TemplateCodeNotify != "SMS_NOTIFY" {
		t.Fatalf("过期后应读到新配置，got %q", got.Aliyun.TemplateCodeNotify)
	}
	if len(reloads) != 1 {
		t.Fatalf("配置变化应触发 1 次 reload，got %d", len(reloads))
	}
	if reloads[0].Aliyun.TemplateCodeNotify != "SMS_NOTIFY" {
		t.Fatalf("reload 应带上新配置，got %q", reloads[0].Aliyun.TemplateCodeNotify)
	}

	// 内容没再变：再过一个 TTL 也不该重复触发，避免每分钟白重建一次 provider。
	now = now.Add(smsSettingsCacheTTL + time.Second)
	if _, err := GetSmsSettings(); err != nil {
		t.Fatalf("GetSmsSettings() error = %v", err)
	}
	if len(reloads) != 1 {
		t.Fatalf("内容未变不该重复触发 reload，got %d", len(reloads))
	}
}

// 本 pod 自己保存配置时仍走原来的即时 reload 路径。
func TestSaveSmsSettingsTriggersReloadHook(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	InvalidateSmsSettingsCache()
	defer InvalidateSmsSettingsCache()

	var reloads int
	RegisterSmsReloadHook(func(SmsSettings) { reloads++ })
	defer RegisterSmsReloadHook(nil)

	settings := DefaultSmsSettings()
	settings.Aliyun.TemplateCodeNotify = "SMS_NOTIFY"
	if err := SaveSmsSettings(settings, nil); err != nil {
		t.Fatalf("SaveSmsSettings() error = %v", err)
	}
	if reloads != 1 {
		t.Fatalf("保存后应触发 1 次 reload，got %d", reloads)
	}
}
