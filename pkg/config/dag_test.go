package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/config"
)

func TestDAGResolver_Success(t *testing.T) {
	resolver := config.NewDAGResolver()

	input := map[string]string{
		"DB_HOST":      "postgres.internal",
		"DB_PORT":      "5432",
		"DB_USER":      "app_user",
		"DB_PASS":      "secret123",
		"DB_NAME":      "billing",
		"DATABASE_URL": "postgres://${DB_USER}:${DB_PASS}@${DB_HOST}:${DB_PORT}/${DB_NAME}",
		"API_PORT":     "8080",
		"FULL_URL":     "http://localhost:${API_PORT}/health",
		"UNBRACED":     "http://localhost:$API_PORT/info",
		"LITERAL_ESC":  "Price is $$100",
	}

	resolved, err := resolver.ResolveDAG(input)
	if err != nil {
		t.Fatalf("Expected successful resolution, got: %v", err)
	}

	expectedDBURL := "postgres://app_user:secret123@postgres.internal:5432/billing"
	if resolved["DATABASE_URL"] != expectedDBURL {
		t.Errorf("DATABASE_URL mismatch: got %q, want %q", resolved["DATABASE_URL"], expectedDBURL)
	}

	if resolved["FULL_URL"] != "http://localhost:8080/health" {
		t.Errorf("FULL_URL mismatch: got %q", resolved["FULL_URL"])
	}

	if resolved["UNBRACED"] != "http://localhost:8080/info" {
		t.Errorf("UNBRACED mismatch: got %q", resolved["UNBRACED"])
	}

	if resolved["LITERAL_ESC"] != "Price is $100" {
		t.Errorf("Literal escape mismatch: got %q, want 'Price is $100'", resolved["LITERAL_ESC"])
	}
}

func TestDAGResolver_CycleDetection(t *testing.T) {
	resolver := config.NewDAGResolver()

	input := map[string]string{
		"VAR_A": "prefix_${VAR_B}",
		"VAR_B": "mid_${VAR_C}",
		"VAR_C": "end_${VAR_A}", // Creates A -> B -> C -> A cycle
	}

	_, err := resolver.ResolveDAG(input)
	if err == nil {
		t.Fatalf("Expected cyclic error, got nil")
	}

	if !errors.Is(err, config.ErrCyclicDependency) {
		t.Errorf("Expected ErrCyclicDependency, got %v", err)
	}

	if !strings.Contains(err.Error(), "VAR_A") || !strings.Contains(err.Error(), "->") {
		t.Errorf("Expected cycle path in error message, got: %v", err)
	}
}

func TestDAGResolver_MissingVariable(t *testing.T) {
	resolver := config.NewDAGResolver()

	input := map[string]string{
		"HOST": "localhost",
		"URL":  "http://${HOST}:${PORT}/api",
	}

	_, err := resolver.ResolveDAG(input)
	if err == nil {
		t.Fatalf("Expected missing variable error, got nil")
	}

	if !errors.Is(err, config.ErrUnresolvedVariable) {
		t.Errorf("Expected ErrUnresolvedVariable, got %v", err)
	}
}
