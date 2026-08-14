-- 语音自动托管（电话秘书）：用户级默认语音托管 agent（type=4 语音大模型）
-- 与文字的 auto_delegate_agent_id 独立；为空表示未启用语音自动托管
ALTER TABLE user_settings
  ADD COLUMN IF NOT EXISTS voice_auto_delegate_agent_id BIGINT;
