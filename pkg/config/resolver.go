package config

import (
	"context"
	"errors"
)

var (
	// ErrCyclicDependency is returned when a circular variable reference is detected.
	ErrCyclicDependency = errors.New("config: circular variable reference detected in DAG")

	// ErrUnresolvedVariable is returned when a referenced variable does not exist.
	ErrUnresolvedVariable = errors.New("config: referenced variable does not exist")
)

// ResolvedEnv represents the computed key-value pairs ready for container injection.
type ResolvedEnv struct {
	Variables map[string]string // Key -> Decrypted Final Value
	Secrets   map[string]bool   // Key -> True if masked secret
}

// ConfigManager resolves the 4-tier cascading hierarchy with DAG interpolation and secret decryption.
type ConfigManager interface {
	// ResolveHierarchy fetches Org, Project, Stage, and Service variables,
	// applies cascading overrides, decrypts secrets, and expands references.
	ResolveHierarchy(
		ctx context.Context,
		orgID, projectID, stageID, serviceID string,
	) (*ResolvedEnv, error)

	// ExpandVariables performs DAG dependency sorting and cycle detection on raw map.
	ExpandVariables(raw map[string]string) (map[string]string, error)

	// BuildMasker returns a SecretMasker initialized with all secret values in this resolution.
	BuildMasker(resolved *ResolvedEnv) SecretMasker
}
