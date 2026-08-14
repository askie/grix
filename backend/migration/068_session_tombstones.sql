-- 会话墓碑表：记录人类用户失去某会话可见性的时刻（退群 / 被踢 / 群解散）。
-- /sessions/sync 据此增量返回 deleted_session_ids，让客户端离线期间错过的会话移除
-- 能增量对账清除，替代每次全量 /sessions/list 整表比对。
CREATE TABLE IF NOT EXISTS session_tombstones (
    user_id    BIGINT       NOT NULL,
    session_id VARCHAR(64)  NOT NULL,
    deleted_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, session_id)
);

CREATE INDEX IF NOT EXISTS idx_session_tombstones_user_deleted
    ON session_tombstones (user_id, deleted_at DESC);
