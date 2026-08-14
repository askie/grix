-- 给 agent_queued_events 补 owner_id 维度，让 revoke/edit 离线事件按
-- (agent_id, owner_id) 隔离 drain，避免跨 owner 串数据：
-- 例如 A 把 agent 共享给 B 后，B 撤回自己跟 agent 的私聊消息时，
-- 旧逻辑会让 A 重连 drain 时也收到 B 的 revoke 事件。
ALTER TABLE agent_queued_events
    ADD COLUMN IF NOT EXISTS owner_id BIGINT NOT NULL DEFAULT 0;

-- drain 时按 (agent_id, owner_id, created_at) 拉取，需要复合索引。
-- owner_id=0 兼容历史数据与主连接（事件无明确 owner 时仍按 agent 投递）。
CREATE INDEX IF NOT EXISTS idx_agent_queued_events_agent_owner_created
    ON agent_queued_events (agent_id, owner_id, created_at);
