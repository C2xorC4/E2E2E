package crypto

import (
	"crypto/rand"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
)

const (
	// Key sizes
	KeySize   = 32
	NonceSize = 24 // XChaCha20-Poly1305 nonce size

	// Argon2 parameters (OWASP recommended minimums)
	ArgonTime    = 3         // 3 iterations for stronger protection
	ArgonMemory  = 64 * 1024 // 64 MB memory
	ArgonThreads = 4
	ArgonKeyLen  = 32
	SaltSize     = 16
)

var (
	ErrInvalidKeySize   = errors.New("invalid key size")
	ErrInvalidNonceSize = errors.New("invalid nonce size")
	ErrDecryptionFailed = errors.New("decryption failed")
)

// KeyPair represents an X25519 key pair
type KeyPair struct {
	PublicKey  [KeySize]byte
	PrivateKey [KeySize]byte
}

// GenerateKeyPair generates a new X25519 key pair
func GenerateKeyPair() (*KeyPair, error) {
	var privateKey [KeySize]byte
	if _, err := io.ReadFull(rand.Reader, privateKey[:]); err != nil {
		return nil, err
	}

	// Clamp the private key for X25519
	privateKey[0] &= 248
	privateKey[31] &= 127
	privateKey[31] |= 64

	var publicKey [KeySize]byte
	curve25519.ScalarBaseMult(&publicKey, &privateKey)

	return &KeyPair{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}, nil
}

// ComputeSharedSecret computes the X25519 shared secret
func ComputeSharedSecret(privateKey, peerPublicKey []byte) ([]byte, error) {
	if len(privateKey) != KeySize || len(peerPublicKey) != KeySize {
		return nil, ErrInvalidKeySize
	}

	var priv, pub, shared [KeySize]byte
	copy(priv[:], privateKey)
	copy(pub[:], peerPublicKey)

	curve25519.ScalarMult(&shared, &priv, &pub)

	// Derive encryption key using BLAKE2b
	key := blake2b.Sum256(shared[:])
	return key[:], nil
}

// GenerateNonce generates a random nonce for XChaCha20-Poly1305
func GenerateNonce() ([]byte, error) {
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return nonce, nil
}

// Encrypt encrypts plaintext using XChaCha20-Poly1305
func Encrypt(key, plaintext, nonce []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}
	if len(nonce) != NonceSize {
		return nil, ErrInvalidNonceSize
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}

	return aead.Seal(nil, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext using XChaCha20-Poly1305
func Decrypt(key, ciphertext, nonce []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}
	if len(nonce) != NonceSize {
		return nil, ErrInvalidNonceSize
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// HashPassword hashes a password using Argon2id
func HashPassword(password string) ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}

	hash := argon2.IDKey([]byte(password), salt, ArgonTime, ArgonMemory, ArgonThreads, ArgonKeyLen)

	// Prepend salt to hash
	result := make([]byte, SaltSize+ArgonKeyLen)
	copy(result[:SaltSize], salt)
	copy(result[SaltSize:], hash)

	return result, nil
}

// VerifyPassword verifies a password against a hash
func VerifyPassword(password string, storedHash []byte) bool {
	if len(storedHash) != SaltSize+ArgonKeyLen {
		return false
	}

	salt := storedHash[:SaltSize]
	expectedHash := storedHash[SaltSize:]

	computedHash := argon2.IDKey([]byte(password), salt, ArgonTime, ArgonMemory, ArgonThreads, ArgonKeyLen)

	// Constant-time comparison
	if len(computedHash) != len(expectedHash) {
		return false
	}
	var diff byte
	for i := 0; i < len(computedHash); i++ {
		diff |= computedHash[i] ^ expectedHash[i]
	}
	return diff == 0
}

// GenerateToken generates a random token for session authentication
func GenerateToken() (string, error) {
	token := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, token); err != nil {
		return "", err
	}
	return encodeBase64(token), nil
}

// HashToken hashes a token for storage
func HashToken(token string) []byte {
	hash := blake2b.Sum256([]byte(token))
	return hash[:]
}

// encodeBase64 encodes bytes to base64 URL-safe string
func encodeBase64(data []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	result := make([]byte, (len(data)*8+5)/6)
	for i := range result {
		bitIndex := i * 6
		byteIndex := bitIndex / 8
		bitOffset := bitIndex % 8
		var val byte
		if bitOffset <= 2 {
			val = (data[byteIndex] >> (2 - bitOffset)) & 0x3F
		} else {
			val = (data[byteIndex] << (bitOffset - 2)) & 0x3F
			if byteIndex+1 < len(data) {
				val |= data[byteIndex+1] >> (10 - bitOffset)
			}
		}
		result[i] = alphabet[val]
	}
	return string(result)
}

// CryptoService provides high-level encryption operations
type CryptoService struct{}

// NewCryptoService creates a new CryptoService
func NewCryptoService() *CryptoService {
	return &CryptoService{}
}

// EncryptForUser encrypts a message for a specific user using their public key and our private key
func (s *CryptoService) EncryptForUser(serverPrivateKey, userPublicKey, plaintext []byte) (ciphertext, nonce []byte, err error) {
	sharedKey, err := ComputeSharedSecret(serverPrivateKey, userPublicKey)
	if err != nil {
		return nil, nil, err
	}

	nonce, err = GenerateNonce()
	if err != nil {
		return nil, nil, err
	}

	ciphertext, err = Encrypt(sharedKey, plaintext, nonce)
	if err != nil {
		return nil, nil, err
	}

	return ciphertext, nonce, nil
}

// DecryptFromUser decrypts a message from a user using their public key and our private key
func (s *CryptoService) DecryptFromUser(serverPrivateKey, userPublicKey, ciphertext, nonce []byte) ([]byte, error) {
	sharedKey, err := ComputeSharedSecret(serverPrivateKey, userPublicKey)
	if err != nil {
		return nil, err
	}

	return Decrypt(sharedKey, ciphertext, nonce)
}
