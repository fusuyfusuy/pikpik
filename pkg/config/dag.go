package config

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// Matches ${VAR_NAME} or $VAR_NAME
	varRegex = regexp.MustCompile(`\$\{([a-zA-Z0-9_]+)\}|\$([a-zA-Z0-9_]+)`)
)

// DAGResolver resolves interdependent environment variables in topological order.
type DAGResolver struct{}

// NewDAGResolver creates a new DAGResolver instance.
func NewDAGResolver() *DAGResolver {
	return &DAGResolver{}
}

// ResolveDAG takes a key-value map and resolves all dependencies topologically.
func (d *DAGResolver) ResolveDAG(vars map[string]string) (map[string]string, error) {
	// 1. Build Adjacency List: key -> dependencies
	adj := make(map[string][]string)
	for k, v := range vars {
		deps := d.extractDependencies(v)
		adj[k] = deps
	}

	// 2. Cycle Detection & Missing Var Detection via 3-color DFS
	const (
		white = 0 // unvisited
		gray  = 1 // visiting
		black = 2 // visited
	)
	colors := make(map[string]int)
	for k := range vars {
		colors[k] = white
	}

	var path []string
	var detectCycle func(node string) error
	detectCycle = func(node string) error {
		colors[node] = gray
		path = append(path, node)

		for _, dep := range adj[node] {
			if _, exists := vars[dep]; !exists {
				return fmt.Errorf("%w: variable '%s' references undefined '%s'", ErrUnresolvedVariable, node, dep)
			}

			if colors[dep] == gray {
				// Cycle detected
				cycleStart := 0
				for i, p := range path {
					if p == dep {
						cycleStart = i
						break
					}
				}
				cyclePath := strings.Join(append(path[cycleStart:], dep), " -> ")
				return fmt.Errorf("%w: %s", ErrCyclicDependency, cyclePath)
			}

			if colors[dep] == white {
				if err := detectCycle(dep); err != nil {
					return err
				}
			}
		}

		path = path[:len(path)-1]
		colors[node] = black
		return nil
	}

	for k := range vars {
		if colors[k] == white {
			if err := detectCycle(k); err != nil {
				return nil, err
			}
		}
	}

	// 3. Topological Evaluation via recursive memoization
	resolved := make(map[string]string)
	var evaluate func(k string) (string, error)
	evaluate = func(k string) (string, error) {
		if val, ok := resolved[k]; ok {
			return val, nil
		}

		rawVal := vars[k]
		// Handle literal $$ escaping
		rawVal = strings.ReplaceAll(rawVal, "$$", "__LITERAL_DOLLAR_PIKPIK__")

		var evalErr error
		expanded := varRegex.ReplaceAllStringFunc(rawVal, func(match string) string {
			depKey := strings.TrimPrefix(match, "$")
			depKey = strings.TrimPrefix(depKey, "{")
			depKey = strings.TrimSuffix(depKey, "}")

			depVal, err := evaluate(depKey)
			if err != nil {
				evalErr = err
				return match
			}
			return depVal
		})

		if evalErr != nil {
			return "", evalErr
		}

		expanded = strings.ReplaceAll(expanded, "__LITERAL_DOLLAR_PIKPIK__", "$")
		resolved[k] = expanded
		return expanded, nil
	}

	for k := range vars {
		if _, err := evaluate(k); err != nil {
			return nil, err
		}
	}

	return resolved, nil
}

func (d *DAGResolver) extractDependencies(val string) []string {
	// Ignore escaped $$
	clean := strings.ReplaceAll(val, "$$", "")
	matches := varRegex.FindAllStringSubmatch(clean, -1)
	var deps []string
	seen := make(map[string]bool)

	for _, m := range matches {
		var dep string
		if m[1] != "" {
			dep = m[1]
		} else {
			dep = m[2]
		}
		if dep != "" && !seen[dep] {
			seen[dep] = true
			deps = append(deps, dep)
		}
	}
	return deps
}
