-- +migrate
-- Connector-native conversation history sync state and idempotent import map.

CREATE TABLE IF NOT EXISTS agent_session_sync_states (
    id BIGSERIAL PRIMARY KEY,
    agent_id BIGINT NOT NULL,
    owner_id BIGINT NOT NULL,
    session_id VARCHAR(64) NOT NULL,
    provider_key VARCHAR(32) NOT NULL DEFAULT '',
    binding_id VARCHAR(255) NOT NULL DEFAULT '',
    sync_run_id VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'queued',
    cursor VARCHAR(2048) NOT NULL DEFAULT '',
    last_error VARCHAR(1024) NOT NULL DEFAULT '',
    imported INTEGER NOT NULL DEFAULT 0,
    meta_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_synced_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_session_sync_unique
    ON agent_session_sync_states (agent_id, session_id, provider_key, binding_id);

CREATE INDEX IF NOT EXISTS idx_agent_session_sync_lookup
    ON agent_session_sync_states (agent_id, session_id);

CREATE INDEX IF NOT EXISTS idx_agent_session_sync_status
    ON agent_session_sync_states (status);

CREATE TABLE IF NOT EXISTS agent_native_message_imports (
    id BIGSERIAL PRIMARY KEY,
    agent_id BIGINT NOT NULL,
    provider_key VARCHAR(32) NOT NULL DEFAULT '',
    binding_id VARCHAR(255) NOT NULL DEFAULT '',
    native_message_id VARCHAR(255) NOT NULL DEFAULT '',
    session_id VARCHAR(64) NOT NULL,
    msg_id BIGINT NOT NULL,
    native_created_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_native_msg_unique
    ON agent_native_message_imports (agent_id, provider_key, binding_id, native_message_id);

CREATE INDEX IF NOT EXISTS idx_agent_native_msg_session
    ON agent_native_message_imports (agent_id, session_id);

CREATE INDEX IF NOT EXISTS idx_agent_native_msg_msg_id
    ON agent_native_message_imports (msg_id);
