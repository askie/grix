package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func controlsRequest(r http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/admin/api/"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}

func controlsValue(t *testing.T, r http.Handler, token string) bool {
	t.Helper()
	w := controlsRequest(r, token, "GET", "settings/system-controls", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var result struct {
		Data struct {
			Items []struct {
				Key       string
				ValueType string `json:"value_type"`
				Value     bool
			}
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Data.Items, 1)
	require.Equal(t, "registration_enabled", result.Data.Items[0].Key)
	require.Equal(t, "boolean", result.Data.Items[0].ValueType)
	return result.Data.Items[0].Value
}

func TestSystemControlsAPI(t *testing.T) {
	r, cleanup := setupAdminLoginRouter(t)
	defer cleanup()
	token := loginAdminForPushSettings(t, r, "controlsadmin", "ControlsPass123A")
	require.False(t, controlsValue(t, r, token), "missing canonical gate defaults closed")
	for _, method := range []string{"GET", "PUT"} {
		path := "settings/system-controls"
		if method == "PUT" {
			path += "/registration_enabled"
		}
		require.Equal(t, 401, controlsRequest(r, "", method, path, `{"value":true}`).Code)
	}
	for _, body := range []string{`{}`, `{"value":null}`, `{"value":"true"}`, `{"value":1}`, `{"value":[]}`, `{"value":{}}`, `{"value":true,"extra":1}`, `{"value":false,"value":true}`, `{"value":true} {}`, `null`} {
		require.Equal(t, 400, controlsRequest(r, token, "PUT", "settings/system-controls/registration_enabled", body).Code, body)
	}
	require.Equal(t, 400, controlsRequest(r, token, "PUT", "settings/system-controls/unknown", `{"value":true}`).Code)
	for _, value := range []bool{true, false, false, true} {
		body, _ := json.Marshal(map[string]any{"value": value})
		w := controlsRequest(r, token, "PUT", "settings/system-controls/registration_enabled", string(body))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, value, controlsValue(t, r, token))
	}
	var count int64
	require.NoError(t, store.DB.Model(&model.FeatureGate{}).Where("key = ?", featuregate.FeatureRegistration).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var logs []model.AdminOperationLog
	require.NoError(t, store.DB.Where("action = ?", "system_control_update").Order("id").Find(&logs).Error)
	require.Len(t, logs, 4)
	for i, log := range logs {
		require.EqualValues(t, 4101, log.AdminID)
		require.Equal(t, "registration_enabled", log.TargetID)
		var detail map[string]any
		require.NoError(t, json.Unmarshal(log.Detail, &detail))
		require.Equal(t, "registration_enabled", detail["key"])
		require.Equal(t, []bool{false, true, false, false}[i], detail["before"])
		require.Equal(t, []bool{true, false, false, true}[i], detail["after"])
	}
	// The legacy UI writes exactly the same canonical row.
	for _, status := range []string{"disabled", "enabled"} {
		body := `{"key":"auth_register","status":"` + status + `"}`
		w := controlsRequest(r, token, "POST", "feature-gates/status", body)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, status == "enabled", controlsValue(t, r, token))
	}
	// A regular administrator needs settings permission on both endpoints.
	require.NoError(t, store.DB.AutoMigrate(&model.AdminRole{}))
	role := model.AdminRole{ID: 9001, Name: "controls-reader", Permissions: `[]`}
	require.NoError(t, store.DB.Create(&role).Error)
	require.NoError(t, store.DB.Model(&model.AdminUser{}).Where("id = ?", 4101).Updates(map[string]any{"role": model.AdminRoleCustom, "role_id": role.ID}).Error)
	for _, method := range []string{"GET", "PUT"} {
		path := "settings/system-controls"
		if method == "PUT" {
			path += "/registration_enabled"
		}
		require.Equal(t, 403, controlsRequest(r, token, method, path, `{"value":false}`).Code)
	}
	require.NoError(t, store.DB.Model(&role).Update("permissions", `["settings"]`).Error)
	require.True(t, controlsValue(t, r, token))
	require.Equal(t, 200, controlsRequest(r, token, "PUT", "settings/system-controls/registration_enabled", `{"value":false}`).Code)
}

func TestSystemControlsAuditRollback(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(map[bool]string{true: "first-save", false: "existing-row"}[missing], func(t *testing.T) {
			r, cleanup := setupAdminLoginRouter(t)
			defer cleanup()
			token := loginAdminForPushSettings(t, r, "rollbackadmin", "ControlsPass123A")
			require.NoError(t, store.DB.Create(&model.FeatureGate{Key: "other_gate", Status: "enabled"}).Error)
			if !missing {
				require.NoError(t, store.DB.Create(&model.FeatureGate{Key: featuregate.FeatureRegistration, DisplayName: "legacy label", Status: "enabled"}).Error)
			}
			require.Equal(t, !missing, controlsValue(t, r, token), "GET must not reset stored values")
			require.NoError(t, store.DB.Exec(`CREATE TRIGGER fail_control_audit BEFORE INSERT ON admin_operation_logs WHEN NEW.action = 'system_control_update' BEGIN SELECT RAISE(ABORT, 'audit failure'); END`).Error)
			require.Equal(t, 500, controlsRequest(r, token, "PUT", "settings/system-controls/registration_enabled", `{"value":false}`).Code)
			require.Equal(t, !missing, controlsValue(t, r, token))
			var count int64
			require.NoError(t, store.DB.Model(&model.FeatureGate{}).Where("key = ?", featuregate.FeatureRegistration).Count(&count).Error)
			require.EqualValues(t, map[bool]int{true: 0, false: 1}[missing], count)
			gate, err := featuregate.GetGate("other_gate")
			require.NoError(t, err)
			require.Equal(t, "enabled", gate.Status)
		})
	}
}

func TestSystemControlsStoredValuesAndReadFailure(t *testing.T) {
	r, cleanup := setupAdminLoginRouter(t)
	defer cleanup()
	token := loginAdminForPushSettings(t, r, "storedadmin", "ControlsPass123A")
	gate := model.FeatureGate{Key: featuregate.FeatureRegistration, DisplayName: "legacy registration", Status: "disabled"}
	require.NoError(t, store.DB.Create(&gate).Error)
	require.False(t, controlsValue(t, r, token))
	stored, err := featuregate.GetGate(gate.Key)
	require.NoError(t, err)
	require.Equal(t, gate.Status, stored.Status)
	require.Equal(t, gate.DisplayName, stored.DisplayName)
	require.Equal(t, 200, controlsRequest(r, token, "PUT", "settings/system-controls/registration_enabled", `{"value":true}`).Code)
	stored, err = featuregate.GetGate(gate.Key)
	require.NoError(t, err)
	require.Equal(t, gate.DisplayName, stored.DisplayName)
	require.NoError(t, store.DB.Callback().Query().Before("gorm:query").Register("control_read_error", func(db *gorm.DB) {
		if db.Statement.Table == "feature_gates" {
			db.AddError(fmt.Errorf("gate unavailable"))
		}
	}))
	defer store.DB.Callback().Query().Remove("control_read_error")
	require.Equal(t, 500, controlsRequest(r, token, "GET", "settings/system-controls", "").Code)
	require.Equal(t, 500, controlsRequest(r, token, "PUT", "settings/system-controls/registration_enabled", `{"value":false}`).Code)
	require.NoError(t, store.DB.Callback().Query().Remove("control_read_error"))
	require.True(t, controlsValue(t, r, token))
}
