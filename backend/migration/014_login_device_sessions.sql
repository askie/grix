CREATE TABLE IF NOT EXISTS login_device_sessions (
    session_id VARCHAR(64) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    device_id VARCHAR(100) NOT NULL,
    platform VARCHAR(32) NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_login_device_sessions_user_revoked
    ON login_device_sessions(user_id, revoked_at);

CREATE INDEX IF NOT EXISTS idx_login_device_sessions_device_id
    ON login_device_sessions(device_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_login_device_sessions_user_device_active
    ON login_device_sessions(user_id, device_id)
    WHERE revoked_at IS NULL;
