package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"e2e-chat/internal/auth"
	"e2e-chat/internal/crypto"
	"e2e-chat/internal/db"
	"e2e-chat/internal/models"
	"e2e-chat/internal/ratelimit"
	"e2e-chat/internal/ws"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// mustMarshal marshals data to JSON, panicking on error (should never happen with valid models)
func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// Allowed WebSocket origins (should match CORS config)
var allowedWSOrigins = map[string]bool{
	"http://localhost:3000":    true, // Development frontend
	"http://localhost:8080":    true, // Local testing
	"https://chat.example.com": true, // Production (update this)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		// Allow requests with no origin (e.g., native clients)
		if origin == "" {
			return true
		}
		return allowedWSOrigins[origin]
	},
}

// Handler contains all HTTP handlers
type Handler struct {
	db          *db.DB
	auth        *auth.Service
	hub         *ws.Hub
	rateLimiter *ratelimit.RateLimiter
}

// NewHandler creates a new Handler
func NewHandler(database *db.DB, authService *auth.Service, hub *ws.Hub, rl *ratelimit.RateLimiter) *Handler {
	return &Handler{
		db:          database,
		auth:        authService,
		hub:         hub,
		rateLimiter: rl,
	}
}

// SetupRoutes configures the router
func (h *Handler) SetupRoutes(r *gin.Engine) {
	// Public routes with auth rate limiting (stricter limits for login/register)
	api := r.Group("/api")
	{
		authGroup := api.Group("/auth")
		authGroup.Use(h.rateLimiter.AuthMiddleware()) // 5 requests per minute per IP
		{
			authGroup.POST("/register", h.Register)
			authGroup.POST("/login", h.Login)
		}
	}

	// Protected routes with API rate limiting (60 requests per minute per IP)
	protected := api.Group("")
	protected.Use(h.auth.Middleware())
	protected.Use(h.rateLimiter.APIMiddleware())
	{
		// Auth
		protected.POST("/auth/logout", h.Logout)
		protected.POST("/auth/device", h.RegisterDevice)

		// Device profile
		protected.PUT("/device/profile", h.UpdateDeviceProfile)
		protected.PUT("/device/mesh", h.UpdateMeshSettings)
		protected.GET("/device/profile", h.GetDeviceProfile)

		// Users
		protected.GET("/users/search", h.SearchUsers)
		protected.GET("/users/:id", h.GetUser)
		protected.GET("/users/:id/keys", h.GetUserKeys)

		// Chats
		protected.GET("/chats", h.ListChats)
		protected.POST("/chats", h.CreateChat)
		protected.GET("/chats/:id", h.GetChat)
		protected.GET("/chats/:id/messages", h.GetMessages)
		protected.POST("/chats/:id/participants", h.AddParticipant)
		protected.POST("/chats/:id/leave", h.LeaveChat)
		protected.POST("/chats/:id/self-destruct", h.SelfDestructChat)

		// Chat invites
		protected.GET("/invites", h.GetPendingInvites)
		protected.POST("/invites/:id/accept", h.AcceptInvite)
		protected.POST("/invites/:id/decline", h.DeclineInvite)

		// Messages
		protected.GET("/messages/search", h.SearchMessages)
	}

	// WebSocket (protected) with WebSocket rate limiting (10 connections per minute per IP)
	r.GET("/ws", h.rateLimiter.WebSocketMiddleware(), h.auth.Middleware(), h.HandleWebSocket)
}

// Auth handlers

