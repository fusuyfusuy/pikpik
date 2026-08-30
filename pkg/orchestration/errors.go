package orchestration

import (
	"errors"
)

var (
	// ErrSocketUnreachable is returned when the Docker daemon socket cannot be reached or is unresponsive.
	ErrSocketUnreachable = errors.New("orchestrator: docker socket unreachable")

	// ErrNotSwarmLeader is returned when a cluster management operation is attempted on a non-leader node.
	ErrNotSwarmLeader = errors.New("orchestrator: operation requires swarm manager leader")

	// ErrInvalidConstraintSyntax is returned when placement constraint expression fails validation.
	ErrInvalidConstraintSyntax = errors.New("orchestrator: invalid placement constraint syntax")

	// ErrUnsupportedConstraintOp is returned when an operator other than == or != is used in a constraint.
	ErrUnsupportedConstraintOp = errors.New("orchestrator: unsupported constraint operator (only == and != supported)")

	// ErrUnsupportedConstraintField is returned when a constraint uses an unknown field prefix.
	ErrUnsupportedConstraintField = errors.New("orchestrator: unsupported constraint field prefix")

	// ErrNoMatchingNodeAvailable is returned when zero active cluster nodes satisfy placement constraints.
	ErrNoMatchingNodeAvailable = errors.New("orchestrator: no active cluster nodes satisfy placement constraints")

	// ErrResourceCapacityExceeded is returned when requested CPU/RAM reservations exceed node capacity.
	ErrResourceCapacityExceeded = errors.New("orchestrator: cluster nodes have insufficient CPU or Memory capacity")

	// ErrContainerHealthTimeout is returned when a container fails the health probation window during rolling updates.
	ErrContainerHealthTimeout = errors.New("orchestrator: container health check timed out")

	// ErrServiceNotFound is returned when a requested Swarm service does not exist.
	ErrServiceNotFound = errors.New("orchestrator: swarm service not found")

	// ErrContainerNotFound is returned when a requested container does not exist.
	ErrContainerNotFound = errors.New("orchestrator: container not found")

	// ErrStackNotFound is returned when a requested Compose stack does not exist.
	ErrStackNotFound = errors.New("orchestrator: stack not found")

	// ErrInvalidStreamHeader is returned when a Docker multiplexed stream header is malformed.
	ErrInvalidStreamHeader = errors.New("orchestrator: invalid docker multiplexed stream header")

	// ErrCyclicDependency is returned when a Compose v2 stack contains circular service dependencies.
	ErrCyclicDependency = errors.New("orchestrator: cyclic dependency detected in compose services")

	// ErrUnknownDependency is returned when a Compose service references an unknown dependency.
	ErrUnknownDependency = errors.New("orchestrator: unknown service dependency")
)
