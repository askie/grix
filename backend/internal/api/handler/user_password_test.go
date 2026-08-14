package handler

import (
	"errors"
	"testing"

	"github.com/askie/grix/backend/internal/api/service"
)

func TestUserPasswordErrorMessage(t *testing.T) {
	t.Run("known business error", func(t *testing.T) {
		msg := userPasswordErrorMessage(service.ErrChangePasswordCodeInvalid)
		if msg != service.ErrChangePasswordCodeInvalid.Error() {
			t.Fatalf("expected known business message, got %q", msg)
		}
	})

	t.Run("unexpected internal error", func(t *testing.T) {
		msg := userPasswordErrorMessage(errors.New("sql: connection refused"))
		if msg != "修改密码失败，请稍后重试" {
			t.Fatalf("expected generic message, got %q", msg)
		}
	})
}
