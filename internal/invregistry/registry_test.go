// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
package invregistry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParsesScopesAndInvariants(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invariants.yaml")
	const fixture = `
scopes:
  - name: INV-PRESENCE
    description: "Presence snapshot correctness"
    boundary: "Current-state presence queries."
    status: migrated
    origin_specs: ["docs/x.md"]
    owned_paths: ["internal/grpc/list_focus_presence*.go"]
    shared_files: ["internal/testsupport/integrationtest/harness.go"]
invariants:
  - id: INV-PRESENCE-1
    scope: INV-PRESENCE
    summary: "snapshot enumerates active sessions"
    legacy: ["I-PRES-1@docs/x.md"]
    binding: pending
    refs:
      - {file: "internal/grpc/list_focus_presence.go", token: "I-PRES-1"}
`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(doc.Scopes) != 1 || doc.Scopes[0].Name != "INV-PRESENCE" {
		t.Fatalf("scopes = %+v", doc.Scopes)
	}
	if got := doc.Scopes[0].OwnedPaths; len(got) != 1 || got[0] != "internal/grpc/list_focus_presence*.go" {
		t.Errorf("owned_paths = %v", got)
	}
	if len(doc.Invariants) != 1 {
		t.Fatalf("want 1 invariant, got %d", len(doc.Invariants))
	}
	e := doc.Invariants[0]
	if e.ID != "INV-PRESENCE-1" || e.Scope != "INV-PRESENCE" || e.Binding != "pending" {
		t.Errorf("entry = %+v", e)
	}
	if len(e.Refs) != 1 || e.Refs[0].Token != "I-PRES-1" {
		t.Errorf("refs = %+v", e.Refs)
	}
}

func TestLoadErrorsOnMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadErrorsOnMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("scopes: [this: is: not: valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}
