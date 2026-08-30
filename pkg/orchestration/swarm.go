package orchestration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

// DockerSwarmManager manages multi-node Docker Swarm clusters, services, and tasks.
type DockerSwarmManager struct {
	cli client.CommonAPIClient
}

// NewDockerSwarmManager creates a new DockerSwarmManager instance.
func NewDockerSwarmManager(cli client.CommonAPIClient) *DockerSwarmManager {
	return &DockerSwarmManager{cli: cli}
}

// InitCluster initializes a new Swarm cluster with this node as the leader manager.
func (s *DockerSwarmManager) InitCluster(ctx context.Context, req SwarmInitRequest) (string, error) {
	listenAddr := req.ListenAddr
	if listenAddr == "" {
		listenAddr = "0.0.0.0:2377"
	}

	nodeID, err := s.cli.SwarmInit(ctx, swarm.InitRequest{
		ListenAddr:      listenAddr,
		AdvertiseAddr:   req.AdvertiseAddr,
		DataPathAddr:    req.DataPathAddr,
		DefaultAddrPool: req.DefaultAddrPool,
		ForceNewCluster: req.ForceNewCluster,
	})
	if err != nil {
		return "", fmt.Errorf("failed to init swarm cluster: %w", err)
	}
	return nodeID, nil
}

// JoinCluster joins an existing Swarm cluster as a manager or worker.
func (s *DockerSwarmManager) JoinCluster(ctx context.Context, req SwarmJoinRequest) error {
	listenAddr := req.ListenAddr
	if listenAddr == "" {
		listenAddr = "0.0.0.0:2377"
	}

	err := s.cli.SwarmJoin(ctx, swarm.JoinRequest{
		ListenAddr:    listenAddr,
		AdvertiseAddr: req.AdvertiseAddr,
		RemoteAddrs:   req.RemoteAddrs,
		JoinToken:     req.JoinToken,
	})
	if err != nil {
		return fmt.Errorf("failed to join swarm cluster: %w", err)
	}
	return nil
}

// LeaveCluster removes this node from the active Swarm cluster.
func (s *DockerSwarmManager) LeaveCluster(ctx context.Context, force bool) error {
	if err := s.cli.SwarmLeave(ctx, force); err != nil {
		return fmt.Errorf("failed to leave swarm cluster: %w", err)
	}
	return nil
}

// GetClusterInfo retrieves swarm cluster metadata and node statistics.
func (s *DockerSwarmManager) GetClusterInfo(ctx context.Context) (*ClusterInfo, error) {
	info, err := s.cli.SwarmInspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm cluster: %w", err)
	}

	nodes, err := s.cli.NodeList(ctx, types.NodeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm nodes: %w", err)
	}

	managers := 0
	workers := 0
	for _, n := range nodes {
		if n.Spec.Role == swarm.NodeRoleManager {
			managers++
		} else {
			workers++
		}
	}

	return &ClusterInfo{
		ID:         info.ID,
		CreatedAt:  info.CreatedAt,
		UpdatedAt:  info.UpdatedAt,
		Spec:       info.Spec,
		JoinTokens: info.JoinTokens,
		Nodes:      len(nodes),
		Managers:   managers,
		Workers:    workers,
	}, nil
}

// CreateService creates a declarative Swarm service.
func (s *DockerSwarmManager) CreateService(ctx context.Context, spec ServiceSpec) (string, error) {
	swarmSpec := s.convertSpecToSwarm(spec)

	resp, err := s.cli.ServiceCreate(ctx, swarmSpec, types.ServiceCreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create swarm service: %w", err)
	}

	return resp.ID, nil
}

// UpdateService updates an existing Swarm service with rolling update parameters.
func (s *DockerSwarmManager) UpdateService(ctx context.Context, serviceID string, version uint64, spec ServiceSpec) error {
	if serviceID == "" {
		return ErrServiceNotFound
	}
	swarmSpec := s.convertSpecToSwarm(spec)

	_, err := s.cli.ServiceUpdate(
		ctx,
		serviceID,
		swarm.Version{Index: version},
		swarmSpec,
		types.ServiceUpdateOptions{
			Rollback: "none",
		},
	)
	if err != nil {
		return fmt.Errorf("failed to update swarm service %s: %w", serviceID, err)
	}

	return nil
}

