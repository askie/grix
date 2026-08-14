-- Phase 2: agents 表扩列，支持语音媒体能力
-- media_capability: 'text' | 'voice' | 'multimodal'
-- voice_provider:   语音 LLM Provider 标识，如 'doubao_realtime'
-- voice_id:         Provider 侧的音色/模型 ID

ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS media_capability VARCHAR(16) NOT NULL DEFAULT 'text',
  ADD COLUMN IF NOT EXISTS voice_provider   VARCHAR(32),
  ADD COLUMN IF NOT EXISTS voice_id         VARCHAR(64);
