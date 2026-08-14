-- 用户会话收藏表
CREATE TABLE IF NOT EXISTS user_session_favorites (
    id          BIGINT       PRIMARY KEY,
    user_id     BIGINT       NOT NULL,
    session_id  VARCHAR(50)  NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_session_favorite UNIQUE (user_id, session_id)
);

CREATE INDEX IF NOT EXISTS idx_usf_user_id ON user_session_favorites (user_id);
