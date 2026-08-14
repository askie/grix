-- 会话级 Agent 运行状态表：每个会话对每个 owner 只有一条记录，
-- 多个 run 事件归并后反映当前最新状态。
CREATE TABLE IF NOT EXISTS session_agent_states (
    session_id   VARCHAR(64)  NOT NULL,
    owner_id     BIGINT       NOT NULL,
    agent_id     BIGINT       NOT NULL,
    state        VARCHAR(32)  NOT NULL DEFAULT 'idle',
    attention    VARCHAR(32)  NOT NULL DEFAULT '',
    last_run_id  VARCHAR(192) NOT NULL DEFAULT '',
    stop_reason  VARCHAR(255) NOT NULL DEFAULT '',
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    notified_at  TIMESTAMPTZ,
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (session_id, owner_id)
);

CREATE INDEX IF NOT EXISTS idx_session_agent_states_owner
    ON session_agent_states(owner_id, agent_id, updated_at DESC);
