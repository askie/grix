-- Accelerate revoke/delete cleanup on user_inbox by original message identity.
CREATE INDEX IF NOT EXISTS idx_user_inbox_msg_session
ON user_inbox(msg_id, session_id);
