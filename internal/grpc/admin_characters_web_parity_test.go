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

// The four things parsed out of the Sheet's module script. Each is duplicated
// across the language boundary and each is asserted separately below, because
// they drift independently: the path SET, the two byte CAP DECLARATIONS, the
// path-to-cap MAPPING, and the maxBytes expression each constructor EMITS.
var (
	adminSheetShortCapRe = regexp.MustCompile(`(?m)^\s*const SHORT = (\d+);`)
	adminSheetLongCapRe  = regexp.MustCompile(`(?m)^\s*const LONG = (\d+);`)
	adminSheetFieldsRe   = regexp.MustCompile(`(?s)export const ADMIN_EDITABLE_FIELDS: EditableField\[\] = \[(.*?)\n\s*\];`)
	adminSheetEntryRe    = regexp.MustCompile(`(?m)^\s*(line|prose)\('([^']+)',`)
	// Each single-expression arrow constructor, name and body. The body is
	// what makes the cap a LINK rather than an assumption: reading the cap off
	// the constructor's NAME re-derives what the Sheet was assumed to emit,
	// which is green under a literal typed in place of SHORT.
	adminSheetCtorRe = regexp.MustCompile(`(?s)const (line|prose) = \([^)]*\): EditableField => \(\{(.*?)\n\s*\}\);`)
	// A maxBytes property's right-hand side, up to the next comma or closing
	// brace. Trimmed by the caller.
	adminSheetMaxBytesRe = regexp.MustCompile(`maxBytes:([^,}]+)`)
)

// resolveAdminSheetCap turns a parsed `maxBytes` expression into the byte count
// it denotes: a bare identifier naming one of the Sheet's own pinned cap
// declarations, or a plain decimal literal.
//
// Anything else is a HARD FAILURE rather than a fallback. Falling back to the
// constructor's name is precisely the defect this resolver replaces — it made
// the guard report the cap the Sheet was assumed to emit instead of the one it
// does emit, so every rendered counter could read a number the server does not
// enforce while the guard stayed green.
func resolveAdminSheetCap(t *testing.T, ctor, expr string, consts map[string]int) int {
	t.Helper()
	if v, ok := consts[expr]; ok {
		return v
	}
	if isDecimalLiteral(expr) {
		v, err := strconv.Atoi(expr)
		require.NoError(t, err, "parsing the %s constructor's maxBytes literal %q", ctor, expr)
		return v
	}
	t.Fatalf("the %s constructor emits maxBytes: %s, which this guard will not resolve. It "+
		"refuses to GUESS a cap it cannot read, because guessing is how a counter comes to "+
		"show a boundary the server does not enforce. A computed cap must be replaced with "+
		"one of the two pinned constants (SHORT or LONG), which are themselves asserted "+
		"against world.MaxNameLength / world.MaxDescriptionLength below.", ctor, expr)
	return 0
}

