CREATE TABLE IF NOT EXISTS gemini_session_contexts (
    agent_id BIGINT NOT NULL,
    session_id VARCHAR(64) NOT NULL,
    cwd VARCHAR(2048) NOT NULL,
    mode_id VARCHAR(191) NOT NULL DEFAULT '',
    model_id VARCHAR(191) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id, session_id)
);

CREATE INDEX IF NOT EXISTS idx_gemini_session_contexts_updated_at
    ON gemini_session_contexts (updated_at DESC);
