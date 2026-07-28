// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// This file guards test/session-matrix.yaml — the committed session-lifecycle
// matrix registry. It landed WITH the registry rather than after it because
// three later plans consumed the registry as their division of labour: without
// a shape test they would be reading a table whose basic integrity rests on a
// human having counted 48 cells correctly once.
//
// WHAT THIS FILE NOW ENFORCES, and what it deliberately does not:
//
//   - SHAPE — 48 rows, unique ids, exactly one disposition per row, and the
//     exact population of every disposition. Pinning every count (not only the
//     not-applicable one) is what stops the bijection below being satisfied by
//     DOWNGRADING an uncovered `spec` row to an excuse disposition, which is
//     the cheapest way to make a coverage claim disappear.
//   - MARKER WELL-FORMEDNESS — a line that looks like a marker either IS one or
//     is documentation carrying the literal `<id>` placeholder. Without this a
//     typo'd or trailing-comment marker would simply not be seen, and "not
//     seen" reads identically to "not written".
//   - MARKER PLACEMENT — every marker sits directly above the `It(` it claims.
//     Without this the bijection is satisfiable by a comment parked anywhere in
//     the tree, which is a coverage claim backed by nothing.
//   - BIJECTION — the `spec` rows and the in-code markers are the same set, of
//     the same pinned size, with no duplicates on either side.
//   - POINTER RESOLUTION — every `spec` and `covered_by` citation names a file
//     that exists and container/name text that actually appears in it.
//
// What it does NOT check: the TRUTH of a row's prose. `notes`, `reason` and
// `gap_notes` are free text, and a row whose disposition and marker agree can
// still describe the world wrongly. That is a human-review property; this file
// makes no claim about it, and a reader must not read a green run as one.
//
// It also carries NO invariant-registry binding annotation and no entry in
// docs/architecture/invariants.yaml — deliberately. The quarantine-registry
// bijection this guard is modelled on is likewise unregistered, and
// `.claude/rules/invariants.md` forbids annotating a test with a binding it does
// not genuinely prove.
//
// That absence was checked by the plan that introduced this file, by grepping
// it for the annotation's literal form. NO TEST IN THE TREE PERFORMS THAT
// GREP — it is a one-time, plan-level verification, and a reader must not take
// the paragraph above as describing a standing guard. Avoiding the literal here
// is still deliberate and still worth keeping: a needle that counts mentions
// cannot tell a real binding from prose about bindings, so spelling it out
// would have made the plan's own check useless from that commit onward (the
// defect 09-15 hit with `WithRealABAC`).

// sessionMatrixTotalPositions is 12 transitions x 4 transport columns. Every
// position gets a row, including the ones the source matrix marks n/a, so that
// a dropped cell is a count failure rather than a silent omission.
const sessionMatrixTotalPositions = 48

// sessionMatrixNotApplicable is the number of positions the source matrix
// (holomush-izk0, reproduced verbatim in 09-RESEARCH.md) marks `n/a`:
// multi-session on the fresh-select, detach-all, reaper-sweep,
// post-ttl-relogin and quit-command rows (5); web-guest on the admin-boot and
// move-arrival rows (2); and web-guest, web-char and multi-session on the
// telnet-only tmux-reattach row (3).
//
// Pinning the number is what stops a future edit from quietly reclassifying an
// awkward-to-cover cell as "not applicable" — the cheapest way to turn a
// coverage gap into an apparent non-question.
const sessionMatrixNotApplicable = 10

