package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2Hasher provides RFC 9106 Argon2id password hashing and verification.
type Argon2Hasher struct {
	time        uint32
	memory      uint32
	parallelism uint8
	keyLen      uint32
	saltLen     uint32
}

// DefaultArgon2Hasher returns standard secure defaults for pikpik (64MB, t=3, p=2).
func DefaultArgon2Hasher() *Argon2Hasher {
	return &Argon2Hasher{
		time:        3,
		memory:      64 * 1024, // 64 MB
		parallelism: 2,
		keyLen:      32,
		saltLen:     16,
	}
}

// FastArgon2Hasher returns lighter parameters for fast unit tests.
func FastArgon2Hasher() *Argon2Hasher {
	return &Argon2Hasher{
		time:        1,
		memory:      8 * 1024, // 8 MB
		parallelism: 1,
		keyLen:      32,
		saltLen:     16,
	}
}

// Hash generates an Argon2id PHC encoded hash string for the provided password.
func (h *Argon2Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("crypto: failed to generate random salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, h.time, h.memory, h.parallelism, h.keyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	// PHC string format: $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.time, h.parallelism, b64Salt, b64Hash,
	)
	return encoded, nil
}

// Verify verifies a plaintext password against an Argon2id PHC encoded hash string.
func (h *Argon2Hasher) Verify(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHashFormat
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, fmt.Errorf("crypto: unsupported argon2 version or corrupt header")
	}

	var memory, timeVal uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeVal, &parallelism); err != nil {
		return false, fmt.Errorf("crypto: failed to parse argon2 parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("crypto: invalid salt encoding: %w", err)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("crypto: invalid hash encoding: %w", err)
	}

	calculatedHash := argon2.IDKey([]byte(password), salt, timeVal, memory, parallelism, uint32(len(expectedHash)))

	// Constant-time comparison to prevent timing side-channel attacks
	if subtle.ConstantTimeCompare(calculatedHash, expectedHash) == 1 {
		return true, nil
	}
	return false, nil
}

// HashPassword hashes a password using the default Argon2id configuration.
func HashPassword(password string) (string, error) {
	return DefaultArgon2Hasher().Hash(password)
}

// VerifyPassword verifies a password against an Argon2id PHC hash.
func VerifyPassword(password, encodedHash string) (bool, error) {
	return DefaultArgon2Hasher().Verify(password, encodedHash)
}
