// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package main

import (
	"go/ast"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// coreOnlyFiles are cmd/holomush files that legitimately import domain
// packages because they are part of the core process entry point, not
// the gateway. Every other .go file in cmd/holomush is treated as
// gateway-side and held to INV-EVENTBUS-1.
var coreOnlyFiles = map[string]struct{}{
	"core.go":                         {},
	"core_test.go":                    {},
	"deps.go":                         {},
	"deps_test.go":                    {},
	"sub_grpc.go":                     {},
	"sub_grpc_adapters_test.go":       {},
	"sub_grpc_test.go":                {},
	"automigrate_test.go":             {},
	"automigrate_integration_test.go": {},
	"migrate.go":                      {},
	"migrate_test.go":                 {},
	"cmd_plugin_events.go":            {},
	"cmd_plugin_validate.go":          {},
	"bootstrap_orphan.go":             {},
	"bootstrap_orphan_test.go":        {},
	// Phase 5 sub-epic E rekey wiring (holomush-jxo8.7.34). Constructs
	// dek.PolicyHashSource over auditchain.Repo for the orchestrator's
	// INV-CRYPTO-112 capture-at-Phase-1 dependency. Core-only.
	"policy_hash_source.go": {},
	// Phase 5 sub-epic E rekey wiring (holomush-jxo8.7.44). Production
	// dek.Manager + Orchestrator + admin RekeyHandler construction. Imports
	// dek/chain/invalidation/kek/world/access/admin; core-only by design.
	"crypto_rekey_wiring.go":      {},
	"crypto_rekey_wiring_test.go": {},
	// Phase 5 sub-epic F R.14 AdminReadStream wiring (holomush-jxo8.8.38).
	// Production readstream.Handler construction (ColdReader, audit emitter,
	// session/DEK/codec adapters). Imports access/admin/eventbus/dek;
	// core-only by design (matches crypto_rekey_wiring.go precedent).
	"readstream_wiring.go":      {},
	"readstream_wiring_test.go": {},
	// `holomush admin` CLI is a host-shell tool, not the gateway. It
	// connects to the same PG/KEK as the core server for break-glass
	// flows (TOTP enroll/verify/recover). Phase 5 sub-epic A.
	"cmd_admin.go":           {},
	"cmd_admin_test.go":      {},
	"cmd_admin_totp.go":      {},
	"cmd_admin_totp_deps.go": {},
	// Phase 7 INV-CRYPTO-45 + INV-CRYPTO-42 + INV-CRYPTO-50 wiring (holomush-1r0v.5).
	// Constructs the boot-time PluginDowngradeFence helpers (crypto_keys
	// lookup, violation emitter). Imports eventbus/history + core; core-only
	// by design (matches crypto_rekey_wiring.go precedent). The KeySelector
	// and AlwaysSensitiveSet derivations moved to internal/plugin/cryptowiring
	// (holomush-5iaov.1/.2), so this file no longer imports codec or plugin.
	"phase7_fence_wiring.go":      {},
	"phase7_fence_wiring_test.go": {},
	// AlwaysSensitiveSet adapter (holomush-5iaov.2). Adapts *plugins.Manager
	// to cryptowiring.ManifestSource for the boot-time AlwaysSensitiveSet
	// call. Imports internal/plugin; core-only (matches phase7_fence_wiring.go
	// precedent).
	"cryptowiring_adapter.go": {},
}

var forbidden = []string{
	"github.com/holomush/holomush/internal/world",
	"github.com/holomush/holomush/internal/access",
	"github.com/holomush/holomush/internal/store",
	"github.com/holomush/holomush/internal/plugin",
	"github.com/holomush/holomush/internal/eventbus",
	"github.com/holomush/holomush/internal/auth/service",
	"github.com/holomush/holomush/internal/command",
}

// TestGatewayImportsAreOnlyProtocolTranslation is INV-EVENTBUS-1. Gateway-side
// files MUST NOT import domain packages. Core-process files are excluded
// via coreOnlyFiles.
func TestGatewayImportsAreOnlyProtocolTranslation(t *testing.T) {
	pkgs, err := packages.Load(
		&packages.Config{
			Mode: packages.NeedName | packages.NeedFiles |
				packages.NeedSyntax | packages.NeedImports |
				packages.NeedTypes,
			// Tests:true loads *_test.go files into pkg.Syntax so the import
			// guard sees gateway-side test files (core_test.go, deps_test.go,
			// sub_grpc_adapters_test.go) which would otherwise bypass INV-EVENTBUS-1.
			Tests: true,
		},
		"github.com/holomush/holomush/cmd/holomush",
		"github.com/holomush/holomush/internal/web/...",
		"github.com/holomush/holomush/internal/telnet/...",
	)
	require.NoError(t, err)
	require.Empty(t, packages.PrintErrors(pkgs))

	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			goFile := pkg.Fset.Position(file.Pos()).Filename
			checkFile(t, pkg.PkgPath, goFile, file)
		}
	}
}

func checkFile(t *testing.T, pkgPath, goFile string, file *ast.File) {
	t.Helper()
	base := filepath.Base(goFile)
	if pkgPath == "github.com/holomush/holomush/cmd/holomush" {
		if _, isCore := coreOnlyFiles[base]; isCore {
			return
		}
	}
	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		for _, bad := range forbidden {
			if importPath == bad || strings.HasPrefix(importPath, bad+"/") {
				t.Errorf("%s/%s imports forbidden domain package %s",
					pkgPath, base, importPath)
			}
		}
	}
}
