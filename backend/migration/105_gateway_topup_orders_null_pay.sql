-- 105_gateway_topup_orders_null_pay.sql
-- 修复：充值下单从第二张「未绑定支付单」的单据起必然 500。
--
-- 096 的表设计里 pay_order_id 未绑定时应为 NULL，唯一索引 uk_gateway_topup_orders_pay
-- 也只约束非空值。但 Go 模型把它定义成非指针 int64，GORM 插入时写的是 0 而不是 NULL；
-- 0 不是 NULL，照样进部分唯一索引。于是第一张未绑定单占住 0 这个槽位后，
-- 之后任何人新建充值单都撞 uk_gateway_topup_orders_pay → 500 → 前端「请联系管理员」。
--
-- 模型已改为可空指针（未绑定落 NULL）。这里把历史遗留的 0 值刷成 NULL，
-- 让被占死的索引槽位释放。topup_record_id 同源（未入账应为 NULL），一并归位。
UPDATE gateway_topup_orders SET pay_order_id = NULL WHERE pay_order_id = 0;
UPDATE gateway_topup_orders SET topup_record_id = NULL WHERE topup_record_id = 0;
