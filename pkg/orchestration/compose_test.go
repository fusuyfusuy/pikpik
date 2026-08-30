package orchestration_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
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

func TestStackManager_RollbackAndErrorPropagation(t *testing.T) {
	ctx := context.Background()

	t.Run("NetworkCreate failure propagates and triggers rollback", func(t *testing.T) {
		removedNets := make([]string, 0)
		mock := &MockDockerClient{
			NetworkCreateFunc: func(ctx context.Context, name string, options types.NetworkCreate) (types.NetworkCreateResponse, error) {
				if name == "teststack_net2" {
					return types.NetworkCreateResponse{}, errors.New("network subnet collision")
				}
				return types.NetworkCreateResponse{ID: "nid-" + name}, nil
			},
			NetworkRemoveFunc: func(ctx context.Context, networkID string) error {
				removedNets = append(removedNets, networkID)
				return nil
			},
		}

		containers := orchestration.NewDockerContainerManager(mock)
		stacks := orchestration.NewDockerStackManager(mock, containers)

		spec := orchestration.ComposeStackSpec{
			Name:     "teststack",
			Networks: []string{"net1", "net2"},
			Services: map[string]orchestration.ComposeServiceDef{
				"app": {Name: "app", Image: "alpine"},
			},
		}

		res, err := stacks.DeployStack(ctx, spec)
		if err == nil {
			t.Fatalf("expected error from NetworkCreate failure, got nil")
		}
		if res == nil || len(res.Errors) == 0 {
			t.Errorf("expected errors recorded in deployment result")
		}

		// First network should have been cleaned up
		if len(removedNets) != 1 || removedNets[0] != "teststack_net1" {
			t.Errorf("expected net1 to be cleaned up on rollback, got %v", removedNets)
		}
	})

	t.Run("VolumeCreate failure propagates and triggers rollback", func(t *testing.T) {
		removedNets := make([]string, 0)
		removedVols := make([]string, 0)
		mock := &MockDockerClient{
			NetworkCreateFunc: func(ctx context.Context, name string, options types.NetworkCreate) (types.NetworkCreateResponse, error) {
				return types.NetworkCreateResponse{ID: "nid-" + name}, nil
			},
			NetworkRemoveFunc: func(ctx context.Context, networkID string) error {
				removedNets = append(removedNets, networkID)
				return nil
			},
			VolumeCreateFunc: func(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
				if options.Name == "testvolstack_vol2" {
					return volume.Volume{}, errors.New("disk quota exceeded")
				}
				return volume.Volume{Name: options.Name}, nil
			},
			VolumeRemoveFunc: func(ctx context.Context, volumeID string, force bool) error {
				removedVols = append(removedVols, volumeID)
				return nil
			},
		}

		containers := orchestration.NewDockerContainerManager(mock)
		stacks := orchestration.NewDockerStackManager(mock, containers)

		spec := orchestration.ComposeStackSpec{
			Name:     "testvolstack",
			Networks: []string{"net1"},
			Volumes:  []string{"vol1", "vol2"},
			Services: map[string]orchestration.ComposeServiceDef{
				"app": {Name: "app", Image: "alpine"},
			},
		}

		res, err := stacks.DeployStack(ctx, spec)
		if err == nil {
			t.Fatalf("expected error from VolumeCreate failure, got nil")
		}
		if res == nil || len(res.Errors) == 0 {
			t.Errorf("expected errors recorded in deployment result")
		}

		// vol1 and net1 should have been cleaned up
		if len(removedVols) != 1 || removedVols[0] != "testvolstack_vol1" {
			t.Errorf("expected vol1 to be cleaned up on rollback, got %v", removedVols)
		}
		if len(removedNets) != 1 || removedNets[0] != "testvolstack_net1" {
			t.Errorf("expected net1 to be cleaned up on rollback, got %v", removedNets)
		}
	})

	t.Run("Container creation failure cleans up volumes and networks", func(t *testing.T) {
		removedNets := make([]string, 0)
		removedVols := make([]string, 0)
		mock := &MockDockerClient{
			NetworkCreateFunc: func(ctx context.Context, name string, options types.NetworkCreate) (types.NetworkCreateResponse, error) {
				return types.NetworkCreateResponse{ID: "nid-" + name}, nil
			},
			NetworkRemoveFunc: func(ctx context.Context, networkID string) error {
				removedNets = append(removedNets, networkID)
				return nil
			},
			VolumeCreateFunc: func(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
				return volume.Volume{Name: options.Name}, nil
			},
			VolumeRemoveFunc: func(ctx context.Context, volumeID string, force bool) error {
				removedVols = append(removedVols, volumeID)
				return nil
			},
			ContainerCreateFunc: func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
				return container.CreateResponse{}, errors.New("image pull rate limit")
			},
		}

		containers := orchestration.NewDockerContainerManager(mock)
		stacks := orchestration.NewDockerStackManager(mock, containers)

		spec := orchestration.ComposeStackSpec{
			Name:     "testcstack",
			Networks: []string{"app_net"},
			Volumes:  []string{"app_data"},
			Services: map[string]orchestration.ComposeServiceDef{
				"web": {Name: "web", Image: "nginx:alpine"},
			},
		}

		res, err := stacks.DeployStack(ctx, spec)
		if err == nil {
			t.Fatalf("expected error from container creation failure, got nil")
		}
		if res == nil || len(res.Errors) == 0 {
			t.Errorf("expected errors in result")
		}

		// Verify volume and network cleanup
		if len(removedVols) != 1 || removedVols[0] != "testcstack_app_data" {
			t.Errorf("expected app_data volume to be cleaned up on rollback, got %v", removedVols)
		}
		if len(removedNets) != 1 || removedNets[0] != "testcstack_app_net" {
			t.Errorf("expected app_net network to be cleaned up on rollback, got %v", removedNets)
		}
	})
}

