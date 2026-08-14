-- 109: 长期保存 connector event_result 终态判决与副作用恢复状态。

CREATE TABLE IF NOT EXISTS agent_event_terminal_ledgers (
    event_id             VARCHAR(192) PRIMARY KEY,
    terminal_commit_token VARCHAR(64) NOT NULL DEFAULT '',
    owner_id             BIGINT       NOT NULL,
    agent_id             BIGINT       NOT NULL,
    session_id           VARCHAR(64)  NOT NULL DEFAULT '',
    session_type         SMALLINT     NOT NULL DEFAULT 0,
    mirror_mode          VARCHAR(32)  NOT NULL DEFAULT '',
    record_only          BOOLEAN      NOT NULL DEFAULT FALSE,
    sender_id            BIGINT       NOT NULL DEFAULT 0,
    trigger_msg_id       BIGINT       NOT NULL DEFAULT 0,
    delegate_event       JSONB        NOT NULL DEFAULT '{}'::jsonb,
    status               VARCHAR(32)  NOT NULL,
    code                 TEXT         NOT NULL DEFAULT '',
    msg                  TEXT         NOT NULL DEFAULT '',
    started_at           TIMESTAMPTZ,
    received_at          BIGINT       NOT NULL DEFAULT 0,
    call_turn            BOOLEAN      NOT NULL DEFAULT FALSE,
    dispatch_generation  BIGINT       NOT NULL DEFAULT 0,
    terminal_at          TIMESTAMPTZ,
    effects_state        VARCHAR(16)  NOT NULL DEFAULT 'pending',
    effects_done_at      TIMESTAMPTZ,
    redis_committed_at   TIMESTAMPTZ,
    task_eligible        BOOLEAN      NOT NULL DEFAULT FALSE,
    task_notification_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    effects_suppressed   BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_event_terminal_owner_agent
    ON agent_event_terminal_ledgers (owner_id, agent_id, event_id);

CREATE INDEX IF NOT EXISTS idx_agent_event_terminal_generation
    ON agent_event_terminal_ledgers (dispatch_generation);

CREATE TABLE IF NOT EXISTS agent_event_terminal_effects (
    event_id       VARCHAR(192) NOT NULL,
    effect         VARCHAR(32)  NOT NULL,
    state          VARCHAR(16)  NOT NULL DEFAULT 'pending',
    claim_token    VARCHAR(64)  NOT NULL DEFAULT '',
    claim_until    TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    attempt_count  INTEGER      NOT NULL DEFAULT 0,
    last_error     TEXT         NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, effect),
    FOREIGN KEY (event_id) REFERENCES agent_event_terminal_ledgers(event_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_agent_event_terminal_effect_state
    ON agent_event_terminal_effects (state, updated_at);

CREATE TABLE IF NOT EXISTS agent_notification_receipts (
    idempotency_key VARCHAR(256) NOT NULL,
    channel         VARCHAR(32)  NOT NULL,
    claimed_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (idempotency_key, channel)
);

CREATE TABLE IF NOT EXISTS agent_run_sequences (
    scope_key VARCHAR(160) PRIMARY KEY,
    value     BIGINT NOT NULL DEFAULT 0
);

ALTER TABLE chat_states
    ADD COLUMN IF NOT EXISTS run_generation BIGINT NOT NULL DEFAULT 0;
