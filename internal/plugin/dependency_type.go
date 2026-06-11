// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"github.com/samber/oops"
	"gopkg.in/yaml.v3"
)

// DependencyKind distinguishes a host-provided capability (no DAG edge) from a
// plugin-or-host-provided gRPC service (provider-before-consumer edge).
type DependencyKind string

const (
	// DependencyCapability is a host-provided capability named in the controlled
	// capability vocabulary (short name, e.g. "world.query").
	DependencyCapability DependencyKind = "capability"
	// DependencyService is a gRPC service contract named by its full proto path.
	DependencyService DependencyKind = "service"
)

// Dependency is one typed entry in a manifest's requires list (spec §1).
type Dependency struct {
	Kind     DependencyKind
	Name     string
	Version  string // semver constraint; services only
	Optional bool
	// Scope carries least-privilege parameters; semantics are sub-spec 4. The
	// foundation parses and round-trips it but does not interpret it.
	Scope string
}

// dependencyYAML is the object form accepted by UnmarshalYAML.
type dependencyYAML struct {
	Capability string `yaml:"capability"`
	Service    string `yaml:"service"`
	Version    string `yaml:"version"`
	Optional   bool   `yaml:"optional"`
	Scope      string `yaml:"scope"`
}

// UnmarshalYAML accepts either a bare string (treated as a service path, for
// backward compatibility with the legacy flat-string requires form) or a typed
// object with exactly one of capability/service.
func (d *Dependency) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		d.Kind = DependencyService
		d.Name = value.Value
		return nil
	}
	var raw dependencyYAML
	if err := value.Decode(&raw); err != nil {
		return oops.Code("DEPENDENCY_MALFORMED").Wrap(err)
	}
	hasCap, hasSvc := raw.Capability != "", raw.Service != ""
	if hasCap == hasSvc {
		return oops.Code("DEPENDENCY_KIND_AMBIGUOUS").
			Errorf("a requires entry MUST have exactly one of capability/service")
	}
	d.Version, d.Optional, d.Scope = raw.Version, raw.Optional, raw.Scope
	if hasCap {
		d.Kind, d.Name = DependencyCapability, raw.Capability
	} else {
		d.Kind, d.Name = DependencyService, raw.Service
	}
	return nil
}

// RequireServices builds a []Dependency of service-kind entries. It exists so
// that test fixtures (and any caller constructing a manifest in Go) can migrate
// from the old []string{...} form with minimal churn.
func RequireServices(names ...string) []Dependency {
	out := make([]Dependency, 0, len(names))
	for _, n := range names {
		out = append(out, Dependency{Kind: DependencyService, Name: n})
	}
	return out
}
