CREATE TABLE IF NOT EXISTS group_qr_codes (
    session_id VARCHAR(50) PRIMARY KEY,
    code VARCHAR(64) NOT NULL UNIQUE,
    creator_user_id BIGINT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    rotated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_group_qr_codes_code ON group_qr_codes(code);
CREATE INDEX IF NOT EXISTS idx_group_qr_codes_expires_at ON group_qr_codes(expires_at);
