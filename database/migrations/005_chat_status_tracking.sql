-- Migration: Add chat status tracking and participant history
-- This enables tracking of orphaned/self-destructed chats and former participants

-- Add status column to chats table
ALTER TABLE chats ADD COLUMN IF NOT EXISTS status VARCHAR(20) DEFAULT 'active';

-- Add leave_reason to chat_participants to distinguish how they left
ALTER TABLE chat_participants ADD COLUMN IF NOT EXISTS leave_reason VARCHAR(20);
-- leave_reason values: NULL (still active), 'left' (voluntarily left), 'self_destructed' (self-destructed)

-- Add status column to chat_participants for easier querying
ALTER TABLE chat_participants ADD COLUMN IF NOT EXISTS status VARCHAR(20) DEFAULT 'active';
-- status values: 'active', 'pending', 'left', 'self_destructed'

-- Create index for chat status filtering
CREATE INDEX IF NOT EXISTS idx_chats_status ON chats(status);

-- Create index for participant status filtering
CREATE INDEX IF NOT EXISTS idx_chat_participants_status ON chat_participants(status);

-- Create index for finding all chats a user has ever been in
CREATE INDEX IF NOT EXISTS idx_chat_participants_user_history ON chat_participants(user_id);

-- Update existing participants to have correct status based on left_at
UPDATE chat_participants
SET status = CASE
    WHEN left_at IS NOT NULL THEN 'left'
    ELSE 'active'
END
WHERE status IS NULL OR status = '';

-- Function to get unique communication partners count for a user
CREATE OR REPLACE FUNCTION get_unique_communication_partners(p_user_id UUID)
RETURNS INT AS $$
BEGIN
    RETURN (
        SELECT COUNT(DISTINCT cp2.user_id)
        FROM chat_participants cp1
        JOIN chat_participants cp2 ON cp1.chat_id = cp2.chat_id
        WHERE cp1.user_id = p_user_id
          AND cp2.user_id != p_user_id
    );
END;
$$ LANGUAGE plpgsql;

-- Function to check if a chat is orphaned (no active participants)
CREATE OR REPLACE FUNCTION update_chat_status_on_leave()
RETURNS TRIGGER AS $$
DECLARE
    active_count INT;
BEGIN
    -- Count remaining active participants
    SELECT COUNT(*) INTO active_count
    FROM chat_participants
    WHERE chat_id = NEW.chat_id
      AND status = 'active';

    -- If no active participants, mark chat as orphaned
    IF active_count = 0 THEN
        UPDATE chats SET status = 'orphaned' WHERE id = NEW.chat_id;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to update chat status when participant leaves
DROP TRIGGER IF EXISTS trigger_update_chat_status ON chat_participants;
CREATE TRIGGER trigger_update_chat_status
    AFTER UPDATE OF status ON chat_participants
    FOR EACH ROW
    WHEN (NEW.status IN ('left', 'self_destructed'))
    EXECUTE FUNCTION update_chat_status_on_leave();
