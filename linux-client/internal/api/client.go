package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"e2e-client/internal/cache"
	"e2e-client/internal/config"
	"e2e-client/internal/crypto"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Client struct {
	config          *config.Config
	cache           *cache.Cache
	ws              *websocket.Conn
	wsMu            sync.Mutex
	listeners       []chan Message
	inviteListeners []chan ChatInvite
	listMu          sync.RWMutex
}

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	PublicKey []byte    `json:"public_key"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
}

type Device struct {
	ID         string `json:"id"`
	DeviceName string `json:"device_name"`
}

type Chat struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	IsGroup   bool      `json:"is_group"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ChatListItem struct {
	Chat         Chat      `json:"chat"`
	LastMessage  *Message  `json:"last_message,omitempty"`
	UnreadCount  int       `json:"unread_count"`
	Participants []User    `json:"participants"`
}

type Message struct {
	ID               string    `json:"id"`
	ChatID           string    `json:"chat_id"`
	SenderID         string    `json:"sender_id"`
	Content          string    `json:"content"`
	ContentType      string    `json:"content_type"`
	CreatedAt        time.Time `json:"created_at"`
	EncryptedContent []byte    `json:"encrypted_content,omitempty"`
	Nonce            []byte    `json:"nonce,omitempty"`
}

type AuthResponse struct {
	User            User   `json:"user"`
	Device          Device `json:"device"`
	Token           string `json:"token"`
	ServerPublicKey []byte `json:"server_public_key"`
}

type ChatInvite struct {
	Chat      Chat `json:"chat"`
	InvitedBy User `json:"invited_by"`
	InvitedAt time.Time `json:"invited_at"`
}

func NewClient(cfg *config.Config) *Client {
	c := &Client{
		config:    cfg,
		listeners: make([]chan Message, 0),
	}

	// Initialize cache with encryption key if available
	if cacheKey, err := cfg.GetCacheKey(); err == nil && len(cacheKey) > 0 {
		if msgCache, err := cache.NewWithKey(cacheKey); err == nil {
			c.cache = msgCache
		}
	} else {
		// Fall back to unencrypted cache
		if msgCache, err := cache.New(); err == nil {
			c.cache = msgCache
		}
	}

	return c
}

func (c *Client) Close() {
	c.DisconnectWS()
	if c.cache != nil {
		c.cache.Close()
	}
}

func (c *Client) request(method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.config.ServerURL+path, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.Token)
	}

	return http.DefaultClient.Do(req)
}

// Register creates a new account (without encryption passphrase)
// Use RegisterWithEncryption for encrypted key storage
func (c *Client) Register(username, email, password, deviceName string) error {
	return c.RegisterWithEncryption(username, email, password, deviceName, "")
}

// RegisterWithEncryption creates a new account with optional key encryption
// If encryptionPassphrase is empty, keys are stored unencrypted (legacy mode)
func (c *Client) RegisterWithEncryption(username, email, password, deviceName, encryptionPassphrase string) error {
	// Generate key pairs
	identityKeys, err := crypto.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("failed to generate identity keys: %w", err)
	}

	deviceKeys, err := crypto.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("failed to generate device keys: %w", err)
	}

	body := map[string]interface{}{
		"username":    username,
		"email":       email,
		"password":    password,
		"public_key":  base64.StdEncoding.EncodeToString(identityKeys.PublicKey[:]),
		"device_name": deviceName,
		"device_key":  base64.StdEncoding.EncodeToString(deviceKeys.PublicKey[:]),
	}

	resp, err := c.request("POST", "/api/auth/register", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("registration failed: %s", errResp.Error)
	}

	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return err
	}

	// Save to config
	c.config.UserID = authResp.User.ID
	c.config.Username = authResp.User.Username
	c.config.Email = authResp.User.Email
	c.config.DeviceID = authResp.Device.ID
	c.config.Token = authResp.Token
	c.config.PublicKey = identityKeys.PublicKey[:]
	c.config.DevicePublicKey = deviceKeys.PublicKey[:]
	c.config.ServerPublicKey = authResp.ServerPublicKey

	// Encrypt keys if passphrase provided
	if encryptionPassphrase != "" {
		if err := c.config.SetKeysAndEncrypt(identityKeys.PrivateKey[:], deviceKeys.PrivateKey[:], encryptionPassphrase); err != nil {
			return fmt.Errorf("failed to encrypt keys: %w", err)
		}
	} else {
		// Legacy unencrypted storage
		c.config.PrivateKey = identityKeys.PrivateKey[:]
		c.config.DevicePrivateKey = deviceKeys.PrivateKey[:]
	}

	if err := c.config.Save(); err != nil {
		return err
	}

	// Reinitialize cache with encryption key
	c.reinitializeCache()

	return nil
}

