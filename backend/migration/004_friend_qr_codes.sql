CREATE TABLE IF NOT EXISTS friend_qr_codes (
    user_id BIGINT PRIMARY KEY,
    code VARCHAR(64) NOT NULL UNIQUE,
    rotated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_friend_qr_codes_code ON friend_qr_codes(code);
