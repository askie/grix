-- Add is_main column to agents table.
-- When true, the agent is a "main agent" with full scope privileges.
ALTER TABLE agents ADD COLUMN IF NOT EXISTS is_main boolean NOT NULL DEFAULT false;

-- Backfill: set is_main=true for agents that currently hold the agent.api.create scope.
UPDATE agents
SET is_main = true
WHERE provider_type = 3
  AND id IN (
    SELECT DISTINCT agent_id FROM agent_api_scopes WHERE scope = 'agent.api.create'
  );
