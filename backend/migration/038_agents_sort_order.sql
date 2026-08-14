-- +migrate
-- Add sort_order to agents for custom ordering within categories.

ALTER TABLE agents ADD COLUMN sort_order INT NOT NULL DEFAULT 0;

CREATE INDEX idx_agents_category_sort ON agents (owner_id, category_id, sort_order, id);
