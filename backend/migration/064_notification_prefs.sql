-- Agent notification preferences: one row per (user, event_key) controlling
-- whether the event notifies and through which channels.
CREATE TABLE IF NOT EXISTS notification_prefs (
    user_id    BIGINT      NOT NULL,
    event_key  TEXT        NOT NULL,
    enabled    BOOLEAN     NOT NULL DEFAULT true,
    channels   JSONB       NOT NULL DEFAULT '["push"]',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, event_key)
);
