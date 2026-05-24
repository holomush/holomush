// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package logging_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// TestINV_L1_LoggingHasNoSentryImport asserts internal/logging never imports
// sentry-go, directly OR transitively. The vendor-neutral boundary: only
// internal/telemetry may touch the Sentry SDK, so an indirect pull (e.g.
// logging → telemetry → sentry-go) is also a violation. (INV-L1)
func TestINV_L1_LoggingHasNoSentryImport(t *testing.T) { // INV-L1
	cfg := &packages.Config{Mode: packages.NeedImports | packages.NeedDeps | packages.NeedName}
	pkgs, err := packages.Load(cfg, "github.com/holomush/holomush/internal/logging")
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)

	// Walk the full transitive dependency graph. NeedDeps populates each
	// imported *Package's own Imports, so BFS from the root reaches every
	// dependency, not just direct imports.
	seen := make(map[string]bool)
	var queue []*packages.Package
	queue = append(queue, pkgs...)
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if seen[p.PkgPath] {
			continue
		}
		seen[p.PkgPath] = true
		require.NotContains(t, p.PkgPath, "getsentry/sentry-go",
			"internal/logging must not import sentry-go transitively (INV-L1); reached %s", p.PkgPath)
		for _, dep := range p.Imports {
			if !seen[dep.PkgPath] {
				queue = append(queue, dep)
			}
		}
	}
}
