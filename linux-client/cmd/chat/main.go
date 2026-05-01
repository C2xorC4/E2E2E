package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"e2e-client/internal/api"
	"e2e-client/internal/config"
	"e2e-client/internal/crypto"
	"e2e-client/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var cfg *config.Config
var client *api.Client
var unlockPassphrase string

func main() {
	var err error
	cfg, err = config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	rootCmd := &cobra.Command{
		Use:   "e2e-chat",
		Short: "End-to-end encrypted chat client",
		Long: `A secure chat client with end-to-end encryption.

Run without arguments to start the interactive TUI.
Use subcommands for CLI operations.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Skip unlock for commands that don't need it
			skipUnlock := map[string]bool{
				"register": true,
				"login":    true,
				"status":   true,
				"help":     true,
			}

			cmdName := cmd.Name()
			if skipUnlock[cmdName] {
				return
			}

			// Check if keys need to be unlocked
			if cfg.IsLoggedIn() && cfg.IsEncrypted() && !cfg.IsUnlocked() {
				if err := unlockKeys(); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to unlock keys: %v\n", err)
					os.Exit(1)
				}
			}

			// Initialize client after potential unlock
			client = api.NewClient(cfg)
		},
		Run: func(cmd *cobra.Command, args []string) {
			runInteractive()
		},
	}

	// Server URL flag
	rootCmd.PersistentFlags().StringVar(&cfg.ServerURL, "server", cfg.ServerURL, "Server URL")

	// Unlock passphrase flag for automation
	rootCmd.PersistentFlags().StringVar(&unlockPassphrase, "unlock", "", "Encryption passphrase to unlock keys (for automation)")

	// Register command
	registerCmd := &cobra.Command{
		Use:   "register",
		Short: "Create a new account",
		Run:   runRegister,
	}
	registerCmd.Flags().String("username", "", "Username")
	registerCmd.Flags().String("email", "", "Email address")
	registerCmd.Flags().String("device", "", "Device name (default: hostname)")
	registerCmd.Flags().Bool("no-encrypt", false, "Skip key encryption (not recommended)")
	rootCmd.AddCommand(registerCmd)

	// Login command
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Login to existing account",
		Run:   runLogin,
	}
	loginCmd.Flags().String("email", "", "Email address")
	loginCmd.Flags().String("device", "", "Device name (default: hostname)")
	loginCmd.Flags().Bool("no-encrypt", false, "Skip key encryption (not recommended)")
	rootCmd.AddCommand(loginCmd)

	// Logout command
	logoutCmd := &cobra.Command{
		Use:   "logout",
		Short: "Logout and clear credentials",
		Run:   runLogout,
	}
	rootCmd.AddCommand(logoutCmd)

	// Status command
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show current login status",
		Run:   runStatus,
	}
	rootCmd.AddCommand(statusCmd)

	// Chats command
	chatsCmd := &cobra.Command{
		Use:   "chats",
		Short: "List all chats",
		Run:   runChats,
	}
	rootCmd.AddCommand(chatsCmd)

	// Send command
	sendCmd := &cobra.Command{
		Use:   "send",
		Short: "Send a message",
		Run:   runSend,
	}
	sendCmd.Flags().String("to", "", "Recipient username or chat ID")
	sendCmd.Flags().String("message", "", "Message content")
	sendCmd.Flags().StringP("msg", "m", "", "Message content (alias)")
	rootCmd.AddCommand(sendCmd)

	// Search command
	searchCmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search for users",
		Args:  cobra.MinimumNArgs(1),
		Run:   runSearch,
	}
	rootCmd.AddCommand(searchCmd)

	// Messages command
	messagesCmd := &cobra.Command{
		Use:   "messages [chat-id]",
		Short: "Show messages from a chat",
		Args:  cobra.ExactArgs(1),
		Run:   runMessages,
	}
	messagesCmd.Flags().Int("limit", 20, "Number of messages to show")
	rootCmd.AddCommand(messagesCmd)

	// Fingerprint command
	fingerprintCmd := &cobra.Command{
		Use:   "fingerprint",
		Short: "Show your key fingerprint for verification",
		Run:   runFingerprint,
	}
	rootCmd.AddCommand(fingerprintCmd)

	// Cache command
	cacheCmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage local message cache",
	}

	cacheShowCmd := &cobra.Command{
		Use:   "show [chat-id]",
		Short: "Show cached messages",
		Args:  cobra.MaximumNArgs(1),
		Run:   runCacheShow,
	}
	cacheShowCmd.Flags().Int("limit", 20, "Number of messages to show")

	cacheStatsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show cache statistics",
		Run:   runCacheStats,
	}

	cacheClearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear the message cache",
		Run:   runCacheClear,
	}

	cacheCmd.AddCommand(cacheShowCmd, cacheStatsCmd, cacheClearCmd)
	rootCmd.AddCommand(cacheCmd)

	// Encryption commands
	encryptCmd := &cobra.Command{
		Use:   "encrypt",
		Short: "Encrypt stored keys with a passphrase",
		Long:  "Encrypts the private keys stored in the config file. This adds an extra layer of security.",
		Run:   runEncrypt,
	}
	rootCmd.AddCommand(encryptCmd)

	changePassphraseCmd := &cobra.Command{
		Use:   "change-passphrase",
		Short: "Change encryption passphrase",
		Run:   runChangePassphrase,
	}
	rootCmd.AddCommand(changePassphraseCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// unlockKeys prompts for passphrase and unlocks encrypted keys
func unlockKeys() error {
	// Check if passphrase was provided via flag
	if unlockPassphrase != "" {
		return cfg.Unlock(unlockPassphrase)
	}

	// Prompt for passphrase
	fmt.Print("Encryption passphrase: ")
	passphraseBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("error reading passphrase: %w", err)
	}

	return cfg.Unlock(string(passphraseBytes))
}

// promptEncryptionPassphrase prompts for a new encryption passphrase with confirmation
func promptEncryptionPassphrase(reader *bufio.Reader) (string, error) {
	fmt.Println()
	fmt.Println("Set up encryption passphrase to protect your private keys.")
	fmt.Println("This passphrase will be required each time you start the client.")
	fmt.Println("Leave blank to skip encryption (not recommended).")
	fmt.Println()

	fmt.Print("Encryption passphrase: ")
	passphraseBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("error reading passphrase: %w", err)
	}

	passphrase := string(passphraseBytes)
	if passphrase == "" {
		fmt.Println("Warning: Keys will be stored unencrypted!")
		return "", nil
	}

	fmt.Print("Confirm passphrase: ")
	confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("error reading passphrase: %w", err)
	}

	if passphrase != string(confirmBytes) {
		return "", fmt.Errorf("passphrases do not match")
	}

	return passphrase, nil
}

func runInteractive() {
	if !cfg.IsLoggedIn() {
		fmt.Println("Not logged in. Please run 'e2e-chat login' or 'e2e-chat register' first.")
		os.Exit(1)
	}

	p := tea.NewProgram(
		tui.NewModel(client),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runRegister(cmd *cobra.Command, args []string) {
	username, _ := cmd.Flags().GetString("username")
	email, _ := cmd.Flags().GetString("email")
	device, _ := cmd.Flags().GetString("device")
	noEncrypt, _ := cmd.Flags().GetBool("no-encrypt")

	reader := bufio.NewReader(os.Stdin)

	if username == "" {
		fmt.Print("Username: ")
		username, _ = reader.ReadString('\n')
		username = strings.TrimSpace(username)
	}

	if email == "" {
		fmt.Print("Email: ")
		email, _ = reader.ReadString('\n')
		email = strings.TrimSpace(email)
	}

	fmt.Print("Password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}
	password := string(passwordBytes)

	fmt.Print("Confirm password: ")
	confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}

	if password != string(confirmBytes) {
		fmt.Fprintln(os.Stderr, "Passwords do not match")
		os.Exit(1)
	}

	// Prompt for encryption passphrase
	var encryptionPassphrase string
	if !noEncrypt {
		encryptionPassphrase, err = promptEncryptionPassphrase(reader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	if device == "" {
		device, _ = os.Hostname()
		if device == "" {
			device = "Linux Client"
		}
	}

	// Initialize client for registration
	client = api.NewClient(cfg)

	fmt.Println("Generating encryption keys...")
	if err := client.RegisterWithEncryption(username, email, password, device, encryptionPassphrase); err != nil {
		fmt.Fprintf(os.Stderr, "Registration failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Registration successful!")
	fmt.Printf("  Username: %s\n", cfg.Username)
	fmt.Printf("  Device: %s\n", device)
	fmt.Printf("  Key fingerprint: %s\n", crypto.CalculateFingerprint(cfg.PublicKey))
	if encryptionPassphrase != "" {
		fmt.Println("  Keys encrypted: yes")
	} else {
		fmt.Println("  Keys encrypted: no (not recommended)")
	}
	fmt.Println("\nRun 'e2e-chat' to start the interactive chat.")
}

func runLogin(cmd *cobra.Command, args []string) {
	email, _ := cmd.Flags().GetString("email")
	device, _ := cmd.Flags().GetString("device")
	noEncrypt, _ := cmd.Flags().GetBool("no-encrypt")

	reader := bufio.NewReader(os.Stdin)

	if email == "" {
		fmt.Print("Email: ")
		email, _ = reader.ReadString('\n')
		email = strings.TrimSpace(email)
	}

	fmt.Print("Password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}
	password := string(passwordBytes)

	// Prompt for encryption passphrase
	var encryptionPassphrase string
	if !noEncrypt {
		encryptionPassphrase, err = promptEncryptionPassphrase(reader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	if device == "" {
		device, _ = os.Hostname()
		if device == "" {
			device = "Linux Client"
		}
	}

	// Initialize client for login
	client = api.NewClient(cfg)

	fmt.Println("Logging in...")
	if err := client.LoginWithEncryption(email, password, device, encryptionPassphrase); err != nil {
		fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Login successful!")
	fmt.Printf("  Username: %s\n", cfg.Username)
	fmt.Printf("  Device: %s\n", device)
	if encryptionPassphrase != "" {
		fmt.Println("  Keys encrypted: yes")
	} else {
		fmt.Println("  Keys encrypted: no (not recommended)")
	}
	fmt.Println("\nRun 'e2e-chat' to start the interactive chat.")
}

func runLogout(cmd *cobra.Command, args []string) {
	if err := client.Logout(); err != nil {
		fmt.Fprintf(os.Stderr, "Logout error: %v\n", err)
	}
	fmt.Println("✓ Logged out successfully")
}

func runStatus(cmd *cobra.Command, args []string) {
	if !cfg.IsLoggedIn() {
		fmt.Println("Status: Not logged in")
		fmt.Printf("Server: %s\n", cfg.ServerURL)
		return
	}

	fmt.Println("Status: Logged in")
	fmt.Printf("  Username: %s\n", cfg.Username)
	fmt.Printf("  Email: %s\n", cfg.Email)
	fmt.Printf("  User ID: %s\n", cfg.UserID)
	fmt.Printf("  Device ID: %s\n", cfg.DeviceID)
	fmt.Printf("  Server: %s\n", cfg.ServerURL)
	fmt.Printf("  Key fingerprint: %s\n", crypto.CalculateFingerprint(cfg.PublicKey))

	// Show encryption status
	if cfg.IsEncrypted() {
		fmt.Println("  Keys encrypted: yes")
		if cfg.IsUnlocked() {
			fmt.Println("  Keys unlocked: yes")
		} else {
			fmt.Println("  Keys unlocked: no")
		}
	} else {
		fmt.Println("  Keys encrypted: no")
	}
}

func runChats(cmd *cobra.Command, args []string) {
	if !cfg.IsLoggedIn() {
		fmt.Fprintln(os.Stderr, "Not logged in. Run 'e2e-chat login' first.")
		os.Exit(1)
	}

	chats, err := client.GetChats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching chats: %v\n", err)
		os.Exit(1)
	}

	if len(chats) == 0 {
		fmt.Println("No chats found.")
		return
	}

	fmt.Printf("%-36s  %-20s  %s\n", "CHAT ID", "NAME", "LAST MESSAGE")
	fmt.Println(strings.Repeat("-", 80))

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
		if name == "" {
			name = "Chat"
		}

		lastMsg := ""
		if chat.LastMessage != nil {
			lastMsg = chat.LastMessage.Content
			if len(lastMsg) > 30 {
				lastMsg = lastMsg[:27] + "..."
			}
		}

		fmt.Printf("%-36s  %-20s  %s\n", chat.Chat.ID, name, lastMsg)
	}
}

func runSend(cmd *cobra.Command, args []string) {
	if !cfg.IsLoggedIn() {
		fmt.Fprintln(os.Stderr, "Not logged in. Run 'e2e-chat login' first.")
		os.Exit(1)
	}

	to, _ := cmd.Flags().GetString("to")
	message, _ := cmd.Flags().GetString("message")
	if message == "" {
		message, _ = cmd.Flags().GetString("msg")
	}

	if to == "" {
		fmt.Fprintln(os.Stderr, "Error: --to flag is required")
		os.Exit(1)
	}

	if message == "" {
		fmt.Fprintln(os.Stderr, "Error: --message flag is required")
		os.Exit(1)
	}

	// Check if 'to' is a chat ID (UUID format) or username
	chatID := to
	if !isUUID(to) {
		// Search for user and create/find chat
		users, err := client.SearchUsers(to)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error searching users: %v\n", err)
			os.Exit(1)
		}

		var targetUser *api.User
		for _, u := range users {
			if u.Username == to {
				targetUser = &u
				break
			}
		}

		if targetUser == nil {
			fmt.Fprintf(os.Stderr, "User '%s' not found\n", to)
			os.Exit(1)
		}

		chat, err := client.CreateChat([]string{targetUser.ID}, false, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating chat: %v\n", err)
			os.Exit(1)
		}
		chatID = chat.ID
	}

	// Connect WebSocket and send message
	if err := client.ConnectWS(); err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting: %v\n", err)
		os.Exit(1)
	}
	defer client.DisconnectWS()

	if err := client.SendMessage(chatID, message); err != nil {
		fmt.Fprintf(os.Stderr, "Error sending message: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Message sent")
}

func runSearch(cmd *cobra.Command, args []string) {
	if !cfg.IsLoggedIn() {
		fmt.Fprintln(os.Stderr, "Not logged in. Run 'e2e-chat login' first.")
		os.Exit(1)
	}

	query := strings.Join(args, " ")
	users, err := client.SearchUsers(query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error searching: %v\n", err)
		os.Exit(1)
	}

	if len(users) == 0 {
		fmt.Println("No users found.")
		return
	}

	fmt.Printf("%-36s  %-20s  %s\n", "USER ID", "USERNAME", "EMAIL")
	fmt.Println(strings.Repeat("-", 80))

	for _, user := range users {
		fmt.Printf("%-36s  %-20s  %s\n", user.ID, user.Username, user.Email)
	}
}

func runMessages(cmd *cobra.Command, args []string) {
	if !cfg.IsLoggedIn() {
		fmt.Fprintln(os.Stderr, "Not logged in. Run 'e2e-chat login' first.")
		os.Exit(1)
	}

	chatID := args[0]
	limit, _ := cmd.Flags().GetInt("limit")

	messages, err := client.GetMessages(chatID, limit, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching messages: %v\n", err)
		os.Exit(1)
	}

	if len(messages) == 0 {
		fmt.Println("No messages in this chat.")
		return
	}

	// Print in chronological order (oldest first)
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		sender := msg.SenderID
		if msg.SenderID == cfg.UserID {
			sender = "You"
		}
		timeStr := msg.CreatedAt.Format("2006-01-02 15:04:05")
		fmt.Printf("[%s] %s: %s\n", timeStr, sender, msg.Content)
	}
}

func runFingerprint(cmd *cobra.Command, args []string) {
	if !cfg.IsLoggedIn() {
		fmt.Fprintln(os.Stderr, "Not logged in. Run 'e2e-chat login' first.")
		os.Exit(1)
	}

	fmt.Println("Your Key Fingerprint:")
	fmt.Println()
	fmt.Printf("  %s\n", crypto.CalculateFingerprint(cfg.PublicKey))
	fmt.Println()
	fmt.Println("Share this fingerprint with contacts to verify your identity.")
	fmt.Println("Compare fingerprints in person or via a trusted channel.")
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

func runCacheShow(cmd *cobra.Command, args []string) {
	if !cfg.IsLoggedIn() {
		fmt.Fprintln(os.Stderr, "Not logged in.")
		os.Exit(1)
	}

	cache := client.GetCache()
	if cache == nil {
		fmt.Fprintln(os.Stderr, "Cache not available")
		os.Exit(1)
	}

	limit, _ := cmd.Flags().GetInt("limit")

	if len(args) > 0 {
		// Show messages for specific chat
		chatID := args[0]
		messages, err := client.GetCachedMessagesChronological(chatID, limit, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Cached messages for chat %s:\n", chatID)
		fmt.Println(strings.Repeat("-", 80))
		for _, msg := range messages {
			sender := msg.SenderID
			if msg.SenderID == cfg.UserID {
				sender = "You"
			}
			fmt.Printf("[%s] %s: %s\n", msg.CreatedAt.Format("2006-01-02 15:04:05"), sender, msg.Content)
		}
	} else {
		// Show all cached chats
		chats, err := client.GetCachedChats()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if len(chats) == 0 {
			fmt.Println("No cached chats. Run 'e2e-chat chats' to sync with server.")
			return
		}

		fmt.Printf("%-36s  %-20s  %s\n", "CHAT ID", "NAME", "LAST MESSAGE")
		fmt.Println(strings.Repeat("-", 80))

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
			if name == "" {
				name = "Chat"
			}

			lastMsg := ""
			if chat.LastMessage != nil {
				lastMsg = chat.LastMessage.Content
				if len(lastMsg) > 30 {
					lastMsg = lastMsg[:27] + "..."
				}
			}

			fmt.Printf("%-36s  %-20s  %s\n", chat.Chat.ID, name, lastMsg)
		}
	}
}

func runCacheStats(cmd *cobra.Command, args []string) {
	cache := client.GetCache()
	if cache == nil {
		fmt.Fprintln(os.Stderr, "Cache not available")
		os.Exit(1)
	}

	chats, _ := cache.GetAllChats()
	users, _ := cache.GetAllUsers()

	totalMessages := 0
	for _, chat := range chats {
		count, _ := cache.GetMessageCount(chat.ID)
		totalMessages += count
	}

	fmt.Println("Cache Statistics:")
	fmt.Printf("  Chats:    %d\n", len(chats))
	fmt.Printf("  Users:    %d\n", len(users))
	fmt.Printf("  Messages: %d\n", totalMessages)

	// Show cache file info
	homeDir, _ := os.UserHomeDir()
	dbPath := homeDir + "/.config/e2e-chat/cache.db"
	if info, err := os.Stat(dbPath); err == nil {
		fmt.Printf("  DB Size:  %.2f KB\n", float64(info.Size())/1024)
	}
}

func runCacheClear(cmd *cobra.Command, args []string) {
	cache := client.GetCache()
	if cache == nil {
		fmt.Fprintln(os.Stderr, "Cache not available")
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Are you sure you want to clear the cache? (y/N): ")
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response != "y" && response != "yes" {
		fmt.Println("Cancelled.")
		return
	}

	if err := cache.ClearAll(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Cache cleared")
}

func runEncrypt(cmd *cobra.Command, args []string) {
	if !cfg.IsLoggedIn() {
		fmt.Fprintln(os.Stderr, "Not logged in. Run 'e2e-chat login' first.")
		os.Exit(1)
	}

	if cfg.IsEncrypted() {
		fmt.Fprintln(os.Stderr, "Keys are already encrypted. Use 'change-passphrase' to change the passphrase.")
		os.Exit(1)
	}

	if !cfg.NeedsEncryption() {
		fmt.Fprintln(os.Stderr, "No keys found to encrypt.")
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	passphrase, err := promptEncryptionPassphrase(reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if passphrase == "" {
		fmt.Println("Cancelled - no passphrase provided.")
		return
	}

	if err := cfg.Encrypt(passphrase); err != nil {
		fmt.Fprintf(os.Stderr, "Encryption failed: %v\n", err)
		os.Exit(1)
	}

	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Keys encrypted successfully!")
	fmt.Println("You will need to enter the passphrase each time you start the client.")
}

func runChangePassphrase(cmd *cobra.Command, args []string) {
	if !cfg.IsLoggedIn() {
		fmt.Fprintln(os.Stderr, "Not logged in. Run 'e2e-chat login' first.")
		os.Exit(1)
	}

	if !cfg.IsEncrypted() {
		fmt.Fprintln(os.Stderr, "Keys are not encrypted. Use 'encrypt' to encrypt them first.")
		os.Exit(1)
	}

	// First, unlock with current passphrase if not already unlocked
	if !cfg.IsUnlocked() {
		fmt.Println("Enter current passphrase to unlock keys:")
		fmt.Print("Current passphrase: ")
		currentBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading passphrase: %v\n", err)
			os.Exit(1)
		}

		if err := cfg.Unlock(string(currentBytes)); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to unlock: %v\n", err)
			os.Exit(1)
		}
	}

	// Now prompt for new passphrase
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("\nEnter new passphrase:")
	newPassphrase, err := promptEncryptionPassphrase(reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if newPassphrase == "" {
		fmt.Println("Cancelled - no passphrase provided.")
		return
	}

	// Re-encrypt with new passphrase
	if err := cfg.Encrypt(newPassphrase); err != nil {
		fmt.Fprintf(os.Stderr, "Encryption failed: %v\n", err)
		os.Exit(1)
	}

	if err := cfg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Passphrase changed successfully!")
}
