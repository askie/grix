-- 126: dedup receipts for edit-triggered @mention dispatch.
--
-- A message edit that newly adds an @mention delivers it to that member once,
-- ever, even across repeated add/remove/add edits on the same message. Kept
-- as its own small table (not a messages column) so the claim is a plain
-- unique-insert race with no lock on the hot messages table.

CREATE TABLE IF NOT EXISTS message_mention_dispatch_receipts (
    msg_id     BIGINT      NOT NULL,
    member_id  BIGINT      NOT NULL,
    session_id VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (msg_id, member_id)
);
