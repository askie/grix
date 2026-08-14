package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// seedAgentShare 写一条 (agentID, ownerID -> sharedTo) 共享行，可选过期时间。
func seedAgentShare(t *testing.T, db *testutil.TestDB, agentID, ownerID, sharedTo int64, status int16, expiresAt *time.Time) {
	t.Helper()
	share := model.AgentShare{
		ID:        time.Now().UnixNano() + friendRelationIDCounter.Add(1),
		AgentID:   agentID,
		OwnerID:   ownerID,
		SharedTo:  sharedTo,
		Status:    status,
		ExpiresAt: expiresAt,
	}
	if err := db.DB.Create(&share).Error; err != nil {
		t.Fatalf("seed agent share agent=%d shared_to=%d err=%v", agentID, sharedTo, err)
	}
}

// setUserStatusForTest 把 testdb 里指定用户的 status 改为 ban/deleted，用于测试封号自动失效。
func setUserStatusForTest(t *testing.T, db *testutil.TestDB, userID int64, status int16) {
	t.Helper()
	if err := db.DB.Model(&model.User{}).
		Where("id = ?", userID).
		Update("status", status).Error; err != nil {
		t.Fatalf("set user %d status=%d err=%v", userID, status, err)
	}
}

func seedVoiceAgent(t *testing.T, db *testutil.TestDB, agentID, ownerID int64, status int16) {
	t.Helper()
	if err := db.DB.Create(&model.Agent{
		ID:           agentID,
		OwnerID:      ownerID,
		AgentName:    fmt.Sprintf("voice_%d", agentID),
		Status:       status,
		ProviderType: model.AgentProviderVoice,
	}).Error; err != nil {
		t.Fatalf("seed voice agent %d err=%v", agentID, err)
	}
}

// TestValidateAutoDelegateAgent_OwnerOrShared 守卫共享 agent 在文字托管 validate 下的全场景。
func TestValidateAutoDelegateAgent_OwnerOrShared(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(15001)
	sharedToID := int64(15002)
	strangerID := int64(15003)
	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, sharedToID)
	seedUser(t, testDB, strangerID)

	t.Run("owner 自己 OK", func(t *testing.T) {
		agentID := int64(15011)
		seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)
		if err := validateAutoDelegateAgent(ownerID, agentID); err != nil {
			t.Fatalf("owner 应通过, got %v", err)
		}
	})

	t.Run("active 共享给被共享者 OK", func(t *testing.T) {
		agentID := int64(15012)
		seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)
		seedAgentShare(t, testDB, agentID, ownerID, sharedToID, model.AgentShareStatusActive, nil)
		if err := validateAutoDelegateAgent(sharedToID, agentID); err != nil {
			t.Fatalf("active 共享应通过, got %v", err)
		}
	})

	t.Run("陌生人 拒 NotOwned", func(t *testing.T) {
		agentID := int64(15013)
		seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)
		if err := validateAutoDelegateAgent(strangerID, agentID); !errors.Is(err, ErrUserSettingsAutoAgentNotOwned) {
			t.Fatalf("陌生人应拒, got %v", err)
		}
	})

	t.Run("撤销共享 立即生效拒", func(t *testing.T) {
		agentID := int64(15014)
		seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)
		seedAgentShare(t, testDB, agentID, ownerID, sharedToID, model.AgentShareStatusRevoked, nil)
		if err := validateAutoDelegateAgent(sharedToID, agentID); !errors.Is(err, ErrUserSettingsAutoAgentNotOwned) {
			t.Fatalf("已撤销共享应拒, got %v", err)
		}
	})

	t.Run("过期共享 立即生效拒", func(t *testing.T) {
		agentID := int64(15015)
		seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)
		past := time.Now().Add(-time.Hour)
		seedAgentShare(t, testDB, agentID, ownerID, sharedToID, model.AgentShareStatusActive, &past)
		if err := validateAutoDelegateAgent(sharedToID, agentID); !errors.Is(err, ErrUserSettingsAutoAgentNotOwned) {
			t.Fatalf("过期共享应拒, got %v", err)
		}
	})

	t.Run("agent 被封禁 共享也拒 Unavailable", func(t *testing.T) {
		agentID := int64(15016)
		seedAgent(t, testDB, agentID, ownerID, 2) // status != 1
		seedAgentShare(t, testDB, agentID, ownerID, sharedToID, model.AgentShareStatusActive, nil)
		if err := validateAutoDelegateAgent(sharedToID, agentID); !errors.Is(err, ErrUserSettingsAutoAgentUnavailable) {
			t.Fatalf("封禁 agent 共享也应拒 Unavailable, got %v", err)
		}
	})

	t.Run("被共享者账号封禁 共享失效拒", func(t *testing.T) {
		agentID := int64(15017)
		bannedUserID := int64(15080)
		seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)
		seedUser(t, testDB, bannedUserID)
		setUserStatusForTest(t, testDB, bannedUserID, model.UserStatusBanned)
		seedAgentShare(t, testDB, agentID, ownerID, bannedUserID, model.AgentShareStatusActive, nil)
		if err := validateAutoDelegateAgent(bannedUserID, agentID); !errors.Is(err, ErrUserSettingsAutoAgentNotOwned) {
			t.Fatalf("被共享者封号后共享应失效, got %v", err)
		}
	})

	t.Run("agent 不存在 拒 NotFound", func(t *testing.T) {
		if err := validateAutoDelegateAgent(ownerID, int64(99999999)); !errors.Is(err, ErrUserSettingsAutoAgentNotFound) {
			t.Fatalf("不存在应 NotFound, got %v", err)
		}
	})
}

