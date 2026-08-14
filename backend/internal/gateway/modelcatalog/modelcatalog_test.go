package modelcatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 连接器按 catalog_version + catalog_sha256 校验落盘，版本与摘要必须稳定、可复算。
func TestCatalogVersionAndSHAStable(t *testing.T) {
	require.NotEmpty(t, Version)

	raw := CanonicalJSON()
	require.NotEmpty(t, raw)

	var parsed Catalog
	require.NoError(t, json.Unmarshal(raw, &parsed))
	assert.Equal(t, Version, parsed.Version, "内嵌 Version 常量必须与 catalog 负载一致")

	sum := sha256.Sum256(raw)
	assert.Equal(t, hex.EncodeToString(sum[:]), SHA256(), "SHA256 必须是对 CanonicalJSON 字节的摘要")
	// 两次计算结果一致（连接器跨进程复算要能对上）。
	assert.Equal(t, CanonicalJSON(), CanonicalJSON())
}

// 当前直连只对 DeepSeek 双模型开放 capability，两个规范名必须都在 catalog 里。
func TestCatalogCoversDeepSeekModels(t *testing.T) {
	assert.True(t, Has("deepseek-v4-flash"))
	assert.True(t, Has("deepseek-v4-pro"))
	assert.False(t, Has("gpt-5"))

	c := Current()
	require.Len(t, c.Models, 2)

	ids := map[string]bool{}
	for _, m := range c.Models {
		assert.NotEmpty(t, m.Slug)
		assert.NotEmpty(t, m.DisplayName)
		assert.NotEmpty(t, m.BaseInstructions)
		assert.NotEmpty(t, m.ShellType)
		assert.NotEmpty(t, m.Visibility)
		assert.True(t, m.SupportedInAPI)
		assert.Equal(t, "tokens", m.TruncationPolicy.Mode)
		assert.Greater(t, m.TruncationPolicy.Limit, 0)
		assert.Greater(t, m.ContextWindow, 0)
		assert.False(t, ids[m.Slug], "模型 ID 重复: %s", m.Slug)
		ids[m.Slug] = true
	}
}

// Codex 0.144 对 models[] 仍有一组反序列化必填字段；缺任一项会在 app-server
// 启动前拒绝整个 catalog。这里锁住真实 CLI 已验证过的最小合同。
func TestCanonicalJSONContainsCodexRequiredModelFields(t *testing.T) {
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	require.NoError(t, json.Unmarshal(CanonicalJSON(), &payload))
	require.NotEmpty(t, payload.Models)
	requiredFields := []string{
		"slug",
		"display_name",
		"base_instructions",
		"experimental_supported_tools",
		"priority",
		"shell_type",
		"support_verbosity",
		"supported_in_api",
		"supported_reasoning_levels",
		"supports_parallel_tool_calls",
		"supports_reasoning_summaries",
		"truncation_policy",
		"visibility",
		"context_window",
		"max_context_window",
	}
	for _, item := range payload.Models {
		for _, field := range requiredFields {
			assert.Contains(t, item, field, "Codex models.json 必填字段缺失: %s", field)
		}
	}
}

// 可选真实兼容验收：让本机 Codex 直接解析后端将要下发的 CanonicalJSON。
// 默认单测不依赖外部 CLI；发布前显式打开开关执行。
func TestCanonicalJSONParsesInRealCodex(t *testing.T) {
	if os.Getenv("RUN_CODEX_CATALOG_COMPAT_E2E") != "1" {
		t.Skip("set RUN_CODEX_CATALOG_COMPAT_E2E=1 to run against the installed Codex CLI")
	}
	command := os.Getenv("CODEX_E2E_COMMAND")
	if command == "" {
		command = "codex"
	}
	if _, err := exec.LookPath(command); err != nil {
		t.Fatalf("Codex CLI not found: %v", err)
	}

	root := t.TempDir()
	catalogPath := filepath.Join(root, "models.json")
	require.NoError(t, os.WriteFile(catalogPath, CanonicalJSON(), 0o600))
	codexHome := filepath.Join(root, "codex-home")
	require.NoError(t, os.Mkdir(codexHome, 0o700))
	cmd := exec.Command(
		command,
		"-c", "model_catalog_json="+strconv.Quote(catalogPath),
		"debug", "models",
	)
	cmd.Env = append(os.Environ(), "CODEX_HOME="+codexHome)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Codex rejected backend catalog: %s", string(output))
}

// Current 返回拷贝，调用方改动不得污染全局（catalog 内容进计费路径的合同，必须恒定）。
func TestCurrentReturnsCopy(t *testing.T) {
	c := Current()
	c.Models[0].Slug = "tampered"
	if len(c.Models[1].SupportedReasoningLevels) > 0 {
		c.Models[1].SupportedReasoningLevels[0].Effort = "tampered"
	}
	assert.Equal(t, "deepseek-v4-flash", Current().Models[0].Slug)
	assert.Equal(t, "low", Current().Models[1].SupportedReasoningLevels[0].Effort)
}
