package orchestration_test

import (
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/orchestration"
)

// TestBuildSwarmUpdateConfig verifies conversion of domain RollingUpdateConfig to Swarm's UpdateConfig.
func TestBuildSwarmUpdateConfig(t *testing.T) {
	// 1. Defaults verification
	defaultCfg := orchestration.RollingUpdateConfig{}
	swarmCfg := orchestration.BuildSwarmUpdateConfig(defaultCfg)

	if swarmCfg.Order != "start-first" {
		t.Errorf("expected default order 'start-first', got '%s'", swarmCfg.Order)
	}
	if swarmCfg.Parallelism != 1 {
		t.Errorf("expected default parallelism 1, got %d", swarmCfg.Parallelism)
	}
	if swarmCfg.Delay != 5*time.Second {
		t.Errorf("expected default delay 5s, got %v", swarmCfg.Delay)
	}
	if swarmCfg.Monitor != 10*time.Second {
		t.Errorf("expected default monitor 10s, got %v", swarmCfg.Monitor)
	}
	if swarmCfg.FailureAction != "rollback" {
		t.Errorf("expected default failure action 'rollback', got '%s'", swarmCfg.FailureAction)
	}

	// 2. Custom values verification
	customCfg := orchestration.RollingUpdateConfig{
		Order:           "stop-first",
		Parallelism:     4,
		Delay:           15 * time.Second,
		Monitor:         30 * time.Second,
		FailureAction:   "pause",
		MaxFailureRatio: 0.25,
	}
	swarmCustomCfg := orchestration.BuildSwarmUpdateConfig(customCfg)

	if swarmCustomCfg.Order != "stop-first" {
		t.Errorf("expected order 'stop-first', got '%s'", swarmCustomCfg.Order)
	}
	if swarmCustomCfg.Parallelism != 4 {
		t.Errorf("expected parallelism 4, got %d", swarmCustomCfg.Parallelism)
	}
	if swarmCustomCfg.Delay != 15*time.Second {
		t.Errorf("expected delay 15s, got %v", swarmCustomCfg.Delay)
	}
	if swarmCustomCfg.Monitor != 30*time.Second {
		t.Errorf("expected monitor 30s, got %v", swarmCustomCfg.Monitor)
	}
	if swarmCustomCfg.FailureAction != "pause" {
		t.Errorf("expected failure action 'pause', got '%s'", swarmCustomCfg.FailureAction)
	}
	if swarmCustomCfg.MaxFailureRatio != 0.25 {
		t.Errorf("expected max failure ratio 0.25, got %v", swarmCustomCfg.MaxFailureRatio)
	}
}
