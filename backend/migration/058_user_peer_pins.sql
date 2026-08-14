-- Add per-viewer human peer pin support independent of friendship.

CREATE TABLE IF NOT EXISTS user_peer_pins (
    id BIGINT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    peer_user_id BIGINT NOT NULL,
    is_pinned BOOLEAN NOT NULL DEFAULT FALSE,
    pinned_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idx_user_peer_pin UNIQUE (user_id, peer_user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_peer_pins_user_pinned
    ON user_peer_pins (user_id, is_pinned, pinned_at DESC);

INSERT INTO user_peer_pins (
    id,
    user_id,
    peer_user_id,
    is_pinned,
    pinned_at,
    created_at,
    updated_at
)
SELECT
    id,
    user_id,
    friend_id,
    is_pinned,
    pinned_at,
    created_at,
    NOW()
FROM friends
WHERE is_pinned = TRUE
ON CONFLICT (user_id, peer_user_id) DO UPDATE SET
    is_pinned = EXCLUDED.is_pinned,
    pinned_at = EXCLUDED.pinned_at,
    updated_at = NOW();
