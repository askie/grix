-- Add index to speed up state-based scans in the self-heal worker.
CREATE INDEX IF NOT EXISTS idx_session_agent_states_state_updated
    ON session_agent_states(state, updated_at DESC);
