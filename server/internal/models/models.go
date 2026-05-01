package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// User represents a registered user
type User struct {
	ID           uuid.UUID  `db:"id" json:"id"`
	Username     string     `db:"username" json:"username"`
	Email        string     `db:"email" json:"email"`
	PasswordHash string     `db:"password_hash" json:"-"`
	PublicKey    []byte     `db:"public_key" json:"public_key"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
	LastSeen     *time.Time `db:"last_seen" json:"last_seen,omitempty"`
}

// Device represents a user's device
type Device struct {
	ID         uuid.UUID  `db:"id" json:"id"`
	UserID     uuid.UUID  `db:"user_id" json:"user_id"`
	DeviceName string     `db:"device_name" json:"device_name"`
	PublicKey  []byte     `db:"public_key" json:"public_key"`
	PushToken  *string    `db:"push_token" json:"push_token,omitempty"`
	LastActive time.Time  `db:"last_active" json:"last_active"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`

	// Device Profile
	Platform     *string `db:"platform" json:"platform,omitempty"`
	OSVersion    *string `db:"os_version" json:"os_version,omitempty"`
	Manufacturer *string `db:"manufacturer" json:"manufacturer,omitempty"`
	Model        *string `db:"model" json:"model,omitempty"`
	HardwareID   *string `db:"hardware_id" json:"hardware_id,omitempty"`
	ScreenWidth  *int    `db:"screen_width" json:"screen_width,omitempty"`
	ScreenHeight *int    `db:"screen_height" json:"screen_height,omitempty"`
	CPUArch      *string `db:"cpu_arch" json:"cpu_arch,omitempty"`
	MemoryMB     *int    `db:"memory_mb" json:"memory_mb,omitempty"`
	AppVersion   *string `db:"app_version" json:"app_version,omitempty"`

	// Mesh Networking
	BluetoothAddress  *string    `db:"bluetooth_address" json:"bluetooth_address,omitempty"`
	WiFiDirectAddress *string    `db:"wifi_direct_address" json:"wifi_direct_address,omitempty"`
	MeshCapable       bool       `db:"mesh_capable" json:"mesh_capable"`
	MeshEnabled       bool       `db:"mesh_enabled" json:"mesh_enabled"`
	MeshDiscoveryID   *uuid.UUID `db:"mesh_discovery_id" json:"mesh_discovery_id,omitempty"`

	// Network State
	LastIPAddress      *string `db:"last_ip_address" json:"last_ip_address,omitempty"`
	LastConnectionType *string `db:"last_connection_type" json:"last_connection_type,omitempty"`
}

// DeviceProfile contains device information sent from clients
type DeviceProfile struct {
	Platform     string `json:"platform"`
	OSVersion    string `json:"os_version"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	HardwareID   string `json:"hardware_id,omitempty"`
	ScreenWidth  int    `json:"screen_width,omitempty"`
	ScreenHeight int    `json:"screen_height,omitempty"`
	CPUArch      string `json:"cpu_arch,omitempty"`
	MemoryMB     int    `json:"memory_mb,omitempty"`
	AppVersion   string `json:"app_version,omitempty"`

	// Mesh networking identifiers
	BluetoothAddress  string `json:"bluetooth_address,omitempty"`
	WiFiDirectAddress string `json:"wifi_direct_address,omitempty"`
	MeshCapable       bool   `json:"mesh_capable"`

	// Network info
	IPAddress      string `json:"ip_address,omitempty"`
	ConnectionType string `json:"connection_type,omitempty"`
}

