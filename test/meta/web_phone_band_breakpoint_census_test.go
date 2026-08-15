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

// tokenBreakpointPrefix is the opening of the Tailwind theme lookup every
// converted rule reads its width from. A line carrying it is BAND-RELEVANT: it
// decides the md or lg band even though it holds no width literal at all.
const tokenBreakpointPrefix = "theme(--breakpoint-"

// bandedViewportRule names one authored viewport rule, the token form it is
// written as, and HOW MANY TIMES that exact rule text appears in that file. The
// slice below is the checked-in census of every such rule under webSrcDir.
//
// count exists because containment cannot count. A file carrying the same rule
// text twice used to be listed twice, and the second entry re-ran the first
// assertion verbatim — deleting one of the two rules left the census green. The
// count field is what turns "this text appears" into "this text appears exactly
// n times", which is the property the duplicate entry was pretending to assert.
type bandedViewportRule struct {
	path  string
	rule  string
	count int
}

// bandedViewportRules is the enumerated set. It is checked in rather than
// derived, because the property under test is that a KNOWN rule still reads the
// token — a derived set would go quiet the moment a rule was deleted.
var bandedViewportRules = []bandedViewportRule{
	{"web/src/lib/components/shell/SectionRail.svelte", ruleBelowMd, 1},
	{"web/src/lib/components/shell/SectionRail.svelte", ruleBelowLg, 1},
	{"web/src/routes/(authed)/admin/+layout.svelte", ruleBelowLg, 1},
	{"web/src/routes/(authed)/admin/+layout.svelte", ruleBelowMd, 1},
	{"web/src/lib/components/admin/AdminNav.svelte", ruleBelowLg, 1},
	{"web/src/lib/components/admin/CharacterTable.svelte", ruleBelowMd, 1},
	{"web/src/lib/components/admin/CharacterFilterBar.svelte", ruleBelowMd, 1},
	{"web/src/lib/components/admin/EditCharacterSheet.svelte", ruleBelowMd, 1},
	{"web/src/lib/components/sidebar/Sidebar.svelte", ruleBelowMd, 1},
	{"web/src/routes/c/[id]/+page.svelte", ruleAtOrAboveM, 1},
	{"web/src/routes/(authed)/characters/[id]/+page.svelte", ruleAtOrAboveM, 1},
	{"web/src/routes/(authed)/characters/new/+page.svelte", ruleAtOrAboveM, 1},
	{"web/src/routes/(authed)/characters/+page.svelte", ruleAtOrAboveM, 1},
	{"web/src/lib/components/characters/CharacterRoster.svelte", ruleAtOrAboveM, 1},
	// TopBar carries TWO rules in ONE style block — the kbd-hint reveal and the
	// mobile-only hide — written with identical text. ONE entry with count 2,
	// not two entries: the pair used to repeat, and because the assertion was a
	// substring test the second entry asserted nothing the first had not.
	{"web/src/lib/components/TopBar.svelte", ruleAtOrAboveM, 2},
}

// forbiddenViewportWidths are the strings a hand-written md or lg breakpoint
// takes. Four decimals, in BOTH px spellings — the `max-width` complements
// (767, 1023) and the `min-width` forms (768, 1024) — plus the two rem
// spellings the Tailwind tokens compile to (48rem, 64rem).
//
// There is no exemption by spelling. `min-width: 768px` is exactly as much a
// hand-written copy of --breakpoint-md as `max-width: 767px` is, and this tree
// carried the two in roughly equal number before the conversion. A census that
// forbade only one would license the other one directory over.
//
// The rem forms are here because they are now the VISIBLE form of these
// breakpoints: `theme(--breakpoint-md)` compiles to `48rem` in the emitted
// stylesheet, and a reader who inspects the build and copies what they see
// types `48rem`, not `768px`. That makes the rem spelling the most likely
// future hand-written duplication, and a census that forbade only the retired
// px spelling would license the live one.
var forbiddenViewportWidths = []string{"767", "768", "1023", "1024", "48rem", "64rem"}

