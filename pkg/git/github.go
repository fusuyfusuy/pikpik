package git

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	DefaultGitHubBaseURL = "https://api.github.com"
	GitHubAPIVersion     = "2022-11-28"
)

var (
	// ErrInvalidPrivateKey is returned when the RSA private key cannot be parsed.
	ErrInvalidPrivateKey = errors.New("git: invalid RSA private key PEM")

	// ErrGitHubAPI is returned when GitHub API returns an error response.
	ErrGitHubAPI = errors.New("git: github api error")
)

// GitHubClient interacts with the GitHub App API and commit status endpoints.
type GitHubClient struct {
	cfg        GitHubAppConfig
	httpClient *http.Client
	baseURL    string
	rsaKey     *rsa.PrivateKey

	tokenMu sync.Mutex
	cached  map[int64]*cachedToken
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

// NewGitHubClient creates a new GitHub App client from configuration.
func NewGitHubClient(cfg GitHubAppConfig) (*GitHubClient, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		if envURL := os.Getenv("GITHUB_API_URL"); envURL != "" {
			baseURL = envURL
		} else {
			baseURL = DefaultGitHubBaseURL
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")

	client := &GitHubClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		baseURL:    baseURL,
		cached:     make(map[int64]*cachedToken),
	}

	if len(cfg.PrivateKey) > 0 {
		key, err := parseRSAPrivateKey(cfg.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPrivateKey, err)
		}
		client.rsaKey = key
	}

	return client, nil
}

func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("PKCS#8 key is not RSA")
	}

	return nil, errors.New("unsupported RSA private key format (expected PKCS#1 or PKCS#8)")
}

// GenerateAppJWT creates a short-lived (max 10 min) RS256 JWT for GitHub App authentication.
func GenerateAppJWT(appID int64, privateKeyPEM []byte) (string, error) {
	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPrivateKey, err)
	}

	return generateJWTWithKey(appID, key)
}

func generateJWTWithKey(appID int64, key *rsa.PrivateKey) (string, error) {
	// 1. Header
	headerJSON := `{"alg":"RS256","typ":"JWT"}`
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(headerJSON))

	// 2. Payload
	now := time.Now().UTC()
	payloadObj := map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": appID,
	}

	payloadBytes, err := json.Marshal(payloadObj)
	if err != nil {
		return "", fmt.Errorf("git: failed to marshal JWT payload: %w", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	// 3. Signature
	signingInput := headerB64 + "." + payloadB64
	hash := sha256.Sum256([]byte(signingInput))

	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("git: failed to sign JWT with RSA key: %w", err)
	}

	sigB64 := base64.RawURLEncoding.EncodeToString(signature)
	return signingInput + "." + sigB64, nil
}

// GenerateJWT creates a new JWT using the client's configured private key.
func (c *GitHubClient) GenerateJWT() (string, error) {
	if c.rsaKey == nil {
		return "", errors.New("git: RSA private key not configured")
	}
	return generateJWTWithKey(c.cfg.AppID, c.rsaKey)
}

// InstallationTokenResponse represents the response from the access tokens endpoint.
type InstallationTokenResponse struct {
	Token        string            `json:"token"`
	ExpiresAt    time.Time         `json:"expires_at"`
	Permissions  map[string]string `json:"permissions,omitempty"`
	RepoSelect   string            `json:"repository_selection,omitempty"`
}

// GetInstallationToken exchanges a GitHub App JWT for a scoped 1-hour Installation Access Token.
func (c *GitHubClient) GetInstallationToken(ctx context.Context, installationID int64) (string, time.Time, error) {
	c.tokenMu.Lock()
	if cached, ok := c.cached[installationID]; ok {
		// If token is valid for at least 5 more minutes, reuse it
		if time.Until(cached.expiresAt) > 5*time.Minute {
			token := cached.token
			exp := cached.expiresAt
			c.tokenMu.Unlock()
			return token, exp, nil
		}
	}
	c.tokenMu.Unlock()

	jwtToken, err := c.GenerateJWT()
	if err != nil {
		return "", time.Time{}, err
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.baseURL, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("git: failed to create access token request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", GitHubAPIVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("git: failed to exchange installation token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("git: failed to read access token response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("%w: status %d: %s", ErrGitHubAPI, resp.StatusCode, string(body))
	}

	var tokenResp InstallationTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("git: failed to decode access token JSON: %w", err)
	}

	c.tokenMu.Lock()
	c.cached[installationID] = &cachedToken{
		token:     tokenResp.Token,
		expiresAt: tokenResp.ExpiresAt,
	}
	c.tokenMu.Unlock()

	return tokenResp.Token, tokenResp.ExpiresAt, nil
}

type commitStatusPayload struct {
	State       string `json:"state"`
	TargetURL   string `json:"target_url,omitempty"`
	Description string `json:"description,omitempty"`
	Context     string `json:"context,omitempty"`
}

// SetCommitStatus updates the commit status on GitHub for a given commit SHA.
// state can be "pending", "success", "error", or "failure".
func SetCommitStatus(ctx context.Context, token, owner, repo, sha, state, description, targetURL string) error {
	baseURL := DefaultGitHubBaseURL
	if envURL := os.Getenv("GITHUB_API_URL"); envURL != "" {
		baseURL = envURL
	}
	return SetCommitStatusWithBaseURL(ctx, baseURL, token, owner, repo, sha, state, description, targetURL)
}

// SetCommitStatusWithBaseURL updates the commit status using a specified base API URL.
func SetCommitStatusWithBaseURL(ctx context.Context, baseURL, token, owner, repo, sha, state, description, targetURL string) error {
	if token == "" {
		return errors.New("git: access token required to set commit status")
	}
	if owner == "" || repo == "" || sha == "" {
		return errors.New("git: owner, repo, and sha are required to set commit status")
	}
	if state == "" {
		state = "pending"
	}

	baseURL = strings.TrimRight(baseURL, "/")
	url := fmt.Sprintf("%s/repos/%s/%s/statuses/%s", baseURL, owner, repo, sha)

	payload := commitStatusPayload{
		State:       state,
		TargetURL:   targetURL,
		Description: description,
		Context:     "pikpik/deploy",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("git: failed to encode commit status payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("git: failed to create commit status request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", GitHubAPIVersion)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("git: commit status request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: status %d: %s", ErrGitHubAPI, resp.StatusCode, string(body))
	}

	return nil
}
