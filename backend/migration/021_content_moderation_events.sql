CREATE TABLE IF NOT EXISTS content_moderation_events (
    id BIGSERIAL PRIMARY KEY,
    session_id VARCHAR(50) NOT NULL,
    msg_id BIGINT NOT NULL,
    sender_id BIGINT NOT NULL,
    sender_type SMALLINT NOT NULL,
    matched_keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
    recall_status VARCHAR(32) NOT NULL DEFAULT '',
    hit_count INTEGER NOT NULL DEFAULT 0,
    mute_applied BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_content_moderation_events_msg
    ON content_moderation_events (msg_id);

CREATE INDEX IF NOT EXISTS idx_content_moderation_events_sender
    ON content_moderation_events (session_id, sender_id, sender_type, created_at DESC);
