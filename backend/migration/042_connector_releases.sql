-- Connector auto-upgrade tables

CREATE TABLE IF NOT EXISTS connector_releases (
    id              BIGINT PRIMARY KEY,
    version         VARCHAR(32) NOT NULL,
    channel         VARCHAR(16) NOT NULL DEFAULT 'stable',
    changelog       TEXT,
    min_version     VARCHAR(32),
    npm_package     VARCHAR(128) NOT NULL DEFAULT 'grix-connector',
    npm_tag         VARCHAR(32) NOT NULL DEFAULT 'latest',
    force           BOOLEAN NOT NULL DEFAULT false,
    metadata        JSONB DEFAULT '{}',
    status          SMALLINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    published_at    TIMESTAMPTZ,
    UNIQUE(version, channel)
);

CREATE TABLE IF NOT EXISTS connector_rollout_rules (
    id                BIGINT PRIMARY KEY,
    release_id        BIGINT NOT NULL REFERENCES connector_releases(id),
    rule_type         VARCHAR(32) NOT NULL,
    rule_value        JSONB NOT NULL,
    priority          INT NOT NULL DEFAULT 0,
    status            SMALLINT NOT NULL DEFAULT 1,
    auto_pause_config JSONB DEFAULT '{}',
    created_at        TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rollout_rules_release ON connector_rollout_rules(release_id, status);

CREATE TABLE IF NOT EXISTS connector_upgrade_reports (
    id              BIGINT PRIMARY KEY,
    agent_id        BIGINT NOT NULL,
    from_version    VARCHAR(32) NOT NULL,
    to_version      VARCHAR(32) NOT NULL,
    status          VARCHAR(16) NOT NULL,
    error_code      VARCHAR(32),
    error_msg       TEXT,
    upgrade_log     TEXT,
    crash_count     INT DEFAULT 0,
    npm_version     VARCHAR(16),
    node_version    VARCHAR(16),
    disk_free_mb    INT,
    platform        VARCHAR(16),
    arch            VARCHAR(16),
    duration_ms     INT,
    reported_at     TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_upgrade_reports_agent ON connector_upgrade_reports(agent_id, reported_at DESC);