// Login authenticates to an existing account (without encryption passphrase)
// Use LoginWithEncryption for encrypted key storage
func (c *Client) Login(email, password, deviceName string) error {
	return c.LoginWithEncryption(email, password, deviceName, "")
}

// LoginWithEncryption authenticates with optional key encryption
// If encryptionPassphrase is empty, keys are stored unencrypted (legacy mode)
func (c *Client) LoginWithEncryption(email, password, deviceName, encryptionPassphrase string) error {
	deviceKeys, err := crypto.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("failed to generate device keys: %w", err)
	}

	body := map[string]interface{}{
		"email":       email,
		"password":    password,
		"device_name": deviceName,
		"device_key":  base64.StdEncoding.EncodeToString(deviceKeys.PublicKey[:]),
	}

	resp, err := c.request("POST", "/api/auth/login", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("login failed: %s", errResp.Error)
	}

	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return err
	}

	c.config.UserID = authResp.User.ID
	c.config.Username = authResp.User.Username
	c.config.Email = authResp.User.Email
	c.config.DeviceID = authResp.Device.ID
	c.config.Token = authResp.Token
	c.config.PublicKey = authResp.User.PublicKey
	c.config.DevicePublicKey = deviceKeys.PublicKey[:]
	c.config.ServerPublicKey = authResp.ServerPublicKey

	// Note: Login doesn't have the identity private key - only device key is generated fresh
	// Encrypt device key if passphrase provided
	if encryptionPassphrase != "" {
		// For login, we only have the device private key (identity key is on another device)
		if err := c.config.SetKeysAndEncrypt(authResp.User.PublicKey, deviceKeys.PrivateKey[:], encryptionPassphrase); err != nil {
			return fmt.Errorf("failed to encrypt keys: %w", err)
		}
	} else {
		// Legacy unencrypted storage
		c.config.DevicePrivateKey = deviceKeys.PrivateKey[:]
	}

	if err := c.config.Save(); err != nil {
		return err
	}

	// Reinitialize cache with encryption key
	c.reinitializeCache()

	return nil
}

// reinitializeCache closes existing cache and creates new one with encryption key
func (c *Client) reinitializeCache() {
	if c.cache != nil {
		c.cache.Close()
	}

	if cacheKey, err := c.config.GetCacheKey(); err == nil && len(cacheKey) > 0 {
		if msgCache, err := cache.NewWithKey(cacheKey); err == nil {
			c.cache = msgCache
		}
	} else {
		if msgCache, err := cache.New(); err == nil {
			c.cache = msgCache
		}
	}
}

func (c *Client) Logout() error {
	c.request("POST", "/api/auth/logout", nil)
	c.DisconnectWS()
	return c.config.Clear()
}

func (c *Client) SearchUsers(query string) ([]User, error) {
	resp, err := c.request("GET", "/api/users/search?q="+url.QueryEscape(query), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var users []User
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, err
	}
	return users, nil
}

func (c *Client) GetChats() ([]ChatListItem, error) {
	resp, err := c.request("GET", "/api/chats", nil)
	if err != nil {
		// Fall back to cached chats
		return c.GetCachedChats()
	}
	defer resp.Body.Close()

	var chats []ChatListItem
	if err := json.NewDecoder(resp.Body).Decode(&chats); err != nil {
		return c.GetCachedChats()
	}

	// Cache chats, participants, and users
	for _, chat := range chats {
		c.cacheChat(&chat.Chat)
		for _, user := range chat.Participants {
			c.cacheUser(&user)
			if c.cache != nil {
				c.cache.AddChatParticipant(chat.Chat.ID, user.ID)
			}
		}
	}

	return chats, nil
}

