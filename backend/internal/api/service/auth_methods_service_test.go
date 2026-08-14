package service

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/stretchr/testify/assert"
	"gorm.io/datatypes"
)

func writeSmsSettings(t *testing.T, s systemsetting.SmsSettings) {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal sms settings: %v", err)
	}
	row := model.SystemSetting{Key: "sms", Value: datatypes.JSON(raw)}
	if err := store.DB.Where("key = ?", row.Key).Assign(row).FirstOrCreate(&row).Error; err != nil {
		t.Fatalf("write sms settings: %v", err)
	}
	systemsetting.InvalidateSmsSettingsCache()
}

func TestGetAuthMethods_CNReadsCNFields(t *testing.T) {
	store.DB = testutil.NewTestDB().DB
	writeSmsSettings(t, systemsetting.SmsSettings{
		PhoneRegisterEnabledCN:     true,
		PhoneRegisterEnabledGlobal: false,
		PhoneLoginEnabledCN:        true,
		PhoneLoginEnabledGlobal:    false,
	})

	v := GetAuthMethods("cn")
	assert.Equal(t, "cn", v.Region)
	assert.True(t, v.PhoneLoginEnabled, "CN login")
	assert.True(t, v.PhoneRegisterEnabled, "CN register")
}

func TestGetAuthMethods_GlobalReadsGlobalFields(t *testing.T) {
	store.DB = testutil.NewTestDB().DB
	writeSmsSettings(t, systemsetting.SmsSettings{
		PhoneRegisterEnabledCN:     false,
		PhoneRegisterEnabledGlobal: true,
		PhoneLoginEnabledCN:        false,
		PhoneLoginEnabledGlobal:    true,
	})

	v := GetAuthMethods("global")
	assert.Equal(t, "global", v.Region)
	assert.True(t, v.PhoneLoginEnabled, "Global login")
	assert.True(t, v.PhoneRegisterEnabled, "Global register")
}

func TestGetAuthMethods_UnknownRegionFallsBackToGlobal(t *testing.T) {
	store.DB = testutil.NewTestDB().DB
	writeSmsSettings(t, systemsetting.SmsSettings{
		PhoneLoginEnabledGlobal:    true,
		PhoneRegisterEnabledGlobal: false,
	})

	v := GetAuthMethods("xyz")
	assert.Equal(t, "global", v.Region)
	assert.True(t, v.PhoneLoginEnabled)
	assert.False(t, v.PhoneRegisterEnabled)
}

func TestGetAuthMethods_RegionParamCaseInsensitive(t *testing.T) {
	store.DB = testutil.NewTestDB().DB
	writeSmsSettings(t, systemsetting.SmsSettings{
		PhoneLoginEnabledCN: true,
	})

	v := GetAuthMethods("  CN  ")
	assert.Equal(t, "cn", v.Region)
	assert.True(t, v.PhoneLoginEnabled, "trim+lowercase normalize")
}

func TestGetAuthMethods_DisabledMeansFalse(t *testing.T) {
	store.DB = testutil.NewTestDB().DB
	writeSmsSettings(t, systemsetting.SmsSettings{
		PhoneRegisterEnabledCN:     false,
		PhoneRegisterEnabledGlobal: false,
		PhoneLoginEnabledCN:        false,
		PhoneLoginEnabledGlobal:    false,
	})

	cn := GetAuthMethods("cn")
	assert.False(t, cn.PhoneLoginEnabled)
	assert.False(t, cn.PhoneRegisterEnabled)

	gl := GetAuthMethods("global")
	assert.False(t, gl.PhoneLoginEnabled)
	assert.False(t, gl.PhoneRegisterEnabled)
}
