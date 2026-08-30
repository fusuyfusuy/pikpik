package orchestration_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/orchestration"
)

// TestResolveDeploymentOrder verifies Kahn's topological sorting algorithm on service DAGs.
func TestResolveDeploymentOrder(t *testing.T) {
	t.Run("linear dependency chain", func(t *testing.T) {
		services := map[string]orchestration.ComposeServiceDef{
			"db": {
				Name: "db",
			},
			"backend": {
				Name:      "backend",
				DependsOn: []string{"db"},
			},
			"frontend": {
				Name:      "frontend",
				DependsOn: []string{"backend"},
			},
		}

		order, err := orchestration.ResolveDeploymentOrder(services)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := []string{"db", "backend", "frontend"}
		if !reflect.DeepEqual(order, expected) {
			t.Errorf("expected order %v, got %v", expected, order)
		}
	})

	t.Run("diamond dependency graph", func(t *testing.T) {
		services := map[string]orchestration.ComposeServiceDef{
			"postgres": {
				Name: "postgres",
			},
			"auth_api": {
				Name:      "auth_api",
				DependsOn: []string{"postgres"},
			},
			"worker": {
				Name:      "worker",
				DependsOn: []string{"postgres"},
			},
			"gateway": {
				Name:      "gateway",
				DependsOn: []string{"auth_api", "worker"},
			},
		}

		order, err := orchestration.ResolveDeploymentOrder(services)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Ensure postgres comes first, gateway comes last
		if order[0] != "postgres" {
			t.Errorf("expected postgres first, got %s", order[0])
		}
		if order[len(order)-1] != "gateway" {
			t.Errorf("expected gateway last, got %s", order[len(order)-1])
		}
	})

	t.Run("cyclic dependency detection", func(t *testing.T) {
		services := map[string]orchestration.ComposeServiceDef{
			"serviceA": {
				Name:      "serviceA",
				DependsOn: []string{"serviceB"},
			},
			"serviceB": {
				Name:      "serviceB",
				DependsOn: []string{"serviceC"},
			},
			"serviceC": {
				Name:      "serviceC",
				DependsOn: []string{"serviceA"},
			},
		}

		_, err := orchestration.ResolveDeploymentOrder(services)
		if !errors.Is(err, orchestration.ErrCyclicDependency) {
			t.Fatalf("expected ErrCyclicDependency, got %v", err)
		}
	})

	t.Run("unknown dependency detection", func(t *testing.T) {
		services := map[string]orchestration.ComposeServiceDef{
			"api": {
				Name:      "api",
				DependsOn: []string{"nonexistent_db"},
			},
		}

		_, err := orchestration.ResolveDeploymentOrder(services)
		if !errors.Is(err, orchestration.ErrUnknownDependency) {
			t.Fatalf("expected ErrUnknownDependency, got %v", err)
		}
	})
}

// TestParseComposeYAML verifies Compose YAML parsing and environment interpolation.
func TestParseComposeYAML(t *testing.T) {
	rawYAML := `
services:
  web:
    image: nginx:alpine
    ports:
      - "${HTTP_PORT:-8080}:80/tcp"
    environment:
      - APP_ENV=${ENV_NAME:-production}
      - DB_HOST=postgres
    depends_on:
      - postgres
    volumes:
      - web_data:/var/www/html
    networks:
      - app_net
    restart: unless-stopped
    healthcheck:
      test: "curl -f http://localhost:80/health || exit 1"
      interval: 10s
      timeout: 5s
      retries: 3

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: ${DB_NAME:-pikpik_db}
      POSTGRES_PASSWORD: secretpassword
    volumes:
      - db_data:/var/lib/postgresql/data
    networks:
      - app_net

networks:
  app_net:

volumes:
  web_data:
  db_data:
`

	envVars := map[string]string{
		"HTTP_PORT": "9000",
		"ENV_NAME":  "staging",
	}

	spec, err := orchestration.ParseComposeYAML(rawYAML, envVars)
	if err != nil {
		t.Fatalf("failed to parse Compose YAML: %v", err)
	}

	if len(spec.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(spec.Services))
	}

	// Verify web service
	web, ok := spec.Services["web"]
	if !ok {
		t.Fatalf("service 'web' not found")
	}
	if web.Image != "nginx:alpine" {
		t.Errorf("expected image nginx:alpine, got %s", web.Image)
	}
	if len(web.Ports) != 1 || web.Ports[0].HostPort != 9000 || web.Ports[0].ContainerPort != 80 {
		t.Errorf("expected port 9000:80, got %+v", web.Ports)
	}
	if web.Environment["APP_ENV"] != "staging" {
		t.Errorf("expected APP_ENV staging, got %s", web.Environment["APP_ENV"])
	}
	if len(web.DependsOn) != 1 || web.DependsOn[0] != "postgres" {
		t.Errorf("expected depends_on [postgres], got %v", web.DependsOn)
	}
	if web.HealthCheck == nil || len(web.HealthCheck.Test) == 0 {
		t.Errorf("expected healthcheck on web service")
	}

	// Verify postgres service default interpolation
	pg, ok := spec.Services["postgres"]
	if !ok {
		t.Fatalf("service 'postgres' not found")
	}
	if pg.Environment["POSTGRES_DB"] != "pikpik_db" {
		t.Errorf("expected default DB_NAME 'pikpik_db', got %s", pg.Environment["POSTGRES_DB"])
	}

	// Verify volumes and networks
	if len(spec.Networks) != 1 || spec.Networks[0] != "app_net" {
		t.Errorf("expected networks [app_net], got %v", spec.Networks)
	}
	if len(spec.Volumes) != 2 {
		t.Errorf("expected 2 volumes, got %d", len(spec.Volumes))
	}
}