// sessionMatrixExpectedCounts pins the population of EVERY disposition, not
// only the not-applicable one.
//
// Why all five: the marker bijection below compares the `spec` rows against the
// in-code markers. The cheapest way to make an unbacked `spec` claim stop
// failing is not to write the missing spec — it is to relabel the row as
// `not-applicable`, `planned` or `not-implementable-from-harness-defaults` and
// let the bijection shrink to fit. Pinning each count makes that relabelling a
// build failure that names the disposition whose population moved. The grid is
// a fixed 12x4, so the totals cannot legitimately drift: covering a new cell
// means moving one row between two pinned counts, which is a deliberate,
// reviewable edit rather than an accident.
//
// `planned` is 1: reattach-cas.multi-session, blocked on a per-connection
// detach seam the harness does not have. It is the matrix's one genuinely
// uncovered cell, and it is REQUIRED to stay expressible — a guard that only
// admitted `spec` rows would force a false claim onto it.
var sessionMatrixExpectedCounts = map[string]int{
	"spec":              32,
	"covered-elsewhere": 2,
	"planned":           1,
	"not-applicable":    sessionMatrixNotApplicable,
	"not-implementable-from-harness-defaults": 3,
}

// sessionMatrixPayloadKeys maps each disposition to the single payload key it
// owns. A row MUST carry its own disposition's key and MUST NOT carry any of
// the other four. `notes` and `issue` are deliberately absent from this map:
// they are free-form annotations permitted on any row, so they cannot imply a
// disposition.
var sessionMatrixPayloadKeys = map[string]string{
	"spec":              "spec",
	"covered-elsewhere": "covered_by",
	"planned":           "owed_by",
	"not-applicable":    "reason",
	"not-implementable-from-harness-defaults": "gap_notes",
}

// sessionMatrixMarkerToken is what makes a source line a MARKER CANDIDATE. The
// colon is part of the token on purpose: prose that says "carries no matrix-row
// marker" is a mention, not a candidate, while `matrix-row:` anywhere on a line
// is someone trying to claim a cell and MUST therefore be well-formed.
const sessionMatrixMarkerToken = "matrix-row:"

// sessionMatrixMarkerPlaceholder is the literal a doc comment uses to describe
// the marker form without claiming a cell. It is not a valid id, so a line
// carrying it can never satisfy the bijection.
const sessionMatrixMarkerPlaceholder = "<id>"

// sessionMatrixMarkerRE matches a well-formed marker line and captures its id.
//
// It is anchored at both ends on purpose. Anchoring at the start rejects the
// trailing-comment form (`It(...) // matrix-row: x.y`), and anchoring at the
// end rejects trailing prose. Both are near-misses that a substring match would
// accept — the failure mode 09-11 hit when `Skip(` matched inside `NotSkip(`.
// A near-miss is not silently tolerated: it fails the well-formedness guard,
// which names the file and line.
//
// The id grammar mirrors the registry's own: <transition-slug>.<column-slug>,
// lowercase alphanumerics with internal hyphens and exactly one dot.
var sessionMatrixMarkerRE = regexp.MustCompile(
	`^[ \t]*//[ \t]*matrix-row:[ \t]*([a-z0-9]+(?:-[a-z0-9]+)*\.[a-z0-9]+(?:-[a-z0-9]+)*)[ \t]*$`,
)

// sessionMatrixSpecOpenRE matches a line that OPENS a RUNNING Ginkgo spec.
//
// It is a WHITELIST — the line must BEGIN with the constructor `It(` — and not
// a substring test with a list of forms to reject. Ginkgo's pending
// constructors (`XIt(`, `PIt(`) and its focused one (`FIt(`) all CONTAIN the
// literal "It(", so `strings.Contains(next, "It(")` accepts every one of them.
// A pending spec's body NEVER EXECUTES and a focused spec suppresses every
// other spec in the suite, yet in either case the marker is still well-formed,
// the bijection still holds, and test/session-matrix.yaml keeps advertising the
// cell as spec-covered by a spec that runs nothing.
//
// That is structurally the same near-miss as `Skip(` matching inside `NotSkip(`
// (the lesson 09-11 records, cited on sessionMatrixMarkerRE above), which is
// exactly why this is phrased as "must begin with the running constructor"
// rather than as `!Contains("It(") || Contains("XIt(") || ...`: a rejection
// list has to be extended every time Ginkgo grows another prefix, and
// forgetting to extend it fails silently. Requiring the identifier to start the
// line cannot admit a prefixed variant at all.
var sessionMatrixSpecOpenRE = regexp.MustCompile(`^[ \t]*It\(`)

