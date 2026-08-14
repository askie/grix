package errcode

import (
	"net/http"
	"testing"
)

func TestErrCodeStruct(t *testing.T) {
	code := ErrCode{
		HTTPStatus: http.StatusBadRequest,
		BizCode:    10001,
		Msg:        "test error",
	}

	if code.HTTPStatus != http.StatusBadRequest {
		t.Errorf("expected HTTPStatus %d, got %d", http.StatusBadRequest, code.HTTPStatus)
	}
	if code.BizCode != 10001 {
		t.Errorf("expected BizCode 10001, got %d", code.BizCode)
	}
	if code.Msg != "test error" {
		t.Errorf("expected Msg 'test error', got '%s'", code.Msg)
	}
}

func TestPredefinedErrorCodes(t *testing.T) {
	tests := []struct {
		name       string
		errCode    ErrCode
		wantStatus int
		wantBiz    int
		wantMsg    string
	}{
		{"Success", Success, http.StatusOK, 0, "success"},
		{"ErrUnauthorized", ErrUnauthorized, http.StatusUnauthorized, 10001, "未授权或 Access Token 已过期"},
		{"ErrRefreshExpired", ErrRefreshExpired, http.StatusUnauthorized, 10002, "Refresh Token 已过期或被吊销"},
		{"ErrBadRequest", ErrBadRequest, http.StatusBadRequest, 10003, "请求参数验证错误"},
		{"ErrNotFound", ErrNotFound, http.StatusNotFound, 10004, "资源不存在"},
		{"ErrRateLimited", ErrRateLimited, http.StatusTooManyRequests, 10005, "请求过于频繁，请稍后再试"},
		{"ErrInternal", ErrInternal, http.StatusInternalServerError, 50001, "服务端内部异常"},
		{"ErrContentViolation", ErrContentViolation, http.StatusBadRequest, 40001, "内容违规"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.errCode.HTTPStatus != tt.wantStatus {
				t.Errorf("HTTPStatus: got %d, want %d", tt.errCode.HTTPStatus, tt.wantStatus)
			}
			if tt.errCode.BizCode != tt.wantBiz {
				t.Errorf("BizCode: got %d, want %d", tt.errCode.BizCode, tt.wantBiz)
			}
			if tt.errCode.Msg != tt.wantMsg {
				t.Errorf("Msg: got '%s', want '%s'", tt.errCode.Msg, tt.wantMsg)
			}
		})
	}
}

func TestErrorCodeRanges(t *testing.T) {
	// Verify error code ranges are logical
	codes := []ErrCode{Success, ErrUnauthorized, ErrRefreshExpired, ErrBadRequest, ErrNotFound, ErrRateLimited, ErrInternal, ErrContentViolation}

	for _, code := range codes {
		// HTTP status should be valid (100-599)
		if code.HTTPStatus < 100 || code.HTTPStatus > 599 {
			t.Errorf("invalid HTTP status code: %d", code.HTTPStatus)
		}

		// Business code should be non-negative
		if code.BizCode < 0 {
			t.Errorf("business code should not be negative: %d", code.BizCode)
		}
	}
}

func TestSuccessCode(t *testing.T) {
	if Success.HTTPStatus != http.StatusOK {
		t.Errorf("Success should have HTTP status 200, got %d", Success.HTTPStatus)
	}
	if Success.BizCode != 0 {
		t.Errorf("Success should have biz code 0, got %d", Success.BizCode)
	}
}

func TestClientErrorCodes(t *testing.T) {
	// 4xx errors are client errors
	clientErrors := []ErrCode{ErrUnauthorized, ErrRefreshExpired, ErrBadRequest, ErrNotFound, ErrContentViolation}

	for _, code := range clientErrors {
		if code.HTTPStatus < 400 || code.HTTPStatus >= 500 {
			t.Errorf("client error should be 4xx, got %d", code.HTTPStatus)
		}
	}
}

func TestServerErrorCodes(t *testing.T) {
	// 5xx errors are server errors
	serverErrors := []ErrCode{ErrInternal}

	for _, code := range serverErrors {
		if code.HTTPStatus < 500 || code.HTTPStatus >= 600 {
			t.Errorf("server error should be 5xx, got %d", code.HTTPStatus)
		}
	}
}
