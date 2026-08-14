-- 链接黑名单规则表（用于点击时校验，详见 docs/architecture/35_link_safety_protection_design.md）
CREATE TABLE IF NOT EXISTS link_blocklist_rules (
    id          BIGINT PRIMARY KEY,
    kind        VARCHAR(16)  NOT NULL,                          -- domain / wildcard / regex / keyword
    value       VARCHAR(512) NOT NULL,
    severity    VARCHAR(16)  NOT NULL DEFAULT 'malicious',      -- malicious / suspicious
    source      VARCHAR(32)  NOT NULL DEFAULT 'manual',         -- manual / safebrowsing / antifraud / ...
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE,
    note        VARCHAR(256),
    hit_count   BIGINT       NOT NULL DEFAULT 0,
    last_hit_at TIMESTAMPTZ,
    created_by  BIGINT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_link_blocklist_kind        ON link_blocklist_rules (kind);
CREATE INDEX IF NOT EXISTS idx_link_blocklist_enabled     ON link_blocklist_rules (enabled);
CREATE INDEX IF NOT EXISTS idx_link_blocklist_last_hit_at ON link_blocklist_rules (last_hit_at);
-- 同一 kind+value 唯一，避免重复规则
CREATE UNIQUE INDEX IF NOT EXISTS uq_link_blocklist_kind_value
    ON link_blocklist_rules (kind, value);
