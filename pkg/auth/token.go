package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

const (
	// DefaultTokenPrefix is the production live token prefix
	DefaultTokenPrefix = "pik_live_"
	// TestTokenPrefix is for non-production / sandbox tokens
	TestTokenPrefix = "pik_test_"

	base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// GenerateRawToken generates a new scoped API token string with the given prefix.
// The resulting string is prefixed with e.g. "pik_live_" followed by 43 Base62 characters.
func GenerateRawToken(prefix string) (string, error) {
	if prefix == "" {
		prefix = DefaultTokenPrefix
	}

	const tokenLen = 43
	b := make([]byte, tokenLen)
	max := big.NewInt(int64(len(base62Chars)))

	for i := 0; i < tokenLen; i++ {
		num, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("auth: failed to generate random token: %w", err)
		}
		b[i] = base62Chars[num.Int64()]
	}

	return prefix + string(b), nil
}

// HashToken computes the SHA-256 hex digest of a raw API token secret.
func HashToken(rawSecret string) string {
	sum := sha256.Sum256([]byte(rawSecret))
	return hex.EncodeToString(sum[:])
}

// ExtractTokenPrefix extracts the first 12 characters of a token for UI display.
func ExtractTokenPrefix(rawSecret string) string {
	if len(rawSecret) >= 12 {
		return rawSecret[:12]
	}
	return rawSecret
}

// HasScope checks if a set of granted scopes satisfies the required scope.
// Supports:
// - Wildcards: "*" or "admin:*" matches any required scope.
// - Resource wildcards: "project:*" matches "project:read", "project:write", etc.
// - Exact matches: "deploy:write" matches "deploy:write".
func HasScope(grantedScopes []string, requiredScope string) bool {
	if requiredScope == "" {
		return true
	}

	reqParts := strings.Split(requiredScope, ":")
	reqResource := reqParts[0]

	for _, granted := range grantedScopes {
		if granted == "*" || granted == "admin:*" {
			return true
		}

		if granted == requiredScope {
			return true
		}

		grantedParts := strings.Split(granted, ":")
		if len(grantedParts) == 2 && grantedParts[1] == "*" {
			if grantedParts[0] == reqResource {
				return true
			}
		}
	}

	return false
}
