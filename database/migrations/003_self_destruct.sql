-- Migration: Add self_destructed status for chat participants
-- This allows users to self-destruct their view of a chat

-- Drop and recreate the check constraint to include self_destructed and left
ALTER TABLE chat_participants
DROP CONSTRAINT IF EXISTS check_participant_status;

ALTER TABLE chat_participants
ADD CONSTRAINT check_participant_status
CHECK (status IN ('pending', 'accepted', 'declined', 'left', 'self_destructed'));

-- Comment for documentation
COMMENT ON COLUMN chat_participants.status IS 'pending: waiting for user to accept, accepted: user is active in chat, declined: user rejected invite, left: user left the chat, self_destructed: user self-destructed their copy of the chat';
