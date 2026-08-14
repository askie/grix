-- 手机号无密码短信登录注册：users 增 phone 字段 + 身份提供商绑定表
-- 详见 docs/architecture/36_phone_sms_auth_design.md

-- 1. users 增可空手机号字段（E.164 格式）与国家区号
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_e164    VARCHAR(20);
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_country VARCHAR(8);

-- 2. email 改为可空（手机号注册用户无 email）。
--    NULL 不会撞唯一索引（PG 默认 NULLs distinct）。
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;

-- 3. 手机号唯一索引（仅索引已绑用户，避免 NULL 撑爆）
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_phone_e164
    ON users (phone_e164)
    WHERE phone_e164 IS NOT NULL;

-- 4. 身份提供商绑定表
--    一个 user 可绑多个 provider（phone_sms_cn / phone_sms_global / email_code / apple / google）。
--    同一 (provider, external_id) 唯一，杜绝同手机号被绑到两个 user 的情况。
CREATE TABLE IF NOT EXISTS user_identities (
    id            BIGINT       PRIMARY KEY,
    user_id       BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider      VARCHAR(32)  NOT NULL,    -- phone_sms_cn / phone_sms_global / email_code / apple / google
    external_id   VARCHAR(255) NOT NULL,    -- 手机号 E.164 / email / apple_sub / google_sub
    country_code  VARCHAR(8),               -- phone provider 时填，如 +86 / +1
    primary_flag  BOOLEAN      NOT NULL DEFAULT FALSE,
    verified_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_user_identities_provider_extid
    ON user_identities (provider, external_id);
CREATE INDEX IF NOT EXISTS idx_user_identities_user
    ON user_identities (user_id);
