-- +migrate
-- Add user-level pin support to friends table.

ALTER TABLE friends ADD COLUMN IF NOT EXISTS is_pinned BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE friends ADD COLUMN IF NOT EXISTS pinned_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_friends_user_pinned ON friends (user_id, is_pinned, pinned_at DESC);
