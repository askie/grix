CREATE TABLE IF NOT EXISTS auth_qr_login_sessions (
    session_id VARCHAR(64) PRIMARY KEY,
    qr_token_hash VARCHAR(64) NOT NULL UNIQUE,
    poll_token_hash VARCHAR(64) NOT NULL UNIQUE,
    status SMALLINT NOT NULL DEFAULT 0,
    scene SMALLINT NOT NULL DEFAULT 1,
    request_ip VARCHAR(45) NOT NULL DEFAULT '',
    request_user_agent VARCHAR(255) NOT NULL DEFAULT '',
    request_device_label VARCHAR(120) NOT NULL DEFAULT '',
    scan_user_id BIGINT,
    scanned_at TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ,
    consumed_at TIMESTAMPTZ,
    canceled_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_auth_qr_login_status_expires
    ON auth_qr_login_sessions(status, expires_at);

CREATE INDEX IF NOT EXISTS idx_auth_qr_login_scan_user_status
    ON auth_qr_login_sessions(scan_user_id, status);