// RemoveService deletes a Swarm service from the cluster.
func (s *DockerSwarmManager) RemoveService(ctx context.Context, serviceID string) error {
	if serviceID == "" {
		return ErrServiceNotFound
	}
	if err := s.cli.ServiceRemove(ctx, serviceID); err != nil {
		return fmt.Errorf("failed to remove swarm service %s: %w", serviceID, err)
	}
	return nil
}

// InspectService retrieves real-time metadata of a Swarm service.
func (s *DockerSwarmManager) InspectService(ctx context.Context, serviceID string) (*ServiceStatus, error) {
	if serviceID == "" {
		return nil, ErrServiceNotFound
	}
	svc, _, err := s.cli.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect swarm service %s: %w", serviceID, err)
	}

	var replicas uint64
	if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
		replicas = *svc.Spec.Mode.Replicated.Replicas
	}

	var image string
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		image = svc.Spec.TaskTemplate.ContainerSpec.Image
	}

	var constraints []string
	if svc.Spec.TaskTemplate.Placement != nil {
		constraints = svc.Spec.TaskTemplate.Placement.Constraints
	}

	return &ServiceStatus{
		ID:              svc.ID,
		Name:            svc.Spec.Name,
		Image:           image,
		Replicas:        replicas,
		DesiredReplicas: replicas,
		RunningReplicas: replicas,
		CreatedAt:       svc.CreatedAt,
		UpdatedAt:       svc.UpdatedAt,
		Version:         svc.Version.Index,
		Labels:          svc.Spec.Labels,
		Constraints:     constraints,
	}, nil
}

// ListServices queries Swarm services matching filter options.
func (s *DockerSwarmManager) ListServices(ctx context.Context, opts ListOptions) ([]ServiceStatus, error) {
	f := filters.NewArgs()
	if opts.ProjectID != "" {
		f.Add("label", fmt.Sprintf("pikpik.project_id=%s", opts.ProjectID))
	}
	for k, v := range opts.Labels {
		f.Add("label", fmt.Sprintf("%s=%s", k, v))
	}

	services, err := s.cli.ServiceList(ctx, types.ServiceListOptions{Filters: f})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm services: %w", err)
	}

	result := make([]ServiceStatus, 0, len(services))
	for _, svc := range services {
		var replicas uint64
		if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
			replicas = *svc.Spec.Mode.Replicated.Replicas
		}

		var image string
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			image = svc.Spec.TaskTemplate.ContainerSpec.Image
		}

		var constraints []string
		if svc.Spec.TaskTemplate.Placement != nil {
			constraints = svc.Spec.TaskTemplate.Placement.Constraints
		}

		result = append(result, ServiceStatus{
			ID:              svc.ID,
			Name:            svc.Spec.Name,
			Image:           image,
			Replicas:        replicas,
			DesiredReplicas: replicas,
			RunningReplicas: replicas,
			CreatedAt:       svc.CreatedAt,
			UpdatedAt:       svc.UpdatedAt,
			Version:         svc.Version.Index,
			Labels:          svc.Spec.Labels,
			Constraints:     constraints,
		})
	}

	return result, nil
}

// ScaleService adjusts the desired replica count of a Swarm service.
func (s *DockerSwarmManager) ScaleService(ctx context.Context, serviceID string, replicas uint64) error {
	if serviceID == "" {
		return ErrServiceNotFound
	}
	svc, _, err := s.cli.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return fmt.Errorf("failed to inspect service for scaling %s: %w", serviceID, err)
	}

	svc.Spec.Mode.Replicated = &swarm.ReplicatedService{
		Replicas: &replicas,
	}

	_, err = s.cli.ServiceUpdate(
		ctx,
		serviceID,
		svc.Version,
		svc.Spec,
		types.ServiceUpdateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to scale service %s: %w", serviceID, err)
	}

	return nil
}

