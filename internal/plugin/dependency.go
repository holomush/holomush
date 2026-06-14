// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"github.com/Masterminds/semver/v3"
	"github.com/samber/oops"
)

// UnsatisfiedDep records one declared dependency the resolver could not satisfy.
type UnsatisfiedDep struct {
	Plugin string
	Entry  Dependency
	Reason string // UNSATISFIED_CAPABILITY | UNSATISFIED_SERVICE | MISDECLARED_DEPENDENCY | VERSION_UNSATISFIED | UNSATISFIED_DEPENDENCY | UNKNOWN_DEPENDENCY_KIND
}

// ResolveResult is the structured output of dependency resolution. A future
// per-plugin quarantine policy reads Unsatisfied/Cycles without a resolver
// rewrite (spec §2).
type ResolveResult struct {
	Ordered     []*DiscoveredPlugin
	Unsatisfied []UnsatisfiedDep
	Cycles      [][]string
	// Grants maps plugin name → the set of dependency tokens (capability tokens
	// like "world.query" and service names) it successfully declared and that
	// resolved. This is the single least-privilege grant authority consumed by
	// both runtimes' delivery shims (INV-PLUGIN-45). A token NOT in a plugin's
	// grant set MUST NOT be wired for that plugin.
	Grants map[string][]string
}