// sessionMatrixSelfPath is this file, excluded from the marker walk because its
// regex literal and its examples are marker-shaped by construction: a walk that
// read it would flag itself and invite the wrong fix. The registry file needs no
// entry here — the walk reads only Go sources, so a `.yaml` is excluded by
// construction, and listing it would be dead code pretending to be a guard.
var sessionMatrixSelfPath = filepath.Join("test", "meta", "session_matrix_registry_test.go")

// sessionMatrixPointer is a `spec:` or `covered_by:` citation. All three fields
// are required: a citation missing any of them cannot be resolved, and an
// unresolvable citation is exactly the plausible-sounding pointer this registry
// exists to stop standing in for coverage.
type sessionMatrixPointer struct {
	File      string `yaml:"file"`
	Container string `yaml:"container"`
	Name      string `yaml:"name"`
}

// sessionMatrixRow is decoded permissively: payload presence is checked against
// the raw map so a typo'd key is a missing-payload failure rather than being
// silently dropped into a struct field that was never populated.
type sessionMatrixRow struct {
	ID          string                `yaml:"id"`
	Transition  string                `yaml:"transition"`
	Column      string                `yaml:"column"`
	Disposition string                `yaml:"disposition"`
	Issue       int                   `yaml:"issue"`
	Spec        *sessionMatrixPointer `yaml:"spec"`
	CoveredBy   *sessionMatrixPointer `yaml:"covered_by"`
}

type sessionMatrixRegistry struct {
	Rows []map[string]any `yaml:"rows"`
}

// sessionMatrixMarkerSite is one marker occurrence, kept with its location so a
// failure can name the offending line rather than only the offending id.
type sessionMatrixMarkerSite struct {
	ID   string
	File string // repo-relative
	Line int    // 1-based
}

func (s sessionMatrixMarkerSite) where() string {
	return fmt.Sprintf("%s:%d", s.File, s.Line)
}

// sessionMatrixScan is one pass over the Go sources, shared by the three
// marker-side guards so they cannot disagree about what the tree contains.
type sessionMatrixScan struct {
	Markers []sessionMatrixMarkerSite
	// Malformed are marker CANDIDATE lines that are neither well-formed
	// markers nor documentation placeholders.
	Malformed []string
	// Misplaced are well-formed markers whose next non-comment line is not the
	// `It(` they claim to introduce.
	Misplaced []string
	// FilesScanned exists so a walk that silently matched nothing cannot let
	// an "all lines are fine" loop pass over an empty set.
	FilesScanned int
}

