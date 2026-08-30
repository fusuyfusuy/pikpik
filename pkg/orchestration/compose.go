package orchestration

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"gopkg.in/yaml.v3"
)

var (
	envVarRegex = regexp.MustCompile(`\$\{([a-zA-Z0-9_]+)(?::-([^}]*))?\}|\$([a-zA-Z0-9_]+)`)
)

// ResolveDeploymentOrder performs topological sorting on Compose services based on DependsOn (Kahn's algorithm).
func ResolveDeploymentOrder(services map[string]ComposeServiceDef) ([]string, error) {
	inDegree := make(map[string]int)
	graph := make(map[string][]string)

	for name := range services {
		inDegree[name] = 0
		graph[name] = make([]string, 0)
	}

	for name, svc := range services {
		for _, dep := range svc.DependsOn {
			if _, exists := services[dep]; !exists {
				return nil, fmt.Errorf("%w: service '%s' depends on unknown service '%s'", ErrUnknownDependency, name, dep)
			}
			// dep must start before name -> edge: dep -> name
			graph[dep] = append(graph[dep], name)
			inDegree[name]++
		}
	}

	// Deterministic ordering: collect sorted candidate queue
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)

	order := make([]string, 0, len(services))
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		order = append(order, curr)

		// Sort neighbors for deterministic execution
		neighbors := graph[curr]
		sort.Strings(neighbors)

		for _, neighbor := range neighbors {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
				sort.Strings(queue)
			}
		}
	}

	if len(order) != len(services) {
		return nil, fmt.Errorf("%w", ErrCyclicDependency)
	}

	return order, nil
}

// interpolateEnv replaces $VAR and ${VAR:-default} occurrences with environment values.
func interpolateEnv(s string, envVars map[string]string) string {
	return envVarRegex.ReplaceAllStringFunc(s, func(match string) string {
		sub := envVarRegex.FindStringSubmatch(match)
		var varName string
		var defaultVal string

		if sub[1] != "" {
			varName = sub[1]
			defaultVal = sub[2]
		} else if sub[3] != "" {
			varName = sub[3]
		}

		if val, exists := envVars[varName]; exists && val != "" {
			return val
		}
		return defaultVal
	})
}

// rawComposeFile represents the raw YAML structure of a Docker Compose v2 file.
type rawComposeFile struct {
	Version  string                            `yaml:"version"`
	Services map[string]rawComposeService      `yaml:"services"`
	Networks map[string]map[string]interface{} `yaml:"networks"`
	Volumes  map[string]map[string]interface{} `yaml:"volumes"`
}

type rawComposeService struct {
	Image       string                 `yaml:"image"`
	Build       interface{}            `yaml:"build"`
	Command     interface{}            `yaml:"command"`
	Entrypoint  interface{}            `yaml:"entrypoint"`
	Environment interface{}            `yaml:"environment"`
	DependsOn   interface{}            `yaml:"depends_on"`
	Ports       []interface{}          `yaml:"ports"`
	Volumes     []interface{}          `yaml:"volumes"`
	Networks    interface{}            `yaml:"networks"`
	Restart     string                 `yaml:"restart"`
	Labels      interface{}            `yaml:"labels"`
	HealthCheck *rawComposeHealthCheck `yaml:"healthcheck"`
}

type rawComposeHealthCheck struct {
	Test        interface{}   `yaml:"test"`
	Interval    time.Duration `yaml:"interval"`
	Timeout     time.Duration `yaml:"timeout"`
	StartPeriod time.Duration `yaml:"start_period"`
	Retries     int           `yaml:"retries"`
}

