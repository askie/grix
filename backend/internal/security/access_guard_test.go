package security

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestPasswordChangeMarker(t *testing.T) {
	store.RDB = testutil.NewMockRedis()

	userID := int64(9001)
	changedAt := time.Now().UTC()
	if err := MarkUserPasswordChanged(userID, changedAt); err != nil {
		t.Fatalf("MarkUserPasswordChanged() error = %v", err)
	}

	if !IsAccessTokenInvalidByPasswordChange(userID, changedAt.Add(-1*time.Second)) {
		t.Fatal("expected token issued before password change to be invalid")
	}
	if IsAccessTokenInvalidByPasswordChange(userID, changedAt) {
		t.Fatal("expected token issued at marker time to remain valid")
	}
	if IsAccessTokenInvalidByPasswordChange(userID, changedAt.Add(1*time.Second)) {
		t.Fatal("expected token issued after password change to remain valid")
	}
}

func TestMarkUserPasswordChangedWithoutRedis(t *testing.T) {
	store.RDB = nil
	if err := MarkUserPasswordChanged(1001, time.Now()); err != nil {
		t.Fatalf("expected nil error when redis unavailable, got %v", err)
	}
}
