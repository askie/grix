-- 047: widget session risk-audit fields for stable fingerprint & anti-spam

ALTER TABLE widget_sessions
  ADD COLUMN IF NOT EXISTS last_init_ip_prefix VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE widget_sessions
  ADD COLUMN IF NOT EXISTS last_init_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_widget_sessions_site_init_ip
  ON widget_sessions(site_id, last_init_ip_prefix, last_init_at DESC);
