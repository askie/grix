-- Step 1 (BEFORE deploying CanonicalModel code): copy active deepseek-v4-flash
-- pricing rules to deepseek-flash. Old and new names may coexist.
-- Idempotent: skips windows/tiers that already have an active deepseek-flash rule.
--
-- Run on CN (TKE clawpool) and Global (AWS grix) separately.

BEGIN;

INSERT INTO gateway_pricing_rules (
    id,
    provider,
    model,
    cached_input_price_per_m,
    uncached_input_price_per_m,
    output_price_per_m,
    source_currency,
    fx_rate_used,
    created_by,
    triggered_by_report_id,
    effective_from,
    effective_to,
    daily_window_start_min,
    daily_window_end_min,
    input_tier_start_tokens,
    input_tier_end_tokens
)
SELECT
    (SELECT COALESCE(MAX(id), 0) FROM gateway_pricing_rules)
        + ROW_NUMBER() OVER (ORDER BY src.id),
    src.provider,
    'deepseek-flash',
    src.cached_input_price_per_m,
    src.uncached_input_price_per_m,
    src.output_price_per_m,
    src.source_currency,
    src.fx_rate_used,
    'manual',
    NULL,
    now(),
    NULL,
    src.daily_window_start_min,
    src.daily_window_end_min,
    src.input_tier_start_tokens,
    src.input_tier_end_tokens
FROM gateway_pricing_rules src
WHERE src.provider = 'deepseek'
  AND src.model = 'deepseek-v4-flash'
  AND src.effective_to IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM gateway_pricing_rules dst
      WHERE dst.provider = 'deepseek'
        AND dst.model = 'deepseek-flash'
        AND dst.effective_to IS NULL
        AND dst.daily_window_start_min IS NOT DISTINCT FROM src.daily_window_start_min
        AND dst.daily_window_end_min IS NOT DISTINCT FROM src.daily_window_end_min
        AND dst.input_tier_start_tokens IS NOT DISTINCT FROM src.input_tier_start_tokens
        AND dst.input_tier_end_tokens IS NOT DISTINCT FROM src.input_tier_end_tokens
  );

-- Expect 3 active flash rules (all-day + 2 peak windows) on both regions after this.
SELECT model,
       count(*) FILTER (WHERE effective_to IS NULL) AS active
FROM gateway_pricing_rules
WHERE provider = 'deepseek'
  AND model IN ('deepseek-flash', 'deepseek-v4-flash')
GROUP BY model
ORDER BY model;

COMMIT;
