-- Add visible_to column for selective message visibility in group chats.
-- NULL means visible to all group members (default behavior).
-- When set to a JSON array of user IDs, only those users (and the sender) can see the message.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS visible_to jsonb DEFAULT NULL;

-- GIN index for efficient JSONB containment queries (visible_to @> '[user_id]').
-- Partial index: only rows with visible_to set are indexed to minimize index size.
CREATE INDEX IF NOT EXISTS idx_messages_visible_to_gin
    ON messages USING GIN (visible_to)
    WHERE visible_to IS NOT NULL;
