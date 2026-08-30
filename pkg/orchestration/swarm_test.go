package orchestration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
)

// TestSwarmManagerClusterLifecycle tests cluster init, join, leave, and info queries.
func TestSwarmManagerClusterLifecycle(t *testing.T) {
	mock := &MockDockerClient{}
	mgr := orchestration.NewDockerSwarmManager(mock)
	ctx := context.Background()

	// 1. Init
	nodeID, err := mgr.InitCluster(ctx, orchestration.SwarmInitRequest{
		ListenAddr:    "0.0.0.0:2377",
		AdvertiseAddr: "192.168.1.10",
	})
	if err != nil {
		t.Fatalf("failed to init cluster: %v", err)
	}
	if nodeID != "mock-leader-node-id" {
		t.Errorf("expected node ID mock-leader-node-id, got %s", nodeID)
	}

	// 2. Join
	err = mgr.JoinCluster(ctx, orchestration.SwarmJoinRequest{
		ListenAddr:    "0.0.0.0:2377",
		AdvertiseAddr: "192.168.1.11",
		RemoteAddrs:   []string{"192.168.1.10:2377"},
		JoinToken:     "SWMTKN-1-worker",
	})
	if err != nil {
		t.Fatalf("failed to join cluster: %v", err)
	}

	// 3. Info
	info, err := mgr.GetClusterInfo(ctx)
	if err != nil {
		t.Fatalf("failed to get cluster info: %v", err)
	}
	if info.ID != "mock-cluster-id" {
		t.Errorf("expected cluster ID mock-cluster-id, got %s", info.ID)
	}
	if info.Nodes != 1 || info.Managers != 1 || info.Workers != 0 {
		t.Errorf("unexpected node counts: %+v", info)
	}

	// 4. Leave
	if err := mgr.LeaveCluster(ctx, true); err != nil {
		t.Fatalf("failed to leave cluster: %v", err)
	}
}

// TestSwarmManagerServiceLifecycle tests service creation, inspection, scaling, listing, updating, and removal.
func TestSwarmManagerServiceLifecycle(t *testing.T) {
	mock := &MockDockerClient{}
	mgr := orchestration.NewDockerSwarmManager(mock)
	ctx := context.Background()

	// 1. Create Service
	spec := orchestration.ServiceSpec{
		Name:      "web-svc",
		ProjectID: "proj-1",
		Image:     "nginx:alpine",
		Replicas:  3,
		Environment: map[string]string{
			"PORT": "80",
		},
		Secrets: []orchestration.SecretAttachment{
			{SecretID: "sec-1", SecretName: "db_password", TargetFileName: "/run/secrets/db_password"},
		},
		Configs: []orchestration.ConfigAttachment{
			{ConfigID: "cfg-1", ConfigName: "nginx_conf", TargetFileName: "/etc/nginx/nginx.conf"},
		},
		Mounts: []orchestration.VolumeMountSpec{
			{Type: "volume", Source: "web_data", Target: "/var/www/html"},
		},
		Networks: []string{"pikpik-overlay"},
		Ports: []orchestration.PortMappingSpec{
			{ContainerPort: 80, HostPort: 80, Protocol: "tcp"},
		},
		Resources: orchestration.ResourceRequirements{
			CPULimit:      2e9,
			MemoryLimit:   1024 * 1024 * 1024,
			CPUReserve:    5e8,
			MemoryReserve: 256 * 1024 * 1024,
		},
		HealthCheck: &orchestration.HealthCheckConfig{
			Test:        []string{"CMD-SHELL", "curl -f http://localhost:80 || exit 1"},
			Interval:    10 * time.Second,
			Timeout:     5 * time.Second,
			StartPeriod: 15 * time.Second,
			Retries:     3,
		},
		Constraints: []string{"node.role == worker"},
		UpdateConfig: orchestration.RollingUpdateConfig{
			Order:       "start-first",
			Parallelism: 1,
			Delay:       5 * time.Second,
		},
		RestartPolicy: orchestration.RestartPolicySpec{
			Condition:   "any",
			MaxAttempts: 3,
		},
		StopGracePeriod: 20 * time.Second,
		Labels: map[string]string{
			"custom.label": "value",
		},
	}

	sid, err := mgr.CreateService(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}
	if sid != "mock-service-id" {
		t.Errorf("expected service ID mock-service-id, got %s", sid)
	}

	// 2. Inspect Service
	status, err := mgr.InspectService(ctx, sid)
	if err != nil {
		t.Fatalf("failed to inspect service: %v", err)
	}
	if status.Name != "mock-service" || status.Replicas != 2 {
		t.Errorf("unexpected service status: %+v", status)
	}

	// 3. Update Service
	err = mgr.UpdateService(ctx, sid, 1, spec)
	if err != nil {
		t.Fatalf("failed to update service: %v", err)
	}

	// 4. Scale Service
	err = mgr.ScaleService(ctx, sid, 5)
	if err != nil {
		t.Fatalf("failed to scale service: %v", err)
	}

	// 5. List Services
	services, err := mgr.ListServices(ctx, orchestration.ListOptions{ProjectID: "proj-1"})
	if err != nil {
		t.Fatalf("failed to list services: %v", err)
	}
	if len(services) != 1 {
		t.Errorf("expected 1 service, got %d", len(services))
	}

	// 6. List Tasks
	tasks, err := mgr.ListTasks(ctx, orchestration.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "task-1" {
		t.Errorf("unexpected task list: %+v", tasks)
	}

	// 7. List Nodes & Update Node
	nodes, err := mgr.ListNodes(ctx)
	if err != nil {
		t.Fatalf("failed to list nodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Role != "manager" {
		t.Errorf("unexpected node list: %+v", nodes)
	}

	err = mgr.UpdateNode(ctx, "node-1", 1, orchestration.NodeSpec{
		Availability: string(swarm.NodeAvailabilityDrain),
		Role:         string(swarm.NodeRoleWorker),
		Labels:       map[string]string{"zone": "us-west"},
	})
	if err != nil {
		t.Fatalf("failed to update node: %v", err)
	}

	// 8. Remove Service
	if err := mgr.RemoveService(ctx, sid); err != nil {
		t.Fatalf("failed to remove service: %v", err)
	}

	// 9. Edge cases
	if _, err := mgr.InspectService(ctx, ""); !errors.Is(err, orchestration.ErrServiceNotFound) {
		t.Errorf("expected ErrServiceNotFound for empty inspect ID, got %v", err)
	}
	if err := mgr.UpdateService(ctx, "", 1, spec); !errors.Is(err, orchestration.ErrServiceNotFound) {
		t.Errorf("expected ErrServiceNotFound for empty update ID, got %v", err)
	}
	if err := mgr.RemoveService(ctx, ""); !errors.Is(err, orchestration.ErrServiceNotFound) {
		t.Errorf("expected ErrServiceNotFound for empty remove ID, got %v", err)
	}
	if err := mgr.ScaleService(ctx, "", 2); !errors.Is(err, orchestration.ErrServiceNotFound) {
		t.Errorf("expected ErrServiceNotFound for empty scale ID, got %v", err)
	}
}
