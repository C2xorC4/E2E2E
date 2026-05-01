package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"e2e-chat/internal/crypto"
	"e2e-chat/internal/db"
	"e2e-chat/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserExists         = errors.New("user already exists")
	ErrInvalidToken       = errors.New("invalid or expired token")
)

const (
	SessionDuration = 30 * 24 * time.Hour // 30 days
)

// Service handles authentication operations
type Service struct {
	db     *db.DB
	crypto *crypto.CryptoService
}

// NewService creates a new auth service
func NewService(database *db.DB) *Service {
	return &Service{
		db:     database,
		crypto: crypto.NewCryptoService(),
	}
}

// Register creates a new user account
func (s *Service) Register(ctx context.Context, req *models.RegisterRequest) (*models.RegisterResponse, error) {
	// Check if user exists
	existing, _ := s.db.GetUserByEmail(ctx, req.Email)
	if existing != nil {
		return nil, ErrUserExists
	}

	// Hash password
	passwordHash, err := crypto.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	// Create user
	user, err := s.db.CreateUser(ctx, req.Username, req.Email, passwordHash, req.PublicKey)
	if err != nil {
		return nil, err
	}

	// Create device with profile if provided
	var device *models.Device
	if req.DeviceProfile != nil {
		device, err = s.db.CreateDeviceWithProfile(ctx, user.ID, req.DeviceName, req.DeviceKey, req.DeviceProfile)
	} else {
		device, err = s.db.CreateDevice(ctx, user.ID, req.DeviceName, req.DeviceKey)
	}
	if err != nil {
		return nil, err
	}

	// Generate server key for this user
	serverKey, err := s.db.CreateServerKey(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	// Create session
	token, err := crypto.GenerateToken()
	if err != nil {
		return nil, err
	}

	tokenHash := hex.EncodeToString(crypto.HashToken(token))
	_, err = s.db.CreateSession(ctx, user.ID, &device.ID, tokenHash, time.Now().Add(SessionDuration))
	if err != nil {
		return nil, err
	}

	return &models.RegisterResponse{
		User:            user,
		Device:          device,
		Token:           token,
		ServerPublicKey: serverKey.PublicKey,
	}, nil
}

// Login authenticates a user
func (s *Service) Login(ctx context.Context, req *models.LoginRequest) (*models.LoginResponse, error) {
	// Get user
	user, err := s.db.GetUserByEmail(ctx, req.Email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	// Verify password
	passwordHash, err := hex.DecodeString(user.PasswordHash)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if !crypto.VerifyPassword(req.Password, passwordHash) {
		return nil, ErrInvalidCredentials
	}

	// Create or get device
	var device *models.Device
	if req.DeviceProfile != nil {
		device, err = s.db.CreateDeviceWithProfile(ctx, user.ID, req.DeviceName, req.DeviceKey, req.DeviceProfile)
	} else {
		device, err = s.db.CreateDevice(ctx, user.ID, req.DeviceName, req.DeviceKey)
	}
	if err != nil {
		// Device might already exist, try to find it and update its profile
		devices, err := s.db.GetDevicesByUserID(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		for _, d := range devices {
			if d.DeviceName == req.DeviceName {
				device = d
				// Update profile if provided
				if req.DeviceProfile != nil {
					s.db.UpdateDeviceProfile(ctx, d.ID, req.DeviceProfile)
				}
				break
			}
		}
		if device == nil {
			return nil, err
		}
	}

	// Get server key
	serverKey, err := s.db.GetServerKeyByUserID(ctx, user.ID)
	if err != nil {
		// Create new server key if not exists
		serverKey, err = s.db.CreateServerKey(ctx, user.ID)
		if err != nil {
			return nil, err
		}
	}

	// Create session
	token, err := crypto.GenerateToken()
	if err != nil {
		return nil, err
	}

	tokenHash := hex.EncodeToString(crypto.HashToken(token))
	_, err = s.db.CreateSession(ctx, user.ID, &device.ID, tokenHash, time.Now().Add(SessionDuration))
	if err != nil {
		return nil, err
	}

	// Update last seen
	s.db.UpdateUserLastSeen(ctx, user.ID)

	return &models.LoginResponse{
		User:            user,
		Device:          device,
		Token:           token,
		ServerPublicKey: serverKey.PublicKey,
	}, nil
}

// ValidateToken validates a session token and returns the session
func (s *Service) ValidateToken(ctx context.Context, token string) (*models.Session, error) {
	tokenHash := hex.EncodeToString(crypto.HashToken(token))
	session, err := s.db.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Update last used
	s.db.UpdateSessionLastUsed(ctx, session.ID)

	return session, nil
}

// Logout invalidates a session
func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return s.db.DeleteSession(ctx, sessionID)
}

// Middleware returns a Gin middleware for authentication
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		// Also check query parameter for WebSocket connections
		if authHeader == "" {
			if token := c.Query("token"); token != "" {
				authHeader = "Bearer " + token
				log.Printf("Auth: Using token from query parameter")
			}
		}

		if authHeader == "" {
			log.Printf("Auth: Missing authorization - path=%s, headers=%v, query=%s", c.Request.URL.Path, c.Request.Header, c.Request.URL.RawQuery)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header"})
			return
		}

		session, err := s.ValidateToken(c.Request.Context(), parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		// Get user
		user, err := s.db.GetUserByID(c.Request.Context(), session.UserID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			return
		}

		// Store session and user in context
		c.Set("session", session)
		c.Set("user", user)
		c.Set("userID", session.UserID)

		if session.DeviceID != nil {
			c.Set("deviceID", *session.DeviceID)
		}

		c.Next()
	}
}

// GetUserFromContext extracts the user from the Gin context
func GetUserFromContext(c *gin.Context) *models.User {
	user, exists := c.Get("user")
	if !exists {
		return nil
	}
	return user.(*models.User)
}

// GetSessionFromContext extracts the session from the Gin context
func GetSessionFromContext(c *gin.Context) *models.Session {
	session, exists := c.Get("session")
	if !exists {
		return nil
	}
	return session.(*models.Session)
}

// GetUserIDFromContext extracts the user ID from the Gin context
func GetUserIDFromContext(c *gin.Context) uuid.UUID {
	userID, exists := c.Get("userID")
	if !exists {
		return uuid.Nil
	}
	return userID.(uuid.UUID)
}

// GetDeviceIDFromContext extracts the device ID from the Gin context
func GetDeviceIDFromContext(c *gin.Context) *uuid.UUID {
	deviceID, exists := c.Get("deviceID")
	if !exists {
		return nil
	}
	id := deviceID.(uuid.UUID)
	return &id
}
