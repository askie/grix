-- phone_e164 空字符串被 GORM 写入为 '' 而非 NULL，
-- 原唯一索引 WHERE phone_e164 IS NOT NULL 未排除 ''，
-- 导致 OAuth 自动注册新用户时触发唯一约束冲突。
UPDATE users SET phone_e164 = NULL WHERE phone_e164 = '';

DROP INDEX IF EXISTS uq_users_phone_e164;
CREATE UNIQUE INDEX uq_users_phone_e164
    ON users (phone_e164)
    WHERE phone_e164 IS NOT NULL AND phone_e164 != '';
