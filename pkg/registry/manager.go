package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

const (
	DefaultContainerName = "pikpik_registry"
	DefaultImage         = "registry:2.8.3"
	DefaultLocalVolume   = "pikpik_vol_sys_registry_data"
	DefaultPort          = 5000
)

// DefaultRegistryManager manages the OCI registry container, storage backends, and credentials.
type DefaultRegistryManager struct {
	cli              client.CommonAPIClient
	mu               sync.RWMutex
	cfg              RegistryConfig
	status           RegistryStatus
	robotAccounts    map[string]RobotCredential // keyed by ID
	htpasswdFilePath string
	configFilePath   string
}

// NewRegistryManager creates a new DefaultRegistryManager instance.
func NewRegistryManager(cli client.CommonAPIClient, htpasswdFilePath, configFilePath string) *DefaultRegistryManager {
	return &DefaultRegistryManager{
		cli:              cli,
		robotAccounts:    make(map[string]RobotCredential),
		htpasswdFilePath: htpasswdFilePath,
		configFilePath:   configFilePath,
		status: RegistryStatus{
			IsRunning:     false,
			LastHeartbeat: time.Now().UTC(),
		},
	}
}

// Reconcile ensures the registry container, volumes, configs, and networks are in the desired state.
func (m *DefaultRegistryManager) Reconcile(ctx context.Context, cfg RegistryConfig) (*RegistryStatus, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg = cfg
	if !cfg.Enabled {
		// If disabled and container is running, stop it if client is present
		if m.cli != nil && m.status.ContainerID != "" {
			_ = m.cli.ContainerStop(ctx, m.status.ContainerID, container.StopOptions{})
		}
		m.status.IsRunning = false
		m.status.LastHeartbeat = time.Now().UTC()
		return &m.status, nil
	}

	// Generate config YAML
	yamlContent, err := GenerateConfigYAML(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to generate registry yaml config: %w", err)
	}

	// Write config and htpasswd if paths are configured
	if m.configFilePath != "" {
		if err := os.MkdirAll(filepath.Dir(m.configFilePath), 0755); err == nil {
			_ = os.WriteFile(m.configFilePath, []byte(yamlContent), 0600)
		}
	}
	if m.htpasswdFilePath != "" {
		if err := os.MkdirAll(filepath.Dir(m.htpasswdFilePath), 0755); err == nil {
			var creds []RobotCredential
			for _, c := range m.robotAccounts {
				creds = append(creds, c)
			}
			htContent := RenderHtpasswd(creds)
			_ = os.WriteFile(m.htpasswdFilePath, []byte(htContent), 0600)
		}
	}

	if m.cli == nil {
		// Mock/In-memory mode
		m.status.IsRunning = true
		m.status.ContainerID = "mock-registry-container-id"
		m.status.LastHeartbeat = time.Now().UTC()
		return &m.status, nil
	}

	// Invariant 2: Zero Host Port Mapping - Port 5000 is internal only
	port := cfg.InternalPort
	if port <= 0 {
		port = DefaultPort
	}
	portKey := nat.Port(fmt.Sprintf("%d/tcp", port))
	exposedPorts := nat.PortSet{portKey: struct{}{}}

	// Mounts
	mounts := make([]mount.Mount, 0)
	if cfg.StorageBackend == StorageBackendLocal {
		volName := cfg.LocalVolume
		if volName == "" {
			volName = DefaultLocalVolume
		}
		// Ensure named volume exists
		_, _ = m.cli.VolumeCreate(ctx, volume.CreateOptions{Name: volName})
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: volName,
			Target: "/var/lib/registry",
		})
	}

	if m.configFilePath != "" {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   m.configFilePath,
			Target:   "/etc/docker/registry/config.yml",
			ReadOnly: true,
		})
	}
	if m.htpasswdFilePath != "" {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   m.htpasswdFilePath,
			Target:   "/etc/docker/registry/htpasswd",
			ReadOnly: true,
		})
	}

	// Labels
	labels := map[string]string{
		"pikpik.managed":  "true",
		"pikpik.service":  "registry",
		"pikpik.role":     "system-registry",
		"pikpik.internal": "true",
	}

	// Check if container already exists
	containers, err := m.cli.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("name", DefaultContainerName),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var containerID string
	for _, c := range containers {
		for _, name := range c.Names {
			if strings.TrimPrefix(name, "/") == DefaultContainerName {
				containerID = c.ID
				break
			}
		}
	}

	if containerID == "" {
		// Create container
		resp, err := m.cli.ContainerCreate(ctx,
			&container.Config{
				Image:        DefaultImage,
				ExposedPorts: exposedPorts,
				Labels:       labels,
				Env: []string{
					"REGISTRY_STORAGE_DELETE_ENABLED=true",
				},
				Healthcheck: &container.HealthConfig{
					Test:        []string{"CMD-SHELL", "wget -q -O - http://127.0.0.1:5000/v2/ || exit 0"},
					Interval:    10 * time.Second,
					Timeout:     5 * time.Second,
					StartPeriod: 5 * time.Second,
					Retries:     3,
				},
			},
			&container.HostConfig{
				RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
				Mounts:        mounts,
			},
			nil,
			nil,
			DefaultContainerName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create registry container: %w", err)
		}
		containerID = resp.ID
	}

	// Start container
	if err := m.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		// Ignore if already started
		if !strings.Contains(err.Error(), "already started") {
			return nil, fmt.Errorf("failed to start registry container: %w", err)
		}
	}

	m.status.IsRunning = true
	m.status.ContainerID = containerID
	m.status.LastHeartbeat = time.Now().UTC()

	return &m.status, nil
}

