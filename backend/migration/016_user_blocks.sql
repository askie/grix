CREATE TABLE IF NOT EXISTS user_blocks (
    id BIGINT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    blocked_user_id BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_blocks_user_blocked
    ON user_blocks(user_id, blocked_user_id);
