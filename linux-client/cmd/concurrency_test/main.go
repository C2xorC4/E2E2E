package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"e2e-client/internal/crypto"

	"github.com/gorilla/websocket"
)

const (
	baseURL     = "http://localhost:8080"
	numUsers    = 10
	msgsPerUser = 5
)

type TestUser struct {
	ID              string
	Username        string
	Email           string
	Token           string
	DeviceID        string
	DevicePrivKey   []byte
	DevicePubKey    []byte
	ServerPublicKey []byte
}

type Message struct {
	ID        string    `json:"id"`
	ChatID    string    `json:"chat_id"`
	SenderID  string    `json:"sender_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

var (
	messagesSent     int64
	messagesReceived int64
	errors           int64
)

func main() {
	fmt.Println("=== E2E Chat Concurrency Test ===")
	fmt.Printf("Users: %d, Messages per user: %d, Total expected: %d\n\n", numUsers, msgsPerUser, numUsers*msgsPerUser)

	// Step 1: Register test users
	fmt.Println("Step 1: Registering test users...")
	users := make([]*TestUser, numUsers)
	for i := 0; i < numUsers; i++ {
		user, err := registerUser(fmt.Sprintf("loadtest%d", i), fmt.Sprintf("loadtest%d@test.com", i))
		if err != nil {
			fmt.Printf("  User %d: login instead... ", i)
			user, err = loginUser(fmt.Sprintf("loadtest%d@test.com", i))
			if err != nil {
				fmt.Printf("FAILED: %v\n", err)
				return
			}
			fmt.Printf("OK\n")
		}
		users[i] = user
		idPreview := user.ID
		if len(idPreview) > 8 {
			idPreview = idPreview[:8]
		}
		fmt.Printf("  User %d: %s (ID: %s)\n", i, user.Username, idPreview)
	}

	// Step 2: Create group chat with first user, add others
	fmt.Println("\nStep 2: Creating group chat...")
	participantIDs := make([]string, len(users))
	for i, u := range users {
		participantIDs[i] = u.ID
	}

	chatID, err := createGroupChat(users[0], "Load Test Chat", participantIDs[1:])
	if err != nil {
		fmt.Printf("Failed to create chat: %v\n", err)
		return
	}
	fmt.Printf("  Chat created: %s\n", chatID[:8])

	// Step 3: Connect all users via WebSocket
	fmt.Println("\nStep 3: Connecting WebSockets...")
	var wg sync.WaitGroup
	startSignal := make(chan struct{})
	allConnected := make(chan struct{})

	conns := make([]*websocket.Conn, numUsers)
	connectedCount := int32(0)

	for i, user := range users {
		wg.Add(1)
		go func(idx int, u *TestUser) {
			defer wg.Done()

			conn, err := connectWebSocket(u)
			if err != nil {
				fmt.Printf("  User %d WebSocket error: %v\n", idx, err)
				atomic.AddInt64(&errors, 1)
				return
			}
			conns[idx] = conn

			// Start receiving messages
			go receiveMessages(conn, u)

			if atomic.AddInt32(&connectedCount, 1) == int32(numUsers) {
				close(allConnected)
			}

			// Wait for start signal
			<-startSignal

			// Send messages
			for j := 0; j < msgsPerUser; j++ {
				msg := fmt.Sprintf("Message %d from %s at %s", j+1, u.Username, time.Now().Format("15:04:05.000"))
				if err := sendMessage(conn, u, chatID, msg); err != nil {
					fmt.Printf("  Send error (user %d, msg %d): %v\n", idx, j, err)
					atomic.AddInt64(&errors, 1)
				} else {
					atomic.AddInt64(&messagesSent, 1)
				}
				// Small random delay to simulate realistic typing
				time.Sleep(time.Millisecond * time.Duration(50+idx*10))
			}
		}(i, user)
	}

	// Wait for all connections
	select {
	case <-allConnected:
		fmt.Printf("  All %d users connected!\n", numUsers)
	case <-time.After(10 * time.Second):
		fmt.Println("  Timeout waiting for connections")
		return
	}

	// Step 4: Start the test
	fmt.Println("\nStep 4: Starting message flood...")
	startTime := time.Now()
	close(startSignal)

	// Wait for all senders to finish
	wg.Wait()
	sendDuration := time.Since(startTime)

	fmt.Printf("\nStep 5: Waiting for message delivery...\n")
	// Give some time for messages to be delivered
	time.Sleep(3 * time.Second)

	// Close connections
	for _, conn := range conns {
		if conn != nil {
			conn.Close()
		}
	}

	// Step 6: Verify messages in database
	fmt.Println("\nStep 6: Verifying messages in database...")
	dbCount, err := countMessagesInChat(chatID, users[0].Token)
	if err != nil {
		fmt.Printf("  DB query error: %v\n", err)
	}

	// Results
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("RESULTS")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("Messages sent:      %d\n", atomic.LoadInt64(&messagesSent))
	fmt.Printf("Messages received:  %d (by all clients combined)\n", atomic.LoadInt64(&messagesReceived))
	fmt.Printf("Messages in DB:     %d\n", dbCount)
	fmt.Printf("Errors:             %d\n", atomic.LoadInt64(&errors))
	fmt.Printf("Send duration:      %v\n", sendDuration)
	fmt.Printf("Throughput:         %.2f msgs/sec\n", float64(messagesSent)/sendDuration.Seconds())

	expectedTotal := int64(numUsers * msgsPerUser)
	if dbCount == int(expectedTotal) && atomic.LoadInt64(&errors) == 0 {
		fmt.Println("\n✓ CONCURRENCY TEST PASSED")
	} else {
		fmt.Println("\n✗ TEST FAILED - message count mismatch or errors occurred")
	}
}

func registerUser(username, email string) (*TestUser, error) {
	deviceKeys, _ := crypto.GenerateKeyPair()
	identityKeys, _ := crypto.GenerateKeyPair()

	body := map[string]interface{}{
		"username":    username,
		"email":       email,
		"password":    "testpass123",
		"public_key":  base64.StdEncoding.EncodeToString(identityKeys.PublicKey[:]),
		"device_name": "LoadTest Device",
		"device_key":  base64.StdEncoding.EncodeToString(deviceKeys.PublicKey[:]),
	}

	resp, err := postJSON("/api/auth/register", "", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return nil, fmt.Errorf("register failed with status %d", resp.StatusCode)
	}

	var result struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
		Token           string `json:"token"`
		ServerPublicKey string `json:"server_public_key"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}

	if result.User.ID == "" {
		return nil, fmt.Errorf("empty user ID in response")
	}

	serverPubKey, _ := base64.StdEncoding.DecodeString(result.ServerPublicKey)

	return &TestUser{
		ID:              result.User.ID,
		Username:        result.User.Username,
		Email:           email,
		Token:           result.Token,
		DeviceID:        result.Device.ID,
		DevicePrivKey:   deviceKeys.PrivateKey[:],
		DevicePubKey:    deviceKeys.PublicKey[:],
		ServerPublicKey: serverPubKey,
	}, nil
}

