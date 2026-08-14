-- 028: Egg dual-package support (persona_zip + skill_zip in one egg)

-- Add new columns to eggs
ALTER TABLE eggs ADD COLUMN IF NOT EXISTS has_persona_zip BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE eggs ADD COLUMN IF NOT EXISTS has_skill_zip BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE eggs ADD COLUMN IF NOT EXISTS skill_target_type VARCHAR(32) NOT NULL DEFAULT '';

-- Add new columns to egg_versions for dual zip
ALTER TABLE egg_versions ADD COLUMN IF NOT EXISTS persona_zip_url TEXT NOT NULL DEFAULT '';
ALTER TABLE egg_versions ADD COLUMN IF NOT EXISTS persona_zip_sha256 VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE egg_versions ADD COLUMN IF NOT EXISTS persona_zip_size BIGINT NOT NULL DEFAULT 0;
ALTER TABLE egg_versions ADD COLUMN IF NOT EXISTS skill_zip_url TEXT NOT NULL DEFAULT '';
ALTER TABLE egg_versions ADD COLUMN IF NOT EXISTS skill_zip_sha256 VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE egg_versions ADD COLUMN IF NOT EXISTS skill_zip_size BIGINT NOT NULL DEFAULT 0;

-- Migrate existing eggs: set has_persona_zip/has_skill_zip from old package_type
UPDATE eggs SET has_persona_zip = TRUE WHERE package_type = 'persona_zip';
UPDATE eggs SET has_skill_zip = TRUE WHERE package_type = 'skill_zip';

-- Migrate existing egg_versions: copy old zip columns to persona_zip columns for persona_zip eggs
UPDATE egg_versions SET
    persona_zip_url = zip_url,
    persona_zip_sha256 = zip_sha256,
    persona_zip_size = zip_size
FROM eggs WHERE egg_versions.egg_id = eggs.id AND eggs.package_type = 'persona_zip';

-- Migrate existing egg_versions: copy old zip columns to skill_zip columns for skill_zip eggs
UPDATE egg_versions SET
    skill_zip_url = zip_url,
    skill_zip_sha256 = zip_sha256,
    skill_zip_size = zip_size
FROM eggs WHERE egg_versions.egg_id = eggs.id AND eggs.package_type = 'skill_zip';

-- Migrate skill_target_type from old target_client_type for skill_zip eggs
UPDATE eggs SET skill_target_type = target_client_type WHERE package_type = 'skill_zip';
