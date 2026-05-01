package main

import (
	"fmt"
	"os"
	"time"

	"e2e-client/internal/api"
	"e2e-client/internal/config"
	"e2e-client/internal/crypto"
)

func main() {
	cfg := &config.Config{
		ServerURL: "http://localhost:8080",
	}
	client := api.NewClient(cfg)

	// Register a test user
	fmt.Println("=== Registering test user 'frank' ===")
	err := client.Register("frank", "frank@test.com", "password123", "Linux Test Client")
	if err != nil {
		fmt.Printf("Register error (may already exist): %v\n", err)
		// Try login instead
		fmt.Println("Trying login...")
		err = client.Login("frank@test.com", "password123", "Linux Test Client")
		if err != nil {
			fmt.Printf("Login error: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("✓ Logged in as: %s\n", cfg.Username)
	fmt.Printf("  User ID: %s\n", cfg.UserID)
	fmt.Printf("  Fingerprint: %s\n", crypto.CalculateFingerprint(cfg.PublicKey))

	// List chats
	fmt.Println("\n=== Listing chats ===")
	chats, err := client.GetChats()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Found %d chats\n", len(chats))
		for _, chat := range chats {
			name := chat.Chat.Name
			if name == "" && len(chat.Participants) > 0 {
				for _, p := range chat.Participants {
					if p.ID != cfg.UserID {
						name = p.Username
						break
					}
				}
			}
			fmt.Printf("  - %s (ID: %s)\n", name, chat.Chat.ID)
		}
	}

	// Search for users
	fmt.Println("\n=== Searching for 'charlie' ===")
	users, err := client.SearchUsers("charlie")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		for _, user := range users {
			fmt.Printf("  Found: %s (%s)\n", user.Username, user.ID)
		}
	}

	// Create chat with charlie (if found)
	if len(users) > 0 {
		fmt.Println("\n=== Creating chat with charlie ===")
		chat, err := client.CreateChat([]string{users[0].ID}, false, "")
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Chat created: %s\n", chat.ID)

			// Connect WebSocket and send message
			fmt.Println("\n=== Connecting WebSocket ===")
			if err := client.ConnectWS(); err != nil {
				fmt.Printf("WebSocket error: %v\n", err)
			} else {
				fmt.Println("✓ Connected")

				// Send a test message
				fmt.Println("\n=== Sending test message ===")
				testMsg := fmt.Sprintf("Hello from Linux CLI client! Time: %s", time.Now().Format(time.RFC3339))
				if err := client.SendMessage(chat.ID, testMsg); err != nil {
					fmt.Printf("Send error: %v\n", err)
				} else {
					fmt.Printf("✓ Message sent: %q\n", testMsg)
				}

				time.Sleep(time.Second)
				client.DisconnectWS()
			}
		}
	}

	// Get messages
	if len(chats) > 0 {
		fmt.Println("\n=== Fetching messages from first chat ===")
		messages, err := client.GetMessages(chats[0].Chat.ID, 5, 0)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			for _, msg := range messages {
				fmt.Printf("  [%s]: %s\n", msg.CreatedAt.Format("15:04"), msg.Content)
			}
		}
	}

	fmt.Println("\n=== Test complete ===")
}
