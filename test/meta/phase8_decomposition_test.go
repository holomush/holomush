// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Verifies: INV-PLUGIN-56
package meta

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// This file is Phase 8's regrowth ratchet (ARCH-01 / ARCH-02, D-11/D-12).
//
// Why it exists, stated plainly so a future reader does not mistake it for
// ceremony: BOTH god objects this phase decomposed had already been FILED as
// god objects in issue #4674 — and both then GREW. internal/grpc/server.go went
// 1331 -> 1891 lines and internal/plugin/manager.go went 1222 -> 1869 while an
// open issue named them. A split with no mechanical gate demonstrably regrows in
// this repository. A census that asserted only "the split happened" would have
// stayed green through every line of that growth, which is why this file pins
// the SIZE and the IMPORT DIRECTION, not merely the shape.
//
// Two halves, deliberately weighted differently:
//
//   - The import-direction half is a durable architectural guarantee and is
//     registered as INV-PLUGIN-56 (see the file-level Verifies annotation above).
//   - The size-ceiling half is a regrowth ratchet, NOT a system invariant. It is
//     deliberately left UNBOUND: per D-14 and .claude/rules/invariants.md, a
//     registry entry backed by a line counter would claim a guarantee the test
//     does not make. Do not add a Verifies annotation to it.

// ---------------------------------------------------------------------------
// Half 1 — import direction (INV-PLUGIN-56)
// ---------------------------------------------------------------------------

// importUnder reports the first import in imports that is targetRel or lives
// beneath it, so a forbidden edge cannot be re-established by reaching for a
// sibling subpackage of the one that was originally cut.
func importUnder(imports []string, targetRel string) (string, bool) {
	target := modulePath + "/" + targetRel
	for _, imp := range imports {
		if imp == target || strings.HasPrefix(imp, target+"/") {
			return imp, true
		}
	}
	return "", false
}

// TestPhase8ImportDirectionHasNoUpwardOrCyclicEdges asserts the layering D-09
// established: nothing in the internal/plugin tree imports up into internal/grpc,
// and internal/eventbus does not depend on internal/plugin in either direction.
//
// Production imports only: worldPkgImports is built on go/build, whose
// Package.Imports excludes _test.go files (those land in TestImports /
// XTestImports). Test files may legitimately hold concrete fixtures that cross
// these boundaries; production code may not.
func TestPhase8ImportDirectionHasNoUpwardOrCyclicEdges(t *testing.T) {
	root := findRepoRoot(t)

	forbidden := []struct {
		fromRel, toRel, why string
	}{
		// D-09 seam 1 (closed by 08-01 + 08-02): seven internal/plugin files
		// imported UP into internal/grpc/focus. They were rewired onto the
		// neutral internal/focuscontract package. The guard targets the whole
		// internal/grpc tree rather than only .../focus: re-importing ANY grpc
		// package recreates the consumer-imports-its-own-consumer inversion that
		// arch-review MEDIUM-4 named, and pinning only the one package that
		// happened to be cut would leave the invariant trivially routable.
		{"internal/plugin", "internal/grpc", "manager.go / host.go imported focus.Coordinator (08-02)"},
		{"internal/plugin/lua", "internal/grpc", "lua/{host,focus_ops_adapter,hostcap_adapter}.go (08-02)"},
		{"internal/plugin/goplugin", "internal/grpc", "goplugin/host.go imported focus.Coordinator (08-02)"},
		{"internal/plugin/hostcap", "internal/grpc", "hostcap/capabilities.go imported focus.Coordinator (08-02)"},

		// D-09 seam 2 (closed by 08-02): the single eventbus -> plugin edge.
		// 08-02 deleted authguard/adapter_manifest.go and inverted the
		// dependency by structural satisfaction.
		{"internal/eventbus/authguard", "internal/plugin", "adapter_manifest.go imported plugins.Manager (08-02, deleted)"},

		// The MIRROR of seam 2. 08-02 inverted the edge; re-adding an adapter on
		// the plugin side would restore the same coupling from the other
		// direction and would look like an entirely reasonable fix to anyone who
		// does not know why the original adapter was deleted. Carried forward
		// from 08-02's closeout; not in the original plan's five-row table.
		{"internal/plugin", "internal/eventbus/authguard", "mirror of seam 2 — re-coupling from the plugin side"},
	}

	for _, e := range forbidden {
		imports := worldPkgImports(t, root, e.fromRel)
		got, found := importUnder(imports, e.toRel)
		require.Falsef(t, found,
			"forbidden import edge re-established: %s must NOT import %s (found %q).\n"+
				"Seam: %s.\n"+
				"This is the bidirectional coupling arch-review MEDIUM-4 identified and Phase 8 "+
				"Wave 0 removed (INV-PLUGIN-56). Production imports only; _test.go is exempt.",
			e.fromRel, e.toRel, got, e.why)
	}
}

