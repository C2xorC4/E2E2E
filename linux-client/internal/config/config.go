package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"e2e-client/internal/crypto"
)

var (
	ErrNotEncrypted     = errors.New("config is not encrypted")
	ErrAlreadyEncrypted = errors.New("config is already encrypted")
	ErrKeysNotUnlocked  = errors.New("keys are not unlocked - call Unlock first")
	ErrInvalidPassword  = errors.New("invalid password")
)

type Config struct {
	ServerURL        string `json:"server_url"`
	UserID           string `json:"user_id"`
	Username         string `json:"username"`
	Email            string `json:"email"`
	DeviceID         string `json:"device_id"`
	Token            string `json:"token"`
	PublicKey        []byte `json:"public_key"`
	DevicePublicKey  []byte `json:"device_public_key"`
	ServerPublicKey  []byte `json:"server_public_key"`

	// Encrypted fields (stored on disk)
	EncryptedPrivateKey       []byte `json:"encrypted_private_key,omitempty"`
	EncryptedDevicePrivateKey []byte `json:"encrypted_device_private_key,omitempty"`
	EncryptedCacheKey         []byte `json:"encrypted_cache_key,omitempty"`
	Salt                      []byte `json:"salt,omitempty"`

	// Legacy unencrypted fields (for migration, not saved when encrypted)
	PrivateKey       []byte `json:"private_key,omitempty"`
	DevicePrivateKey []byte `json:"device_private_key,omitempty"`

	// Runtime fields (not persisted)
	unlocked         bool   // whether keys have been decrypted
	decryptedPrivKey []byte // decrypted private key (in memory only)
	decryptedDevKey  []byte // decrypted device private key (in memory only)
	cacheKey         []byte // decrypted cache encryption key (in memory only)
}

func configPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	configDir := filepath.Join(homeDir, ".config", "e2e-chat")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", err
	}

	return filepath.Join(configDir, "config.json"), nil
}

func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{
				ServerURL: "http://localhost:8080",
			}, nil
		}
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c *Config) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

func (c *Config) IsLoggedIn() bool {
	return c.Token != "" && c.UserID != ""
}

func (c *Config) Clear() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// IsEncrypted returns true if the config has encrypted keys
func (c *Config) IsEncrypted() bool {
	return len(c.Salt) > 0 && len(c.EncryptedPrivateKey) > 0
}

// NeedsEncryption returns true if the config has unencrypted keys that should be encrypted
func (c *Config) NeedsEncryption() bool {
	return len(c.PrivateKey) > 0 && !c.IsEncrypted()
}

// IsUnlocked returns true if the keys have been decrypted and are available
func (c *Config) IsUnlocked() bool {
	return c.unlocked
}

// Encrypt encrypts the private keys with the given passphrase
// This should be called when setting up encryption for the first time
func (c *Config) Encrypt(passphrase string) error {
	if c.IsEncrypted() && !c.unlocked {
		return ErrAlreadyEncrypted
	}

	// Get the keys to encrypt (either from decrypted runtime values or legacy fields)
	var privateKey, devicePrivateKey []byte
	if c.unlocked {
		privateKey = c.decryptedPrivKey
		devicePrivateKey = c.decryptedDevKey
	} else {
		privateKey = c.PrivateKey
		devicePrivateKey = c.DevicePrivateKey
	}

	if len(privateKey) == 0 || len(devicePrivateKey) == 0 {
		return errors.New("no keys to encrypt")
	}

	// Generate salt
	salt, err := crypto.GenerateSalt()
	if err != nil {
		return err
	}

	// Derive encryption key from passphrase
	encKey, err := crypto.DeriveKeyFromPassphrase(passphrase, salt)
	if err != nil {
		return err
	}

	// Encrypt private keys
	encPrivKey, err := crypto.EncryptWithKey(encKey, privateKey)
	if err != nil {
		return err
	}

	encDevKey, err := crypto.EncryptWithKey(encKey, devicePrivateKey)
	if err != nil {
		return err
	}

	// Derive and encrypt cache key
	cacheKey, err := crypto.DeriveCacheKey(devicePrivateKey)
	if err != nil {
		return err
	}

	encCacheKey, err := crypto.EncryptWithKey(encKey, cacheKey)
	if err != nil {
		return err
	}

	// Update config
	c.Salt = salt
	c.EncryptedPrivateKey = encPrivKey
	c.EncryptedDevicePrivateKey = encDevKey
	c.EncryptedCacheKey = encCacheKey

	// Clear legacy unencrypted fields
	c.PrivateKey = nil
	c.DevicePrivateKey = nil

	// Store decrypted keys in memory
	c.decryptedPrivKey = privateKey
	c.decryptedDevKey = devicePrivateKey
	c.cacheKey = cacheKey
	c.unlocked = true

	return nil
}

