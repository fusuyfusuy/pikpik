package orchestration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
)

func TestAdversarial_Swarm_UnreachableManagerNodeReporting(t *testing.T) {
	mock := &MockDockerClient{}
	mgr := orchestration.NewDockerSwarmManager(mock)
	ctx := context.Background()

	mock.NodeListFunc = func(ctx context.Context, options types.NodeListOptions) ([]swarm.Node, error) {
		return []swarm.Node{
			{
				ID: "mgr-01",
				Description: swarm.NodeDescription{
					Hostname: "manager-01",
				},
				Spec: swarm.NodeSpec{
					Role:         swarm.NodeRoleManager,
					Availability: swarm.NodeAvailabilityActive,
				},
				Status: swarm.NodeStatus{
					State: swarm.NodeStateReady,
					Addr:  "192.168.1.10",
				},
				ManagerStatus: &swarm.ManagerStatus{
					Reachability: swarm.ReachabilityUnreachable,
				},
			},
			{
				ID: "worker-01",
				Description: swarm.NodeDescription{
					Hostname: "worker-01",
				},
				Spec: swarm.NodeSpec{
					Role:         swarm.NodeRoleWorker,
					Availability: swarm.NodeAvailabilityActive,
				},
				Status: swarm.NodeStatus{
					State: swarm.NodeStateDown,
					Addr:  "192.168.1.11",
				},
				ManagerStatus: nil, // Workers have nil ManagerStatus
			},
		} , nil
	}

	nodes, err := mgr.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes failed: %v", err)
	}

	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	if nodes[0].Reachability != "unreachable" {
		t.Fatalf("expected manager node reachability 'unreachable', got %s", nodes[0].Reachability)
	}
	if nodes[1].Reachability != "unreachable" {
		t.Fatalf("expected worker default reachability 'unreachable', got %s", nodes[1].Reachability)
	}
}

func TestAdversarial_Swarm_LossOfQuorumServiceCreateFailFast(t *testing.T) {
	mock := &MockDockerClient{}
	mgr := orchestration.NewDockerSwarmManager(mock)
	ctx := context.Background()

	errQuorumLost := errors.New("rpc error: code = Unknown desc = The swarm does not have a leader. It is possible that the leader is currently undergoing raft election")
	mock.ServiceCreateFunc = func(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
		return swarm.ServiceCreateResponse{}, errQuorumLost
	}

	mock.NodeListFunc = func(ctx context.Context, options types.NodeListOptions) ([]swarm.Node, error) {
		return []swarm.Node{
			{
				ID: "mgr-01",
				Spec: swarm.NodeSpec{
					Role:         swarm.NodeRoleManager,
					Availability: swarm.NodeAvailabilityActive,
				},
				Status: swarm.NodeStatus{State: swarm.NodeStateReady},
			},
		}, nil
	}

	spec := orchestration.ServiceSpec{
		Name:  "web",
		Image: "nginx:alpine",
	}

	start := time.Now()
	_, err := mgr.CreateService(ctx, spec)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error on quorum loss, got nil")
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("CreateService took excessive time on quorum loss: %v", elapsed)
	}
}

func TestAdversarial_Swarm_FlappingWorkerAvailabilityRapidTransitions(t *testing.T) {
	// Simulate rapid availability transitions
	availabilities := []string{"active", "pause", "drain", "down", "active"}

	baseNode := orchestration.NodeStatus{
		ID:        "worker-flapping",
		Role:      "worker",
		State:     "ready",
		NanoCPUs:  4e9,
		MemoryBytes: 8 * 1024 * 1024 * 1024,
	}

	req := orchestration.ResourceRequirements{MemoryReserve: 1024 * 1024 * 1024}
	constraint, err := orchestration.ParseConstraint("node.role == worker")
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		avail := availabilities[i%len(availabilities)]
		node := baseNode
		node.Availability = avail
		if avail == "down" {
			node.State = "down"
		}

		err := orchestration.ValidateConstraintsAgainstNodes([]orchestration.PlacementConstraint{constraint}, req, []orchestration.NodeStatus{node})
		if avail == "active" {
			if err != nil {
				t.Fatalf("iteration %d: active ready node was rejected: %v", i, err)
			}
		} else {
			if err == nil {
				t.Fatalf("iteration %d: non-active/down node (%s/%s) was accepted for placement", i, avail, node.State)
			}
		}
	}
}

func TestAdversarial_Swarm_ConcurrentNodeUpdateVersionConflict(t *testing.T) {
	mock := &MockDockerClient{}
	mgr := orchestration.NewDockerSwarmManager(mock)
	ctx := context.Background()

	mock.NodeInspectWithRawFunc = func(ctx context.Context, node_id string) (swarm.Node, []byte, error) {
		return swarm.Node{
			ID: node_id,
			Meta: swarm.Meta{
				Version: swarm.Version{Index: 42},
			},
		}, nil, nil
	}

	mock.NodeUpdateFunc = func(ctx context.Context, nodeID string, version swarm.Version, node swarm.NodeSpec) error {
		if version.Index != 42 {
			return errors.New("rpc error: code = InvalidArgument desc = update out of sequence")
		}
		return nil
	}

	// 1. Success with current version
	err := mgr.UpdateNode(ctx, "worker-1", 42, orchestration.NodeSpec{Availability: "drain"})
	if err != nil {
		t.Fatalf("unexpected error updating node: %v", err)
	}

	// 2. Conflict with stale version
	err = mgr.UpdateNode(ctx, "worker-1", 40, orchestration.NodeSpec{Availability: "drain"})
	if err == nil {
		t.Fatal("expected error with stale version index, got nil")
	}
}

