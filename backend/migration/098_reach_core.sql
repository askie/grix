CREATE TABLE IF NOT EXISTS reach_tasks (
    id           BIGINT PRIMARY KEY,
    kind         VARCHAR(32) NOT NULL,
    event_key    VARCHAR(64) NOT NULL DEFAULT '',
    template_id  BIGINT NOT NULL DEFAULT 0,
    channels     JSONB NOT NULL DEFAULT '[]',
    audience     JSONB NOT NULL DEFAULT '{}',
    status       VARCHAR(24) NOT NULL DEFAULT 'draft',
    scheduled_at TIMESTAMPTZ,
    stats        JSONB NOT NULL DEFAULT '{}',
    region       VARCHAR(16) NOT NULL DEFAULT '',
    created_by   BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS reach_templates (
    id         BIGINT PRIMARY KEY,
    name       VARCHAR(128) NOT NULL DEFAULT '',
    title      VARCHAR(256) NOT NULL DEFAULT '',
    in_app_body TEXT NOT NULL DEFAULT '',
    push_body   TEXT NOT NULL DEFAULT '',
    email_html  TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS reach_send_logs (
    id         BIGINT PRIMARY KEY,
    task_id    BIGINT NOT NULL,
    user_id    BIGINT NOT NULL,
    channel    VARCHAR(32) NOT NULL,
    status     VARCHAR(16) NOT NULL DEFAULT 'pending',
    error      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_reach_send_dedup
    ON reach_send_logs (task_id, user_id, channel);
