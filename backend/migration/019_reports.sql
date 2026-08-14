CREATE TABLE IF NOT EXISTS reports (
    id BIGSERIAL PRIMARY KEY,
    reporter_user_id BIGINT NOT NULL,
    target_type SMALLINT NOT NULL,
    target_user_id BIGINT NOT NULL DEFAULT 0,
    target_session_id VARCHAR(50) NOT NULL DEFAULT '',
    source_session_id VARCHAR(50) NOT NULL DEFAULT '',
    reason_code VARCHAR(32) NOT NULL,
    description VARCHAR(500) NOT NULL DEFAULT '',
    status SMALLINT NOT NULL DEFAULT 1,
    resolution SMALLINT NOT NULL DEFAULT 0,
    assigned_admin_id BIGINT,
    resolved_admin_id BIGINT,
    resolved_note VARCHAR(500) NOT NULL DEFAULT '',
    reporter_snapshot JSONB NOT NULL,
    target_snapshot JSONB NOT NULL,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_reports_target_type CHECK (target_type IN (1, 2)),
    CONSTRAINT chk_reports_status CHECK (status IN (1, 2, 3))
);

CREATE INDEX IF NOT EXISTS idx_reports_reporter_user_id
    ON reports (reporter_user_id);

CREATE INDEX IF NOT EXISTS idx_reports_target_user_id
    ON reports (target_user_id);

CREATE INDEX IF NOT EXISTS idx_reports_target_session_id
    ON reports (target_session_id);

CREATE INDEX IF NOT EXISTS idx_reports_status_created_at
    ON reports (status, created_at DESC);

CREATE TABLE IF NOT EXISTS report_attachments (
    id BIGSERIAL PRIMARY KEY,
    report_id BIGINT NOT NULL,
    slot_no SMALLINT NOT NULL,
    object_key VARCHAR(512) NOT NULL,
    mime_type VARCHAR(64) NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    sha256 VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_report_attachments_report
        FOREIGN KEY (report_id) REFERENCES reports(id) ON DELETE CASCADE,
    CONSTRAINT uq_report_attachments_report_slot UNIQUE (report_id, slot_no)
);

CREATE INDEX IF NOT EXISTS idx_report_attachments_report_id
    ON report_attachments (report_id);

CREATE TABLE IF NOT EXISTS report_action_logs (
    id BIGSERIAL PRIMARY KEY,
    report_id BIGINT NOT NULL,
    admin_id BIGINT NOT NULL,
    action VARCHAR(32) NOT NULL,
    detail JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_report_action_logs_report
        FOREIGN KEY (report_id) REFERENCES reports(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_report_action_logs_report_id
    ON report_action_logs (report_id, created_at ASC);
