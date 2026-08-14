-- 语音大模型护栏 + 客服开关（provider_type=4）
-- voice_max_call_seconds: 单通话时长上限(秒)，0=不限
-- voice_daily_call_limit: 每日通话次数上限，0=不限
-- voice_allow_visitor:    是否开放访客/客服通话（Phase C 使用）
ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS voice_max_call_seconds INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS voice_daily_call_limit INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS voice_allow_visitor BOOLEAN NOT NULL DEFAULT FALSE;