// MeshPeer represents a discovered nearby device for mesh networking
type MeshPeer struct {
	ID                  uuid.UUID  `db:"id" json:"id"`
	DeviceID            uuid.UUID  `db:"device_id" json:"device_id"`
	PeerDeviceID        *uuid.UUID `db:"peer_device_id" json:"peer_device_id,omitempty"`
	PeerDiscoveryID     uuid.UUID  `db:"peer_discovery_id" json:"peer_discovery_id"`
	PeerBluetoothAddr   *string    `db:"peer_bluetooth_address" json:"peer_bluetooth_address,omitempty"`
	PeerWiFiDirectAddr  *string    `db:"peer_wifi_direct_address" json:"peer_wifi_direct_address,omitempty"`
	SignalStrength      *int       `db:"signal_strength" json:"signal_strength,omitempty"`
	LastSeen            time.Time  `db:"last_seen" json:"last_seen"`
	ConnectionQuality   *string    `db:"connection_quality" json:"connection_quality,omitempty"`
}

// ServerKey represents server-generated keys for client-server encryption
type ServerKey struct {
	ID         uuid.UUID  `db:"id" json:"id"`
	UserID     uuid.UUID  `db:"user_id" json:"user_id"`
	PublicKey  []byte     `db:"public_key" json:"public_key"`
	PrivateKey []byte     `db:"private_key" json:"-"` // Never expose
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	RotatedAt  *time.Time `db:"rotated_at" json:"rotated_at,omitempty"`
}

// Chat represents a conversation (1:1 or group)
type Chat struct {
	ID        uuid.UUID  `db:"id" json:"id"`
	Name      *string    `db:"name" json:"name,omitempty"`
	IsGroup   bool       `db:"is_group" json:"is_group"`
	CreatedBy *uuid.UUID `db:"created_by" json:"created_by,omitempty"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt time.Time  `db:"updated_at" json:"updated_at"`
}

// ChatParticipant represents a user's membership in a chat
type ChatParticipant struct {
	ID       uuid.UUID  `db:"id" json:"id"`
	ChatID   uuid.UUID  `db:"chat_id" json:"chat_id"`
	UserID   uuid.UUID  `db:"user_id" json:"user_id"`
	Role     string     `db:"role" json:"role"`
	Status   string     `db:"status" json:"status"` // pending, accepted, declined
	JoinedAt time.Time  `db:"joined_at" json:"joined_at"`
	LeftAt   *time.Time `db:"left_at" json:"left_at,omitempty"`
}

// ChatInvite represents a pending chat invitation for API responses
type ChatInvite struct {
	Chat       *Chat  `json:"chat"`
	InvitedBy  *User  `json:"invited_by"`
	InvitedAt  time.Time `json:"invited_at"`
}

// Message represents a chat message
type Message struct {
	ID          uuid.UUID        `db:"id" json:"id"`
	ChatID      uuid.UUID        `db:"chat_id" json:"chat_id"`
	SenderID    uuid.UUID        `db:"sender_id" json:"sender_id"`
	Content     string           `db:"content" json:"content"`
	ContentType string           `db:"content_type" json:"content_type"`
	Metadata    *json.RawMessage `db:"metadata" json:"metadata,omitempty"`
	CreatedAt   time.Time        `db:"created_at" json:"created_at"`
	EditedAt    *time.Time       `db:"edited_at" json:"edited_at,omitempty"`
	DeletedAt   *time.Time       `db:"deleted_at" json:"deleted_at,omitempty"`
}

// MessageDelivery tracks message delivery to devices
type MessageDelivery struct {
	ID          uuid.UUID  `db:"id" json:"id"`
	MessageID   uuid.UUID  `db:"message_id" json:"message_id"`
	DeviceID    uuid.UUID  `db:"device_id" json:"device_id"`
	DeliveredAt *time.Time `db:"delivered_at" json:"delivered_at,omitempty"`
	ReadAt      *time.Time `db:"read_at" json:"read_at,omitempty"`
}

// Session represents an authenticated session
type Session struct {
	ID        uuid.UUID  `db:"id" json:"id"`
	UserID    uuid.UUID  `db:"user_id" json:"user_id"`
	DeviceID  *uuid.UUID `db:"device_id" json:"device_id,omitempty"`
	TokenHash string     `db:"token_hash" json:"-"`
	ExpiresAt time.Time  `db:"expires_at" json:"expires_at"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	LastUsed  time.Time  `db:"last_used" json:"last_used"`
}

// API Request/Response types

