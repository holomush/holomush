// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// webPackageJSON is the web client's manifest, relative to the repo root. It
// holds the `browserslist` key this file pins.
const webPackageJSON = "web/package.json"

// This file is NEW coverage, not a second guard over an existing property.
// Nothing in the tree asserted anything about web/package.json before it.
//
// # What it guards, and why the property exists at all
//
// All sixteen authored viewport rules under web/src ship as CSS media query
// RANGE syntax (`@media (width >= theme(--breakpoint-md))` and its `width <`
// complement). A media query containing a feature the browser cannot parse
// evaluates to `unknown` and never matches — it does not degrade, it dies. On
// a browser below the range-syntax boundary the persistent rail never
// collapses on a phone, `.mobile-only` never hides on a desktop, and the admin
// nav never narrows. The pre-conversion `min-width`/`max-width` forms worked
// everywhere, and the diff that converted them named no trade.
//
// That made the supported browser floor a DERIVED value — an unnamed
// consequence of whichever CSS spelling the last conversion happened to pick.
// The `browserslist` key makes it CHOSEN. This test keeps it chosen: the floor
// cannot quietly disappear the way it quietly appeared.
//
// Gap G-06.1-6 in
// .planning/phases/06.1-admin-portal-web-surface-shadcn-components-and-the-single-ro/06.1-VERIFICATION.md.
//
// # Where the declared numbers come from
//
// web/package.json is JSON and admits no comment, so the derivation lives here.
//
// THE RULE, which is the durable part: the floor is the Vite baseline target,
// raised per-browser wherever a shipped dependency binds higher. A future Vite
// bump follows that procedure rather than reading a frozen literal out of this
// comment.
//
// SOURCE — Vite 8 defaults `build.target` to "baseline-widely-available"
// (no override exists in web/vite.config.ts), which resolves to
// ESBUILD_BASELINE_WIDELY_AVAILABLE_TARGET in
// web/node_modules/vite/dist/node/chunks/node.js:610 —
// chrome111, edge111, firefox114, safari16.4, ios16.4. That is what this build
// already transpiles JS to, so it is a floor the toolchain owns rather than a
// hand-picked number.
//
// RAISE — the built stylesheet uses `@property` 59 times, unguarded. MDN gives
// `@property` as Chrome 85 / Firefox 128 / Safari 16.4 / iOS Safari 16.4.
// Firefox 114-127 executes the transpiled JS perfectly well and still fails on
// the CSS, so Vite's firefox114 is raised to firefox128. Declaring 114 would
// publish a support contract the stylesheet cannot honour.
//
// NOT raised, and why — each of these was checked at its call site in the built
// output rather than assumed from its presence:
//
//   - `color-mix()` (130 uses) emits a hex fallback and is wrapped in
//     `@supports (color: color-mix(in lab, red, red))`. It degrades.
//   - `Element.checkVisibility()` (3 uses) is feature-detected
//     (`` `checkVisibility` in e ``) inside tabbable's opt-in `full-native`
//     display-check mode. An unsupporting browser takes the other branch.
//   - `requestIdleCallback` (1 use) is shimmed: `requestIdleCallback || setTimeout`.
//   - `text-wrap: balance` and `field-sizing: content` are Tailwind utilities
//     that degrade to ordinary layout. Tailwind documents both as opt-in
//     bleeding-edge rather than as baseline.
//   - `popover` (7 CSS hits) is not the Popover API. Every hit is a shadcn
//     design token — `--color-popover`, `.bg-popover`. The built JS contains no
//     `showPopover`, `hidePopover` or `popover=` at all.
//   - `:has()`, `@container`, `structuredClone`, `Object.hasOwn`,
//     `ResizeObserver`, `IntersectionObserver` and the media range syntax
//     itself all have boundaries at or below the floor above, so none binds.
//
// # What this test does NOT cover
//
// It does not read the built stylesheet, and it deliberately does not duplicate
// its neighbour. test/meta/web_phone_band_breakpoint_census_test.go owns the
// SPELLING of the authored rules — that each derives its width from a Tailwind
// token, and how many times each appears. This file owns only the DECLARATION
// of the floor those rules require. Deleting a banded rule is the census's
// failure; deleting the floor is this one's.
//
// It also does not prove the declared floor is CORRECT — no test can, because
// correctness here is a product decision about who the product says it serves.
// It proves the floor is DECLARED, non-empty, states versions a reader can
// check a support statement against, and is not below the `@property` boundary
// the shipped stylesheet demonstrably requires.
//
// This file carries NO invariant-binding annotation of any kind. No registry
// invariant covers this property; per .claude/rules/invariants.md a fabricated
// binding is a false green, and one is allocated deliberately and shipped
// `binding: pending` rather than bound to a test on sight.
//
// That sentence deliberately does not SPELL the annotation token. The plan's
// acceptance criterion greps this file for it and requires no match, so prose
// mentioning it verbatim would fail the check on the strength of a comment —
// the same defect 06.1-08 hit when one marker comment named its counterpart.

