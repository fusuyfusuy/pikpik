package orchestration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// DockerContainerManager manages standalone container lifecycle directly via Docker Engine socket.
type DockerContainerManager struct {
	cli client.CommonAPIClient
}

// NewDockerContainerManager creates a new DockerContainerManager instance.
func NewDockerContainerManager(cli client.CommonAPIClient) *DockerContainerManager {
	return &DockerContainerManager{cli: cli}
}

// Create builds and instantiates a container spec in Docker Engine without shelling.
func (m *DockerContainerManager) Create(ctx context.Context, spec ContainerSpec) (string, error) {
	// 1. Environment variables
	envList := make([]string, 0, len(spec.Environment))
	for k, v := range spec.Environment {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}

	// 2. Labels
	labels := make(map[string]string)
	for k, v := range spec.Labels {
		labels[k] = v
	}
	labels["pikpik.managed"] = "true"
	if spec.ProjectID != "" {
		labels["pikpik.project_id"] = spec.ProjectID
	}
	if spec.Name != "" {
		labels["pikpik.name"] = spec.Name
	}

	// 3. Exposed Ports & Port Bindings
	exposedPorts := make(nat.PortSet)
	portBindings := make(nat.PortMap)
	for _, p := range spec.ExposedPorts {
		proto := strings.ToLower(p.Protocol)
		if proto == "" {
			proto = "tcp"
		}
		portKey := nat.Port(fmt.Sprintf("%d/%s", p.ContainerPort, proto))
		exposedPorts[portKey] = struct{}{}

		if p.HostPort > 0 {
			portBindings[portKey] = []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: fmt.Sprintf("%d", p.HostPort),
				},
			}
		}
	}

	// 4. Health Check
	var healthConfig *container.HealthConfig
	if spec.HealthCheck != nil && len(spec.HealthCheck.Test) > 0 {
		healthConfig = &container.HealthConfig{
			Test:        spec.HealthCheck.Test,
			Interval:    spec.HealthCheck.Interval,
			Timeout:     spec.HealthCheck.Timeout,
			StartPeriod: spec.HealthCheck.StartPeriod,
			Retries:     spec.HealthCheck.Retries,
		}
	}

	// 5. Stop Timeout
	var stopTimeoutSec *int
	if spec.StopTimeout > 0 {
		sec := int(spec.StopTimeout.Seconds())
		stopTimeoutSec = &sec
	}

	// 6. Container Config
	config := &container.Config{
		Image:        spec.Image,
		Env:          envList,
		Labels:       labels,
		User:         spec.User,
		WorkingDir:   spec.WorkingDir,
		Cmd:          spec.Command,
		Entrypoint:   spec.Entrypoint,
		ExposedPorts: exposedPorts,
		Healthcheck:  healthConfig,
		StopTimeout:  stopTimeoutSec,
	}

	// 7. Mounts
	mounts := make([]mount.Mount, 0, len(spec.Mounts))
	for _, m := range spec.Mounts {
		mountType := mount.TypeVolume
		if m.Type == "bind" {
			if err := ValidateMountSource(m.Source); err != nil {
				return "", fmt.Errorf("container %q invalid mount source: %w", spec.Name, err)
			}
			mountType = mount.TypeBind
		} else if m.Type == "tmpfs" {
			mountType = mount.TypeTmpfs
		}
		mounts = append(mounts, mount.Mount{
			Type:     mountType,
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}

	// 8. Restart Policy
	restartPolicy := container.RestartPolicy{
		Name: container.RestartPolicyMode(spec.RestartPolicy),
	}
	if spec.RestartPolicy == "" {
		restartPolicy.Name = container.RestartPolicyUnlessStopped
	}

	// 9. Host Config
	hostConfig := &container.HostConfig{
		Mounts:       mounts,
		PortBindings: portBindings,
		Resources: container.Resources{
			NanoCPUs: spec.Resources.CPULimit,
			Memory:   spec.Resources.MemoryLimit,
		},
		RestartPolicy: restartPolicy,
	}

	// 10. Networking Config
	var netConfig *network.NetworkingConfig
	if len(spec.Networks) > 0 {
		netConfig = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				spec.Networks[0]: {},
			},
		}
	}

	resp, err := m.cli.ContainerCreate(ctx, config, hostConfig, netConfig, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	// Attach additional networks if specified
	if len(spec.Networks) > 1 {
		for _, netName := range spec.Networks[1:] {
			if err := m.cli.NetworkConnect(ctx, netName, resp.ID, nil); err != nil {
				// Attempt cleanup on network connection failure
				_ = m.cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
				return "", fmt.Errorf("failed to connect network %s: %w", netName, err)
			}
		}
	}

	return resp.ID, nil
}

