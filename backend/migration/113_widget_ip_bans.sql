-- Widget 访客 IP 封禁：按 owner 维度封禁访客来源 IP，对该 owner 名下所有 widget 站点生效。
-- 背景：此前只能按 visitor_key 封禁单个访客会话（widget_sessions.status=3），
--   访客更换 visitor_key 即可绕过；IP 封禁与 visitor_key 封禁相互独立，可单独解除。
-- widget_ip_bans：ban 访客时自动把该会话最近 init IP 写入；默认 7 天过期（expires_at），
--   重复封禁同一 (owner, ip) 走 upsert 刷新过期时间；
--   signature 为服务端 HMAC 签名（同 agent_ip_rules 先例），加载时验签，
--   防止直改数据库篡改规则。
-- widget_sessions.last_init_ip：记录访客最近一次 init 的完整 IP（此前只有 /24 前缀
--   last_init_ip_prefix），ban 访客时据此自动写入 IP 封禁。
-- 幂等：IF NOT EXISTS。

CREATE TABLE IF NOT EXISTS widget_ip_bans (
    id BIGINT PRIMARY KEY,
    owner_user_id BIGINT NOT NULL,
    ip_cidr VARCHAR(64) NOT NULL,
    reason VARCHAR(255) NOT NULL DEFAULT '',
    source_session_id VARCHAR(64) NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    signature VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_widget_ip_bans_owner_ip
    ON widget_ip_bans (owner_user_id, ip_cidr);
CREATE INDEX IF NOT EXISTS idx_widget_ip_bans_owner_expires
    ON widget_ip_bans (owner_user_id, expires_at);

ALTER TABLE widget_sessions ADD COLUMN IF NOT EXISTS last_init_ip VARCHAR(64) NOT NULL DEFAULT '';
