-- Add per-user session message notification mute flag.
ALTER TABLE session_members
    ADD COLUMN IF NOT EXISTS is_muted BOOLEAN NOT NULL DEFAULT FALSE;
