-- 095_payment_core.sql
-- 独立支付系统核心表：支付单 / 退款单 / 入站通知流水。
-- 金额统一 numeric(24,12)，与 gateway 记账口径一致；currency 为 ISO 4217。

-- 支付单
CREATE TABLE IF NOT EXISTS pay_order (
    id                BIGINT         PRIMARY KEY,
    biz_type          VARCHAR(32)    NOT NULL,
    biz_order_id      VARCHAR(64)    NOT NULL,
    channel           VARCHAR(32)    NOT NULL,
    amount            NUMERIC(24,12) NOT NULL,
    currency          VARCHAR(8)     NOT NULL,
    status            VARCHAR(24)    NOT NULL,
    subject           VARCHAR(256)   NOT NULL DEFAULT '',
    channel_trade_no  VARCHAR(128)   NOT NULL DEFAULT '',
    paid_at           TIMESTAMPTZ,
    created_at        TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ    NOT NULL DEFAULT now()
);
-- 下单幂等：同一业务单只对应一张支付单
CREATE UNIQUE INDEX IF NOT EXISTS uk_pay_biz ON pay_order (biz_type, biz_order_id);
-- 对账 / 通知反查
CREATE INDEX IF NOT EXISTS idx_pay_order_channel_trade ON pay_order (channel_trade_no);
CREATE INDEX IF NOT EXISTS idx_pay_order_status_created ON pay_order (status, created_at);

-- 退款单
CREATE TABLE IF NOT EXISTS pay_refund (
    id                 BIGINT         PRIMARY KEY,
    pay_order_id       BIGINT         NOT NULL,
    biz_refund_id      VARCHAR(64)    NOT NULL,
    amount             NUMERIC(24,12) NOT NULL,
    currency           VARCHAR(8)     NOT NULL,
    status             VARCHAR(24)    NOT NULL,
    channel_refund_no  VARCHAR(128)   NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ    NOT NULL DEFAULT now()
);
-- 退款幂等
CREATE UNIQUE INDEX IF NOT EXISTS uk_pay_refund_biz ON pay_refund (biz_refund_id);
CREATE INDEX IF NOT EXISTS idx_pay_refund_order ON pay_refund (pay_order_id);

-- 入站通知流水 / 去重
CREATE TABLE IF NOT EXISTS pay_notify_log (
    id                BIGINT         PRIMARY KEY,
    channel           VARCHAR(32)    NOT NULL,
    channel_trade_no  VARCHAR(128)   NOT NULL,
    raw               TEXT           NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ    NOT NULL DEFAULT now()
);
-- 通知幂等：同一渠道同一交易号只处理一次
CREATE UNIQUE INDEX IF NOT EXISTS uk_pay_notify ON pay_notify_log (channel, channel_trade_no);
