package cache

import (
	"database/sql"
	"encoding/base64"
	"os"
	"path/filepath"
	"time"

	"e2e-client/internal/crypto"

	_ "github.com/mattn/go-sqlite3"
)

type Cache struct {
	db            *sql.DB
	encryptionKey []byte // Optional encryption key for message content
}

type CachedMessage struct {
	ID          string
	ChatID      string
	SenderID    string
	Content     string
	ContentType string
	CreatedAt   time.Time
	Synced      bool
}

type CachedChat struct {
	ID        string
	Name      string
	IsGroup   bool
	UpdatedAt time.Time
}

type CachedUser struct {
	ID        string
	Username  string
	Email     string
	PublicKey []byte
}

// New creates a new cache without encryption
func New() (*Cache, error) {
	return NewWithKey(nil)
}

// NewWithKey creates a new cache with optional encryption key
func NewWithKey(encryptionKey []byte) (*Cache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(homeDir, ".config", "e2e-chat")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(cacheDir, "cache.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	cache := &Cache{
		db:            db,
		encryptionKey: encryptionKey,
	}
	if err := cache.init(); err != nil {
		db.Close()
		return nil, err
	}

	return cache, nil
}

// SetEncryptionKey sets or updates the encryption key for the cache
func (c *Cache) SetEncryptionKey(key []byte) {
	c.encryptionKey = key
}

// IsEncrypted returns true if cache encryption is enabled
func (c *Cache) IsEncrypted() bool {
	return len(c.encryptionKey) > 0
}

// encryptContent encrypts content if encryption key is set
func (c *Cache) encryptContent(content string) (string, error) {
	if len(c.encryptionKey) == 0 {
		return content, nil
	}

	encrypted, err := crypto.EncryptWithKey(c.encryptionKey, []byte(content))
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// decryptContent decrypts content if encryption key is set
func (c *Cache) decryptContent(content string) (string, error) {
	if len(c.encryptionKey) == 0 {
		return content, nil
	}

	// Try to decode as base64 - if it fails, content may be unencrypted (legacy)
	encrypted, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		// Not base64, return as-is (likely unencrypted legacy data)
		return content, nil
	}

	decrypted, err := crypto.DecryptWithKey(c.encryptionKey, encrypted)
	if err != nil {
		// Decryption failed, might be unencrypted legacy data
		return content, nil
	}

	return string(decrypted), nil
}

func (c *Cache) init() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		email TEXT,
		public_key BLOB,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS chats (
		id TEXT PRIMARY KEY,
		name TEXT,
		is_group BOOLEAN DEFAULT FALSE,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS chat_participants (
		chat_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		PRIMARY KEY (chat_id, user_id),
		FOREIGN KEY (chat_id) REFERENCES chats(id),
		FOREIGN KEY (user_id) REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		chat_id TEXT NOT NULL,
		sender_id TEXT NOT NULL,
		content TEXT NOT NULL,
		content_type TEXT DEFAULT 'text',
		created_at DATETIME NOT NULL,
		synced BOOLEAN DEFAULT FALSE,
		FOREIGN KEY (chat_id) REFERENCES chats(id),
		FOREIGN KEY (sender_id) REFERENCES users(id)
	);

	CREATE INDEX IF NOT EXISTS idx_messages_chat_id ON messages(chat_id);
	CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(chat_id, created_at);
	CREATE INDEX IF NOT EXISTS idx_messages_synced ON messages(synced);
	`

	_, err := c.db.Exec(schema)
	return err
}

func (c *Cache) Close() error {
	return c.db.Close()
}

// User operations

func (c *Cache) SaveUser(user *CachedUser) error {
	_, err := c.db.Exec(`
		INSERT OR REPLACE INTO users (id, username, email, public_key, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, user.ID, user.Username, user.Email, user.PublicKey)
	return err
}

