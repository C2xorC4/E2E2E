package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"e2e-chat/internal/crypto"
	"e2e-chat/internal/db"
	"e2e-chat/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 65536
)

// Client represents a connected WebSocket client
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	userID   uuid.UUID
	deviceID uuid.UUID
}

// Hub maintains the set of active clients and broadcasts messages
type Hub struct {
	// Registered clients by user ID (one user can have multiple devices)
	clients map[uuid.UUID]map[uuid.UUID]*Client // userID -> deviceID -> Client

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Broadcast to specific users
	broadcast chan *BroadcastMessage

	// Mutex for clients map
	mu sync.RWMutex

	// Database connection
	db *db.DB

	// Crypto service
	crypto *crypto.CryptoService
}

// BroadcastMessage represents a message to broadcast
type BroadcastMessage struct {
	UserIDs []uuid.UUID
	Message []byte
	Exclude *uuid.UUID // Exclude this device
}

// NewHub creates a new Hub
func NewHub(database *db.DB) *Hub {
	return &Hub{
		clients:    make(map[uuid.UUID]map[uuid.UUID]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *BroadcastMessage, 256),
		db:         database,
		crypto:     crypto.NewCryptoService(),
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.registerClient(client)

		case client := <-h.unregister:
			h.unregisterClient(client)

		case msg := <-h.broadcast:
			h.broadcastMessage(msg)
		}
	}
}

func (h *Hub) registerClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client.userID]; !ok {
		h.clients[client.userID] = make(map[uuid.UUID]*Client)
	}
	h.clients[client.userID][client.deviceID] = client

	log.Printf("Client registered: user=%s device=%s", client.userID, client.deviceID)

	// Notify contacts of online status
	go h.broadcastPresence(client.userID, true)
}

func (h *Hub) unregisterClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if devices, ok := h.clients[client.userID]; ok {
		if _, ok := devices[client.deviceID]; ok {
			delete(devices, client.deviceID)
			close(client.send)

			if len(devices) == 0 {
				delete(h.clients, client.userID)
				// Notify contacts of offline status
				go h.broadcastPresence(client.userID, false)
			}
		}
	}

	log.Printf("Client unregistered: user=%s device=%s", client.userID, client.deviceID)
}

func (h *Hub) broadcastMessage(msg *BroadcastMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, userID := range msg.UserIDs {
		if devices, ok := h.clients[userID]; ok {
			for deviceID, client := range devices {
				if msg.Exclude != nil && deviceID == *msg.Exclude {
					continue
				}
				select {
				case client.send <- msg.Message:
				default:
					// Client's buffer is full, close connection
					close(client.send)
					delete(devices, deviceID)
				}
			}
		}
	}
}

func (h *Hub) broadcastPresence(userID uuid.UUID, online bool) {
	presence := models.WSPresenceUpdate{
		UserID: userID,
		Online: online,
	}

	payload, _ := json.Marshal(presence)
	msg := models.WSMessage{
		Type:    "presence.update",
		Payload: payload,
	}
	data, _ := json.Marshal(msg)

	// Get all contacts of this user (users in shared chats)
	ctx := context.Background()
	chats, err := h.db.GetChatsByUserID(ctx, userID)
	if err != nil {
		return
	}

	contactIDs := make(map[uuid.UUID]bool)
	for _, chat := range chats {
		participants, err := h.db.GetChatParticipants(ctx, chat.ID)
		if err != nil {
			continue
		}
		for _, p := range participants {
			if p.ID != userID {
				contactIDs[p.ID] = true
			}
		}
	}

	userIDs := make([]uuid.UUID, 0, len(contactIDs))
	for id := range contactIDs {
		userIDs = append(userIDs, id)
	}

	h.broadcast <- &BroadcastMessage{
		UserIDs: userIDs,
		Message: data,
	}
}

// IsUserOnline checks if a user has any online devices
func (h *Hub) IsUserOnline(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[userID]
	return ok
}

// GetOnlineDevices returns online device IDs for a user
func (h *Hub) GetOnlineDevices(userID uuid.UUID) []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()

	devices := h.clients[userID]
	if devices == nil {
		return nil
	}

	result := make([]uuid.UUID, 0, len(devices))
	for deviceID := range devices {
		result = append(result, deviceID)
	}
	return result
}

// SendToUser sends a message to all devices of a user
func (h *Hub) SendToUser(userID uuid.UUID, msg *models.WSMessage, excludeDevice *uuid.UUID) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.broadcast <- &BroadcastMessage{
		UserIDs: []uuid.UUID{userID},
		Message: data,
		Exclude: excludeDevice,
	}
}