func (c *Client) GetCachedChats() ([]ChatListItem, error) {
	if c.cache == nil {
		return nil, fmt.Errorf("cache not available")
	}

	cachedChats, err := c.cache.GetAllChats()
	if err != nil {
		return nil, err
	}

	users, _ := c.cache.GetAllUsers()

	var chats []ChatListItem
	for _, cc := range cachedChats {
		chat := ChatListItem{
			Chat: Chat{
				ID:        cc.ID,
				Name:      cc.Name,
				IsGroup:   cc.IsGroup,
				UpdatedAt: cc.UpdatedAt,
			},
		}

		// Get participants
		participantIDs, _ := c.cache.GetChatParticipants(cc.ID)
		for _, uid := range participantIDs {
			if user, ok := users[uid]; ok {
				chat.Participants = append(chat.Participants, User{
					ID:        user.ID,
					Username:  user.Username,
					Email:     user.Email,
					PublicKey: user.PublicKey,
				})
			}
		}

		// Get last message
		if lastMsg, err := c.cache.GetLastMessage(cc.ID); err == nil {
			chat.LastMessage = &Message{
				ID:          lastMsg.ID,
				ChatID:      lastMsg.ChatID,
				SenderID:    lastMsg.SenderID,
				Content:     lastMsg.Content,
				ContentType: lastMsg.ContentType,
				CreatedAt:   lastMsg.CreatedAt,
			}
		}

		chats = append(chats, chat)
	}

	return chats, nil
}

func (c *Client) cacheChat(chat *Chat) {
	if c.cache == nil {
		return
	}

	c.cache.SaveChat(&cache.CachedChat{
		ID:        chat.ID,
		Name:      chat.Name,
		IsGroup:   chat.IsGroup,
		UpdatedAt: chat.UpdatedAt,
	})
}

func (c *Client) cacheUser(user *User) {
	if c.cache == nil {
		return
	}

	c.cache.SaveUser(&cache.CachedUser{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		PublicKey: user.PublicKey,
	})
}

func (c *Client) CreateChat(participantIDs []string, isGroup bool, name string) (*Chat, error) {
	body := map[string]interface{}{
		"participants": participantIDs,
		"is_group":     isGroup,
	}
	if name != "" {
		body["name"] = name
	}

	resp, err := c.request("POST", "/api/chats", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("create chat failed: %s", errResp.Error)
	}

	var chat Chat
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		return nil, err
	}
	return &chat, nil
}

func (c *Client) InviteToChat(chatID, userID string) error {
	body := map[string]interface{}{
		"user_id": userID,
	}

	resp, err := c.request("POST", fmt.Sprintf("/api/chats/%s/participants", chatID), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("invite failed: %s", errResp.Error)
	}

	return nil
}

func (c *Client) GetPendingInvites() ([]ChatInvite, error) {
	resp, err := c.request("GET", "/api/invites", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("get invites failed: %s", errResp.Error)
	}

	var invites []ChatInvite
	if err := json.NewDecoder(resp.Body).Decode(&invites); err != nil {
		return nil, err
	}
	return invites, nil
}

