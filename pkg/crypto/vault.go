package crypto

import (
	"context"
	"errors"
)

var (
	// ErrDecryptionFailed is returned when AES-GCM authentication fails or payload is corrupted.
	ErrDecryptionFailed = errors.New("crypto: authenticated decryption failed or corrupt payload")

	// ErrInvalidMasterKey is returned when the master secret key does not meet length/security requirements.
	ErrInvalidMasterKey = errors.New("crypto: invalid master secret key")

	// ErrInvalidEnvelope is returned when the encrypted envelope string does not match the v1 format.
	ErrInvalidEnvelope = errors.New("crypto: invalid envelope format")

	// ErrInvalidHashFormat is returned when an Argon2id hash cannot be parsed.
	ErrInvalidHashFormat = errors.New("crypto: invalid argon2id hash format")
)

// Vault handles field-level AES-256-GCM encryption with Scrypt key derivation.
type Vault interface {
	// Encrypt encrypts plaintext bytes and returns a versioned envelope string.
	// Format: v1:base64(iv):base64(authTag):base64(ciphertext)
	Encrypt(ctx context.Context, plaintext []byte) (string, error)

	// Decrypt parses an envelope string, verifies authentication tag, and returns plaintext.
	Decrypt(ctx context.Context, envelope string) ([]byte, error)

	// EncryptString is a helper for UTF-8 strings.
	EncryptString(ctx context.Context, plainText string) (string, error)

	// DecryptString is a helper for UTF-8 strings.
	DecryptString(ctx context.Context, envelope string) (string, error)
}
