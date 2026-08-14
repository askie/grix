-- 给收藏路径表加机器名：收藏夹按所在机器分组，客户端默认只展示当前机器的收藏。
-- 老数据 machine_name 为空，客户端归为"未知机器"分组。
ALTER TABLE user_favorite_paths
    ADD COLUMN IF NOT EXISTS machine_name VARCHAR(255) NOT NULL DEFAULT '';

-- 唯一约束从 (user_id, path) 改为 (user_id, machine_name, path)：
-- 同一路径在不同机器上可以各收藏一份，互不冲突。
DROP INDEX IF EXISTS idx_user_favorite_paths_user_path;

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_favorite_paths_user_machine_path
    ON user_favorite_paths (user_id, machine_name, path);
