-- 108: 单轮对话审计的关联与生命周期元数据。
-- 正文仅保留在 connector 本地回放库，不进入平台数据库。

CREATE TABLE IF NOT EXISTS conversation_audit_turns (
    id            BIGINT       PRIMARY KEY,
    owner_id      BIGINT       NOT NULL,
    agent_id      BIGINT       NOT NULL,
    session_id    VARCHAR(64)  NOT NULL,
    msg_id        BIGINT       NOT NULL,
    event_id      VARCHAR(191) NOT NULL,
    audit_id      VARCHAR(191) NOT NULL DEFAULT '',
    turn_id       VARCHAR(191) NOT NULL DEFAULT '',
    state         VARCHAR(32)  NOT NULL,
    revision      INTEGER      NOT NULL DEFAULT 0,
    quality       VARCHAR(32)  NOT NULL DEFAULT '',
    truncated     BOOLEAN      NOT NULL DEFAULT FALSE,
    error_code    VARCHAR(96)  NOT NULL DEFAULT '',
    error_message VARCHAR(512) NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_audit_turn_agent_event
    ON conversation_audit_turns (agent_id, event_id);
CREATE INDEX IF NOT EXISTS idx_conversation_audit_turn_owner_msg
    ON conversation_audit_turns (owner_id, session_id, msg_id, agent_id);
CREATE INDEX IF NOT EXISTS idx_conversation_audit_turn_audit_id
    ON conversation_audit_turns (audit_id);
CREATE INDEX IF NOT EXISTS idx_conversation_audit_turn_turn_id
    ON conversation_audit_turns (turn_id);
