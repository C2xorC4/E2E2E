-- Migration: Add chat invite status to chat_participants
-- This allows users to accept or decline chat invitations

-- Add status column to chat_participants
ALTER TABLE chat_participants
ADD COLUMN IF NOT EXISTS status VARCHAR(20) DEFAULT 'accepted';

-- For existing chats, set status to 'accepted' (backward compatibility)
UPDATE chat_participants SET status = 'accepted' WHERE status IS NULL;

-- Add NOT NULL constraint after setting defaults
ALTER TABLE chat_participants ALTER COLUMN status SET NOT NULL;

-- Add check constraint for valid status values
ALTER TABLE chat_participants
ADD CONSTRAINT check_participant_status
CHECK (status IN ('pending', 'accepted', 'declined'));

-- Index for finding pending invites efficiently
CREATE INDEX IF NOT EXISTS idx_chat_participants_pending
ON chat_participants(user_id, status)
WHERE status = 'pending';

-- Comment for documentation
COMMENT ON COLUMN chat_participants.status IS 'pending: waiting for user to accept, accepted: user is active in chat, declined: user rejected invite';
