-- App download statistics (simplified: download_completed only)

CREATE TABLE IF NOT EXISTS app_download_reports (
    id            BIGINT PRIMARY KEY,
    user_id       BIGINT NOT NULL,
    release_id    BIGINT NOT NULL REFERENCES app_releases(id),
    from_build    INT,
    platform      VARCHAR(16) NOT NULL,
    error_msg     TEXT,
    duration_ms   INT,
    reported_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_app_dl_reports_release ON app_download_reports(release_id, reported_at DESC);
CREATE INDEX IF NOT EXISTS idx_app_dl_reports_user ON app_download_reports(user_id, reported_at DESC);
