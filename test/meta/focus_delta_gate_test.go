// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is provided by findRepoRoot in inv_binding_test.go (same package).

func nonTestGoFilesContaining(t *testing.T, root, needle string) []string {
	t.Helper()
	var hits []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path) //nolint:gosec // G122: meta-test walker reads source files for invariant grep; no symlink concern
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(b), needle) {
			rel, _ := filepath.Rel(root, path)
			hits = append(hits, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

// INV-SCENE-38: per-connection focus-delta delivery is driven ONLY inside
// internal/grpc/focus. The interface decl + the registry impl/adapter in
// internal/grpc are the only other legitimate occurrences.
func TestSendToConnectionConfinedToFocusAndRegistry(t *testing.T) {
	root := findRepoRoot(t)
	allowed := map[string]bool{
		"internal/grpc/stream_registry.go":           true, // impl + ConnectionSenderAdapter
		"internal/grpc/focus/subscription_router.go": true, // ConnectionSender interface decl
		"internal/grpc/focus/focus_delta.go":         true, // the sole driver (the gate)
	}
	for _, f := range nonTestGoFilesContaining(t, root, "SendToConnection(") {
		if !allowed[f] {
			t.Errorf("INV-SCENE-38 violation: SendToConnection( used outside the focus gate: %s", f)
		}
	}
}

// INV-SCENE-41: the registry-derived adapter pair is assembled ONLY in the
// FocusStreamCoordinatorOptions helper (constructors are defined in
// stream_registry.go).
func TestFocusAdapterPairAssembledOnlyInHelper(t *testing.T) {
	root := findRepoRoot(t)
	allowed := map[string]bool{
		"internal/grpc/stream_registry.go": true, // constructor definitions
		"internal/grpc/focus_wiring.go":    true, // the single assembly point
	}
	for _, needle := range []string{"NewConnectionSenderAdapter(", "NewStreamSenderAdapter("} {
		for _, f := range nonTestGoFilesContaining(t, root, needle) {
			if !allowed[f] {
				t.Errorf("INV-SCENE-41 violation: %s used outside FocusStreamCoordinatorOptions: %s", needle, f)
			}
		}
	}
}
