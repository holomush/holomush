// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
)

// adminEditCharacterSheetPath is the admin edit surface whose declaration this
// package pins. It is repo-relative; a moved or renamed Sheet fails the
// require.FileExists below rather than silently dropping this coverage.
const adminEditCharacterSheetPath = "web/src/lib/components/admin/EditCharacterSheet.svelte"

// The three things parsed out of the Sheet's module script. Each is duplicated
// across the language boundary and each is asserted separately below, because
// they drift independently: the path SET, the two byte CAPS, and the
// path-to-cap MAPPING.
var (
	adminSheetShortCapRe = regexp.MustCompile(`(?m)^\s*const SHORT = (\d+);`)
	adminSheetLongCapRe  = regexp.MustCompile(`(?m)^\s*const LONG = (\d+);`)
	adminSheetFieldsRe   = regexp.MustCompile(`(?s)export const ADMIN_EDITABLE_FIELDS: EditableField\[\] = \[(.*?)\n\s*\];`)
	adminSheetEntryRe    = regexp.MustCompile(`(?m)^\s*(line|prose)\('([^']+)',`)
)

// repoRootFromCwd walks upward from the test's working directory until it finds
// the directory containing go.mod, which marks the repository root. Go runs a
// test with its working directory set to the package directory, so this
// resolves from internal/grpc. The idiom is already in-tree at
// internal/store/no_delete_grep_test.go and test/meta/meta_helpers_test.go.
func repoRootFromCwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root (no go.mod in any parent of %q)", dir)
		}
		dir = parent
	}
}

