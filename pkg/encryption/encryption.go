package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// Service handles AES-256-GCM encryption with per-customer key derivation via HKDF.
type Service struct {
	masterKey []byte
}

// New creates a new encryption service.
// masterKeyBase64 should be a base64-encoded 32-byte key.
// If empty, a dev-mode default key is used.
func New(masterKeyBase64 string) (*Service, error) {
	if masterKeyBase64 == "" {
		// Dev-mode default: "bastio-dev-master-key-32-bytes!!" (exactly 32 bytes)
		masterKeyBase64 = "YmFzdGlvLWRldi1tYXN0ZXIta2V5LTMyLWJ5dGVzISE="
	}

	masterKey, err := base64.StdEncoding.DecodeString(masterKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode master key: %w", err)
	}

	if len(masterKey) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(masterKey))
	}

	return &Service{masterKey: masterKey}, nil
}

// Encrypted holds encrypted data.
type Encrypted struct {
	Ciphertext string `json:"ciphertext"` // base64
	Nonce      string `json:"nonce"`      // base64
	Version    int    `json:"version"`
}

// Encrypt encrypts plaintext using a customer-specific derived key.
func (s *Service) Encrypt(plaintext, customerID, context string) (*Encrypted, error) {
	key, err := s.deriveKey(customerID, context)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	return &Encrypted{
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Version:    1,
	}, nil
}

// Decrypt decrypts an Encrypted value using a customer-specific derived key.
func (s *Service) Decrypt(enc *Encrypted, customerID, context string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(enc.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	nonce, err := base64.StdEncoding.DecodeString(enc.Nonce)
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}

	key, err := s.deriveKey(customerID, context)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// deriveKey derives a customer-specific 32-byte key using HKDF-SHA256.
func (s *Service) deriveKey(customerID, ctx string) ([]byte, error) {
	salt := sha256.Sum256([]byte(customerID))

	info := fmt.Sprintf("bastio-customer-%s", customerID)
	if ctx != "" {
		info = fmt.Sprintf("%s-%s", info, ctx)
	}

	r := hkdf.New(sha256.New, s.masterKey, salt[:], []byte(info))

	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	return key, nil
}

// HashAPIKey hashes an API key for storage/lookup using SHA-256.
func HashAPIKey(apiKey string) string {
	h := sha256.Sum256([]byte(apiKey))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
