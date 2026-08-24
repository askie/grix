-- 触达模板增加短信文案，供营销任务 sms 通道使用。
ALTER TABLE reach_templates ADD COLUMN IF NOT EXISTS sms_body TEXT NOT NULL DEFAULT '';
