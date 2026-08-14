CREATE TABLE IF NOT EXISTS agent_queued_events (
    id BIGSERIAL PRIMARY KEY,
    agent_id BIGINT NOT NULL,
    cmd VARCHAR(32) NOT NULL,
    event_key VARCHAR(191) NOT NULL,
    payload JSONB NOT NULL,
    dispatch_attempts INTEGER NOT NULL DEFAULT 0,
    dispatched_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_queued_events_event_key
    ON agent_queued_events (event_key);

CREATE INDEX IF NOT EXISTS idx_agent_queued_events_agent_created
    ON agent_queued_events (agent_id, created_at ASC, id ASC);
