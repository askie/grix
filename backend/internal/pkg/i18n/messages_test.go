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

func TestLocalizeMessage_SkillErrors(t *testing.T) {
	// 与 errcode.go 27001-27009 一一对应，防止 errcode 改文案后翻译表漂移。
	cases := []struct{ zh, en string }{
		{"技能名称不能为空", "Skill name is required"},
		{"技能名称过长（上限 100 字符）", "Skill name is too long (max 100 characters)"},
		{"同名技能已存在", "A skill with the same name already exists"},
		{"技能不存在", "Skill not found"},
		{"技能内容不能为空", "Skill content is required"},
		{"技能内容超过上限", "Skill content exceeds the size limit"},
		{"系统内置技能只读", "Built-in skills are read-only"},
		{"技能名称含非法字符（不能包含路径分隔符、..、前导点或控制字符）", "Skill name contains invalid characters (path separators, .., leading dots, or control characters are not allowed)"},
		{"grix- 前缀技能为平台保留，不可上传", "Skills with the grix- prefix are reserved by the platform and cannot be uploaded"},
	}
	for _, tc := range cases {
		if got := LocalizeMessage(tc.zh, "en-US"); got != tc.en {
			t.Errorf("LocalizeMessage(%q, en)=%q want %q", tc.zh, got, tc.en)
		}
		if got := LocalizeMessage(tc.en, "zh-CN"); got != tc.zh {
			t.Errorf("LocalizeMessage(%q, zh)=%q want %q", tc.en, got, tc.zh)
		}
	}
}
