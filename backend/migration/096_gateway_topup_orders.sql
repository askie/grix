-- 096_gateway_topup_orders.sql
-- 用户自助充值单：把支付系统的支付单 ↔ 用户/钱包/充值金额接起来。
-- 余额、充值流水、消费扣减继续复用 gateway 现有表；这里只加"充值发起"单据，
-- 以及给充值流水补一层支付幂等约束。

-- 充值单：用户发起一笔充值时创建，支付成功后据此入账。
CREATE TABLE IF NOT EXISTS gateway_topup_orders (
    id               BIGINT         PRIMARY KEY,
    wallet_id        BIGINT         NOT NULL REFERENCES gateway_wallets(id),
    owner_id         BIGINT         NOT NULL,
    source_currency  VARCHAR(8)     NOT NULL,
    source_amount    NUMERIC(24,12) NOT NULL,
    channel          VARCHAR(32)    NOT NULL,
    status           VARCHAR(24)    NOT NULL,          -- PENDING / PAID / CLOSED / FAILED
    pay_order_id     BIGINT,                            -- 支付系统支付单号（下单后回填）
    topup_record_id  BIGINT,                            -- 入账后回填 gateway_topup_records.id
    created_at       TIMESTAMPTZ    NOT NULL DEFAULT now(),
    paid_at          TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ    NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_gateway_topup_orders_owner
    ON gateway_topup_orders(owner_id, created_at DESC);
-- 一张支付单只对应一张充值单
CREATE UNIQUE INDEX IF NOT EXISTS uk_gateway_topup_orders_pay
    ON gateway_topup_orders(pay_order_id) WHERE pay_order_id IS NOT NULL;

-- 充值入账幂等：同一支付引用只入账一次。历史手工充值 payment_reference 可空，
-- 用部分唯一索引，只约束非空引用。
CREATE UNIQUE INDEX IF NOT EXISTS uk_gateway_topup_records_ref
    ON gateway_topup_records(payment_reference)
    WHERE payment_reference IS NOT NULL AND payment_reference <> '';