// TestInspectComposeYAML verifies YAML AST inspection, variable extraction, and Swarm suggestion.
func TestInspectComposeYAML(t *testing.T) {
	rawYAML := `version: '3.8'
services:
  web:
    image: node:20-alpine
    ports:
      - "3000:3000"
    environment:
      - API_KEY=${APP_API_KEY}
      - DB_URL=postgres://${DB_USER:-admin}:${DB_PASSWORD}@postgres:5432/${DB_NAME:-prod}
    volumes:
      - ./data:/app/data
    deploy:
      replicas: 3
  postgres:
    image: postgres:16-alpine
    ports:
      - "5432"
    environment:
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:

networks:
  backend:
`

	result, err := orchestration.InspectComposeYAML(rawYAML)
	if err != nil {
		t.Fatalf("InspectComposeYAML failed: %v", err)
	}

	if len(result.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(result.Services))
	}
	if result.SuggestedRuntime != "swarm" {
		t.Errorf("expected suggested runtime 'swarm' due to deploy.replicas=3, got '%s'", result.SuggestedRuntime)
	}

	// Verify Services
	if result.Services[0].Name != "postgres" || result.Services[1].Name != "web" {
		t.Errorf("expected sorted services [postgres, web], got [%s, %s]", result.Services[0].Name, result.Services[1].Name)
	}
	if !result.Services[1].HasDeployBlock || result.Services[1].Replicas != 3 {
		t.Errorf("expected web service to have deploy block with 3 replicas")
	}

	// Verify Exposed Ports
	if len(result.ExposedPorts) != 2 {
		t.Fatalf("expected 2 exposed ports, got %v", result.ExposedPorts)
	}
	if result.ExposedPorts[0] != 3000 || result.ExposedPorts[1] != 5432 {
		t.Errorf("expected exposed ports [3000, 5432], got %v", result.ExposedPorts)
	}

	// Verify Declared Volumes & Networks
	if len(result.DeclaredVolumes) < 1 || result.DeclaredVolumes[0] != "pgdata" {
		t.Errorf("expected declared volume 'pgdata', got %v", result.DeclaredVolumes)
	}
	if len(result.DeclaredNetworks) < 1 || result.DeclaredNetworks[0] != "backend" {
		t.Errorf("expected declared network 'backend', got %v", result.DeclaredNetworks)
	}

	// Verify Extracted Variables
	// Expected variables: APP_API_KEY (secret, required), DB_USER (default: admin), DB_PASSWORD (secret, required), DB_NAME (default: prod)
	varNames := make(map[string]orchestration.ComposeVariableDef)
	for _, v := range result.Variables {
		varNames[v.Name] = v
	}

	if v, ok := varNames["APP_API_KEY"]; !ok || !v.IsSecret || !v.Required {
		t.Errorf("expected APP_API_KEY to be secret and required: %+v", v)
	}
	if v, ok := varNames["DB_USER"]; !ok || v.DefaultValue != "admin" || v.Required {
		t.Errorf("expected DB_USER with default 'admin' and not required: %+v", v)
	}
	if v, ok := varNames["DB_PASSWORD"]; !ok || !v.IsSecret || !v.Required {
		t.Errorf("expected DB_PASSWORD to be secret and required: %+v", v)
	}
	if v, ok := varNames["DB_NAME"]; !ok || v.DefaultValue != "prod" || v.Required {
		t.Errorf("expected DB_NAME with default 'prod' and not required: %+v", v)
	}
}

