package orchestration

import (
	"context"
	"fmt"
	"net/http"

	"github.com/docker/docker/client"
)

// EngineClient wraps the Docker Engine socket client and coordinates runtime modes.
type EngineClient struct {
	cli        client.CommonAPIClient
	mode       RuntimeMode
	containers ContainerManager
	swarm      SwarmManager
	stacks     StackManager
	logs       LogStreamer
}

// NewDockerEngineClient initializes the Unix socket client with API negotiation.
func NewDockerEngineClient(ctx context.Context, socketPath string) (*EngineClient, error) {
	if socketPath == "" {
		socketPath = "unix:///var/run/docker.sock"
	}

	cli, err := client.NewClientWithOpts(
		client.WithHost(socketPath),
		client.WithHTTPClient(&http.Client{
			Timeout: 0,
		}),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return NewOrchestrator(ctx, cli)
}

// NewOrchestrator initializes an orchestrator gateway wrapping any CommonAPIClient implementation.
func NewOrchestrator(ctx context.Context, cli client.CommonAPIClient) (*EngineClient, error) {
	mode, err := DetectRuntimeMode(ctx, cli)
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("%w: %v", ErrSocketUnreachable, err)
	}

	containers := NewDockerContainerManager(cli)
	swarmMgr := NewDockerSwarmManager(cli)
	stacks := NewDockerStackManager(cli, containers)
	logs := NewDockerLogStreamer(cli)

	return &EngineClient{
		cli:        cli,
		mode:       mode,
		containers: containers,
		swarm:      swarmMgr,
		stacks:     stacks,
		logs:       logs,
	}, nil
}

// DetectRuntimeMode probes the Docker daemon to determine whether Swarm is active or standalone mode is used.
func DetectRuntimeMode(ctx context.Context, cli client.CommonAPIClient) (RuntimeMode, error) {
	info, err := cli.Info(ctx)
	if err != nil {
		return ModeDisconnected, err
	}

	if info.Swarm.LocalNodeState == "active" {
		if info.Swarm.ControlAvailable {
			return ModeSwarmLeader, nil
		}
		return ModeSwarmWorker, nil
	}

	return ModeStandalone, nil
}

// Mode returns the active operational mode of the cluster or standalone node.
func (e *EngineClient) Mode() RuntimeMode {
	return e.mode
}

// Ping checks if the Docker daemon socket is alive and responding.
func (e *EngineClient) Ping(ctx context.Context) error {
	_, err := e.cli.Ping(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSocketUnreachable, err)
	}
	return nil
}

// Close terminates the underlying Docker API client and releases active connections.
func (e *EngineClient) Close() error {
	return e.cli.Close()
}

// RawClient returns the underlying CommonAPIClient for low-level stream/exec operations.
func (e *EngineClient) RawClient() client.CommonAPIClient {
	return e.cli
}

// Containers returns the standalone ContainerManager.
func (e *EngineClient) Containers() ContainerManager {
	return e.containers
}

// Swarm returns the Swarm cluster SwarmManager.
func (e *EngineClient) Swarm() SwarmManager {
	return e.swarm
}

// Stacks returns the Compose v2 StackManager.
func (e *EngineClient) Stacks() StackManager {
	return e.stacks
}

// Logs returns the LogStreamer.
func (e *EngineClient) Logs() LogStreamer {
	return e.logs
}
