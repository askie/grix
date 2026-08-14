-- Add per-user session pinning fields.
ALTER TABLE session_members
    ADD COLUMN IF NOT EXISTS is_pinned BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE session_members
    ADD COLUMN IF NOT EXISTS pinned_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_member_pin_active
    ON session_members(member_type, member_id, is_pinned DESC, pinned_at DESC, last_active_at DESC);