// Unlock decrypts the private keys using the given passphrase
func (c *Config) Unlock(passphrase string) error {
	if !c.IsEncrypted() {
		// Handle legacy unencrypted config
		if len(c.PrivateKey) > 0 && len(c.DevicePrivateKey) > 0 {
			c.decryptedPrivKey = c.PrivateKey
			c.decryptedDevKey = c.DevicePrivateKey
			// Derive cache key from device private key
			cacheKey, err := crypto.DeriveCacheKey(c.DevicePrivateKey)
			if err != nil {
				return err
			}
			c.cacheKey = cacheKey
			c.unlocked = true
			return nil
		}
		return ErrNotEncrypted
	}

	// Derive decryption key from passphrase
	decKey, err := crypto.DeriveKeyFromPassphrase(passphrase, c.Salt)
	if err != nil {
		return err
	}

	// Decrypt private key
	privateKey, err := crypto.DecryptWithKey(decKey, c.EncryptedPrivateKey)
	if err != nil {
		return ErrInvalidPassword
	}

	// Decrypt device private key
	devicePrivateKey, err := crypto.DecryptWithKey(decKey, c.EncryptedDevicePrivateKey)
	if err != nil {
		return ErrInvalidPassword
	}

	// Decrypt cache key
	cacheKey, err := crypto.DecryptWithKey(decKey, c.EncryptedCacheKey)
	if err != nil {
		return ErrInvalidPassword
	}

	// Store decrypted keys in memory
	c.decryptedPrivKey = privateKey
	c.decryptedDevKey = devicePrivateKey
	c.cacheKey = cacheKey
	c.unlocked = true

	return nil
}

// GetPrivateKey returns the decrypted private key
func (c *Config) GetPrivateKey() ([]byte, error) {
	if !c.unlocked {
		// Handle legacy unencrypted config
		if len(c.PrivateKey) > 0 {
			return c.PrivateKey, nil
		}
		return nil, ErrKeysNotUnlocked
	}
	return c.decryptedPrivKey, nil
}

// GetDevicePrivateKey returns the decrypted device private key
func (c *Config) GetDevicePrivateKey() ([]byte, error) {
	if !c.unlocked {
		// Handle legacy unencrypted config
		if len(c.DevicePrivateKey) > 0 {
			return c.DevicePrivateKey, nil
		}
		return nil, ErrKeysNotUnlocked
	}
	return c.decryptedDevKey, nil
}

// GetCacheKey returns the cache encryption key
func (c *Config) GetCacheKey() ([]byte, error) {
	if !c.unlocked {
		// Handle legacy unencrypted config - derive cache key
		if len(c.DevicePrivateKey) > 0 {
			return crypto.DeriveCacheKey(c.DevicePrivateKey)
		}
		return nil, ErrKeysNotUnlocked
	}
	return c.cacheKey, nil
}

// SetKeysAndEncrypt sets the private keys and encrypts them with the passphrase
// Used during registration/login when creating new keys
func (c *Config) SetKeysAndEncrypt(privateKey, devicePrivateKey []byte, passphrase string) error {
	// Temporarily store keys for encryption
	c.decryptedPrivKey = privateKey
	c.decryptedDevKey = devicePrivateKey
	c.unlocked = true

	return c.Encrypt(passphrase)
}

// Lock clears the decrypted keys from memory
func (c *Config) Lock() {
	// Securely clear decrypted keys from memory
	for i := range c.decryptedPrivKey {
		c.decryptedPrivKey[i] = 0
	}
	for i := range c.decryptedDevKey {
		c.decryptedDevKey[i] = 0
	}
	for i := range c.cacheKey {
		c.cacheKey[i] = 0
	}

	c.decryptedPrivKey = nil
	c.decryptedDevKey = nil
	c.cacheKey = nil
	c.unlocked = false
}
