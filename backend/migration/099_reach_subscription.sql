CREATE TABLE IF NOT EXISTS reach_subscriptions (
    user_id     BIGINT PRIMARY KEY,
    subscribed  BOOLEAN NOT NULL DEFAULT TRUE,
    topics      JSONB NOT NULL DEFAULT '[]',
    unsub_token VARCHAR(64) NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_reach_unsub_token ON reach_subscriptions (unsub_token) WHERE unsub_token != '';
