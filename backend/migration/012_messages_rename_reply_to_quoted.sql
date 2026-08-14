DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'messages'
          AND column_name = 'reply_to_msg_id'
    ) AND NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'messages'
          AND column_name = 'quoted_message_id'
    ) THEN
        ALTER TABLE messages RENAME COLUMN reply_to_msg_id TO quoted_message_id;
    END IF;
END $$;