// Start starts an existing container.
func (m *DockerContainerManager) Start(ctx context.Context, containerID string) error {
	if containerID == "" {
		return ErrContainerNotFound
	}
	if err := m.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container %s: %w", containerID, err)
	}
	return nil
}

// Stop stops a running container with a specified grace period.
func (m *DockerContainerManager) Stop(ctx context.Context, containerID string, timeout time.Duration) error {
	if containerID == "" {
		return ErrContainerNotFound
	}
	sec := int(timeout.Seconds())
	if timeout == 0 {
		sec = 10
	}
	if err := m.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &sec}); err != nil {
		return fmt.Errorf("failed to stop container %s: %w", containerID, err)
	}
	return nil
}

// Restart restarts a container with a specified grace period.
func (m *DockerContainerManager) Restart(ctx context.Context, containerID string, timeout time.Duration) error {
	if containerID == "" {
		return ErrContainerNotFound
	}
	sec := int(timeout.Seconds())
	if timeout == 0 {
		sec = 10
	}
	if err := m.cli.ContainerRestart(ctx, containerID, container.StopOptions{Timeout: &sec}); err != nil {
		return fmt.Errorf("failed to restart container %s: %w", containerID, err)
	}
	return nil
}

// Remove deletes a container and optionally its anonymous volumes.
func (m *DockerContainerManager) Remove(ctx context.Context, containerID string, force bool, removeVolumes bool) error {
	if containerID == "" {
		return ErrContainerNotFound
	}
	if err := m.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: force, RemoveVolumes: removeVolumes}); err != nil {
		return fmt.Errorf("failed to remove container %s: %w", containerID, err)
	}
	return nil
}

// Inspect retrieves low-level container metadata mapped into domain status.
func (m *DockerContainerManager) Inspect(ctx context.Context, containerID string) (*ContainerStatus, error) {
	if containerID == "" {
		return nil, ErrContainerNotFound
	}
	raw, err := m.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container %s: %w", containerID, err)
	}

	health := "none"
	if raw.State != nil && raw.State.Health != nil {
		health = raw.State.Health.Status
	}

	state := "unknown"
	statusStr := ""
	var createdAt time.Time
	if raw.State != nil {
		state = raw.State.Status
		statusStr = raw.State.Status
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw.Created); err == nil {
		createdAt = parsed
	}

	ipAddr := ""
	if raw.NetworkSettings != nil {
		ipAddr = raw.NetworkSettings.IPAddress
		if ipAddr == "" && len(raw.NetworkSettings.Networks) > 0 {
			for _, ep := range raw.NetworkSettings.Networks {
				if ep.IPAddress != "" {
					ipAddr = ep.IPAddress
					break
				}
			}
		}
	}

	var ports []PortMappingSpec
	if raw.NetworkSettings != nil && raw.NetworkSettings.Ports != nil {
		for portKey, bindings := range raw.NetworkSettings.Ports {
			var hostPort uint32
			if len(bindings) > 0 {
				fmt.Sscanf(bindings[0].HostPort, "%d", &hostPort)
			}
			ports = append(ports, PortMappingSpec{
				ContainerPort: uint32(portKey.Int()),
				HostPort:      hostPort,
				Protocol:      portKey.Proto(),
			})
		}
	}

	var labels map[string]string
	var image string
	if raw.Config != nil {
		labels = raw.Config.Labels
		image = raw.Config.Image
	}

	return &ContainerStatus{
		ID:        raw.ID,
		Name:      strings.TrimPrefix(raw.Name, "/"),
		Image:     image,
		State:     state,
		Status:    statusStr,
		Health:    health,
		CreatedAt: createdAt,
		IPAddress: ipAddr,
		Ports:     ports,
		Labels:    labels,
	}, nil
}

