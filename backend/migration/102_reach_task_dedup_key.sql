-- reach_tasks 加任务级去重键，修复版本发布重复触达。
--
-- 背景：一次发版会给 iOS、macOS 各建一条 AppRelease 记录并分别 publish，
-- 每条记录都发一个 reach 事件，消费端对每个事件新建一个 ReachTask 全量群发，
-- 结果同版本用户收到多轮"新版本已发布"站内信+邮件（2026-07-10 实发事故）。
--
-- 修复：任务创建即按 dedup_key 去重（app_release_published:<channel>:<version>，
-- 带通道防止 beta 占键吞掉同版本 stable 的正式通知），同通道同版本只允许存在
-- 一个群发任务，后续事件 ON CONFLICT DO NOTHING 直接跳过。该键同时兜住 NATS
-- 重投递（AckWait 超时 MaxDeliver=3）造成的重复。
--
-- 列可空：marketing / ab-test 等人工任务不设去重键（NULL 不参与唯一约束），
-- 历史行保持 NULL 不受影响。

ALTER TABLE reach_tasks ADD COLUMN IF NOT EXISTS dedup_key VARCHAR(128);

CREATE UNIQUE INDEX IF NOT EXISTS uq_reach_tasks_dedup_key
    ON reach_tasks (dedup_key);
