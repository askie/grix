package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRequestLanguage(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, RequestLanguage(c))
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-App-Locale", "en-US")
	r.ServeHTTP(w, req)

	if got := w.Body.String(); got != "en" {
		t.Fatalf("expected en, got %s", got)
	}
}

func TestRequestAppLanguage(t *testing.T) {
	cases := []struct {
		locale string
		want   string
	}{
		{"zh-CN", "zh"},
		{"zh_TW", "zh"},
		{"en-US", "en"},
		{"ja-JP", "ja"},
		{"ko", "ko"},
		{"de-DE", "de"},
		{"fr-FR", "fr"},
		{"es-ES", "es"},
		{"pt-BR", "pt"},
		{"ru-RU", "ru"},
		{"ar", "ar"},
		{"hi-IN", "hi"},
		{"it-IT", "en"},          // app 未支持的语言 fallback en
		{"ja-JP,en;q=0.9", "ja"}, // Accept-Language 风格取第一个
		{"", "zh"},               // 无头默认 zh，与 RequestLanguage 一致
	}
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, RequestAppLanguage(c))
	})
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		if tc.locale != "" {
			req.Header.Set("X-App-Locale", tc.locale)
		}
		r.ServeHTTP(w, req)
		if got := w.Body.String(); got != tc.want {
			t.Fatalf("locale=%q got=%s want=%s", tc.locale, got, tc.want)
		}
	}
}

func TestLocalizeMessage(t *testing.T) {
	msg := LocalizeMessage("用户不存在或密码错误", "en-US")
	if msg != "User does not exist or password is incorrect" {
		t.Fatalf("unexpected localized message: %s", msg)
	}

	zh := LocalizeMessage("Missing or invalid authorization header", "zh-CN")
	if zh != "缺少或无效的认证请求头" {
		t.Fatalf("unexpected zh fallback message: %s", zh)
	}
}