// isDecimalLiteral reports whether s is a non-empty run of decimal digits. A
// sign or a separator is not a literal for this purpose: the resolver above
// admits only the two shapes it can attribute a meaning to.
func isDecimalLiteral(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

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
// FOUR things were duplicated across the Go/TypeScript boundary, not one, and
// each drifts on its own:
//
//   - the PATH SET — ADMIN_EDITABLE_FIELDS against adminProfileMaskablePaths, in
//     both directions;
//   - the two BYTE CAP DECLARATIONS — the Sheet's SHORT and LONG literals against
//     world.MaxNameLength and world.MaxDescriptionLength, by VALUE. A trailing
//     comment naming a Go constant is documentation, not a link;
//   - the PATH-TO-CAP MAPPING — which paths the Sheet declares with `line` and
//     which with `prose`, per path against that entry's maxBytes. Set equality
//     alone cannot see a `line` that should have been a `prose`;
//   - the EMITTED maxBytes EXPRESSION — what each constructor actually puts on
//     the field it builds, resolved to a number against those two declarations.
//     Pinning the declarations leaves the constructors unpinned: `maxBytes: 90`
//     typed in place of `maxBytes: SHORT` leaves `const SHORT = 100` reading
//     correctly, so every assertion that consults only the declaration stays
//     green while all seven line counters render a boundary the server does not
//     enforce. An expression that is neither a pinned identifier nor a decimal
//     literal fails loudly rather than resolving to a guess.
//
// # What it does NOT cover
//
// It compares DECLARATIONS, not RENDERED OUTPUT. Nothing here observes a
// counter. That the Sheet renders a control for every path it declares, and
// that each control's counter renders THAT field's maxBytes, is asserted over
// rendered output by the Sheet's own suite
// (web/src/lib/components/admin/EditCharacterSheet.svelte.test.ts); that the
// counter's unit is BYTES rather than code points is proven in a browser by the
// byte-cap block of web/e2e/admin-portal.spec.ts, which drives the admin Sheet
// at both caps. The counter's arithmetic is web/src/lib/text/byteCount.ts and
// its tests. Together with this guard the chain runs unbroken from the Go
// constant to the number an operator reads.
//
// It says nothing about the ORDER the paths appear in — that order is the
// wire's own and is deliberately not asserted here, because the Sheet is free
// to reorder its form without the server caring.
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

	// --- what each constructor EMITS, resolved against those declarations ----
	// The declarations above say what SHORT and LONG are. They say nothing
	// about whether the constructors USE them, and a number typed in place of
	// SHORT is invisible to every assertion that reads only the declaration.
	// So the emitted expression is parsed and resolved here, and the mapping
	// below is built from the resolved values rather than from a constructor's
	// name.

	consts := map[string]int{"SHORT": shortCap, "LONG": longCap}

	ctorCaps := make(map[string]int, 2)
	for _, c := range adminSheetCtorRe.FindAllStringSubmatch(src, -1) {
		name, ctorBody := c[1], c[2]
		mb := adminSheetMaxBytesRe.FindStringSubmatch(ctorBody)
		require.Len(t, mb, 2,
			"the %s constructor declares no maxBytes property — every EditableField carries a "+
				"byte cap, and a constructor that emits none leaves the fields it builds with "+
				"no boundary this guard can compare", name)
		ctorCaps[name] = resolveAdminSheetCap(t, name, strings.TrimSpace(mb[1]), consts)
	}

	// Anti-vacuity for the block just above: a constructor that stopped
	// matching would drop every path it builds out of the mapping, and the
	// two caps collapsing would make the per-path assertion below unable to
	// tell a line from a prose — the same reason the declarations must differ.
	for _, name := range []string{"line", "prose"} {
		require.Contains(t, ctorCaps, name,
			"the %s constructor was not found in %s — a rename or a reformat makes this guard "+
				"build an incomplete mapping instead of comparing one",
			name, adminEditCharacterSheetPath)
	}
	require.NotEqual(t, ctorCaps["line"], ctorCaps["prose"],
		"the two constructors emit the same cap (%d): the per-path mapping assertion below "+
			"cannot distinguish a line from a prose when they agree", ctorCaps["line"])

	// --- the path-to-cap mapping, read from the constructor -----------------
	// `line` means whatever the `line` constructor EMITS and `prose` likewise.
	// Reading the mapping from the CONSTRUCTOR rather than from a separate
	// table is why the mapping is covered at all.

	webCaps := make(map[string]int, len(entries))
	for _, e := range entries {
		kind, path := e[1], e[2]
		if _, dup := webCaps[path]; dup {
			t.Fatalf("the Sheet declares %q twice: a duplicate renders the field twice while "+
				"leaving the set comparison below green", path)
		}
		capBytes, known := ctorCaps[kind]
		if !known {
			t.Fatalf("the Sheet declares %q with the %q constructor, which this guard resolved "+
				"no cap for: a third constructor was added without extending this guard, and "+
				"inferring its cap would be a guess", path, kind)
		}
		webCaps[path] = capBytes
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
