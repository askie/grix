CREATE TABLE IF NOT EXISTS user_sync_heads (
    user_id BIGINT PRIMARY KEY,
    head_cursor BIGINT NOT NULL DEFAULT 0 CHECK (head_cursor >= 0),
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_sync_events (
    user_id BIGINT NOT NULL,
    stream_cursor BIGINT NOT NULL CHECK (stream_cursor > 0),
    event_kind VARCHAR(64) NOT NULL,
    entity_type VARCHAR(32) NOT NULL,
    entity_id VARCHAR(128) NOT NULL,
    entity_version BIGINT NOT NULL DEFAULT 0,
    tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    command_id VARCHAR(128) NOT NULL DEFAULT '',
    payload JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, stream_cursor)
);
CREATE INDEX IF NOT EXISTS idx_user_sync_events_entity
    ON user_sync_events(user_id, entity_type, entity_id, stream_cursor DESC);

CREATE TABLE IF NOT EXISTS device_sync_cursors (
    user_id BIGINT NOT NULL,
    device_id VARCHAR(100) NOT NULL,
    stream_name VARCHAR(32) NOT NULL DEFAULT 'chat',
    generation VARCHAR(64) NOT NULL,
    last_resume_cursor BIGINT NOT NULL DEFAULT 0 CHECK (last_resume_cursor >= 0),
    committed_cursor BIGINT NOT NULL DEFAULT 0 CHECK (committed_cursor >= 0),
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, device_id, stream_name)
);

CREATE TABLE IF NOT EXISTS sync_command_receipts (
    user_id BIGINT NOT NULL,
    command_kind VARCHAR(64) NOT NULL,
    command_id VARCHAR(128) NOT NULL,
    response JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, command_kind, command_id)
);

ALTER TABLE messages ADD COLUMN IF NOT EXISTS state_version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS state_version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE session_members ADD COLUMN IF NOT EXISTS state_version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE session_members ADD COLUMN IF NOT EXISTS is_tombstone BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE session_history_resets ADD COLUMN IF NOT EXISTS state_version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE user_peer_pins ADD COLUMN IF NOT EXISTS state_version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE user_peer_mutes ADD COLUMN IF NOT EXISTS state_version BIGINT NOT NULL DEFAULT 1;
