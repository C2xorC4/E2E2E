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
	KeySize   = 32
	NonceSize = 24
	SaltSize  = 16

	// Argon2 parameters (OWASP recommendations)
	Argon2Time    = 3
	Argon2Memory  = 64 * 1024 // 64 MB
	Argon2Threads = 4
)

var (
	ErrInvalidKeySize   = errors.New("invalid key size")
	ErrInvalidNonceSize = errors.New("invalid nonce size")
	ErrDecryptionFailed = errors.New("decryption failed")
	ErrInvalidSaltSize  = errors.New("invalid salt size")
)

type KeyPair struct {
	PublicKey  [KeySize]byte
	PrivateKey [KeySize]byte
}

func GenerateKeyPair() (*KeyPair, error) {
	var privateKey [KeySize]byte
	if _, err := io.ReadFull(rand.Reader, privateKey[:]); err != nil {
		return nil, err
	}

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

func ComputeSharedSecret(privateKey, peerPublicKey []byte) ([]byte, error) {
	if len(privateKey) != KeySize || len(peerPublicKey) != KeySize {
		return nil, ErrInvalidKeySize
	}

	var priv, pub, shared [KeySize]byte
	copy(priv[:], privateKey)
	copy(pub[:], peerPublicKey)

	curve25519.ScalarMult(&shared, &priv, &pub)

	key := blake2b.Sum256(shared[:])
	return key[:], nil
}

func GenerateNonce() ([]byte, error) {
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return nonce, nil
}

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

func CalculateFingerprint(publicKey []byte) string {
	hash := blake2b.Sum256(publicKey)
	fp := make([]byte, 0, 23)
	for i := 0; i < 8; i++ {
		if i > 0 {
			fp = append(fp, ':')
		}
		fp = append(fp, hexByte(hash[i])...)
	}
	return string(fp)
}

func hexByte(b byte) []byte {
	const hex = "0123456789abcdef"
	return []byte{hex[b>>4], hex[b&0x0f]}
}

// GenerateSalt generates a random salt for key derivation
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	return salt, nil
}

// DeriveKeyFromPassphrase derives a 32-byte encryption key from a passphrase using Argon2id
func DeriveKeyFromPassphrase(passphrase string, salt []byte) ([]byte, error) {
	if len(salt) != SaltSize {
		return nil, ErrInvalidSaltSize
	}

	key := argon2.IDKey(
		[]byte(passphrase),
		salt,
		Argon2Time,
		Argon2Memory,
		Argon2Threads,
		KeySize,
	)

	return key, nil
}

// EncryptWithKey encrypts plaintext using the provided key
// Returns ciphertext (which includes the nonce prepended)
func EncryptWithKey(key, plaintext []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}

	nonce, err := GenerateNonce()
	if err != nil {
		return nil, err
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}

	// Prepend nonce to ciphertext
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	result := make([]byte, NonceSize+len(ciphertext))
	copy(result[:NonceSize], nonce)
	copy(result[NonceSize:], ciphertext)

	return result, nil
}

// DecryptWithKey decrypts ciphertext using the provided key
// Expects nonce to be prepended to ciphertext
func DecryptWithKey(key, ciphertextWithNonce []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeySize
	}

	if len(ciphertextWithNonce) < NonceSize {
		return nil, ErrDecryptionFailed
	}

	nonce := ciphertextWithNonce[:NonceSize]
	ciphertext := ciphertextWithNonce[NonceSize:]

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

// DeriveCacheKey derives a cache encryption key from the device private key
func DeriveCacheKey(devicePrivateKey []byte) ([]byte, error) {
	if len(devicePrivateKey) != KeySize {
		return nil, ErrInvalidKeySize
	}

	// Use BLAKE2b to derive a key from the device private key
	// This ensures cache data is tied to this device
	h, err := blake2b.New256([]byte("e2e-chat-cache-key"))
	if err != nil {
		return nil, err
	}
	h.Write(devicePrivateKey)
	key := h.Sum(nil)

	return key, nil
}
