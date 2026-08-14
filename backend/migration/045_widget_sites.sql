CREATE TABLE IF NOT EXISTS widget_sites (
  id BIGINT PRIMARY KEY,
  owner_user_id BIGINT NOT NULL,
  site_key VARCHAR(64) NOT NULL UNIQUE,
  site_secret_hash VARCHAR(255) NOT NULL,
  site_name VARCHAR(255) NOT NULL,
  allowed_origins JSONB NOT NULL DEFAULT '[]'::jsonb,
  status SMALLINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_widget_sites_owner_status_updated
  ON widget_sites(owner_user_id, status, updated_at DESC);
