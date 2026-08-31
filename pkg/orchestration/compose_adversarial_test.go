package orchestration_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/orchestration"
)

func TestAdversarial_Compose_CyclicDependencyTopologicalSort(t *testing.T) {
	// CMP-01: Direct Self-Dependency (A -> A)
	selfDepServices := map[string]orchestration.ComposeServiceDef{
		"app": {
			Image:     "alpine",
			DependsOn: []string{"app"},
		},
	}
	_, err := orchestration.ResolveDeploymentOrder(selfDepServices)
	if err == nil {
		t.Fatal("expected ErrCyclicDependency for self-dependency, got nil")
	}
	if !errors.Is(err, orchestration.ErrCyclicDependency) {
		t.Fatalf("expected error wrapping ErrCyclicDependency, got %v", err)
	}

	// CMP-01: 3-Node Cycle (A -> B -> C -> A)
	cycle3Services := map[string]orchestration.ComposeServiceDef{
		"web": {
			Image:     "nginx",
			DependsOn: []string{"api"},
		},
		"api": {
			Image:     "node-api",
			DependsOn: []string{"db"},
		},
		"db": {
			Image:     "postgres",
			DependsOn: []string{"web"}, // Cycle back to web
		},
	}

	start := time.Now()
	_, err = orchestration.ResolveDeploymentOrder(cycle3Services)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected ErrCyclicDependency for 3-node cycle, got nil")
	}
	if !errors.Is(err, orchestration.ErrCyclicDependency) {
		t.Fatalf("expected error wrapping ErrCyclicDependency, got %v", err)
	}
	if elapsed > 10*time.Millisecond {
		t.Fatalf("ResolveDeploymentOrder cycle detection exceeded 10ms threshold: %v", elapsed)
	}

	// CMP-01: Disconnected subgraph with cycle (A -> B (ok), C -> D -> C (cycle))
	mixedServices := map[string]orchestration.ComposeServiceDef{
		"redis": {Image: "redis"},
		"worker": {
			Image:     "worker",
			DependsOn: []string{"redis"},
		},
		"bad1": {
			Image:     "bad",
			DependsOn: []string{"bad2"},
		},
		"bad2": {
			Image:     "bad",
			DependsOn: []string{"bad1"},
		},
	}
	_, err = orchestration.ResolveDeploymentOrder(mixedServices)
	if err == nil || !errors.Is(err, orchestration.ErrCyclicDependency) {
		t.Fatalf("expected ErrCyclicDependency on mixed graph with cycle, got %v", err)
	}
}

func TestAdversarial_Compose_UnknownDependencyTopologicalSort(t *testing.T) {
	// CMP-02: Unknown dependency
	services := map[string]orchestration.ComposeServiceDef{
		"web": {
			Image:     "nginx",
			DependsOn: []string{"ghost-service-xyz"},
		},
	}
	_, err := orchestration.ResolveDeploymentOrder(services)
	if err == nil {
		t.Fatal("expected ErrUnknownDependency, got nil")
	}
	if !errors.Is(err, orchestration.ErrUnknownDependency) {
		t.Fatalf("expected error wrapping ErrUnknownDependency, got %v", err)
	}
	if !strings.Contains(err.Error(), "ghost-service-xyz") {
		t.Fatalf("expected error to name missing service ghost-service-xyz, got %v", err)
	}
}

func TestAdversarial_Compose_YAMLBombsAndEntityExpansion(t *testing.T) {
	// CMP-03: YAML Billion Laughs Anchor Bomb
	yamlBomb := `
version: "3.8"
services:
  a: &a ["lol","lol","lol","lol","lol","lol","lol","lol","lol"]
  b: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a]
  c: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b]
  d: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c]
  e: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d]
  f: &f [*e,*e,*e,*e,*e,*e,*e,*e,*e]
  app:
    image: alpine
    environment:
      BOMB: *f
`
	start := time.Now()
	// Must not crash with OOM / stack overflow
	_, _ = orchestration.InspectComposeYAML(yamlBomb)
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Fatalf("YAML anchor bomb processing took excessive time: %v", elapsed)
	}
}

func TestAdversarial_Compose_HostilePortsAndResourceReservations(t *testing.T) {
	// CMP-04 & CMP-05: Hostile Ports & Resources
	hostileYAML := `
version: "3.8"
services:
  malicious_svc:
    image: alpine:latest
    ports:
      - "99999:80"
      - "-80:80"
      - "0:0"
      - "abc:def"
      - "80:80/sctp"
      - "127.0.0.1:80:80:80"
    deploy:
      resources:
        reservations:
          cpus: "-4"
          memory: "-20GB"
`
	res, err := orchestration.InspectComposeYAML(hostileYAML)
	if err != nil {
		// Acceptable to return parse error
		return
	}

	if res == nil {
		t.Fatal("expected non-nil result")
	}

	// Ensure ports map handled gracefully without panicking or creating invalid uint32 ports > 65535
	for _, p := range res.ExposedPorts {
		if p > 65535 {
			t.Fatalf("invalid port > 65535 extracted: %d", p)
		}
	}
}

func TestAdversarial_Compose_NullByteAndInjectionInServiceNames(t *testing.T) {
	// CMP-06: Shell injection / null bytes in service names
	injectionYAML := `
version: "3.8"
services:
  "svc; rm -rf /":
    image: alpine
  "svc\x00hidden":
    image: alpine
  "$(whoami)":
    image: alpine
`
	res, err := orchestration.InspectComposeYAML(injectionYAML)
	if err != nil {
		// Acceptable if YAML unmarshaler or parser rejects invalid chars
		return
	}

	// Ensure services parsed without panic
	if len(res.Services) == 0 {
		t.Fatal("expected services to be parsed")
	}
}
