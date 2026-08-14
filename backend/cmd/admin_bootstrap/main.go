package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/askie/grix/backend/config"
	adminservice "github.com/askie/grix/backend/internal/admin/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
)

func main() {
	configPath := flag.String("config", "config.yaml", "config file path")
	username := flag.String("username", "", "admin username")
	password := flag.String("password", "", "admin password")
	nickname := flag.String("nickname", "Super Admin", "admin nickname")
	flag.Parse()

	if strings.TrimSpace(*username) == "" || strings.TrimSpace(*password) == "" {
		fmt.Fprintln(os.Stderr, "username and password are required")
		os.Exit(1)
	}
	if err := adminservice.ValidateAdminPassword(*password); err != nil {
		fmt.Fprintf(os.Stderr, "invalid password: %v\n", err)
		os.Exit(1)
	}

	logger.Init()
	config.Load(*configPath)
	store.InitPostgres(config.C.Postgres)
	store.MustInitSchema()

	if err := snowflake.Init(config.C.Snowflake.MachineID); err != nil {
		logger.L.Fatalf("snowflake init error: %v", err)
	}

	hash, err := adminservice.HashAdminPassword(*password)
	if err != nil {
		logger.L.Fatalf("password hash error: %v", err)
	}

	admin := model.AdminUser{
		ID:           snowflake.GenID(),
		Username:     strings.TrimSpace(*username),
		PasswordHash: hash,
		Nickname:     strings.TrimSpace(*nickname),
		Role:         model.AdminRoleSuperAdmin,
		Status:       model.AdminStatusActive,
	}

	var existing model.AdminUser
	if err := store.DB.Where("username = ?", admin.Username).First(&existing).Error; err == nil {
		if err := store.DB.Model(&model.AdminUser{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{
				"password_hash": admin.PasswordHash,
				"nickname":      admin.Nickname,
				"status":        model.AdminStatusActive,
			}).Error; err != nil {
			logger.L.Fatalf("update admin error: %v", err)
		}
		fmt.Printf("admin updated: %s\n", admin.Username)
		return
	}

	if err := store.DB.Create(&admin).Error; err != nil {
		logger.L.Fatalf("create admin error: %v", err)
	}
	fmt.Printf("admin created: %s\n", admin.Username)
}
