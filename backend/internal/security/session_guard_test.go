package security

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestLoginSessionRevocationMarker(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		userID    int64 = 9301
		sessionID       = "revoked-session-1"
	)

	revokedAt := time.Now().UTC()
	if err := testDB.DB.Create(&model.LoginDeviceSession{
		SessionID:  sessionID,
		UserID:     userID,
		DeviceID:   "ios-device-9301",
		Platform:   "ios",
		LastSeenAt: revokedAt,
		RevokedAt:  &revokedAt,
	}).Error; err != nil {
		t.Fatalf("seed login device session error = %v", err)
	}

	if !IsLoginSessionRevoked(userID, sessionID) {
		t.Fatal("expected login session to be revoked")
	}
}
