-- Persist the connector version reported at agent-api WebSocket auth so the
-- backend can find agents still running outdated connectors (see
-- NotifyConnectorUpgrade). Only agent-api connectors report a client_version;
-- other agent types keep the empty default.

ALTER TABLE agents ADD COLUMN IF NOT EXISTS connector_version VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS connector_version_seen_at TIMESTAMPTZ;
