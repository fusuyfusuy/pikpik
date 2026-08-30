package orchestration

import (
	"fmt"
	"strings"
)

// ParseConstraint parses and normalizes a single constraint expression.
func ParseConstraint(raw string) (PlacementConstraint, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return PlacementConstraint{}, ErrInvalidConstraintSyntax
	}

	var op string
	var parts []string

	if strings.Contains(trimmed, "==") {
		op = "=="
		parts = strings.SplitN(trimmed, "==", 2)
	} else if strings.Contains(trimmed, "!=") {
		op = "!="
		parts = strings.SplitN(trimmed, "!=", 2)
	} else {
		return PlacementConstraint{}, fmt.Errorf("%w: %s (must contain == or !=)", ErrUnsupportedConstraintOp, raw)
	}

	field := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])

	if field == "" || val == "" {
		return PlacementConstraint{}, fmt.Errorf("%w: empty field or value in '%s'", ErrInvalidConstraintSyntax, raw)
	}

	// Validate supported field patterns
	switch {
	case field == "node.id", field == "node.hostname", field == "node.role":
		if field == "node.role" && val != "manager" && val != "worker" {
			return PlacementConstraint{}, fmt.Errorf("%w: node.role must be 'manager' or 'worker', got '%s'", ErrInvalidConstraintSyntax, val)
		}
	case strings.HasPrefix(field, "node.labels."):
		labelKey := strings.TrimPrefix(field, "node.labels.")
		if labelKey == "" {
			return PlacementConstraint{}, fmt.Errorf("%w: missing label key in '%s'", ErrInvalidConstraintSyntax, field)
		}
	case strings.HasPrefix(field, "engine.labels."):
		labelKey := strings.TrimPrefix(field, "engine.labels.")
		if labelKey == "" {
			return PlacementConstraint{}, fmt.Errorf("%w: missing engine label key in '%s'", ErrInvalidConstraintSyntax, field)
		}
	default:
		return PlacementConstraint{}, fmt.Errorf("%w: '%s'", ErrUnsupportedConstraintField, field)
	}

	return PlacementConstraint{
		Field:    field,
		Operator: op,
		Value:    val,
	}, nil
}

// ParseConstraints parses a slice of raw constraint strings.
func ParseConstraints(raws []string) ([]PlacementConstraint, error) {
	if len(raws) == 0 {
		return nil, nil
	}

	parsed := make([]PlacementConstraint, 0, len(raws))
	for _, raw := range raws {
		c, err := ParseConstraint(raw)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, c)
	}
	return parsed, nil
}

// MatchesNode checks whether a given physical Node satisfies a placement constraint.
func (c PlacementConstraint) MatchesNode(node NodeStatus) bool {
	var actualValue string
	exists := false

	switch {
	case c.Field == "node.id":
		actualValue = node.ID
		exists = true
	case c.Field == "node.hostname":
		actualValue = node.Hostname
		exists = true
	case c.Field == "node.role":
		actualValue = node.Role
		exists = true
	case strings.HasPrefix(c.Field, "node.labels."):
		key := strings.TrimPrefix(c.Field, "node.labels.")
		if node.Labels != nil {
			actualValue, exists = node.Labels[key]
		}
	case strings.HasPrefix(c.Field, "engine.labels."):
		key := strings.TrimPrefix(c.Field, "engine.labels.")
		if node.EngineLabels != nil {
			actualValue, exists = node.EngineLabels[key]
		}
	}

	if c.Operator == "==" {
		return exists && actualValue == c.Value
	} else if c.Operator == "!=" {
		return !exists || actualValue != c.Value
	}
	return false
}

// ValidateConstraintsAgainstNodes verifies that at least one active node in the cluster
// can schedule this service and satisfies requested resource reservations.
func ValidateConstraintsAgainstNodes(constraints []PlacementConstraint, req ResourceRequirements, nodes []NodeStatus) error {
	var candidateNodes []NodeStatus

	for _, node := range nodes {
		if node.State != "ready" || node.Availability != "active" {
			continue
		}

		allMatch := true
		for _, constraint := range constraints {
			if !constraint.MatchesNode(node) {
				allMatch = false
				break
			}
		}

		if allMatch {
			candidateNodes = append(candidateNodes, node)
		}
	}

	if len(candidateNodes) == 0 {
		return ErrNoMatchingNodeAvailable
	}

	// Verify at least one candidate node has sufficient capacity for reservations
	if req.MemoryReserve > 0 || req.CPUReserve > 0 {
		hasCapacity := false
		for _, node := range candidateNodes {
			if (req.MemoryReserve == 0 || node.MemoryBytes >= req.MemoryReserve) &&
				(req.CPUReserve == 0 || node.NanoCPUs >= req.CPUReserve) {
				hasCapacity = true
				break
			}
		}
		if !hasCapacity {
			return ErrResourceCapacityExceeded
		}
	}

	return nil
}