func (c *Client) AcceptInvite(chatID string) error {
	resp, err := c.request("POST", fmt.Sprintf("/api/invites/%s/accept", chatID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("accept invite failed: %s", errResp.Error)
	}

	return nil
}

func (c *Client) DeclineInvite(chatID string) error {
	resp, err := c.request("POST", fmt.Sprintf("/api/invites/%s/decline", chatID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("decline invite failed: %s", errResp.Error)
	}

	return nil
}

func (c *Client) LeaveChat(chatID string) error {
	resp, err := c.request("POST", fmt.Sprintf("/api/chats/%s/leave", chatID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("leave chat failed: %s", errResp.Error)
	}

	// Clear local cache for this chat
	if c.cache != nil {
		c.cache.DeleteChatMessages(chatID)
		c.cache.DeleteChat(chatID)
	}

	return nil
}

func (c *Client) SelfDestructChat(chatID string) error {
	resp, err := c.request("POST", fmt.Sprintf("/api/chats/%s/self-destruct", chatID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("self-destruct failed: %s", errResp.Error)
	}

	// Clear local cache for this chat completely
	if c.cache != nil {
		c.cache.DeleteChatMessages(chatID)
		c.cache.DeleteChat(chatID)
	}

	return nil
}

func (c *Client) GetMessages(chatID string, limit, offset int) ([]Message, error) {
	// Try to fetch from server first
	path := fmt.Sprintf("/api/chats/%s/messages?limit=%d&offset=%d", chatID, limit, offset)
	resp, err := c.request("GET", path, nil)

	if err == nil {
		defer resp.Body.Close()

		var messages []Message
		if err := json.NewDecoder(resp.Body).Decode(&messages); err == nil {
			// Decrypt and cache messages
			for i := range messages {
				if messages[i].EncryptedContent != nil && messages[i].Nonce != nil {
					decrypted, err := c.decryptMessage(messages[i].EncryptedContent, messages[i].Nonce)
					if err == nil {
						messages[i].Content = decrypted
					}
				}

				// Save to cache
				c.cacheMessage(&messages[i], true)
			}

			return messages, nil
		}
	}

	// Fall back to cache if server unavailable
	return c.GetCachedMessages(chatID, limit, offset)
}

// GetCachedMessages retrieves messages from local cache only
func (c *Client) GetCachedMessages(chatID string, limit, offset int) ([]Message, error) {
	if c.cache == nil {
		return nil, fmt.Errorf("cache not available")
	}

	cached, err := c.cache.GetMessages(chatID, limit, offset)
	if err != nil {
		return nil, err
	}

	messages := make([]Message, len(cached))
	for i, cm := range cached {
		messages[i] = Message{
			ID:          cm.ID,
			ChatID:      cm.ChatID,
			SenderID:    cm.SenderID,
			Content:     cm.Content,
			ContentType: cm.ContentType,
			CreatedAt:   cm.CreatedAt,
		}
	}

	return messages, nil
}

// GetCachedMessagesChronological retrieves messages in chronological order (oldest first)
func (c *Client) GetCachedMessagesChronological(chatID string, limit, offset int) ([]Message, error) {
	if c.cache == nil {
		return nil, fmt.Errorf("cache not available")
	}

	cached, err := c.cache.GetMessagesChronological(chatID, limit, offset)
	if err != nil {
		return nil, err
	}

	messages := make([]Message, len(cached))
	for i, cm := range cached {
		messages[i] = Message{
			ID:          cm.ID,
			ChatID:      cm.ChatID,
			SenderID:    cm.SenderID,
			Content:     cm.Content,
			ContentType: cm.ContentType,
			CreatedAt:   cm.CreatedAt,
		}
	}

	return messages, nil
}

func (c *Client) cacheMessage(msg *Message, synced bool) {
	if c.cache == nil {
		return
	}

	c.cache.SaveMessage(&cache.CachedMessage{
		ID:          msg.ID,
		ChatID:      msg.ChatID,
		SenderID:    msg.SenderID,
		Content:     msg.Content,
		ContentType: msg.ContentType,
		CreatedAt:   msg.CreatedAt,
		Synced:      synced,
	})
}

// WebSocket methods

func (c *Client) ConnectWS() error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()

	if c.ws != nil {
		return nil
	}

	wsURL := c.config.ServerURL
	if len(wsURL) > 4 && wsURL[:4] == "http" {
		wsURL = "ws" + wsURL[4:]
	}

	// Parse URL and add token as query parameter (more reliable than headers for WebSocket)
	u, _ := url.Parse(wsURL + "/ws")
	q := u.Query()
	q.Set("token", c.config.Token)
	u.RawQuery = q.Encode()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.config.Token)

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		return fmt.Errorf("websocket connect failed: %w", err)
	}

	c.ws = conn
	go c.readLoop()

	return nil
}

func (c *Client) DisconnectWS() {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()

	if c.ws != nil {
		c.ws.Close()
		c.ws = nil
	}
}

func (c *Client) Subscribe() chan Message {
	c.listMu.Lock()
	defer c.listMu.Unlock()

	ch := make(chan Message, 100)
	c.listeners = append(c.listeners, ch)
	return ch
}

func (c *Client) Unsubscribe(ch chan Message) {
	c.listMu.Lock()
	defer c.listMu.Unlock()

	for i, l := range c.listeners {
		if l == ch {
			c.listeners = append(c.listeners[:i], c.listeners[i+1:]...)
			close(ch)
			break
		}
	}
}

func (c *Client) SubscribeInvites() chan ChatInvite {
	c.listMu.Lock()
	defer c.listMu.Unlock()

	ch := make(chan ChatInvite, 100)
	c.inviteListeners = append(c.inviteListeners, ch)
	return ch
}

func (c *Client) readLoop() {
	for {
		c.wsMu.Lock()
		ws := c.ws
		c.wsMu.Unlock()

		if ws == nil {
			return
		}

		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}

		var msg struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		// Handle different message types
		switch msg.Type {
		case "typing.start", "typing.stop",
			"presence.update", "presence.online", "presence.offline",
			"user.left", "user.self_destructed",
			"chat.accepted":
			// Silently ignore these - they don't need to update the UI
			continue

		case "chat.invite":
			// Parse invite and notify listeners
			var invite ChatInvite
			if err := json.Unmarshal(msg.Payload, &invite); err != nil {
				continue
			}

			c.listMu.RLock()
			for _, ch := range c.inviteListeners {
				select {
				case ch <- invite:
				default:
				}
			}
			c.listMu.RUnlock()
			continue
		}

		if msg.Type == "message.receive" {
			var payload struct {
				Message          Message `json:"message"`
				EncryptedContent []byte  `json:"encrypted_content"`
				Nonce            []byte  `json:"nonce"`
			}
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				continue
			}

			message := payload.Message
			if payload.EncryptedContent != nil && payload.Nonce != nil {
				decrypted, err := c.decryptMessage(payload.EncryptedContent, payload.Nonce)
				if err == nil {
					message.Content = decrypted
				}
			}

			// Cache the received message
			c.cacheMessage(&message, true)

			c.listMu.RLock()
			for _, ch := range c.listeners {
				select {
				case ch <- message:
				default:
				}
			}
			c.listMu.RUnlock()
		}
	}
}