// breakpointExemption is one allowlist entry: the SYMBOL whose declaration and
// pin are permitted to carry a forbidden width, and why.
//
// The symbol — not the file — is the unit of exemption. A file-wide exemption
// would silently absorb a SECOND hand-written media-query constant added beside
// the first, which is exactly the duplication this census exists to catch.
type breakpointExemption struct {
	symbol string
	reason string
}

// breakpointLiteralAllowlist maps a repo-relative PATH to the exemption that
// applies inside it. A line in an allowlisted file is exempt only when it also
// carries the exemption's symbol; any other forbidden literal in that same file
// is an ordinary offender.
//
// It is keyed by PATH, not by path:line. Both exempt files are edited by
// ordinary work — a case inserted above a fixture moves every line below it —
// and a stale path:line key does not merely fail: it fails OPEN, silently
// exempting whichever line inherited the number while permitting the literal
// this census exists to catch. The symbol scope inside the file is what the
// line keys were really buying, and it moves with the constant.
var breakpointLiteralAllowlist = map[string]breakpointExemption{
	"web/src/lib/hooks/mediaQuery.svelte.ts": {
		symbol: "DESKTOP_MEDIA_QUERY",
		reason: "the DESKTOP_MEDIA_QUERY declaration — THE single source of truth for the " +
			"JS half of the bridge. SSR and this jsdom have no stylesheet to read, so the " +
			"non-browser path needs a literal and one authored value is unavoidable; forbidding " +
			"it would forbid the fix. That literal is pinned to the built Tailwind token by the " +
			"browser proof in web/e2e/admin-band-root-font.spec.ts, which reads --breakpoint-md " +
			"off :root at runtime and asserts the two halves are exact complements, and it is " +
			"pinned to ONE place by this census",
	},
	"web/src/lib/hooks/mediaQuery.svelte.test.ts": {
		symbol: "DESKTOP_MEDIA_QUERY",
		reason: "the expect(DESKTOP_MEDIA_QUERY).toBe(...) assertion PINS the exempted " +
			"declaration — it is the guard on that line, not a second copy of it, and it is why " +
			"a silent edit to the hook fails `task test`. The hook's other fixtures pass an " +
			"arbitrary off-band width, because they are test INPUTS to the hook under test, not " +
			"declarations of a boundary any surface renders at",
	},
}

