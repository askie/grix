-- Stable, append-only keyset for resumable session-list bootstrap. Activity
-- fields are mutable and cannot safely page a live account.
CREATE SEQUENCE IF NOT EXISTS session_members_bootstrap_id_seq;

ALTER TABLE session_members
    ADD COLUMN IF NOT EXISTS bootstrap_id BIGINT;

ALTER TABLE session_members
    ALTER COLUMN bootstrap_id
    SET DEFAULT nextval('session_members_bootstrap_id_seq');

UPDATE session_members
SET bootstrap_id = nextval('session_members_bootstrap_id_seq')
WHERE bootstrap_id IS NULL OR bootstrap_id <= 0;

SELECT setval(
    'session_members_bootstrap_id_seq',
    COALESCE(MAX(bootstrap_id), 1),
    COUNT(*) > 0
)
FROM session_members;

ALTER SEQUENCE session_members_bootstrap_id_seq
    OWNED BY session_members.bootstrap_id;

ALTER TABLE session_members
    ALTER COLUMN bootstrap_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_session_members_bootstrap_id
    ON session_members(bootstrap_id);

CREATE INDEX IF NOT EXISTS idx_member_bootstrap
    ON session_members(member_type, member_id, bootstrap_id);
