ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS moderation_status SMALLINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS banned_reason VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS banned_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS banned_by BIGINT;

CREATE INDEX IF NOT EXISTS idx_sessions_moderation_status
    ON sessions (moderation_status);
