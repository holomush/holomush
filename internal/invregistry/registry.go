// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package invregistry defines the canonical schema for the invariant registry
// (docs/architecture/invariants.yaml) and a loader for it. It is the single
// Go definition of the registry shape, shared by the registry tooling
// (cmd/inv-migrate, cmd/inv-render) so the schema cannot drift between tools.
//
// The human-readable companion (docs/architecture/invariants.md) is rendered
// from the YAML by cmd/inv-render; it is never hand-edited inside the generated
// regions and never parsed back (the old dual-parse consistency lint is gone).
package invregistry

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Ref is a path-anchored annotation site: a file plus the ID token to anchor
// on. Never a line number — line numbers drift between classification and
// migration.
type Ref struct {
	File  string `yaml:"file"`
	Token string `yaml:"token"`
}

// Entry mirrors one invariant in invariants.yaml.
type Entry struct {
	ID         string   `yaml:"id"`
	Scope      string   `yaml:"scope"`
	OriginSpec string   `yaml:"origin_spec"`
	Legacy     []string `yaml:"legacy"`
	Summary    string   `yaml:"summary"`
	Severity   string   `yaml:"severity"`
	Status     string   `yaml:"status"`
	AssertedBy []string `yaml:"asserted_by"`
	External   bool     `yaml:"external"`
	Binding    string   `yaml:"binding"`
	Refs       []Ref    `yaml:"refs"`
}

// Scope mirrors one scope record in invariants.yaml. Status is "pending" until
// the scope's refs have been migrated to canonical INV-<SCOPE>-N form, then
// "migrated".
type Scope struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Boundary    string   `yaml:"boundary"`
	Status      string   `yaml:"status"` // pending | migrated
	OriginSpecs []string `yaml:"origin_specs"`
	OwnedPaths  []string `yaml:"owned_paths"`  // path globs; MAY target individual files
	SharedFiles []string `yaml:"shared_files"` // exact paths annotating >1 scope
}

// Doc is the whole registry document.
type Doc struct {
	Scopes     []Scope `yaml:"scopes"`
	Invariants []Entry `yaml:"invariants"`
}

// Load reads and parses the registry YAML at path.
func Load(path string) (Doc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Doc{}, fmt.Errorf("read registry %s: %w", path, err)
	}
	var d Doc
	if err := yaml.Unmarshal(data, &d); err != nil {
		return Doc{}, fmt.Errorf("parse registry %s: %w", path, err)
	}
	return d, nil
}
