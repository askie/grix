-- Latest-trigger lookup per (session, agent, owner) for agent outbound
-- visibility fallback when an output arrives without a live event_id.

CREATE INDEX IF NOT EXISTS idx_agent_event_terminal_session_latest
    ON agent_event_terminal_ledgers (session_id, agent_id, owner_id, created_at DESC);
