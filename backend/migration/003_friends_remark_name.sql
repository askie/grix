-- Add per-owner remark display name for friend relation (user_id -> friend_id)
ALTER TABLE friends
ADD COLUMN IF NOT EXISTS remark_name VARCHAR(50) NOT NULL DEFAULT '';
