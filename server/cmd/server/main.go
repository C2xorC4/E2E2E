package main

import (
	"log"
	"os"
	"strings"

	"e2e-chat/internal/admin"
	"e2e-chat/internal/api"
	"e2e-chat/internal/auth"
	"e2e-chat/internal/db"
	"e2e-chat/internal/ratelimit"
	"e2e-chat/internal/ws"

	"github.com/gin-gonic/gin"
)

// splitOrigins parses comma-separated origins from environment variable
func splitOrigins(s string) []string {
	var origins []string
	for _, o := range strings.Split(s, ",") {
		if trimmed := strings.TrimSpace(o); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

func main() {
	// Get database URL from environment
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://localhost:5432/e2echat?sslmode=disable"
	}

	// Connect to database
	database, err := db.New(dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	log.Println("Connected to database")

	// Create services
	authService := auth.NewService(database)
	hub := ws.NewHub(database)
	rateLimiter := ratelimit.New()
	defer rateLimiter.Stop()

	// Start WebSocket hub
	go hub.Run()

	log.Println("Rate limiter initialized")

	// Create handler
	handler := api.NewHandler(database, authService, hub, rateLimiter)

	// Setup router
	r := gin.Default()

	// Allowed origins for CORS (configure via environment in production)
	allowedOrigins := map[string]bool{
		"http://localhost:3000":  true, // Development frontend
		"http://localhost:8080":  true, // Local testing
		"https://chat.example.com": true, // Production (update this)
	}
	if envOrigins := os.Getenv("ALLOWED_ORIGINS"); envOrigins != "" {
		for _, origin := range splitOrigins(envOrigins) {
			allowedOrigins[origin] = true
		}
	}

	// CORS middleware with origin whitelist
	r.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		if allowedOrigins[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Max-Age", "3600")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Setup API routes
	handler.SetupRoutes(r)

	// Setup admin/moderation dashboard
	adminHandler := admin.NewHandler(database)
	adminHandler.SetupRoutes(r)

	// Get port from environment
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
