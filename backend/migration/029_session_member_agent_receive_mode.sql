ALTER TABLE session_members
    ADD COLUMN IF NOT EXISTS agent_receive_mode smallint NOT NULL DEFAULT 1;

ALTER TABLE session_members
    ADD COLUMN IF NOT EXISTS agent_receive_backlog_count integer NOT NULL DEFAULT 8;