// ParseComposeYAML parses and converts raw Compose YAML into a domain ComposeStackSpec.
func ParseComposeYAML(rawYAML string, envVars map[string]string) (*ComposeStackSpec, error) {
	if envVars == nil {
		envVars = make(map[string]string)
	}

	interpolated := interpolateEnv(rawYAML, envVars)

	var raw rawComposeFile
	if err := yaml.Unmarshal([]byte(interpolated), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse compose YAML: %w", err)
	}

	spec := &ComposeStackSpec{
		RawYAML:  rawYAML,
		Services: make(map[string]ComposeServiceDef),
		EnvVars:  envVars,
	}

	for netName := range raw.Networks {
		spec.Networks = append(spec.Networks, netName)
	}
	sort.Strings(spec.Networks)

	for volName := range raw.Volumes {
		spec.Volumes = append(spec.Volumes, volName)
	}
	sort.Strings(spec.Volumes)

	for name, s := range raw.Services {
		svc := ComposeServiceDef{
			Name:        name,
			Image:       s.Image,
			Restart:     s.Restart,
			Environment: make(map[string]string),
			Labels:      make(map[string]string),
		}

		// Parse Command
		switch cmd := s.Command.(type) {
		case string:
			svc.Command = strings.Fields(cmd)
		case []interface{}:
			for _, item := range cmd {
				if str, ok := item.(string); ok {
					svc.Command = append(svc.Command, str)
				}
			}
		}

		// Parse Entrypoint
		switch ep := s.Entrypoint.(type) {
		case string:
			svc.Entrypoint = strings.Fields(ep)
		case []interface{}:
			for _, item := range ep {
				if str, ok := item.(string); ok {
					svc.Entrypoint = append(svc.Entrypoint, str)
				}
			}
		}

		// Parse Environment
		switch env := s.Environment.(type) {
		case map[string]interface{}:
			for k, v := range env {
				svc.Environment[k] = fmt.Sprintf("%v", v)
			}
		case []interface{}:
			for _, item := range env {
				if str, ok := item.(string); ok {
					parts := strings.SplitN(str, "=", 2)
					if len(parts) == 2 {
						svc.Environment[parts[0]] = parts[1]
					} else if len(parts) == 1 {
						svc.Environment[parts[0]] = envVars[parts[0]]
					}
				}
			}
		}

		// Parse DependsOn
		switch deps := s.DependsOn.(type) {
		case []interface{}:
			for _, item := range deps {
				if str, ok := item.(string); ok {
					svc.DependsOn = append(svc.DependsOn, str)
				}
			}
		case map[string]interface{}:
			for k := range deps {
				svc.DependsOn = append(svc.DependsOn, k)
			}
		}

		// Parse Networks
		switch nets := s.Networks.(type) {
		case []interface{}:
			for _, item := range nets {
				if str, ok := item.(string); ok {
					svc.Networks = append(svc.Networks, str)
				}
			}
		case map[string]interface{}:
			for k := range nets {
				svc.Networks = append(svc.Networks, k)
			}
		}

		// Parse Ports
		for _, item := range s.Ports {
			if str, ok := item.(string); ok {
				// e.g. "80:80", "8080:80/tcp", "80"
				parts := strings.Split(str, "/")
				proto := "tcp"
				if len(parts) == 2 {
					proto = parts[1]
				}
				portMappings := strings.Split(parts[0], ":")
				if len(portMappings) == 2 {
					hostP, _ := strconv.ParseUint(portMappings[0], 10, 32)
					contP, _ := strconv.ParseUint(portMappings[1], 10, 32)
					svc.Ports = append(svc.Ports, PortMappingSpec{
						HostPort:      uint32(hostP),
						ContainerPort: uint32(contP),
						Protocol:      proto,
					})
				} else if len(portMappings) == 1 {
					contP, _ := strconv.ParseUint(portMappings[0], 10, 32)
					svc.Ports = append(svc.Ports, PortMappingSpec{
						ContainerPort: uint32(contP),
						Protocol:      proto,
					})
				}
			}
		}

		// Parse Volumes / Mounts
		for _, item := range s.Volumes {
			if str, ok := item.(string); ok {
				parts := strings.Split(str, ":")
				if len(parts) >= 2 {
					mType := "volume"
					if strings.HasPrefix(parts[0], "/") || strings.HasPrefix(parts[0], ".") {
						mType = "bind"
					}
					readOnly := false
					if len(parts) == 3 && (parts[2] == "ro" || strings.Contains(parts[2], "ro")) {
						readOnly = true
					}
					svc.Mounts = append(svc.Mounts, VolumeMountSpec{
						Type:     mType,
						Source:   parts[0],
						Target:   parts[1],
						ReadOnly: readOnly,
					})
				}
			}
		}

		// Parse Labels
		switch lbls := s.Labels.(type) {
		case map[string]interface{}:
			for k, v := range lbls {
				svc.Labels[k] = fmt.Sprintf("%v", v)
			}
		case []interface{}:
			for _, item := range lbls {
				if str, ok := item.(string); ok {
					parts := strings.SplitN(str, "=", 2)
					if len(parts) == 2 {
						svc.Labels[parts[0]] = parts[1]
					}
				}
			}
		}

		// Parse HealthCheck
		if s.HealthCheck != nil {
			var testCmd []string
			switch t := s.HealthCheck.Test.(type) {
			case string:
				testCmd = []string{"CMD-SHELL", t}
			case []interface{}:
				for _, item := range t {
					if str, ok := item.(string); ok {
						testCmd = append(testCmd, str)
					}
				}
			}
			svc.HealthCheck = &HealthCheckConfig{
				Test:        testCmd,
				Interval:    s.HealthCheck.Interval,
				Timeout:     s.HealthCheck.Timeout,
				StartPeriod: s.HealthCheck.StartPeriod,
				Retries:     s.HealthCheck.Retries,
			}
		}

		spec.Services[name] = svc
	}

	return spec, nil
}

