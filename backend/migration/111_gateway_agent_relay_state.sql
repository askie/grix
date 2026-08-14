-- 111_gateway_agent_relay_state.sql
-- "Grix中转"按 Agent 开关的真值上移服务端（docs/frontend/gateway_relay_mobile_design.md §2.2）。
--
-- 为什么不挂在 gateway_virtual_keys（凭证生命周期表，revoke 会丢状态）也不塞进
-- agents.config（缺期望/实际双态扩展空间）：开关的 desired（用户要什么）与
-- applied（connector 最近一次回执的实际态）是两份独立数据，需要独立的一行来承载。
--
-- 主键 agent_id：agent 全库唯一、单 owner，wallet 通过 owner 间接关联；
-- wallet_id 仍落库，供按钱包批量取回（GET /v1/gateway/agents）与对账。
-- relay_model 是唯一权威的期望模型；gateway_virtual_keys.relay_model 只是最近一次签发快照。
CREATE TABLE IF NOT EXISTS gateway_agent_relay_state (
    agent_id      BIGINT       PRIMARY KEY REFERENCES agents(id),
    wallet_id     BIGINT       NOT NULL REFERENCES gateway_wallets(id),
    -- desired：期望开关
    enabled       BOOLEAN      NOT NULL DEFAULT FALSE,
    -- desired：唯一权威的期望模型，空 = 走网关模型映射/兜底
    relay_model   VARCHAR(128) NOT NULL DEFAULT '',
    -- 乐观锁：多端并发写时 +1，expected_revision 不匹配拒绝
    revision      BIGINT       NOT NULL DEFAULT 1,
    -- actual：connector 最近一次有效回执的实际态（M2 WS 协议写回）
    applied       BOOLEAN      NOT NULL DEFAULT FALSE,
    -- 最近一次有效回执时间
    applied_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- GET /v1/gateway/agents 按钱包批量取回该用户所有 Agent 的开关状态。
CREATE INDEX IF NOT EXISTS idx_gateway_agent_relay_state_wallet ON gateway_agent_relay_state(wallet_id);