func TestAdversarial_Swarm_ContradictoryRoleAndZoneConstraints(t *testing.T) {
	// Contradictory constraints: role == manager AND role == worker
	c1, _ := orchestration.ParseConstraint("node.role == manager")
	c2, _ := orchestration.ParseConstraint("node.role == worker")

	nodes := []orchestration.NodeStatus{
		{ID: "node-1", Role: "manager", State: "ready", Availability: "active"},
		{ID: "node-2", Role: "worker", State: "ready", Availability: "active"},
	}

	req := orchestration.ResourceRequirements{}
	err := orchestration.ValidateConstraintsAgainstNodes([]orchestration.PlacementConstraint{c1, c2}, req, nodes)
	if err == nil || !errors.Is(err, orchestration.ErrNoMatchingNodeAvailable) {
		t.Fatalf("expected ErrNoMatchingNodeAvailable for contradictory role constraints, got %v", err)
	}

	// Mutually exclusive zone labels
	cZone1, _ := orchestration.ParseConstraint("node.labels.zone == us-east-1")
	cZone2, _ := orchestration.ParseConstraint("node.labels.zone == eu-west-1")

	nodesZone := []orchestration.NodeStatus{
		{ID: "node-1", Role: "worker", State: "ready", Availability: "active", Labels: map[string]string{"zone": "us-east-1"}},
		{ID: "node-2", Role: "worker", State: "ready", Availability: "active", Labels: map[string]string{"zone": "eu-west-1"}},
	}

	err = orchestration.ValidateConstraintsAgainstNodes([]orchestration.PlacementConstraint{cZone1, cZone2}, req, nodesZone)
	if err == nil || !errors.Is(err, orchestration.ErrNoMatchingNodeAvailable) {
		t.Fatalf("expected ErrNoMatchingNodeAvailable for mutually exclusive zones, got %v", err)
	}
}

func TestAdversarial_Swarm_InvalidConstraintSyntaxAndOperators(t *testing.T) {
	// Single '=' operator is invalid
	_, err := orchestration.ParseConstraint("node.role = manager")
	if err == nil || !errors.Is(err, orchestration.ErrUnsupportedConstraintOp) {
		t.Fatalf("expected ErrUnsupportedConstraintOp for '=', got %v", err)
	}

	// Empty string
	_, err = orchestration.ParseConstraint("")
	if err == nil || !errors.Is(err, orchestration.ErrInvalidConstraintSyntax) {
		t.Fatalf("expected ErrInvalidConstraintSyntax for empty constraint, got %v", err)
	}
}

func TestAdversarial_Swarm_ResourceReservationExceedingClusterCapacity(t *testing.T) {
	// 3 nodes with 32GB RAM each
	nodes := []orchestration.NodeStatus{
		{ID: "node-1", Role: "worker", State: "ready", Availability: "active", MemoryBytes: 32 * 1024 * 1024 * 1024, NanoCPUs: 8e9},
		{ID: "node-2", Role: "worker", State: "ready", Availability: "active", MemoryBytes: 32 * 1024 * 1024 * 1024, NanoCPUs: 8e9},
		{ID: "node-3", Role: "worker", State: "ready", Availability: "active", MemoryBytes: 32 * 1024 * 1024 * 1024, NanoCPUs: 8e9},
	}

	req := orchestration.ResourceRequirements{
		MemoryReserve: 128 * 1024 * 1024 * 1024, // 128GB requested
	}

	err := orchestration.ValidateConstraintsAgainstNodes(nil, req, nodes)
	if err == nil || !errors.Is(err, orchestration.ErrResourceCapacityExceeded) {
		t.Fatalf("expected ErrResourceCapacityExceeded for 128GB reservation on 32GB nodes, got %v", err)
	}
}

func TestAdversarial_Swarm_RollingUpdateFailureAndRollbackVerification(t *testing.T) {
	cfg := orchestration.RollingUpdateConfig{
		Parallelism:     2,
		Delay:           5 * time.Second,
		FailureAction:   "rollback",
		Monitor:         10 * time.Second,
		MaxFailureRatio: 0.2,
		Order:           "start-first",
	}

	swarmConfig := orchestration.BuildSwarmUpdateConfig(cfg)
	if swarmConfig == nil {
		t.Fatal("expected non-nil swarm update config")
	}

	if swarmConfig.FailureAction != "rollback" {
		t.Fatalf("expected FailureAction == 'rollback', got %s", swarmConfig.FailureAction)
	}
	if swarmConfig.Order != "start-first" {
		t.Fatalf("expected Order == 'start-first', got %s", swarmConfig.Order)
	}
	if swarmConfig.Parallelism != 2 {
		t.Fatalf("expected Parallelism == 2, got %d", swarmConfig.Parallelism)
	}
}

func TestAdversarial_Swarm_UpdateConfigOrderStartFirstPreservation(t *testing.T) {
	cfg := orchestration.RollingUpdateConfig{
		Order: "start-first",
	}
	swarmConfig := orchestration.BuildSwarmUpdateConfig(cfg)
	if swarmConfig.Order != "start-first" {
		t.Fatalf("expected start-first preserved, got %s", swarmConfig.Order)
	}

	cfgStop := orchestration.RollingUpdateConfig{
		Order: "stop-first",
	}
	swarmConfigStop := orchestration.BuildSwarmUpdateConfig(cfgStop)
	if swarmConfigStop.Order != "stop-first" {
		t.Fatalf("expected stop-first preserved, got %s", swarmConfigStop.Order)
	}
}
