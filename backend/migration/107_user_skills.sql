-- 107: 自定义技能多机器同步
-- 用户自定义技能包，平台按 owner 维度存储，connector 拉取后同步到各机器 grix/skills。
-- 平台只存与分发，不解析 content。

CREATE TABLE IF NOT EXISTS user_skills (
    id          BIGINT       PRIMARY KEY,
    owner_id    BIGINT       NOT NULL,
    name        VARCHAR(100) NOT NULL,
    content     TEXT         NOT NULL,
    version     BIGINT       NOT NULL DEFAULT 1,
    digest      VARCHAR(64)  NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 同一 owner 下技能名唯一（owner_id=0 预留系统内置技能）。
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_skills_owner_name ON user_skills (owner_id, name);
