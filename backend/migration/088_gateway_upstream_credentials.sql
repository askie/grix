-- 网关上游厂商官方凭据表：由塘主后台动态增删，不再固化在 env/secret 里。
-- api_key/api_secret 以 AES-GCM 密文落库（复用 internal/pkg/secretcrypto），明文永不入库；
-- key_hint 存明文末4位仅供后台展示。同一 provider+purpose 可挂多把启用中的凭据（推理转发轮询分流）。
-- purpose: inference=推理转发Key(DeepSeek Key / 火山 ARK Key)；reconcile=对账用凭据(火山费用中心 AK/SK)。
CREATE TABLE IF NOT EXISTS gateway_upstream_credentials (
    id              BIGINT PRIMARY KEY,
    provider        TEXT        NOT NULL,
    purpose         TEXT        NOT NULL DEFAULT 'inference',
    api_key_enc     TEXT        NOT NULL,
    api_secret_enc  TEXT        NOT NULL DEFAULT '',
    key_hint        TEXT        NOT NULL DEFAULT '',
    base_url        TEXT        NOT NULL DEFAULT '',
    region          TEXT        NOT NULL DEFAULT '',
    label           TEXT        NOT NULL DEFAULT '',
    enabled         BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 网关运行时按 (provider, purpose, enabled) 取启用凭据，建复合索引。
CREATE INDEX IF NOT EXISTS idx_gateway_upstream_cred_lookup
    ON gateway_upstream_credentials (provider, purpose, enabled);
