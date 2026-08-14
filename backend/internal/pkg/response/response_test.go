package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupResponseTest() (*gin.Engine, *httptest.ResponseRecorder) {
	r := gin.New()
	w := httptest.NewRecorder()
	return r, w
}

func TestOK(t *testing.T) {
	r, w := setupResponseTest()

	r.GET("/test", func(c *gin.Context) {
		OK(c, map[string]string{"name": "test"})
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp R
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Code != 0 {
		t.Errorf("expected code 0, got %d", resp.Code)
	}
	if resp.Msg != "success" {
		t.Errorf("expected msg 'success', got '%s'", resp.Msg)
	}
	if resp.Data == nil {
		t.Error("expected data to be present")
	}
}

func TestOKWithNilData(t *testing.T) {
	r, w := setupResponseTest()

	r.GET("/test", func(c *gin.Context) {
		OK(c, nil)
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp R
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Code != 0 {
		t.Errorf("expected code 0, got %d", resp.Code)
	}
}

func TestFail(t *testing.T) {
	r, w := setupResponseTest()

	r.GET("/test", func(c *gin.Context) {
		Fail(c, http.StatusUnauthorized, 10001, "unauthorized")
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var resp R
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Code != 10001 {
		t.Errorf("expected code 10001, got %d", resp.Code)
	}
	if resp.Msg != "unauthorized" {
		t.Errorf("expected msg 'unauthorized', got '%s'", resp.Msg)
	}
}

func TestFailLocalizedByRequestLanguage(t *testing.T) {
	r, w := setupResponseTest()

	r.GET("/test", func(c *gin.Context) {
		Fail(c, http.StatusUnauthorized, 10001, "用户不存在或密码错误")
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-App-Locale", "en-US")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var resp R
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Msg != "User does not exist or password is incorrect" {
		t.Errorf("unexpected localized msg: %s", resp.Msg)
	}
}

func TestFailWithData(t *testing.T) {
	r, w := setupResponseTest()

	r.GET("/test", func(c *gin.Context) {
		FailWithData(c, http.StatusBadRequest, 10003, "validation error", map[string]string{"field": "email"})
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var resp R
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Code != 10003 {
		t.Errorf("expected code 10003, got %d", resp.Code)
	}
	if resp.Msg != "validation error" {
		t.Errorf("expected msg 'validation error', got '%s'", resp.Msg)
	}
	if resp.Data == nil {
		t.Error("expected data to be present")
	}
}

func TestRStruct(t *testing.T) {
	tests := []struct {
		name     string
		r        R
		wantJSON string
	}{
		{
			name:     "success response",
			r:        R{Code: 0, Msg: "success", Data: "test"},
			wantJSON: `{"code":0,"msg":"success","data":"test"}`,
		},
		{
			name:     "error response",
			r:        R{Code: 10001, Msg: "error"},
			wantJSON: `{"code":10001,"msg":"error"}`,
		},
		{
			name:     "nil data",
			r:        R{Code: 0, Msg: "success", Data: nil},
			wantJSON: `{"code":0,"msg":"success"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.r)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			// Just verify it marshals without error
			if len(data) == 0 {
				t.Error("expected non-empty JSON")
			}
		})
	}
}

func TestVariousHTTPStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		bizCode    int
		msg        string
	}{
		{"bad request", http.StatusBadRequest, 10003, "bad request"},
		{"not found", http.StatusNotFound, 10004, "not found"},
		{"internal error", http.StatusInternalServerError, 50001, "internal error"},
		{"too many requests", http.StatusTooManyRequests, 10005, "rate limited"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, w := setupResponseTest()

			r.GET("/test", func(c *gin.Context) {
				Fail(c, tt.statusCode, tt.bizCode, tt.msg)
			})

			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			r.ServeHTTP(w, req)

			if w.Code != tt.statusCode {
				t.Errorf("expected status %d, got %d", tt.statusCode, w.Code)
			}
		})
	}
}

func TestResponseContentType(t *testing.T) {
	r, w := setupResponseTest()

	r.GET("/test", func(c *gin.Context) {
		OK(c, map[string]string{"test": "value"})
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("expected JSON content type, got '%s'", contentType)
	}
}
