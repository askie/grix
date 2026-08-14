-- 111: 对话审计开关的服务端持久化偏好（user + agent 维度）。
-- 缺行即关闭；一个 Agent 开启后，该 Agent 的所有会话均审计。

CREATE TABLE IF NOT EXISTS conversation_audit_prefs (
    user_id    BIGINT      NOT NULL,
    agent_id   BIGINT      NOT NULL,
    enabled    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, agent_id)
);
