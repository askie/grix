package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

// resolveAgentVoiceSpec 真实实现（非各测试文件里替换的 mock）按 locale 解析
// Language + Opening：Language 恒归一化非空，Opening 按语言从 agent 配置的
// VoiceWelcomeI18n 中选取，找不到精确语言时回退 en_US。
func TestResolveAgentVoiceSpec_LanguageAndOpening(t *testing.T) {
	config.C.Server.VoiceCryptoSecret = "call-deps-test-secret"
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	defer func() { store.DB = nil }()
	require.NoError(t, snowflake.Init(1))

	cipher, err := secretcrypto.Encrypt("sk-test-voice-key")
	require.NoError(t, err)

	welcome, err := json.Marshal(map[string]string{
		"en_US": "Hi, how can I help?",
		"zh_CN": "您好，请问有什么可以帮您？",
	})
	require.NoError(t, err)

	agent := model.Agent{
		ID:                snowflake.GenID(),
		OwnerID:           1,
		ProviderType:      model.AgentProviderVoice,
		Status:            1,
		VoiceProvider:     "openai_realtime",
		VoiceModel:        "gpt-4o-realtime-preview",
		VoiceAPIKeyCipher: cipher,
		VoiceWelcomeI18n:  datatypes.JSON(welcome),
	}
	require.NoError(t, store.DB.Create(&agent).Error)

	spec, err := resolveAgentVoiceSpec(agent.ID, "zh-CN")
	require.NoError(t, err)
	require.Equal(t, "zh_CN", spec.Language)
	require.Equal(t, "您好，请问有什么可以帮您？", spec.Opening)

	// 未知语言归一化回退 en_US，Opening 同样回退 en_US 文案
	spec, err = resolveAgentVoiceSpec(agent.ID, "xx-YY")
	require.NoError(t, err)
	require.Equal(t, "en_US", spec.Language)
	require.Equal(t, "Hi, how can I help?", spec.Opening)

	// 空 locale（非 widget 场景，无来源）同样归一化兜底 en_US
	spec, err = resolveAgentVoiceSpec(agent.ID, "")
	require.NoError(t, err)
	require.Equal(t, "en_US", spec.Language)
	require.Equal(t, "Hi, how can I help?", spec.Opening)
}

// agent 未配置 VoiceWelcomeI18n 时 Opening 为空（不主动打招呼），Language 仍归一化。
func TestResolveAgentVoiceSpec_NoOpeningConfigured(t *testing.T) {
	config.C.Server.VoiceCryptoSecret = "call-deps-test-secret"
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	defer func() { store.DB = nil }()
	require.NoError(t, snowflake.Init(1))

	cipher, err := secretcrypto.Encrypt("sk-test-voice-key-2")
	require.NoError(t, err)

	agent := model.Agent{
		ID:                snowflake.GenID(),
		OwnerID:           1,
		ProviderType:      model.AgentProviderVoice,
		Status:            1,
		VoiceProvider:     "doubao_realtime",
		VoiceModel:        "O",
		VoiceAPIKeyCipher: cipher,
	}
	require.NoError(t, store.DB.Create(&agent).Error)

	spec, err := resolveAgentVoiceSpec(agent.ID, "ja-JP")
	require.NoError(t, err)
	require.Equal(t, "ja_JP", spec.Language)
	require.Empty(t, spec.Opening)
}
