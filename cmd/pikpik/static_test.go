package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticHandler(t *testing.T) {
	handler := NewStaticHandler()

	t.Run("Serve Root Index HTML", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		contentType := rec.Header().Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			t.Errorf("expected text/html content-type, got %s", contentType)
		}

		cacheControl := rec.Header().Get("Cache-Control")
		if !strings.Contains(cacheControl, "no-cache") {
			t.Errorf("expected no-cache for index.html, got %s", cacheControl)
		}
	})

	t.Run("Serve SPA Fallback for Deep Links", func(t *testing.T) {
		routes := []string{"/apps", "/nodes", "/stacks", "/databases", "/ingress", "/settings"}
		for _, route := range routes {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200 for %s, got %d", route, rec.Code)
			}

			contentType := rec.Header().Get("Content-Type")
			if !strings.Contains(contentType, "text/html") {
				t.Errorf("expected text/html for %s, got %s", route, contentType)
			}
		}
	})

	t.Run("Reject Non-GET/HEAD Methods", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405 for POST, got %d", rec.Code)
		}
	})

	t.Run("Return 404 JSON for Unknown API/WS/Healthz Routes", func(t *testing.T) {
		unknownRoutes := []string{"/api/v1/nonexistent", "/ws/unknown", "/healthz/extra"}
		for _, route := range unknownRoutes {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected status 404 for %s, got %d", route, rec.Code)
			}
			if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				t.Errorf("expected application/json content-type for %s, got %s", route, rec.Header().Get("Content-Type"))
			}
		}
	})
}
