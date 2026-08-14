-- 103: 触达任务增加可编辑文案快照。
-- 版本发布公告改为草稿制：发布事件只生成 draft 任务并预填默认中英文案，
-- 由管理后台编辑文案后手动触发发送。content 结构：
-- {"zh": {"title","body","email_subject","email_intro"}, "en": {...}}
ALTER TABLE reach_tasks
    ADD COLUMN IF NOT EXISTS content jsonb NOT NULL DEFAULT '{}';
