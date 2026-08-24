-- Record which transport reported connector_version (grix-connector,
-- hermes-agent, openclaw-grix ...). Upgrade notices only target grix-connector;
-- other clients use their own version lines and must not be compared against
-- connector semver.

ALTER TABLE agents ADD COLUMN IF NOT EXISTS connector_client VARCHAR(32) NOT NULL DEFAULT '';
