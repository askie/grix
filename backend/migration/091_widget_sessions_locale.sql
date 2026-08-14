-- widget_sessions 增加 locale 列。
-- WidgetSession 模型在 widget 多语言化改动中新增了 Locale 字段（访客浏览器语言归一化结果，
-- 见 pkg/locale.Normalize），用于该会话后续语音通话选取开场白/system prompt 语言。
-- 但生产走编号迁移引擎、不依赖 GORM AutoMigrate，此前漏写迁移导致 /v1/widget/visitor/init
-- 在 UPDATE locale 时报 "column locale does not exist" (SQLSTATE 42703)，网页 widget 弹窗打不开。
-- 幂等：IF NOT EXISTS，与 model 定义保持一致（varchar(16) NOT NULL DEFAULT ''）。

ALTER TABLE widget_sessions
    ADD COLUMN IF NOT EXISTS locale VARCHAR(16) NOT NULL DEFAULT '';