// SendToUsers sends a message to multiple users
func (h *Hub) SendToUsers(userIDs []uuid.UUID, msg *models.WSMessage, excludeDevice *uuid.UUID) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.broadcast <- &BroadcastMessage{
		UserIDs: userIDs,
		Message: data,
		Exclude: excludeDevice,
	}
}

// BroadcastToChat sends a message to all participants of a chat
func (h *Hub) BroadcastToChat(chatID uuid.UUID, msg models.WSMessage) {
	ctx := context.Background()
	participants, err := h.db.GetChatParticipants(ctx, chatID)
	if err != nil {
		log.Printf("Error getting chat participants for broadcast: %v", err)
		return
	}

	userIDs := make([]uuid.UUID, len(participants))
	for i, p := range participants {
		userIDs[i] = p.ID
	}

	h.SendToUsers(userIDs, &msg, nil)
}

// NewClient creates a new client and starts its goroutines
func (h *Hub) NewClient(conn *websocket.Conn, userID, deviceID uuid.UUID) *Client {
	client := &Client{
		hub:      h,
		conn:     conn,
		send:     make(chan []byte, 256),
		userID:   userID,
		deviceID: deviceID,
	}

	h.register <- client

	go client.writePump()
	go client.readPump()

	// Deliver pending messages
	go h.deliverPendingMessages(client)

	return client
}

