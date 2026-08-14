-- 为用户表添加区域字段，记录用户注册时所在的部署区域（cn / global）
ALTER TABLE users ADD COLUMN IF NOT EXISTS region VARCHAR(16) NOT NULL DEFAULT 'global';
CREATE INDEX IF NOT EXISTS idx_users_region ON users(region);
