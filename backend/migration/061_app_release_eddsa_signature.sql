-- Add EdDSA (Ed25519) signature field for Sparkle/WinSparkle update verification
ALTER TABLE app_releases ADD COLUMN IF NOT EXISTS eddsa_signature VARCHAR(128) NOT NULL DEFAULT '';
