package deploy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	// imageRegex verifies standard OCI image references: [host[:port]/][namespace/]repository[:tag]
	imageRegex = regexp.MustCompile(`^([a-zA-Z0-9.-]+(?::[0-9]+)?\/)?([a-z0-9_.-]+(?:\/[a-z0-9_.-]+)*)(:[a-zA-Z0-9_.-]+)?$`)
)

// DefaultDeployWebhookHandler implements DeployWebhookHandler.
type DefaultDeployWebhookHandler struct {
	mu          sync.RWMutex
	options     HandlerOptions
	limiter     *TokenBucketLimiter
	tokens      map[string]*NudgeTokenInfo // keyed by hex-encoded sha256 token hash
	tokenMap    map[string]string          // tokenID -> tokenHash
	dispatcher  DeploymentDispatcher
}

// NewDeployWebhookHandler creates a new DeployWebhookHandler instance.
func NewDeployWebhookHandler(opts HandlerOptions) *DefaultDeployWebhookHandler {
	if opts.RateLimitPerMin <= 0 {
		opts.RateLimitPerMin = 10
	}
	if opts.BurstLimit <= 0 {
		opts.BurstLimit = 3
	}
	if opts.IPRateLimit <= 0 {
		opts.IPRateLimit = 30
	}
	if opts.IPBurstLimit <= 0 {
		opts.IPBurstLimit = 10
	}

	return &DefaultDeployWebhookHandler{
		options:    opts,
		limiter:    NewTokenBucketLimiter(),
		tokens:     make(map[string]*NudgeTokenInfo),
		tokenMap:   make(map[string]string),
	}
}

// SetDispatcher configures the deployment dispatcher callback.
func (h *DefaultDeployWebhookHandler) SetDispatcher(dispatcher DeploymentDispatcher) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dispatcher = dispatcher
}

// RegisterTokenForTest manually injects a token hash for testing.
func (h *DefaultDeployWebhookHandler) RegisterTokenForTest(tokenHash string, info *NudgeTokenInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()
	info.TokenHash = tokenHash
	h.tokens[tokenHash] = info
	if info.ID != "" {
		h.tokenMap[info.ID] = tokenHash
	}
}

// GenerateToken creates a new webhook token for a target service.
func (h *DefaultDeployWebhookHandler) GenerateToken(ctx context.Context, serviceID, projectID string) (string, *NudgeTokenInfo, error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate secure random bytes: %w", err)
	}

	rawToken := "pik_ndg_" + hex.EncodeToString(rawBytes)
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return "", nil, err
	}
	id := "tok_" + hex.EncodeToString(idBytes)

	info := &NudgeTokenInfo{
		ID:                id,
		ProjectID:         projectID,
		ServiceID:         serviceID,
		TokenHash:         tokenHash,
		AllowedRegistries: h.options.AllowedHosts,
		RateLimitPerMin:   h.options.RateLimitPerMin,
		IsActive:          true,
		CreatedAt:         time.Now().UTC(),
	}

	h.mu.Lock()
	h.tokens[tokenHash] = info
	h.tokenMap[id] = tokenHash
	h.mu.Unlock()

	return rawToken, info, nil
}

// RevokeToken invalidates an existing nudge webhook token.
func (h *DefaultDeployWebhookHandler) RevokeToken(ctx context.Context, tokenID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	tokenHash, exists := h.tokenMap[tokenID]
	if !exists {
		return ErrInvalidNudgeToken
	}

	delete(h.tokens, tokenHash)
	delete(h.tokenMap, tokenID)
	return nil
}

// ValidatePayload verifies payload JSON structure, size, and registry allowlist.
func (h *DefaultDeployWebhookHandler) ValidatePayload(payload *DeployNudgePayload, allowedRegistries []string) error {
	if payload == nil || strings.TrimSpace(payload.Image) == "" {
		return fmt.Errorf("%w: image field is required", ErrInvalidImageReference)
	}

	image := strings.TrimSpace(payload.Image)
	if !imageRegex.MatchString(image) {
		return fmt.Errorf("%w: invalid image reference %q", ErrInvalidImageReference, image)
	}

	// If allowed registries are specified, enforce allowlist check
	if len(allowedRegistries) > 0 {
		matched := false
		for _, allowed := range allowedRegistries {
			allowed = strings.TrimSpace(allowed)
			if allowed == "" {
				continue
			}
			if registryAllowed(image, allowed) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%w: image %q does not match allowed registries %v", ErrUnauthorizedRegistry, image, allowedRegistries)
		}
	}

	return nil
}