// ---------------------------------------------------------------------------
// Half 2 — size ceilings (DELIBERATELY UNBOUND; see the file header)
// ---------------------------------------------------------------------------

// overCeiling is the ceiling comparison, factored out so its boundary semantics
// can be asserted directly with synthetic values rather than inferred by growing
// a real file. The comparison is INCLUSIVE at the ceiling: a file whose count
// exactly equals its ceiling passes; one line over fails.
func overCeiling(actual, ceiling int) bool { return actual > ceiling }

// countLines counts newlines, matching `wc -l`, which is the figure every
// Phase 8 SUMMARY recorded and therefore the figure the ceilings are calibrated
// against.
func countLines(t *testing.T, path string) int {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoErrorf(t, err, "read %s", path)
	return bytes.Count(body, []byte{'\n'})
}

// phase8Ceilings maps each decomposed file to its line ceiling.
//
// Every ceiling is derived from the file's MEASURED post-split actual (recorded
// in the trailing comment) plus roughly 10-15% headroom — loose enough that an
// ordinary bug fix does not trip it, tight enough that a new cluster does. The
// actuals are committed alongside the ceilings on purpose: a future ceiling bump
// then shows up in review as a visibly widening gap rather than an opaque number
// change.
//
// No ceiling approaches the pre-split god-object sizes (server.go 1891,
// manager.go 1869). A gate calibrated so it can never bite is worse than no gate
// — it emits a green signal that displaces the review attention it replaced,
// which is how server.go reached 1891 unchallenged in the first place.
var phase8Ceilings = map[string]int{
	// ARCH-01 — CoreServer facade + its four extracted units.
	"internal/grpc/server.go":            750,  // actual 657 (was 1891 pre-phase)
	"internal/grpc/subscribe_handler.go": 1100, // actual 973
	"internal/grpc/command_handler.go":   480,  // actual 417
	"internal/grpc/lifecycle_handler.go": 445,  // actual 386
	"internal/grpc/query_handler.go":     250,  // actual 213

	// ARCH-02 — Manager facade + its three extracted units.
	"internal/plugin/manager.go":        800,  // actual 702 (was 1876 pre-phase)
	"internal/plugin/loader.go":         1300, // actual 1142 — largest unit; next split candidate
	"internal/plugin/runtime.go":        680,  // actual 593
	"internal/plugin/identity_store.go": 250,  // actual 212
}

