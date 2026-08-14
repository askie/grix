-- 语音大脑通话新增文字 agent ID 字段，用于 callsegment 正确路由转写消息。
-- 对于普通语音通话此字段为 NULL，不影响现有逻辑。
ALTER TABLE call_records ADD COLUMN IF NOT EXISTS text_agent_id BIGINT;