// registryAllowed reports whether image is covered by the allowed registry
// entry, matching only on path-segment boundaries (an exact match, or a
// prefix match immediately followed by "/") rather than a naive string
// prefix. This prevents typosquat bypasses such as "ghcr.io/fusuycorp"
// matching "ghcr.io/fusuycorpevil/backdoor". A trailing "/*" or "/" on the
// allowlist entry is treated the same as the bare entry.
func registryAllowed(image, allowed string) bool {
	allowed = strings.TrimSuffix(allowed, "/*")
	allowed = strings.TrimSuffix(allowed, "/")
	if allowed == "" {
		return false
	}
	return image == allowed || strings.HasPrefix(image, allowed+"/")
}

// ServeHTTP implements the http.Handler interface for POST /api/deploy/nudge/{token}.
func (h *DefaultDeployWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
		return
	}

	// 1. Extract token from URL path
	// Handles /api/deploy/nudge/{token} or /deploy/nudge/{token} or /{token}
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	var rawToken string
	for i, part := range parts {
		if part == "nudge" && i+1 < len(parts) {
			rawToken = parts[i+1]
			break
		}
	}
	if rawToken == "" && len(parts) > 0 {
		rawToken = parts[len(parts)-1]
	}

	if rawToken == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"UNAUTHORIZED_NUDGE_TOKEN"}`))
		return
	}

	// 2. Client IP extraction & Rate limiting
	clientIP := extractClientIP(r)
	ipAllowed, ipRetry := h.limiter.Allow("ip:"+clientIP, h.options.IPRateLimit, h.options.IPBurstLimit)
	if !ipAllowed {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(ipRetry.Seconds())+1))
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"RATE_LIMIT_EXCEEDED","retryAfter":%d}`, int(ipRetry.Seconds())+1)))
		return
	}

	// 3. Token lookup & Constant-time validation
	incomingHash := sha256.Sum256([]byte(rawToken))
	incomingHashHex := hex.EncodeToString(incomingHash[:])

	h.mu.RLock()
	var matchedInfo *NudgeTokenInfo
	for hashHex, info := range h.tokens {
		if subtle.ConstantTimeCompare([]byte(hashHex), []byte(incomingHashHex)) == 1 {
			matchedInfo = info
			break
		}
	}
	h.mu.RUnlock()

	if matchedInfo == nil || !matchedInfo.IsActive {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"UNAUTHORIZED_NUDGE_TOKEN"}`))
		return
	}

	// 4. Token-level rate limiting
	limitPerMin := matchedInfo.RateLimitPerMin
	if limitPerMin <= 0 {
		limitPerMin = h.options.RateLimitPerMin
	}
	burst := h.options.BurstLimit
	tokAllowed, tokRetry := h.limiter.Allow("tok:"+incomingHashHex, limitPerMin, burst)
	if !tokAllowed {
		w.Header().Set("Content-Type", "application/json")
		retrySecs := int(tokRetry.Seconds()) + 1
		if retrySecs < 1 {
			retrySecs = 60
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retrySecs))
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"RATE_LIMIT_EXCEEDED","retryAfter":%d}`, retrySecs)))
		return
	}

	// 5. Body size enforcement: Max 64KB
	maxBodyReader := http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(maxBodyReader)
	var payload DeployNudgePayload
	if err := decoder.Decode(&payload); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = w.Write([]byte(`{"error":"PAYLOAD_TOO_LARGE","message":"webhook payload exceeds 64KB ceiling"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"INVALID_PAYLOAD","message":%q}`, err.Error())))
		return
	}

	// 6. Payload validation
	allowedRegs := matchedInfo.AllowedRegistries
	if len(allowedRegs) == 0 {
		allowedRegs = h.options.AllowedHosts
	}
	if err := h.ValidatePayload(&payload, allowedRegs); err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, ErrUnauthorizedRegistry) {
			w.WriteHeader(http.StatusForbidden)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"VALIDATION_FAILED","message":%q}`, err.Error())))
		return
	}

	// 7. Dispatch deployment
	var deploymentID string
	h.mu.RLock()
	disp := h.dispatcher
	h.mu.RUnlock()

	if disp != nil {
		var err error
		deploymentID, err = disp.DispatchDeploy(r.Context(), matchedInfo.ServiceID, payload)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"DISPATCH_FAILED","message":%q}`, err.Error())))
			return
		}
	} else {
		depBytes := make([]byte, 6)
		_, _ = rand.Read(depBytes)
		deploymentID = "dep_" + hex.EncodeToString(depBytes)
	}

	resp := NudgeResponse{
		DeploymentID: deploymentID,
		ServiceID:    matchedInfo.ServiceID,
		Status:       "QUEUED",
		QueuedAt:     time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

// extractClientIP returns the actual TCP peer address to key the rate
// limiter on. Proxy headers (X-Forwarded-For/X-Real-IP) are deliberately
// NOT trusted: this daemon has no reverse-proxy trust configuration, and
// honoring caller-supplied headers on this unauthenticated endpoint lets any
// client mint an unlimited number of distinct rate-limit buckets (spoofing
// a new header value per request), defeating rate limiting and exhausting
// the limiter's memory.
func extractClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
