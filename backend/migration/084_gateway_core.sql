-- 大模型计费网关核心表：虚拟Key + 记账货币(USD)钱包 + 消费/充值流水 + 价目表 + 汇率参考 + 对账报告
-- 详见 docs/architecture/36_llm_billing_gateway_design.md
-- 金额字段一律 NUMERIC(24,12)，不用浮点、不用预设刻度的整数，保留厂商官方报价原始小数位。

CREATE TABLE IF NOT EXISTS gateway_wallets (
    id              BIGINT PRIMARY KEY,
    owner_id        BIGINT NOT NULL UNIQUE,
    balance         NUMERIC(24, 12) NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS gateway_virtual_keys (
    id              BIGINT PRIMARY KEY,
    wallet_id       BIGINT NOT NULL REFERENCES gateway_wallets(id),
    key_hash        VARCHAR(64) NOT NULL UNIQUE,
    key_hint        VARCHAR(16) NOT NULL,
    label           VARCHAR(64),
    status          VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_gateway_virtual_keys_wallet ON gateway_virtual_keys(wallet_id);

-- 对账报告先建（价目表的自动调价审计字段要引用它）
CREATE TABLE IF NOT EXISTS gateway_reconciliation_reports (
    id                    BIGINT PRIMARY KEY,
    provider              VARCHAR(32) NOT NULL,
    window_start          TIMESTAMPTZ NOT NULL,
    window_end            TIMESTAMPTZ NOT NULL,
    vendor_actual_cost    NUMERIC(24, 12) NOT NULL,
    ledger_expected_cost  NUMERIC(24, 12) NOT NULL,
    diff                  NUMERIC(24, 12) NOT NULL,
    diff_ratio            NUMERIC(10, 6),
    status                VARCHAR(16) NOT NULL,
    auto_adjusted         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_gateway_reconciliation_provider_window
    ON gateway_reconciliation_reports(provider, window_start DESC);

CREATE TABLE IF NOT EXISTS gateway_pricing_rules (
    id                          BIGINT PRIMARY KEY,
    provider                    VARCHAR(32) NOT NULL,
    model                       VARCHAR(64) NOT NULL,
    cached_input_price_per_m    NUMERIC(24, 12) NOT NULL,
    uncached_input_price_per_m  NUMERIC(24, 12) NOT NULL,
    output_price_per_m         NUMERIC(24, 12) NOT NULL,
    source_currency             VARCHAR(8) NOT NULL,
    fx_rate_used                NUMERIC(24, 12),
    created_by                  VARCHAR(16) NOT NULL DEFAULT 'manual',
    triggered_by_report_id       BIGINT REFERENCES gateway_reconciliation_reports(id),
    effective_from              TIMESTAMPTZ NOT NULL,
    effective_to                TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_gateway_pricing_lookup
    ON gateway_pricing_rules(provider, model, effective_from);

CREATE TABLE IF NOT EXISTS gateway_ledger_entries (
    id                  BIGINT PRIMARY KEY,
    wallet_id           BIGINT NOT NULL REFERENCES gateway_wallets(id),
    virtual_key_id      BIGINT NOT NULL REFERENCES gateway_virtual_keys(id),
    request_id          VARCHAR(64) NOT NULL,
    provider            VARCHAR(32) NOT NULL,
    model               VARCHAR(64) NOT NULL,
    prompt_tokens       INT NOT NULL DEFAULT 0,
    cached_tokens       INT NOT NULL DEFAULT 0,
    completion_tokens   INT NOT NULL DEFAULT 0,
    reasoning_tokens    INT NOT NULL DEFAULT 0,
    cost                NUMERIC(24, 12) NOT NULL DEFAULT 0,
    balance_after       NUMERIC(24, 12),
    status              VARCHAR(16) NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_gateway_ledger_wallet_created
    ON gateway_ledger_entries(wallet_id, created_at DESC);

CREATE TABLE IF NOT EXISTS gateway_topup_records (
    id                  BIGINT PRIMARY KEY,
    wallet_id           BIGINT NOT NULL REFERENCES gateway_wallets(id),
    source_currency     VARCHAR(8) NOT NULL,
    source_amount       NUMERIC(24, 12) NOT NULL,
    fx_rate_used        NUMERIC(24, 12) NOT NULL,
    credited_amount     NUMERIC(24, 12) NOT NULL,
    payment_channel     VARCHAR(32),
    payment_reference   VARCHAR(128),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_gateway_topup_wallet_created
    ON gateway_topup_records(wallet_id, created_at DESC);

CREATE TABLE IF NOT EXISTS gateway_fx_rates (
    id              BIGINT PRIMARY KEY,
    from_currency   VARCHAR(8) NOT NULL,
    to_currency     VARCHAR(8) NOT NULL DEFAULT 'USD',
    rate            NUMERIC(24, 12) NOT NULL,
    effective_from  TIMESTAMPTZ NOT NULL,
    source          VARCHAR(32) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_gateway_fx_rates_lookup
    ON gateway_fx_rates(from_currency, to_currency, effective_from DESC);
