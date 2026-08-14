package handler

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/gin-gonic/gin"
)

type ossDeleteSpyServer struct {
	server  *httptest.Server
	mu      sync.Mutex
	methods []string
	paths   []string
}

func newOSSDeleteSpyServer(t *testing.T) *ossDeleteSpyServer {
	t.Helper()

	spy := &ossDeleteSpyServer{}
	spy.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spy.mu.Lock()
		spy.methods = append(spy.methods, r.Method)
		spy.paths = append(spy.paths, r.URL.Path)
		spy.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(spy.server.Close)

	return spy
}

func (s *ossDeleteSpyServer) localhostEndpoint(t *testing.T) string {
	t.Helper()

	_, port, err := net.SplitHostPort(s.server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	return "localhost:" + port
}

func (s *ossDeleteSpyServer) requestSnapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	methods := append([]string(nil), s.methods...)
	paths := append([]string(nil), s.paths...)
	return methods, paths
}

func setupOSSDeleteHandlerTest(t *testing.T, userID int64) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Next()
	})
	r.POST("/oss/delete", OSSDeleteObjects)
	return r
}

func configureOSSDeleteHandlerTest(t *testing.T, endpoint string) {
	t.Helper()

	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
		_ = service.InitOSS()
	})

	sharedCfg := config.OSSConfig{
		Endpoint:  endpoint,
		AccessKey: "test-ak",
		SecretKey: "test-sk",
		Bucket:    "test-bucket",
		Region:    "us-east-1",
	}
	config.C.OSS.Media = sharedCfg
	config.C.OSS.Media.StorageDir = "aibot/media"
	config.C.OSS.Avatar = sharedCfg
	config.C.OSS.Avatar.Bucket = "test-avatar"
	config.C.OSS.Report = sharedCfg
	config.C.OSS.Report.Bucket = "test-report"

	if err := service.InitOSS(); err != nil {
		t.Fatalf("init oss: %v", err)
	}
}

func TestOSSDeleteObjectsSuccess(t *testing.T) {
	const userID = int64(42)

	spy := newOSSDeleteSpyServer(t)
	configureOSSDeleteHandlerTest(t, spy.localhostEndpoint(t))
	router := setupOSSDeleteHandlerTest(t, userID)

	body := []byte(`{"object_keys":["aibot/media/user/42/1_demo.png"," aibot/media/user/42/2_demo.mp4 "]}`)
	req, _ := http.NewRequest(http.MethodPost, "/oss/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", recorder.Code, recorder.Body.String())
	}

	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}

	methods, paths := spy.requestSnapshot()
	if len(methods) != 2 {
		t.Fatalf("expected 2 delete requests, got %d", len(methods))
	}
	for _, method := range methods {
		if method != http.MethodDelete {
			t.Fatalf("expected delete method, got %s", method)
		}
	}
	if !strings.Contains(paths[0], "user/42/1_demo.png") {
		t.Fatalf("expected first delete path to contain first object key, got %q", paths[0])
	}
	if !strings.Contains(paths[1], "user/42/2_demo.mp4") {
		t.Fatalf("expected second delete path to contain second object key, got %q", paths[1])
	}
}

func TestOSSDeleteObjectsForbiddenObjectKey(t *testing.T) {
	const userID = int64(42)

	spy := newOSSDeleteSpyServer(t)
	configureOSSDeleteHandlerTest(t, spy.localhostEndpoint(t))
	router := setupOSSDeleteHandlerTest(t, userID)

	body := []byte(`{"object_keys":["aibot/media/user/99/1_demo.png"]}`)
	req, _ := http.NewRequest(http.MethodPost, "/oss/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeAPIErrorCode(t, recorder.Body.Bytes()); code != 4003 {
		t.Fatalf("expected code 4003, got %d", code)
	}

	methods, _ := spy.requestSnapshot()
	if len(methods) != 0 {
		t.Fatalf("expected no delete requests for forbidden object key, got %d", len(methods))
	}
}

func TestOSSDeleteObjectsInvalidRequest(t *testing.T) {
	router := setupOSSDeleteHandlerTest(t, 42)

	req, _ := http.NewRequest(http.MethodPost, "/oss/delete", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if code := decodeAPIErrorCode(t, recorder.Body.Bytes()); code != 10003 {
		t.Fatalf("expected code 10003, got %d", code)
	}
}
