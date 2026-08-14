-- 功能开关白名单控制体系
-- feature_gates: 定义功能开关（key + 状态）
-- feature_gate_users: 白名单用户关联

CREATE TABLE IF NOT EXISTS feature_gates (
    key VARCHAR(64) PRIMARY KEY,
    display_name VARCHAR(128) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'disabled' CHECK (status IN ('disabled', 'whitelist', 'enabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS feature_gate_users (
    feature_key VARCHAR(64) NOT NULL REFERENCES feature_gates(key) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (feature_key, user_id)
);

CREATE INDEX IF NOT EXISTS idx_feature_gate_users_user_id ON feature_gate_users(user_id);

-- 初始种子数据：语音相关功能默认关闭
INSERT INTO feature_gates (key, display_name, status) VALUES
    ('voice_call', '语音通话', 'disabled'),
    ('voice_delegate', '语音托管', 'disabled'),
    ('agent_voice_llm', 'Agent 语音大模型', 'disabled')
ON CONFLICT (key) DO NOTHING;
