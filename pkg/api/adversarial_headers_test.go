package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/store"
)

type mockUserStore struct {
	store.UserStore
	users map[string]*store.User
}

func (m *mockUserStore) GetByID(ctx context.Context, id string) (*store.User, error) {
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return nil, store.ErrNotFound
}

type mockSessionStore struct {
	store.SessionStore
	sessions map[string]*store.Session
}

func (m *mockSessionStore) GetByID(ctx context.Context, id string) (*store.Session, error) {
	if s, ok := m.sessions[id]; ok {
		return s, nil
	}
	return nil, store.ErrNotFound
}

type mockStore struct {
	store.Store
	users    *mockUserStore
	sessions *mockSessionStore
}

func (m *mockStore) Users() store.UserStore {
	return m.users
}

func (m *mockStore) Sessions() store.SessionStore {
	return m.sessions
}

func TestAdversarial_API_CRLFInjectionAndHeaderSplitting(t *testing.T) {
	// HDR-01: CRLF Injection in header values
	crlfHeaders := []string{
		"admin=true\r\nSet-Cookie: pwned=true\r\n",
		"req_12345\r\nInjected-Header: evil",
		"bearer token\r\n\r\nGET /evil HTTP/1.1",
	}

	handler := api.AuthMiddleware(nil, nil, "public")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	for _, hdr := range crlfHeaders {
		req := httptest.NewRequest("GET", "/api/v1/health", nil)
		req.Header.Set("X-Request-ID", hdr)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		// Ensure response has valid sanitized headers and does not split response headers
		cookies := rec.Result().Cookies()
		for _, c := range cookies {
			if c.Name == "pwned" {
				t.Fatalf("HDR-01: CRLF injection succeeded in setting unauthorized cookie: %v", c)
			}
		}
	}
}

func TestAdversarial_API_NullBytesInAuthTokens(t *testing.T) {
	// HDR-02: Null bytes in token / bearer header
	st := &mockStore{
		users: &mockUserStore{
			users: map[string]*store.User{
				"usr_1": {ID: "usr_1", Email: "admin@pikpik.io", Role: api.RoleAdmin},
			},
		},
		sessions: &mockSessionStore{
			sessions: map[string]*store.Session{
				"valid_sess": {ID: "valid_sess", UserID: "usr_1", ExpiresAt: time.Now().Add(1 * time.Hour)},
			},
		},
	}

	middleware := api.AuthMiddleware(nil, st, api.RoleAdmin)
	targetHandler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("admin access granted"))
	}))

	nullByteTokens := []string{
		"valid_sess\x00extra",
		"valid_sess\x00admin",
		"\x00valid_sess",
		"pik_tok_\x00deadbeef",
		"Bearer valid_sess\x00",
	}

	for _, tok := range nullByteTokens {
		req := httptest.NewRequest("GET", "/api/v1/admin/resource", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()

		targetHandler.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			t.Fatalf("HDR-02: Null byte token %q bypassed authentication with 200 OK!", tok)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for token %q, got %d", tok, rec.Code)
		}
	}
}

func TestAdversarial_API_OversizedHeadersAndBufferLimits(t *testing.T) {
	// HDR-03: Massive 100KB header
	st := &mockStore{
		users:    &mockUserStore{users: map[string]*store.User{}},
		sessions: &mockSessionStore{sessions: map[string]*store.Session{}},
	}

	middleware := api.AuthMiddleware(nil, st, api.RoleAdmin)
	targetHandler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	oversizedPayload := strings.Repeat("A", 100*1024)
	req := httptest.NewRequest("GET", "/api/v1/services", nil)
	req.Header.Set("Authorization", "Bearer "+oversizedPayload)
	rec := httptest.NewRecorder()

	targetHandler.ServeHTTP(rec, req)

	// Must fail closed (401 Unauthorized) without panic
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized on oversized auth token, got %d", rec.Code)
	}
}

func TestAdversarial_API_SpoofedXForwardedForBypass(t *testing.T) {
	// HDR-04: Spoofed X-Forwarded-For headers
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	req.RemoteAddr = "203.0.113.195:44321"
	req.Header.Set("X-Forwarded-For", "127.0.0.1, 10.0.0.1")
	req.Header.Set("X-Real-IP", "127.0.0.1")

	clientIP := api.ExtractClientIP(req)
	// As per pikpik design invariant, ExtractClientIP ignores X-Forwarded-For to prevent rate-limit spoofing
	if clientIP != "203.0.113.195" {
		t.Fatalf("HDR-04: Expected ExtractClientIP to use direct socket IP 203.0.113.195, got %s", clientIP)
	}
}
