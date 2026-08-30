package crypto_test

import (
	"testing"

	"github.com/fusuycorp/pikpik/pkg/crypto"
)

func TestArgon2id_HashAndVerify(t *testing.T) {
	hasher := crypto.FastArgon2Hasher()
	password := "CorrectHorseBatteryStaple#2026"

	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}

	// 1. Verify exact match
	valid, err := hasher.Verify(password, encoded)
	if err != nil || !valid {
		t.Errorf("Expected password to verify successfully, valid=%v, err=%v", valid, err)
	}

	// 2. Verify wrong password failure
	valid, err = hasher.Verify("WrongPassword!99", encoded)
	if valid {
		t.Errorf("Expected wrong password to fail verification")
	}

	// 3. Verify format integrity
	if len(encoded) == 0 || encoded[:10] != "$argon2id$" {
		t.Errorf("Invalid PHC format prefix: %s", encoded)
	}
}

func TestArgon2id_InvalidFormats(t *testing.T) {
	hasher := crypto.FastArgon2Hasher()

	testCases := []string{
		"",
		"invalid-string",
		"$bcrypt$v=19$m=65536,t=3,p=2$salt$hash",
		"$argon2id$v=99$m=65536,t=3,p=2$salt$hash",
		"$argon2id$v=19$badparams$salt$hash",
		"$argon2id$v=19$m=65536,t=3,p=2$!invalid-b64!$hash",
		"$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$!invalid-hash!",
	}

	for _, tc := range testCases {
		valid, err := hasher.Verify("password", tc)
		if valid || err == nil {
			t.Errorf("Expected error for invalid format %q, got valid=%v, err=%v", tc, valid, err)
		}
	}
}
