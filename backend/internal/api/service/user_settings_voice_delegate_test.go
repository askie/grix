package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

func TestUpdateUserSettings_VoiceAutoDelegate(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(9601)
	voiceAgentID := int64(9602)
	textAgentID := int64(9603)
	seedUser(t, testDB, userID)
	if err := testDB.DB.Create(&model.Agent{ID: voiceAgentID, OwnerID: userID, AgentName: "voice", Status: 1, ProviderType: model.AgentProviderVoice}).Error; err != nil {
		t.Fatalf("seed voice agent: %v", err)
	}
	if err := testDB.DB.Create(&model.Agent{ID: textAgentID, OwnerID: userID, AgentName: "text", Status: 1, ProviderType: model.AgentProviderRemote}).Error; err != nil {
		t.Fatalf("seed text agent: %v", err)
	}

	// 设置 type=4 语音 agent 成功
	raw := fmt.Sprintf("%d", voiceAgentID)
	resp, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
		Chat: &UserSettingsChatUpdateReq{
			VoiceAutoDelegateAgentID: OptionalStringUpdate{Set: true, Value: &raw},
		},
	})
	if err != nil {
		t.Fatalf("set voice auto delegate error: %v", err)
	}
	if resp.Chat.VoiceAutoDelegateAgentID == nil || *resp.Chat.VoiceAutoDelegateAgentID != voiceAgentID {
		t.Fatalf("expected voice_auto_delegate_agent_id=%d, got %#v", voiceAgentID, resp.Chat.VoiceAutoDelegateAgentID)
	}
	if gotID, ok := LoadUserVoiceAutoDelegateAgentID(userID); !ok || gotID != voiceAgentID {
		t.Fatalf("LoadUserVoiceAutoDelegateAgentID = %d,%v want %d,true", gotID, ok, voiceAgentID)
	}

	// 非 type=4 被拒
	rawText := fmt.Sprintf("%d", textAgentID)
	if _, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
		Chat: &UserSettingsChatUpdateReq{
			VoiceAutoDelegateAgentID: OptionalStringUpdate{Set: true, Value: &rawText},
		},
	}); !errors.Is(err, ErrUserSettingsVoiceAgentNotVoice) {
		t.Fatalf("expected ErrUserSettingsVoiceAgentNotVoice, got %v", err)
	}

	// 清空
	blank := ""
	resp, err = UpdateUserSettings(userID, UserSettingsUpdateReq{
		Chat: &UserSettingsChatUpdateReq{
			VoiceAutoDelegateAgentID: OptionalStringUpdate{Set: true, Value: &blank},
		},
	})
	if err != nil {
		t.Fatalf("clear voice auto delegate error: %v", err)
	}
	if resp.Chat.VoiceAutoDelegateAgentID != nil {
		t.Fatalf("expected cleared voice_auto_delegate_agent_id, got %v", *resp.Chat.VoiceAutoDelegateAgentID)
	}
}
