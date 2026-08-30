package crypto_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/crypto"
)

func TestAESVault_EncryptDecryptRoundtrip(t *testing.T) {
	ctx := context.Background()
	masterSecret := "super-secure-production-master-key-32-chars!"
	vault, err := crypto.NewAESVault(masterSecret)
	if err != nil {
		t.Fatalf("Failed to init vault: %v", err)
	}

	secretPayload := "postgres://user:pass@10.0.0.5:5432/production_db"

	envelope, err := vault.EncryptString(ctx, secretPayload)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	if !strings.HasPrefix(envelope, "v1:") {
		t.Fatalf("Expected envelope to start with 'v1:', got: %s", envelope)
	}

	// Decrypt
	decrypted, err := vault.DecryptString(ctx, envelope)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	if decrypted != secretPayload {
		t.Fatalf("Decrypted mismatch: got %q, want %q", decrypted, secretPayload)
	}

	// Verify tampering detection (GCM auth tag check)
	tampered := envelope[:len(envelope)-4] + "AAAA"
	_, err = vault.DecryptString(ctx, tampered)
	if err == nil {
		t.Fatalf("Expected decryption error on tampered ciphertext, got nil")
	}
}

func TestAESVault_ShortMasterKey(t *testing.T) {
	_, err := crypto.NewAESVault("short-key")
	if !errors.Is(err, crypto.ErrInvalidMasterKey) {
		t.Errorf("Expected ErrInvalidMasterKey for short key, got: %v", err)
	}
}

func TestAESVault_InvalidEnvelopes(t *testing.T) {
	ctx := context.Background()
	vault, err := crypto.NewAESVault("super-secure-master-key-32-bytes!!")
	if err != nil {
		t.Fatalf("Failed to init vault: %v", err)
	}

	invalidCases := []string{
		"",
		"v2:aaa:bbb:ccc",
		"v1:aaa:bbb",
		"v1:!invalid-b64!:bbb:ccc",
		"v1:aaa:!invalid-b64!:ccc",
		"v1:aaa:bbb:!invalid-b64!",
	}

	for _, tc := range invalidCases {
		_, err := vault.Decrypt(ctx, tc)
		if err == nil {
			t.Errorf("Expected decryption error for envelope %q, got nil", tc)
		}
	}
}
