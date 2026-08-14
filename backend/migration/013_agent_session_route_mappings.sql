CREATE TABLE IF NOT EXISTS agent_session_route_mappings (
    id BIGSERIAL PRIMARY KEY,
    agent_id BIGINT NOT NULL,
    owner_id BIGINT NOT NULL,
    channel VARCHAR(32) NOT NULL,
    account_id VARCHAR(64) NOT NULL,
    route_session_key VARCHAR(191) NOT NULL,
    session_id VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_route_mapping_unique
    ON agent_session_route_mappings (agent_id, channel, account_id, route_session_key);

CREATE INDEX IF NOT EXISTS idx_agent_route_mapping_session
    ON agent_session_route_mappings (agent_id, channel, account_id, session_id);

CREATE INDEX IF NOT EXISTS idx_agent_route_mapping_owner
    ON agent_session_route_mappings (owner_id, session_id);