// DockerStackManager manages multi-container Compose v2 applications directly through Docker Engine socket.
type DockerStackManager struct {
	cli        client.CommonAPIClient
	containers ContainerManager
}

// NewDockerStackManager creates a new DockerStackManager instance.
func NewDockerStackManager(cli client.CommonAPIClient, containers ContainerManager) *DockerStackManager {
	return &DockerStackManager{
		cli:        cli,
		containers: containers,
	}
}

// DeployStack parses topological dependencies and deploys a multi-container stack with health gating and rollback.
func (m *DockerStackManager) DeployStack(ctx context.Context, spec ComposeStackSpec) (*StackDeploymentResult, error) {
	if spec.Name == "" {
		return nil, fmt.Errorf("stack name cannot be empty")
	}

	result := &StackDeploymentResult{
		StackName:  spec.Name,
		DeployedAt: time.Now(),
	}

	// 1. Reconcile Networks
	createdNets := make([]string, 0)
	for _, netName := range spec.Networks {
		fullNetName := fmt.Sprintf("%s_%s", spec.Name, netName)
		_, err := m.cli.NetworkCreate(ctx, fullNetName, types.NetworkCreate{
			Driver: "bridge",
			Labels: map[string]string{
				"pikpik.stack_name": spec.Name,
				"pikpik.project_id": spec.ProjectID,
				"pikpik.managed":    "true",
			},
		})
		if err == nil {
			createdNets = append(createdNets, fullNetName)
		}
	}
	result.CreatedNetworks = createdNets

	// 2. Reconcile Volumes
	createdVols := make([]string, 0)
	for _, volName := range spec.Volumes {
		fullVolName := fmt.Sprintf("%s_%s", spec.Name, volName)
		_, err := m.cli.VolumeCreate(ctx, volume.CreateOptions{
			Name: fullVolName,
			Labels: map[string]string{
				"pikpik.stack_name": spec.Name,
				"pikpik.project_id": spec.ProjectID,
				"pikpik.managed":    "true",
			},
		})
		if err == nil {
			createdVols = append(createdVols, fullVolName)
		}
	}
	result.CreatedVolumes = createdVols

	// 3. Resolve Topological Service Deployment Order
	order, err := ResolveDeploymentOrder(spec.Services)
	if err != nil {
		return nil, err
	}

	// 4. Sequentially Deploy Services with Rollback Guard
	deployedContainers := make([]string, 0)
	rollback := func(deployErr error) (*StackDeploymentResult, error) {
		// Teardown all containers created during this deployment
		for _, cid := range deployedContainers {
			_ = m.containers.Stop(context.Background(), cid, 5*time.Second)
			_ = m.containers.Remove(context.Background(), cid, true, true)
		}
		// Teardown networks created during this deployment
		for _, nid := range createdNets {
			_ = m.cli.NetworkRemove(context.Background(), nid)
		}
		result.Errors = append(result.Errors, deployErr.Error())
		return result, deployErr
	}

	for _, svcName := range order {
		svcDef := spec.Services[svcName]

		// Resolve networks for container
		var svcNetworks []string
		if len(svcDef.Networks) > 0 {
			for _, n := range svcDef.Networks {
				svcNetworks = append(svcNetworks, fmt.Sprintf("%s_%s", spec.Name, n))
			}
		} else if len(spec.Networks) > 0 {
			// Connect to first declared stack network by default
			svcNetworks = append(svcNetworks, fmt.Sprintf("%s_%s", spec.Name, spec.Networks[0]))
		}

		// Resolve volume names with stack prefix
		var svcMounts []VolumeMountSpec
		for _, mnt := range svcDef.Mounts {
			targetSource := mnt.Source
			if mnt.Type == "volume" {
				targetSource = fmt.Sprintf("%s_%s", spec.Name, mnt.Source)
			}
			svcMounts = append(svcMounts, VolumeMountSpec{
				Type:     mnt.Type,
				Source:   targetSource,
				Target:   mnt.Target,
				ReadOnly: mnt.ReadOnly,
			})
		}

		// Merge labels
		labels := make(map[string]string)
		for k, v := range svcDef.Labels {
			labels[k] = v
		}
		labels["pikpik.stack_name"] = spec.Name
		labels["pikpik.service_name"] = svcName
		labels["pikpik.project_id"] = spec.ProjectID
		labels["pikpik.managed"] = "true"

		// Merge environment
		envMap := make(map[string]string)
		for k, v := range spec.EnvVars {
			envMap[k] = v
		}
		for k, v := range svcDef.Environment {
			envMap[k] = v
		}

		containerSpec := ContainerSpec{
			Name:          fmt.Sprintf("%s_%s", spec.Name, svcName),
			ProjectID:     spec.ProjectID,
			Image:         svcDef.Image,
			Environment:   envMap,
			Mounts:        svcMounts,
			Networks:      svcNetworks,
			ExposedPorts:  svcDef.Ports,
			Resources:     svcDef.Resources,
			HealthCheck:   svcDef.HealthCheck,
			RestartPolicy: svcDef.Restart,
			Labels:        labels,
			Command:       svcDef.Command,
			Entrypoint:    svcDef.Entrypoint,
		}

		cid, err := m.containers.Create(ctx, containerSpec)
		if err != nil {
			return rollback(fmt.Errorf("failed to create service container '%s': %w", svcName, err))
		}
		deployedContainers = append(deployedContainers, cid)

		if err := m.containers.Start(ctx, cid); err != nil {
			return rollback(fmt.Errorf("failed to start service container '%s': %w", svcName, err))
		}

		// Health probing if configured
		if svcDef.HealthCheck != nil && len(svcDef.HealthCheck.Test) > 0 {
			timeout := svcDef.HealthCheck.StartPeriod + (svcDef.HealthCheck.Timeout * time.Duration(svcDef.HealthCheck.Retries))
			if timeout == 0 {
				timeout = 30 * time.Second
			}
			probeCtx, probeCancel := context.WithTimeout(ctx, timeout)
			ticker := time.NewTicker(200 * time.Millisecond)
			healthy := false

			for !healthy {
				select {
				case <-probeCtx.Done():
					ticker.Stop()
					probeCancel()
					return rollback(fmt.Errorf("healthcheck timeout for service '%s'", svcName))
				case <-ticker.C:
					stat, inspectErr := m.containers.Inspect(probeCtx, cid)
					if inspectErr == nil {
						if stat.Health == "healthy" {
							healthy = true
						} else if stat.Health == "unhealthy" || stat.State == "exited" {
							ticker.Stop()
							probeCancel()
							return rollback(fmt.Errorf("service '%s' reported unhealthy/exited", svcName))
						}
					}
				}
			}
			ticker.Stop()
			probeCancel()
		}

		result.ServicesDeployed = append(result.ServicesDeployed, svcName)
	}

	return result, nil
}

