package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/scrypt"
)

var defaultVaultSalt = []byte("pikpik-vault-master-salt-v1")

// AESVault implements Vault using Scrypt key derivation and AES-256-GCM.
type AESVault struct {
	derivedKey []byte
}

// NewAESVault derives a 256-bit key from masterSecret using Scrypt (N=32768, r=8, p=1).
func NewAESVault(masterSecret string) (*AESVault, error) {
	if len(masterSecret) < 16 {
		return nil, ErrInvalidMasterKey
	}

	key, err := scrypt.Key([]byte(masterSecret), defaultVaultSalt, 32768, 8, 1, 32)
	if err != nil {
		return nil, fmt.Errorf("crypto: scrypt key derivation failed: %w", err)
	}

	return &AESVault{derivedKey: key}, nil
}

// Encrypt encrypts plaintext bytes and returns an envelope formatted as:
// v1:<base64(iv)>:<base64(authTag)>:<base64(ciphertext)>
func (v *AESVault) Encrypt(ctx context.Context, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(v.derivedKey)
	if err != nil {
		return "", fmt.Errorf("crypto: new cipher error: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: new gcm error: %w", err)
	}

	// 96-bit (12-byte) unique IV per encryption
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: failed to generate nonce: %w", err)
	}

	// Seal encrypts and appends the 16-byte authentication tag
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	tagSize := 16
	if len(sealed) < tagSize {
		return "", errors.New("crypto: invalid sealed ciphertext length")
	}

	ciphertext := sealed[:len(sealed)-tagSize]
	authTag := sealed[len(sealed)-tagSize:]

	b64IV := base64.StdEncoding.EncodeToString(nonce)
	b64Tag := base64.StdEncoding.EncodeToString(authTag)
	b64Cipher := base64.StdEncoding.EncodeToString(ciphertext)

	return fmt.Sprintf("v1:%s:%s:%s", b64IV, b64Tag, b64Cipher), nil
}

// Decrypt parses the v1 envelope string, verifies the GCM auth tag, and returns the plaintext bytes.
func (v *AESVault) Decrypt(ctx context.Context, envelope string) ([]byte, error) {
	parts := strings.Split(envelope, ":")
	if len(parts) != 4 || parts[0] != "v1" {
		return nil, ErrInvalidEnvelope
	}

	nonce, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidEnvelope
	}

	authTag, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidEnvelope
	}

	ciphertext, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return nil, ErrInvalidEnvelope
	}

	block, err := aes.NewCipher(v.derivedKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher error: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm error: %w", err)
	}

	if len(nonce) != gcm.NonceSize() {
		return nil, ErrInvalidEnvelope
	}

	// Reconstruct sealed slice (ciphertext + authTag)
	sealed := append(ciphertext, authTag...)
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// EncryptString encrypts a UTF-8 string.
func (v *AESVault) EncryptString(ctx context.Context, plainText string) (string, error) {
	return v.Encrypt(ctx, []byte(plainText))
}

// DecryptString decrypts a v1 envelope to a UTF-8 string.
func (v *AESVault) DecryptString(ctx context.Context, envelope string) (string, error) {
	b, err := v.Decrypt(ctx, envelope)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
