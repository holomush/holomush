// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// webSrcDir is the authored web source tree, relative to the repo root. It is
// the walk root rather than web/src/routes, because a viewport rule is just as
// consequential in a component under lib/ as in a route file.
const webSrcDir = "web/src"

// The three rule texts every banded viewport rule under webSrcDir is written
// as. Each derives its width from a Tailwind theme token rather than from a
// number typed into the file.
//
// The RANGE form is a correctness improvement, not tidying. A `max-width`
// declared one below the boundary and a `min-width` declared at it are not
// complements: at a fractional width such as 767.5px — reachable under browser
// zoom and fractional DPI scaling — neither matches, and a component whose JS
// half and CSS half read those two forms takes different branches at the same
// width. `width < token` and `width >= token` are exact complements everywhere.
const (
	ruleBelowMd    = "@media (width < theme(--breakpoint-md))"
	ruleAtOrAboveM = "@media (width >= theme(--breakpoint-md))"
	ruleBelowLg    = "@media (width < theme(--breakpoint-lg))"
)

// bandedViewportRule names one authored viewport rule and the token form it is
// written as. The slice below is the checked-in census of every such rule under
// webSrcDir; a file carrying two rules contributes two entries.
type bandedViewportRule struct {
	path string
	rule string
}

// bandedViewportRules is the enumerated set. It is checked in rather than
// derived, because the property under test is that a KNOWN rule still reads the
// token — a derived set would go quiet the moment a rule was deleted.
var bandedViewportRules = []bandedViewportRule{
	{"web/src/lib/components/shell/SectionRail.svelte", ruleBelowMd},
	{"web/src/lib/components/shell/SectionRail.svelte", ruleBelowLg},
	{"web/src/routes/(authed)/admin/+layout.svelte", ruleBelowLg},
	{"web/src/routes/(authed)/admin/+layout.svelte", ruleBelowMd},
	{"web/src/lib/components/admin/AdminNav.svelte", ruleBelowLg},
	{"web/src/lib/components/admin/CharacterTable.svelte", ruleBelowMd},
	{"web/src/lib/components/admin/CharacterFilterBar.svelte", ruleBelowMd},
	{"web/src/lib/components/admin/EditCharacterSheet.svelte", ruleBelowMd},
	{"web/src/lib/components/sidebar/Sidebar.svelte", ruleBelowMd},
	{"web/src/routes/c/[id]/+page.svelte", ruleAtOrAboveM},
	{"web/src/routes/(authed)/characters/[id]/+page.svelte", ruleAtOrAboveM},
	{"web/src/routes/(authed)/characters/new/+page.svelte", ruleAtOrAboveM},
	{"web/src/routes/(authed)/characters/+page.svelte", ruleAtOrAboveM},
	{"web/src/lib/components/characters/CharacterRoster.svelte", ruleAtOrAboveM},
	// TopBar carries TWO rules in ONE style block — the kbd-hint reveal and the
	// mobile-only hide — so its pair repeats.
	{"web/src/lib/components/TopBar.svelte", ruleAtOrAboveM},
	{"web/src/lib/components/TopBar.svelte", ruleAtOrAboveM},
}

// forbiddenViewportWidths are the four decimal strings a hand-written md or lg
// breakpoint takes, in BOTH spellings: the `max-width` complements (767, 1023)
// and the `min-width` forms (768, 1024).
//
// There is no exemption by spelling. `min-width: 768px` is exactly as much a
// hand-written copy of --breakpoint-md as `max-width: 767px` is, and this tree
// carried the two in roughly equal number before the conversion. A census that
// forbade only one would license the other one directory over.
var forbiddenViewportWidths = []string{"767", "768", "1023", "1024"}

// breakpointLiteralAllowlist maps a repo-relative PATH to the reason every
// forbidden literal in it is permitted.
//
// It is keyed by PATH, not by path:line. Both exempt files are edited by
// ordinary work — a case inserted above a fixture moves every line below it —
// and a stale path:line key does not merely fail: it fails OPEN, silently
// exempting whichever line inherited the number while permitting the literal
// this census exists to catch. Nothing else in either file needs scanning, so
// the path key gives up no coverage the line keys were buying.
var breakpointLiteralAllowlist = map[string]string{
	"web/src/lib/hooks/mediaQuery.svelte.ts": "the DESKTOP_MEDIA_QUERY declaration — THE single source of truth for the " +
		"JS half of the bridge. Tailwind v4 compiles --breakpoint-md away at build time and does not " +
		"emit it to :root, so the JS side provably cannot read the token at runtime and one authored " +
		"number is unavoidable; forbidding it would forbid the fix",
	"web/src/lib/hooks/mediaQuery.svelte.test.ts": "two kinds of line, both in the one file whose whole job is that constant. " +
		"The expect(DESKTOP_MEDIA_QUERY).toBe(...) assertion PINS the exempted declaration — it is the " +
		"guard on that line, not a second copy of it, and it is why a silent edit to the hook fails " +
		"`task test`. The mediaQuery('(min-width: 768px)') fixtures are test INPUTS to the hook under " +
		"test, not declarations of a boundary any surface renders at",
}

