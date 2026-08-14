CREATE TABLE IF NOT EXISTS widget_sessions (
  id BIGINT PRIMARY KEY,
  site_id BIGINT NOT NULL,
  owner_user_id BIGINT NOT NULL,
  visitor_id BIGINT NOT NULL,
  visitor_key VARCHAR(128) NOT NULL,
  session_id VARCHAR(50) NOT NULL UNIQUE,
  visitor_name VARCHAR(255) NOT NULL DEFAULT '',
  visitor_email VARCHAR(255) NOT NULL DEFAULT '',
  last_page_url TEXT NOT NULL DEFAULT '',
  status SMALLINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_active_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_widget_sessions_site_visitor_status
  ON widget_sessions(site_id, visitor_key, status);

CREATE INDEX IF NOT EXISTS idx_widget_sessions_owner_status_updated
  ON widget_sessions(owner_user_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_widget_sessions_visitor_status
  ON widget_sessions(visitor_id, status);