// TestSessionMatrixRegistryShape asserts the structural properties a reader of
// test/session-matrix.yaml would otherwise have to take on trust.
func TestSessionMatrixRegistryShape(t *testing.T) {
	rows := loadSessionMatrixRows(t)

	// Compare lengths rather than using require.Len, which renders the whole
	// 48-row collection into the failure message and buries the count.
	require.Equal(t, sessionMatrixTotalPositions, len(rows),
		"test/session-matrix.yaml MUST carry one row per position in the 12x4 matrix; "+
			"a differing count means a cell was dropped or invented")

	expectedTotal := 0
	for _, n := range sessionMatrixExpectedCounts {
		expectedTotal += n
	}
	require.Equal(t, sessionMatrixTotalPositions, expectedTotal,
		"the pinned per-disposition counts MUST sum to the grid size; a map that "+
			"does not add up would let a row change disposition without any count moving")

	seen := make(map[string]int, len(rows))
	counts := make(map[string]int, len(sessionMatrixExpectedCounts))

	for i, raw := range rows {
		row := decodeSessionMatrixRow(t, i, raw)

		require.NotEmpty(t, row.ID, "row %d MUST carry an id (the bijection key)", i)
		require.NotEmpty(t, row.Transition, "row %q MUST name its transition", row.ID)
		require.NotEmpty(t, row.Column, "row %q MUST name its transport column", row.ID)

		if prev, dup := seen[row.ID]; dup {
			t.Fatalf("duplicate row id %q at rows[%d]; already used at rows[%d]. "+
				"The id is the bijection key, so a duplicate would let two cells "+
				"share one spec marker", row.ID, i, prev)
		}
		seen[row.ID] = i

		ownKey, known := sessionMatrixPayloadKeys[row.Disposition]
		require.Truef(t, known,
			"row %q has unknown disposition %q; permitted values are %v",
			row.ID, row.Disposition, sessionMatrixDispositions())

		require.Containsf(t, raw, ownKey,
			"row %q declares disposition %q but is missing its required payload key %q",
			row.ID, row.Disposition, ownKey)

		for disposition, key := range sessionMatrixPayloadKeys {
			if key == ownKey {
				continue
			}
			require.NotContainsf(t, raw, key,
				"row %q declares disposition %q but also carries %q, the payload of %q. "+
					"A row carries exactly one disposition; two payloads means the cell's "+
					"real status is ambiguous",
				row.ID, row.Disposition, key, disposition)
		}

		if _, hasBlockedOn := raw["blocked_on"]; hasBlockedOn {
			require.Equalf(t, "planned", row.Disposition,
				"row %q carries blocked_on, which names a missing seam and is meaningful "+
					"only on a planned row", row.ID)
		}

		// A gap the system has but the harness cannot reach is only honest
		// while it is tracked somewhere a reader can follow. Without this the
		// disposition degrades into a permanent excuse.
		if row.Disposition == "not-implementable-from-harness-defaults" {
			require.NotZerof(t, row.Issue,
				"row %q is dispositioned %q and MUST cite the issue tracking the gap; "+
					"an untracked gap is indistinguishable from an abandoned one",
				row.ID, row.Disposition)
		}

		counts[row.Disposition]++
	}

	require.Equal(t, sessionMatrixExpectedCounts, counts,
		"every disposition's population is pinned. A differing count means a row changed "+
			"kind: most dangerously, a `spec` row with no spec relabelled as an excuse so "+
			"the marker bijection shrinks to fit. Move a row between dispositions only as a "+
			"deliberate edit that updates sessionMatrixExpectedCounts in the same change")
}

// TestSessionMatrixMarkerCandidateLinesAreEitherMarkersOrDocumentation rejects
// near-miss markers.
//
// A line carrying `matrix-row:` is someone claiming a cell. If it does not
// match the anchored marker form and does not carry the `<id>` placeholder that
// marks it as documentation, it is a claim nothing can see — and an invisible
// claim is indistinguishable from an absent one, which is precisely how a
// bijection gets satisfied by a set that quietly shrank.
func TestSessionMatrixMarkerCandidateLinesAreEitherMarkersOrDocumentation(t *testing.T) {
	scan := scanSessionMatrixMarkers(t, findRepoRoot(t))

	require.Empty(t, scan.Malformed,
		"these lines carry %q but are neither an anchored marker (`// %s <id>` alone on "+
			"its line) nor documentation carrying the literal %q. Each is a coverage claim "+
			"the bijection cannot see:\n  %s",
		sessionMatrixMarkerToken, sessionMatrixMarkerToken, sessionMatrixMarkerPlaceholder,
		strings.Join(scan.Malformed, "\n  "))
}

// TestSessionMatrixEveryMarkerSitsDirectlyAboveTheSpecItClaims stops a marker
// being parked anywhere in the tree.
//
// The bijection alone treats a marker as a claim of coverage no matter where it
// sits, so a comment dropped into a helper — or into a file with no specs at
// all — would satisfy a registry row that has no spec behind it. Requiring the
// next non-comment line to open a Ginkgo spec ties the claim to something that
// runs.
func TestSessionMatrixEveryMarkerSitsDirectlyAboveTheSpecItClaims(t *testing.T) {
	scan := scanSessionMatrixMarkers(t, findRepoRoot(t))

	require.NotEmpty(t, scan.Markers,
		"found no markers at all; an empty marker set would satisfy this guard vacuously")

	require.Empty(t, scan.Misplaced,
		"a marker claims the spec it introduces, so the next non-comment line MUST open one "+
			"with `It(`. These markers introduce something else:\n  %s",
		strings.Join(scan.Misplaced, "\n  "))
}