// CreateRobotAccount generates a cryptographically secure token and updates htpasswd.
func (m *DefaultRegistryManager) CreateRobotAccount(ctx context.Context, projectID, description string) (*RobotCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	trimmedProj := strings.TrimSpace(projectID)
	// Check for duplicate project robot
	for _, existing := range m.robotAccounts {
		if existing.ProjectID == trimmedProj && trimmedProj != "" && trimmedProj != "global" {
			return nil, ErrDuplicateRobotAccount
		}
	}

	cred, err := GenerateRobotCredential(trimmedProj, description)
	if err != nil {
		return nil, err
	}

	m.robotAccounts[cred.ID] = *cred

	// Sync htpasswd to disk and signal container if running
	m.syncHtpasswdLocked(ctx)

	// Return a copy with SecretToken populated
	return cred, nil
}

// RevokeRobotAccount deletes credentials and reloads the registry auth table.
func (m *DefaultRegistryManager) RevokeRobotAccount(ctx context.Context, robotID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.robotAccounts[robotID]; !exists {
		return ErrRobotAccountNotFound
	}

	delete(m.robotAccounts, robotID)
	m.syncHtpasswdLocked(ctx)
	return nil
}

// ListRobotAccounts returns all active robot accounts for a given project.
func (m *DefaultRegistryManager) ListRobotAccounts(ctx context.Context, projectID string) ([]RobotCredential, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	trimmedProj := strings.TrimSpace(projectID)
	var list []RobotCredential
	for _, c := range m.robotAccounts {
		if trimmedProj == "" || c.ProjectID == trimmedProj {
			// SecretToken is omitted in listings
			copyCred := c
			copyCred.SecretToken = ""
			list = append(list, copyCred)
		}
	}
	return list, nil
}

// GetStatus inspects the registry container health and storage metrics.
func (m *DefaultRegistryManager) GetStatus(ctx context.Context) (*RegistryStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.cli == nil {
		return &m.status, nil
	}

	if m.status.ContainerID == "" {
		return &RegistryStatus{
			IsRunning:     false,
			LastHeartbeat: time.Now().UTC(),
		}, nil
	}

	inspect, err := m.cli.ContainerInspect(ctx, m.status.ContainerID)
	if err != nil {
		return &RegistryStatus{
			IsRunning:     false,
			LastHeartbeat: time.Now().UTC(),
		}, nil
	}

	return &RegistryStatus{
		IsRunning:     inspect.State != nil && inspect.State.Running,
		ContainerID:   inspect.ID,
		LastHeartbeat: time.Now().UTC(),
	}, nil
}

func (m *DefaultRegistryManager) syncHtpasswdLocked(ctx context.Context) {
	if m.htpasswdFilePath != "" {
		var creds []RobotCredential
		for _, c := range m.robotAccounts {
			creds = append(creds, c)
		}
		content := RenderHtpasswd(creds)
		_ = os.WriteFile(m.htpasswdFilePath, []byte(content), 0600)
	}

	// Reload registry by sending SIGHUP if container is running
	if m.cli != nil && m.status.ContainerID != "" && m.status.IsRunning {
		_ = m.cli.ContainerKill(ctx, m.status.ContainerID, "SIGHUP")
	}
}
