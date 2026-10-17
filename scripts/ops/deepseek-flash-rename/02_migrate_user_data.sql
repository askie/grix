-- Step 2 (AFTER code deploy with CanonicalModel, BEFORE closing old pricing):
-- migrate user-facing stored model names to deepseek-flash.
-- Also cleans dirty prefixed values that CanonicalModel cannot exact-match.
--
-- Run on CN (TKE clawpool) and Global (AWS grix) separately.

BEGIN;

-- Wallet default model
UPDATE gateway_relay_settings
SET default_model = 'deepseek-flash',
    updated_at = now()
WHERE default_model IN (
    'deepseek-v4-flash',
    'deepseek/deepseek-v4-flash',
    'volcengine/deepseek-v4-flash',
    'DeepSeek-V4-Flash-0731'
);

-- model_map values
UPDATE gateway_relay_settings
SET model_map = (
        SELECT COALESCE(jsonb_object_agg(
            e.key,
            to_jsonb(
                CASE e.value #>> '{}'
                    WHEN 'deepseek-v4-flash' THEN 'deepseek-flash'
                    WHEN 'deepseek/deepseek-v4-flash' THEN 'deepseek-flash'
                    WHEN 'volcengine/deepseek-v4-flash' THEN 'deepseek-flash'
                    WHEN 'DeepSeek-V4-Flash-0731' THEN 'deepseek-flash'
                    ELSE e.value #>> '{}'
                END
            )
        ), '{}'::jsonb)
        FROM jsonb_each(model_map) AS e
    ),
    updated_at = now()
WHERE model_map::text LIKE '%deepseek-v4-flash%'
   OR model_map::text LIKE '%DeepSeek-V4-Flash%';

-- Desired relay state (must be done before closing old pricing: sync Issue uses exact servable names)
UPDATE gateway_agent_relay_state
SET relay_model = 'deepseek-flash',
    updated_at = now()
WHERE relay_model IN (
    'deepseek-v4-flash',
    'deepseek/deepseek-v4-flash',
    'volcengine/deepseek-v4-flash',
    'DeepSeek-V4-Flash-0731'
);

-- Align active virtual key snapshots with desired state (avoids pointless re-issue)
UPDATE gateway_virtual_keys
SET relay_model = 'deepseek-flash'
WHERE status = 'active'
  AND relay_model IN (
    'deepseek-v4-flash',
    'deepseek/deepseek-v4-flash',
    'volcengine/deepseek-v4-flash',
    'DeepSeek-V4-Flash-0731'
);

-- Optional system default (only if previously set)
UPDATE system_settings
SET value = to_jsonb('deepseek-flash'::text)
WHERE key = 'gateway.default_model'
  AND value::text LIKE '%deepseek-v4-flash%';

-- Verification
SELECT 'relay_settings_default' AS kind, default_model AS model, count(*) AS n
FROM gateway_relay_settings
GROUP BY default_model
ORDER BY n DESC;

SELECT 'relay_state' AS kind, relay_model AS model, count(*) AS n
FROM gateway_agent_relay_state
WHERE relay_model <> ''
GROUP BY relay_model
ORDER BY n DESC
LIMIT 20;

SELECT 'active_vk' AS kind, relay_model AS model, count(*) AS n
FROM gateway_virtual_keys
WHERE status = 'active' AND relay_model <> ''
GROUP BY relay_model
ORDER BY n DESC
LIMIT 20;

SELECT count(*) AS leftover_v4_flash_refs
FROM (
    SELECT 1 FROM gateway_relay_settings WHERE default_model LIKE '%deepseek-v4-flash%' OR default_model LIKE '%DeepSeek-V4-Flash%'
    UNION ALL
    SELECT 1 FROM gateway_relay_settings WHERE model_map::text LIKE '%deepseek-v4-flash%' OR model_map::text LIKE '%DeepSeek-V4-Flash%'
    UNION ALL
    SELECT 1 FROM gateway_agent_relay_state WHERE relay_model LIKE '%deepseek-v4-flash%' OR relay_model LIKE '%DeepSeek-V4-Flash%'
    UNION ALL
    SELECT 1 FROM gateway_virtual_keys WHERE status = 'active' AND (relay_model LIKE '%deepseek-v4-flash%' OR relay_model LIKE '%DeepSeek-V4-Flash%')
) t;

COMMIT;
