-- App client release management (mobile + desktop update checking)

CREATE TABLE IF NOT EXISTS app_releases (
    id              BIGINT PRIMARY KEY,
    version         VARCHAR(32) NOT NULL,       -- semver, e.g. "2.4.0"
    build_number    INT NOT NULL DEFAULT 0,     -- Flutter build number, e.g. 400
    platform        VARCHAR(16) NOT NULL,       -- ios | android | macos | windows
    channel         VARCHAR(16) NOT NULL DEFAULT 'stable',  -- stable | beta
    changelog       TEXT,
    min_version     VARCHAR(32),                -- force-update threshold: below this → must update
    min_build       INT DEFAULT NULL,
    update_method   VARCHAR(16) NOT NULL DEFAULT 'download', -- download | app_store | google_play
    download_url    VARCHAR(512),
    app_store_url   VARCHAR(512),
    file_size       BIGINT DEFAULT 0,           -- bytes
    sha256          VARCHAR(64),
    metadata        JSONB DEFAULT '{}',
    status          SMALLINT NOT NULL DEFAULT 1, -- 1=draft 2=published 3=revoked 4=paused
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    published_at    TIMESTAMPTZ,
    UNIQUE(version, build_number, platform, channel)
);

CREATE INDEX IF NOT EXISTS idx_app_releases_platform_status ON app_releases(platform, channel, status);

CREATE TABLE IF NOT EXISTS app_rollout_rules (
    id                BIGINT PRIMARY KEY,
    release_id        BIGINT NOT NULL REFERENCES app_releases(id),
    rule_type         VARCHAR(32) NOT NULL,     -- user_list | percentage
    rule_value        JSONB NOT NULL,            -- {"user_ids": [...]} or {"percent": 50}
    priority          INT NOT NULL DEFAULT 0,
    status            SMALLINT NOT NULL DEFAULT 1, -- 1=active 2=paused
    created_at        TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_app_rollout_rules_release ON app_rollout_rules(release_id, status);
