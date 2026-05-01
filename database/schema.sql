-- Encrypted Chat Application Database Schema
-- PostgreSQL with full-text search support

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    public_key BYTEA NOT NULL,  -- User's X25519 identity public key
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_seen TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_username_trgm ON users USING gin(username gin_trgm_ops);

-- Devices table (multiple devices per user)
CREATE TABLE devices (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_name VARCHAR(100) NOT NULL,
    public_key BYTEA NOT NULL,  -- Device-specific X25519 public key
    push_token VARCHAR(500),     -- For push notifications
    last_active TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(user_id, public_key)
);

CREATE INDEX idx_devices_user_id ON devices(user_id);

-- Server keys table (server-generated keys for client-server encryption)
CREATE TABLE server_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    public_key BYTEA NOT NULL,   -- Server's X25519 public key for this user
    private_key BYTEA NOT NULL,  -- Server's X25519 private key (encrypted at rest)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    rotated_at TIMESTAMP WITH TIME ZONE,
    UNIQUE(user_id)
);

CREATE INDEX idx_server_keys_user_id ON server_keys(user_id);

-- Chats table (1:1 or group conversations)
CREATE TABLE chats (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100),           -- NULL for 1:1 chats
    is_group BOOLEAN DEFAULT FALSE,
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_chats_created_at ON chats(created_at DESC);

-- Chat participants (many-to-many relationship)
CREATE TABLE chat_participants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(20) DEFAULT 'member',  -- 'admin', 'member'
    joined_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    left_at TIMESTAMP WITH TIME ZONE,
    UNIQUE(chat_id, user_id)
);

CREATE INDEX idx_chat_participants_chat_id ON chat_participants(chat_id);
CREATE INDEX idx_chat_participants_user_id ON chat_participants(user_id);
CREATE INDEX idx_chat_participants_active ON chat_participants(chat_id, user_id) WHERE left_at IS NULL;

-- Messages table with full-text search
CREATE TABLE messages (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    chat_id UUID NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    sender_id UUID NOT NULL REFERENCES users(id),
    content TEXT NOT NULL,       -- Plaintext content (decrypted by server)
    content_type VARCHAR(50) DEFAULT 'text',  -- 'text', 'image', 'file', 'system.*'
    metadata JSONB,              -- Additional data (file info, etc.)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    edited_at TIMESTAMP WITH TIME ZONE,
    deleted_at TIMESTAMP WITH TIME ZONE,
    -- Full-text search vector
    search_vector TSVECTOR GENERATED ALWAYS AS (to_tsvector('english', content)) STORED
);

CREATE INDEX idx_messages_chat_id ON messages(chat_id);
CREATE INDEX idx_messages_sender_id ON messages(sender_id);
CREATE INDEX idx_messages_created_at ON messages(chat_id, created_at DESC);
CREATE INDEX idx_messages_search ON messages USING gin(search_vector);

-- Message delivery status (for multi-device delivery tracking)
CREATE TABLE message_deliveries (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    delivered_at TIMESTAMP WITH TIME ZONE,
    read_at TIMESTAMP WITH TIME ZONE,
    UNIQUE(message_id, device_id)
);

CREATE INDEX idx_message_deliveries_message_id ON message_deliveries(message_id);
CREATE INDEX idx_message_deliveries_device_id ON message_deliveries(device_id);
CREATE INDEX idx_message_deliveries_pending ON message_deliveries(device_id) WHERE delivered_at IS NULL;

-- Sessions table for authentication
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id UUID REFERENCES devices(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL,  -- Hashed session token
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_used TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_token_hash ON sessions(token_hash);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Triggers for updated_at
CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_chats_updated_at
    BEFORE UPDATE ON chats
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Function to search messages
CREATE OR REPLACE FUNCTION search_messages(
    p_user_id UUID,
    p_query TEXT,
    p_limit INT DEFAULT 50,
    p_offset INT DEFAULT 0
)
RETURNS TABLE (
    message_id UUID,
    chat_id UUID,
    sender_id UUID,
    content TEXT,
    created_at TIMESTAMP WITH TIME ZONE,
    rank REAL
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        m.id,
        m.chat_id,
        m.sender_id,
        m.content,
        m.created_at,
        ts_rank(m.search_vector, plainto_tsquery('english', p_query)) AS rank
    FROM messages m
    JOIN chat_participants cp ON cp.chat_id = m.chat_id
    WHERE cp.user_id = p_user_id
      AND cp.left_at IS NULL
      AND m.deleted_at IS NULL
      AND m.search_vector @@ plainto_tsquery('english', p_query)
    ORDER BY rank DESC, m.created_at DESC
    LIMIT p_limit
    OFFSET p_offset;
END;
$$ LANGUAGE plpgsql;

-- View for chat list with last message
CREATE OR REPLACE VIEW chat_list_view AS
SELECT
    c.id AS chat_id,
    c.name AS chat_name,
    c.is_group,
    cp.user_id,
    (
        SELECT json_build_object(
            'id', m.id,
            'content', CASE WHEN LENGTH(m.content) > 100 THEN SUBSTRING(m.content, 1, 100) || '...' ELSE m.content END,
            'sender_id', m.sender_id,
            'created_at', m.created_at
        )
        FROM messages m
        WHERE m.chat_id = c.id AND m.deleted_at IS NULL
        ORDER BY m.created_at DESC
        LIMIT 1
    ) AS last_message,
    (
        SELECT COUNT(*)::INT
        FROM messages m
        JOIN message_deliveries md ON md.message_id = m.id
        JOIN devices d ON d.id = md.device_id
        WHERE m.chat_id = c.id
          AND d.user_id = cp.user_id
          AND md.read_at IS NULL
          AND m.sender_id != cp.user_id
    ) AS unread_count
FROM chats c
JOIN chat_participants cp ON cp.chat_id = c.id
WHERE cp.left_at IS NULL;