// TestSessionMatrixSpecRowsAndInCodeMarkersAreBijective is the guard the whole
// registry exists for: a row claiming a spec must have one, and a spec's marker
// must belong to a row.
//
// The comparison is deliberately symmetric about the excluded dispositions. The
// registry side contributes only `spec` rows, because the other four kinds have
// no marker by design — but the marker side contributes EVERYTHING it finds, so
// a marker naming a `not-applicable`, `planned` or
// `not-implementable-from-harness-defaults` row fails loudly instead of being
// quietly ignored. Ignoring it is what would let a known-uncoverable cell be
// upgraded to covered by adding a comment.
func TestSessionMatrixSpecRowsAndInCodeMarkersAreBijective(t *testing.T) {
	root := findRepoRoot(t)

	rows := loadSessionMatrixRows(t)
	dispositions := make(map[string]string, len(rows))
	var specIDs []string
	for i, raw := range rows {
		row := decodeSessionMatrixRow(t, i, raw)
		dispositions[row.ID] = row.Disposition
		if row.Disposition == "spec" {
			specIDs = append(specIDs, row.ID)
		}
	}

	scan := scanSessionMatrixMarkers(t, root)

	// Non-vacuity, both sides. An empty diff over two empty sets passes
	// trivially, so a bug that emptied both would otherwise report success.
	require.NotEmpty(t, specIDs, "the registry contributed no `spec` rows to compare")
	require.NotEmpty(t, scan.Markers, "the walk found no markers to compare")
	require.Equal(t, sessionMatrixExpectedCounts["spec"], len(specIDs),
		"the `spec` row count is pinned; see sessionMatrixExpectedCounts")

	// Duplicates on the marker side would let two specs answer for one row while
	// leaving another row silently unbacked, and the set comparison alone cannot
	// see it.
	byID := make(map[string][]sessionMatrixMarkerSite, len(scan.Markers))
	for _, m := range scan.Markers {
		byID[m.ID] = append(byID[m.ID], m)
	}
	for id, sites := range byID {
		if len(sites) > 1 {
			where := make([]string, 0, len(sites))
			for _, s := range sites {
				where = append(where, s.where())
			}
			sort.Strings(where)
			t.Errorf("row %q is claimed by %d markers (%s); the id is the bijection key, "+
				"so exactly one spec may claim it", id, len(sites), strings.Join(where, ", "))
		}
	}

	markerIDs := make([]string, 0, len(byID))
	for id := range byID {
		markerIDs = append(markerIDs, id)
	}

	specSet := make(map[string]struct{}, len(specIDs))
	for _, id := range specIDs {
		specSet[id] = struct{}{}
	}

	var rowsWithoutMarker []string
	for _, id := range specIDs {
		if _, ok := byID[id]; !ok {
			rowsWithoutMarker = append(rowsWithoutMarker, id)
		}
	}

	var markersWithoutSpecRow []string
	for _, id := range markerIDs {
		if _, ok := specSet[id]; ok {
			continue
		}
		if d, known := dispositions[id]; known {
			markersWithoutSpecRow = append(markersWithoutSpecRow,
				fmt.Sprintf("%s (at %s) names a row dispositioned %q; a marker cannot "+
					"upgrade a non-spec row to covered — change the row's disposition "+
					"deliberately or remove the marker", id, byID[id][0].where(), d))
			continue
		}
		markersWithoutSpecRow = append(markersWithoutSpecRow,
			fmt.Sprintf("%s (at %s) names no row in the registry at all", id, byID[id][0].where()))
	}

	sort.Strings(rowsWithoutMarker)
	sort.Strings(markersWithoutSpecRow)

	require.Empty(t, rowsWithoutMarker,
		"these rows declare `disposition: spec` but no spec in the tree carries their "+
			"marker. Resolve by WRITING the spec or by moving the row to an honest "+
			"disposition — never by deleting the row:\n  %s",
		strings.Join(rowsWithoutMarker, "\n  "))

	require.Empty(t, markersWithoutSpecRow,
		"these markers claim a cell the registry does not record as spec-covered:\n  %s",
		strings.Join(markersWithoutSpecRow, "\n  "))

	// Belt and braces: after the two diffs the sets must be identical, which
	// also catches any ordering or counting mistake in the loops above.
	sort.Strings(specIDs)
	sort.Strings(markerIDs)
	require.Equal(t, specIDs, markerIDs,
		"the `spec` row set and the marker set MUST be identical")
}