// ListTasks lists Swarm task instances across the cluster.
func (s *DockerSwarmManager) ListTasks(ctx context.Context, opts ListOptions) ([]TaskStatus, error) {
	f := filters.NewArgs()
	if opts.ProjectID != "" {
		f.Add("label", fmt.Sprintf("pikpik.project_id=%s", opts.ProjectID))
	}
	for k, v := range opts.Labels {
		f.Add("label", fmt.Sprintf("%s=%s", k, v))
	}

	tasks, err := s.cli.TaskList(ctx, types.TaskListOptions{Filters: f})
	if err != nil {
		return nil, fmt.Errorf("failed to list swarm tasks: %w", err)
	}

	result := make([]TaskStatus, 0, len(tasks))
	for _, t := range tasks {
		containerID := ""
		if t.Status.ContainerStatus != nil {
			containerID = t.Status.ContainerStatus.ContainerID
		}

		result = append(result, TaskStatus{
			ID:           t.ID,
			ServiceID:    t.ServiceID,
			NodeID:       t.NodeID,
			Slot:         t.Slot,
			State:        string(t.Status.State),
			DesiredState: string(t.DesiredState),
			Message:      t.Status.Message,
			Err:          t.Status.Err,
			ContainerID:  containerID,
			CreatedAt:    t.Meta.CreatedAt,
			UpdatedAt:    t.Meta.UpdatedAt,
		})
	}

	return result, nil
}

