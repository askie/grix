-- agents 增加 voice_welcome_i18n 列。
-- Agent 模型在 widget/语音多语言化改动中新增了 VoiceWelcomeI18n 字段（按语言存语音开场白，
-- json: map[locale]string，见 pkg/locale.Supported），但生产走编号迁移引擎、不跑 GORM
-- AutoMigrate，此前漏写迁移导致该列未建。AgentCreate 必写此列（空 map 也编码为 '{}'），
-- AgentUpdate 携带该字段时同样写入，一旦触发即报 "column voice_welcome_i18n does not exist"
-- (SQLSTATE 42703)，新建/编辑 agent 500。这是与 091_widget_sessions_locale 同根因的姊妹缺列。
-- 幂等：IF NOT EXISTS，与 model 定义保持一致（jsonb，默认 '{}'）。

ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS voice_welcome_i18n JSONB NOT NULL DEFAULT '{}'::jsonb;
