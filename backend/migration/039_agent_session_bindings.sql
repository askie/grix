-- +migrate
-- Unified per-session binding state for agent private-chat toolbar rendering.

CREATE TABLE IF NOT EXISTS agent_session_bindings (
    agent_id BIGINT NOT NULL,
    session_id VARCHAR(64) NOT NULL,
    provider_key VARCHAR(32) NOT NULL DEFAULT '',
    binding_id VARCHAR(255) NOT NULL DEFAULT '',
    cwd VARCHAR(2048) NOT NULL DEFAULT '',
    status VARCHAR(64) NOT NULL DEFAULT '',
    worker_status VARCHAR(64) NOT NULL DEFAULT '',
    meta_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id, session_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_session_bindings_session_id
    ON agent_session_bindings (session_id);
