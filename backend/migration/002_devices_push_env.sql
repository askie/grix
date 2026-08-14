ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS push_env VARCHAR(32);

UPDATE devices
SET push_env = 'default'
WHERE COALESCE(push_env, '') = ''
  AND platform <> 'ios';

UPDATE devices
SET push_env = 'unknown'
WHERE COALESCE(push_env, '') = ''
  AND platform = 'ios';

UPDATE devices
SET is_active = FALSE
WHERE platform = 'ios'
  AND push_env = 'unknown'
  AND is_active = TRUE;

ALTER TABLE devices
    ALTER COLUMN push_env SET NOT NULL;

DROP INDEX IF EXISTS idx_unique_devices;

CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_devices
    ON devices(user_id, platform, push_env, device_token);

ALTER TABLE devices
    DROP CONSTRAINT IF EXISTS chk_devices_push_env;

ALTER TABLE devices
    ADD CONSTRAINT chk_devices_push_env
    CHECK (
        (platform = 'ios' AND push_env IN ('apns_sandbox', 'apns_production', 'unknown'))
        OR
        (platform <> 'ios' AND push_env = 'default')
    );
