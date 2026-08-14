-- Seed: region_select feature gate (disabled by default)
INSERT INTO feature_gates (key, display_name, status, created_at, updated_at)
VALUES ('region_select', '区域选择', 'disabled', NOW(), NOW())
ON CONFLICT (key) DO NOTHING;
