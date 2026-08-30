package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/store"
)

type contextKey string

const (
	contextKeyRequestID contextKey = "pikpik_request_id"
	contextKeyUser      contextKey = "pikpik_user"
	contextKeyToken     contextKey = "pikpik_token"
	contextKeyRole      contextKey = "pikpik_role"
)

// Role hierarchy levels
const (
	RoleViewer    = "viewer"
	RoleDeveloper = "developer"
	RoleAdmin     = "admin"
	RoleOwner     = "owner"
)

var roleLevels = map[string]int{
	RoleViewer:    1,
	RoleDeveloper: 2,
	RoleAdmin:     3,
	RoleOwner:     4,
}

// GenerateRequestID creates a ULID/hex-prefixed request ID.
func GenerateRequestID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

// GetRequestID extracts the request ID from context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(contextKeyRequestID).(string); ok && id != "" {
		return id
	}
	return GenerateRequestID()
}

// GetUserFromContext extracts authenticated user DTO.
func GetUserFromContext(ctx context.Context) (*UserDTO, bool) {
	u, ok := ctx.Value(contextKeyUser).(*UserDTO)
	return u, ok
}

// GetRoleFromContext extracts authenticated role.
func GetRoleFromContext(ctx context.Context) string {
	if r, ok := ctx.Value(contextKeyRole).(string); ok && r != "" {
		return r
	}
	return ""
}

// ExtractBearerOrCookie extracts the authentication secret from headers, cookie, or query parameters.
func ExtractToken(r *http.Request) string {
	// 1. Authorization header
	authHeader := r.Header.Get("Authorization")
	if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}

	// 2. Sec-WebSocket-Protocol header (pikpik-auth.<token>)
	wsProto := r.Header.Get("Sec-WebSocket-Protocol")
	if wsProto != "" {
		for _, part := range strings.Split(wsProto, ",") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "pikpik-auth.") {
				return strings.TrimPrefix(part, "pikpik-auth.")
			}
		}
	}

	// 3. Cookie session
	if cookie, err := r.Cookie("pikpik_session"); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// 4. Query param fallback
	if tok := r.URL.Query().Get("token"); tok != "" {
		return tok
	}

	return ""
}

// ExtractClientIP returns the caller's IP address.
func ExtractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
			return strings.TrimSpace(parts[0])
		}
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return strings.TrimSpace(xrip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// IsRoleAllowed checks if userRole satisfies the minimum required role.
func IsRoleAllowed(userRole, requiredRole string) bool {
	if requiredRole == "" || requiredRole == "public" {
		return true
	}
	userLvl := roleLevels[strings.ToLower(userRole)]
	reqLvl := roleLevels[strings.ToLower(requiredRole)]
	if userLvl == 0 {
		return false
	}
	return userLvl >= reqLvl
}

// AuthMiddleware creates an HTTP middleware verifying authentication and enforcing RBAC.
func AuthMiddleware(authSvc auth.AuthService, st store.Store, requiredRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := r.Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = GenerateRequestID()
			}
			w.Header().Set("X-Request-ID", reqID)
			ctx := context.WithValue(r.Context(), contextKeyRequestID, reqID)

			if requiredRole == "public" || requiredRole == "" {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			rawSecret := ExtractToken(r)
			if rawSecret == "" {
				WriteError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "Missing authentication token or session", nil, reqID)
				return
			}

			// Validate token / session
			var userRole string
			var userDTO *UserDTO

			// Try API token validation
			if strings.HasPrefix(rawSecret, auth.DefaultTokenPrefix) {
				if authSvc != nil {
					apiTok, err := authSvc.ValidateAPIToken(ctx, rawSecret, "")
					if err != nil {
						WriteError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "Invalid or expired API token", nil, reqID)
						return
					}
					// Fetch user to determine role
					if st != nil {
						if u, err := st.Users().GetByID(ctx, apiTok.UserID); err == nil && u != nil {
							userRole = u.Role
							userDTO = &UserDTO{
								ID:        u.ID,
								Email:     u.Email,
								Role:      u.Role,
								CreatedAt: u.CreatedAt,
							}
						}
					}
					if userRole == "" {
						userRole = RoleAdmin // API tokens default to admin permissions if user query omitted
					}
				} else {
					userRole = RoleAdmin
				}
			} else {
				// Session cookie or raw token validation
				if st != nil {
					sess, err := st.Sessions().GetByID(ctx, rawSecret)
					if err == nil && sess != nil {
						if time.Now().UTC().Before(sess.ExpiresAt) {
							if u, err := st.Users().GetByID(ctx, sess.UserID); err == nil && u != nil {
								userRole = u.Role
								userDTO = &UserDTO{
									ID:        u.ID,
									Email:     u.Email,
									Role:      u.Role,
									CreatedAt: u.CreatedAt,
								}
							}
						}
					}
				}
			}

			if userRole == "" && authSvc == nil && st == nil {
				userRole = RoleAdmin
				userDTO = &UserDTO{
					ID:    "mock_user",
					Email: "admin@pikpik.local",
					Role:  RoleAdmin,
				}
			}

			if userRole == "" {
				WriteError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "Invalid or expired session/token", nil, reqID)
				return
			}

			if !IsRoleAllowed(userRole, requiredRole) {
				WriteError(w, http.StatusForbidden, ErrCodeForbidden, "Insufficient role permissions for operation", map[string]any{
					"required_role": requiredRole,
					"active_role":   userRole,
				}, reqID)
				return
			}

			ctx = context.WithValue(ctx, contextKeyRole, userRole)
			if userDTO != nil {
				ctx = context.WithValue(ctx, contextKeyUser, userDTO)
			}
			ctx = context.WithValue(ctx, contextKeyToken, rawSecret)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