func (h *Handler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.auth.Register(c.Request.Context(), &req)
	if err != nil {
		if err == auth.ErrUserExists {
			c.JSON(http.StatusConflict, gin.H{"error": "user already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, resp)
}

func (h *Handler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.auth.Login(c.Request.Context(), &req)
	if err != nil {
		if err == auth.ErrInvalidCredentials {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) Logout(c *gin.Context) {
	session := auth.GetSessionFromContext(c)
	if session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	if err := h.auth.Logout(c.Request.Context(), session.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

func (h *Handler) RegisterDevice(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)

	var req struct {
		DeviceName string `json:"device_name" binding:"required"`
		DeviceKey  []byte `json:"device_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	device, err := h.db.CreateDevice(c.Request.Context(), userID, req.DeviceName, req.DeviceKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, device)
}

// Device profile handlers

func (h *Handler) UpdateDeviceProfile(c *gin.Context) {
	deviceID := auth.GetDeviceIDFromContext(c)
	if deviceID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no device in session"})
		return
	}

	var req models.UpdateDeviceProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get client IP and add to profile
	if req.DeviceProfile.IPAddress == "" {
		req.DeviceProfile.IPAddress = c.ClientIP()
	}

	if err := h.db.UpdateDeviceProfile(c.Request.Context(), *deviceID, req.DeviceProfile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Return updated device
	device, _ := h.db.GetDeviceByID(c.Request.Context(), *deviceID)
	c.JSON(http.StatusOK, device)
}

func (h *Handler) UpdateMeshSettings(c *gin.Context) {
	deviceID := auth.GetDeviceIDFromContext(c)
	if deviceID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no device in session"})
		return
	}

	var req models.UpdateMeshSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.db.UpdateMeshSettings(c.Request.Context(), *deviceID, req.MeshEnabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Return updated device
	device, _ := h.db.GetDeviceByID(c.Request.Context(), *deviceID)
	c.JSON(http.StatusOK, device)
}

func (h *Handler) GetDeviceProfile(c *gin.Context) {
	deviceID := auth.GetDeviceIDFromContext(c)
	if deviceID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no device in session"})
		return
	}

	device, err := h.db.GetDeviceByID(c.Request.Context(), *deviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}

	c.JSON(http.StatusOK, device)
}

// User handlers

func (h *Handler) SearchUsers(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}

	limit := 20
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	users, err := h.db.SearchUsers(c.Request.Context(), query, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, users)
}

func (h *Handler) GetUser(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}

	user, err := h.db.GetUserByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}

func (h *Handler) GetUserKeys(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}

	user, err := h.db.GetUserByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	devices, err := h.db.GetDevicesByUserID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type KeyInfo struct {
		UserPublicKey []byte `json:"user_public_key"`
		Devices       []struct {
			ID        uuid.UUID `json:"id"`
			Name      string    `json:"name"`
			PublicKey []byte    `json:"public_key"`
		} `json:"devices"`
	}

	keyInfo := KeyInfo{
		UserPublicKey: user.PublicKey,
	}

	for _, device := range devices {
		keyInfo.Devices = append(keyInfo.Devices, struct {
			ID        uuid.UUID `json:"id"`
			Name      string    `json:"name"`
			PublicKey []byte    `json:"public_key"`
		}{
			ID:        device.ID,
			Name:      device.DeviceName,
			PublicKey: device.PublicKey,
		})
	}

	c.JSON(http.StatusOK, keyInfo)
}

// Chat handlers

func (h *Handler) ListChats(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)

	chats, err := h.db.GetChatsByUserID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Build response with participants and last message
	var chatList []models.ChatListItem
	for _, chat := range chats {
		participants, _ := h.db.GetChatParticipants(c.Request.Context(), chat.ID)
		messages, _ := h.db.GetMessagesByChat(c.Request.Context(), chat.ID, 1, 0)

		item := models.ChatListItem{
			Chat:         chat,
			Participants: participants,
		}
		if len(messages) > 0 {
			item.LastMessage = messages[0]
		}

		chatList = append(chatList, item)
	}

	c.JSON(http.StatusOK, chatList)
}

func (h *Handler) CreateChat(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)

	var req models.CreateChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// For 1:1 chats, find or create
	if !req.IsGroup && len(req.Participants) == 1 {
		chat, err := h.db.FindOrCreateDirectChat(c.Request.Context(), userID, req.Participants[0])
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Send invite notification to the other user
		creator, _ := h.db.GetUserByID(c.Request.Context(), userID)
		if creator != nil {
			h.hub.SendToUser(req.Participants[0], &models.WSMessage{
				Type: "chat.invite",
				Payload: mustMarshal(models.WSChatInvite{
					Chat:      chat,
					InvitedBy: creator,
				}),
			}, nil)
		}

		c.JSON(http.StatusCreated, chat)
		return
	}

	// Create group chat
	chat, err := h.db.CreateChat(c.Request.Context(), req.Name, req.IsGroup, userID, req.Participants)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Send invite notifications to all participants
	creator, _ := h.db.GetUserByID(c.Request.Context(), userID)
	if creator != nil {
		for _, participantID := range req.Participants {
			if participantID != userID {
				h.hub.SendToUser(participantID, &models.WSMessage{
					Type: "chat.invite",
					Payload: mustMarshal(models.WSChatInvite{
						Chat:      chat,
						InvitedBy: creator,
					}),
				}, nil)
			}
		}
	}

	c.JSON(http.StatusCreated, chat)
}

func (h *Handler) GetChat(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)

	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
		return
	}

	// Verify user is in chat
	inChat, err := h.db.IsUserInChat(c.Request.Context(), userID, chatID)
	if err != nil || !inChat {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a participant of this chat"})
		return
	}

	chat, err := h.db.GetChatByID(c.Request.Context(), chatID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
		return
	}

	participants, _ := h.db.GetChatParticipants(c.Request.Context(), chatID)

	c.JSON(http.StatusOK, gin.H{
		"chat":         chat,
		"participants": participants,
	})
}

func (h *Handler) GetMessages(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)
	deviceID := auth.GetDeviceIDFromContext(c)

	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
		return
	}

	// Verify user is in chat
	inChat, err := h.db.IsUserInChat(c.Request.Context(), userID, chatID)
	if err != nil || !inChat {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a participant of this chat"})
		return
	}

	limit := 50
	offset := 0
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if o := c.Query("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	messages, err := h.db.GetMessagesByChat(c.Request.Context(), chatID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get server key for encryption
	serverKey, err := h.db.GetServerKeyByUserID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server key not found"})
		return
	}

	// Get device for encryption
	var device *models.Device
	if deviceID != nil {
		device, err = h.db.GetDeviceByID(c.Request.Context(), *deviceID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "device not found"})
			return
		}
	}

	// Encrypt messages for response
	type EncryptedMessage struct {
		*models.Message
		EncryptedContent []byte `json:"encrypted_content,omitempty"`
		Nonce            []byte `json:"nonce,omitempty"`
	}

	encryptedMessages := make([]EncryptedMessage, len(messages))
	for i, msg := range messages {
		encryptedMessages[i] = EncryptedMessage{Message: msg}

		if device != nil {
			// Encrypt content
			encrypted, nonce, err := encryptForDevice(serverKey.PrivateKey, device.PublicKey, []byte(msg.Content))
			if err == nil {
				encryptedMessages[i].EncryptedContent = encrypted
				encryptedMessages[i].Nonce = nonce
				encryptedMessages[i].Content = "" // Clear plaintext
			}
		}
	}

	c.JSON(http.StatusOK, encryptedMessages)
}

func encryptForDevice(serverPrivKey, devicePubKey, plaintext []byte) ([]byte, []byte, error) {
	cryptoSvc := crypto.NewCryptoService()
	return cryptoSvc.EncryptForUser(serverPrivKey, devicePubKey, plaintext)
}

func (h *Handler) AddParticipant(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)

	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
		return
	}

	// Verify user is in chat
	inChat, err := h.db.IsUserInChat(c.Request.Context(), userID, chatID)
	if err != nil || !inChat {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a participant of this chat"})
		return
	}

	var req struct {
		UserID uuid.UUID `json:"user_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.db.AddChatParticipant(c.Request.Context(), chatID, req.UserID, "member"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Send invite notification to the added user
	chat, _ := h.db.GetChatByID(c.Request.Context(), chatID)
	inviter, _ := h.db.GetUserByID(c.Request.Context(), userID)
	if chat != nil && inviter != nil {
		h.hub.SendToUser(req.UserID, &models.WSMessage{
			Type: "chat.invite",
			Payload: mustMarshal(models.WSChatInvite{
				Chat:      chat,
				InvitedBy: inviter,
			}),
		}, nil)
	}

	c.JSON(http.StatusOK, gin.H{"message": "participant invited"})
}

// Message handlers

func (h *Handler) SearchMessages(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)

	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}

	limit := 50
	offset := 0
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if o := c.Query("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	messages, err := h.db.SearchMessages(c.Request.Context(), userID, query, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, messages)
}

// WebSocket handler

func (h *Handler) HandleWebSocket(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)
	deviceID := auth.GetDeviceIDFromContext(c)

	if deviceID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device ID required for WebSocket connection"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	h.hub.NewClient(conn, userID, *deviceID)
}

// Invite handlers

func (h *Handler) GetPendingInvites(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)

	invites, err := h.db.GetPendingInvites(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, invites)
}

func (h *Handler) AcceptInvite(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)

	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
		return
	}

	if err := h.db.AcceptChatInvite(c.Request.Context(), userID, chatID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invite not found"})
		return
	}

	// Get user info for notification
	user, _ := h.db.GetUserByID(c.Request.Context(), userID)

	// Notify other participants that this user accepted
	if user != nil {
		h.hub.BroadcastToChat(chatID, models.WSMessage{
			Type: "chat.accepted",
			Payload: mustMarshal(models.WSChatAccepted{
				ChatID:   chatID,
				UserID:   userID,
				Username: user.Username,
			}),
		})
	}

	c.JSON(http.StatusOK, gin.H{"message": "invite accepted"})
}

func (h *Handler) DeclineInvite(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)

	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
		return
	}

	if err := h.db.DeclineChatInvite(c.Request.Context(), userID, chatID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invite not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "invite declined"})
}

