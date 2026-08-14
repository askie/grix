CREATE TABLE IF NOT EXISTS agent_api_scopes (
    agent_id BIGINT NOT NULL,
    scope VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id, scope),
    CONSTRAINT fk_agent_api_scopes_agent
        FOREIGN KEY (agent_id) REFERENCES agents(id),
    CONSTRAINT chk_agent_api_scopes_scope_non_empty
        CHECK (scope <> '')
);

CREATE INDEX IF NOT EXISTS idx_agent_api_scopes_agent_id
    ON agent_api_scopes (agent_id);
