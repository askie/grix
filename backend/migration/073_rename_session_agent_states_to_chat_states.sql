-- Rename table to reflect "chat state" semantics.
ALTER TABLE session_agent_states RENAME TO chat_states;

-- Rename existing indexes to match new table name.
ALTER INDEX IF EXISTS idx_session_agent_states_owner         RENAME TO idx_chat_states_owner_agent;
ALTER INDEX IF EXISTS idx_session_agent_states_state_updated RENAME TO idx_chat_states_state_updated;

-- Add indexes optimised for the paginated chat_state_query:
--   base query:          WHERE owner_id = ?                ORDER BY updated_at DESC
--   state-filtered:      WHERE owner_id = ? AND state = ?  ORDER BY updated_at DESC
CREATE INDEX IF NOT EXISTS idx_chat_states_owner_updated
    ON chat_states(owner_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_chat_states_owner_state_updated
    ON chat_states(owner_id, state, updated_at DESC);