// isViewportDecidingLine reports whether a source line DECIDES a viewport band.
//
// Three clauses, and the third is load-bearing rather than redundant. Without
// it the scan reaches only CSS rules and a `matchMedia(` call, and a media
// condition STRING in a .ts file — precisely the form the JS half of this
// bridge takes — is invisible. With it, a hand-written '(min-width: 768px)'
// anywhere under web/src is caught, which is also what makes the allowlist an
// enumerated decision rather than an accident of what the predicate misses.
func isViewportDecidingLine(line string) bool {
	if strings.HasPrefix(strings.TrimSpace(line), "@media") {
		return true
	}
	if strings.Contains(line, "matchMedia(") {
		return true
	}
	return strings.Contains(line, "(min-width:") || strings.Contains(line, "(max-width:")
}

// lineCarriesForbiddenWidth reports whether a line contains any forbidden width
// as a SUBSTRING.
//
// Substring containment is the right predicate here and a word-boundary regex
// is not needed, because the scan is already narrowed to viewport-deciding
// lines: a media condition carrying those digits is a banded rule by
// construction. Do not "tighten" this into something that stops matching.
func lineCarriesForbiddenWidth(line string) bool {
	for _, w := range forbiddenViewportWidths {
		if strings.Contains(line, w) {
			return true
		}
	}
	return false
}

// referenceDirectiveFor returns the `@reference` line a Svelte scoped style
// block at path must carry for Tailwind to resolve theme() inside it.
//
// The path is depth-derived and NOT uniform: this tree has files two, three and
// four directories below web/src, so a single constant would be wrong for
// TopBar.svelte and for the two nested characters/ route files.
func referenceDirectiveFor(t *testing.T, repoRelPath string) string {
	t.Helper()
	rel, err := filepath.Rel(webSrcDir, repoRelPath)
	require.NoError(t, err, "path %q must sit under %s", repoRelPath, webSrcDir)
	dir := filepath.ToSlash(filepath.Dir(rel))
	depth := strings.Count(dir, "/") + 1
	return `@reference "` + strings.Repeat("../", depth) + `app.css";`
}

