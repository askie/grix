-- Agent 共享表：把一个 agent 共享给别的账户使用（方案：多连接物理隔离）。
-- connector 用「主人 api_key + shared_owner_id」为被共享者建立独立 WS 连接；
-- 后端校验主人 api_key + 本表存在 (agent_id, shared_to) 有效记录，把该连接身份认定为 shared_to。
-- agent 主人(owner_id)与被共享者(shared_to)各走各的连接，Server 数据/手机端按各自身份隔离。
CREATE TABLE IF NOT EXISTS agent_shares (
    id BIGINT PRIMARY KEY,
    agent_id BIGINT NOT NULL,
    owner_id BIGINT NOT NULL,
    shared_to BIGINT NOT NULL,
    status SMALLINT NOT NULL DEFAULT 1, -- 1=有效 2=撤销
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 同一 agent 对同一被共享者只保留一条有效共享（也用于握手校验按 (agent_id, shared_to) 查）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_shares_agent_to_active
    ON agent_shares (agent_id, shared_to) WHERE status = 1;

-- 列「分享给我的 agent」：按被共享者查。
CREATE INDEX IF NOT EXISTS idx_agent_shares_shared_to
    ON agent_shares (shared_to) WHERE status = 1;
