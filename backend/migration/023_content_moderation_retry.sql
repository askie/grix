ALTER TABLE content_moderation_events
    ADD COLUMN IF NOT EXISTS recall_attempts INTEGER NOT NULL DEFAULT 0;

ALTER TABLE content_moderation_events
    ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ;

ALTER TABLE content_moderation_events
    ALTER COLUMN recall_status SET DEFAULT 'pending';

CREATE INDEX IF NOT EXISTS idx_content_moderation_events_retry
    ON content_moderation_events (recall_status, next_retry_at);
