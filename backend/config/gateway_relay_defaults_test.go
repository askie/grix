package config

import (
	"testing"

	"github.com/spf13/viper"
)

// 部署侧的 configmap 里可以完全没有 gateway 段（线上现状就是如此），此时这两个开关吃的
// 就是这里的默认值。默认值一旦被改回去，所有 Claude/Codex agent 会静默退回 MITM——
// 换账号计费的风险不会有任何报错提示，只能靠这条守住。
func TestGatewayRelayDefaultsWithoutConfigSection(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	setDefaults()

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}

	if !cfg.Gateway.DirectRelayEnabled {
		t.Fatalf("direct_relay_enabled 默认应为 true：Claude/Codex 的主路径是原生直连，MITM 只是回退")
	}
	if !cfg.Gateway.RelayStateEnabled {
		t.Fatalf("relay_state_enabled 默认应为 true：中转开关以服务端为真值")
	}
}

// 回滚路径必须仍然可用：环境变量能把直连关掉换回 MITM，不需要改镜像。
func TestDirectRelayCanBeDisabledByEnv(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	setDefaults()

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}

	t.Setenv("AIBOT_GATEWAY_DIRECT_RELAY_ENABLED", "false")
	applyEnvOverrides(&cfg)

	if cfg.Gateway.DirectRelayEnabled {
		t.Fatalf("AIBOT_GATEWAY_DIRECT_RELAY_ENABLED=false 必须能关掉直连（回滚路径）")
	}
}
