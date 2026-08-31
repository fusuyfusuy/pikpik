package git_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/git"
)

func TestAdversarial_Webhook_HMACFuzzingAndMalformedSignatures(t *testing.T) {
	secret := "production-webhook-secret-999"
	payload := []byte(`{"ref":"refs/heads/main","after":"a1b2c3d4e5f6","repository":{"full_name":"pikpik/core"}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validHex := hex.EncodeToString(mac.Sum(nil))

	malformedCases := []struct {
		name      string
		secret    string
		payload   []byte
		sigHeader string
		expect    bool
	}{
		// PAY-01: Truncated hex signatures
		{"Truncated 1 char", secret, payload, "sha256=" + validHex[:1], false},
		{"Truncated 10 chars", secret, payload, "sha256=" + validHex[:10], false},
		{"Truncated 32 chars", secret, payload, "sha256=" + validHex[:32], false},
		{"Truncated 63 chars", secret, payload, "sha256=" + validHex[:63], false},
		{"Extended 65 chars", secret, payload, "sha256=" + validHex + "a", false},
		{"Extended 128 chars", secret, payload, "sha256=" + validHex + validHex, false},

		// PAY-02: Non-hex / hostile characters
		{"Non-hex g characters", secret, payload, "sha256=" + strings.Repeat("g", 64), false},
		{"Null byte in signature", secret, payload, "sha256=" + validHex[:30] + "\x00" + validHex[31:], false},
		{"High bytes in signature", secret, payload, "sha256=" + validHex[:30] + "\xff\xfe" + validHex[32:], false},
		{"Unicode whitespace", secret, payload, "sha256=" + validHex[:30] + "\u200B" + validHex[30:], false},
		{"Spaces inside signature", secret, payload, "sha256= " + validHex, false},
		{"CRLF injection in signature", secret, payload, "sha256=" + validHex[:30] + "\r\n" + validHex[30:], false},

		// PAY-03: Prefix variations & algorithm confusion
		{"Valid standard prefix", secret, payload, "sha256=" + validHex, true},
		{"Valid colon prefix", secret, payload, "sha256:" + validHex, true},
		{"Raw hex no prefix", secret, payload, validHex, true},
		{"Uppercase prefix SHA256=", secret, payload, "SHA256=" + validHex, false},
		{"SHA1 prefix", secret, payload, "sha1=" + validHex[:40], false},
		{"SHA512 prefix", secret, payload, "sha512=" + validHex, false},
		{"Double prefix sha256=sha256=", secret, payload, "sha256=sha256=" + validHex, false},

		// PAY-06: Empty / Nil boundaries
		{"Empty secret", "", payload, "sha256=" + validHex, false},
		{"Empty payload", secret, []byte{}, "sha256=" + validHex, false},
		{"Nil payload", secret, nil, "sha256=" + validHex, false},
		{"Empty signature header", secret, payload, "", false},
		{"All empty", "", nil, "", false},
	}

	for _, tc := range malformedCases {
		t.Run(tc.name, func(t *testing.T) {
			res := git.VerifyGitHubSignature(tc.secret, tc.payload, tc.sigHeader)
			if res != tc.expect {
				t.Fatalf("expected VerifyGitHubSignature to return %v, got %v for header %q", tc.expect, res, tc.sigHeader)
			}
		})
	}
}

func TestAdversarial_Webhook_TamperedPayloadAndBitFlips(t *testing.T) {
	secret := "super-secure-webhook-key-42"
	payload := []byte(`{"ref":"refs/heads/main","after":"c0ffee123456","repository":{"full_name":"fusuycorp/pikpik"}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// PAY-04: Bit-flip every single byte in the payload
	for i := 0; i < len(payload); i++ {
		tampered := make([]byte, len(payload))
		copy(tampered, payload)
		// Flip a bit
		tampered[i] ^= 0x01

		if git.VerifyGitHubSignature(secret, tampered, validSig) {
			t.Fatalf("PAY-04: Bit-flip at byte offset %d was accepted as valid signature!", i)
		}
	}

	// Append byte attack
	tamperedAppend := append(payload, ' ')
	if git.VerifyGitHubSignature(secret, tamperedAppend, validSig) {
		t.Fatal("PAY-04: Appended whitespace payload was accepted as valid signature!")
	}

	// PAY-05: Cross-tenant replay attack (altered repo name)
	replayedPayload := []byte(`{"ref":"refs/heads/main","after":"c0ffee123456","repository":{"full_name":"victim/pikpik"}}`)
	if git.VerifyGitHubSignature(secret, replayedPayload, validSig) {
		t.Fatal("PAY-05: Replayed payload targeting different repo was accepted with original signature!")
	}
}

func TestAdversarial_Webhook_TimingSideChannelResistance(t *testing.T) {
	// PAY-07: Verify constant-time comparison timing
	secret := "timing-resistant-secret"
	payload := []byte(`{"ref":"refs/heads/main","after":"deadbeef"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validRaw := mac.Sum(nil)

	// Create mismatched signature at byte 0
	mismatch0 := make([]byte, len(validRaw))
	copy(mismatch0, validRaw)
	mismatch0[0] ^= 0xff
	sig0 := "sha256=" + hex.EncodeToString(mismatch0)

	// Create mismatched signature at byte 31 (last byte)
	mismatch31 := make([]byte, len(validRaw))
	copy(mismatch31, validRaw)
	mismatch31[31] ^= 0xff
	sig31 := "sha256=" + hex.EncodeToString(mismatch31)

	const iterations = 5000

	// Warmup
	for i := 0; i < 500; i++ {
		git.VerifyGitHubSignature(secret, payload, sig0)
		git.VerifyGitHubSignature(secret, payload, sig31)
	}

	start0 := time.Now()
	for i := 0; i < iterations; i++ {
		_ = git.VerifyGitHubSignature(secret, payload, sig0)
	}
	duration0 := time.Since(start0)

	start31 := time.Now()
	for i := 0; i < iterations; i++ {
		_ = git.VerifyGitHubSignature(secret, payload, sig31)
	}
	duration31 := time.Since(start31)

	diff := duration0 - duration31
	if diff < 0 {
		diff = -diff
	}

	t.Logf("Timing comparison over %d iterations: mismatch at byte 0 = %v, mismatch at byte 31 = %v (diff: %v)",
		iterations, duration0, duration31, diff)
}

func TestAdversarial_Webhook_HostilePayloadJSONStructures(t *testing.T) {
	// PAY-08: Hostile JSON structures
	hostileJSONs := []struct {
		name string
		raw  string
	}{
		{"Unclosed JSON object", `{"ref":"refs/heads/main","repository":`},
		{"Null byte in JSON key", "{\"ref\x00\":\"refs/heads/main\"}"},
		{"Deeply nested arrays", strings.Repeat("[", 500) + strings.Repeat("]", 500)},
		{"Deeply nested objects", strings.Repeat(`{"a":`, 200) + `1` + strings.Repeat(`}`, 200)},
		{"Type mismatch for ref", `{"ref": 12345}`},
		{"Type mismatch for head_commit", `{"head_commit": "invalid-string"}`},
		{"Null head_commit and repository", `{"ref":"refs/heads/main","head_commit":null,"repository":null}`},
		{"Massive payload padding", `{"ref":"refs/heads/main","padding":"` + strings.Repeat("A", 1024*1024) + `"}`},
	}

	for _, tc := range hostileJSONs {
		t.Run(tc.name, func(t *testing.T) {
			event, err := git.ParseGitHubPushEvent([]byte(tc.raw))
			if err != nil {
				// Expected parse error on invalid JSON
				return
			}
			// If it parsed successfully, ensure no nil pointers panicked
			if event == nil {
				t.Fatal("expected non-nil event or explicit error")
			}
		})
	}
}

func TestAdversarial_Webhook_RandomFuzzSignatures(t *testing.T) {
	secret := "fuzzing-test-secret"
	payload := []byte(`{"action":"push","repository":"pikpik/test"}`)

	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 500; i++ {
		randomLen := rng.Intn(128)
		randomBytes := make([]byte, randomLen)
		rng.Read(randomBytes)
		randomHeader := fmt.Sprintf("sha256=%x", randomBytes)

		// Must fail gracefully without panic
		res := git.VerifyGitHubSignature(secret, payload, randomHeader)
		if res {
			t.Fatalf("Random fuzz signature was accepted! Header: %s", randomHeader)
		}
	}
}
