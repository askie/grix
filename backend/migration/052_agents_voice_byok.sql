-- 语音大模型 BYOK：agents 表扩列，承载用户自配的语音大模型
-- voice_model:          Provider 侧模型名，如 'gpt-4o-realtime-preview'
-- voice_endpoint:       可选自定义 base URL（自建/代理）
-- voice_api_key_cipher: 用户 API key 的 AES-256-GCM 密文（base64），永不明文落库/回传
-- voice_api_key_hint:   API key 末 4 位，仅用于前端展示

ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS voice_model          VARCHAR(100),
  ADD COLUMN IF NOT EXISTS voice_endpoint        VARCHAR(255),
  ADD COLUMN IF NOT EXISTS voice_api_key_cipher TEXT,
  ADD COLUMN IF NOT EXISTS voice_api_key_hint   VARCHAR(16);
