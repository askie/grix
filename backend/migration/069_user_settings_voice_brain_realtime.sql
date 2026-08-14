-- 语音大脑工作模式开关：true=豆包实时互动(端到端+502背景注入)，false=STT+TTS 念稿兜底。
-- 默认 true（实时互动）。仅作用于语音大脑(owner 主动呼出)，与客服(widget)互不影响。
ALTER TABLE user_settings
    ADD COLUMN IF NOT EXISTS voice_brain_realtime BOOLEAN NOT NULL DEFAULT TRUE;