func (c *Cache) GetUser(id string) (*CachedUser, error) {
	row := c.db.QueryRow(`SELECT id, username, email, public_key FROM users WHERE id = ?`, id)

	var user CachedUser
	err := row.Scan(&user.ID, &user.Username, &user.Email, &user.PublicKey)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (c *Cache) GetAllUsers() (map[string]*CachedUser, error) {
	rows, err := c.db.Query(`SELECT id, username, email, public_key FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make(map[string]*CachedUser)
	for rows.Next() {
		var user CachedUser
		if err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.PublicKey); err != nil {
			continue
		}
		users[user.ID] = &user
	}
	return users, nil
}

// Chat operations

func (c *Cache) SaveChat(chat *CachedChat) error {
	_, err := c.db.Exec(`
		INSERT OR REPLACE INTO chats (id, name, is_group, updated_at)
		VALUES (?, ?, ?, ?)
	`, chat.ID, chat.Name, chat.IsGroup, chat.UpdatedAt)
	return err
}

func (c *Cache) GetChat(id string) (*CachedChat, error) {
	row := c.db.QueryRow(`SELECT id, name, is_group, updated_at FROM chats WHERE id = ?`, id)

	var chat CachedChat
	err := row.Scan(&chat.ID, &chat.Name, &chat.IsGroup, &chat.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &chat, nil
}

func (c *Cache) GetAllChats() ([]*CachedChat, error) {
	rows, err := c.db.Query(`
		SELECT id, name, is_group, updated_at
		FROM chats
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []*CachedChat
	for rows.Next() {
		var chat CachedChat
		if err := rows.Scan(&chat.ID, &chat.Name, &chat.IsGroup, &chat.UpdatedAt); err != nil {
			continue
		}
		chats = append(chats, &chat)
	}
	return chats, nil
}

func (c *Cache) AddChatParticipant(chatID, userID string) error {
	_, err := c.db.Exec(`
		INSERT OR IGNORE INTO chat_participants (chat_id, user_id)
		VALUES (?, ?)
	`, chatID, userID)
	return err
}

func (c *Cache) GetChatParticipants(chatID string) ([]string, error) {
	rows, err := c.db.Query(`SELECT user_id FROM chat_participants WHERE chat_id = ?`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userIDs []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		userIDs = append(userIDs, userID)
	}
	return userIDs, nil
}

// Message operations

func (c *Cache) SaveMessage(msg *CachedMessage) error {
	// Encrypt content before storing
	content, err := c.encryptContent(msg.Content)
	if err != nil {
		return err
	}

	_, err = c.db.Exec(`
		INSERT OR REPLACE INTO messages (id, chat_id, sender_id, content, content_type, created_at, synced)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, msg.ID, msg.ChatID, msg.SenderID, content, msg.ContentType, msg.CreatedAt, msg.Synced)

	// Update chat's updated_at
	if err == nil {
		c.db.Exec(`UPDATE chats SET updated_at = ? WHERE id = ?`, msg.CreatedAt, msg.ChatID)
	}

	return err
}

func (c *Cache) GetMessages(chatID string, limit, offset int) ([]*CachedMessage, error) {
	rows, err := c.db.Query(`
		SELECT id, chat_id, sender_id, content, content_type, created_at, synced
		FROM messages
		WHERE chat_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, chatID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*CachedMessage
	for rows.Next() {
		var msg CachedMessage
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.SenderID, &msg.Content, &msg.ContentType, &msg.CreatedAt, &msg.Synced); err != nil {
			continue
		}
		// Decrypt content after retrieval
		if decrypted, err := c.decryptContent(msg.Content); err == nil {
			msg.Content = decrypted
		}
		messages = append(messages, &msg)
	}
	return messages, nil
}

func (c *Cache) GetMessagesChronological(chatID string, limit, offset int) ([]*CachedMessage, error) {
	// Get messages in chronological order (oldest first)
	rows, err := c.db.Query(`
		SELECT id, chat_id, sender_id, content, content_type, created_at, synced
		FROM messages
		WHERE chat_id = ?
		ORDER BY created_at ASC
		LIMIT ? OFFSET ?
	`, chatID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*CachedMessage
	for rows.Next() {
		var msg CachedMessage
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.SenderID, &msg.Content, &msg.ContentType, &msg.CreatedAt, &msg.Synced); err != nil {
			continue
		}
		// Decrypt content after retrieval
		if decrypted, err := c.decryptContent(msg.Content); err == nil {
			msg.Content = decrypted
		}
		messages = append(messages, &msg)
	}
	return messages, nil
}

func (c *Cache) GetLastMessage(chatID string) (*CachedMessage, error) {
	row := c.db.QueryRow(`
		SELECT id, chat_id, sender_id, content, content_type, created_at, synced
		FROM messages
		WHERE chat_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, chatID)

	var msg CachedMessage
	err := row.Scan(&msg.ID, &msg.ChatID, &msg.SenderID, &msg.Content, &msg.ContentType, &msg.CreatedAt, &msg.Synced)
	if err != nil {
		return nil, err
	}
	// Decrypt content after retrieval
	if decrypted, err := c.decryptContent(msg.Content); err == nil {
		msg.Content = decrypted
	}
	return &msg, nil
}

func (c *Cache) GetUnsyncedMessages() ([]*CachedMessage, error) {
	rows, err := c.db.Query(`
		SELECT id, chat_id, sender_id, content, content_type, created_at, synced
		FROM messages
		WHERE synced = FALSE
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*CachedMessage
	for rows.Next() {
		var msg CachedMessage
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.SenderID, &msg.Content, &msg.ContentType, &msg.CreatedAt, &msg.Synced); err != nil {
			continue
		}
		// Decrypt content after retrieval
		if decrypted, err := c.decryptContent(msg.Content); err == nil {
			msg.Content = decrypted
		}
		messages = append(messages, &msg)
	}
	return messages, nil
}

func (c *Cache) MarkMessageSynced(id string) error {
	_, err := c.db.Exec(`UPDATE messages SET synced = TRUE WHERE id = ?`, id)
	return err
}

func (c *Cache) GetMessageCount(chatID string) (int, error) {
	var count int
	err := c.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE chat_id = ?`, chatID).Scan(&count)
	return count, err
}

func (c *Cache) DeleteMessage(id string) error {
	_, err := c.db.Exec(`DELETE FROM messages WHERE id = ?`, id)
	return err
}

func (c *Cache) ClearChat(chatID string) error {
	_, err := c.db.Exec(`DELETE FROM messages WHERE chat_id = ?`, chatID)
	return err
}

func (c *Cache) ClearAll() error {
	_, err := c.db.Exec(`
		DELETE FROM messages;
		DELETE FROM chat_participants;
		DELETE FROM chats;
		DELETE FROM users;
	`)
	return err
}

func (c *Cache) DeleteChatMessages(chatID string) error {
	_, err := c.db.Exec(`DELETE FROM messages WHERE chat_id = ?`, chatID)
	return err
}

func (c *Cache) DeleteChat(chatID string) error {
	// Delete in order of dependencies
	c.db.Exec(`DELETE FROM messages WHERE chat_id = ?`, chatID)
	c.db.Exec(`DELETE FROM chat_participants WHERE chat_id = ?`, chatID)
	_, err := c.db.Exec(`DELETE FROM chats WHERE id = ?`, chatID)
	return err
}

// Search messages locally
// Note: When encryption is enabled, search only works by decrypting all messages
// and filtering client-side, which may be slow for large caches
func (c *Cache) SearchMessages(query string, limit int) ([]*CachedMessage, error) {
	// If encryption is enabled, we need to decrypt and search client-side
	if c.IsEncrypted() {
		return c.searchMessagesEncrypted(query, limit)
	}

	rows, err := c.db.Query(`
		SELECT id, chat_id, sender_id, content, content_type, created_at, synced
		FROM messages
		WHERE content LIKE ?
		ORDER BY created_at DESC
		LIMIT ?
	`, "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*CachedMessage
	for rows.Next() {
		var msg CachedMessage
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.SenderID, &msg.Content, &msg.ContentType, &msg.CreatedAt, &msg.Synced); err != nil {
			continue
		}
		messages = append(messages, &msg)
	}
	return messages, nil
}

// searchMessagesEncrypted searches encrypted messages by decrypting each one
func (c *Cache) searchMessagesEncrypted(query string, limit int) ([]*CachedMessage, error) {
	rows, err := c.db.Query(`
		SELECT id, chat_id, sender_id, content, content_type, created_at, synced
		FROM messages
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*CachedMessage
	for rows.Next() {
		var msg CachedMessage
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.SenderID, &msg.Content, &msg.ContentType, &msg.CreatedAt, &msg.Synced); err != nil {
			continue
		}
		// Decrypt content
		if decrypted, err := c.decryptContent(msg.Content); err == nil {
			msg.Content = decrypted
		}
		// Check if decrypted content contains query (case-insensitive)
		if containsIgnoreCase(msg.Content, query) {
			messages = append(messages, &msg)
			if len(messages) >= limit {
				break
			}
		}
	}
	return messages, nil
}

// containsIgnoreCase checks if s contains substr (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	sLower := make([]byte, len(s))
	substrLower := make([]byte, len(substr))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		sLower[i] = c
	}
	for i := 0; i < len(substr); i++ {
		c := substr[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		substrLower[i] = c
	}
	return containsBytes(sLower, substrLower)
}

// containsBytes checks if s contains substr
func containsBytes(s, substr []byte) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
