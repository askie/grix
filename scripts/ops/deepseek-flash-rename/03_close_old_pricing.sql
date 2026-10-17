-- Step 3 (AFTER step 2 user-data migration): retire deepseek-v4-flash pricing so
-- the C-end model list only shows deepseek-flash.
--
-- Run on CN (TKE clawpool) and Global (AWS grix) separately.

BEGIN;

UPDATE gateway_pricing_rules
SET effective_to = now()
WHERE provider = 'deepseek'
  AND model = 'deepseek-v4-flash'
  AND effective_to IS NULL;

SELECT model,
       count(*) FILTER (WHERE effective_to IS NULL) AS active,
       count(*) AS total
FROM gateway_pricing_rules
WHERE provider = 'deepseek'
  AND model IN ('deepseek-flash', 'deepseek-v4-flash', 'deepseek-v4-pro')
GROUP BY model
ORDER BY model;

COMMIT;
