package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/pkg/userpref"
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

// newVoiceAgentWithWelcome 建一个配好中英开场白的语音 agent，供主叫语言用例复用。
func newVoiceAgentWithWelcome(t *testing.T, keySeed string) model.Agent {
	t.Helper()
	cipher, err := secretcrypto.Encrypt(keySeed)
	require.NoError(t, err)

	welcome, err := json.Marshal(map[string]string{
		"en_US": "Hello!",
		"zh_CN": "你好！",
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
	return agent
}

// App 内通话（非 widget）按主叫用户的语言偏好选开场白：
// 设过 zh-CN 讲中文；没设过 / 查询出错时都回退 en_US，且绝不因为取语言失败而报错。
func TestResolveCallerLocale_DrivesOpeningLanguage(t *testing.T) {
	config.C.Server.VoiceCryptoSecret = "call-deps-test-secret"
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	defer func() { store.DB = nil }()
	require.NoError(t, snowflake.Init(1))

	agent := newVoiceAgentWithWelcome(t, "sk-test-caller-locale")

	const (
		zhUserID    int64 = 90001
		noPrefUser  int64 = 90002
		dbErrUserID int64 = 90003
	)
	for _, uid := range []int64{zhUserID, noPrefUser, dbErrUserID} {
		userpref.InvalidatePreferredLanguage(uid)
		defer userpref.InvalidatePreferredLanguage(uid)
	}

	// 1. 主叫设置了中文（库里存归一化后的 "zh"，等价于 App 传的 zh-CN）→ 中文开场白
	require.NoError(t, store.DB.Create(&model.UserSetting{
		UserID:            zhUserID,
		PreferredLanguage: userpref.NormalizeLanguage("zh-CN"),
	}).Error)
	require.Equal(t, "zh", resolveCallerLocale(zhUserID))

	spec, err := resolveAgentVoiceSpec(agent.ID, resolveCallerLocale(zhUserID))
	require.NoError(t, err)
	require.Equal(t, "zh_CN", spec.Language)
	require.Equal(t, "你好！", spec.Opening)

	// 2. 主叫没有任何语言偏好记录 → 空串 → 归一化兜底 en_US（不能兜底成 zh）
	require.Equal(t, "", resolveCallerLocale(noPrefUser))

	spec, err = resolveAgentVoiceSpec(agent.ID, resolveCallerLocale(noPrefUser))
	require.NoError(t, err)
	require.Equal(t, "en_US", spec.Language)
	require.Equal(t, "Hello!", spec.Opening)

	// 3. 查询偏好出错（这里用删表模拟）→ 依然返回空串走 en_US，通话不受影响
	require.NoError(t, store.DB.Migrator().DropTable(&model.UserSetting{}))
	require.Equal(t, "", resolveCallerLocale(dbErrUserID))

	spec, err = resolveAgentVoiceSpec(agent.ID, resolveCallerLocale(dbErrUserID))
	require.NoError(t, err)
	require.Equal(t, "en_US", spec.Language)
	require.Equal(t, "Hello!", spec.Opening)

	// 查询失败不写缓存：建回表后同一用户能立刻读到真实偏好，不用等 TTL 过期。
	require.NoError(t, store.DB.Migrator().AutoMigrate(&model.UserSetting{}))
	require.NoError(t, store.DB.Create(&model.UserSetting{
		UserID:            dbErrUserID,
		PreferredLanguage: "ja",
	}).Error)
	require.Equal(t, "ja", resolveCallerLocale(dbErrUserID))
}

// 非法 userID（AI 代接路径拿不到主叫方时会传 0）不查库，直接空串兜底 en_US。
func TestResolveCallerLocale_InvalidUserID(t *testing.T) {
	require.Equal(t, "", resolveCallerLocale(0))
	require.Equal(t, "", resolveCallerLocale(-1))
}

// callCtrl 未注入时 AI 代接路径取不到主叫方，返回空串而不是 panic。
func TestResolveCallerLocaleByCallID_NoController(t *testing.T) {
	orig := callCtrl
	callCtrl = nil
	defer func() { callCtrl = orig }()

	require.Equal(t, "", resolveCallerLocaleByCallID(12345))
}
