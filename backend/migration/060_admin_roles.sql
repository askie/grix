-- 管理后台 RBAC：角色表 + 管理员绑定角色
CREATE TABLE IF NOT EXISTS admin_roles (
    id          BIGINT PRIMARY KEY,
    name        VARCHAR(64) NOT NULL,
    description VARCHAR(255) NOT NULL DEFAULT '',
    permissions TEXT NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uni_admin_roles_name UNIQUE (name)
);

ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS role_id BIGINT NULL;