// TestNoAuthoredViewportRuleCarriesAHandWrittenBreakpointLiteral asserts that no
// viewport-deciding line under web/src holds a hand-written md or lg breakpoint
// width, in either spelling, outside an enumerated allowlist.
//
// # What this buys
//
// The md and lg breakpoints were duplicated across seventeen authored sites —
// sixteen @media rules plus one matchMedia query string — with nothing keeping
// them equal. They fired together by coincidence, not by construction. Every
// rule now derives from Tailwind's own --breakpoint-md / --breakpoint-lg, and
// this census is what stops a new hand-written copy from reappearing.
//
// # Comment immunity, and its limit
//
// The predicate is line-shaped, so prose that names a boundary in words —
// "collapses below 768px", "rendered only inside the 1023px band" — is never a
// viewport-deciding line and is never flagged. The comments explaining this
// mechanism therefore cannot invalidate it. But a comment that QUOTES a media
// condition verbatim contains `(max-width:` and IS flagged, which is the
// correct outcome: a quoted rule text is a copy that goes stale when the rule
// changes.
//
// # What this census does NOT cover
//
// It is a guard over SOURCE TEXT, not over rendered layout. It cannot see
// whether the rules fire at the same width in a browser — that is the boundary
// block in web/e2e/admin-portal.spec.ts. And it cannot tie the one deliberately
// allowlisted authored number, DESKTOP_MEDIA_QUERY, to a token the build
// compiles away; what it does instead is force that number to exist in exactly
// one place, pinned by exactly one assertion. See breakpointLiteralAllowlist.
func TestNoAuthoredViewportRuleCarriesAHandWrittenBreakpointLiteral(t *testing.T) {
	root := findRepoRoot(t)
	walkRoot := filepath.Join(root, webSrcDir)

	// A moved or renamed source tree must fail loudly. Without this the walk
	// yields no offenders AND no scanned lines, and the census reports success
	// for having looked at nothing.
	require.DirExists(t, walkRoot,
		"the authored web source tree is the walk root; if it moved, repoint webSrcDir")

	scannedExtensions := map[string]struct{}{".svelte": {}, ".css": {}, ".ts": {}}

	var offenders []string
	scanned := 0
	allowlistHits := map[string]int{}

	err := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if _, ok := scannedExtensions[filepath.Ext(d.Name())]; !ok {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)
		content, readErr := os.ReadFile(path) //nolint:gosec // walk-derived path under the repo root
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(content), "\n") {
			if !isViewportDecidingLine(line) {
				continue
			}
			scanned++
			if !lineCarriesForbiddenWidth(line) {
				continue
			}
			if _, allowed := breakpointLiteralAllowlist[relSlash]; allowed {
				allowlistHits[relSlash]++
				continue
			}
			offenders = append(offenders,
				relSlash+":"+strconv.Itoa(i+1)+"  "+strings.TrimSpace(line))
		}
		return nil
	})
	require.NoError(t, err, "walk %s", webSrcDir)
	sort.Strings(offenders)

	require.Greater(t, scanned, 15,
		"only %d viewport-deciding lines were scanned under %s — a bad walk root, a "+
			"renamed extension set or a predicate that stopped matching all yield zero "+
			"offenders, and without this floor the census reports success for having "+
			"looked at nothing", scanned, webSrcDir)

	// The allowlist must not widen silently. An entry that no longer names a real
	// file, or that names one carrying no forbidden literal, is a standing
	// exemption over a file nobody is checking — the same fail-open shape the
	// path key closes, one level up.
	//
	// This runs BEFORE the offender assertion on purpose: a stale allowlist
	// invalidates what the offender list MEANS, so it must be reported first and
	// must stay reachable while offenders are outstanding.
	for path, reason := range breakpointLiteralAllowlist {
		require.FileExists(t, filepath.Join(root, path),
			"allowlisted path %q no longer exists; its exemption (%s) is dead and must be removed",
			path, reason)
		require.Positive(t, allowlistHits[path],
			"allowlisted path %q carries no viewport-deciding line with a forbidden width; "+
				"its exemption (%s) is stale and must be removed", path, reason)
	}

	require.Empty(t, offenders,
		"viewport-deciding line(s) carry a hand-written breakpoint width:\n  %s\n\n"+
			"A viewport width written by hand is a copy of a Tailwind default that nothing "+
			"keeps in step. Derive it: `@media (width < theme(--breakpoint-md))` or "+
			"`@media (width >= theme(--breakpoint-md))`, with an `@reference` line at the top "+
			"of the style block. The JS half reads DESKTOP_MEDIA_QUERY from "+
			"web/src/lib/hooks/mediaQuery.svelte.ts.",
		strings.Join(offenders, "\n  "))
}

// TestEveryBandedViewportRuleDerivesItsWidthFromTheTailwindToken asserts that
// every enumerated rule is written in the token form AND that its file carries
// the `@reference` directive that lets Tailwind resolve theme() inside a Svelte
// scoped style block.
//
// The directive is not optional and not decorative: svelte.config.js configures
// no vitePreprocess, so a component style block reaches @tailwindcss/vite as a
// standalone stylesheet with no theme loaded, and theme() there fails the build
// with "Could not resolve value for theme function". That is a loud failure
// rather than a silently dropped rule, which is what makes the conversion safe.
func TestEveryBandedViewportRuleDerivesItsWidthFromTheTailwindToken(t *testing.T) {
	root := findRepoRoot(t)
	require.NotEmpty(t, bandedViewportRules, "the checked-in rule census must not be empty")

	for _, r := range bandedViewportRules {
		abs := filepath.Join(root, r.path)
		// A moved file must FAIL rather than silently drop out of coverage.
		require.FileExists(t, abs, "enumerated viewport rule lives in %s", r.path)

		content, err := os.ReadFile(abs)
		require.NoError(t, err, "read %s", r.path)
		src := string(content)

		require.Contains(t, src, r.rule,
			"%s must carry the token-derived rule %q — a hand-written width is a copy of a "+
				"Tailwind default that nothing keeps in step", r.path, r.rule)

		directive := referenceDirectiveFor(t, r.path)
		require.Contains(t, src, directive,
			"%s must carry %q inside its style block, or theme() cannot resolve there",
			r.path, directive)
	}
}
