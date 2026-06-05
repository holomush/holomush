// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ref is a path-anchored annotation site: a file plus the ID token to anchor on.
// Never a line number — line numbers drift between classification and migration.
type ref struct {
	File  string `yaml:"file"`
	Token string `yaml:"token"`
}

// registryEntry mirrors one invariant in invariants.yaml.
type registryEntry struct {
	ID         string   `yaml:"id"`
	Scope      string   `yaml:"scope"`
	OriginSpec string   `yaml:"origin_spec"`
	Legacy     []string `yaml:"legacy"`
	Summary    string   `yaml:"summary"`
	Severity   string   `yaml:"severity"`
	Status     string   `yaml:"status"`
	AssertedBy []string `yaml:"asserted_by"`
	External   bool     `yaml:"external"`
	Binding    string   `yaml:"binding"`
	Refs       []ref    `yaml:"refs"`
}

type scopeRecord struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Boundary    string   `yaml:"boundary"`
	Status      string   `yaml:"status"` // pending | migrated
	OriginSpecs []string `yaml:"origin_specs"`
	OwnedPaths  []string `yaml:"owned_paths"`  // path globs; MAY target individual files
	SharedFiles []string `yaml:"shared_files"` // exact paths annotating >1 scope
}

type registryDoc struct {
	Scopes     []scopeRecord   `yaml:"scopes"`
	Invariants []registryEntry `yaml:"invariants"`
}

// loadRegistry parses invariants.yaml from the repo root.
func loadRegistry(t *testing.T) registryDoc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(findRepoRoot(t), "docs", "architecture", "invariants.yaml"))
	if err != nil {
		t.Fatalf("read invariants.yaml: %v", err)
	}
	var reg registryDoc
	if err := yaml.Unmarshal(data, &reg); err != nil {
		t.Fatalf("parse invariants.yaml: %v", err)
	}
	return reg
}

func TestRegistrySchemaParsesOwnershipFields(t *testing.T) {
	const fixture = `
scopes:
  - name: INV-PRESENCE
    status: migrated
    origin_specs: ["docs/superpowers/specs/x.md"]
    owned_paths: ["internal/grpc/focus/**"]
    shared_files: ["test/integration/wholesystem/census_test.go"]
invariants:
  - id: INV-PRESENCE-1
    scope: INV-PRESENCE
    origin_spec: "docs/superpowers/specs/x.md"
    legacy: ["INV-3@docs/superpowers/specs/x.md"]
    summary: "snapshot enumerates all active sessions"
    binding: pending
    refs:
      - {file: "internal/grpc/focus/presence.go", token: "INV-3"}
`
	var reg registryDoc
	if err := yaml.Unmarshal([]byte(fixture), &reg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(reg.Scopes) != 1 {
		t.Fatalf("want 1 scope, got %d", len(reg.Scopes))
	}
	sc := reg.Scopes[0]
	if sc.Status != "migrated" || len(sc.OwnedPaths) != 1 || len(sc.SharedFiles) != 1 {
		t.Errorf("scope ownership fields not parsed: %+v", sc)
	}
	inv := reg.Invariants[0]
	if inv.OriginSpec == "" || len(inv.Refs) != 1 {
		t.Fatalf("invariant origin/refs not parsed: %+v", inv)
	}
	if inv.Refs[0].File != "internal/grpc/focus/presence.go" || inv.Refs[0].Token != "INV-3" {
		t.Errorf("ref not parsed: %+v", inv.Refs[0])
	}
}

// TestOwnedPathsPartition asserts that no path glob is claimed by two scopes'
// owned_paths unless it appears in some scope's shared_files allowlist. This is
// the deterministic F2 defense: owned_paths MUST partition the annotated tree
// so a mislabeled annotation (e.g. a scene file stamped INV-CRYPTO-*) cannot
// pass the provenance guard's ownership check.
func TestOwnedPathsPartition(t *testing.T) {
	reg := loadRegistry(t)
	shared := map[string]bool{}
	for _, sc := range reg.Scopes {
		for _, f := range sc.SharedFiles {
			shared[f] = true
		}
	}
	type entry struct {
		scope string
		g     ownedGlob
	}
	var all []entry
	for _, sc := range reg.Scopes {
		for _, p := range sc.OwnedPaths {
			if shared[p] {
				continue // explicitly shared; ownership waived
			}
			all = append(all, entry{sc.Name, parseOwnedGlob(p)})
		}
	}
	// Cross-scope pairwise. owned_paths MUST partition the annotated tree under
	// longest-prefix-wins semantics: a more-specific glob may carve a sub-area
	// out of a broader one (strict one-way containment — e.g. INV-CLUSTER
	// internal/eventbus/crypto/invalidation/** under INV-CRYPTO
	// internal/eventbus/crypto/**). Only flag (a) two globs covering the exact
	// same files, or (b) a genuinely ambiguous partial overlap where neither
	// contains the other (no winner). This supersedes the old exact-string-dup
	// check, which silently passed semantic prefix overlaps (holomush-hz0v4.14.20).
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			a, b := all[i], all[j]
			if a.scope == b.scope {
				continue // a scope may sub-divide its own territory freely
			}
			switch globConflict(a.g, b.g) {
			case conflictSameFiles:
				t.Errorf("owned_paths overlap: %q (%s) and %q (%s) cover the same files; declare one in shared_files or make one a more-specific carve-out",
					a.g.raw, a.scope, b.g.raw, b.scope)
			case conflictAmbiguous:
				t.Errorf("ambiguous owned_paths overlap: %q (%s) and %q (%s) intersect but neither contains the other (no longest-prefix-wins winner); disambiguate via a more-specific glob or shared_files",
					a.g.raw, a.scope, b.g.raw, b.scope)
			}
			// conflictNone: disjoint, or strict one-way containment (intentional
			// longest-prefix-wins carve-out) — both allowed.
		}
	}
}

