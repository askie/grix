package service

import (
	"context"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/security"
)

func clearAdminLoginLock(ctx context.Context, admin *model.AdminUser) error {
	if admin == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	guards := []*security.LoginGuard{
		security.NewAdminLoginGuardByAdminID(admin.ID),
	}

	username := strings.TrimSpace(admin.Username)
	if username != "" {
		guards = append(guards, security.NewAdminLoginGuardByUsername(username))
	}

	for _, guard := range guards {
		if err := guard.ClearLock(ctx); err != nil {
			return err
		}
	}
	return nil
}
