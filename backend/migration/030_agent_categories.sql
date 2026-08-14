-- Agent categories: user-managed hierarchical classification for agents
CREATE TABLE IF NOT EXISTS agent_categories (
    id         BIGSERIAL   PRIMARY KEY,
    owner_id   BIGINT      NOT NULL,
    parent_id  BIGINT      NOT NULL DEFAULT 0,
    name       VARCHAR(100) NOT NULL,
    sort_order INTEGER     NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_categories_owner_id ON agent_categories(owner_id);
CREATE INDEX IF NOT EXISTS idx_agent_categories_parent_id ON agent_categories(parent_id);

-- Add category_id column to agents table
ALTER TABLE agents ADD COLUMN IF NOT EXISTS category_id BIGINT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_agents_category_id ON agents(category_id);
