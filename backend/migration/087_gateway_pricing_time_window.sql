-- 分时定价：同一模型可按"每日时段"挂多条价（如 DeepSeek 错峰/高峰/平峰）。
-- daily_window_start_min / daily_window_end_min 是"北京时间(UTC+8)的当日分钟数"[0,1440)，左闭右开。
-- 两者都为 NULL = 全天兜底价(平峰基准价)；start>end 表示跨零点的时段。
-- 计价时按"请求完成时刻"落在哪个时段选对应价（DeepSeek 官方也是按完成时刻判定错峰）。
ALTER TABLE gateway_pricing_rules ADD COLUMN IF NOT EXISTS daily_window_start_min INT;
ALTER TABLE gateway_pricing_rules ADD COLUMN IF NOT EXISTS daily_window_end_min   INT;

-- 唯一约束改为把"时段"纳入区分维度：同一 provider+model 下，每个时段(含"全天兜底"这个NULL档)至多一条生效规则。
-- 用 NULLS NOT DISTINCT(PG15+) 保证两条"全天兜底"(NULL)也算冲突，避免出现两条基准价。
DROP INDEX IF EXISTS uq_gateway_pricing_active;
CREATE UNIQUE INDEX IF NOT EXISTS uq_gateway_pricing_active_window
    ON gateway_pricing_rules (provider, model, daily_window_start_min) NULLS NOT DISTINCT
    WHERE effective_to IS NULL;
