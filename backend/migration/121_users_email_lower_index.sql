-- Google/Apple 登录按邮箱认领已有账号，以及绑定邮箱时的判重，都改成忽略大小写
-- （LOWER(email) = ?）。email 上原有的索引是普通列索引，函数比较用不上，
-- 这里补一个表达式索引，避免这些路径退化成全表扫。

CREATE INDEX IF NOT EXISTS idx_users_email_lower
    ON users (LOWER(email));
