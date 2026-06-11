// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"fmt"
	"strings"

	"github.com/samber/oops"
)

// ResolveDependencyOrder returns plugins sorted so that providers load before
// consumers. It uses Kahn's algorithm on a DAG whose edges encode two kinds of
// dependency:
//
//  1. Service edges — plugin A requires service S, plugin B provides S →
//     B must load before A.
//  2. Named manifest dependencies — plugin A's Dependencies map lists plugin B →
//     B must load before A.
//
// serverServices lists services the HoloMUSH core itself exposes; these satisfy
// Requires declarations without creating plugin-to-plugin edges.
//
// Errors:
//   - DUPLICATE_SERVICE_PROVIDER — two plugins declare the same Provides entry
//   - UNSATISFIED_REQUIRES — required service not provided by any plugin or server
//   - UNSATISFIED_DEPENDENCY — named plugin in Dependencies not discovered
//   - CIRCULAR_DEPENDENCY — a cycle exists in the dependency graph
func ResolveDependencyOrder(plugins []*DiscoveredPlugin, serverServices []string) ([]*DiscoveredPlugin, error) {
	// Index plugins by name for fast lookup.
	byName := make(map[string]*DiscoveredPlugin, len(plugins))
	for _, p := range plugins {
		if existing, ok := byName[p.Manifest.Name]; ok {
			return nil, oops.
				Code("DUPLICATE_PLUGIN_NAME").
				With("plugin", p.Manifest.Name).
				With("dir_a", existing.Dir).
				With("dir_b", p.Dir).
				Errorf("plugin %q is declared by both %q and %q", p.Manifest.Name, existing.Dir, p.Dir)
		}
		byName[p.Manifest.Name] = p
	}

	// Build service → provider map.
	// "" as value means the server provides it (no edge needed).
	svcProvider := make(map[string]string)
	for _, svc := range serverServices {
		svcProvider[svc] = ""
	}
	for _, p := range plugins {
		for _, svc := range p.Manifest.Provides {
			if existing, seen := svcProvider[svc]; seen && existing != "" {
				return nil, oops.
					Code("DUPLICATE_SERVICE_PROVIDER").
					With("service", svc).
					With("provider_a", existing).
					With("provider_b", p.Manifest.Name).
					Errorf("service %q is provided by both %q and %q", svc, existing, p.Manifest.Name)
			}
			svcProvider[svc] = p.Manifest.Name
		}
	}

	// Validate all Requires are satisfiable.
	for _, p := range plugins {
		for _, svc := range p.Manifest.RequiredServiceNames() {
			if _, ok := svcProvider[svc]; !ok {
				return nil, oops.
					Code("UNSATISFIED_REQUIRES").
					With("plugin", p.Manifest.Name).
					With("service", svc).
					Errorf("plugin %q requires service %q which is not provided by any plugin or server", p.Manifest.Name, svc)
			}
		}
	}

	// Validate all named Dependencies refer to discovered plugins.
	for _, p := range plugins {
		for dep := range p.Manifest.Dependencies {
			if _, ok := byName[dep]; !ok {
				return nil, oops.
					Code("UNSATISFIED_DEPENDENCY").
					With("plugin", p.Manifest.Name).
					With("dependency", dep).
					Errorf("plugin %q depends on %q which is not discovered", p.Manifest.Name, dep)
			}
		}
	}

	// Build adjacency list (edges: prerequisite → dependents) and in-degree map.
	// An edge X→Y means X must load before Y (i.e., Y depends on X).
	inDegree := make(map[string]int, len(plugins))
	dependents := make(map[string][]string, len(plugins)) // prerequisite → []consumer
	for _, p := range plugins {
		if _, exists := inDegree[p.Manifest.Name]; !exists {
			inDegree[p.Manifest.Name] = 0
		}
	}

	addEdge := func(prereq, consumer string) {
		dependents[prereq] = append(dependents[prereq], consumer)
		inDegree[consumer]++
	}

	for _, p := range plugins {
		// Service edges.
		for _, svc := range p.Manifest.RequiredServiceNames() {
			providerName := svcProvider[svc]
			if providerName == "" {
				// Server-provided; no plugin edge needed.
				continue
			}
			addEdge(providerName, p.Manifest.Name)
		}
		// Named manifest dependency edges.
		for dep := range p.Manifest.Dependencies {
			addEdge(dep, p.Manifest.Name)
		}
	}

	// Kahn's algorithm: start with zero-in-degree nodes.
	queue := make([]string, 0, len(plugins))
	for _, p := range plugins {
		if inDegree[p.Manifest.Name] == 0 {
			queue = append(queue, p.Manifest.Name)
		}
	}

	result := make([]*DiscoveredPlugin, 0, len(plugins))
	for len(queue) > 0 {
		// Pop front.
		name := queue[0]
		queue = queue[1:]
		result = append(result, byName[name])

		for _, consumer := range dependents[name] {
			inDegree[consumer]--
			if inDegree[consumer] == 0 {
				queue = append(queue, consumer)
			}
		}
	}

	if len(result) != len(plugins) {
		// Collect names still in the cycle for a helpful error message.
		var cycleNames []string
		for name, deg := range inDegree {
			if deg > 0 {
				cycleNames = append(cycleNames, name)
			}
		}
		return nil, oops.
			Code("CIRCULAR_DEPENDENCY").
			With("plugins", strings.Join(cycleNames, ", ")).
			Errorf("circular dependency detected among plugins: %s", fmt.Sprintf("[%s]", strings.Join(cycleNames, ", ")))
	}

	return result, nil
}
