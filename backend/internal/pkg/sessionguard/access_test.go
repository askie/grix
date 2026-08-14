package sessionguard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func TestValidateSessionAvailable(t *testing.T) {
	testDB := testutil.NewTestDB()
	previousDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = previousDB
		testDB.Close()
	})

	now := time.Now().UTC()
	sessions := []model.Session{
		{
			SessionID:        "direct-normal",
			OwnerID:          1001,
			SessionType:      model.SessionTypeDirect,
			ModerationStatus: model.SessionModerationStatusActive,
			IsDeleted:        false,
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		{
			SessionID:        "group-banned",
			OwnerID:          1002,
			SessionType:      model.SessionTypeGroup,
			ModerationStatus: model.SessionModerationStatusBanned,
			IsDeleted:        false,
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		{
			SessionID:        "deleted-session",
			OwnerID:          1003,
			SessionType:      model.SessionTypeDirect,
			ModerationStatus: model.SessionModerationStatusActive,
			IsDeleted:        true,
			CreatedAt:        now,
			UpdatedAt:        now,
		},
	}
	for i := range sessions {
		if err := store.DB.Create(&sessions[i]).Error; err != nil {
			t.Fatalf("seed session %s error: %v", sessions[i].SessionID, err)
		}
	}

	t.Run("empty session id", func(t *testing.T) {
		err := ValidateSessionAvailable(context.Background(), store.DB, "   ")
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("ValidateSessionAvailable() error = %v, want %v", err, gorm.ErrRecordNotFound)
		}
	})

	t.Run("nil db fallback", func(t *testing.T) {
		err := ValidateSessionAvailable(nil, nil, "direct-normal")
		if err != nil {
			t.Fatalf("ValidateSessionAvailable() error = %v, want nil", err)
		}
	})

	t.Run("deleted session", func(t *testing.T) {
		err := ValidateSessionAvailable(context.Background(), store.DB, "deleted-session")
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("ValidateSessionAvailable() error = %v, want %v", err, gorm.ErrRecordNotFound)
		}
	})

	t.Run("group banned", func(t *testing.T) {
		err := ValidateSessionAvailable(context.Background(), store.DB, "group-banned")
		if !errors.Is(err, ErrSessionBanned) {
			t.Fatalf("ValidateSessionAvailable() error = %v, want %v", err, ErrSessionBanned)
		}
	})

	t.Run("normal session", func(t *testing.T) {
		err := ValidateSessionAvailable(context.Background(), store.DB, "direct-normal")
		if err != nil {
			t.Fatalf("ValidateSessionAvailable() error = %v, want nil", err)
		}
	})
}
