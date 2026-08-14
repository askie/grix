package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/api"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// e2eContext holds testing resources
type e2eContext struct {
	router *gin.Engine
	db     *testutil.TestDB
}

// setupE2E initializes the entire E2E test context
func setupE2E(t *testing.T) *e2eContext {
	t.Helper()

	// 1. Initialize Test DB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB

	// In test mode we may also want to mock Redis if functions rely on it
	store.RDB = testutil.NewMockRedis()

	// 注册受 auth_register 公共功能门控制，生产默认关闭、需管理员开启。
	// 测试用例（loginHelper 等）依赖注册可用，故在每个 E2E 上下文显式开启该门。
	// SaveGate 内部会失效 featuregate 缓存，避免跨用例的全局缓存污染。
	if err := featuregate.SaveGate("auth_register", "注册", model.FeatureStatusEnabled); err != nil {
		t.Fatalf("failed to enable auth_register gate: %v", err)
	}

	// 2. Initialize JWT settings
	jwt.Init("e2e-secret-key-for-testing", 3600, 86400)

	// 3. Initialize Snowflake for generation
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("failed to init snowflake: %v", err)
	}

	// 4. Init logger before router setup so route registration logs are safe.
	logger.Init()

	// 5. Initialize required feature gates for auth
	initFeatureGatesForE2E(testDB.DB)

	// 6. Initialize required system settings
	initSystemSettingsForE2E(testDB.DB)

	// 7. Create the real API router
	r := api.SetupRouter()

	return &e2eContext{
		router: r,
		db:     testDB,
	}
}

// initFeatureGatesForE2E initializes required feature gates in test database
func initFeatureGatesForE2E(db *gorm.DB) {
	// Enable auth_register feature for E2E tests
	authRegisterGate := &model.FeatureGate{
		Key:         "auth_register",
		DisplayName: "用户注册",
		Status:      model.FeatureStatusEnabled,
	}
	_ = db.FirstOrCreate(authRegisterGate, "key = ?", "auth_register")
}

// initSystemSettingsForE2E initializes required system settings in test database
func initSystemSettingsForE2E(db *gorm.DB) {
	// Create minimal auth settings
	authSettings := map[string]interface{}{
		"auto_add_customer_user_id": 0,
	}
	raw, _ := json.Marshal(authSettings)

	authSetting := &model.SystemSetting{
		Key:   "auth",
		Value: raw,
	}
	_ = db.FirstOrCreate(authSetting, "key = ?", "auth")
}

func startTestWSServer(t *testing.T, nodePrefix string) (*ws.Server, int) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test ws port failed: %v", err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		t.Fatalf("unexpected listen addr type: %T", ln.Addr())
	}
	port := addr.Port

	server := ws.NewServer(port, nextNodeID(nodePrefix), "", "/v1/agent-api/ws", 30, "", false)
	go func() {
		_ = server.Serve(ln)
	}()
	waitForWSServerReady(t, port)
	return server, port
}

// waitForWSServerReady waits until the HTTP handler is serving /health.
// Bare TCP dial is not enough: the test holds the listener before Serve starts,
// so TCP can succeed while HTTP Accept is not ready yet.
func waitForWSServerReady(t *testing.T, port int) {
	t.Helper()

	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("health status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("ws server not ready on %s: %v", url, lastErr)
}

func nextNodeID(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "node-test"
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// cleanup releases testing resources
func (ctx *e2eContext) cleanup() {
	ctx.db.Close()
}

// doReq is a helper to execute an HTTP request against the E2E router
func (ctx *e2eContext) doReq(t *testing.T, method, path string, token string, reqBody interface{}) *httptest.ResponseRecorder {
	t.Helper()

	var bodyReader *bytes.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}

	req, err := http.NewRequest(method, path, bodyReader)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	w := httptest.NewRecorder()
	ctx.router.ServeHTTP(w, req)
	return w
}

// parseResp is a helper to parse JSON response body
func parseResp(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var res map[string]interface{}
	dec := json.NewDecoder(w.Body)
	dec.UseNumber()
	if err := dec.Decode(&res); err != nil {
		t.Fatalf("failed to unmarshal response: %v, body: %s", err, w.Body.String())
	}
	return res
}

// loginHelper registers or logs in an user and returns their access token and details.
func (ctx *e2eContext) loginHelper(t *testing.T, email, password string, deviceID ...string) (string, int64) {
	t.Helper()
	account := email
	if !strings.Contains(account, "@") {
		account = fmt.Sprintf("%s@example.com", account)
	}
	loginDeviceID := "e2e-device-" + strings.ReplaceAll(strings.ReplaceAll(account, "@", "-"), ".", "-")
	if len(deviceID) > 0 && strings.TrimSpace(deviceID[0]) != "" {
		loginDeviceID = strings.TrimSpace(deviceID[0])
	}

	// Try Register First
	const emailCode = "654321"
	key := fmt.Sprintf("auth:email_code:%s:%s", "register", account)
	if err := store.RDB.Set(context.Background(), key, emailCode, 5*time.Minute).Err(); err != nil {
		t.Fatalf("seed email code failed: %v", err)
	}

	reqPayload := map[string]interface{}{
		"email":      account,
		"password":   password,
		"email_code": emailCode,
		"device_id":  loginDeviceID,
		"platform":   "test",
	}

	w := ctx.doReq(t, "POST", "/v1/auth/register", "", reqPayload)
	if w.Code != http.StatusOK {
		// If register fails (e.g., email exists), try Login
		loginPayload := map[string]interface{}{
			"account":   account,
			"password":  password,
			"device_id": loginDeviceID,
			"platform":  "test",
		}
		w = ctx.doReq(t, "POST", "/v1/auth/login", "", loginPayload)
		if w.Code != http.StatusOK {
			t.Fatalf("loginHelper failed: expected 200, got %d, body: %s", w.Code, w.Body.String())
		}
	}

	res := parseResp(t, w)

	data, ok := res["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("loginHelper failed: missing or invalid 'data' field in response")
	}

	token, _ := data["access_token"].(string)
	userMap, _ := data["user"].(map[string]interface{})
	n, err := parseID(userMap["id"])
	if err != nil {
		t.Fatalf("loginHelper failed: parse user id error: %v", err)
	}

	return token, n
}

func parseID(v interface{}) (int64, error) {
	switch x := v.(type) {
	case string:
		return strconv.ParseInt(x, 10, 64)
	case json.Number:
		return x.Int64()
	case float64:
		return int64(x), nil
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	default:
		return 0, strconv.ErrSyntax
	}
}
