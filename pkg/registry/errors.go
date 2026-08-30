package registry

import "errors"

var (
	// ErrRegistryNotRunning is returned when operations are attempted on a stopped registry.
	ErrRegistryNotRunning = errors.New("pikpik: embedded registry container is not running")

	// ErrRobotAccountNotFound is returned when a requested robot account cannot be located.
	ErrRobotAccountNotFound = errors.New("pikpik: robot account not found")

	// ErrDuplicateRobotAccount is returned when a duplicate robot account username/project exists.
	ErrDuplicateRobotAccount = errors.New("pikpik: robot account already exists for project")

	// ErrInvalidRegistryConfig is returned when the storage configuration fails validation.
	ErrInvalidRegistryConfig = errors.New("pikpik: invalid registry storage configuration")
)