// ResolveDependencyOrder validates and orders the unified dependency graph and
// returns a structured result.
//
// The graph encodes two kinds of dependency edge plus a no-edge capability
// requirement:
//
//  1. Service edges — plugin A requires service S, plugin B provides S →
//     B must load before A.
//  2. Named manifest dependencies — plugin A's Dependencies map lists plugin B →
//     B must load before A.
//  3. Capability requirements — a host-provided capability named in the
//     controlled vocabulary; satisfied by presence in vocab, never an edge.
//
// serverServices lists services the HoloMUSH core itself exposes (e.g.
// holomush.world.v1.WorldService); these satisfy service Requires declarations
// without creating plugin-to-plugin edges. vocab lists the valid host
// capabilities.
//
// A bare Go error is returned ONLY for hard structural faults that make the
// graph un-indexable: DUPLICATE_PLUGIN_NAME and DUPLICATE_SERVICE_PROVIDER.
// Per-entry validation failures (UNSATISFIED_CAPABILITY, UNSATISFIED_SERVICE,
// MISDECLARED_DEPENDENCY, VERSION_UNSATISFIED, UNSATISFIED_DEPENDENCY,
// UNKNOWN_DEPENDENCY_KIND) are reported in res.Unsatisfied, and dependency
// cycles in res.Cycles — not as Go errors.
func ResolveDependencyOrder(plugins []*DiscoveredPlugin, serverServices []string, vocab *CapabilityVocabulary) (*ResolveResult, error) {
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
			// Any prior provider — a peer plugin OR the host ("" sentinel) — is a
			// hard collision. The "" host case is deliberately NOT excused: a
			// plugin declaring Provides of a server-owned service would otherwise
			// silently overwrite the host's ownership below, corrupting load-order
			// edges and broker/service routing (holomush-et5lz). A core service
			// is never a plugin override target; that would be an explicit feature.
			if existing, seen := svcProvider[svc]; seen {
				providerA := existing
				if providerA == "" {
					providerA = "(server)"
				}
				return nil, oops.
					Code("DUPLICATE_SERVICE_PROVIDER").
					With("service", svc).
					With("provider_a", providerA).
					With("provider_b", p.Manifest.Name).
					Errorf("service %q is provided by both %q and %q", svc, providerA, p.Manifest.Name)
			}
			svcProvider[svc] = p.Manifest.Name
		}
	}

	res := &ResolveResult{
		Grants: make(map[string][]string),
	}

	// Per-entry, kind-aware validation. Capability entries add no edge; service
	// entries are checked for a provider (and optional version constraint).
	for _, p := range plugins {
		for _, dep := range p.Manifest.Requires {
			switch dep.Kind {
			case DependencyCapability:
				if !vocab.Has(dep.Name) {
					// A capability entry naming a plugin-provided service is a
					// MISDECLARED kind/provider mismatch (INV-PLUGIN-42) — a hard
					// configuration error reported REGARDLESS of dep.Optional, since
					// optional could otherwise silence the mismatch and skip the
					// required ordering edge (load-order inversion).
					if _, isService := svcProvider[dep.Name]; isService {
						res.Unsatisfied = append(res.Unsatisfied, UnsatisfiedDep{Plugin: p.Manifest.Name, Entry: dep, Reason: "MISDECLARED_DEPENDENCY"})
					} else if !dep.Optional {
						res.Unsatisfied = append(res.Unsatisfied, UnsatisfiedDep{Plugin: p.Manifest.Name, Entry: dep, Reason: "UNSATISFIED_CAPABILITY"})
					}
					// Not granted: vocab miss (whether misdeclared or unsatisfied).
				} else {
					// Capability present in vocab: grant the token.
					res.Grants[p.Manifest.Name] = append(res.Grants[p.Manifest.Name], dep.Name)
				}
			case DependencyService:
				provider, ok := svcProvider[dep.Name]
				if !ok {
					if vocab.Has(dep.Name) {
						// Service entry naming a known host capability: kind/provider
						// mismatch, reported unconditionally (INV-PLUGIN-42).
						res.Unsatisfied = append(res.Unsatisfied, UnsatisfiedDep{Plugin: p.Manifest.Name, Entry: dep, Reason: "MISDECLARED_DEPENDENCY"})
						continue
					}
					if !dep.Optional {
						res.Unsatisfied = append(res.Unsatisfied, UnsatisfiedDep{Plugin: p.Manifest.Name, Entry: dep, Reason: "UNSATISFIED_SERVICE"})
					}
					// No provider: not granted (optional or required).
					continue
				}
				// Version check: when the provider is a plugin (non-empty name) and
				// the dep carries a constraint, verify it. A version mismatch is
				// graceful-degrade for an optional dep (not recorded in Unsatisfied)
				// and a hard fault for a required dep (recorded); NEITHER is granted —
				// the grant set authorizes only valid, version-satisfied resolutions.
				// Server-provided services (provider == "") are exempt: the host
				// does not carry a plugin Manifest.Version, so the constraint cannot
				// be evaluated and the dep is always considered satisfied.
				if provider != "" && dep.Version != "" {
					if !versionSatisfies(byName[provider].Manifest.Version, dep.Version) {
						if !dep.Optional {
							res.Unsatisfied = append(res.Unsatisfied, UnsatisfiedDep{Plugin: p.Manifest.Name, Entry: dep, Reason: "VERSION_UNSATISFIED"})
						}
						// Version constraint failed: not granted (optional or required).
						continue
					}
				}
				// Provider found and version satisfied (or no constraint, or
				// server-provided): grant the service name.
				res.Grants[p.Manifest.Name] = append(res.Grants[p.Manifest.Name], dep.Name)
			default:
				// Zero-value or unknown DependencyKind (e.g. a Go-constructed
				// Dependency{Name:"x"} with empty Kind). A required dependency of
				// an unrecognized kind MUST be reported, never silently dropped
				// (INV-PLUGIN-41).
				if !dep.Optional {
					res.Unsatisfied = append(res.Unsatisfied, UnsatisfiedDep{Plugin: p.Manifest.Name, Entry: dep, Reason: "UNKNOWN_DEPENDENCY_KIND"})
				}
			}
		}

		// Named manifest dependencies (the Dependencies map: plugin A names plugin
		// B). A named dependency not discovered in byName MUST be reported, never
		// silently dropped (INV-PLUGIN-41) — the edge-build loop below only adds an
		// edge for the satisfied case.
		for dep := range p.Manifest.Dependencies {
			if _, ok := byName[dep]; !ok {
				res.Unsatisfied = append(res.Unsatisfied, UnsatisfiedDep{
					Plugin: p.Manifest.Name,
					Entry:  Dependency{Kind: DependencyService, Name: dep},
					Reason: "UNSATISFIED_DEPENDENCY",
				})
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
		// Service edges (capability requirements add no edge).
		for _, svc := range p.Manifest.RequiredServiceNames() {
			providerName, ok := svcProvider[svc]
			if !ok || providerName == "" {
				// Unsatisfied or server-provided; no plugin edge.
				continue
			}
			addEdge(providerName, p.Manifest.Name)
		}
		// Named manifest dependency edges.
		for dep := range p.Manifest.Dependencies {
			if _, ok := byName[dep]; ok {
				addEdge(dep, p.Manifest.Name)
			}
		}
	}

	// Kahn's algorithm: start with zero-in-degree nodes.
	queue := make([]string, 0, len(plugins))
	for _, p := range plugins {
		if inDegree[p.Manifest.Name] == 0 {
			queue = append(queue, p.Manifest.Name)
		}
	}

	ordered := make([]*DiscoveredPlugin, 0, len(plugins))
	for len(queue) > 0 {
		// Pop front.
		name := queue[0]
		queue = queue[1:]
		ordered = append(ordered, byName[name])

		for _, consumer := range dependents[name] {
			inDegree[consumer]--
			if inDegree[consumer] == 0 {
				queue = append(queue, consumer)
			}
		}
	}

	if len(ordered) != len(plugins) {
		// Collect names still in the cycle.
		var cycleNames []string
		for name, deg := range inDegree {
			if deg > 0 {
				cycleNames = append(cycleNames, name)
			}
		}
		res.Cycles = append(res.Cycles, cycleNames)
		return res, nil
	}

	res.Ordered = ordered
	return res, nil
}

// versionSatisfies reports whether version satisfies the semver constraint.
// A malformed version or constraint is treated as not satisfying (fail-closed).
func versionSatisfies(version, constraint string) bool {
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return false
	}
	v, err := semver.NewVersion(version)
	if err != nil {
		return false
	}
	return c.Check(v)
}
