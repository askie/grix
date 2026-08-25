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

	// 生产实际走的是完整 Load 链路：默认值 → unmarshal → applyEnvOverrides。线上并没有
	// 设这两个环境变量，所以必须确认"环境变量缺失"这一支不会把默认值覆盖掉——只断言
	// unmarshal 之后的值，会漏掉 override 把 bool 写成零值这类改动。
	applyEnvOverrides(&cfg)

	if !cfg.Gateway.DirectRelayEnabled {
		t.Fatalf("环境变量缺失时 applyEnvOverrides 不得覆盖 direct_relay_enabled 的默认值")
	}
	if !cfg.Gateway.RelayStateEnabled {
		t.Fatalf("环境变量缺失时 applyEnvOverrides 不得覆盖 relay_state_enabled 的默认值")
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
