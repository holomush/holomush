// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
package main

import (
	"strings"
	"testing"

	"github.com/holomush/holomush/internal/invregistry"
)

const markedTemplate = `# Title

intro prose (hand-authored, must survive)

## Scope index

<!-- BEGIN GENERATED: scope-index (edit invariants.yaml) -->
STALE SCOPE CONTENT
<!-- END GENERATED: scope-index -->

prose between regions (must survive)

## Invariant tables

<!-- BEGIN GENERATED: invariant-tables -->
STALE TABLE CONTENT
<!-- END GENERATED: invariant-tables -->

trailing prose (must survive)
`

func sampleDoc() invregistry.Doc {
	return invregistry.Doc{
		Scopes: []invregistry.Scope{
			{Name: "INV-PRESENCE", Description: "Presence snapshot correctness", Boundary: "Current-state presence."},
			{Name: "INV-SESSION", Description: "Session lifecycle", Boundary: "Session state machine."},
		},
		Invariants: []invregistry.Entry{
			{
				ID: "INV-PRESENCE-1", Scope: "INV-PRESENCE", Summary: "snapshot enumerates active sessions",
				Legacy: []string{"I-PRES-1@docs/x.md"}, Binding: "pending",
			},
			{
				ID: "INV-PRESENCE-2", Scope: "INV-PRESENCE", Summary: "exempt from temporal floor",
				Legacy: []string{"I-PRES-2@docs/x.md"}, Binding: "pending",
			},
		},
	}
}

func TestRenderFillsGeneratedRegionsAndPreservesProse(t *testing.T) {
	out, err := Render(sampleDoc(), markedTemplate)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"intro prose (hand-authored, must survive)",
		"prose between regions (must survive)",
		"trailing prose (must survive)",
		"| `INV-PRESENCE` | Presence snapshot correctness | Current-state presence. |",
		"| `INV-SESSION` | Session lifecycle | Session state machine. |",
		"### `INV-PRESENCE`",
		"| `INV-PRESENCE-1` | snapshot enumerates active sessions | `I-PRES-1` | pending |",
		"| `INV-PRESENCE-2` | exempt from temporal floor | `I-PRES-2` | pending |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q\n--- output ---\n%s", want, out)
		}
	}
	if strings.Contains(out, "STALE") {
		t.Errorf("stale generated content not replaced:\n%s", out)
	}
	// INV-SESSION has no invariants → no table section for it.
	if strings.Contains(out, "### `INV-SESSION`") {
		t.Errorf("scope with no invariants should not get an invariant table")
	}
}

func TestRenderIsIdempotent(t *testing.T) {
	once, err := Render(sampleDoc(), markedTemplate)
	if err != nil {
		t.Fatalf("first Render: %v", err)
	}
	twice, err := Render(sampleDoc(), once)
	if err != nil {
		t.Fatalf("second Render: %v", err)
	}
	if once != twice {
		t.Errorf("Render not idempotent; second pass differs:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

func TestRenderEmptyInvariantsEmitsPlaceholder(t *testing.T) {
	doc := invregistry.Doc{Scopes: []invregistry.Scope{{Name: "INV-PRESENCE", Description: "d", Boundary: "b"}}}
	out, err := Render(doc, markedTemplate)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "_No invariants migrated yet") {
		t.Errorf("expected placeholder for empty invariants, got:\n%s", out)
	}
}

func TestRenderFailsLoudOnUndeclaredScope(t *testing.T) {
	// id prefix is a declared scope, but the scope FIELD is a typo — the
	// meta-test (which keys off the id prefix) would not catch it, so the
	// renderer must.
	doc := invregistry.Doc{
		Scopes: []invregistry.Scope{{Name: "INV-PRESENCE", Description: "d", Boundary: "b"}},
		Invariants: []invregistry.Entry{
			{ID: "INV-PRESENCE-1", Scope: "INV-PRESNCE", Summary: "typo'd scope field", Binding: "pending"},
		},
	}
	_, err := Render(doc, markedTemplate)
	if err == nil {
		t.Fatal("expected error for invariant with undeclared scope, got nil")
	}
	if !strings.Contains(err.Error(), "undeclared scope") {
		t.Errorf("error = %q, want mention of undeclared scope", err)
	}
}

func TestRenderFailsLoudOnMissingMarker(t *testing.T) {
	noEnd := strings.ReplaceAll(markedTemplate, "<!-- END GENERATED: scope-index -->", "")
	if _, err := Render(sampleDoc(), noEnd); err == nil {
		t.Fatal("expected error for missing END marker, got nil")
	}
	noBegin := strings.ReplaceAll(markedTemplate, "<!-- BEGIN GENERATED: invariant-tables -->", "")
	if _, err := Render(sampleDoc(), noBegin); err == nil {
		t.Fatal("expected error for missing BEGIN marker, got nil")
	}
}

func TestCellEscapesPipesAndCollapsesWhitespace(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"collapses folded-scalar newlines", "a\n  b\tc", "a b c"},
		{"escapes pipe", "x | y", `x \| y`},
		{"trims", "  hi  ", "hi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cell(tt.in); got != tt.want {
				t.Errorf("cell(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLegacyCellStripsOriginSpecSuffix(t *testing.T) {
	got := legacyCell([]string{"I-PRES-1@docs/superpowers/specs/x.md", "INV-3@docs/y.md"})
	if got != "`I-PRES-1`, `INV-3`" {
		t.Errorf("legacyCell = %q", got)
	}
	if got := legacyCell(nil); got != "—" {
		t.Errorf("empty legacyCell = %q, want em dash", got)
	}
}