// TestPhase8DecomposedFilesStayUnderTheirCeilings is the regrowth ratchet.
//
// DELIBERATELY CARRIES NO Verifies ANNOTATION (D-14). It counts lines; it does
// not assert a system invariant.
func TestPhase8DecomposedFilesStayUnderTheirCeilings(t *testing.T) {
	root := findRepoRoot(t)

	for rel, ceiling := range phase8Ceilings {
		actual := countLines(t, filepath.Join(root, rel))
		require.Falsef(t, overCeiling(actual, ceiling),
			"%s has regrown to %d lines, over its ceiling of %d.\n\n"+
				"This is the Phase 8 regrowth ratchet (ARCH-01/ARCH-02, D-11/D-12). It is not "+
				"arbitrary: both files this phase decomposed had ALREADY been filed as god objects "+
				"in issue #4674 and then grew anyway — server.go 1331 -> 1891, manager.go "+
				"1222 -> 1869. The ceiling exists because that happened.\n\n"+
				"Do NOT fix this by raising the ceiling in the same change that tripped it. "+
				"Recalibrating a gate to accommodate the regression it just caught is how the gate "+
				"stops being a gate. Extract the new cluster into its own unit instead, as Phase 8 "+
				"did; if the growth is genuinely irreducible, raise the ceiling in a SEPARATE, "+
				"separately-reviewed commit that says why.",
			rel, actual, ceiling)
	}
}

// TestPhase8CeilingComparisonIsInclusiveAtTheCeiling pins the boundary semantics
// against synthetic values, so at-ceiling and one-over behavior is asserted
// directly rather than inferred from whatever size the real files happen to be.
func TestPhase8CeilingComparisonIsInclusiveAtTheCeiling(t *testing.T) {
	require.False(t, overCeiling(699, 700), "one line under the ceiling must pass")
	require.False(t, overCeiling(700, 700), "exactly at the ceiling must PASS (inclusive)")
	require.True(t, overCeiling(701, 700), "one line over the ceiling must FAIL")
}

// TestPhase8CeilingsAreCalibratedBelowThePreSplitSizes guards the ratchet
// against being neutered wholesale. A ceiling at or above the pre-split god
// object is not a gate at all.
func TestPhase8CeilingsAreCalibratedBelowThePreSplitSizes(t *testing.T) {
	require.Lessf(t, phase8Ceilings["internal/grpc/server.go"], 1891,
		"server.go's ceiling must stay below its pre-split size of 1891 lines")
	require.Lessf(t, phase8Ceilings["internal/plugin/manager.go"], 1869,
		"manager.go's ceiling must stay below the 1869 lines #4674 recorded")
}

// ---------------------------------------------------------------------------
// Half 3 — the decomposition census
// ---------------------------------------------------------------------------

// phase8Units maps each extracted unit to the file that defines it and the
// separately-testable proof test its plan's SUMMARY recorded. Each proof test
// constructs its unit from an EXTERNAL test package with only that unit's own
// collaborators — no parent facade, no integration harness. That external
// package is the D-02 property: it is what makes "separately testable" a fact
// rather than a claim.
var phase8Units = map[string]struct{ unitFile, proofTest string }{
	// ARCH-01 — CoreServer (internal/grpc), proof tests in package grpc_test.
	"SubscribeHandler": {"internal/grpc/subscribe_handler.go", "internal/grpc/subscribe_handler_test.go"},
	"CommandHandler":   {"internal/grpc/command_handler.go", "internal/grpc/command_handler_test.go"},
	"LifecycleHandler": {"internal/grpc/lifecycle_handler.go", "internal/grpc/lifecycle_handler_test.go"},
	"QueryHandler":     {"internal/grpc/query_handler.go", "internal/grpc/query_handler_test.go"},

	// ARCH-02 — Manager (internal/plugin), proof tests in package plugins_test.
	"IdentityStore": {"internal/plugin/identity_store.go", "internal/plugin/identity_store_test.go"},
	"PluginRuntime": {"internal/plugin/runtime.go", "internal/plugin/runtime_test.go"},
	"PluginLoader":  {"internal/plugin/loader.go", "internal/plugin/loader_test.go"},
}

