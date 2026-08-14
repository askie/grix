CREATE TABLE IF NOT EXISTS register_welcome_compensations (
    id BIGSERIAL PRIMARY KEY,
    register_user_id BIGINT NOT NULL,
    customer_user_id BIGINT NOT NULL,
    status SMALLINT NOT NULL DEFAULT 0,
    attempt_count INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_register_welcome_compensations_target
ON register_welcome_compensations(register_user_id, customer_user_id);

CREATE INDEX IF NOT EXISTS idx_register_welcome_compensations_due
ON register_welcome_compensations(status, next_retry_at, id);
