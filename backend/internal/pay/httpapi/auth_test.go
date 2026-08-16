package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupTokenRouter(token string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/v1/pay", RequireInternalToken(token))
	g.POST("/orders", func(c *gin.Context) { c.Status(http.StatusOK) })
	pub := r.Group("/v1/pay")
	pub.POST("/notify/:channel", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func TestRequireInternalToken(t *testing.T) {
	const token = "test-shared-secret"
	r := setupTokenRouter(token)

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"缺失密钥", "", http.StatusUnauthorized},
		{"错误密钥", "wrong", http.StatusUnauthorized},
		{"正确密钥", token, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/pay/orders", nil)
			if tc.header != "" {
				req.Header.Set(InternalTokenHeader, tc.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}

	// 回调路径保持公开：不带密钥也可达（鉴权由通道验签负责）。
	req := httptest.NewRequest(http.MethodPost, "/v1/pay/notify/mock", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("notify status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRequireInternalTokenEmptyAllowsAll(t *testing.T) {
	r := setupTokenRouter("") // 本地 mock 联调模式：空密钥放行
	req := httptest.NewRequest(http.MethodPost, "/v1/pay/orders", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
