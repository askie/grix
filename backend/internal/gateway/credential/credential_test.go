package credential

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	logger.Init() // 解密失败跳过路径会打日志，测试里也需初始化 logger.L，否则 nil 解引用
	_ = snowflake.Init(1)
	tdb := testutil.NewTestDB()
	t.Cleanup(tdb.Close)
	return New(tdb.DB)
}

func TestCreate_EncryptsAndHints(t *testing.T) {
	s := newTestService(t)
	cred, err := s.Create(CreateInput{Provider: "deepseek", APIKey: "test", Label: "主Key"})
	require.NoError(t, err)

	// 库里存的是密文，绝不能等于明文；末4位 hint 用于展示。
	assert.NotEqual(t, "test", cred.APIKeyEnc)
	assert.NotEmpty(t, cred.APIKeyEnc)
	assert.Equal(t, "test", cred.KeyHint)
	assert.Equal(t, PurposeInference, cred.Purpose) // 默认 inference
	assert.True(t, cred.Enabled)

	// 解密取用应还原出明文。
	got, err := s.NextInference("deepseek")
	require.NoError(t, err)
	assert.Equal(t, "test", got.APIKey)
}

func TestNextInference_RoundRobinSkipsDisabled(t *testing.T) {
	s := newTestService(t)
	_, err := s.Create(CreateInput{Provider: "deepseek", APIKey: "key-AAAA"})
	require.NoError(t, err)
	_, err = s.Create(CreateInput{Provider: "deepseek", APIKey: "key-BBBB"})
	require.NoError(t, err)
	disabled, err := s.Create(CreateInput{Provider: "deepseek", APIKey: "key-CCCC"})
	require.NoError(t, err)
	require.NoError(t, s.SetEnabled(disabled.ID, false))

	seen := map[string]int{}
	for i := 0; i < 6; i++ {
		r, err := s.NextInference("deepseek")
		require.NoError(t, err)
		seen[r.APIKey]++
	}
	// 两把启用的应各被轮到，停用的那把绝不出现。
	assert.Equal(t, 3, seen["key-AAAA"])
	assert.Equal(t, 3, seen["key-BBBB"])
	assert.Equal(t, 0, seen["key-CCCC"])
}

func TestNextInference_SkipsUndecryptableButKeepsGood(t *testing.T) {
	s := newTestService(t)
	good, err := s.Create(CreateInput{Provider: "deepseek", APIKey: "key-GOOD"})
	require.NoError(t, err)

	// 手动塞一把密文损坏的启用凭据（模拟密文损坏/主密钥轮换）。
	require.NoError(t, s.db.Create(&model.GatewayUpstreamCredential{
		ID: good.ID + 1, Provider: "deepseek", Purpose: PurposeInference,
		APIKeyEnc: "@@@not-valid-base64-ciphertext@@@", KeyHint: "junk", Enabled: true,
	}).Error)

	// 一把坏Key绝不能拖垮整组：仍应稳定拿到那把好Key。
	for i := 0; i < 4; i++ {
		r, err := s.NextInference("deepseek")
		require.NoError(t, err)
		assert.Equal(t, "key-GOOD", r.APIKey)
	}
}

func TestNextInference_NoCredential(t *testing.T) {
	s := newTestService(t)
	_, err := s.NextInference("deepseek")
	assert.True(t, errors.Is(err, ErrNoCredential))

	// 只有对账凭据、没有推理凭据时，推理解析也应报 ErrNoCredential。
	_, err = s.Create(CreateInput{Provider: "volcano_ark", Purpose: PurposeReconcile, APIKey: "AK", APISecret: "SK", Region: "cn-beijing"})
	require.NoError(t, err)
	_, err = s.NextInference("volcano_ark")
	assert.True(t, errors.Is(err, ErrNoCredential))
}

func TestFirstReconcile(t *testing.T) {
	s := newTestService(t)
	_, ok := s.FirstReconcile("volcano_ark")
	assert.False(t, ok)

	_, err := s.Create(CreateInput{Provider: "volcano_ark", Purpose: PurposeReconcile, APIKey: "AK-123", APISecret: "SK-456", Region: "cn-shanghai"})
	require.NoError(t, err)

	r, ok := s.FirstReconcile("volcano_ark")
	require.True(t, ok)
	assert.Equal(t, "AK-123", r.APIKey)
	assert.Equal(t, "SK-456", r.APISecret)
	assert.Equal(t, "cn-shanghai", r.Region)
}

func TestDelete(t *testing.T) {
	s := newTestService(t)
	cred, err := s.Create(CreateInput{Provider: "deepseek", APIKey: "key-DDDD"})
	require.NoError(t, err)
	require.NoError(t, s.Delete(cred.ID))

	_, err = s.NextInference("deepseek")
	assert.True(t, errors.Is(err, ErrNoCredential))

	// 删除不存在的记录应报 not found。
	assert.Error(t, s.Delete(cred.ID))
}

func TestList_NeverLeaksPlaintext(t *testing.T) {
	s := newTestService(t)
	_, err := s.Create(CreateInput{Provider: "deepseek", APIKey: "sk-secret-tail9999", Label: "x"})
	require.NoError(t, err)

	rows, err := s.List("deepseek")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "9999", rows[0].KeyHint)
	assert.NotContains(t, rows[0].APIKeyEnc, "secret") // 密文里不含明文片段

	// JSON 序列化不得带出密文字段（json:"-"）。
	var m model.GatewayUpstreamCredential = rows[0]
	assert.NotEmpty(t, m.APIKeyEnc) // 结构体内有值
}
