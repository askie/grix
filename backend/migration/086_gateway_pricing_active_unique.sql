-- 硬保证：同一 provider+model 至多只有一条"当前生效"(effective_to IS NULL)的价目规则。
-- 防止人工建价与对账自动调价并发交错时，两方各自 INSERT 出一条 effective_to=NULL 的僵尸行。
-- 有了这个部分唯一索引，并发中后提交的一方会因唯一冲突失败（对账侧记错误日志、人工侧收到报错可重试），
-- 而不会留下无法收口的重复生效规则。
CREATE UNIQUE INDEX IF NOT EXISTS uq_gateway_pricing_active
    ON gateway_pricing_rules (provider, model)
    WHERE effective_to IS NULL;