// registryVerifiesRE matches `// Verifies: INV-<SCOPE>-<N>` annotations in test files.
var registryVerifiesRE = regexp.MustCompile(`//\s*Verifies:\s*(INV-[A-Z]+-\d+)`)

// registryInvRefRE matches invariant IDs referenced in spec prose but not via Verifies.
// Used for the orphan-detection pass.
var registryInvRefRE = regexp.MustCompile(`\b(INV-[A-Z]+-\d+)\b`)

// checkRegistryBindings validates registry structural invariants (duplicate IDs,
// scope membership, binding presence, pending constraints, external path existence)
// against the provided bindings map (INV-ID → []file paths). Returns findings as
// strings so callers can either t.Error each finding (real-repo test) or assert
// the count matches expectations (table-driven subtests).
// root is used only for external path existence checks.
func checkRegistryBindings(reg registryDoc, bindings map[string][]string, root string) []string {
	var findings []string

	// Check for duplicate IDs and build scope-membership index.
	byID := make(map[string]registryEntry, len(reg.Invariants))
	seenScopes := make(map[string]bool)
	for _, e := range reg.Invariants {
		if _, dup := byID[e.ID]; dup {
			findings = append(findings, fmt.Sprintf("duplicate ID in registry: %s", e.ID))
		}
		byID[e.ID] = e
		parts := strings.SplitN(e.ID, "-", 3)
		if len(parts) == 3 {
			seenScopes[parts[0]+"-"+parts[1]] = true
		}
	}

	// Cross-check: every MIGRATED scope has at least one invariant. Pending
	// scopes are declared up-front by the .14.2 scaffold and populated as each
	// is migrated, so an empty pending scope is the expected mid-migration state
	// (holomush-hz0v4.14.5 — the first migrated scope surfaced this; before any
	// scope landed the enclosing test skipped, so the end-state assumption was
	// latent).
	for _, sc := range reg.Scopes {
		if sc.Status == "migrated" && !seenScopes[sc.Name] {
			findings = append(findings, fmt.Sprintf("migrated scope %s has no invariants", sc.Name))
		}
	}

	// Assert every registry entry has a binding or external path.
	for _, e := range reg.Invariants {
		if e.External {
			for _, p := range e.AssertedBy {
				absPath := filepath.Join(root, p)
				if _, statErr := os.Stat(absPath); statErr != nil {
					findings = append(findings, fmt.Sprintf("%s: external path %q does not exist: %v", e.ID, p, statErr))
				}
			}
			continue
		}
		if e.Binding == "pending" {
			if len(e.AssertedBy) > 0 {
				findings = append(findings, fmt.Sprintf("%s: binding: pending entries MUST NOT carry asserted_by (no fabricated bindings)", e.ID))
			}
			continue
		}
		if len(bindings[e.ID]) == 0 {
			findings = append(findings, fmt.Sprintf("%s: no test binding found (expected at least one `// Verifies: %s` comment in a *_test.go file)", e.ID, e.ID))
		}
	}
	return findings
}