// TestSessionMatrixCitedSpecTextAppearsInTheCitedFile resolves every pointer the
// registry offers as evidence.
//
// It covers `covered_by` (a pre-existing spec cited instead of writing a new
// one) and `spec` alike: both are claims about text in a file, and a claim about
// a file nobody opens is the plausible-sounding pointer this registry exists to
// stop. A citation that drifted when a spec was renamed fails here rather than
// quietly aging into fiction.
func TestSessionMatrixCitedSpecTextAppearsInTheCitedFile(t *testing.T) {
	root := findRepoRoot(t)
	rows := loadSessionMatrixRows(t)

	checked := 0
	for i, raw := range rows {
		row := decodeSessionMatrixRow(t, i, raw)

		for _, cite := range []struct {
			key string
			ptr *sessionMatrixPointer
		}{{"spec", row.Spec}, {"covered_by", row.CoveredBy}} {
			if cite.ptr == nil {
				continue
			}
			checked++

			require.NotEmptyf(t, cite.ptr.File, "row %q: %s.file is empty", row.ID, cite.key)
			require.NotEmptyf(t, cite.ptr.Container, "row %q: %s.container is empty", row.ID, cite.key)
			require.NotEmptyf(t, cite.ptr.Name, "row %q: %s.name is empty", row.ID, cite.key)

			abs := filepath.Join(root, filepath.FromSlash(cite.ptr.File))
			data, err := os.ReadFile(abs)
			require.NoErrorf(t, err,
				"row %q cites %s.file %q, which cannot be read; a citation to a file that "+
					"is not there is not evidence of coverage",
				row.ID, cite.key, cite.ptr.File)

			body := string(data)
			require.Truef(t, sessionMatrixCitationResolves(body, cite.ptr.Container),
				"row %q cites %s.container %q, which does not appear in %s as a complete "+
					"quoted string literal outside a comment",
				row.ID, cite.key, cite.ptr.Container, cite.ptr.File)
			require.Truef(t, sessionMatrixCitationResolves(body, cite.ptr.Name),
				"row %q cites %s.name %q, which does not appear in %s as a complete "+
					"quoted string literal outside a comment",
				row.ID, cite.key, cite.ptr.Name, cite.ptr.File)
		}
	}

	// Without this a registry whose pointers all failed to decode would sail
	// through the loop above having checked nothing.
	require.Equal(t,
		sessionMatrixExpectedCounts["spec"]+sessionMatrixExpectedCounts["covered-elsewhere"],
		checked,
		"every `spec` and `covered-elsewhere` row MUST offer a resolvable citation; a "+
			"lower count means a pointer failed to decode and was skipped rather than checked")
}

