ALTER TABLE egg_translation_jobs
    ADD COLUMN IF NOT EXISTS source_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