// RemoveStack stops and tears down all containers, networks, and resources associated with a stack.
func (m *DockerStackManager) RemoveStack(ctx context.Context, stackName string) error {
	if stackName == "" {
		return ErrStackNotFound
	}

	containers, err := m.containers.List(ctx, ListOptions{
		Labels: map[string]string{"pikpik.stack_name": stackName},
		All:    true,
	})
	if err != nil {
		return fmt.Errorf("failed to list stack containers: %w", err)
	}

	for _, c := range containers {
		_ = m.containers.Stop(ctx, c.ID, 5*time.Second)
		_ = m.containers.Remove(ctx, c.ID, true, true)
	}

	// Remove stack networks
	f := filters.NewArgs()
	f.Add("label", fmt.Sprintf("pikpik.stack_name=%s", stackName))
	nets, _ := m.cli.NetworkList(ctx, types.NetworkListOptions{Filters: f})
	for _, n := range nets {
		_ = m.cli.NetworkRemove(ctx, n.ID)
	}

	return nil
}

// InspectStack queries the current runtime status and active containers of a stack.
func (m *DockerStackManager) InspectStack(ctx context.Context, stackName string) (*StackStatus, error) {
	if stackName == "" {
		return nil, ErrStackNotFound
	}

	containers, err := m.containers.List(ctx, ListOptions{
		Labels: map[string]string{"pikpik.stack_name": stackName},
		All:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect stack containers: %w", err)
	}

	if len(containers) == 0 {
		return nil, ErrStackNotFound
	}

	projectID := ""
	state := "running"
	for _, c := range containers {
		if c.Labels != nil && c.Labels["pikpik.project_id"] != "" {
			projectID = c.Labels["pikpik.project_id"]
		}
		if c.State != "running" {
			state = "degraded"
		}
	}

	return &StackStatus{
		Name:       stackName,
		ProjectID:  projectID,
		Containers: containers,
		State:      state,
		CreatedAt:  containers[0].CreatedAt,
	}, nil
}

// ListStacks discovers and summarizes all active Compose stacks managed by pikpik.
func (m *DockerStackManager) ListStacks(ctx context.Context) ([]StackSummary, error) {
	containers, err := m.containers.List(ctx, ListOptions{
		All: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list stacks: %w", err)
	}

	stacksMap := make(map[string]*StackSummary)
	for _, c := range containers {
		stackName, hasStack := c.Labels["pikpik.stack_name"]
		if !hasStack || stackName == "" {
			continue
		}

		summary, exists := stacksMap[stackName]
		if !exists {
			summary = &StackSummary{
				Name:            stackName,
				ProjectID:       c.Labels["pikpik.project_id"],
				ServicesCount:   0,
				ContainersCount: 0,
				State:           "running",
				CreatedAt:       c.CreatedAt,
			}
			stacksMap[stackName] = summary
		}

		summary.ContainersCount++
		if c.Labels["pikpik.service_name"] != "" {
			summary.ServicesCount++
		}
		if c.State != "running" {
			summary.State = "degraded"
		}
	}

	result := make([]StackSummary, 0, len(stacksMap))
	for _, s := range stacksMap {
		result = append(result, *s)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}
