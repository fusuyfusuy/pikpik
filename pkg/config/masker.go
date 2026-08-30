package config

import (
	"sort"
	"strings"
)

// SecretMasker redacts sensitive secret values from logs and error strings.
type SecretMasker interface {
	Mask(input string) string
}

type multiStringMasker struct {
	secrets []string
}

// NewSecretMasker creates a new SecretMasker with the provided secret values.
// Secrets with length <= 3 characters are ignored to prevent excessive false-positive masking.
func NewSecretMasker(secrets []string) SecretMasker {
	seen := make(map[string]bool)
	var filtered []string

	for _, s := range secrets {
		// Only mask secrets longer than 3 characters
		if len(s) > 3 && !seen[s] {
			seen[s] = true
			filtered = append(filtered, s)
		}
	}

	// Sort secrets descending by length so longer substrings match first
	sort.Slice(filtered, func(i, j int) bool {
		return len(filtered[i]) > len(filtered[j])
	})

	return &multiStringMasker{secrets: filtered}
}

// Mask replaces all occurrences of registered secrets with [REDACTED].
func (m *multiStringMasker) Mask(input string) string {
	if input == "" || len(m.secrets) == 0 {
		return input
	}

	result := input
	for _, secret := range m.secrets {
		result = strings.ReplaceAll(result, secret, "[REDACTED]")
	}
	return result
}
