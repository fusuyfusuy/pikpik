package config_test

import (
	"testing"

	"github.com/fusuycorp/pikpik/pkg/config"
)

func TestSecretMasker(t *testing.T) {
	secrets := []string{
		"supersecretpassword",
		"admin123",
		"abc", // <= 3 chars, should not be masked
	}

	masker := config.NewSecretMasker(secrets)

	rawLog := "Connecting to postgres://user:supersecretpassword@localhost/db with admin123 and abc token"
	masked := masker.Mask(rawLog)

	expected := "Connecting to postgres://user:[REDACTED]@localhost/db with [REDACTED] and abc token"
	if masked != expected {
		t.Errorf("Mask mismatch:\ngot:  %q\nwant: %q", masked, expected)
	}

	// Empty input
	if masker.Mask("") != "" {
		t.Errorf("Expected empty string for empty input")
	}

	// Nil / empty secrets
	emptyMasker := config.NewSecretMasker(nil)
	if emptyMasker.Mask(rawLog) != rawLog {
		t.Errorf("Expected unchanged string when no secrets present")
	}
}