// TestValidateVoiceAutoDelegateAgent_OwnerOrShared 同上，额外验证 provider_type=voice。
func TestValidateVoiceAutoDelegateAgent_OwnerOrShared(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(15201)
	sharedToID := int64(15202)
	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, sharedToID)

	t.Run("owner type=voice OK", func(t *testing.T) {
		agentID := int64(15210)
		seedVoiceAgent(t, testDB, agentID, ownerID, 1)
		if err := validateVoiceAutoDelegateAgent(ownerID, agentID); err != nil {
			t.Fatalf("owner voice 应通过, got %v", err)
		}
	})

	t.Run("active 共享 type=voice OK", func(t *testing.T) {
		agentID := int64(15211)
		seedVoiceAgent(t, testDB, agentID, ownerID, 1)
		seedAgentShare(t, testDB, agentID, ownerID, sharedToID, model.AgentShareStatusActive, nil)
		if err := validateVoiceAutoDelegateAgent(sharedToID, agentID); err != nil {
			t.Fatalf("active 共享 voice 应通过, got %v", err)
		}
	})

	t.Run("共享但 agent 非 voice 类型 拒 NotVoice", func(t *testing.T) {
		agentID := int64(15212)
		if err := testDB.DB.Create(&model.Agent{
			ID:           agentID,
			OwnerID:      ownerID,
			AgentName:    "text",
			Status:       1,
			ProviderType: model.AgentProviderRemote,
		}).Error; err != nil {
			t.Fatalf("seed remote agent: %v", err)
		}
		seedAgentShare(t, testDB, agentID, ownerID, sharedToID, model.AgentShareStatusActive, nil)
		if err := validateVoiceAutoDelegateAgent(sharedToID, agentID); !errors.Is(err, ErrUserSettingsVoiceAgentNotVoice) {
			t.Fatalf("共享非 voice 类型应 NotVoice, got %v", err)
		}
	})

	t.Run("撤销共享 voice 也拒", func(t *testing.T) {
		agentID := int64(15213)
		seedVoiceAgent(t, testDB, agentID, ownerID, 1)
		seedAgentShare(t, testDB, agentID, ownerID, sharedToID, model.AgentShareStatusRevoked, nil)
		if err := validateVoiceAutoDelegateAgent(sharedToID, agentID); !errors.Is(err, ErrUserSettingsAutoAgentNotOwned) {
			t.Fatalf("撤销共享 voice 应拒, got %v", err)
		}
	})
}

// TestUpdateUserSettings_AcceptSharedAgent E2E 守卫:
// 调用 UpdateUserSettings 把共享给我的 agent 设成 auto_delegate,期待不被拒;
// 撤销共享后,再次调用同一 agent 应被拒。
func TestUpdateUserSettings_AcceptSharedAgent(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	ownerID := int64(15301)
	sharedToID := int64(15302)
	agentID := int64(15303)

	seedUser(t, testDB, ownerID)
	seedUser(t, testDB, sharedToID)
	seedAgent(t, testDB, agentID, ownerID, model.AgentStatusActive)
	seedAgentShare(t, testDB, agentID, ownerID, sharedToID, model.AgentShareStatusActive, nil)

	rawID := fmt.Sprintf("%d", agentID)
	resp, err := UpdateUserSettings(sharedToID, UserSettingsUpdateReq{
		Chat: &UserSettingsChatUpdateReq{
			AutoDelegateAgentID: OptionalStringUpdate{Set: true, Value: &rawID},
		},
	})
	if err != nil {
		t.Fatalf("被共享者设置共享 agent 应通过, got %v", err)
	}
	if resp.Chat.AutoDelegateAgentID == nil || *resp.Chat.AutoDelegateAgentID != agentID {
		t.Fatalf("expected auto_delegate_agent_id=%d, got %#v", agentID, resp.Chat.AutoDelegateAgentID)
	}

	// 撤销共享后,再尝试设置应被拒(模拟主人 revoke)
	if err := store.DB.Model(&model.AgentShare{}).
		Where("agent_id = ? AND shared_to = ?", agentID, sharedToID).
		Update("status", model.AgentShareStatusRevoked).Error; err != nil {
		t.Fatalf("revoke share err: %v", err)
	}
	if _, err := UpdateUserSettings(sharedToID, UserSettingsUpdateReq{
		Chat: &UserSettingsChatUpdateReq{
			AutoDelegateAgentID: OptionalStringUpdate{Set: true, Value: &rawID},
		},
	}); !errors.Is(err, ErrUserSettingsAutoAgentNotOwned) {
		t.Fatalf("撤销后再设置应被拒, got %v", err)
	}
}