// TestEveryRegistryInvariantHasBinding asserts that every invariant in
// docs/architecture/invariants.yaml has at least one test binding, or is
// explicitly marked binding: pending (tolerated — verification backfill tracked
// separately) or external: true.
func TestEveryRegistryInvariantHasBinding(t *testing.T) {
	root := findRepoRoot(t)

	// 1. Parse the YAML registry.
	data, err := os.ReadFile(filepath.Join(root, "docs", "architecture", "invariants.yaml"))
	if err != nil {
		t.Fatalf("read invariants.yaml: %v", err)
	}
	var reg registryDoc
	if err = yaml.Unmarshal(data, &reg); err != nil {
		t.Fatalf("parse invariants.yaml: %v", err)
	}
	if len(reg.Invariants) == 0 {
		// Scaffolding phase: the registry is populated per-scope during the
		// holomush-hz0v4.14 migration. Until the first scope lands, an empty
		// registry is expected — skip rather than fail so the scaffold can land
		// green. Once any invariant exists, the binding assertions below enforce.
		// TEMPORARY: this skip MUST be removed once the registry is populated —
		// tracked by holomush-hz0v4.14.18 (gates final verification .14.17), so a
		// later regression that empties the registry fails loudly instead of skipping.
		t.Skip("invariants.yaml has no entries yet — populated per-scope by the holomush-hz0v4.14 migration (skip removed by holomush-hz0v4.14.18)")
	}

	// 2. Walk the repo for // Verifies: annotations.
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open repo root: %v", err)
	}
	defer func() { _ = rootFS.Close() }()

	bindings := make(map[string][]string) // INV-ID -> list of file paths

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		f, openErr := rootFS.Open(rel)
		if openErr != nil {
			return openErr
		}
		fileData, readErr := io.ReadAll(f)
		closeErr := f.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		matches := registryVerifiesRE.FindAllSubmatch(fileData, -1)
		for _, m := range matches {
			id := string(m[1])
			bindings[id] = append(bindings[id], rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}

	// 3. Assert every registry entry has a binding or external path.
	for _, finding := range checkRegistryBindings(reg, bindings, root) {
		t.Error(finding)
	}

	pendingCount := 0
	for _, e := range reg.Invariants {
		if e.Binding == "pending" {
			pendingCount++
		}
	}
	t.Logf("registry: %d invariant(s) marked binding: pending (verification backfill tracked separately)", pendingCount)

	// 4. Orphan detection: scan specs/ for INV-<SCOPE>-<N> references that belong
	// to a MIGRATED scope but are missing from the registry. Restricting to
	// migrated scopes makes the check incremental-migration-safe:
	//   - legacy family tokens (INV-RB-11, INV-TS-1, …) match the canonical regex
	//     but their prefix is not a declared scope — they migrate to a real scope
	//     later while specs retain the legacy label as the `legacy:` origin, so
	//     they are not orphans;
	//   - illustrative INV-<SCOPE>-N examples for not-yet-migrated scopes (e.g.
	//     INV-CRYPTO-1 in the registry-design specs) are tolerated until that
	//     scope is actually migrated.
	// Once a scope is migrated, every INV-<SCOPE>-N a spec references for it MUST
	// be registered. Surfaced by INV-PRESENCE as the first migrated scope
	// (holomush-hz0v4.14.5); before any scope landed this whole test skipped, so
	// the over-broad match was latent.
	migratedScopes := make(map[string]bool, len(reg.Scopes))
	for _, sc := range reg.Scopes {
		if sc.Status == "migrated" {
			migratedScopes[sc.Name] = true
		}
	}
	byID := make(map[string]registryEntry, len(reg.Invariants))
	for _, e := range reg.Invariants {
		byID[e.ID] = e
	}
	orphans := make(map[string]int)
	err = filepath.WalkDir(filepath.Join(root, "docs", "superpowers", "specs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		specData, readErr := os.ReadFile(path) //nolint:gosec // G122: meta-test walker reads in-repo doc files for invariant-ref grep; no symlink concern
		if readErr != nil {
			return readErr
		}
		matches := registryInvRefRE.FindAllSubmatch(specData, -1)
		for _, m := range matches {
			id := string(m[1])
			parts := strings.SplitN(id, "-", 3)
			if len(parts) < 2 || !migratedScopes[parts[0]+"-"+parts[1]] {
				continue // not a migrated scope's canonical ID — not an orphan yet
			}
			if _, known := byID[id]; !known {
				orphans[id]++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk specs: %v", err)
	}
	for id, n := range orphans {
		t.Errorf("orphan invariant %s referenced %d time(s) in specs/ but not in invariants.yaml", id, n)
	}
}

// TestRegistryBindingChecks is a table-driven test of the registry structural
// validation logic via checkRegistryBindings. Each case exercises one
// enumerated failure mode using a synthetic registryDoc + bindings map.
func TestRegistryBindingChecks(t *testing.T) {
	root := findRepoRoot(t) // used for external path existence checks

	tests := []struct {
		name         string
		reg          registryDoc
		bindings     map[string][]string
		wantFindings int    // 0 = expect pass, >0 = expect at least this many findings
		findingLike  string // substring that MUST appear in findings when wantFindings > 0
	}{
		{
			name: "passes on empty invariant list",
			reg:  registryDoc{},
			// empty registry → no invariants to check → no findings
			wantFindings: 0,
		},
		{
			name: "detects duplicate IDs",
			reg: registryDoc{
				Invariants: []registryEntry{
					{ID: "INV-FOO-1", Scope: "INV-FOO", Binding: "pending"},
					{ID: "INV-FOO-1", Scope: "INV-FOO", Binding: "pending"},
				},
			},
			bindings:     map[string][]string{},
			wantFindings: 1,
			findingLike:  "duplicate ID",
		},
		{
			name: "detects migrated scope with no invariants",
			reg: registryDoc{
				Scopes: []scopeRecord{{Name: "INV-BAR", Status: "migrated"}},
				Invariants: []registryEntry{
					{ID: "INV-FOO-1", Scope: "INV-FOO", Binding: "pending"},
				},
			},
			bindings:     map[string][]string{},
			wantFindings: 1,
			findingLike:  "migrated scope INV-BAR has no invariants",
		},
		{
			name: "detects missing binding",
			reg: registryDoc{
				Invariants: []registryEntry{
					{ID: "INV-FOO-1", Scope: "INV-FOO"},
				},
			},
			bindings:     map[string][]string{}, // no Verifies annotations found
			wantFindings: 1,
			findingLike:  "INV-FOO-1: no test binding found",
		},
		{
			name: "passes with binding present",
			reg: registryDoc{
				Invariants: []registryEntry{
					{ID: "INV-FOO-1", Scope: "INV-FOO"},
				},
			},
			bindings:     map[string][]string{"INV-FOO-1": {"some/test_file_test.go"}},
			wantFindings: 0,
		},
		{
			name: "passes binding: pending without asserted_by",
			reg: registryDoc{
				Invariants: []registryEntry{
					{ID: "INV-FOO-1", Scope: "INV-FOO", Binding: "pending"},
				},
			},
			bindings:     map[string][]string{},
			wantFindings: 0,
		},
		{
			name: "detects binding: pending with asserted_by (fabricated binding)",
			reg: registryDoc{
				Invariants: []registryEntry{
					{ID: "INV-FOO-1", Scope: "INV-FOO", Binding: "pending", AssertedBy: []string{"some/file_test.go"}},
				},
			},
			bindings:     map[string][]string{},
			wantFindings: 1,
			findingLike:  "MUST NOT carry asserted_by",
		},
		{
			name: "detects external path that does not exist",
			reg: registryDoc{
				Invariants: []registryEntry{
					{ID: "INV-FOO-1", Scope: "INV-FOO", External: true, AssertedBy: []string{"nonexistent/path/file.go"}},
				},
			},
			bindings:     map[string][]string{},
			wantFindings: 1,
			findingLike:  "does not exist",
		},
		{
			name: "passes external path that exists",
			reg: registryDoc{
				Invariants: []registryEntry{
					// Use a well-known file in the repo that is guaranteed to exist.
					{ID: "INV-FOO-1", Scope: "INV-FOO", External: true, AssertedBy: []string{"docs/architecture/invariants.yaml"}},
				},
			},
			bindings:     map[string][]string{},
			wantFindings: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := checkRegistryBindings(tc.reg, tc.bindings, root)
			if tc.wantFindings == 0 {
				if len(findings) != 0 {
					t.Errorf("expected no findings, got %d: %v", len(findings), findings)
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("expected at least %d finding(s), got none", tc.wantFindings)
			}
			if tc.findingLike != "" {
				found := false
				for _, f := range findings {
					if strings.Contains(f, tc.findingLike) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected a finding containing %q, got: %v", tc.findingLike, findings)
				}
			}
		})
	}
}

// bareInvRE matches a residual un-migrated bare INV-N token (no scope prefix).
var bareInvRE = regexp.MustCompile(`\bINV-\d+\b`)

// checkProvenance is the guard body, factored so the negative test can call it
// against a synthetic registry+tree and assert it returns findings.
func checkProvenance(root string, reg registryDoc) []string {
	var findings []string
	migrated := map[string]bool{}
	owned := map[string]string{} // glob -> scope (already partition-checked elsewhere)
	shared := map[string]map[string]bool{}
	for _, sc := range reg.Scopes {
		if sc.Status == "migrated" {
			migrated[sc.Name] = true
		}
		for _, p := range sc.OwnedPaths {
			owned[p] = sc.Name
		}
		shared[sc.Name] = map[string]bool{}
		for _, f := range sc.SharedFiles {
			shared[sc.Name][f] = true
		}
	}
	// For each migrated scope's recorded refs, confirm the canonical token is at that site.
	for _, e := range reg.Invariants {
		if !migrated[e.Scope] {
			continue
		}
		for _, r := range e.Refs {
			data, err := os.ReadFile(filepath.Join(root, r.File))
			if err != nil {
				findings = append(findings, fmt.Sprintf("%s: recorded ref unreadable (%v)", e.ID, err))
				continue
			}
			if !regexp.MustCompile(`\b` + regexp.QuoteMeta(e.ID) + `\b`).Match(data) {
				findings = append(findings, fmt.Sprintf("%s: canonical token absent at recorded site %s", e.ID, r.File))
			}
			// Ownership: the file must be owned by e.Scope OR explicitly shared to it.
			if !shared[e.Scope][r.File] && !pathOwnedBy(owned, r.File, e.Scope) {
				findings = append(findings, fmt.Sprintf("%s: ref %s not in %s owned_paths/shared_files", e.ID, r.File, e.Scope))
			}
		}
	}
	// Residual check: a migrated scope's owned files MUST NOT still carry a bare
	// INV-N (un-migrated) OR a registry-known *prefixed* legacy family token
	// (INV-RB-2, INV-P7-5, I-PRES-6, …). bareInvRE alone (\bINV-\d+\b) misses the
	// prefixed legacy tokens, so a forgotten legacy annotation in a migrated-scope
	// owned file survived silently until human diff review caught it
	// (holomush-hz0v4.14.21). legRE covers the prefixed legacy tokens; the two
	// regexes are disjoint by construction (legRE excludes bare INV-N), so a file
	// is reported at most once per offending token class.
	legRE := legacyTokenRE(reg)
	checkFile := func(scope, abs string) {
		rel, _ := filepath.Rel(root, abs)
		if shared[scope][rel] {
			return // shared files carry mixed/foreign scopes; checked via refs only
		}
		// abs is built from in-repo owned_paths globs (filepath.Join under root); no external taint.
		body, rerr := os.ReadFile(abs)
		if rerr != nil {
			return
		}
		if m := bareInvRE.Find(body); m != nil {
			findings = append(findings, fmt.Sprintf("%s: residual bare INV-N (%s) in migrated-scope file %s", scope, m, rel))
		}
		if legRE != nil {
			if m := legRE.Find(body); m != nil {
				findings = append(findings, fmt.Sprintf("%s: residual legacy token (%s) in migrated-scope file %s", scope, m, rel))
			}
		}
	}
	// Resolve each owned_paths entry to concrete files. Three shapes:
	//   dir/**      → WalkDir the subtree (skipDirs + .claude/worktrees guard — the
	//                 latter is explicit because skipDirs has no plain "worktrees"
	//                 key, so the .claude/worktrees pollution bug holomush-jb1ec is
	//                 only prevented by the path check below; keep both).
	//   dir/glob*.go → filepath.Glob. A literal-`*` path is NOT walkable by WalkDir
	//                 (it errors → silent skip), so file-glob owned_paths got NO
	//                 residual check before holomush-hz0v4.14.21 — expand them here.
	//   dir/file.go → a single concrete path, checked directly.
	for _, sc := range reg.Scopes {
		if sc.Status != "migrated" {
			continue
		}
		for _, glob := range sc.OwnedPaths {
			switch {
			case strings.HasSuffix(glob, "/**"):
				base := strings.TrimSuffix(glob, "/**")
				_ = filepath.WalkDir(filepath.Join(root, base), func(p string, d fs.DirEntry, err error) error {
					if err != nil {
						return nil //nolint:nilerr // missing dir glob → skip, not fatal
					}
					if d.IsDir() {
						if _, skip := skipDirs[d.Name()]; skip || strings.Contains(p, "/.claude/worktrees/") {
							return fs.SkipDir
						}
						return nil
					}
					checkFile(sc.Name, p)
					return nil
				})
			case strings.ContainsAny(glob, "*?["):
				matches, gerr := filepath.Glob(filepath.Join(root, glob))
				if gerr != nil {
					// A malformed glob (e.g. an unbalanced '[') would otherwise
					// silently skip the residual checks for this owned_path — the
					// exact silent-skip class this walk exists to close. Fail closed.
					findings = append(findings, fmt.Sprintf("%s: invalid owned_paths glob %q: %v", sc.Name, glob, gerr))
					continue
				}
				for _, p := range matches {
					checkFile(sc.Name, p)
				}
			default:
				checkFile(sc.Name, filepath.Join(root, glob))
			}
		}
	}
	return findings
}

// pathOwnedBy reports whether file matches any owned-path glob assigned to scope.
// Uses doublestar-style matching via filepath.Match on each path segment prefix;
// for simplicity a glob ending in /** matches any file under that prefix.
func pathOwnedBy(owned map[string]string, file, scope string) bool {
	for glob, sc := range owned {
		if sc != scope {
			continue
		}
		if strings.HasSuffix(glob, "/**") {
			if strings.HasPrefix(file, strings.TrimSuffix(glob, "**")) {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(glob, file); ok {
			return true
		}
	}
	return false
}

// legacyTokenRE builds a regex matching any registry-known *prefixed* legacy
// family token (INV-RB-2, INV-P7-5, I-PRES-6, INV-GW-1, INV-W9ML-3, …) recorded
// in any entry's legacy: list, stripped of its @origin suffix. Bare INV-N legacy
// tokens are EXCLUDED — those are already caught by bareInvRE, so excluding them
// keeps the two residual regexes disjoint (no double-report). Returns nil when
// there are no prefixed legacy tokens (empty alternation). Word boundaries on
// both ends mean a short token (INV-1) never matches inside a canonical id
// (INV-COMMAND-1) or a longer legacy token (INV-GW-1 vs INV-1). (holomush-hz0v4.14.21)
func legacyTokenRE(reg registryDoc) *regexp.Regexp {
	bareLegacy := regexp.MustCompile(`^INV-\d+$`)
	seen := map[string]bool{}
	var toks []string
	for _, e := range reg.Invariants {
		for _, l := range e.Legacy {
			tok := l
			if i := strings.IndexByte(tok, '@'); i >= 0 {
				tok = tok[:i]
			}
			tok = strings.TrimSpace(tok)
			if tok == "" || seen[tok] || bareLegacy.MatchString(tok) {
				continue
			}
			seen[tok] = true
			toks = append(toks, regexp.QuoteMeta(tok))
		}
	}
	if len(toks) == 0 {
		return nil
	}
	return regexp.MustCompile(`\b(?:` + strings.Join(toks, "|") + `)\b`)
}

// ownedGlob is a parsed owned_paths entry for semantic-overlap analysis.
//
//	subtree "dir/**"   → owns every path under dir/
//	file    "dir/name" → owns files matching name directly in dir (name MAY carry
//	                     a single-segment wildcard *,?,[…]; concrete if literal)
type ownedGlob struct {
	raw     string
	subtree bool
	dir     string // subtree root, or the containing directory for file globs
	name    string // file glob/name (empty for subtree)
}

func parseOwnedGlob(g string) ownedGlob {
	if strings.HasSuffix(g, "/**") {
		return ownedGlob{raw: g, subtree: true, dir: strings.TrimSuffix(g, "/**")}
	}
	dir, name := filepath.Split(g)
	return ownedGlob{raw: g, dir: strings.TrimSuffix(dir, "/"), name: name}
}

// dirUnderOrEq reports whether child is the same directory as parent or nested
// beneath it (segment-aligned path prefix).
func dirUnderOrEq(parent, child string) bool {
	return child == parent || strings.HasPrefix(child, parent+"/")
}

// globContains reports whether every file matched by b is also matched by a
// (a's coverage ⊇ b's). A strict one-way containment is an intentional
// longest-prefix-wins carve-out (the more-specific glob legitimately owns a
// sub-area of the broader one).
func globContains(a, b ownedGlob) bool {
	if a.subtree {
		return dirUnderOrEq(a.dir, b.dir) // b's subtree root, or its files' dir, lies under a
	}
	if b.subtree {
		return false // a file-level glob can never contain a whole subtree
	}
	if a.dir != b.dir {
		return false
	}
	return namePatternContains(a.name, b.name)
}

// globsOverlap reports whether a and b can both match some common file.
func globsOverlap(a, b ownedGlob) bool {
	if a.subtree && b.subtree {
		return dirUnderOrEq(a.dir, b.dir) || dirUnderOrEq(b.dir, a.dir)
	}
	if a.subtree {
		return dirUnderOrEq(a.dir, b.dir)
	}
	if b.subtree {
		return dirUnderOrEq(b.dir, a.dir)
	}
	if a.dir != b.dir {
		return false
	}
	return namePatternsOverlap(a.name, b.name)
}

// splitStar splits a single-segment filename pattern around its star span:
// everything before the first '*' and everything after the last '*'. Caller
// MUST ensure p contains a '*' (guarded by starOnly); the i<0 fallback keeps it
// panic-safe regardless.
func splitStar(p string) (pre, suf string) {
	i := strings.IndexByte(p, '*')
	if i < 0 {
		return p, ""
	}
	return p[:i], p[strings.LastIndexByte(p, '*')+1:]
}

// starOnly reports whether a pattern's only wildcard metacharacter is '*' (no
// '?' or '['). The prefix/suffix reconciliation in namePatternsOverlap and the
// witness method in namePatternContains are only sound for '*'-only patterns.
func starOnly(p string) bool {
	return strings.Contains(p, "*") && !strings.ContainsAny(p, "?[")
}

// namePatternsOverlap reports whether two single-segment filename patterns can
// match a common filename. Exact-vs-glob defers to filepath.Match; '*'-only
// glob-vs-glob reconciles the required prefix and suffix around the stars (a
// witness exists iff the literal prefixes are prefix-compatible and the literal
// suffixes are suffix-compatible — the star absorbs the middle). Patterns using
// '?'/'[' that can't be decided precisely are treated as POSSIBLY overlapping
// (fail-closed: a spurious flag forces an explicit shared_files/carve-out, while
// a missed overlap would let an ambiguous partition slip through).
func namePatternsOverlap(p, q string) bool {
	if p == q {
		return true
	}
	pWild := strings.ContainsAny(p, "*?[")
	qWild := strings.ContainsAny(q, "*?[")
	switch {
	case pWild && !qWild:
		ok, _ := filepath.Match(p, q)
		return ok
	case !pWild && qWild:
		ok, _ := filepath.Match(q, p)
		return ok
	case !pWild && !qWild:
		return p == q
	}
	if !starOnly(p) || !starOnly(q) {
		return true // '?'/'[' present — assume possible overlap (fail-closed)
	}
	pp, ps := splitStar(p)
	qp, qs := splitStar(q)
	return (strings.HasPrefix(pp, qp) || strings.HasPrefix(qp, pp)) &&
		(strings.HasSuffix(ps, qs) || strings.HasSuffix(qs, ps))
}

// namePatternContains reports whether every filename matching b also matches a.
// An exact a contains only itself; a '*'-only a is tested against representative
// witnesses of b. A '?'/'['-bearing a can't be soundly proven to contain b, so
// it returns false — fail-closed: a non-containment turns an overlap into a
// flagged ambiguity rather than silently allowing it as a carve-out.
func namePatternContains(a, b string) bool {
	if !strings.ContainsAny(a, "*?[") {
		return a == b
	}
	if !starOnly(a) {
		return false
	}
	for _, w := range nameWitnesses(b) {
		if ok, _ := filepath.Match(a, w); !ok {
			return false
		}
	}
	return true
}

// nameWitnesses returns representative filenames matching pattern b: the literal
// itself when b has no '*', else the star span filled with both the empty string
// and a non-empty padding so a candidate container must match every length.
func nameWitnesses(b string) []string {
	if !strings.Contains(b, "*") {
		return []string{b}
	}
	pre, suf := splitStar(b)
	return []string{pre + suf, pre + "Zz9" + suf}
}

// globConflictKind classifies a cross-scope owned_paths pair.
type globConflictKind int

const (
	conflictNone      globConflictKind = iota // disjoint, or intentional one-way carve-out
	conflictSameFiles                         // two globs cover the exact same files
	conflictAmbiguous                         // overlap with no longest-prefix-wins winner
)

// globConflict decides whether two cross-scope owned globs violate the
// longest-prefix-wins partition. Factored so the negative test can assert the
// decision directly (holomush-hz0v4.14.20).
func globConflict(a, b ownedGlob) globConflictKind {
	if !globsOverlap(a, b) {
		return conflictNone
	}
	ab := globContains(a, b)
	ba := globContains(b, a)
	switch {
	case ab && ba:
		return conflictSameFiles
	case !ab && !ba:
		return conflictAmbiguous
	default:
		return conflictNone // strict one-way containment: a carve-out, allowed
	}
}

func TestProvenanceGuard(t *testing.T) {
	root := findRepoRoot(t)
	reg := loadRegistry(t)
	if f := checkProvenance(root, reg); len(f) > 0 {
		for _, line := range f {
			t.Error(line)
		}
	}
}

func TestProvenanceGuardFailsOnMislabel(t *testing.T) {
	dir := t.TempDir()
	// A scene file mislabeled with a CRYPTO id; CRYPTO owns only crypto paths.
	if err := os.MkdirAll(filepath.Join(dir, "plugins", "core-scenes"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	scene := filepath.Join("plugins", "core-scenes", "board.go")
	if err := os.WriteFile(filepath.Join(dir, scene), []byte("// INV-CRYPTO-1: mislabeled\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	reg := registryDoc{
		Scopes:     []scopeRecord{{Name: "INV-CRYPTO", Status: "migrated", OwnedPaths: []string{"internal/eventbus/crypto/**"}}},
		Invariants: []registryEntry{{ID: "INV-CRYPTO-1", Scope: "INV-CRYPTO", Refs: []ref{{File: scene, Token: "INV-CRYPTO-1"}}}},
	}
	f := checkProvenance(dir, reg)
	if len(f) == 0 {
		t.Fatal("guard must fail on a scene file stamped INV-CRYPTO-1, but passed")
	}
	// Assert the OWNERSHIP check fired (not merely "some" finding): the token IS
	// present in the fixture, so a count-only assertion would still pass if a
	// future edit dropped the token and surfaced a different finding instead,
	// silently losing ownership coverage.
	found := false
	for _, line := range f {
		if strings.Contains(line, "not in INV-CRYPTO owned_paths") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected an owned_paths ownership finding, got: %v", f)
	}
}

// TestProvenanceGuardFailsOnResidualLegacyToken proves the holomush-hz0v4.14.21
// hardening: a forgotten *prefixed* legacy family token (INV-GW-7) left in a
// migrated scope's owned file is caught. bareInvRE (\bINV-\d+\b) misses it (the
// "GW" follows "INV-", so no bare INV-N match), so before .14.21 only human diff
// review caught such a leftover.
func TestProvenanceGuardFailsOnResidualLegacyToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal", "gateway_invariants"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	owned := filepath.Join("internal", "gateway_invariants", "stale.go")
	if err := os.WriteFile(filepath.Join(dir, owned), []byte("// INV-GW-7: forgotten legacy annotation\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	reg := registryDoc{
		Scopes: []scopeRecord{{Name: "INV-EVENTBUS", Status: "migrated", OwnedPaths: []string{"internal/gateway_invariants/**"}}},
		// INV-GW-7 is registry-known (recorded on some entry's legacy: list).
		Invariants: []registryEntry{{ID: "INV-EVENTBUS-1", Scope: "INV-EVENTBUS", Legacy: []string{"INV-GW-7@docs/x.md"}}},
	}
	f := checkProvenance(dir, reg)
	found := false
	for _, line := range f {
		if strings.Contains(line, "residual legacy token (INV-GW-7)") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("guard must flag a residual prefixed legacy token in a migrated-owned file, got: %v", f)
	}
}

// TestProvenanceGuardWalksFileGlobOwnedPaths proves the second half of
// holomush-hz0v4.14.21: a `*`-file-glob owned_path is now residual-walked via
// filepath.Glob. Before the fix, WalkDir on a literal-`*` path errored and the
// file got NO residual check at all (silent skip).
func TestProvenanceGuardWalksFileGlobOwnedPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal", "grpc"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	owned := filepath.Join("internal", "grpc", "foo_wiring.go")
	if err := os.WriteFile(filepath.Join(dir, owned), []byte("// INV-3: un-migrated bare token\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	reg := registryDoc{
		Scopes: []scopeRecord{{Name: "INV-SCENE", Status: "migrated", OwnedPaths: []string{"internal/grpc/foo_wiring*.go"}}},
	}
	f := checkProvenance(dir, reg)
	found := false
	for _, line := range f {
		if strings.Contains(line, "residual bare INV-N (INV-3)") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("guard must residual-walk file-glob owned_paths, got: %v", f)
	}
}

// TestProvenanceGuardFailsOnMalformedGlob proves the residual walk fails closed
// on an invalid owned_paths glob (e.g. an unbalanced '['): filepath.Glob returns
// ErrBadPattern, which MUST surface as a finding rather than be discarded and
// silently skip the residual checks for that owned_path (CodeRabbit PR #4381).
func TestProvenanceGuardFailsOnMalformedGlob(t *testing.T) {
	reg := registryDoc{
		Scopes: []scopeRecord{{Name: "INV-SCENE", Status: "migrated", OwnedPaths: []string{"internal/grpc/[bad.go"}}},
	}
	f := checkProvenance(t.TempDir(), reg)
	found := false
	for _, line := range f {
		if strings.Contains(line, "invalid owned_paths glob") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("guard must fail closed on a malformed owned_paths glob, got: %v", f)
	}
}

// TestOwnedPathsPartitionSemantics proves the holomush-hz0v4.14.20 hardening:
// the partition check flags genuinely-ambiguous overlaps and exact-same-file
// duplicates while ALLOWING intentional longest-prefix-wins carve-outs.
func TestOwnedPathsPartitionSemantics(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want globConflictKind
	}{
		{"ambiguous crossing patterns (a*.go ∩ *b.go, neither contains)", "internal/foo/a*.go", "internal/foo/*b.go", conflictAmbiguous},
		{"longest-prefix-wins subtree carve-out", "internal/eventbus/crypto/**", "internal/eventbus/crypto/invalidation/**", conflictNone},
		{"concrete carve-out under a subtree", "internal/foo/**", "internal/foo/bar.go", conflictNone},
		{"file-glob carve-out under a subtree", "internal/foo/**", "internal/foo/bar*.go", conflictNone},
		{"identical subtree globs", "internal/foo/**", "internal/foo/**", conflictSameFiles},
		{"disjoint subtrees", "internal/foo/**", "internal/bar/**", conflictNone},
		{"disjoint same-dir file-globs", "internal/foo/a*.go", "internal/foo/c*.go", conflictNone},
		{"identical concrete files", "internal/foo/x.go", "internal/foo/x.go", conflictSameFiles},
		// '?'-bearing patterns can't be decided precisely → fail-closed: assume
		// overlap, and (neither soundly contains the other) report ambiguous
		// rather than panic or silently pass. Guards the splitStar no-'*' path.
		{"question-mark patterns are handled fail-closed (no panic)", "internal/foo/a?.go", "internal/foo/?b.go", conflictAmbiguous},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := globConflict(parseOwnedGlob(tc.a), parseOwnedGlob(tc.b))
			if got != tc.want {
				t.Errorf("globConflict(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
