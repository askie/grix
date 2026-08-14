CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS egg_categories (
    id VARCHAR(64) PRIMARY KEY,
    code VARCHAR(64) NOT NULL UNIQUE,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS egg_category_i18n (
    category_id VARCHAR(64) NOT NULL REFERENCES egg_categories(id) ON DELETE CASCADE,
    locale VARCHAR(16) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (category_id, locale)
);

CREATE TABLE IF NOT EXISTS eggs (
    id VARCHAR(128) PRIMARY KEY,
    category_id VARCHAR(64) NOT NULL REFERENCES egg_categories(id),
    package_type VARCHAR(32) NOT NULL CHECK (package_type IN ('persona_zip', 'skill_zip')),
    target_client_type VARCHAR(32) NOT NULL CHECK (target_client_type IN ('openclaw', 'claude')),
    default_color VARCHAR(16) NOT NULL DEFAULT '#D97706',
    default_emoji VARCHAR(16) NOT NULL DEFAULT '🌍',
    status VARCHAR(16) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'banned')),
    install_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_eggs_category_status ON eggs(category_id, status);

CREATE TABLE IF NOT EXISTS egg_i18n (
    egg_id VARCHAR(128) NOT NULL REFERENCES eggs(id) ON DELETE CASCADE,
    locale VARCHAR(16) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    vibe VARCHAR(128) NOT NULL DEFAULT '',
    search_text_normalized TEXT NOT NULL DEFAULT '',
    search_tsv tsvector,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (egg_id, locale)
);

CREATE INDEX IF NOT EXISTS idx_egg_i18n_search_tsv ON egg_i18n USING GIN(search_tsv);
CREATE INDEX IF NOT EXISTS idx_egg_i18n_search_trgm ON egg_i18n USING GIN(search_text_normalized gin_trgm_ops);

CREATE TABLE IF NOT EXISTS egg_versions (
    egg_id VARCHAR(128) NOT NULL REFERENCES eggs(id) ON DELETE CASCADE,
    version INT NOT NULL CHECK (version > 0),
    zip_url TEXT NOT NULL,
    zip_sha256 VARCHAR(128) NOT NULL,
    zip_size BIGINT NOT NULL,
    artifact_manifest_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (egg_id, version)
);

CREATE TABLE IF NOT EXISTS egg_version_i18n (
    egg_id VARCHAR(128) NOT NULL,
    version INT NOT NULL,
    locale VARCHAR(16) NOT NULL,
    version_desc TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (egg_id, version, locale),
    FOREIGN KEY (egg_id, version) REFERENCES egg_versions(egg_id, version) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS egg_installs (
    install_id VARCHAR(64) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    egg_id VARCHAR(128) NOT NULL REFERENCES eggs(id),
    version INT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'success', 'failed')),
    step VARCHAR(64) NOT NULL DEFAULT 'pending',
    target_agent_id BIGINT,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    error_msg TEXT NOT NULL DEFAULT '',
    idempotency_key VARCHAR(128) NOT NULL,
    counter_applied BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_egg_installs_user_created ON egg_installs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_egg_installs_status_created ON egg_installs(status, created_at DESC);
