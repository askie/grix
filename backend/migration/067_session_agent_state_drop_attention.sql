-- 会话级 Agent 状态收口为单一互斥值：删除独立的 attention 维度。
-- approval/question 并入 state（waiting_approval/waiting_question），
-- review/failure 并入 completed/failed，使用方仅凭一个 state 即可决策。
-- 历史遗留的僵尸 running 行由 ws 启动自愈扫描翻面，不在迁移处理。
ALTER TABLE session_agent_states DROP COLUMN IF EXISTS attention;