type RegisterRequest struct {
	Username      string         `json:"username" binding:"required,min=3,max=50"`
	Email         string         `json:"email" binding:"required,email"`
	Password      string         `json:"password" binding:"required,min=8"`
	PublicKey     []byte         `json:"public_key" binding:"required"`
	DeviceName    string         `json:"device_name" binding:"required"`
	DeviceKey     []byte         `json:"device_key" binding:"required"`
	DeviceProfile *DeviceProfile `json:"device_profile,omitempty"`
}

type RegisterResponse struct {
	User            *User   `json:"user"`
	Device          *Device `json:"device"`
	Token           string  `json:"token"`
	ServerPublicKey []byte  `json:"server_public_key"`
}

type LoginRequest struct {
	Email         string         `json:"email" binding:"required,email"`
	Password      string         `json:"password" binding:"required"`
	DeviceName    string         `json:"device_name" binding:"required"`
	DeviceKey     []byte         `json:"device_key" binding:"required"`
	DeviceProfile *DeviceProfile `json:"device_profile,omitempty"`
}

// UpdateDeviceProfileRequest for updating device profile after login
type UpdateDeviceProfileRequest struct {
	DeviceProfile *DeviceProfile `json:"device_profile" binding:"required"`
}

// UpdateMeshSettingsRequest for toggling mesh network mode
type UpdateMeshSettingsRequest struct {
	MeshEnabled bool `json:"mesh_enabled"`
}

type LoginResponse struct {
	User            *User   `json:"user"`
	Device          *Device `json:"device"`
	Token           string  `json:"token"`
	ServerPublicKey []byte  `json:"server_public_key"`
}

type CreateChatRequest struct {
	Name         *string     `json:"name,omitempty"`
	IsGroup      bool        `json:"is_group"`
	Participants []uuid.UUID `json:"participants" binding:"required,min=1"`
}

type SendMessageRequest struct {
	ChatID          uuid.UUID `json:"chat_id" binding:"required"`
	EncryptedContent []byte   `json:"encrypted_content" binding:"required"`
	ContentType     string    `json:"content_type"`
	Nonce           []byte    `json:"nonce" binding:"required"`
}

type ChatListItem struct {
	Chat         *Chat    `json:"chat"`
	LastMessage  *Message `json:"last_message,omitempty"`
	UnreadCount  int      `json:"unread_count"`
	Participants []*User  `json:"participants"`
}

// WebSocket message types

type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type WSMessageSend struct {
	ChatID           uuid.UUID `json:"chat_id"`
	EncryptedContent []byte    `json:"encrypted_content"`
	ContentType      string    `json:"content_type"`
	Nonce            []byte    `json:"nonce"`
}

type WSMessageReceive struct {
	Message          *Message `json:"message"`
	EncryptedContent []byte   `json:"encrypted_content"`
	Nonce            []byte   `json:"nonce"`
}

type WSTypingIndicator struct {
	ChatID uuid.UUID `json:"chat_id"`
	UserID uuid.UUID `json:"user_id"`
	Typing bool      `json:"typing"`
}

type WSPresenceUpdate struct {
	UserID uuid.UUID `json:"user_id"`
	Online bool      `json:"online"`
}

// WebSocket notification for new chat/invite
type WSChatInvite struct {
	Chat      *Chat `json:"chat"`
	InvitedBy *User `json:"invited_by"`
}

// WebSocket notification when invite is accepted
type WSChatAccepted struct {
	ChatID   uuid.UUID `json:"chat_id"`
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
}

// WebSocket notification when user leaves a chat
type WSUserLeft struct {
	ChatID   uuid.UUID `json:"chat_id"`
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
}

// WebSocket notification when user self-destructs a chat
type WSUserSelfDestructed struct {
	ChatID   uuid.UUID `json:"chat_id"`
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
}

// System message content types
const (
	ContentTypeText           = "text"
	ContentTypeSystem         = "system"
	ContentTypeUserLeft       = "system.user_left"
	ContentTypeSelfDestructed = "system.self_destructed"
)
