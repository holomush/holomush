// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const runYAML = `
scopes:
  - name: INV-PRESENCE
    description: "Presence snapshot correctness"
    boundary: "Current-state presence queries."
`

func writeRunFixtures(t *testing.T, mdBody string) (yamlPath, mdPath string) {
	t.Helper()
	dir := t.TempDir()
	yamlPath = filepath.Join(dir, "invariants.yaml")
	mdPath = filepath.Join(dir, "invariants.md")
	if err := os.WriteFile(yamlPath, []byte(runYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	md := "# Title\n\n## Scope index\n\n" +
		"<!-- BEGIN GENERATED: scope-index -->\n" + mdBody + "\n<!-- END GENERATED: scope-index -->\n\n" +
		"## Invariant tables\n\n" +
		"<!-- BEGIN GENERATED: invariant-tables -->\n_No invariants migrated yet — populated per-scope by the holomush-hz0v4.14 migration._\n<!-- END GENERATED: invariant-tables -->\n"
	if err := os.WriteFile(mdPath, []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}
	return yamlPath, mdPath
}

func TestRunWriteThenCheckRoundTrips(t *testing.T) {
	yamlPath, mdPath := writeRunFixtures(t, "STALE")

	// First write renders the stale region.
	if err := run(yamlPath, mdPath, false); err != nil {
		t.Fatalf("run(write): %v", err)
	}
	got, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "STALE") {
		t.Errorf("write did not refresh generated region:\n%s", got)
	}
	if !strings.Contains(string(got), "| `INV-PRESENCE` | Presence snapshot correctness | Current-state presence queries. |") {
		t.Errorf("write did not render scope row:\n%s", got)
	}

	// -check now passes (file is in sync).
	if err := run(yamlPath, mdPath, true); err != nil {
		t.Errorf("run(check) on fresh file = %v, want nil", err)
	}
}

func TestRunCheckFailsOnStaleMarkdown(t *testing.T) {
	yamlPath, mdPath := writeRunFixtures(t, "STALE")
	err := run(yamlPath, mdPath, true)
	if err == nil {
		t.Fatal("run(check) on stale file = nil, want drift error")
	}
	if !strings.Contains(err.Error(), "out of date") {
		t.Errorf("error = %q, want 'out of date'", err)
	}
	// -check must NOT mutate the file.
	got, _ := os.ReadFile(mdPath)
	if !strings.Contains(string(got), "STALE") {
		t.Errorf("-check mutated the file; it must be read-only")
	}
}

func TestRunErrorsOnMissingRegistry(t *testing.T) {
	_, mdPath := writeRunFixtures(t, "x")
	if err := run(filepath.Join(t.TempDir(), "nope.yaml"), mdPath, true); err == nil {
		t.Fatal("expected error for missing registry")
	}
}

func TestRunErrorsOnMissingMarkdown(t *testing.T) {
	yamlPath, _ := writeRunFixtures(t, "x")
	if err := run(yamlPath, filepath.Join(t.TempDir(), "nope.md"), true); err == nil {
		t.Fatal("expected error for missing markdown")
	}
}

func TestRunErrorsOnMissingMarkerInMarkdown(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "invariants.yaml")
	mdPath := filepath.Join(dir, "invariants.md")
	if err := os.WriteFile(yamlPath, []byte(runYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mdPath, []byte("# Title\n\nno markers here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(yamlPath, mdPath, false); err == nil {
		t.Fatal("expected error for markdown without generated markers")
	}
}
