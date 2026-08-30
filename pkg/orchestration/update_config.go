package orchestration

import (
	"time"

	"github.com/docker/docker/api/types/swarm"
)

// BuildSwarmUpdateConfig transforms a domain RollingUpdateConfig into Docker Swarm's native UpdateConfig.
func BuildSwarmUpdateConfig(cfg RollingUpdateConfig) *swarm.UpdateConfig {
	order := "start-first"
	if cfg.Order == "stop-first" {
		order = "stop-first"
	}

	parallelism := cfg.Parallelism
	if parallelism == 0 {
		parallelism = 1
	}

	delay := cfg.Delay
	if delay == 0 {
		delay = 5 * time.Second
	}

	monitor := cfg.Monitor
	if monitor == 0 {
		monitor = 10 * time.Second
	}

	failureAction := "rollback"
	if cfg.FailureAction == "pause" || cfg.FailureAction == "continue" {
		failureAction = cfg.FailureAction
	}

	return &swarm.UpdateConfig{
		Parallelism:     parallelism,
		Delay:           delay,
		FailureAction:   failureAction,
		Monitor:         monitor,
		MaxFailureRatio: cfg.MaxFailureRatio,
		Order:           order,
	}
}
