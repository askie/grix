CREATE TABLE IF NOT EXISTS webhook_endpoints (
  id BIGINT PRIMARY KEY,
  user_id BIGINT NOT NULL,
  session_id VARCHAR(50) NOT NULL,
  token_hash VARCHAR(128) NOT NULL UNIQUE,
  token_value VARCHAR(255) NOT NULL,
  token_prefix VARCHAR(16) NOT NULL,
  expires_at TIMESTAMPTZ NULL,
  last_used_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_webhook_session_user_active
  ON webhook_endpoints(session_id, user_id)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_webhook_expires_active
  ON webhook_endpoints(expires_at)
  WHERE deleted_at IS NULL;