// isViewportDecidingLine reports whether a source line DECIDES a viewport band.
//
// Five clauses. The third is load-bearing rather than redundant: without it the
// scan reaches only CSS rules and a `matchMedia(` call, and a media condition
// STRING in a .ts file — precisely the form the JS half of this bridge takes —
// is invisible. With it, a hand-written '(min-width: 768px)' anywhere under
// web/src is caught, which is also what makes the allowlist an enumerated
// decision rather than an accident of what the predicate misses.
//
// The fourth and fifth clauses close two shapes that decide the same band while
// satisfying none of the first three:
//
//   - `min-[` / `max-[` — a Tailwind v4 arbitrary variant, `class="min-[768px]:flex"`.
//     It is a media query written in the class attribute, with no `@media` and no
//     parenthesised feature name. No such class exists under web/src today, so
//     this clause is prospective coverage rather than a live escape being closed.
//
//   - `window.innerWidth` / `window.outerWidth` — a JS width comparison branching
//     on the same boundary without a media query. Unlike the arbitrary variant,
//     this shape DOES occur today: four lines (three in
//     lib/components/terminal/Composer.svelte, one in lib/sentry.ts). All four are
//     positioning or telemetry arithmetic and none carries a band width, so they
//     add to the screened tally and to no offender list.
//
// That last case is the predicate working as designed, not a false positive.
// This is a SCREEN, deliberately wider than the property: a line reaches the
// offender list only if it also carries a forbidden width, and reaches the
// band-relevant tally only if it carries a forbidden width or a breakpoint
// token. Widening the screen costs nothing and narrowing it is how a shape
// escapes, so do not "tighten" a clause to exclude arithmetic.
func isViewportDecidingLine(line string) bool {
	if strings.HasPrefix(strings.TrimSpace(line), "@media") {
		return true
	}
	if strings.Contains(line, "matchMedia(") {
		return true
	}
	if strings.Contains(line, "(min-width:") || strings.Contains(line, "(max-width:") {
		return true
	}
	if strings.Contains(line, "min-[") || strings.Contains(line, "max-[") {
		return true
	}
	return strings.Contains(line, "window.innerWidth") || strings.Contains(line, "window.outerWidth")
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
//
// filepath.Dir renders a file sitting DIRECTLY under web/src as ".", for which
// `strings.Count(".", "/") + 1` is 1 — one parent segment too many, pointing at
// web/app.css. No such file carries a banded rule today, so the fix is not a
// repair of a live failure; it is why one can be added later without a puzzling
// build error that names a path nobody wrote.
func referenceDirectiveFor(t *testing.T, repoRelPath string) string {
	t.Helper()
	rel, err := filepath.Rel(webSrcDir, repoRelPath)
	require.NoError(t, err, "path %q must sit under %s", repoRelPath, webSrcDir)
	dir := filepath.ToSlash(filepath.Dir(rel))
	depth := 0
	if dir != "." {
		depth = strings.Count(dir, "/") + 1
	}
	return `@reference "` + strings.Repeat("../", depth) + `app.css";`
}

// TestReferenceDirectiveForYieldsOneParentSegmentPerDirectoryBelowWebSrc pins
// the depth arithmetic at every level this tree uses, including the depth-0 case
// that has no file yet. A helper whose contract is only ever exercised through
// the files that happen to exist cannot tell a reader which answer is correct
// for the file they are about to add.
func TestReferenceDirectiveForYieldsOneParentSegmentPerDirectoryBelowWebSrc(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "a file directly under web/src references app.css as a sibling, with no parent segment",
			path: "web/src/app.css",
			want: `@reference "app.css";`,
		},
		{
			name: "a file one directory below web/src climbs one level",
			path: "web/src/lib/probe.svelte",
			want: `@reference "../app.css";`,
		},
		{
			name: "a file two directories below web/src climbs two levels",
			path: "web/src/lib/components/TopBar.svelte",
			want: `@reference "../../app.css";`,
		},
		{
			name: "a file four directories below web/src climbs four levels",
			path: "web/src/routes/(authed)/characters/[id]/+page.svelte",
			want: `@reference "../../../../app.css";`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, referenceDirectiveFor(t, tc.path),
				"%s sits %d directories below %s", tc.path,
				strings.Count(tc.want, "../"), webSrcDir)
		})
	}
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
// block in web/e2e/admin-portal.spec.ts, and, at a NON-default root font size
// where the two units can diverge, web/e2e/admin-band-root-font.spec.ts. Nor
// does it tie the one deliberately allowlisted authored value,
// DESKTOP_MEDIA_QUERY, to the token the build emits; that tie is the browser
// proof's job. What this census does instead is force that value to exist in
// exactly one place, pinned by exactly one assertion, in the same unit as the
// CSS half. See breakpointLiteralAllowlist.
func TestNoAuthoredViewportRuleCarriesAHandWrittenBreakpointLiteral(t *testing.T) {
	root := findRepoRoot(t)
	walkRoot := filepath.Join(root, webSrcDir)

	// A moved or renamed source tree must fail loudly. Without this the walk
	// yields no offenders AND no scanned lines, and the census reports success
	// for having looked at nothing.
	require.DirExists(t, walkRoot,
		"the authored web source tree is the walk root; if it moved, repoint webSrcDir")

	// The extension filter is INVERTED: every extension is classified, as either
	// scanned or deliberately skipped, and one in neither set fails the test.
	//
	// The previous allow-set failed OPEN. A `.tsx`, `.scss`, `.js` or `.postcss`
	// file added under web/src later simply did not match, so it was skipped with
	// no signal at all — and the hand-picked floor this walk used to carry was far
	// too loose to notice a whole language dropping out of coverage. Failing on an
	// unrecognised extension converts that silence into a one-line decision for
	// whoever adds the file: scan it, or say why not.
	scannedExtensions := map[string]struct{}{".svelte": {}, ".css": {}, ".ts": {}}
	skippedExtensions := map[string]struct{}{
		".json": {}, ".html": {}, ".svg": {}, ".png": {}, ".ico": {}, ".webp": {},
		".avif": {}, ".woff": {}, ".woff2": {}, ".map": {}, ".md": {}, ".snap": {},
	}

	var offenders []string
	var unclassified []string
	scanned := 0
	bandRelevant := 0
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
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)

		ext := filepath.Ext(d.Name())
		if ext == "" {
			// An extensionless file (a LICENSE, a dotfile) carries no authored
			// stylesheet or module and needs no classification decision.
			return nil
		}
		if _, skip := skippedExtensions[ext]; skip {
			return nil
		}
		if _, scan := scannedExtensions[ext]; !scan {
			unclassified = append(unclassified, relSlash+"  (extension "+ext+")")
			return nil
		}

		content, readErr := os.ReadFile(path) //nolint:gosec // walk-derived path under the repo root
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(content), "\n") {
			if !isViewportDecidingLine(line) {
				continue
			}
			scanned++
			forbidden := lineCarriesForbiddenWidth(line)
			if forbidden || strings.Contains(line, tokenBreakpointPrefix) {
				bandRelevant++
			}
			if !forbidden {
				continue
			}
			// Symbol-scoped, not file-scoped: a forbidden width on a line that
			// does NOT carry the exempted symbol is an ordinary offender even
			// inside an allowlisted file. A hit therefore means "a line
			// carrying the exempted symbol AND a forbidden width".
			if ex, allowed := breakpointLiteralAllowlist[relSlash]; allowed && strings.Contains(line, ex.symbol) {
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
	sort.Strings(unclassified)

	// Reported FIRST, because an unclassified file means the corpus measurement
	// below is incomplete — the floor and the offender list are both statements
	// about a walk that provably did not reach everything it passed.
	require.Empty(t, unclassified,
		"file(s) under %s carry an extension this census neither scans nor skips:\n  %s\n\n"+
			"Classify each one. If it can hold an authored viewport rule (a .tsx, .scss, .js, "+
			".postcss), add its extension to scannedExtensions; if it cannot, add it to "+
			"skippedExtensions. Doing neither is how a whole language leaves coverage in "+
			"silence, which is the fail-open shape the old allow-set had.",
		webSrcDir, strings.Join(unclassified, "\n  "))

	// ANTI-VACUITY FLOOR, derived from this census's own checked-in claims.
	//
	// It is a SUM over count, not len(bandedViewportRules), because the census is
	// occurrence-counted: one entry with count 2 claims two rules in the corpus,
	// so the corpus size the census asserts is the sum of its counts.
	//
	// The tally is over BAND-RELEVANT lines — a viewport-deciding line carrying a
	// forbidden width or a breakpoint token — and not over every media line. The
	// old floor counted `prefers-reduced-motion` and `prefers-color-scheme` rules
	// and every `matchMedia(` call site, none of which can ever hold a band
	// literal. It sat at 15 while the band-relevant corpus was 18: the walk could
	// have stopped reaching more than half this tree and still reported success.
	// (Measured: with lib/components excluded the screened count is 18 — still
	// over the old floor — while the band-relevant count falls to 8.)
	//
	// Two things a future reader will be tempted to "improve", and must not:
	//
	//  1. Allowlisted lines DO count here. The tally is over every band-relevant
	//     line the walk REACHED, and an exempted line is still a line it reached.
	//     Filtering the allowlist out would be a different measurement wearing this
	//     one's name. At the time of writing: 16 token lines + 2 allowlisted symbol
	//     lines = 18, against a claimed sum of 16.
	//
	//  2. The floor is INTENDED to sit close to the tally, and would still be
	//     correct at exactly zero headroom. It is an anti-vacuity floor, not a
	//     budget — its job is to fail when the walk stops reaching the corpus, and
	//     it does that best when it tracks the corpus. Deleting a rule therefore
	//     fails BOTH this floor and the occurrence assertion in the sibling test,
	//     producing two messages about one deletion. That redundancy is correct and
	//     deliberate. Do NOT tune it away by subtracting a margin, by filtering the
	//     tally, or by loosening the comparison; that is relaxing a guard rather
	//     than repairing it.
	ruleCensusOccurrences := 0
	for _, r := range bandedViewportRules {
		ruleCensusOccurrences += r.count
	}
	require.GreaterOrEqual(t, bandRelevant, ruleCensusOccurrences,
		"the walk reached %d band-relevant viewport line(s) under %s but this census claims "+
			"%d rule occurrence(s) live there, so the walk is not reaching its own corpus — a "+
			"moved walk root, a directory added to skipDirs, or a predicate that stopped "+
			"matching. (%d viewport-deciding line(s) were screened in total; that wider number "+
			"is context, not the floor, because most media rules can never carry a band "+
			"literal.)",
		bandRelevant, webSrcDir, ruleCensusOccurrences, scanned)

	// The allowlist must not widen silently. An entry that no longer names a real
	// file, or that names one carrying no forbidden literal, is a standing
	// exemption over a file nobody is checking — the same fail-open shape the
	// path key closes, one level up.
	//
	// This runs BEFORE the offender assertion on purpose: a stale allowlist
	// invalidates what the offender list MEANS, so it must be reported first and
	// must stay reachable while offenders are outstanding.
	for path, ex := range breakpointLiteralAllowlist {
		abs := filepath.Join(root, path)
		require.FileExists(t, abs,
			"allowlisted path %q no longer exists; its exemption (%s) is dead and must be removed",
			path, ex.reason)

		content, readErr := os.ReadFile(abs)
		require.NoError(t, readErr, "read allowlisted path %q", path)
		require.Contains(t, string(content), ex.symbol,
			"allowlisted path %q no longer mentions %q — the exemption names a symbol the file "+
				"does not declare, so it is dead and must be removed (or repointed at the symbol "+
				"that replaced it). Exemption: %s",
			path, ex.symbol, ex.reason)

		// Symbol-scoped meaning: zero hits means %q is present but no longer
		// declares a width literal at all, at which point the exemption buys
		// nothing and is genuinely stale.
		//
		// A rem literal on that line satisfies this assertion exactly as a px
		// literal did — `48rem` is a forbidden width here just as `768` is.
		// This assertion is keyed to the SYMBOL, never to a spelling, so
		// rewriting the constant from px to rem (or back) leaves it green.
		// Read that sentence before concluding the guard rejects its own repair.
		require.Positive(t, allowlistHits[path],
			"allowlisted path %q declares %q but no line carrying that symbol also carries a "+
				"forbidden viewport width; the exemption buys nothing and is stale. (A rem width "+
				"such as 48rem counts here exactly as 768 does — this check is symbol-scoped, not "+
				"spelling-scoped.) Exemption: %s",
			path, ex.symbol, ex.reason)
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

		// A zero-count entry asserts nothing: strings.Count would have to equal 0,
		// which every file that does not carry the rule satisfies. Such an entry
		// would sit in the census looking like coverage while being a no-op, so it
		// is rejected outright rather than allowed to pass.
		require.Positive(t, r.count,
			"census entry %s / %q claims %d occurrence(s); an entry must claim at least one, "+
				"or it asserts nothing while appearing to", r.path, r.rule, r.count)

		content, err := os.ReadFile(abs)
		require.NoError(t, err, "read %s", r.path)
		src := string(content)

		// Occurrence EQUALITY, not containment. Containment cannot tell one
		// occurrence from two: when TopBar's two identical rules were listed as two
		// entries, deleting either of them left both assertions green because the
		// surviving one still satisfied both. Counting is what makes the second
		// rule genuinely covered.
		require.Equal(t, r.count, strings.Count(src, r.rule),
			"%s carries the token-derived rule %q %d time(s); this census claims %d. A "+
				"substring test cannot tell one occurrence from two, so the count is the "+
				"assertion: if a rule was deliberately removed, drop the count here in the same "+
				"change; if it was removed by accident, this is the accident. (A hand-written "+
				"width is a copy of a Tailwind default that nothing keeps in step.)",
			r.path, r.rule, strings.Count(src, r.rule), r.count)

		directive := referenceDirectiveFor(t, r.path)
		require.Contains(t, src, directive,
			"%s must carry %q inside its style block, or theme() cannot resolve there",
			r.path, directive)
	}
}
