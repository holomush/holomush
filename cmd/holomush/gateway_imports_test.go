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
	// 07-09 item 5: the bootstrap orphan boot gate's definition + tests
	// moved to internal/bootstrap/setup (behind the Bootstrap -> Database
	// edge); the two bootstrap-orphan files no longer exist here.
	//
	// 07-09: the crypto/admin wiring block hoisted out of core.go into its
	// own memoized builder. Imports dek/chain/invalidation/access/admin/
	// eventbus/core/store; core-only by design (matches
	// crypto_rekey_wiring.go precedent).
	"cryptowiring.go":      {},
	"cryptowiring_test.go": {},
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
	// Test-only fixture for cmd_admin_totp.go's run function; imports
	// internal/auth for the auth.Player stub type. Surfaced by 07-04's
	// internal/auth/service -> internal/auth phantom-package fix (D-17):
	// the phantom entry never matched anything, so this pre-existing
	// import was never actually gated before now.
	"cmd_admin_totp_run_test.go": {},
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
	// KEK passphrase resolution + keyfile provisioning (holomush-5rh.8.29.12).
	// Imports internal/eventbus/crypto/kek for FileSource and KEKByteLength;
	// core-only (matches cmd_admin_totp_deps.go precedent).
	"kek_provision.go":      {},
	"kek_provision_test.go": {},
	// `holomush audit dlq` CLI is a host-shell operator tool (like
	// cmd_admin.go / migrate.go), not the gateway. It reads the
	// EVENTS_AUDIT_DLQ JetStream stream and writes events_audit directly to
	// replay dead letters (CLUSTER-04); imports internal/eventbus +
	// internal/eventbus/audit by design. No admin UDS.
	"cmd_audit.go":      {},
	"cmd_audit_test.go": {},
	// `holomush outbox skip` CLI is a host-shell operator tool (like
	// cmd_audit.go), not the gateway. It drives the world-change outbox
	// SkipService, which owns BOTH a Postgres pool AND a JetStream publisher
	// (05-07, MODEL-04); imports internal/world/{outbox,postgres,setup} +
	// internal/eventbus by design. No admin UDS.
	"outbox_admin.go":      {},
	"outbox_admin_test.go": {},
	// `holomush world genesis` / `world epoch-reset` CLI is a host-shell operator
	// tool (like outbox_admin.go), not the gateway. It emits the cutover genesis
	// snapshot / advances the feed epoch by driving the outbox GenesisService with
	// the injected postgres GenesisStore (05-11, MODEL-04, round-4 A3); imports
	// internal/world/{outbox,postgres} by design. No admin UDS, no crypto/abac.
	"world_genesis.go":      {},
	"world_genesis_test.go": {},
	// 07-09 item 6: the crypto-operator allow-list validation's definition +
	// tests moved to internal/access/setup (ABACSubsystem's own Start,
	// against its own pool); the two crypto-operator-validation files no
	// longer exist here.
	//
	// 07-10 Task 3/4: pins the real production topological start order and
	// proves the real production dependency graph acyclic (MEDIUM-11 /
	// T-07-65). Constructs every production subsystem type with a
	// zero-value config purely to read its live DependsOn() — this is the
	// core process's own orchestrator wiring under test, not the gateway;
	// imports the same domain packages core.go itself imports.
	"core_topo_order_test.go": {},
	// 07-11 D-13.1: core_subsystems_test.go's new
	// TestStartAllActivatesNothingUntilEverySubsystemHasPrepared property
	// test constructs every production subsystem type (the same
	// zero-value-config pattern core_topo_order_test.go uses) to source
	// DependsOn() live rather than hand-copying it, so it needs the exact
	// same domain imports. Core process's own orchestrator wiring under
	// test, not the gateway.
	"core_subsystems_test.go": {},
}

// gatewayForbiddenPackages is the single, shared policy list read by both
// gateway boundary gates (this direct-import AST gate and the transitive
// closure gate in gateway_closure_test.go). internal/core, internal/session,
// and internal/grpc are forbidden WHOLESALE rather than allow-listed by
// symbol: D-15 rejects the per-symbol escape hatch that let coreOnlyFiles
// grow to ~30 unreviewed judgement calls. internal/core and internal/session
// are already dependency-free leaves (verified live: `go list -deps
// ./internal/core` and `./internal/session` each return only themselves);
// internal/grpc is the one entry that reaches the DB (a 41-package closure
// through world/access/command/store) and was removed from the gateway by
// 07-01's internal/grpcclient extraction.
var gatewayForbiddenPackages = []string{
	"github.com/holomush/holomush/internal/world",
	"github.com/holomush/holomush/internal/access",
	"github.com/holomush/holomush/internal/store",
	"github.com/holomush/holomush/internal/plugin",
	"github.com/holomush/holomush/internal/eventbus",
	// internal/auth (not internal/auth/service, which does not exist -
	// go list ./internal/auth/... yields no service subpackage). Matched
	// below by exact-path OR "internal/auth/"-prefix (checkFile and
	// closureContainsPackage both use this rule), so it does NOT
	// accidentally match an unrelated internal/authz-style sibling; there
	// is no such sibling today.
	"github.com/holomush/holomush/internal/auth",
	"github.com/holomush/holomush/internal/command",
	"github.com/holomush/holomush/internal/core",
	"github.com/holomush/holomush/internal/session",
	"github.com/holomush/holomush/internal/grpc",
}

// TestGatewayImportsAreOnlyProtocolTranslation is INV-EVENTBUS-1. Gateway-side
// files MUST NOT import domain packages. Core-process files are excluded
// via coreOnlyFiles.
//
// Verifies: INV-EVENTBUS-1
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
		for _, bad := range gatewayForbiddenPackages {
			if importPath == bad || strings.HasPrefix(importPath, bad+"/") {
				t.Errorf("%s/%s imports forbidden domain package %s",
					pkgPath, base, importPath)
			}
		}
	}
}