func TestStackManager_StopAndRestart(t *testing.T) {
	ctx := context.Background()
	stoppedContainers := make([]string, 0)
	restartedContainers := make([]string, 0)

	mock := &MockDockerClient{
		ContainerListFunc: func(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
			return []types.Container{
				{
					ID:    "c-1",
					Names: []string{"/mystack_web"},
					State: "running",
					Labels: map[string]string{
						"pikpik.stack_name": "mystack",
					},
				},
				{
					ID:    "c-2",
					Names: []string{"/mystack_db"},
					State: "running",
					Labels: map[string]string{
						"pikpik.stack_name": "mystack",
					},
				},
			}, nil
		},
		ContainerStopFunc: func(ctx context.Context, containerID string, options container.StopOptions) error {
			stoppedContainers = append(stoppedContainers, containerID)
			return nil
		},
		ContainerRestartFunc: func(ctx context.Context, containerID string, options container.StopOptions) error {
			restartedContainers = append(restartedContainers, containerID)
			return nil
		},
	}

	containers := orchestration.NewDockerContainerManager(mock)
	stacks := orchestration.NewDockerStackManager(mock, containers)

	if err := stacks.StopStack(ctx, "mystack"); err != nil {
		t.Fatalf("StopStack error: %v", err)
	}
	if len(stoppedContainers) != 2 {
		t.Errorf("expected 2 stopped containers, got %d", len(stoppedContainers))
	}

	if err := stacks.RestartStack(ctx, "mystack"); err != nil {
		t.Fatalf("RestartStack error: %v", err)
	}
	if len(restartedContainers) != 2 {
		t.Errorf("expected 2 restarted containers, got %d", len(restartedContainers))
	}
}

func TestStackManager_DualNetworkingAndPersistentVolumes(t *testing.T) {
	ctx := context.Background()
	createdNets := make([]string, 0)
	createdVols := make([]string, 0)
	createdContainers := make([]string, 0)

	mock := &MockDockerClient{
		NetworkCreateFunc: func(ctx context.Context, name string, options types.NetworkCreate) (types.NetworkCreateResponse, error) {
			createdNets = append(createdNets, name)
			return types.NetworkCreateResponse{ID: "net-" + name}, nil
		},
		VolumeCreateFunc: func(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
			createdVols = append(createdVols, options.Name)
			return volume.Volume{Name: options.Name}, nil
		},
		ContainerCreateFunc: func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
			createdContainers = append(createdContainers, containerName)
			return container.CreateResponse{ID: "cid-" + containerName}, nil
		},
	}

	containers := orchestration.NewDockerContainerManager(mock)
	stacks := orchestration.NewDockerStackManager(mock, containers)

	spec := orchestration.ComposeStackSpec{
		Name:      "blog",
		ProjectID: "prj-abc",
		Networks:  []string{"webnet"},
		Volumes:   []string{"dbdata"},
		Services: map[string]orchestration.ComposeServiceDef{
			"db": {
				Name:  "db",
				Image: "postgres:16",
				Mounts: []orchestration.VolumeMountSpec{
					{Type: "volume", Source: "dbdata", Target: "/var/lib/postgresql/data"},
				},
			},
		},
	}

	res, err := stacks.DeployStack(ctx, spec)
	if err != nil {
		t.Fatalf("DeployStack failed: %v", err)
	}

	if len(res.ServicesDeployed) != 1 {
		t.Errorf("expected 1 service deployed, got %d", len(res.ServicesDeployed))
	}

	// Check persistent volume scoping: pikpik_vol_<proj>_<stack>_<vol>
	expectedVol := "pikpik_vol_prj-abc_blog_dbdata"
	if len(createdVols) != 1 || createdVols[0] != expectedVol {
		t.Errorf("expected scoped volume %s, got %v", expectedVol, createdVols)
	}

	// Check dual networking: stack network created, project mesh network created
	expectedStackNet := "blog_webnet"
	hasStackNet := false
	for _, n := range createdNets {
		if n == expectedStackNet {
			hasStackNet = true
		}
	}
	if !hasStackNet {
		t.Errorf("expected stack network %s in %v", expectedStackNet, createdNets)
	}
}

