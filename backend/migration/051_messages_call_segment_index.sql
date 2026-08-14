-- Phase 3: messages 表 call_segment 索引
-- msg_type=6 的消息通过 extra->>'call_id' 查询，加索引提升性能

CREATE INDEX IF NOT EXISTS idx_msg_call_segment
    ON messages ((extra->>'call_id'))
    WHERE msg_type = 6;
