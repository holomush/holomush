// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package logging_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// TestINV_L1_LoggingHasNoSentryImport asserts internal/logging never imports
// sentry-go (transitively included). The vendor-neutral boundary: only
// internal/telemetry may touch the Sentry SDK. (INV-L1)
func TestINV_L1_LoggingHasNoSentryImport(t *testing.T) { // INV-L1
	cfg := &packages.Config{Mode: packages.NeedImports | packages.NeedDeps | packages.NeedName}
	pkgs, err := packages.Load(cfg, "github.com/holomush/holomush/internal/logging")
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)
	for _, p := range pkgs {
		for imp := range p.Imports {
			require.NotContains(t, imp, "getsentry/sentry-go",
				"internal/logging must not import sentry-go (INV-L1); found via %s", p.PkgPath)
		}
	}
}
