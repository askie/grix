-- Per-viewer mute for a human/agent peer, independent of session-level mute.
-- Private conversation list "close notifications" is this flag, so later
-- threads with the same peer inherit it instead of copying session.is_muted.

CREATE TABLE IF NOT EXISTS user_peer_mutes (
    id BIGINT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    peer_user_id BIGINT NOT NULL,
    is_muted BOOLEAN NOT NULL DEFAULT FALSE,
    muted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idx_user_peer_mute UNIQUE (user_id, peer_user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_peer_mutes_user_muted
    ON user_peer_mutes (user_id, is_muted);
