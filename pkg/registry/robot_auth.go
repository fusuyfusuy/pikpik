package registry

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// BcryptCost is the standard bcrypt computational cost.
	BcryptCost = 10
	// TokenPrefix is the fixed prefix for registry robot secret tokens.
	TokenPrefix = "pik_reg_"
)

// GenerateRandomToken generates a CSPRNG secret token formatted as pik_reg_<32_bytes_base64url>.
func GenerateRandomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to read secure random bytes: %w", err)
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(bytes), nil
}

// GenerateRobotCredential creates a new RobotCredential with raw secret and bcrypt hash.
func GenerateRobotCredential(projectID, description string) (*RobotCredential, error) {
	token, err := GenerateRandomToken()
	if err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(token), BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to generate bcrypt hash: %w", err)
	}

	var username string
	trimmedProj := strings.TrimSpace(projectID)
	if trimmedProj == "" || trimmedProj == "global" {
		username = "pikpik-ci-global"
	} else {
		username = fmt.Sprintf("pikpik-robot-%s", trimmedProj)
	}

	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	id := fmt.Sprintf("rob_%s", base64.RawURLEncoding.EncodeToString(idBytes))

	return &RobotCredential{
		ID:          id,
		ProjectID:   trimmedProj,
		Username:    username,
		SecretToken: token,
		BcryptHash:  string(hash),
		Description: description,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

// RenderHtpasswd formats a slice of robot credentials into an htpasswd file content string.
func RenderHtpasswd(creds []RobotCredential) string {
	var sb strings.Builder
	for _, c := range creds {
		if c.Username != "" && c.BcryptHash != "" {
			sb.WriteString(fmt.Sprintf("%s:%s\n", c.Username, c.BcryptHash))
		}
	}
	return sb.String()
}

// ParseHtpasswd parses an htpasswd format string into a map of username -> bcrypt_hash.
func ParseHtpasswd(content string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			user := strings.TrimSpace(parts[0])
			hash := strings.TrimSpace(parts[1])
			if user != "" && hash != "" {
				result[user] = hash
			}
		}
	}
	return result
}

// VerifyCredential verifies if the given username and secret match against the htpasswd entries.
func VerifyCredential(username, secret, htpasswdContent string) bool {
	entries := ParseHtpasswd(htpasswdContent)
	hash, ok := entries[username]
	if !ok {
		return false
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret))
	return err == nil
}
