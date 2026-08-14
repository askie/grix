-- 语音 Agent 并发接待上限（多访客客服）
-- voice_max_concurrent_calls: 同一 agent 同一时刻最多并发接待的通话数，0=不限，默认 5。
-- 用于放开 AI 并发接待多访客后，限制瞬时并发以约束 BYOK 成本与 Provider 连接数。
ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS voice_max_concurrent_calls INT NOT NULL DEFAULT 5;