// TestAdminEditableFieldsInTheWebSheetMatchTheServerMaskAllowlist pins the admin
// Sheet's declaration against the allowlist the server actually enforces.
//
// # What it buys, named individually
//
// THREE things were duplicated across the Go/TypeScript boundary, not one, and
// each drifts on its own:
//
//   - the PATH SET — ADMIN_EDITABLE_FIELDS against adminProfileMaskablePaths, in
//     both directions;
//   - the two BYTE CAPS — the Sheet's SHORT and LONG literals against
//     world.MaxNameLength and world.MaxDescriptionLength, by VALUE. A trailing
//     comment naming a Go constant is documentation, not a link;
//   - the PATH-TO-CAP MAPPING — which paths the Sheet declares with `line` and
//     which with `prose`, per path against that entry's maxBytes. Set equality
//     alone cannot see a `line` that should have been a `prose`.
//
// # What it does NOT cover
//
// It compares DECLARATIONS, not behaviour. It says nothing about whether the
// Sheet renders a control for every path it declares (the Sheet's own suite
// asserts that over rendered output), nothing about the counter's arithmetic
// (web/src/lib/text/byteCount.ts and its tests), and nothing about the ORDER the
// paths appear in — that order is the wire's own and is deliberately not
// asserted here, because the Sheet is free to reorder its form without the
// server caring.
//
// TestAdminProfileMaskAllowlistMatchesSpec is the sibling that pins the same map
// UPWARD against 01-SPEC §10.6. The two together say the map agrees with the
// SPEC above it and with the client below it.
//
// The mitigation that keeps this a guard rather than a fix: adminProfileMaskablePaths
// is compared by exact string and an unlisted path is REJECTED, not ignored, so
// client drift has always failed CLOSED — the worst outcome was an operator edit
// refused at the RPC, never a wrong write. This test changes when drift is
// CAUGHT, not who decides.
//
// # Why it parses text
//
// The principle is the one internal/world/validation.go:24-30 already applies,
// aliasing the charname leaf's bounds so the two cannot drift (D-28). Across a
// LANGUAGE boundary an alias is not available, so the choice is a parse, a
// generator, or a shared schema. The parse is chosen: the authority
// (adminProfileMaskablePaths) is UNEXPORTED, so a test in this package holds it
// as a live symbol and world.MaxNameLength as a live constant, leaving exactly
// ONE side to parse. A test in test/meta would have to re-parse the Go source
// too, comparing two derived readings, neither of which is the value the server
// enforces.
func TestAdminEditableFieldsInTheWebSheetMatchTheServerMaskAllowlist(t *testing.T) {
	t.Parallel()

	sheet := filepath.Join(repoRootFromCwd(t), filepath.FromSlash(adminEditCharacterSheetPath))
	require.FileExists(t, sheet,
		"the admin edit Sheet moved or was renamed; this guard must fail loudly rather than "+
			"quietly stop comparing anything")
	raw, err := os.ReadFile(sheet)
	require.NoError(t, err, "reading the admin edit Sheet")
	src := string(raw)

	// --- anti-vacuity, asserted BEFORE any comparison -----------------------
	// Without these a parse that silently matched nothing compares two empty
	// maps and reports success — the exact failure this guard exists to prevent.

	body := adminSheetFieldsRe.FindStringSubmatch(src)
	require.Len(t, body, 2,
		"ADMIN_EDITABLE_FIELDS was not found in %s — a rename or a reformat makes this guard "+
			"compare nothing and pass vacuously", adminEditCharacterSheetPath)

	entries := adminSheetEntryRe.FindAllStringSubmatch(body[1], -1)
	require.NotEmpty(t, entries,
		"no line()/prose() entry parsed out of ADMIN_EDITABLE_FIELDS — the declaration was "+
			"reformatted and this guard is comparing an empty set")

	shortMatch := adminSheetShortCapRe.FindStringSubmatch(src)
	require.Len(t, shortMatch, 2, "the Sheet's SHORT cap declaration was not found")
	longMatch := adminSheetLongCapRe.FindStringSubmatch(src)
	require.Len(t, longMatch, 2, "the Sheet's LONG cap declaration was not found")

	shortCap, err := strconv.Atoi(shortMatch[1])
	require.NoError(t, err, "parsing the Sheet's SHORT cap")
	longCap, err := strconv.Atoi(longMatch[1])
	require.NoError(t, err, "parsing the Sheet's LONG cap")
	require.Positive(t, shortCap, "a non-positive SHORT cap would make every value over-length")
	require.Positive(t, longCap, "a non-positive LONG cap would make every value over-length")
	require.NotEqual(t, shortCap, longCap,
		"the two caps collapsing into one would make the per-path mapping assertion below "+
			"unable to distinguish a line from a prose")

	require.NotEmpty(t, adminProfileMaskablePaths,
		"an empty server allowlist would satisfy both directions of the set comparison vacuously")

	// --- the path-to-cap mapping, read from the constructor -----------------
	// `line` means the short cap and `prose` means the long one. Reading the
	// mapping from the CONSTRUCTOR rather than from a separate table is why the
	// mapping is covered at all.

	webCaps := make(map[string]int, len(entries))
	for _, e := range entries {
		kind, path := e[1], e[2]
		if _, dup := webCaps[path]; dup {
			t.Fatalf("the Sheet declares %q twice: a duplicate renders the field twice while "+
				"leaving the set comparison below green", path)
		}
		if kind == "line" {
			webCaps[path] = shortCap
		} else {
			webCaps[path] = longCap
		}
	}

	// --- the two caps, by value ---------------------------------------------

	assert.Equal(t, world.MaxNameLength, shortCap,
		"the Sheet's SHORT cap and world.MaxNameLength must draw the SAME boundary: the counter "+
			"warns at one byte count while the server refuses at another otherwise, and a trailing "+
			"comment naming the constant is documentation, not a link")
	assert.Equal(t, world.MaxDescriptionLength, longCap,
		"the Sheet's LONG cap and world.MaxDescriptionLength must draw the SAME boundary")

	// --- the path set, in both directions -----------------------------------

	var missingFromClient, unknownToServer []string
	for path := range adminProfileMaskablePaths {
		if _, ok := webCaps[path]; !ok {
			missingFromClient = append(missingFromClient, path)
		}
	}
	for path := range webCaps {
		if _, ok := adminProfileMaskablePaths[path]; !ok {
			unknownToServer = append(unknownToServer, path)
		}
	}
	assert.Empty(t, missingFromClient,
		"the server accepts a writable path the Sheet offers no control for — an admin-reachable "+
			"field with no way to reach it, which is an ADMIN-04 scope loss")
	assert.Empty(t, unknownToServer,
		"the Sheet offers a control for a path the server's closed allowlist refuses — the save is "+
			"rejected at the RPC, which fails closed but wastes the operator's edit")

	// --- the per-path cap agreement -----------------------------------------
	// The assertion that catches a `line` that should have been a `prose`, which
	// set equality alone stays green under.

	var mismatched []string
	for path, clientCap := range webCaps {
		field, ok := adminProfileMaskablePaths[path]
		if !ok {
			continue // already reported as unknownToServer
		}
		if clientCap != field.maxBytes {
			mismatched = append(mismatched,
				path+": the Sheet counts to "+strconv.Itoa(clientCap)+
					" bytes, the server caps at "+strconv.Itoa(field.maxBytes))
		}
	}
	assert.Empty(t, mismatched,
		"a path the Sheet and the server cap differently: the operator is either warned at a "+
			"boundary the server does not enforce, or refused at one the counter never showed.\n"+
			strings.Join(mismatched, "\n"))
}
