package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/stretchr/testify/require"
)

func loginAdminForPushSettings(t *testing.T, r http.Handler, username, password string) string {
	t.Helper()
	seedAdmin(t, username, password)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data.Token)
	return resp.Data.Token
}

func putPushSettings(t *testing.T, r http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/api/settings/push", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	return w
}

// 回归测试：旧版塘主表单只提交 4 个老通道字段。若 PUT 用零值结构体反序列化，
// 未携带的 5 个厂商通道会被静默写成 false，一次保存就把华为/小米等通道全关掉。
// 请求体缺失的字段必须保持默认开启。
func TestApiUpdatePushChannelSettings_LegacyFormKeepsVendorChannelsEnabled(t *testing.T) {
	r, cleanup := setupAdminLoginRouter(t)
	defer cleanup()
	systemsetting.InvalidatePushChannelSettingsCache()

	token := loginAdminForPushSettings(t, r, "pushsettingsadmin", "PushSettPass123A")

	// 旧表单：只带 4 个老字段，其中关掉 FCM / WebPush（国内区常见配置）。
	body := `{"ios_apn_enabled":true,"android_fcm_enabled":false,"web_push_enabled":false,"jpush_enabled":true}`
	w := putPushSettings(t, r, token, body)
	require.Equal(t, http.StatusOK, w.Code)

	saved, err := systemsetting.GetPushChannelSettings()
	require.NoError(t, err)

	// 显式提交的字段按提交值生效。
	require.True(t, saved.IOSAPNsEnabled)
	require.False(t, saved.AndroidFCMEnabled)
	require.False(t, saved.WebPushEnabled)
	require.True(t, saved.JPushEnabled)

	// 未提交的厂商通道必须保持开启，不能被零值静默关闭。
	require.True(t, saved.AndroidHuaweiEnabled, "huawei channel must not be silently disabled")
	require.True(t, saved.AndroidHonorEnabled, "honor channel must not be silently disabled")
	require.True(t, saved.AndroidXiaomiEnabled, "xiaomi channel must not be silently disabled")
	require.True(t, saved.AndroidOppoEnabled, "oppo channel must not be silently disabled")
	require.True(t, saved.AndroidVivoEnabled, "vivo channel must not be silently disabled")
}

// 存量打底的另一面：管理员先前手动关掉的通道，不能被一次旧表单提交静默重新打开。
// 用默认值（全开）打底会犯这个反向错误。
func TestApiUpdatePushChannelSettings_LegacyFormKeepsPreviouslyDisabledChannelOff(t *testing.T) {
	r, cleanup := setupAdminLoginRouter(t)
	defer cleanup()
	systemsetting.InvalidatePushChannelSettingsCache()

	token := loginAdminForPushSettings(t, r, "pushsettingsadmin3", "PushSettPass123A")

	// 管理员先临时停掉华为通道。
	disabled := systemsetting.DefaultPushChannelSettings()
	disabled.AndroidHuaweiEnabled = false
	require.NoError(t, systemsetting.SavePushChannelSettings(disabled, nil))

	// 之后有人用旧表单保存（请求体不含厂商字段）。
	body := `{"ios_apn_enabled":true,"android_fcm_enabled":true,"web_push_enabled":true,"jpush_enabled":true}`
	w := putPushSettings(t, r, token, body)
	require.Equal(t, http.StatusOK, w.Code)

	saved, err := systemsetting.GetPushChannelSettings()
	require.NoError(t, err)

	require.False(t, saved.AndroidHuaweiEnabled, "previously disabled channel must not be silently re-enabled")
	require.True(t, saved.AndroidXiaomiEnabled, "untouched channel keeps its stored value")
}

// 显式提交 false 时必须真的关掉——存量打底不能吃掉用户的关闭意图。
func TestApiUpdatePushChannelSettings_ExplicitDisableIsHonored(t *testing.T) {
	r, cleanup := setupAdminLoginRouter(t)
	defer cleanup()
	systemsetting.InvalidatePushChannelSettingsCache()

	token := loginAdminForPushSettings(t, r, "pushsettingsadmin2", "PushSettPass123A")

	body := `{"ios_apn_enabled":true,"android_fcm_enabled":true,"web_push_enabled":true,"jpush_enabled":true,` +
		`"android_huawei_enabled":false,"android_honor_enabled":true,"android_xiaomi_enabled":false,` +
		`"android_oppo_enabled":true,"android_vivo_enabled":true}`
	w := putPushSettings(t, r, token, body)
	require.Equal(t, http.StatusOK, w.Code)

	saved, err := systemsetting.GetPushChannelSettings()
	require.NoError(t, err)

	require.False(t, saved.AndroidHuaweiEnabled)
	require.False(t, saved.AndroidXiaomiEnabled)
	require.True(t, saved.AndroidHonorEnabled)
	require.True(t, saved.AndroidOppoEnabled)
	require.True(t, saved.AndroidVivoEnabled)
}
