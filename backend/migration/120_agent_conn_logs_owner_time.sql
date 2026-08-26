-- 「沉默用户触达」人群口径要按 owner 反查最近 N 天有没有 agent 连接过：
--   NOT EXISTS (SELECT 1 FROM agent_connection_logs l WHERE l.owner_id = users.id AND l.connected_at >= cutoff)
-- 已有索引是 (agent_id, connected_at DESC)，按 owner_id 查只能全表扫，这里补 owner 维度的索引。

CREATE INDEX IF NOT EXISTS idx_agent_conn_logs_owner_time
    ON agent_connection_logs (owner_id, connected_at DESC);
