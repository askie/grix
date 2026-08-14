-- 110: make Agent API send_msg idempotency permanent at the message sink.
--
-- Keep this as a new empty table: adding a unique index to the partitioned
-- messages hot table would scan/lock all historical messages during rollout.
-- The receipt row and message/inbox rows are written in one transaction.

CREATE TABLE IF NOT EXISTS send_msg_idempotency_receipts (
    session_id     VARCHAR(50) NOT NULL,
    sender_id      BIGINT      NOT NULL,
    client_msg_key VARCHAR(64) NOT NULL,
    msg_id         BIGINT      NOT NULL,
    inbox_seq      BIGINT      NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (session_id, sender_id, client_msg_key)
);
