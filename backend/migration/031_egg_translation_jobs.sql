CREATE TABLE IF NOT EXISTS egg_translation_jobs (
    id BIGSERIAL PRIMARY KEY,
    job_type VARCHAR(32) NOT NULL,
    egg_id VARCHAR(128) NOT NULL DEFAULT '',
    version INT NOT NULL DEFAULT 0,
    source_locale VARCHAR(16) NOT NULL DEFAULT '',
    target_locale VARCHAR(16) NOT NULL,
    status SMALLINT NOT NULL DEFAULT 0,
    attempt_count INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_type, egg_id, version, target_locale)
);

CREATE INDEX IF NOT EXISTS idx_egg_translation_jobs_status_retry
    ON egg_translation_jobs(status, next_retry_at);