func (h *Hub) deliverPendingMessages(client *Client) {
	ctx := context.Background()
	messages, err := h.db.GetPendingDeliveries(ctx, client.deviceID)
	if err != nil {
		log.Printf("Error fetching pending messages: %v", err)
		return
	}

	// Get server key for this user
	serverKey, err := h.db.GetServerKeyByUserID(ctx, client.userID)
	if err != nil {
		log.Printf("Error fetching server key: %v", err)
		return
	}

	// Get user's device key
	device, err := h.db.GetDeviceByID(ctx, client.deviceID)
	if err != nil {
		log.Printf("Error fetching device: %v", err)
		return
	}

	for _, msg := range messages {
		// Encrypt message content for this device
		encrypted, nonce, err := h.crypto.EncryptForUser(serverKey.PrivateKey, device.PublicKey, []byte(msg.Content))
		if err != nil {
			log.Printf("Error encrypting message: %v", err)
			continue
		}

		wsMsg := models.WSMessageReceive{
			Message:          msg,
			EncryptedContent: encrypted,
			Nonce:            nonce,
		}

		payload, _ := json.Marshal(wsMsg)
		outMsg := models.WSMessage{
			Type:    "message.receive",
			Payload: payload,
		}

		data, _ := json.Marshal(outMsg)
		select {
		case client.send <- data:
			h.db.MarkMessageDelivered(ctx, msg.ID, client.deviceID)
		default:
			return
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		c.handleMessage(message)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleMessage(data []byte) {
	preview := string(data)
	if len(preview) > 200 {
		preview = preview[:200]
	}
	log.Printf("WS handleMessage from user=%s device=%s: %s", c.userID, c.deviceID, preview)

	var msg models.WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("Error parsing WebSocket message: %v", err)
		return
	}

	log.Printf("WS message type: %s", msg.Type)

	switch msg.Type {
	case "message.send":
		c.handleSendMessage(msg.Payload)
	case "typing.start":
		c.handleTyping(msg.Payload, true)
	case "typing.stop":
		c.handleTyping(msg.Payload, false)
	case "message.read":
		c.handleMessageRead(msg.Payload)
	default:
		log.Printf("Unknown message type: %s", msg.Type)
	}
}

func (c *Client) handleSendMessage(payload json.RawMessage) {
	log.Printf("handleSendMessage: user=%s device=%s payload_len=%d", c.userID, c.deviceID, len(payload))

	var req models.WSMessageSend
	if err := json.Unmarshal(payload, &req); err != nil {
		log.Printf("Error parsing send message: %v, payload: %s", err, string(payload))
		return
	}

	log.Printf("handleSendMessage: chat=%s content_len=%d nonce_len=%d", req.ChatID, len(req.EncryptedContent), len(req.Nonce))

	ctx := context.Background()

	// Verify user is in chat
	inChat, err := c.hub.db.IsUserInChat(ctx, c.userID, req.ChatID)
	if err != nil || !inChat {
		log.Printf("User %s not in chat %s (err=%v, inChat=%v)", c.userID, req.ChatID, err, inChat)
		return
	}

	// Get server key
	serverKey, err := c.hub.db.GetServerKeyByUserID(ctx, c.userID)
	if err != nil {
		log.Printf("Error getting server key: %v", err)
		return
	}

	// Get sender's device
	device, err := c.hub.db.GetDeviceByID(ctx, c.deviceID)
	if err != nil {
		log.Printf("Error getting device: %v", err)
		return
	}

	// Decrypt the message from sender
	plaintext, err := c.hub.crypto.DecryptFromUser(serverKey.PrivateKey, device.PublicKey, req.EncryptedContent, req.Nonce)
	if err != nil {
		log.Printf("Error decrypting message: %v", err)
		return
	}

	// Store message in database (plaintext)
	contentType := req.ContentType
	if contentType == "" {
		contentType = "text"
	}

	message, err := c.hub.db.CreateMessage(ctx, req.ChatID, c.userID, string(plaintext), contentType)
	if err != nil {
		log.Printf("Error storing message: %v", err)
		return
	}

	log.Printf("Message stored: id=%s content_preview='%.50s'", message.ID, plaintext)

	// Create delivery records
	c.hub.db.CreateMessageDeliveries(ctx, message.ID, c.userID, req.ChatID)

	// Get chat participants
	participants, err := c.hub.db.GetChatParticipants(ctx, req.ChatID)
	if err != nil {
		log.Printf("Error getting participants: %v", err)
		return
	}

	log.Printf("Forwarding message to %d participants", len(participants))

	// Encrypt and send to each participant
	for _, participant := range participants {
		if participant.ID == c.userID {
			log.Printf("Skipping sender %s", participant.ID)
			continue // Skip sender
		}

		log.Printf("Sending to participant %s (%s)", participant.ID, participant.Username)

		// Get participant's server key
		recipientServerKey, err := c.hub.db.GetServerKeyByUserID(ctx, participant.ID)
		if err != nil {
			log.Printf("Error getting recipient server key: %v", err)
			continue
		}

		// Get all devices for this participant
		devices, err := c.hub.db.GetDevicesByUserID(ctx, participant.ID)
		if err != nil {
			log.Printf("Error getting recipient devices: %v", err)
			continue
		}

		for _, recipientDevice := range devices {
			// Encrypt for each device
			encrypted, nonce, err := c.hub.crypto.EncryptForUser(recipientServerKey.PrivateKey, recipientDevice.PublicKey, plaintext)
			if err != nil {
				log.Printf("Error encrypting for recipient: %v", err)
				continue
			}

			wsMsg := models.WSMessageReceive{
				Message:          message,
				EncryptedContent: encrypted,
				Nonce:            nonce,
			}

			msgPayload, _ := json.Marshal(wsMsg)
			outMsg := &models.WSMessage{
				Type:    "message.receive",
				Payload: msgPayload,
			}

			c.hub.SendToUser(participant.ID, outMsg, nil)

			// Mark as delivered if online
			if c.hub.IsUserOnline(participant.ID) {
				c.hub.db.MarkMessageDelivered(ctx, message.ID, recipientDevice.ID)
			}
		}
	}
}

func (c *Client) handleTyping(payload json.RawMessage, typing bool) {
	var chatID struct {
		ChatID uuid.UUID `json:"chat_id"`
	}
	if err := json.Unmarshal(payload, &chatID); err != nil {
		return
	}

	ctx := context.Background()
	participants, err := c.hub.db.GetChatParticipants(ctx, chatID.ChatID)
	if err != nil {
		return
	}

	indicator := models.WSTypingIndicator{
		ChatID: chatID.ChatID,
		UserID: c.userID,
		Typing: typing,
	}

	msgPayload, _ := json.Marshal(indicator)
	msg := &models.WSMessage{
		Type:    "typing.update",
		Payload: msgPayload,
	}

	userIDs := make([]uuid.UUID, 0, len(participants)-1)
	for _, p := range participants {
		if p.ID != c.userID {
			userIDs = append(userIDs, p.ID)
		}
	}

	c.hub.SendToUsers(userIDs, msg, nil)
}

func (c *Client) handleMessageRead(payload json.RawMessage) {
	var req struct {
		MessageID uuid.UUID `json:"message_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return
	}

	ctx := context.Background()
	c.hub.db.MarkMessageRead(ctx, req.MessageID, c.deviceID)
}
