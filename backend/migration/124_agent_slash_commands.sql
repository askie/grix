-- Agent 自定义斜杠命令。内置命令仍由后端按 client_type 静态注册（internal/agentslashcmd），
-- 这张表只存主人自己加的那部分；工具栏快照下发时按创建顺序追加在内置命令之后。
-- owner_id 冗余保存 agent 主人，便于按人清理与审计；写接口只允许 agent 主人调用。
-- (agent_id, name) 唯一：同一 agent 下命令名不重复；与内置命令重名在服务层拦截。
CREATE TABLE IF NOT EXISTS agent_slash_commands (
    id BIGINT PRIMARY KEY,
    agent_id BIGINT NOT NULL,
    owner_id BIGINT NOT NULL,
    name VARCHAR(64) NOT NULL,
    description VARCHAR(200) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_slash_commands_name
    ON agent_slash_commands (agent_id, name);