// List returns all containers matching filter criteria.
func (m *DockerContainerManager) List(ctx context.Context, opts ListOptions) ([]ContainerStatus, error) {
	f := filters.NewArgs()
	if opts.ProjectID != "" {
		f.Add("label", fmt.Sprintf("pikpik.project_id=%s", opts.ProjectID))
	}
	for k, v := range opts.Labels {
		f.Add("label", fmt.Sprintf("%s=%s", k, v))
	}

	containers, err := m.cli.ContainerList(ctx, container.ListOptions{
		All:     opts.All,
		Filters: f,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	result := make([]ContainerStatus, 0, len(containers))
	for _, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		var ports []PortMappingSpec
		for _, p := range c.Ports {
			ports = append(ports, PortMappingSpec{
				ContainerPort: uint32(p.PrivatePort),
				HostPort:      uint32(p.PublicPort),
				Protocol:      p.Type,
			})
		}

		ipAddr := ""
		if c.NetworkSettings != nil && len(c.NetworkSettings.Networks) > 0 {
			for _, ep := range c.NetworkSettings.Networks {
				if ep.IPAddress != "" {
					ipAddr = ep.IPAddress
					break
				}
			}
		}

		result = append(result, ContainerStatus{
			ID:        c.ID,
			Name:      name,
			Image:     c.Image,
			State:     c.State,
			Status:    c.Status,
			CreatedAt: time.Unix(c.Created, 0),
			IPAddress: ipAddr,
			Ports:     ports,
			Labels:    c.Labels,
		})
	}

	return result, nil
}

// DeployWithRollingUpdate executes a start-before-stop rolling update for standalone containers.
func (m *DockerContainerManager) DeployWithRollingUpdate(ctx context.Context, spec ContainerSpec, updateCfg RollingUpdateConfig) (*RollingUpdateResult, error) {
	startTime := time.Now()

	// 1. Locate any active existing container matching the target name/service
	var oldContainerID string
	existingList, err := m.List(ctx, ListOptions{
		ProjectID: spec.ProjectID,
		All:       true,
	})
	if err == nil {
		for _, c := range existingList {
			if (spec.Name != "" && c.Name == spec.Name) ||
				(c.Labels != nil && spec.Name != "" && c.Labels["pikpik.name"] == spec.Name) {
				oldContainerID = c.ID
				break
			}
		}
	}

	// 2. Generate unique ephemeral name for the new container
	baseName := spec.Name
	if baseName == "" {
		baseName = fmt.Sprintf("app_%s", spec.ProjectID)
	}
	ephemeralName := fmt.Sprintf("%s_v_%d", baseName, time.Now().UnixNano())

	newSpec := spec
	newSpec.Name = ephemeralName
	if newSpec.Labels == nil {
		newSpec.Labels = make(map[string]string)
	}
	newSpec.Labels["pikpik.name"] = baseName
	newSpec.Labels["pikpik.ephemeral_version"] = fmt.Sprintf("%d", time.Now().Unix())

	// 3. Create the new container
	newContainerID, err := m.Create(ctx, newSpec)
	if err != nil {
		return nil, fmt.Errorf("rolling update create failed: %w", err)
	}

	// 4. Start the new container
	if err := m.Start(ctx, newContainerID); err != nil {
		_ = m.Remove(ctx, newContainerID, true, true)
		return nil, fmt.Errorf("rolling update start failed: %w", err)
	}

	// 5. Health Probation window
	monitorDuration := updateCfg.Monitor
	if monitorDuration == 0 {
		monitorDuration = 10 * time.Second
	}

	probationCtx, cancel := context.WithTimeout(ctx, monitorDuration+5*time.Second)
	defer cancel()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	healthy := false
	probationDeadline := time.Now().Add(monitorDuration)

	for {
		select {
		case <-probationCtx.Done():
			// Timeout reached without confirmation of health
			_ = m.Remove(ctx, newContainerID, true, true)
			return nil, ErrContainerHealthTimeout

		case <-ticker.C:
			status, err := m.Inspect(probationCtx, newContainerID)
			if err != nil {
				_ = m.Remove(ctx, newContainerID, true, true)
				return nil, fmt.Errorf("rolling update inspect probe failed: %w", err)
			}

			if status.State == "exited" || status.State == "dead" {
				_ = m.Remove(ctx, newContainerID, true, true)
				return nil, fmt.Errorf("%w: container exited prematurely with state '%s'", ErrContainerHealthTimeout, status.State)
			}

			if spec.HealthCheck != nil && len(spec.HealthCheck.Test) > 0 {
				if status.Health == "healthy" {
					healthy = true
					break
				}
				if status.Health == "unhealthy" {
					_ = m.Remove(ctx, newContainerID, true, true)
					return nil, fmt.Errorf("%w: container healthcheck failed", ErrContainerHealthTimeout)
				}
			} else {
				// If no health check defined, verify running state
				if status.State == "running" {
					healthy = true
					break
				}
			}

			if time.Now().After(probationDeadline) {
				if status.State == "running" {
					healthy = true
					break
				}
				_ = m.Remove(ctx, newContainerID, true, true)
				return nil, ErrContainerHealthTimeout
			}
		}

		if healthy {
			break
		}
	}

	swappedAt := time.Now()

	// 6. Graceful Drain & Teardown of Old Container
	if oldContainerID != "" && oldContainerID != newContainerID {
		stopGrace := spec.StopTimeout
		if stopGrace == 0 {
			stopGrace = 30 * time.Second
		}
		_ = m.Stop(ctx, oldContainerID, stopGrace)
		_ = m.Remove(ctx, oldContainerID, true, false)
	}

	return &RollingUpdateResult{
		OldContainerID: oldContainerID,
		NewContainerID: newContainerID,
		SwappedAt:      swappedAt,
		Duration:       time.Since(startTime),
		Healthy:        true,
	}, nil
}