// sessionMatrixCitationResolves reports whether text appears in body as a
// COMPLETE double-quoted string literal on a line that is not a whole-line
// comment. It is how a `spec.container` / `spec.name` / `covered_by.*`
// citation is resolved.
//
// A whole-file `Contains(body, text)` — what this used to be — resolves a
// citation whose text survives only inside a comment or inside some longer
// unrelated string. For the 32 `spec` rows the marker bijection backstops that;
// for the 2 `covered-elsewhere` rows this is the ONLY check standing between
// the registry and a coverage claim backed by prose. Requiring the surrounding
// quotes means the citation has to name the Ginkgo container/spec label
// verbatim, which is what the registry claims it does.
//
// Deliberately not a full Go parse: the labels cited are plain interpreted
// string literals with no escapes (verified across all 34 pointers), so line
// scanning is sufficient and keeps the failure message pointing at a file and
// a citation rather than at a parse error.
func sessionMatrixCitationResolves(body, text string) bool {
	needle := `"` + text + `"`
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// scanSessionMatrixMarkers walks the repository's Go sources once and returns
// every marker, every near-miss, and every misplaced marker.
//
// Scope: Go sources only, including non-test files — a marker in production
// code claims a cell just as loudly as one in a spec, and it would have no
// `It(` under it, so it is caught by the placement guard rather than ignored.
func scanSessionMatrixMarkers(t *testing.T, root string) sessionMatrixScan {
	t.Helper()

	var scan sessionMatrixScan

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == sessionMatrixSelfPath {
			return nil
		}

		data, readErr := os.ReadFile(path) //nolint:gosec // path comes from the repo-root walk
		if readErr != nil {
			return readErr
		}
		scan.FilesScanned++

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if !strings.Contains(line, sessionMatrixMarkerToken) {
				continue
			}
			m := sessionMatrixMarkerRE.FindStringSubmatch(line)
			if m == nil {
				if strings.Contains(line, sessionMatrixMarkerPlaceholder) {
					continue // documentation of the marker form, not a claim
				}
				scan.Malformed = append(scan.Malformed,
					fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
				continue
			}

			site := sessionMatrixMarkerSite{ID: m[1], File: rel, Line: i + 1}
			scan.Markers = append(scan.Markers, site)

			if next, ok := nextNonCommentLine(lines, i+1); !ok || !sessionMatrixSpecOpenRE.MatchString(next) {
				scan.Misplaced = append(scan.Misplaced,
					fmt.Sprintf("%s claims %q but the next non-comment line is %q, which does not "+
						"OPEN a running Ginkgo spec (a pending XIt/PIt or focused FIt line "+
						"contains \"It(\" but is not one)",
						site.where(), site.ID, strings.TrimSpace(next)))
			}
		}
		return nil
	})
	require.NoError(t, err, "walk the repository for session-matrix markers")

	require.NotZero(t, scan.FilesScanned,
		"the walk read no Go files; every marker-side assertion would pass vacuously")

	return scan
}

// nextNonCommentLine returns the first line at or after from that is neither
// blank nor a whole-line comment. A marker is often followed by further
// explanatory comment lines, so "directly above" means "with nothing but
// commentary in between", not "on the adjacent line".
func nextNonCommentLine(lines []string, from int) (string, bool) {
	for i := from; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		return lines[i], true
	}
	return "", false
}

// loadSessionMatrixRows reads and parses the registry. Every failure is fatal:
// a read or parse error MUST NOT degrade into an empty row set, which would
// trivially satisfy a "no bad rows" loop and report success on an unreadable
// registry.
func loadSessionMatrixRows(t *testing.T) []map[string]any {
	t.Helper()

	path := filepath.Join(findRepoRoot(t), "test", "session-matrix.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "read test/session-matrix.yaml")

	var registry sessionMatrixRegistry
	require.NoError(t, yaml.Unmarshal(data, &registry),
		"test/session-matrix.yaml MUST parse as YAML")
	require.NotEmpty(t, registry.Rows,
		"test/session-matrix.yaml parsed to zero rows; the top-level key MUST be `rows`")

	return registry.Rows
}

// decodeSessionMatrixRow re-encodes one raw row so the typed fields are read by
// the same YAML decoder that produced the raw map, keeping the two views in
// agreement.
func decodeSessionMatrixRow(t *testing.T, i int, raw map[string]any) sessionMatrixRow {
	t.Helper()

	encoded, err := yaml.Marshal(raw)
	require.NoErrorf(t, err, "re-encode rows[%d]", i)

	var row sessionMatrixRow
	require.NoErrorf(t, yaml.Unmarshal(encoded, &row), "decode rows[%d]", i)
	return row
}

func sessionMatrixDispositions() []string {
	out := make([]string, 0, len(sessionMatrixPayloadKeys))
	for d := range sessionMatrixPayloadKeys {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
