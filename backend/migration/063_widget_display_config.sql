ALTER TABLE widget_sites
    ADD COLUMN IF NOT EXISTS display_config JSONB NOT NULL DEFAULT '{}';
