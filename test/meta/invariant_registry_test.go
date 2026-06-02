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
	owner := map[string]string{} // path glob -> scope
	shared := map[string]bool{}
	for _, sc := range reg.Scopes {
		for _, f := range sc.SharedFiles {
			shared[f] = true
		}
	}
	for _, sc := range reg.Scopes {
		for _, p := range sc.OwnedPaths {
			if shared[p] {
				continue // explicitly shared; ownership waived
			}
			if prev, dup := owner[p]; dup {
				t.Errorf("owned_paths overlap: %q owned by both %s and %s", p, prev, sc.Name)
			}
			owner[p] = sc.Name
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
	// INV-N (un-migrated). Walk each migrated scope's owned dirs, reusing the
	// package-level skipDirs set (.git/.jj/.worktrees/vendor/node_modules/bin/
	// build/dist) AND the explicit .claude/worktrees path guard — skipDirs does
	// NOT contain a plain "worktrees" key, so the bare WalkDir-into-.claude/
	// worktrees pollution bug (holomush-jb1ec) is only prevented by the path
	// guard below; keep both.
	for _, sc := range reg.Scopes {
		if sc.Status != "migrated" {
			continue
		}
		for _, glob := range sc.OwnedPaths {
			base := strings.TrimSuffix(glob, "**")
			base = strings.TrimSuffix(base, "/")
			_ = filepath.WalkDir(filepath.Join(root, base), func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil //nolint:nilerr // missing/!dir globs (file-specific owned_paths) → skip, not fatal
				}
				if d.IsDir() {
					if _, skip := skipDirs[d.Name()]; skip || strings.Contains(p, "/.claude/worktrees/") {
						return fs.SkipDir
					}
					return nil
				}
				rel, _ := filepath.Rel(root, p)
				if shared[sc.Name][rel] {
					return nil // shared files carry mixed scopes; checked via refs only
				}
				body, rerr := os.ReadFile(p) //nolint:gosec // G304: in-repo walk under owned_paths
				if rerr == nil && bareInvRE.Match(body) {
					findings = append(findings, fmt.Sprintf("%s: residual bare INV-N in migrated-scope file %s", sc.Name, rel))
				}
				return nil
			})
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
