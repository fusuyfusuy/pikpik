package orchestration_test

import (
	"testing"

	"github.com/fusuycorp/pikpik/pkg/orchestration"
)

// TestPlacementConstraintParser tests the formal constraint parser.
func TestPlacementConstraintParser(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    orchestration.PlacementConstraint
		expectError bool
	}{
		{
			name:  "valid node.role equality",
			input: "node.role == worker",
			expected: orchestration.PlacementConstraint{
				Field:    "node.role",
				Operator: "==",
				Value:    "worker",
			},
			expectError: false,
		},
		{
			name:  "valid node.role manager equality",
			input: "node.role == manager",
			expected: orchestration.PlacementConstraint{
				Field:    "node.role",
				Operator: "==",
				Value:    "manager",
			},
			expectError: false,
		},
		{
			name:  "valid node.labels inequality",
			input: "node.labels.storage != hdd",
			expected: orchestration.PlacementConstraint{
				Field:    "node.labels.storage",
				Operator: "!=",
				Value:    "hdd",
			},
			expectError: false,
		},
		{
			name:  "valid node.hostname equality",
			input: "node.hostname == server-alpha-02",
			expected: orchestration.PlacementConstraint{
				Field:    "node.hostname",
				Operator: "==",
				Value:    "server-alpha-02",
			},
			expectError: false,
		},
		{
			name:  "valid node.id equality",
			input: "node.id == 2ivku778",
			expected: orchestration.PlacementConstraint{
				Field:    "node.id",
				Operator: "==",
				Value:    "2ivku778",
			},
			expectError: false,
		},
		{
			name:  "valid engine.labels equality",
			input: "engine.labels.operatingsystem == linux",
			expected: orchestration.PlacementConstraint{
				Field:    "engine.labels.operatingsystem",
				Operator: "==",
				Value:    "linux",
			},
			expectError: false,
		},
		{
			name:        "invalid empty input",
			input:       "",
			expectError: true,
		},
		{
			name:        "invalid operator",
			input:       "node.role >= manager",
			expectError: true,
		},
		{
			name:        "invalid empty value",
			input:       "node.labels.zone ==",
			expectError: true,
		},
		{
			name:        "invalid empty field",
			input:       "== worker",
			expectError: true,
		},
		{
			name:        "invalid role value",
			input:       "node.role == master",
			expectError: true,
		},
		{
			name:        "invalid empty label key",
			input:       "node.labels. == us-east",
			expectError: true,
		},
		{
			name:        "invalid empty engine label key",
			input:       "engine.labels. == linux",
			expectError: true,
		},
		{
			name:        "unsupported field prefix",
			input:       "custom.field == value",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := orchestration.ParseConstraint(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error for input '%s', got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error for input '%s': %v", tt.input, err)
			}

			if result.Field != tt.expected.Field || result.Operator != tt.expected.Operator || result.Value != tt.expected.Value {
				t.Errorf("expected %+v, got %+v", tt.expected, result)
			}
		})
	}
}

// TestParseConstraintsSlice tests parsing multiple constraint strings.
func TestParseConstraintsSlice(t *testing.T) {
	raws := []string{
		"node.role == worker",
		"node.labels.zone == us-east-1",
	}

	parsed, err := orchestration.ParseConstraints(raws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 parsed constraints, got %d", len(parsed))
	}

	// Empty slice test
	empty, err := orchestration.ParseConstraints(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected nil slice, got %v (err: %v)", empty, err)
	}

	// Invalid slice element test
	_, err = orchestration.ParseConstraints([]string{"invalid constraint"})
	if err == nil {
		t.Fatalf("expected error for invalid constraint slice")
	}
}

// TestValidateConstraintsAgainstNodes validates multi-node cluster scheduling logic.
func TestValidateConstraintsAgainstNodes(t *testing.T) {
	nodes := []orchestration.NodeStatus{
		{
			ID:           "node-1",
			Hostname:     "gateway-01",
			Role:         "manager",
			State:        "ready",
			Availability: "active",
			Labels:       map[string]string{"tier": "gateway"},
			NanoCPUs:     4 * 1e9,
			MemoryBytes:  8 * 1024 * 1024 * 1024,
		},
		{
			ID:           "node-2",
			Hostname:     "worker-01",
			Role:         "worker",
			State:        "ready",
			Availability: "active",
			Labels:       map[string]string{"tier": "compute", "zone": "us-east-1a"},
			NanoCPUs:     8 * 1e9,
			MemoryBytes:  16 * 1024 * 1024 * 1024,
		},
		{
			ID:           "node-3",
			Hostname:     "worker-02-paused",
			Role:         "worker",
			State:        "ready",
			Availability: "pause",
			Labels:       map[string]string{"tier": "compute", "zone": "us-east-1a"},
			NanoCPUs:     8 * 1e9,
			MemoryBytes:  16 * 1024 * 1024 * 1024,
		},
	}

	// Test 1: Worker constraint matches node-2
	c1, _ := orchestration.ParseConstraint("node.role == worker")
	err := orchestration.ValidateConstraintsAgainstNodes([]orchestration.PlacementConstraint{c1}, orchestration.ResourceRequirements{}, nodes)
	if err != nil {
		t.Fatalf("expected node-2 to match worker constraint, got: %v", err)
	}

	// Test 2: Unmatched label fails fast
	c2, _ := orchestration.ParseConstraint("node.labels.zone == us-west-2b")
	err = orchestration.ValidateConstraintsAgainstNodes([]orchestration.PlacementConstraint{c2}, orchestration.ResourceRequirements{}, nodes)
	if err != orchestration.ErrNoMatchingNodeAvailable {
		t.Fatalf("expected ErrNoMatchingNodeAvailable, got: %v", err)
	}

	// Test 3: Resource reservation exceeding worker RAM
	req := orchestration.ResourceRequirements{MemoryReserve: 32 * 1024 * 1024 * 1024} // 32GB requested, max node has 16GB
	err = orchestration.ValidateConstraintsAgainstNodes([]orchestration.PlacementConstraint{c1}, req, nodes)
	if err != orchestration.ErrResourceCapacityExceeded {
		t.Fatalf("expected ErrResourceCapacityExceeded, got: %v", err)
	}

	// Test 4: Resource reservation satisfied
	reqOK := orchestration.ResourceRequirements{MemoryReserve: 8 * 1024 * 1024 * 1024, CPUReserve: 2 * 1e9}
	err = orchestration.ValidateConstraintsAgainstNodes([]orchestration.PlacementConstraint{c1}, reqOK, nodes)
	if err != nil {
		t.Fatalf("expected resource reservation to pass, got: %v", err)
	}

	// Test 5: Matches node ID and Hostname
	cID, _ := orchestration.ParseConstraint("node.id == node-1")
	if !cID.MatchesNode(nodes[0]) {
		t.Errorf("expected node-1 to match node.id constraint")
	}
	cHost, _ := orchestration.ParseConstraint("node.hostname != gateway-01")
	if cHost.MatchesNode(nodes[0]) {
		t.Errorf("expected node-1 to NOT match != hostname constraint")
	}
	if !cHost.MatchesNode(nodes[1]) {
		t.Errorf("expected node-2 to match != hostname constraint")
	}
}
