// Package testutil provides testing utilities for the backend
package testutil

import (
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestDB wraps an in-memory SQLite database for testing
type TestDB struct {
	DB *gorm.DB
}

// NewTestDB creates an in-memory SQLite database for testing
func NewTestDB() *TestDB {
	dsn := fmt.Sprintf("file:testdb_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		panic("failed to connect test database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to access test sql database")
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	// Tests use GORM model migration on in-memory SQLite.
	if err := store.AutoMigrateWithDB(db); err != nil {
		panic("failed to init test database schema: " + err.Error())
	}

	return &TestDB{DB: db}
}

// Close closes the test database
func (t *TestDB) Close() {
	sqlDB, _ := t.DB.DB()
	_ = sqlDB.Close()
}

// Cleanup truncates all tables
func (t *TestDB) Cleanup() {
	t.DB.Exec("DELETE FROM users")
	t.DB.Exec("DELETE FROM sessions")
	t.DB.Exec("DELETE FROM session_members")
	t.DB.Exec("DELETE FROM messages")
	t.DB.Exec("DELETE FROM user_inbox")
	t.DB.Exec("DELETE FROM devices")
	t.DB.Exec("DELETE FROM friend_requests")
	t.DB.Exec("DELETE FROM friends")
	t.DB.Exec("DELETE FROM user_blocks")
	t.DB.Exec("DELETE FROM group_qr_codes")
	t.DB.Exec("DELETE FROM user_settings")
}

// FixtureBuilder helps create test data
type FixtureBuilder struct {
	db *gorm.DB
}

// NewFixtureBuilder creates a new fixture builder
func NewFixtureBuilder(db *gorm.DB) *FixtureBuilder {
	return &FixtureBuilder{db: db}
}

// userIDCounter ensures unique IDs in fast loops
var userIDCounter int64

// CreateUser creates a test user
func (f *FixtureBuilder) CreateUser(overrides ...func(*model.User)) *model.User {
	// Generate unique ID: timestamp + counter to avoid collisions in tight loops
	baseID := time.Now().UnixNano()
	userIDCounter++
	uniqueID := baseID + userIDCounter

	user := &model.User{
		ID:           uniqueID,
		Username:     "testuser",
		Email:        fmt.Sprintf("testuser%d@example.com", uniqueID),
		PasswordHash: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZRGdjGj/n3.eG1H0B/XQsI3a3hOyC", // bcrypt hash of "password123"
		Nickname:     "Test User",
	}
	for _, override := range overrides {
		override(user)
	}
	f.db.Create(user)
	return user
}

// CreateSession creates a test session
func (f *FixtureBuilder) CreateSession(overrides ...func(*model.Session)) *model.Session {
	session := &model.Session{
		SessionID:   "test-session-" + time.Now().Format("20060102150405"),
		OwnerID:     1,
		SessionType: 1, // 1: private chat
	}
	for _, override := range overrides {
		override(session)
	}
	f.db.Create(session)
	return session
}

// CreateUserWithDefaults creates a user with specified fields
func (f *FixtureBuilder) CreateUserWithDefaults(id int64, username string) *model.User {
	return f.CreateUser(func(u *model.User) {
		u.ID = id
		u.Username = username
		u.Email = username + "@example.com"
		u.Nickname = username
	})
}

// NewMockRedis creates a mock Redis client using miniredis
// This returns a real redis.Client connected to an in-memory Redis server
func NewMockRedis() *redis.Client {
	mr, err := miniredis.Run()
	if err != nil {
		panic("failed to start miniredis: " + err.Error())
	}

	return redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
}