func (c *Client) SendMessage(chatID, content string) error {
	// Generate a local message ID
	msgID := uuid.New().String()
	now := time.Now()

	// Save to cache immediately (optimistic)
	localMsg := &Message{
		ID:          msgID,
		ChatID:      chatID,
		SenderID:    c.config.UserID,
		Content:     content,
		ContentType: "text",
		CreatedAt:   now,
	}
	c.cacheMessage(localMsg, false) // Not synced yet

	c.wsMu.Lock()
	ws := c.ws
	c.wsMu.Unlock()

	if ws == nil {
		return fmt.Errorf("not connected")
	}

	// Get device private key
	devicePrivKey, err := c.config.GetDevicePrivateKey()
	if err != nil {
		return fmt.Errorf("failed to get device private key: %w", err)
	}

	// Encrypt the message
	sharedKey, err := crypto.ComputeSharedSecret(devicePrivKey, c.config.ServerPublicKey)
	if err != nil {
		return err
	}

	nonce, err := crypto.GenerateNonce()
	if err != nil {
		return err
	}

	ciphertext, err := crypto.Encrypt(sharedKey, []byte(content), nonce)
	if err != nil {
		return err
	}

	msg := map[string]interface{}{
		"type": "message.send",
		"payload": map[string]interface{}{
			"chat_id":           chatID,
			"encrypted_content": base64.StdEncoding.EncodeToString(ciphertext),
			"content_type":      "text",
			"nonce":             base64.StdEncoding.EncodeToString(nonce),
		},
	}

	c.wsMu.Lock()
	err = ws.WriteJSON(msg)
	c.wsMu.Unlock()

	if err == nil && c.cache != nil {
		// Mark as synced on successful send
		c.cache.MarkMessageSynced(msgID)
	}

	return err
}

func (c *Client) SendTyping(chatID string, typing bool) {
	c.wsMu.Lock()
	ws := c.ws
	c.wsMu.Unlock()

	if ws == nil {
		return
	}

	msgType := "typing.start"
	if !typing {
		msgType = "typing.stop"
	}

	msg := map[string]interface{}{
		"type": msgType,
		"payload": map[string]interface{}{
			"chat_id": chatID,
		},
	}

	c.wsMu.Lock()
	ws.WriteJSON(msg)
	c.wsMu.Unlock()
}

func (c *Client) decryptMessage(ciphertext, nonce []byte) (string, error) {
	devicePrivKey, err := c.config.GetDevicePrivateKey()
	if err != nil {
		return "", fmt.Errorf("failed to get device private key: %w", err)
	}

	sharedKey, err := crypto.ComputeSharedSecret(devicePrivKey, c.config.ServerPublicKey)
	if err != nil {
		return "", err
	}

	plaintext, err := crypto.Decrypt(sharedKey, ciphertext, nonce)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

func (c *Client) GetConfig() *config.Config {
	return c.config
}

func (c *Client) GetCache() *cache.Cache {
	return c.cache
}

// SyncUnsent attempts to send any locally cached messages that weren't synced
func (c *Client) SyncUnsent() error {
	if c.cache == nil {
		return nil
	}

	unsynced, err := c.cache.GetUnsyncedMessages()
	if err != nil {
		return err
	}

	for _, msg := range unsynced {
		// Re-send the message
		if err := c.SendMessage(msg.ChatID, msg.Content); err == nil {
			// Delete the old unsynced version (SendMessage creates a new one)
			c.cache.DeleteMessage(msg.ID)
		}
	}

	return nil
}
