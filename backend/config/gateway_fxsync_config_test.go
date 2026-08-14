package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

// 验证 config.yaml 里的 "24h" 能被 viper 解码成 time.Duration（依赖 mapstructure 的 duration hook）。
func TestFxSyncYamlDurationDecodes(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	viper.SetConfigFile("../config.yaml")
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("读取 config.yaml 失败: %v", err)
	}
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}
	fx := cfg.Gateway.FxSync
	if !fx.Enabled {
		t.Fatalf("enabled 应为 true")
	}
	if fx.Interval != 24*time.Hour {
		t.Fatalf("interval 应解析为 24h，实际 %v", fx.Interval)
	}
	if len(fx.Currencies) != 1 || fx.Currencies[0] != "CNY" {
		t.Fatalf("currencies 应为 [CNY]，实际 %v", fx.Currencies)
	}
	t.Logf("PASS yaml: enabled=%v interval=%v currencies=%v", fx.Enabled, fx.Interval, fx.Currencies)
}

// 验证 4 个环境变量都能覆盖 yaml 值（memory 记载：新配置项必须进 applyEnvOverrides 白名单）。
func TestFxSyncEnvOverrides(t *testing.T) {
	t.Setenv("AIBOT_GATEWAY_FXSYNC_ENABLED", "false")
	t.Setenv("AIBOT_GATEWAY_FXSYNC_CURRENCIES", "CNY,EUR")
	t.Setenv("AIBOT_GATEWAY_FXSYNC_INTERVAL", "90m")
	t.Setenv("AIBOT_GATEWAY_FXSYNC_API_KEY", "k123")
	t.Setenv("AIBOT_GATEWAY_FXSYNC_API_URL", "https://example.test/v6/latest")

	cfg := Config{}
	cfg.Gateway.FxSync.Enabled = true
	cfg.Gateway.FxSync.Interval = 24 * time.Hour
	cfg.Gateway.FxSync.Currencies = []string{"CNY"}
	applyEnvOverrides(&cfg)

	fx := cfg.Gateway.FxSync
	if fx.Enabled {
		t.Fatal("enabled 未被 env 覆盖为 false")
	}
	if fx.Interval != 90*time.Minute {
		t.Fatalf("interval 未被覆盖: %v", fx.Interval)
	}
	if len(fx.Currencies) != 2 || fx.Currencies[0] != "CNY" || fx.Currencies[1] != "EUR" {
		t.Fatalf("currencies 未被覆盖: %v", fx.Currencies)
	}
	if fx.APIKey != "k123" || fx.APIURL != "https://example.test/v6/latest" {
		t.Fatalf("api_key/api_url 未被覆盖: %q %q", fx.APIKey, fx.APIURL)
	}
	t.Logf("PASS env: enabled=%v interval=%v currencies=%v", fx.Enabled, fx.Interval, fx.Currencies)
}
