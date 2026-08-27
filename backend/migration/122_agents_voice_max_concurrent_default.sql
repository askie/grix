-- 语音 Agent 并发接待上限改为用户可配置（1..10），默认值由 5 改为 2。
-- 字段此前未透出到 API，现有行的 5 均为旧默认值而非用户选择，一并对齐到新默认值。
ALTER TABLE agents
  ALTER COLUMN voice_max_concurrent_calls SET DEFAULT 2;
UPDATE agents SET voice_max_concurrent_calls = 2 WHERE voice_max_concurrent_calls = 5;