// propertyBoundFloors are the per-browser minimums forced by the built
// stylesheet's 59 unguarded `@property` rules, keyed by browserslist browser
// name. These are asserted as FLOORS (at least this), never as equalities, so
// raising the declared floor stays legal and only lowering it below what the
// CSS requires fails.
//
// Chrome and Edge are deliberately absent. Their declared 111 comes from Vite's
// baseline target, which legitimately moves on a Vite major; `@property` itself
// is Chrome 85 and so binds nothing there. Pinning 111 here would freeze a
// literal the rule above says to re-derive.
var propertyBoundFloors = map[string]string{
	"firefox": "128",
	"safari":  "16.4",
	"ios_saf": "16.4",
}

// unpinnableQueryPrefixes open a browserslist query that names no version a
// support statement can be checked against. `> 0.5%` and `last 2 versions`
// resolve against caniuse usage data that moves under the project's feet, and
// `not ie 11` subtracts rather than declaring a floor.
var unpinnableQueryPrefixes = []string{">", "<", "last ", "not "}

func TestWebPackageDeclaresAnExplicitBrowserFloorForRangeSyntaxMediaQueries(t *testing.T) {
	t.Parallel()

	root := findRepoRoot(t)
	manifest := filepath.Join(root, webPackageJSON)

	// A moved or renamed manifest must fail LOUDLY. Without this the read below
	// would hand back an error that a permissive unmarshal could turn into a nil
	// browserslist, and the test would stop asserting instead of failing.
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("%s is missing or unreadable (%v).\n"+
			"This guard pins the web client's declared browser floor and cannot do so "+
			"from a manifest it cannot find. If the manifest moved, move this constant "+
			"with it — do not delete the assertion.", webPackageJSON, err)
	}

	raw, err := os.ReadFile(manifest)
	require.NoError(t, err, "reading %s", webPackageJSON)

	// A POINTER slice separates the two failure modes the mutations exercise:
	// nil means the key is absent, non-nil-but-empty means it is present and
	// vacuous. Collapsing them would report a deletion as an emptying.
	var pkg struct {
		Browserslist *[]string `json:"browserslist"`
	}
	require.NoError(t, json.Unmarshal(raw, &pkg), "parsing %s as JSON", webPackageJSON)

	const why = "The sixteen banded viewport rules under web/src ship as CSS media query " +
		"RANGE syntax. A browser that cannot parse a range condition evaluates the query " +
		"to `unknown` and NEVER matches it, so the responsive layout is dead rather than " +
		"degraded — the rail does not collapse on a phone and the admin nav does not " +
		"narrow. The floor those rules require must therefore be a declared decision, not " +
		"an unnamed consequence of a CSS spelling. See 06.1-VERIFICATION.md gap G-06.1-6."

	require.NotNil(t, pkg.Browserslist,
		"%s declares no `browserslist` key.\n%s", webPackageJSON, why)
	require.NotEmpty(t, *pkg.Browserslist,
		"%s declares an EMPTY `browserslist`, which pins nothing.\n%s", webPackageJSON, why)

	declared := map[string]string{}
	for _, entry := range *pkg.Browserslist {
		q := strings.TrimSpace(entry)

		require.NotEqual(t, "defaults", strings.ToLower(q),
			"%s declares the browserslist entry %q, which names no version.\n"+
				"`defaults` resolves against caniuse usage data that moves without a commit "+
				"here, so it cannot be checked against a published support statement.\n%s",
			webPackageJSON, entry, why)

		for _, bad := range unpinnableQueryPrefixes {
			require.False(t, strings.HasPrefix(strings.ToLower(q), bad),
				"%s declares the browserslist entry %q, which opens with %q and so names no "+
					"explicit minimum version.\nDeclare `<browser> >= <version>` instead: a "+
					"support contract has to name a version a reader can check.\n%s",
				webPackageJSON, entry, bad, why)
		}

		name, version, ok := strings.Cut(q, ">=")
		require.True(t, ok,
			"%s declares the browserslist entry %q, which this guard cannot read as a "+
				"floor.\nEvery entry must take the form `<browser> >= <version>`; the `>=` "+
				"spelling is load-bearing, because bare `chrome 111` means ONLY 111 to "+
				"browserslist while `chrome >= 111` is the floor this key exists to state.\n%s",
			webPackageJSON, entry, why)

		name = strings.TrimSpace(name)
		version = strings.TrimSpace(version)
		require.NotEmpty(t, name, "%s: browserslist entry %q names no browser", webPackageJSON, entry)
		require.True(t, strings.ContainsAny(version, "0123456789"),
			"%s: browserslist entry %q carries no version number after `>=`.\n%s",
			webPackageJSON, entry, why)

		// A REPEATED browser is rejected outright rather than overwritten.
		//
		// browserslist evaluates its array as a UNION, so the effective floor for
		// a browser declared twice is the LOWER of the two, not the last one
		// written. Measured: `browserslist "safari >= 15, safari >= 16.4"`
		// resolves down to safari 15, while `browserslist "safari >= 16.4"` stops
		// at 16.4. A plain last-wins map therefore fails OPEN in one of the two
		// orderings — append `safari >= 15` ABOVE the 16.4 line, which is what a
		// tidy alphabetical or append-at-top edit produces, and the map reads
		// "16.4", versionAtLeast passes, and this guard reports success over a
		// support contract the shipped stylesheet cannot honour. That is the
		// defect class the whole file exists to prevent.
		//
		// Taking the MINIMUM instead of failing would also be sound arithmetic,
		// but it would leave the manifest saying two different things about one
		// browser. One entry per browser is the property worth holding.
		key := strings.ToLower(name)
		if prior, dup := declared[key]; dup {
			require.Failf(t, "duplicate browserslist floor",
				"%s declares a floor for %q twice (%s and %s).\nbrowserslist UNIONS its queries, "+
					"so the EFFECTIVE floor is the LOWER of the two — and which one this guard "+
					"would have read depends on the order they happen to be written in, so a "+
					"lowered floor can pass green. Declare exactly one entry per browser.\n%s",
				webPackageJSON, name, prior, version, why)
		}
		declared[key] = version
	}

	// The floor may be RAISED freely; it may not sit below what the shipped
	// stylesheet requires. A declared value that the CSS cannot honour is a
	// false support contract, which is the defect class this whole guard exists
	// to prevent rather than merely to document.
	for browser, minimum := range propertyBoundFloors {
		got, ok := declared[browser]
		require.True(t, ok,
			"%s declares no floor for %q.\nThe built stylesheet uses `@property` 59 times "+
				"unguarded, and %s supports it only from %s, so the floor has to say so.\n%s",
			webPackageJSON, browser, browser, minimum, why)

		require.True(t, versionAtLeast(got, minimum),
			"%s declares %s >= %s, which is BELOW the %s floor the shipped stylesheet "+
				"requires.\nThe built CSS uses `@property` 59 times unguarded and MDN gives "+
				"`@property` as Firefox 128 / Safari 16.4 / iOS Safari 16.4. A browser below "+
				"that runs the transpiled JS fine and still fails on the CSS, so declaring a "+
				"lower number publishes a support contract the stylesheet cannot honour.\n"+
				"Raise the declaration, or — if a dependency genuinely dropped the "+
				"requirement — re-derive the floor per the procedure in this file's doc "+
				"comment and update %s with the evidence.",
			webPackageJSON, browser, got, minimum, "propertyBoundFloors")
	}
}

// versionAtLeast reports whether the dotted numeric version got is greater than
// or equal to want, comparing component-wise so that 16.4 >= 16.4 and 17 > 16.4
// both hold. A component that does not parse compares as 0, which fails closed:
// an unreadable version is treated as below the floor rather than waved through.
func versionAtLeast(got, want string) bool {
	g := strings.Split(got, ".")
	w := strings.Split(want, ".")
	for i := 0; i < len(g) || i < len(w); i++ {
		gv, wv := 0, 0
		if i < len(g) {
			gv, _ = strconv.Atoi(strings.TrimSpace(g[i]))
		}
		if i < len(w) {
			wv, _ = strconv.Atoi(strings.TrimSpace(w[i]))
		}
		if gv != wv {
			return gv > wv
		}
	}
	return true
}
