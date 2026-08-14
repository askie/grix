-- 按输入长度分档定价：部分厂商（火山豆包 2.0 系列）按"单次请求的输入 token 数"分档收费，
-- 网关一口价在长上下文档会亏钱。input_tier_start_tokens / input_tier_end_tokens 是
-- 输入 token 数区间 [start,end)，左闭右开；两者都为 NULL = 不按输入长度分档。
-- 计价时按"该笔请求 缓存命中+未命中 输入 token 数"命中的档选价；
-- 档外的输入落全天兜底价——兜底价按最高档配置即可保证任何输入长度都不亏。
ALTER TABLE gateway_pricing_rules ADD COLUMN IF NOT EXISTS input_tier_start_tokens INT;
ALTER TABLE gateway_pricing_rules ADD COLUMN IF NOT EXISTS input_tier_end_tokens   INT;

-- 唯一约束把"输入档"纳入区分维度：同一 provider+model+时段+输入档 至多一条生效规则。
-- 沿用 NULLS NOT DISTINCT(PG15+)：NULL 档（不分档）彼此也算冲突，避免出现两条兜底价。
DROP INDEX IF EXISTS uq_gateway_pricing_active_window;
CREATE UNIQUE INDEX IF NOT EXISTS uq_gateway_pricing_active_window_tier
    ON gateway_pricing_rules (provider, model, daily_window_start_min, input_tier_start_tokens) NULLS NOT DISTINCT
    WHERE effective_to IS NULL;
