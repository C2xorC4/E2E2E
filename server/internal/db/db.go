package db

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"e2e-chat/internal/crypto"
	"e2e-chat/internal/models"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// DB wraps sqlx.DB with application-specific methods
type DB struct {
	*sqlx.DB
}

// New creates a new database connection
func New(dataSourceName string) (*DB, error) {
	db, err := sqlx.Connect("postgres", dataSourceName)
	if err != nil {
		return nil, err
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &DB{db}, nil
}

// User operations

// CreateUser creates a new user with their key pair
func (db *DB) CreateUser(ctx context.Context, username, email string, passwordHash, publicKey []byte) (*models.User, error) {
	user := &models.User{
		ID:           uuid.New(),
		Username:     username,
		Email:        email,
		PasswordHash: hex.EncodeToString(passwordHash),
		PublicKey:    publicKey,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO users (id, username, email, password_hash, public_key, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, user.ID, user.Username, user.Email, user.PasswordHash, user.PublicKey, user.CreatedAt, user.UpdatedAt)

	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserByEmail retrieves a user by email
func (db *DB) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	user := &models.User{}
	err := db.GetContext(ctx, user, `
		SELECT id, username, email, password_hash, public_key, created_at, updated_at, last_seen
		FROM users WHERE email = $1
	`, email)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// GetUserByID retrieves a user by ID
func (db *DB) GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	user := &models.User{}
	err := db.GetContext(ctx, user, `
		SELECT id, username, email, password_hash, public_key, created_at, updated_at, last_seen
		FROM users WHERE id = $1
	`, id)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// SearchUsers searches for users by username
func (db *DB) SearchUsers(ctx context.Context, query string, limit int) ([]*models.User, error) {
	users := []*models.User{}
	err := db.SelectContext(ctx, &users, `
		SELECT id, username, email, public_key, created_at, updated_at, last_seen
		FROM users
		WHERE username ILIKE $1
		ORDER BY similarity(username, $2) DESC
		LIMIT $3
	`, "%"+query+"%", query, limit)
	if err != nil {
		return nil, err
	}
	return users, nil
}

// UpdateUserLastSeen updates the user's last seen timestamp
func (db *DB) UpdateUserLastSeen(ctx context.Context, userID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		UPDATE users SET last_seen = NOW() WHERE id = $1
	`, userID)
	return err
}

// Device operations

// CreateDevice creates a new device for a user
func (db *DB) CreateDevice(ctx context.Context, userID uuid.UUID, deviceName string, publicKey []byte) (*models.Device, error) {
	device := &models.Device{
		ID:         uuid.New(),
		UserID:     userID,
		DeviceName: deviceName,
		PublicKey:  publicKey,
		LastActive: time.Now(),
		CreatedAt:  time.Now(),
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO devices (id, user_id, device_name, public_key, last_active, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, device.ID, device.UserID, device.DeviceName, device.PublicKey, device.LastActive, device.CreatedAt)

	if err != nil {
		return nil, err
	}

	return device, nil
}

// CreateDeviceWithProfile creates a new device with full profile information
func (db *DB) CreateDeviceWithProfile(ctx context.Context, userID uuid.UUID, deviceName string, publicKey []byte, profile *models.DeviceProfile) (*models.Device, error) {
	meshDiscoveryID := uuid.New()

	device := &models.Device{
		ID:              uuid.New(),
		UserID:          userID,
		DeviceName:      deviceName,
		PublicKey:       publicKey,
		LastActive:      time.Now(),
		CreatedAt:       time.Now(),
		MeshDiscoveryID: &meshDiscoveryID,
	}

	// Set profile fields if provided
	if profile != nil {
		device.Platform = strPtr(profile.Platform)
		device.OSVersion = strPtr(profile.OSVersion)
		device.Manufacturer = strPtr(profile.Manufacturer)
		device.Model = strPtr(profile.Model)
		device.HardwareID = strPtr(profile.HardwareID)
		device.CPUArch = strPtr(profile.CPUArch)
		device.AppVersion = strPtr(profile.AppVersion)
		device.BluetoothAddress = strPtr(profile.BluetoothAddress)
		device.WiFiDirectAddress = strPtr(profile.WiFiDirectAddress)
		device.MeshCapable = profile.MeshCapable
		device.LastIPAddress = strPtr(profile.IPAddress)
		device.LastConnectionType = strPtr(profile.ConnectionType)
		if profile.ScreenWidth > 0 {
			device.ScreenWidth = &profile.ScreenWidth
		}
		if profile.ScreenHeight > 0 {
			device.ScreenHeight = &profile.ScreenHeight
		}
		if profile.MemoryMB > 0 {
			device.MemoryMB = &profile.MemoryMB
		}
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO devices (
			id, user_id, device_name, public_key, last_active, created_at,
			platform, os_version, manufacturer, model, hardware_id,
			screen_width, screen_height, cpu_arch, memory_mb, app_version,
			bluetooth_address, wifi_direct_address, mesh_capable, mesh_discovery_id,
			last_ip_address, last_connection_type
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18, $19, $20,
			$21, $22
		)
	`, device.ID, device.UserID, device.DeviceName, device.PublicKey, device.LastActive, device.CreatedAt,
		device.Platform, device.OSVersion, device.Manufacturer, device.Model, device.HardwareID,
		device.ScreenWidth, device.ScreenHeight, device.CPUArch, device.MemoryMB, device.AppVersion,
		device.BluetoothAddress, device.WiFiDirectAddress, device.MeshCapable, device.MeshDiscoveryID,
		device.LastIPAddress, device.LastConnectionType)

	if err != nil {
		return nil, err
	}

	return device, nil
}

// UpdateDeviceProfile updates the profile information for a device
func (db *DB) UpdateDeviceProfile(ctx context.Context, deviceID uuid.UUID, profile *models.DeviceProfile) error {
	_, err := db.ExecContext(ctx, `
		UPDATE devices SET
			platform = COALESCE($2, platform),
			os_version = COALESCE($3, os_version),
			manufacturer = COALESCE($4, manufacturer),
			model = COALESCE($5, model),
			hardware_id = COALESCE($6, hardware_id),
			screen_width = COALESCE($7, screen_width),
			screen_height = COALESCE($8, screen_height),
			cpu_arch = COALESCE($9, cpu_arch),
			memory_mb = COALESCE($10, memory_mb),
			app_version = COALESCE($11, app_version),
			bluetooth_address = COALESCE($12, bluetooth_address),
			wifi_direct_address = COALESCE($13, wifi_direct_address),
			mesh_capable = $14,
			last_ip_address = COALESCE($15, last_ip_address),
			last_connection_type = COALESCE($16, last_connection_type),
			last_active = NOW()
		WHERE id = $1
	`, deviceID,
		strPtr(profile.Platform), strPtr(profile.OSVersion),
		strPtr(profile.Manufacturer), strPtr(profile.Model), strPtr(profile.HardwareID),
		intPtr(profile.ScreenWidth), intPtr(profile.ScreenHeight),
		strPtr(profile.CPUArch), intPtr(profile.MemoryMB), strPtr(profile.AppVersion),
		strPtr(profile.BluetoothAddress), strPtr(profile.WiFiDirectAddress),
		profile.MeshCapable,
		strPtr(profile.IPAddress), strPtr(profile.ConnectionType))
	return err
}

// UpdateMeshSettings updates the mesh networking settings for a device
func (db *DB) UpdateMeshSettings(ctx context.Context, deviceID uuid.UUID, enabled bool) error {
	_, err := db.ExecContext(ctx, `
		UPDATE devices SET mesh_enabled = $2, last_active = NOW()
		WHERE id = $1
	`, deviceID, enabled)
	return err
}

// Helper functions for nullable fields
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}

// GetDevicesByUserID retrieves all devices for a user with full profile
func (db *DB) GetDevicesByUserID(ctx context.Context, userID uuid.UUID) ([]*models.Device, error) {
	devices := []*models.Device{}
	err := db.SelectContext(ctx, &devices, `
		SELECT id, user_id, device_name, public_key, push_token, last_active, created_at,
		       platform, os_version, manufacturer, model, hardware_id,
		       screen_width, screen_height, cpu_arch, memory_mb, app_version,
		       bluetooth_address, wifi_direct_address, mesh_capable, mesh_enabled, mesh_discovery_id,
		       last_ip_address, last_connection_type
		FROM devices WHERE user_id = $1
		ORDER BY last_active DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	return devices, nil
}

// GetDeviceByID retrieves a device by ID with full profile
func (db *DB) GetDeviceByID(ctx context.Context, deviceID uuid.UUID) (*models.Device, error) {
	device := &models.Device{}
	err := db.GetContext(ctx, device, `
		SELECT id, user_id, device_name, public_key, push_token, last_active, created_at,
		       platform, os_version, manufacturer, model, hardware_id,
		       screen_width, screen_height, cpu_arch, memory_mb, app_version,
		       bluetooth_address, wifi_direct_address, mesh_capable, mesh_enabled, mesh_discovery_id,
		       last_ip_address, last_connection_type
		FROM devices WHERE id = $1
	`, deviceID)
	if err != nil {
		return nil, err
	}
	return device, nil
}

// GetMeshEnabledDevices retrieves all devices with mesh networking enabled
func (db *DB) GetMeshEnabledDevices(ctx context.Context) ([]*models.Device, error) {
	devices := []*models.Device{}
	err := db.SelectContext(ctx, &devices, `
		SELECT id, user_id, device_name, public_key, last_active,
		       platform, model, bluetooth_address, wifi_direct_address,
		       mesh_capable, mesh_enabled, mesh_discovery_id
		FROM devices
		WHERE mesh_enabled = TRUE AND mesh_capable = TRUE
		ORDER BY last_active DESC
	`)
	return devices, err
}

// UpdateDeviceLastActive updates the device's last active timestamp
func (db *DB) UpdateDeviceLastActive(ctx context.Context, deviceID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		UPDATE devices SET last_active = NOW() WHERE id = $1
	`, deviceID)
	return err
}

// Server key operations

// CreateServerKey creates a new server key pair for a user
func (db *DB) CreateServerKey(ctx context.Context, userID uuid.UUID) (*models.ServerKey, error) {
	keyPair, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}

	serverKey := &models.ServerKey{
		ID:         uuid.New(),
		UserID:     userID,
		PublicKey:  keyPair.PublicKey[:],
		PrivateKey: keyPair.PrivateKey[:],
		CreatedAt:  time.Now(),
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO server_keys (id, user_id, public_key, private_key, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, serverKey.ID, serverKey.UserID, serverKey.PublicKey, serverKey.PrivateKey, serverKey.CreatedAt)

	if err != nil {
		return nil, err
	}

	return serverKey, nil
}

// GetServerKeyByUserID retrieves the server key for a user
func (db *DB) GetServerKeyByUserID(ctx context.Context, userID uuid.UUID) (*models.ServerKey, error) {
	serverKey := &models.ServerKey{}
	err := db.GetContext(ctx, serverKey, `
		SELECT id, user_id, public_key, private_key, created_at, rotated_at
		FROM server_keys WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	return serverKey, nil
}

// Session operations

// CreateSession creates a new session for a user
func (db *DB) CreateSession(ctx context.Context, userID uuid.UUID, deviceID *uuid.UUID, tokenHash string, expiresAt time.Time) (*models.Session, error) {
	session := &models.Session{
		ID:        uuid.New(),
		UserID:    userID,
		DeviceID:  deviceID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, device_id, token_hash, expires_at, created_at, last_used)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, session.ID, session.UserID, session.DeviceID, session.TokenHash, session.ExpiresAt, session.CreatedAt, session.LastUsed)

	if err != nil {
		return nil, err
	}

	return session, nil
}

// GetSessionByTokenHash retrieves a session by token hash
func (db *DB) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*models.Session, error) {
	session := &models.Session{}
	err := db.GetContext(ctx, session, `
		SELECT id, user_id, device_id, token_hash, expires_at, created_at, last_used
		FROM sessions WHERE token_hash = $1 AND expires_at > NOW()
	`, tokenHash)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// UpdateSessionLastUsed updates the session's last used timestamp
func (db *DB) UpdateSessionLastUsed(ctx context.Context, sessionID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		UPDATE sessions SET last_used = NOW() WHERE id = $1
	`, sessionID)
	return err
}

// DeleteSession deletes a session
func (db *DB) DeleteSession(ctx context.Context, sessionID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
	return err
}

// Chat operations

// CreateChat creates a new chat
func (db *DB) CreateChat(ctx context.Context, name *string, isGroup bool, createdBy uuid.UUID, participantIDs []uuid.UUID) (*models.Chat, error) {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	chat := &models.Chat{
		ID:        uuid.New(),
		Name:      name,
		IsGroup:   isGroup,
		CreatedBy: &createdBy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO chats (id, name, is_group, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, chat.ID, chat.Name, chat.IsGroup, chat.CreatedBy, chat.CreatedAt, chat.UpdatedAt)
	if err != nil {
		return nil, err
	}

	// Add creator as admin with accepted status
	_, err = tx.ExecContext(ctx, `
		INSERT INTO chat_participants (id, chat_id, user_id, role, status, joined_at)
		VALUES ($1, $2, $3, 'admin', 'accepted', NOW())
	`, uuid.New(), chat.ID, createdBy)
	if err != nil {
		return nil, err
	}

	// Add other participants with pending status (they need to accept)
	for _, participantID := range participantIDs {
		if participantID != createdBy {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO chat_participants (id, chat_id, user_id, role, status, joined_at)
				VALUES ($1, $2, $3, 'member', 'pending', NOW())
			`, uuid.New(), chat.ID, participantID)
			if err != nil {
				return nil, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return chat, nil
}

// GetChatByID retrieves a chat by ID
func (db *DB) GetChatByID(ctx context.Context, chatID uuid.UUID) (*models.Chat, error) {
	chat := &models.Chat{}
	err := db.GetContext(ctx, chat, `
		SELECT id, name, is_group, created_by, created_at, updated_at
		FROM chats WHERE id = $1
	`, chatID)
	if err != nil {
		return nil, err
	}
	return chat, nil
}

// GetChatsByUserID retrieves all accepted chats for a user
func (db *DB) GetChatsByUserID(ctx context.Context, userID uuid.UUID) ([]*models.Chat, error) {
	chats := []*models.Chat{}
	err := db.SelectContext(ctx, &chats, `
		SELECT c.id, c.name, c.is_group, c.created_by, c.created_at, c.updated_at
		FROM chats c
		JOIN chat_participants cp ON cp.chat_id = c.id
		WHERE cp.user_id = $1 AND cp.left_at IS NULL AND cp.status = 'accepted'
		ORDER BY c.updated_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	return chats, nil
}

// GetChatParticipants retrieves all participants in a chat
func (db *DB) GetChatParticipants(ctx context.Context, chatID uuid.UUID) ([]*models.User, error) {
	users := []*models.User{}
	err := db.SelectContext(ctx, &users, `
		SELECT u.id, u.username, u.email, u.public_key, u.created_at, u.updated_at, u.last_seen
		FROM users u
		JOIN chat_participants cp ON cp.user_id = u.id
		WHERE cp.chat_id = $1 AND cp.left_at IS NULL
	`, chatID)
	if err != nil {
		return nil, err
	}
	return users, nil
}

// IsUserInChat checks if a user is an active participant in a chat (status = accepted)
func (db *DB) IsUserInChat(ctx context.Context, userID, chatID uuid.UUID) (bool, error) {
	var count int
	err := db.GetContext(ctx, &count, `
		SELECT COUNT(*) FROM chat_participants
		WHERE user_id = $1 AND chat_id = $2 AND left_at IS NULL AND status = 'accepted'
	`, userID, chatID)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// IsUserPendingInChat checks if a user has a pending invite for a chat
func (db *DB) IsUserPendingInChat(ctx context.Context, userID, chatID uuid.UUID) (bool, error) {
	var count int
	err := db.GetContext(ctx, &count, `
		SELECT COUNT(*) FROM chat_participants
		WHERE user_id = $1 AND chat_id = $2 AND left_at IS NULL AND status = 'pending'
	`, userID, chatID)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetUserChatStatus returns the status of a user in a chat (pending, accepted, left, etc.)
func (db *DB) GetUserChatStatus(ctx context.Context, userID, chatID uuid.UUID) (string, error) {
	var status string
	err := db.GetContext(ctx, &status, `
		SELECT status FROM chat_participants
		WHERE user_id = $1 AND chat_id = $2 AND left_at IS NULL
	`, userID, chatID)
	if err != nil {
		return "", err
	}
	return status, nil
}

// AddChatParticipant adds a participant to a chat with pending status
func (db *DB) AddChatParticipant(ctx context.Context, chatID, userID uuid.UUID, role string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO chat_participants (id, chat_id, user_id, role, status, joined_at)
		VALUES ($1, $2, $3, $4, 'pending', NOW())
		ON CONFLICT (chat_id, user_id) DO UPDATE SET left_at = NULL, role = $4, status = 'pending'
	`, uuid.New(), chatID, userID, role)
	return err
}

// GetPendingInvites retrieves all pending chat invites for a user
func (db *DB) GetPendingInvites(ctx context.Context, userID uuid.UUID) ([]*models.ChatInvite, error) {
	type inviteRow struct {
		ChatID      uuid.UUID  `db:"chat_id"`
		ChatName    *string    `db:"chat_name"`
		IsGroup     bool       `db:"is_group"`
		ChatCreated time.Time  `db:"chat_created"`
		InviterID   uuid.UUID  `db:"inviter_id"`
		InviterName string     `db:"inviter_name"`
		InviterEmail string    `db:"inviter_email"`
		InvitedAt   time.Time  `db:"invited_at"`
	}

	rows := []inviteRow{}
	err := db.SelectContext(ctx, &rows, `
		SELECT c.id as chat_id, c.name as chat_name, c.is_group, c.created_at as chat_created,
		       u.id as inviter_id, u.username as inviter_name, u.email as inviter_email,
		       cp.joined_at as invited_at
		FROM chat_participants cp
		JOIN chats c ON c.id = cp.chat_id
		JOIN users u ON u.id = c.created_by
		WHERE cp.user_id = $1 AND cp.status = 'pending' AND cp.left_at IS NULL
		ORDER BY cp.joined_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}

	invites := make([]*models.ChatInvite, len(rows))
	for i, row := range rows {
		invites[i] = &models.ChatInvite{
			Chat: &models.Chat{
				ID:        row.ChatID,
				Name:      row.ChatName,
				IsGroup:   row.IsGroup,
				CreatedAt: row.ChatCreated,
			},
			InvitedBy: &models.User{
				ID:       row.InviterID,
				Username: row.InviterName,
				Email:    row.InviterEmail,
			},
			InvitedAt: row.InvitedAt,
		}
	}
	return invites, nil
}

// AcceptChatInvite accepts a pending chat invitation
func (db *DB) AcceptChatInvite(ctx context.Context, userID, chatID uuid.UUID) error {
	result, err := db.ExecContext(ctx, `
		UPDATE chat_participants
		SET status = 'accepted'
		WHERE user_id = $1 AND chat_id = $2 AND status = 'pending'
	`, userID, chatID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeclineChatInvite declines a pending chat invitation
func (db *DB) DeclineChatInvite(ctx context.Context, userID, chatID uuid.UUID) error {
	result, err := db.ExecContext(ctx, `
		UPDATE chat_participants
		SET status = 'declined', left_at = NOW()
		WHERE user_id = $1 AND chat_id = $2 AND status = 'pending'
	`, userID, chatID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// LeaveChat marks a user as having left a chat
func (db *DB) LeaveChat(ctx context.Context, userID, chatID uuid.UUID) error {
	result, err := db.ExecContext(ctx, `
		UPDATE chat_participants
		SET left_at = NOW(), status = 'left', leave_reason = 'left'
		WHERE user_id = $1 AND chat_id = $2 AND left_at IS NULL
	`, userID, chatID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	// Check if chat is now orphaned (no active participants)
	db.updateChatStatusIfOrphaned(ctx, chatID)

	return nil
}

// SelfDestructChat flags a chat for self-destruction by a user
// This sets a flag in the participant record that others can see
func (db *DB) SelfDestructChat(ctx context.Context, userID, chatID uuid.UUID) error {
	result, err := db.ExecContext(ctx, `
		UPDATE chat_participants
		SET left_at = NOW(), status = 'self_destructed', leave_reason = 'self_destructed'
		WHERE user_id = $1 AND chat_id = $2 AND left_at IS NULL
	`, userID, chatID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	// Update chat status to self_destructed
	db.ExecContext(ctx, `
		UPDATE chats SET status = 'self_destructed' WHERE id = $1
	`, chatID)

	return nil
}

// updateChatStatusIfOrphaned checks if a chat has no active participants and marks it orphaned
func (db *DB) updateChatStatusIfOrphaned(ctx context.Context, chatID uuid.UUID) {
	var activeCount int
	db.GetContext(ctx, &activeCount, `
		SELECT COUNT(*) FROM chat_participants
		WHERE chat_id = $1 AND status = 'active'
	`, chatID)

	if activeCount == 0 {
		db.ExecContext(ctx, `
			UPDATE chats SET status = 'orphaned' WHERE id = $1 AND status = 'active'
		`, chatID)
	}
}

// GetSelfDestructedParticipants returns participants who have self-destructed from a chat
func (db *DB) GetSelfDestructedParticipants(ctx context.Context, chatID uuid.UUID) ([]*models.User, error) {
	users := []*models.User{}
	err := db.SelectContext(ctx, &users, `
		SELECT u.id, u.username, u.email, u.public_key, u.created_at, u.updated_at
		FROM users u
		JOIN chat_participants cp ON cp.user_id = u.id
		WHERE cp.chat_id = $1 AND cp.status = 'self_destructed'
	`, chatID)
	if err != nil {
		return nil, err
	}
	return users, nil
}

// Message operations

// CreateMessage creates a new message
func (db *DB) CreateMessage(ctx context.Context, chatID, senderID uuid.UUID, content, contentType string) (*models.Message, error) {
	message := &models.Message{
		ID:          uuid.New(),
		ChatID:      chatID,
		SenderID:    senderID,
		Content:     content,
		ContentType: contentType,
		CreatedAt:   time.Now(),
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO messages (id, chat_id, sender_id, content, content_type, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, message.ID, message.ChatID, message.SenderID, message.Content, message.ContentType, message.CreatedAt)

	if err != nil {
		return nil, err
	}

	// Update chat's updated_at
	db.ExecContext(ctx, `UPDATE chats SET updated_at = NOW() WHERE id = $1`, chatID)

	return message, nil
}

// GetMessagesByChat retrieves messages for a chat with pagination
func (db *DB) GetMessagesByChat(ctx context.Context, chatID uuid.UUID, limit, offset int) ([]*models.Message, error) {
	messages := []*models.Message{}
	err := db.SelectContext(ctx, &messages, `
		SELECT id, chat_id, sender_id, content, content_type, metadata, created_at, edited_at, deleted_at
		FROM messages
		WHERE chat_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, chatID, limit, offset)
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// GetMessageByID retrieves a message by ID
func (db *DB) GetMessageByID(ctx context.Context, messageID uuid.UUID) (*models.Message, error) {
	message := &models.Message{}
	err := db.GetContext(ctx, message, `
		SELECT id, chat_id, sender_id, content, content_type, metadata, created_at, edited_at, deleted_at
		FROM messages WHERE id = $1
	`, messageID)
	if err != nil {
		return nil, err
	}
	return message, nil
}

// SearchMessages searches messages using full-text search
func (db *DB) SearchMessages(ctx context.Context, userID uuid.UUID, query string, limit, offset int) ([]*models.Message, error) {
	messages := []*models.Message{}
	err := db.SelectContext(ctx, &messages, `
		SELECT m.id, m.chat_id, m.sender_id, m.content, m.content_type, m.created_at
		FROM messages m
		JOIN chat_participants cp ON cp.chat_id = m.chat_id
		WHERE cp.user_id = $1
		  AND cp.left_at IS NULL
		  AND m.deleted_at IS NULL
		  AND m.search_vector @@ plainto_tsquery('english', $2)
		ORDER BY ts_rank(m.search_vector, plainto_tsquery('english', $2)) DESC, m.created_at DESC
		LIMIT $3 OFFSET $4
	`, userID, query, limit, offset)
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// Message delivery operations

// CreateMessageDeliveries creates delivery records for all devices of chat participants
func (db *DB) CreateMessageDeliveries(ctx context.Context, messageID, senderID uuid.UUID, chatID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO message_deliveries (id, message_id, device_id)
		SELECT uuid_generate_v4(), $1, d.id
		FROM devices d
		JOIN chat_participants cp ON cp.user_id = d.user_id
		WHERE cp.chat_id = $2 AND cp.left_at IS NULL AND d.user_id != $3
	`, messageID, chatID, senderID)
	return err
}

// MarkMessageDelivered marks a message as delivered to a device
func (db *DB) MarkMessageDelivered(ctx context.Context, messageID, deviceID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		UPDATE message_deliveries SET delivered_at = NOW()
		WHERE message_id = $1 AND device_id = $2
	`, messageID, deviceID)
	return err
}

// MarkMessageRead marks a message as read by a device
func (db *DB) MarkMessageRead(ctx context.Context, messageID, deviceID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		UPDATE message_deliveries SET read_at = NOW(), delivered_at = COALESCE(delivered_at, NOW())
		WHERE message_id = $1 AND device_id = $2
	`, messageID, deviceID)
	return err
}

// GetPendingDeliveries retrieves messages pending delivery for a device
func (db *DB) GetPendingDeliveries(ctx context.Context, deviceID uuid.UUID) ([]*models.Message, error) {
	messages := []*models.Message{}
	err := db.SelectContext(ctx, &messages, `
		SELECT m.id, m.chat_id, m.sender_id, m.content, m.content_type, m.created_at
		FROM messages m
		JOIN message_deliveries md ON md.message_id = m.id
		WHERE md.device_id = $1 AND md.delivered_at IS NULL
		ORDER BY m.created_at ASC
	`, deviceID)
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// Find or create direct chat between two users
func (db *DB) FindOrCreateDirectChat(ctx context.Context, userID1, userID2 uuid.UUID) (*models.Chat, error) {
	// Try to find existing direct chat
	var chatID uuid.UUID
	err := db.GetContext(ctx, &chatID, `
		SELECT c.id FROM chats c
		JOIN chat_participants cp1 ON cp1.chat_id = c.id AND cp1.user_id = $1 AND cp1.left_at IS NULL
		JOIN chat_participants cp2 ON cp2.chat_id = c.id AND cp2.user_id = $2 AND cp2.left_at IS NULL
		WHERE c.is_group = false
		LIMIT 1
	`, userID1, userID2)

	if err == nil {
		return db.GetChatByID(ctx, chatID)
	}

	if err != sql.ErrNoRows {
		return nil, err
	}

	// Create new direct chat
	return db.CreateChat(ctx, nil, false, userID1, []uuid.UUID{userID2})
}

// ============================================
// Admin/Moderation Methods
// ============================================

// MessageWithUser includes sender username for admin display
type MessageWithUser struct {
	models.Message
	SenderUsername string `db:"sender_username"`
}

// GetAllUsers returns all users for admin view
func (db *DB) GetAllUsers(ctx context.Context) ([]*models.User, error) {
	users := []*models.User{}
	err := db.SelectContext(ctx, &users, `
		SELECT id, username, email, public_key, created_at, updated_at, last_seen
		FROM users
		ORDER BY created_at DESC
	`)
	return users, err
}

// UserWithStats includes user stats for admin view
type UserWithStats struct {
	models.User
	MessageCount   int `db:"message_count"`
	UniqueContacts int `db:"unique_contacts"`
	ChatCount      int `db:"chat_count"`
}

// GetAllUsersSorted returns all users with stats, sorted by the specified field
func (db *DB) GetAllUsersSorted(ctx context.Context, sortBy, order string) ([]*UserWithStats, error) {
	// Validate sort field to prevent SQL injection
	validFields := map[string]string{
		"username":        "u.username",
		"email":           "u.email",
		"created_at":      "u.created_at",
		"last_seen":       "u.last_seen",
		"message_count":   "message_count",
		"unique_contacts": "unique_contacts",
		"chat_count":      "chat_count",
	}

	sortField, ok := validFields[sortBy]
	if !ok {
		sortField = "u.created_at"
	}

	// Validate order
	if order != "asc" && order != "desc" {
		order = "desc"
	}

	users := []*UserWithStats{}
	query := fmt.Sprintf(`
		SELECT u.id, u.username, u.email, u.public_key, u.created_at, u.updated_at, u.last_seen,
		       COALESCE(msg.message_count, 0) as message_count,
		       COALESCE(contacts.unique_contacts, 0) as unique_contacts,
		       COALESCE(chats.chat_count, 0) as chat_count
		FROM users u
		LEFT JOIN (
			SELECT sender_id, COUNT(*) as message_count
			FROM messages WHERE deleted_at IS NULL
			GROUP BY sender_id
		) msg ON msg.sender_id = u.id
		LEFT JOIN (
			SELECT cp1.user_id, COUNT(DISTINCT cp2.user_id) as unique_contacts
			FROM chat_participants cp1
			JOIN chat_participants cp2 ON cp1.chat_id = cp2.chat_id AND cp1.user_id != cp2.user_id
			GROUP BY cp1.user_id
		) contacts ON contacts.user_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(DISTINCT chat_id) as chat_count
			FROM chat_participants
			GROUP BY user_id
		) chats ON chats.user_id = u.id
		ORDER BY %s %s NULLS LAST
	`, sortField, order)

	err := db.SelectContext(ctx, &users, query)
	return users, err
}

// GetAllChats returns all chats for admin view
func (db *DB) GetAllChats(ctx context.Context) ([]*models.Chat, error) {
	chats := []*models.Chat{}
	err := db.SelectContext(ctx, &chats, `
		SELECT id, name, is_group, created_at, updated_at
		FROM chats
		ORDER BY created_at DESC
	`)
	return chats, err
}

// GetAllMessages returns paginated messages for admin view
func (db *DB) GetAllMessages(ctx context.Context, limit, offset int) ([]*models.Message, error) {
	messages := []*models.Message{}
	err := db.SelectContext(ctx, &messages, `
		SELECT id, chat_id, sender_id, content, content_type, created_at, edited_at, deleted_at
		FROM messages
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	return messages, err
}

// GetRecentMessages returns messages from the last duration with sender info
func (db *DB) GetRecentMessages(ctx context.Context, duration time.Duration, limit int) ([]*MessageWithUser, error) {
	messages := []*MessageWithUser{}
	since := time.Now().Add(-duration)
	err := db.SelectContext(ctx, &messages, `
		SELECT m.id, m.chat_id, m.sender_id, m.content, m.content_type, m.created_at,
		       m.edited_at, m.deleted_at, u.username as sender_username
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.deleted_at IS NULL AND m.created_at > $1
		ORDER BY m.created_at DESC
		LIMIT $2
	`, since, limit)
	return messages, err
}

// GetMessagesByUserID returns messages sent by a specific user
func (db *DB) GetMessagesByUserID(ctx context.Context, userID uuid.UUID, limit int) ([]*models.Message, error) {
	messages := []*models.Message{}
	err := db.SelectContext(ctx, &messages, `
		SELECT id, chat_id, sender_id, content, content_type, created_at, edited_at, deleted_at
		FROM messages
		WHERE sender_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2
	`, userID, limit)
	return messages, err
}

// GetChatMessageCount returns the number of messages in a chat
func (db *DB) GetChatMessageCount(ctx context.Context, chatID uuid.UUID) (int, error) {
	var count int
	err := db.GetContext(ctx, &count, `
		SELECT COUNT(*) FROM messages WHERE chat_id = $1 AND deleted_at IS NULL
	`, chatID)
	return count, err
}

// GetUserMessageCount returns the total number of messages sent by a user
func (db *DB) GetUserMessageCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := db.GetContext(ctx, &count, `
		SELECT COUNT(*) FROM messages WHERE sender_id = $1 AND deleted_at IS NULL
	`, userID)
	return count, err
}

// SearchAllMessages performs full-text search across all messages (admin)
func (db *DB) SearchAllMessages(ctx context.Context, query string, limit int) ([]*MessageWithUser, error) {
	messages := []*MessageWithUser{}
	err := db.SelectContext(ctx, &messages, `
		SELECT m.id, m.chat_id, m.sender_id, m.content, m.content_type, m.created_at,
		       m.edited_at, m.deleted_at, u.username as sender_username
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.deleted_at IS NULL
		AND m.search_vector @@ plainto_tsquery('english', $1)
		ORDER BY ts_rank(m.search_vector, plainto_tsquery('english', $1)) DESC, m.created_at DESC
		LIMIT $2
	`, query, limit)
	return messages, err
}

// DeleteMessage soft-deletes a message
func (db *DB) DeleteMessage(ctx context.Context, messageID uuid.UUID) error {
	_, err := db.ExecContext(ctx, `
		UPDATE messages SET deleted_at = NOW() WHERE id = $1
	`, messageID)
	return err
}

// DeleteUser deletes a user and their associated data
func (db *DB) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete sessions
	tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)

	// Delete devices
	tx.ExecContext(ctx, `DELETE FROM devices WHERE user_id = $1`, userID)

	// Delete server keys
	tx.ExecContext(ctx, `DELETE FROM server_keys WHERE user_id = $1`, userID)

	// Remove from chat participants
	tx.ExecContext(ctx, `UPDATE chat_participants SET left_at = NOW() WHERE user_id = $1`, userID)

	// Soft delete messages (keep for audit trail)
	tx.ExecContext(ctx, `UPDATE messages SET deleted_at = NOW() WHERE sender_id = $1`, userID)

	// Delete the user
	_, err = tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ============================================
// Enhanced Admin/Moderation Methods
// ============================================

// ChatWithStatus includes chat status information for admin view
type ChatWithStatus struct {
	models.Chat
	Status string `db:"status"`
}

// ParticipantWithStatus includes participant's status in the chat
type ParticipantWithStatus struct {
	models.User
	Status      string     `db:"status"`
	LeaveReason *string    `db:"leave_reason"`
	JoinedAt    time.Time  `db:"joined_at"`
	LeftAt      *time.Time `db:"left_at"`
}

// GetAllChatsWithStatus returns all chats with their status
func (db *DB) GetAllChatsWithStatus(ctx context.Context) ([]*ChatWithStatus, error) {
	chats := []*ChatWithStatus{}
	err := db.SelectContext(ctx, &chats, `
		SELECT id, name, is_group, created_at, updated_at, COALESCE(status, 'active') as status
		FROM chats
		ORDER BY
			CASE status
				WHEN 'active' THEN 0
				WHEN 'orphaned' THEN 1
				WHEN 'self_destructed' THEN 2
				ELSE 3
			END,
			updated_at DESC
	`)
	return chats, err
}

// ChatWithStats includes chat stats for admin view
type ChatWithStats struct {
	ID               uuid.UUID  `db:"id"`
	Name             *string    `db:"name"`
	IsGroup          bool       `db:"is_group"`
	Status           string     `db:"status"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`
	MessageCount     int        `db:"message_count"`
	ParticipantCount int        `db:"participant_count"`
	LastMessageAt    *time.Time `db:"last_message_at"`
}

// GetAllChatsWithStatsSorted returns all chats with stats, sorted by the specified field
func (db *DB) GetAllChatsWithStatsSorted(ctx context.Context, sortBy, order string) ([]*ChatWithStats, error) {
	// Validate sort field to prevent SQL injection
	validFields := map[string]string{
		"name":              "c.name",
		"status":            "c.status",
		"is_group":          "c.is_group",
		"created_at":        "c.created_at",
		"updated_at":        "c.updated_at",
		"message_count":     "message_count",
		"participant_count": "participant_count",
		"last_message_at":   "last_message_at",
	}

	sortField, ok := validFields[sortBy]
	if !ok {
		sortField = "c.created_at"
	}

	// Validate order
	if order != "asc" && order != "desc" {
		order = "desc"
	}

	chats := []*ChatWithStats{}
	query := fmt.Sprintf(`
		SELECT c.id, c.name, c.is_group, COALESCE(c.status, 'active') as status,
		       c.created_at, c.updated_at,
		       COALESCE(msg.message_count, 0) as message_count,
		       COALESCE(parts.participant_count, 0) as participant_count,
		       msg.last_message_at
		FROM chats c
		LEFT JOIN (
			SELECT chat_id, COUNT(*) as message_count, MAX(created_at) as last_message_at
			FROM messages WHERE deleted_at IS NULL
			GROUP BY chat_id
		) msg ON msg.chat_id = c.id
		LEFT JOIN (
			SELECT chat_id, COUNT(*) as participant_count
			FROM chat_participants WHERE left_at IS NULL
			GROUP BY chat_id
		) parts ON parts.chat_id = c.id
		ORDER BY %s %s NULLS LAST
	`, sortField, order)

	err := db.SelectContext(ctx, &chats, query)
	return chats, err
}

// GetChatsByStatus returns chats filtered by status
func (db *DB) GetChatsByStatus(ctx context.Context, status string) ([]*ChatWithStatus, error) {
	chats := []*ChatWithStatus{}
	err := db.SelectContext(ctx, &chats, `
		SELECT id, name, is_group, created_at, updated_at, COALESCE(status, 'active') as status
		FROM chats
		WHERE COALESCE(status, 'active') = $1
		ORDER BY updated_at DESC
	`, status)
	return chats, err
}

// GetAllChatParticipants returns all participants (current and former) for a chat
func (db *DB) GetAllChatParticipants(ctx context.Context, chatID uuid.UUID) ([]*ParticipantWithStatus, error) {
	participants := []*ParticipantWithStatus{}
	err := db.SelectContext(ctx, &participants, `
		SELECT u.id, u.username, u.email, u.public_key, u.created_at, u.updated_at, u.last_seen,
		       COALESCE(cp.status, 'active') as status, cp.leave_reason, cp.joined_at, cp.left_at
		FROM users u
		JOIN chat_participants cp ON cp.user_id = u.id
		WHERE cp.chat_id = $1
		ORDER BY
			CASE cp.status
				WHEN 'active' THEN 0
				WHEN 'accepted' THEN 0
				WHEN 'left' THEN 1
				WHEN 'self_destructed' THEN 2
				ELSE 3
			END,
			cp.joined_at
	`, chatID)
	return participants, err
}

// GetUserUniqueCommunicationCount returns the number of unique users this user has communicated with
func (db *DB) GetUserUniqueCommunicationCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := db.GetContext(ctx, &count, `
		SELECT COUNT(DISTINCT cp2.user_id)
		FROM chat_participants cp1
		JOIN chat_participants cp2 ON cp1.chat_id = cp2.chat_id
		WHERE cp1.user_id = $1
		  AND cp2.user_id != $1
	`, userID)
	return count, err
}

// UserCommunicationPartner represents a user's communication partner with chat info
type UserCommunicationPartner struct {
	UserID       uuid.UUID  `db:"user_id"`
	Username     string     `db:"username"`
	Email        string     `db:"email"`
	ChatCount    int        `db:"chat_count"`
	MessageCount int        `db:"message_count"`
	LastMessage  *time.Time `db:"last_message"`
}

// GetUserCommunicationPartners returns all users this user has communicated with
func (db *DB) GetUserCommunicationPartners(ctx context.Context, userID uuid.UUID) ([]*UserCommunicationPartner, error) {
	partners := []*UserCommunicationPartner{}
	err := db.SelectContext(ctx, &partners, `
		SELECT
			u.id as user_id,
			u.username,
			u.email,
			COUNT(DISTINCT cp1.chat_id) as chat_count,
			COALESCE(SUM(msg_counts.msg_count), 0)::int as message_count,
			MAX(msg_counts.last_msg) as last_message
		FROM users u
		JOIN chat_participants cp2 ON cp2.user_id = u.id
		JOIN chat_participants cp1 ON cp1.chat_id = cp2.chat_id AND cp1.user_id = $1
		LEFT JOIN (
			SELECT chat_id, COUNT(*) as msg_count, MAX(created_at) as last_msg
			FROM messages
			WHERE deleted_at IS NULL
			GROUP BY chat_id
		) msg_counts ON msg_counts.chat_id = cp1.chat_id
		WHERE u.id != $1
		GROUP BY u.id, u.username, u.email
		ORDER BY last_message DESC NULLS LAST
	`, userID)
	return partners, err
}

// GetAllChatsForUser returns all chats a user has ever been in (including left/self-destructed)
func (db *DB) GetAllChatsForUser(ctx context.Context, userID uuid.UUID) ([]*ChatWithStatus, error) {
	chats := []*ChatWithStatus{}
	err := db.SelectContext(ctx, &chats, `
		SELECT c.id, c.name, c.is_group, c.created_at, c.updated_at, COALESCE(c.status, 'active') as status
		FROM chats c
		JOIN chat_participants cp ON cp.chat_id = c.id
		WHERE cp.user_id = $1
		ORDER BY c.updated_at DESC
	`, userID)
	return chats, err
}

// ChatSearchResult represents a chat in search results
type ChatSearchResult struct {
	ChatID              uuid.UUID  `db:"chat_id"`
	ChatName            *string    `db:"chat_name"`
	IsGroup             bool       `db:"is_group"`
	Status              string     `db:"status"`
	MessageCount        int        `db:"message_count"`
	ParticipantCount    int        `db:"participant_count"`
	CreatedAt           time.Time  `db:"created_at"`
	LastMessageAt       *time.Time `db:"last_message_at"`
	ParticipantNames    string     `db:"participant_names"`
	FormerParticipants  string     `db:"former_participants"`
}

// SearchChatsAdvanced performs advanced chat search with multiple criteria
func (db *DB) SearchChatsAdvanced(ctx context.Context, params ChatSearchParams) ([]*ChatSearchResult, error) {
	query := `
		WITH chat_stats AS (
			SELECT
				c.id as chat_id,
				c.name as chat_name,
				c.is_group,
				COALESCE(c.status, 'active') as status,
				c.created_at,
				COUNT(DISTINCT m.id) FILTER (WHERE m.deleted_at IS NULL) as message_count,
				COUNT(DISTINCT cp.user_id) as participant_count,
				MAX(m.created_at) as last_message_at,
				COALESCE(STRING_AGG(DISTINCT CASE WHEN cp.left_at IS NULL THEN u.username END, ', '), '') as participant_names,
				COALESCE(STRING_AGG(DISTINCT CASE WHEN cp.left_at IS NOT NULL THEN u.username || ' (' || COALESCE(cp.leave_reason, 'left') || ')' END, ', '), '') as former_participants
			FROM chats c
			LEFT JOIN chat_participants cp ON cp.chat_id = c.id
			LEFT JOIN users u ON u.id = cp.user_id
			LEFT JOIN messages m ON m.chat_id = c.id
			GROUP BY c.id, c.name, c.is_group, c.status, c.created_at
		)
		SELECT * FROM chat_stats
		WHERE 1=1
	`
	args := []interface{}{}
	argNum := 1

	// Filter by status
	if params.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argNum)
		args = append(args, params.Status)
		argNum++
	}

	// Filter by participant (current or former)
	if params.ParticipantID != uuid.Nil {
		query += fmt.Sprintf(` AND chat_id IN (
			SELECT chat_id FROM chat_participants WHERE user_id = $%d
		)`, argNum)
		args = append(args, params.ParticipantID)
		argNum++
	}

	// Filter by participant username search
	if params.ParticipantName != "" {
		query += fmt.Sprintf(` AND (
			participant_names ILIKE $%d OR former_participants ILIKE $%d
		)`, argNum, argNum)
		args = append(args, "%"+params.ParticipantName+"%")
		argNum++
	}

	// Filter by message content search
	if params.MessageQuery != "" {
		query += fmt.Sprintf(` AND chat_id IN (
			SELECT DISTINCT chat_id FROM messages
			WHERE deleted_at IS NULL
			AND search_vector @@ plainto_tsquery('english', $%d)
		)`, argNum)
		args = append(args, params.MessageQuery)
		argNum++
	}

	query += " ORDER BY last_message_at DESC NULLS LAST LIMIT 100"

	results := []*ChatSearchResult{}
	err := db.SelectContext(ctx, &results, query, args...)
	return results, err
}

// ChatSearchParams contains search parameters for advanced chat search
type ChatSearchParams struct {
	Status          string    // Filter by chat status: active, orphaned, self_destructed
	ParticipantID   uuid.UUID // Filter by participant (current or former)
	ParticipantName string    // Filter by participant username (partial match)
	MessageQuery    string    // Full-text search in messages
}

// GetChatsInvolvingUser returns all chats where a specific user is or was a participant
// This is useful to find all conversations a user of interest appears in
func (db *DB) GetChatsInvolvingUser(ctx context.Context, userID uuid.UUID) ([]*ChatSearchResult, error) {
	results := []*ChatSearchResult{}
	err := db.SelectContext(ctx, &results, `
		WITH chat_stats AS (
			SELECT
				c.id as chat_id,
				c.name as chat_name,
				c.is_group,
				COALESCE(c.status, 'active') as status,
				c.created_at,
				COUNT(DISTINCT m.id) FILTER (WHERE m.deleted_at IS NULL) as message_count,
				COUNT(DISTINCT cp.user_id) as participant_count,
				MAX(m.created_at) as last_message_at,
				COALESCE(STRING_AGG(DISTINCT CASE WHEN cp.left_at IS NULL THEN u.username END, ', '), '') as participant_names,
				COALESCE(STRING_AGG(DISTINCT CASE WHEN cp.left_at IS NOT NULL THEN u.username || ' (' || COALESCE(cp.leave_reason, 'left') || ')' END, ', '), '') as former_participants
			FROM chats c
			JOIN chat_participants target_cp ON target_cp.chat_id = c.id AND target_cp.user_id = $1
			LEFT JOIN chat_participants cp ON cp.chat_id = c.id
			LEFT JOIN users u ON u.id = cp.user_id
			LEFT JOIN messages m ON m.chat_id = c.id
			GROUP BY c.id, c.name, c.is_group, c.status, c.created_at
		)
		SELECT * FROM chat_stats
		ORDER BY last_message_at DESC NULLS LAST
	`, userID)
	return results, err
}