// Leave/Self-destruct handlers

func (h *Handler) LeaveChat(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)

	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
		return
	}

	// Check user's status in the chat
	status, err := h.db.GetUserChatStatus(c.Request.Context(), userID, chatID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a participant of this chat"})
		return
	}

	// If user has a pending invite, treat leave as declining the invite
	if status == "pending" {
		if err := h.db.DeclineChatInvite(c.Request.Context(), userID, chatID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "invite declined"})
		return
	}

	// Verify user has accepted the chat
	if status != "accepted" {
		c.JSON(http.StatusForbidden, gin.H{"error": "not an active participant of this chat"})
		return
	}

	// Leave the chat
	if err := h.db.LeaveChat(c.Request.Context(), userID, chatID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get user info for notification
	user, _ := h.db.GetUserByID(c.Request.Context(), userID)

	// Create a system message
	if user != nil {
		systemMsg, _ := h.db.CreateMessage(c.Request.Context(), chatID, userID,
			user.Username+" has left the chat", models.ContentTypeUserLeft)

		// Notify other participants
		h.hub.BroadcastToChat(chatID, models.WSMessage{
			Type: "user.left",
			Payload: mustMarshal(models.WSUserLeft{
				ChatID:   chatID,
				UserID:   userID,
				Username: user.Username,
			}),
		})

		// Also send the system message
		if systemMsg != nil {
			h.hub.BroadcastToChat(chatID, models.WSMessage{
				Type: "message.receive",
				Payload: mustMarshal(models.WSMessageReceive{
					Message: systemMsg,
				}),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "left chat"})
}

func (h *Handler) SelfDestructChat(c *gin.Context) {
	userID := auth.GetUserIDFromContext(c)

	chatID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chat ID"})
		return
	}

	// Check user's status in the chat
	status, err := h.db.GetUserChatStatus(c.Request.Context(), userID, chatID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a participant of this chat"})
		return
	}

	// If user has a pending invite, treat self-destruct as declining the invite
	if status == "pending" {
		if err := h.db.DeclineChatInvite(c.Request.Context(), userID, chatID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "invite declined"})
		return
	}

	// Verify user has accepted the chat
	if status != "accepted" {
		c.JSON(http.StatusForbidden, gin.H{"error": "not an active participant of this chat"})
		return
	}

	// Self-destruct the chat
	if err := h.db.SelfDestructChat(c.Request.Context(), userID, chatID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get user info for notification
	user, _ := h.db.GetUserByID(c.Request.Context(), userID)

	// Create a system message
	if user != nil {
		systemMsg, _ := h.db.CreateMessage(c.Request.Context(), chatID, userID,
			user.Username+" has self-destructed the chat", models.ContentTypeSelfDestructed)

		// Notify other participants
		h.hub.BroadcastToChat(chatID, models.WSMessage{
			Type: "user.self_destructed",
			Payload: mustMarshal(models.WSUserSelfDestructed{
				ChatID:   chatID,
				UserID:   userID,
				Username: user.Username,
			}),
		})

		// Also send the system message
		if systemMsg != nil {
			h.hub.BroadcastToChat(chatID, models.WSMessage{
				Type: "message.receive",
				Payload: mustMarshal(models.WSMessageReceive{
					Message: systemMsg,
				}),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "chat self-destructed"})
}