func loginUser(email string) (*TestUser, error) {
	deviceKeys, _ := crypto.GenerateKeyPair()

	body := map[string]interface{}{
		"email":       email,
		"password":    "testpass123",
		"device_name": "LoadTest Device",
		"device_key":  base64.StdEncoding.EncodeToString(deviceKeys.PublicKey[:]),
	}

	resp, err := postJSON("/api/auth/login", "", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("login failed with status %d", resp.StatusCode)
	}

	var result struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
		Token           string `json:"token"`
		ServerPublicKey string `json:"server_public_key"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}

	if result.User.ID == "" {
		return nil, fmt.Errorf("empty user ID in response")
	}

	serverPubKey, _ := base64.StdEncoding.DecodeString(result.ServerPublicKey)

	return &TestUser{
		ID:              result.User.ID,
		Username:        result.User.Username,
		Email:           email,
		Token:           result.Token,
		DeviceID:        result.Device.ID,
		DevicePrivKey:   deviceKeys.PrivateKey[:],
		DevicePubKey:    deviceKeys.PublicKey[:],
		ServerPublicKey: serverPubKey,
	}, nil
}

func createGroupChat(creator *TestUser, name string, participantIDs []string) (string, error) {
	body := map[string]interface{}{
		"name":         name,
		"is_group":     true,
		"participants": participantIDs,
	}

	resp, err := postJSON("/api/chats", creator.Token, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.ID, nil
}

func connectWebSocket(user *TestUser) (*websocket.Conn, error) {
	u := url.URL{Scheme: "ws", Host: "localhost:8080", Path: "/ws"}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+user.Token)

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	return conn, err
}

func sendMessage(conn *websocket.Conn, user *TestUser, chatID, content string) error {
	// Encrypt the message
	sharedKey, err := crypto.ComputeSharedSecret(user.DevicePrivKey, user.ServerPublicKey)
	if err != nil {
		return err
	}

	nonce, _ := crypto.GenerateNonce()
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

	return conn.WriteJSON(msg)
}

func receiveMessages(conn *websocket.Conn, user *TestUser) {
	for {
		_, data, err := conn.ReadMessage()
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

		if msg.Type == "message.receive" {
			atomic.AddInt64(&messagesReceived, 1)
		}
	}
}

func countMessagesInChat(chatID string, token string) (int, error) {
	req, _ := http.NewRequest("GET", baseURL+"/api/chats/"+chatID+"/messages?limit=1000", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var messages []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return 0, err
	}

	return len(messages), nil
}

func postJSON(path, token string, body interface{}) (*http.Response, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", baseURL+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return http.DefaultClient.Do(req)
}