// TestPhase8CensusHoldsSevenSeparatelyTestableUnits asserts each extracted unit
// still exists, still declares its type, and still has a proof test in an
// EXTERNAL test package.
func TestPhase8CensusHoldsSevenSeparatelyTestableUnits(t *testing.T) {
	root := findRepoRoot(t)

	require.Len(t, phase8Units, 7,
		"Phase 8 extracted seven units — four off CoreServer (ARCH-01) and three off Manager (ARCH-02)")

	for unit, loc := range phase8Units {
		unitBody, err := os.ReadFile(filepath.Join(root, loc.unitFile))
		require.NoErrorf(t, err, "unit %s: %s is missing", unit, loc.unitFile)
		require.Containsf(t, string(unitBody), "type "+unit+" struct",
			"unit %s must be defined in %s", unit, loc.unitFile)

		proofBody, err := os.ReadFile(filepath.Join(root, loc.proofTest))
		require.NoErrorf(t, err, "unit %s: proof test %s is missing", unit, loc.proofTest)

		pkg := testPackageClause(t, proofBody)
		require.Truef(t, strings.HasSuffix(pkg, "_test"),
			"unit %s: its proof test %s declares %q, but must be an EXTERNAL test package "+
				"(package <name>_test). Constructing the unit from outside its own package is what "+
				"proves it is separately testable — an in-package test can reach unexported state "+
				"and prove nothing about the seam (D-02).",
			unit, loc.proofTest, pkg)
	}
}

// testPackageClause returns the package name declared by a Go source file.
func testPackageClause(t *testing.T, body []byte) string {
	t.Helper()
	for _, line := range strings.Split(string(body), "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "package "); ok {
			return strings.TrimSpace(after)
		}
	}
	t.Fatal("no package clause found")
	return ""
}

// TestPhase8FacadesHoldNoExtractedState is the structural half of the census,
// and it is the assertion that actually matters for regrowth.
//
// A line ceiling catches bulk; it cannot distinguish 80 lines of legitimate
// delegation from 80 lines of state creeping back onto the facade. Manager is
// the sharper case: roughly 430 of its 702 lines are option declarations,
// one-line forwarders and package-level helpers that grow MECHANICALLY (~6 lines
// per new exported method, ~8 per new option), so its ceiling alone would either
// be too tight to survive normal work or too loose to catch a relapse. The field
// count is immune to that growth and catches the regression the ceiling cannot.
func TestPhase8FacadesHoldNoExtractedState(t *testing.T) {
	root := findRepoRoot(t)

	managerFields := structFieldNames(t, filepath.Join(root, "internal/plugin/manager.go"), "Manager")
	require.ElementsMatchf(t, []string{"loader", "runtime", "identity", "cfg"}, managerFields,
		"Manager must remain a facade over its three units plus its option holder, holding NO plugin "+
			"state of its own (ARCH-02). Fields found: %v. A new field here is how the god object "+
			"comes back — it grew 1222 -> 1869 lines that way once already (#4674).", managerFields)

	serverBody, err := os.ReadFile(filepath.Join(root, "internal/grpc/server.go"))
	require.NoError(t, err)
	for _, handler := range []string{
		"subscribeHandler *SubscribeHandler",
		"commandHandler   *CommandHandler",
		"lifecycleHandler *LifecycleHandler",
		"queryHandler     *QueryHandler",
	} {
		require.Containsf(t, string(serverBody), handler,
			"CoreServer must hold each extracted unit as a field, delegating to it rather than "+
				"reimplementing the cluster (ARCH-01): %q not found", handler)
	}
}

// structFieldNames returns the field names declared in the named struct,
// skipping blank lines and comments.
func structFieldNames(t *testing.T, path, typeName string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoErrorf(t, err, "read %s", path)

	lines := strings.Split(string(body), "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "type "+typeName+" struct {") {
			start = i + 1
			break
		}
	}
	require.NotEqualf(t, -1, start, "type %s struct not found in %s", typeName, path)

	var fields []string
	for _, line := range lines[start:] {
		if strings.HasPrefix(line, "}") {
			return fields
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		fields = append(fields, strings.Fields(trimmed)[0])
	}
	t.Fatalf("unterminated struct %s in %s", typeName, path)
	return nil
}
