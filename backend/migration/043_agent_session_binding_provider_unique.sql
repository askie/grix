-- +migrate
-- Ensure one plugin-side session is imported into at most one active AIBot session.

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_session_bindings_agent_provider_binding
    ON agent_session_bindings (agent_id, provider_key, binding_id)
    WHERE binding_id <> '';
