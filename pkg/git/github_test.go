package git_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/git"
)

func generateTestRSAKey(t *testing.T) ([]byte, *rsa.PublicKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate rsa key: %v", err)
	}

	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	pemBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	}
	pemBytes := pem.EncodeToMemory(pemBlock)
	return pemBytes, &privateKey.PublicKey
}

func TestGenerateAppJWT(t *testing.T) {
	pemBytes, pubKey := generateTestRSAKey(t)
	appID := int64(123456)

	token, err := git.GenerateAppJWT(appID, pemBytes)
	if err != nil {
		t.Fatalf("failed to generate app jwt: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts in jwt, got %d", len(parts))
	}

	// 1. Verify header
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("failed to decode jwt header: %v", err)
	}
	var header map[string]any
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("failed to unmarshal jwt header: %v", err)
	}
	if header["alg"] != "RS256" {
		t.Errorf("expected alg RS256, got %v", header["alg"])
	}

	// 2. Verify payload
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("failed to decode jwt payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("failed to unmarshal jwt payload: %v", err)
	}
	if iss, ok := payload["iss"].(float64); !ok || int64(iss) != appID {
		t.Errorf("expected iss %d, got %v", appID, payload["iss"])
	}

	// 3. Verify RS256 signature
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("failed to decode jwt sig: %v", err)
	}

	signingInput := parts[0] + "." + parts[1]
	hash := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], sig); err != nil {
		t.Fatalf("rsa signature verification failed: %v", err)
	}
}

func TestGitHubClient_GetInstallationToken(t *testing.T) {
	pemBytes, _ := generateTestRSAKey(t)
	appID := int64(987654)
	installationID := int64(456789)

	serverHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHits++
		expectedPath := "/app/installations/456789/access_tokens"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected method POST, got %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			t.Errorf("expected Bearer authorization header, got %q", authHeader)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		resp := git.InstallationTokenResponse{
			Token:     "ghs_mocked_installation_token_abc123",
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := git.GitHubAppConfig{
		AppID:      appID,
		PrivateKey: pemBytes,
		BaseURL:    server.URL,
	}

	client, err := git.NewGitHubClient(cfg)
	if err != nil {
		t.Fatalf("failed to create github client: %v", err)
	}

	ctx := context.Background()
	token, expiresAt, err := client.GetInstallationToken(ctx, installationID)
	if err != nil {
		t.Fatalf("failed to get installation token: %v", err)
	}

	if token != "ghs_mocked_installation_token_abc123" {
		t.Errorf("unexpected token: %s", token)
	}
	if expiresAt.Before(time.Now()) {
		t.Errorf("unexpected expiresAt: %v", expiresAt)
	}

	// Test caching: subsequent call within validity window should not hit server again
	cachedToken, _, err := client.GetInstallationToken(ctx, installationID)
	if err != nil {
		t.Fatalf("failed second token fetch: %v", err)
	}
	if cachedToken != token {
		t.Errorf("expected cached token %s, got %s", token, cachedToken)
	}
	if serverHits != 1 {
		t.Errorf("expected 1 server hit due to caching, got %d", serverHits)
	}
}

func TestSetCommitStatus(t *testing.T) {
	var capturedRequest struct {
		Path        string
		Method      string
		AuthHeader  string
		State       string
		TargetURL   string
		Description string
		Context     string
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest.Path = r.URL.Path
		capturedRequest.Method = r.Method
		capturedRequest.AuthHeader = r.Header.Get("Authorization")

		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		capturedRequest.State = body["state"]
		capturedRequest.TargetURL = body["target_url"]
		capturedRequest.Description = body["description"]
		capturedRequest.Context = body["context"]

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"state":"success"}`))
	}))
	defer server.Close()

	ctx := context.Background()
	err := git.SetCommitStatusWithBaseURL(
		ctx,
		server.URL,
		"mock-github-token-123",
		"fusuycorp",
		"pikpik",
		"abcdef1234567890",
		"success",
		"Pikpik deployment finished successfully",
		"https://pikpik.fusuy.io/deploy/123",
	)
	if err != nil {
		t.Fatalf("failed to set commit status: %v", err)
	}

	expectedPath := "/repos/fusuycorp/pikpik/statuses/abcdef1234567890"
	if capturedRequest.Path != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, capturedRequest.Path)
	}
	if capturedRequest.Method != http.MethodPost {
		t.Errorf("expected POST method, got %s", capturedRequest.Method)
	}
	if capturedRequest.AuthHeader != "Bearer mock-github-token-123" {
		t.Errorf("expected Bearer token auth header, got %s", capturedRequest.AuthHeader)
	}
	if capturedRequest.State != "success" {
		t.Errorf("expected state success, got %s", capturedRequest.State)
	}
	if capturedRequest.Description != "Pikpik deployment finished successfully" {
		t.Errorf("expected description, got %s", capturedRequest.Description)
	}
	if capturedRequest.TargetURL != "https://pikpik.fusuy.io/deploy/123" {
		t.Errorf("expected target url, got %s", capturedRequest.TargetURL)
	}
	if capturedRequest.Context != "pikpik/deploy" {
		t.Errorf("expected context pikpik/deploy, got %s", capturedRequest.Context)
	}
}
