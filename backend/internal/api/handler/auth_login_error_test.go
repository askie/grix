package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/security"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 登录错误必须语义化映射：禁用 403 / 锁定 429 / 不可用 503 / 凭证错误 401。
// 客户端依赖 401 判定"凭证被拒"（跨区提示只在 401 出现），非凭证错误不得落 401。
func TestFailByLoginErrorMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantMsg    string
		hideErr    bool
	}{
		{"disabled user -> 403", security.ErrUserDisabled, http.StatusForbidden, `"code":10001`, security.ErrUserDisabled.Error(), false},
		{"locked account -> 429", &security.LoginLockedError{Remaining: 30 * time.Minute}, http.StatusTooManyRequests, `"code":10001`, security.LoginLockedMessage(30 * time.Minute), false},
		{"infra unavailable -> 503", service.ErrAuthServiceUnavailable, http.StatusServiceUnavailable, `"code":50001`, "服务端内部异常", true},
		{"invalid credentials -> 401", errors.New("用户不存在或密码错误"), http.StatusUnauthorized, `"code":10001`, "用户不存在或密码错误", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
			failByLoginError(c, tc.err)
			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Contains(t, w.Body.String(), tc.wantCode)
			assert.Contains(t, w.Body.String(), tc.wantMsg)
			if tc.hideErr {
				assert.NotContains(t, w.Body.String(), tc.err.Error())
			}
		})
	}
}

// 锁定错误的文案必须与原 LoginLockedMessage 完全一致，前端提示不受重构影响。
func TestLoginLockedErrorMessageUnchanged(t *testing.T) {
	e := &security.LoginLockedError{Remaining: 90 * time.Minute}
	assert.Equal(t, security.LoginLockedMessage(90*time.Minute), e.Error())
}