// ListNodes queries physical Swarm nodes and their status.
func (s *DockerSwarmManager) ListNodes(ctx context.Context) ([]NodeStatus, error) {
	nodes, err := s.cli.NodeList(ctx, types.NodeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	result := make([]NodeStatus, 0, len(nodes))
	for _, n := range nodes {
		reachability := "unreachable"
		if n.ManagerStatus != nil {
			reachability = string(n.ManagerStatus.Reachability)
		}

		result = append(result, NodeStatus{
			ID:            n.ID,
			Hostname:      n.Description.Hostname,
			Role:          string(n.Spec.Role),
			State:         string(n.Status.State),
			Availability:  string(n.Spec.Availability),
			Reachability:  reachability,
			IPAddress:     n.Status.Addr,
			EngineVersion: n.Description.Engine.EngineVersion,
			Labels:        n.Spec.Annotations.Labels,
			NanoCPUs:      n.Description.Resources.NanoCPUs,
			MemoryBytes:   n.Description.Resources.MemoryBytes,
		})
	}

	return result, nil
}

// UpdateNode modifies Swarm node attributes (availability, role, labels).
func (s *DockerSwarmManager) UpdateNode(ctx context.Context, nodeID string, version uint64, spec NodeSpec) error {
	if nodeID == "" {
		return errors.New("orchestrator: node ID required")
	}

	node, _, err := s.cli.NodeInspectWithRaw(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to inspect node %s: %w", nodeID, err)
	}

	if spec.Availability != "" {
		node.Spec.Availability = swarm.NodeAvailability(spec.Availability)
	}
	if spec.Role != "" {
		node.Spec.Role = swarm.NodeRole(spec.Role)
	}
	if spec.Labels != nil {
		node.Spec.Annotations.Labels = spec.Labels
	}

	err = s.cli.NodeUpdate(ctx, nodeID, swarm.Version{Index: version}, node.Spec)
	if err != nil {
		return fmt.Errorf("failed to update node %s: %w", nodeID, err)
	}

	return nil
}

// convertSpecToSwarm maps domain ServiceSpec into Docker Engine's swarm.ServiceSpec.
func (s *DockerSwarmManager) convertSpecToSwarm(spec ServiceSpec) swarm.ServiceSpec {
	// Replicas
	replicas := spec.Replicas
	if replicas == 0 {
		replicas = 1
	}

	// Labels
	labels := make(map[string]string)
	for k, v := range spec.Labels {
		labels[k] = v
	}
	labels["pikpik.managed"] = "true"
	if spec.ProjectID != "" {
		labels["pikpik.project_id"] = spec.ProjectID
	}

	// Environment
	envList := make([]string, 0, len(spec.Environment))
	for k, v := range spec.Environment {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}

	// Mounts
	mounts := make([]mount.Mount, 0, len(spec.Mounts))
	for _, m := range spec.Mounts {
		mountType := mount.TypeVolume
		if m.Type == "bind" {
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

	// Secrets
	secrets := make([]*swarm.SecretReference, 0, len(spec.Secrets))
	for _, sec := range spec.Secrets {
		mode := sec.TargetMode
		if mode == 0 {
			mode = 0444
		}
		secrets = append(secrets, &swarm.SecretReference{
			SecretID:   sec.SecretID,
			SecretName: sec.SecretName,
			File: &swarm.SecretReferenceFileTarget{
				Name: sec.TargetFileName,
				UID:  sec.TargetUID,
				GID:  sec.TargetGID,
				Mode: os.FileMode(mode),
			},
		})
	}

	// Configs
	configs := make([]*swarm.ConfigReference, 0, len(spec.Configs))
	for _, cfg := range spec.Configs {
		mode := cfg.TargetMode
		if mode == 0 {
			mode = 0444
		}
		configs = append(configs, &swarm.ConfigReference{
			ConfigID:   cfg.ConfigID,
			ConfigName: cfg.ConfigName,
			File: &swarm.ConfigReferenceFileTarget{
				Name: cfg.TargetFileName,
				UID:  cfg.TargetUID,
				GID:  cfg.TargetGID,
				Mode: os.FileMode(mode),
			},
		})
	}

	// Networks
	networks := make([]swarm.NetworkAttachmentConfig, 0, len(spec.Networks))
	for _, netName := range spec.Networks {
		networks = append(networks, swarm.NetworkAttachmentConfig{
			Target: netName,
		})
	}

	// Resource limits
	var resources *swarm.ResourceRequirements
	if spec.Resources.MemoryLimit > 0 || spec.Resources.CPULimit > 0 || spec.Resources.MemoryReserve > 0 || spec.Resources.CPUReserve > 0 {
		resources = &swarm.ResourceRequirements{
			Limits: &swarm.Limit{
				NanoCPUs:    spec.Resources.CPULimit,
				MemoryBytes: spec.Resources.MemoryLimit,
			},
			Reservations: &swarm.Resources{
				NanoCPUs:    spec.Resources.CPUReserve,
				MemoryBytes: spec.Resources.MemoryReserve,
			},
		}
	}

	// Healthcheck
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

	// Placement Constraints
	var placement *swarm.Placement
	if len(spec.Constraints) > 0 {
		placement = &swarm.Placement{
			Constraints: spec.Constraints,
		}
	}

	// Restart Policy
	condition := swarm.RestartPolicyConditionAny
	if spec.RestartPolicy.Condition == "none" {
		condition = swarm.RestartPolicyConditionNone
	} else if spec.RestartPolicy.Condition == "on-failure" {
		condition = swarm.RestartPolicyConditionOnFailure
	}

	maxAttempts := spec.RestartPolicy.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 5
	}

	restartPolicy := &swarm.RestartPolicy{
		Condition:   condition,
		MaxAttempts: &maxAttempts,
		Delay:       &spec.RestartPolicy.Delay,
		Window:      &spec.RestartPolicy.Window,
	}

	// Stop grace period
	var stopGracePeriod *time.Duration
	if spec.StopGracePeriod > 0 {
		stopGracePeriod = &spec.StopGracePeriod
	}

	// Endpoint Ports
	var endpointSpec *swarm.EndpointSpec
	if len(spec.Ports) > 0 {
		portConfigs := make([]swarm.PortConfig, 0, len(spec.Ports))
		for _, p := range spec.Ports {
			proto := swarm.PortConfigProtocolTCP
			if strings.ToLower(p.Protocol) == "udp" {
				proto = swarm.PortConfigProtocolUDP
			}
			portConfigs = append(portConfigs, swarm.PortConfig{
				Protocol:      proto,
				TargetPort:    p.ContainerPort,
				PublishedPort: p.HostPort,
			})
		}
		endpointSpec = &swarm.EndpointSpec{
			Ports: portConfigs,
		}
	}

	return swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   spec.Name,
			Labels: labels,
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:           spec.Image,
				Env:             envList,
				Mounts:          mounts,
				Secrets:         secrets,
				Configs:         configs,
				Healthcheck:     healthConfig,
				StopGracePeriod: stopGracePeriod,
			},
			Networks:      networks,
			Resources:     resources,
			Placement:     placement,
			RestartPolicy: restartPolicy,
		},
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{
				Replicas: &replicas,
			},
		},
		UpdateConfig: BuildSwarmUpdateConfig(spec.UpdateConfig),
		EndpointSpec: endpointSpec,
	}
}
