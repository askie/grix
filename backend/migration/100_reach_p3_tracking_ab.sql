-- reach_send_logs: open/click tracking + region
ALTER TABLE reach_send_logs ADD COLUMN IF NOT EXISTS opened_at  TIMESTAMPTZ;
ALTER TABLE reach_send_logs ADD COLUMN IF NOT EXISTS clicked_at TIMESTAMPTZ;
ALTER TABLE reach_send_logs ADD COLUMN IF NOT EXISTS region     VARCHAR(16) NOT NULL DEFAULT '';

-- reach_tasks: A/B testing
ALTER TABLE reach_tasks ADD COLUMN IF NOT EXISTS ab_group_id VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE reach_tasks ADD COLUMN IF NOT EXISTS ab_variant  VARCHAR(32) NOT NULL DEFAULT '';

-- index for A/B group lookup
CREATE INDEX IF NOT EXISTS idx_reach_tasks_ab_group ON reach_tasks (ab_group_id) WHERE ab_group_id != '';
