-- 手机号加密存储：真实号改密文存储，配套盲索引(唯一/精确查号)与末4位明文(塘主搜索/前端脱敏)。
-- 真实号存量数据的加密回填由 Go 程序 service.RunPhoneEncryptionMigration 完成（需应用层密钥，SQL 做不了）。
-- 手机号加密存储。

-- 1. users 增加密三列（均可空，回填前为 NULL）
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_cipher TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_last4  VARCHAR(8);
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_blind  VARCHAR(64);

-- 2. 唯一约束从明文号迁到盲索引；末4位建普通索引供塘主精确搜索
--    旧明文唯一索引不再需要（盲索引保证全局唯一）；先建新索引再删旧索引。
--    注意：GORM 对未绑手机的用户把 phone_blind 写成空串 '' 而非 NULL（见 migration 081 同款坑），
--    故谓词必须同时排除 NULL 与 ''，否则多个无手机用户的 '' 会撞唯一索引导致注册/解绑 500。
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_phone_blind
    ON users (phone_blind)
    WHERE phone_blind IS NOT NULL AND phone_blind <> '';

CREATE INDEX IF NOT EXISTS idx_users_phone_last4
    ON users (phone_last4)
    WHERE phone_last4 IS NOT NULL AND phone_last4 <> '';

DROP INDEX IF EXISTS uq_users_phone_e164;
