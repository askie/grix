package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

// 语音大脑设置（owner 主动呼出的语音通道）：必须是 owner 本人的 type=4 语音大模型，
// 与语音托管(voice_auto_delegate)各存各的、互不影响。
func TestUpdateUserSettings_VoiceBrain(t *testing.T) {
	testDB, cleanup := setupSessionTest(t)
	defer cleanup()

	userID := int64(9701)
	voiceAgentID := int64(9702)
	textAgentID := int64(9703)
	seedUser(t, testDB, userID)
	if err := testDB.DB.Create(&model.Agent{ID: voiceAgentID, OwnerID: userID, AgentName: "voice", Status: 1, ProviderType: model.AgentProviderVoice}).Error; err != nil {
		t.Fatalf("seed voice agent: %v", err)
	}
	if err := testDB.DB.Create(&model.Agent{ID: textAgentID, OwnerID: userID, AgentName: "text", Status: 1, ProviderType: model.AgentProviderRemote}).Error; err != nil {
		t.Fatalf("seed text agent: %v", err)
	}

	// 设置 type=4 语音大脑成功
	raw := fmt.Sprintf("%d", voiceAgentID)
	resp, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
		Chat: &UserSettingsChatUpdateReq{
			VoiceBrainAgentID: OptionalStringUpdate{Set: true, Value: &raw},
		},
	})
	if err != nil {
		t.Fatalf("set voice brain error: %v", err)
	}
	if resp.Chat.VoiceBrainAgentID == nil || *resp.Chat.VoiceBrainAgentID != voiceAgentID {
		t.Fatalf("expected voice_brain_agent_id=%d, got %#v", voiceAgentID, resp.Chat.VoiceBrainAgentID)
	}
	if gotID, ok := LoadUserVoiceBrainAgentID(userID); !ok || gotID != voiceAgentID {
		t.Fatalf("LoadUserVoiceBrainAgentID = %d,%v want %d,true", gotID, ok, voiceAgentID)
	}

	// 语音大脑与语音托管互不影响：设语音大脑不应动到 voice_auto_delegate
	if resp.Chat.VoiceAutoDelegateAgentID != nil {
		t.Fatalf("voice brain update must not set voice_auto_delegate, got %v", *resp.Chat.VoiceAutoDelegateAgentID)
	}

	// 非 type=4 被拒
	rawText := fmt.Sprintf("%d", textAgentID)
	if _, err := UpdateUserSettings(userID, UserSettingsUpdateReq{
		Chat: &UserSettingsChatUpdateReq{
			VoiceBrainAgentID: OptionalStringUpdate{Set: true, Value: &rawText},
		},
	}); !errors.Is(err, ErrUserSettingsVoiceAgentNotVoice) {
		t.Fatalf("expected ErrUserSettingsVoiceAgentNotVoice, got %v", err)
	}

	// 清空
	blank := ""
	resp, err = UpdateUserSettings(userID, UserSettingsUpdateReq{
		Chat: &UserSettingsChatUpdateReq{
			VoiceBrainAgentID: OptionalStringUpdate{Set: true, Value: &blank},
		},
	})
	if err != nil {
		t.Fatalf("clear voice brain error: %v", err)
	}
	if resp.Chat.VoiceBrainAgentID != nil {
		t.Fatalf("expected cleared voice_brain_agent_id, got %v", *resp.Chat.VoiceBrainAgentID)
	}
}
