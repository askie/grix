-- Agent WS 连接安全（阶段0）：连接日志 + IP 规则（黑/白名单）。
-- 背景：/v1/agent-api 此前不记录客户端真实 IP，也没有 IP 维度的封禁能力。
-- agent_connection_logs：每次 agent WS 上线一条，断开时回填 disconnected_at/reason。
-- agent_ip_rules：按 agent（agent_id=0 表示全局）配置封禁/白名单规则；
--   signature 为服务端 HMAC 签名，加载时验签，防止直改数据库篡改规则。
-- 幂等：IF NOT EXISTS。

CREATE TABLE IF NOT EXISTS agent_connection_logs (
    id BIGINT PRIMARY KEY,
    agent_id BIGINT NOT NULL,
    owner_id BIGINT NOT NULL DEFAULT 0,
    is_primary BOOLEAN NOT NULL DEFAULT TRUE,
    client_type VARCHAR(32) NOT NULL DEFAULT '',
    client_ip VARCHAR(45) NOT NULL DEFAULT '',
    ip_location VARCHAR(128) NOT NULL DEFAULT '',
    geo_changed BOOLEAN NOT NULL DEFAULT FALSE,
    allowlist_miss BOOLEAN NOT NULL DEFAULT FALSE,
    node_id VARCHAR(64) NOT NULL DEFAULT '',
    connected_at TIMESTAMPTZ NOT NULL,
    disconnected_at TIMESTAMPTZ,
    disconnect_reason VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_conn_logs_agent_time
    ON agent_connection_logs (agent_id, connected_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_conn_logs_ip
    ON agent_connection_logs (client_ip);

CREATE TABLE IF NOT EXISTS agent_ip_rules (
    id BIGINT PRIMARY KEY,
    agent_id BIGINT NOT NULL DEFAULT 0,
    rule_type VARCHAR(16) NOT NULL,
    ip_cidr VARCHAR(64) NOT NULL,
    remark VARCHAR(255) NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    signature VARCHAR(88) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_ip_rules_scope
    ON agent_ip_rules (agent_id, rule_type, ip_cidr);
