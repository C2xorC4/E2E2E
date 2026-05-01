package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"e2e-chat/internal/crypto"

	"github.com/gorilla/websocket"
)

const baseURL = "http://localhost:8080"

type LoginResponse struct {
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Device struct {
		ID string `json:"id"`
	} `json:"device"`
	Token           string `json:"token"`
	ServerPublicKey string `json:"server_public_key"`
}

type ChatResponse struct {
	ID string `json:"id"`
}

func main() {
	// Generate key pairs for test users
	aliceKeys, _ := crypto.GenerateKeyPair()
	aliceDeviceKeys, _ := crypto.GenerateKeyPair()
	bobKeys, _ := crypto.GenerateKeyPair()
	bobDeviceKeys, _ := crypto.GenerateKeyPair()

	// Register Alice
	aliceResp := register("charlie", "charlie@test.com", "password123",
		aliceKeys.PublicKey[:], aliceDeviceKeys.PublicKey[:])
	if aliceResp == nil {
		// Try login if already exists
		aliceResp = login("charlie@test.com", "password123", aliceDeviceKeys.PublicKey[:])
	}
	fmt.Printf("Charlie logged in: ID=%s\n", aliceResp.User.ID)

	// Register Bob
	bobResp := register("dave", "dave@test.com", "password123",
		bobKeys.PublicKey[:], bobDeviceKeys.PublicKey[:])
	if bobResp == nil {
		bobResp = login("dave@test.com", "password123", bobDeviceKeys.PublicKey[:])
	}
	fmt.Printf("Dave logged in: ID=%s\n", bobResp.User.ID)

	// Create chat between them
	chatID := createChat(aliceResp.Token, bobResp.User.ID)
	fmt.Printf("Chat created: ID=%s\n", chatID)

	// Decode server public key
	serverPubKey, _ := base64.StdEncoding.DecodeString(aliceResp.ServerPublicKey)

	// Connect WebSocket and send message
	sendMessageViaWS(aliceResp.Token, aliceDeviceKeys.PrivateKey[:], serverPubKey, chatID,
		"Hello Dave! This is an encrypted test message.")

	sendMessageViaWS(aliceResp.Token, aliceDeviceKeys.PrivateKey[:], serverPubKey, chatID,
		"Testing server-side message storage and search capability.")

	sendMessageViaWS(aliceResp.Token, aliceDeviceKeys.PrivateKey[:], serverPubKey, chatID,
		"The quick brown fox jumps over the lazy dog.")

	fmt.Println("\n3 test messages sent successfully!")
	fmt.Println("Check the database to verify plaintext storage.")
}

func register(username, email, password string, publicKey, deviceKey []byte) *LoginResponse {
	body := map[string]interface{}{
		"username":    username,
		"email":       email,
		"password":    password,
		"public_key":  base64.StdEncoding.EncodeToString(publicKey),
		"device_name": username + " Device",
		"device_key":  base64.StdEncoding.EncodeToString(deviceKey),
	}
	jsonBody, _ := json.Marshal(body)

	resp, err := http.Post(baseURL+"/api/auth/register", "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("Register error: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return nil
	}

	var result LoginResponse
	json.NewDecoder(resp.Body).Decode(&result)
	return &result
}

func login(email, password string, deviceKey []byte) *LoginResponse {
	body := map[string]interface{}{
		"email":       email,
		"password":    password,
		"device_name": "Test Device",
		"device_key":  base64.StdEncoding.EncodeToString(deviceKey),
	}
	jsonBody, _ := json.Marshal(body)

	resp, err := http.Post(baseURL+"/api/auth/login", "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Fatalf("Login error: %v", err)
	}
	defer resp.Body.Close()

	var result LoginResponse
	json.NewDecoder(resp.Body).Decode(&result)
	return &result
}

func createChat(token, participantID string) string {
	body := map[string]interface{}{
		"is_group":     false,
		"participants": []string{participantID},
	}
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", baseURL+"/api/chats", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Create chat error: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result ChatResponse
	json.Unmarshal(respBody, &result)
	return result.ID
}

func sendMessageViaWS(token string, devicePrivKey, serverPubKey []byte, chatID, message string) {
	u := url.URL{Scheme: "ws", Host: "localhost:8080", Path: "/ws"}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		log.Printf("WebSocket dial error: %v", err)
		return
	}
	defer conn.Close()

	// Compute shared secret and encrypt message
	sharedKey, err := crypto.ComputeSharedSecret(devicePrivKey, serverPubKey)
	if err != nil {
		log.Printf("Shared secret error: %v", err)
		return
	}

	nonce, _ := crypto.GenerateNonce()
	ciphertext, err := crypto.Encrypt(sharedKey, []byte(message), nonce)
	if err != nil {
		log.Printf("Encrypt error: %v", err)
		return
	}

	// Send message
	wsMsg := map[string]interface{}{
		"type": "message.send",
		"payload": map[string]interface{}{
			"chat_id":           chatID,
			"encrypted_content": base64.StdEncoding.EncodeToString(ciphertext),
			"content_type":      "text",
			"nonce":             base64.StdEncoding.EncodeToString(nonce),
		},
	}

	if err := conn.WriteJSON(wsMsg); err != nil {
		log.Printf("Write error: %v", err)
		return
	}

	fmt.Printf("Sent: %q\n", message)
	time.Sleep(500 * time.Millisecond)
}
