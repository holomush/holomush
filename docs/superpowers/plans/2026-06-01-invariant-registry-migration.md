<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Safe `INV-<SCOPE>-N` Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate every in-code invariant annotation to the canonical `INV-<SCOPE>-N` form via a closed-world, registry-driven rename that makes the two Task-4 failure modes (value-keyed rename, existence-only verification) structurally impossible.

**Architecture:** A per-ref-classified registry (`docs/architecture/invariants.yaml`) records each invariant's canonical `id`, `scope`, `legacy` IDs, and **path-anchored `refs` (`{file, token}`)**; each scope declares partitioned `owned_paths` (+ a `shared_files` allowlist) and a `status: pending|migrated`. A migration tool (`cmd/inv-migrate`) rewrites only the recorded `{file, token}` sites for one scope at a time. A deterministic provenance guard (extending `test/meta/invariant_registry_test.go`) enforces existence + site-match + `owned_paths` ownership on `migrated` scopes. Scopes migrate one PR at a time, easiest first, with the `P7` CRYPTO/PLUGIN split last.

**Tech Stack:** Go (`gopkg.in/yaml.v3`, `testing`), the existing `test/meta` harness, `task` runner. Builds on the preserved registry infra (draft PR #4358: `holomush-hz0v4.1`/`.2`/`.3`/`.10`).

**Spec:** [docs/superpowers/specs/2026-06-01-invariant-registry-migration-redesign.md](../specs/2026-06-01-invariant-registry-migration-redesign.md)

> **Dependency on draft PR #4358.** This plan extends files introduced by the
> preserved-infra branch `invariant-registry-infra` (`docs/architecture/invariants.yaml`,
> `test/meta/invariant_registry_test.go`, `scripts/check-invariant-registry-consistency.sh`).
> Phase 1 assumes that branch is the base (bundled spec+plan PR rebases onto it,
> or it lands first). All paths below are grounded against that branch.

---

## File Structure

| File | Responsibility | Created by |
| --- | --- | --- |
| `docs/architecture/invariants.yaml` | Authoritative registry: per-invariant `id`/`scope`/`legacy`/`summary`/`binding`/`refs`, and per-scope `owned_paths`/`shared_files`/`origin_specs`/`status`. | exists (`.1`); extended Task 2, populated Phase 4 |
| `test/meta/invariant_registry_test.go` | The Go structs + the provenance guard (existence + site-match + ownership) + partition + negative tests. | exists (`.2`); extended Tasks 1, 4 |
| `cmd/inv-migrate/main.go` | CLI entrypoint: `inv-migrate -scope <SCOPE> [-dry-run]`. Thin wrapper over the rewrite package. | Task 3 |
| `cmd/inv-migrate/migrate.go` | Pure rewrite logic: load registry → for a scope's entries, rewrite each `{file, token}` → idempotent, refuses unrecorded sites. | Task 3 |
| `cmd/inv-migrate/migrate_test.go` | Unit tests for the rewrite logic (idempotence, scope isolation, refuse-unrecorded). | Task 3 |
| `docs/architecture/invariants.md` | Human-readable view; GENERATED from the YAML by `cmd/inv-render`, guarded by `inv-render -check` generate-and-diff (holomush-hz0v4.15, supersedes consistency lint `.3`). | exists (`.3`); regenerated Phase 4 |

**Scope vocabulary (from `invariants.yaml`):** `CRYPTO, PRIVACY, PRESENCE, SCENE, PLUGIN, EVENTBUS, CLUSTER, ACCESS, SESSION, STORE, TELEMETRY, BRANDING, DOCS`.

---

## Phase 1: Schema foundation

### Task 1: Extend registry Go structs for `origin_spec`, `refs`, and per-scope ownership

**Files:**

- Modify: `test/meta/invariant_registry_test.go` (the `registryEntry` and `registryDoc` structs near the top)
- Test: `test/meta/invariant_registry_test.go` (new `TestRegistrySchemaParsesOwnershipFields`)

- [ ] **Step 1: Write the failing test**

Add to `test/meta/invariant_registry_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestRegistrySchemaParsesOwnershipFields ./test/meta/`
Expected: FAIL — `reg.Scopes[0].Status` / `OwnedPaths` / `inv.OriginSpec` / `inv.Refs` are undefined fields (compile error).

- [ ] **Step 3: Extend the structs**

Replace the `registryEntry` and `registryDoc` struct declarations in `test/meta/invariant_registry_test.go` with:

```go
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
	Status      string   `yaml:"status"`       // pending | migrated
	OriginSpecs []string `yaml:"origin_specs"`
	OwnedPaths  []string `yaml:"owned_paths"`  // path globs; MAY target individual files
	SharedFiles []string `yaml:"shared_files"` // exact paths annotating >1 scope
}

type registryDoc struct {
	Scopes     []scopeRecord   `yaml:"scopes"`
	Invariants []registryEntry `yaml:"invariants"`
}
```

Also add the shared loader here (used by Tasks 2 and 4; relies on `findRepoRoot`,
which exists in `test/meta/inv_binding_test.go` and is relocated to
`meta_helpers_test.go` in Task 16):

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestRegistrySchemaParsesOwnershipFields ./test/meta/`
Expected: PASS.

- [ ] **Step 5: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`
(`jj commit -m "test(invariants): registry schema fields for scope ownership + path-anchored refs (holomush-hz0v4.14)"`).

---

### Task 2: Re-derive the family→scope map with evidence; populate per-scope ownership

**Files:**

- Modify: `docs/architecture/invariants.yaml` (add `status`/`origin_specs`/`owned_paths`/`shared_files` to each `scopes:` entry)
- Modify: `docs/superpowers/specs/2026-06-01-invariant-registry-migration-redesign.md` (record the derived family→scope map table in § 2)
- Test: `test/meta/invariant_registry_test.go` (new `TestOwnedPathsPartition`)

- [ ] **Step 1: Derive the map from source specs (evidence pass)**

For each legacy family (`P4 P5 P6 P7 FS FW RB GW PC TS WS RA ROPS LOAD SH Y5INX W9ML M`) and for bare `INV-N`, find its defining spec and record the scope. Use:

```bash
# For a family, find where its invariants are DEFINED (spec prose), not just referenced:
rg -n 'INV-RB-[0-9]+' docs/superpowers/specs/ site/ | head
# For bare INV-N in a package, read the annotation text to confirm domain:
rg -n '\bINV-[0-9]+\b' internal/plugin/pluginauthz/ -C1
```

Record each assignment with its origin spec in the spec's § 2 table (NOT assumed — every row cites a spec). Starting hypotheses to **verify**: `P4/P5/P6/FS/FW→SCENE`, `RB→CRYPTO`, `GW→EVENTBUS`, `PC→PLUGIN`, `P7→CRYPTO`+`PLUGIN` (split), `TS/WS/RA/ROPS/LOAD/SH→` resolve against actual asserts (likely `EVENTBUS`/`STORE`/`PLUGIN`); resolve `Y5INX/W9ML/M`.

- [ ] **Step 2: Write the failing partition test**

Add to `test/meta/invariant_registry_test.go`:

```go
func TestOwnedPathsPartition(t *testing.T) {
	reg := loadRegistry(t) // defined in Task 1 Step 3
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `task test -- -run TestOwnedPathsPartition ./test/meta/`
Expected: FAIL — scopes have no `owned_paths` yet, so the map is empty and the test trivially passes OR fails to find `loadRegistry`. If it passes vacuously, that is acceptable for an empty map; proceed to Step 4 to populate, after which re-running guards the invariant.

- [ ] **Step 4: Populate per-scope ownership in `invariants.yaml`**

For every scope, add `status: pending`, `origin_specs:`, `owned_paths:` (file-specific globs where a directory holds multiple scopes — e.g. `test/meta/inv_binding_test.go` not `test/meta/**`), and `shared_files:` for cross-scope files. Example:

```yaml
  - name: INV-PRESENCE
    description: "Presence snapshot correctness, ..."
    boundary: "..."
    status: pending
    origin_specs: ["docs/superpowers/specs/2026-05-19-presence-snapshot-design.md"]
    owned_paths:
      - "internal/grpc/focus/presence*.go"
      - "test/integration/presence/**"
    shared_files:
      - "test/integration/wholesystem/census_test.go"
```

- [ ] **Step 5: Run partition test to verify it passes**

Run: `task test -- -run TestOwnedPathsPartition ./test/meta/`
Expected: PASS (no path owned by two scopes outside `shared_files`).

- [ ] **Step 6: Commit**

`jj commit -m "docs(invariants): re-derive family→scope map + per-scope owned_paths/shared_files (holomush-hz0v4.14)"`

---

## Phase 2: Migration tool

### Task 3: Build `cmd/inv-migrate` — closed-world, site-addressed, idempotent

**Files:**

- Create: `cmd/inv-migrate/migrate.go`
- Create: `cmd/inv-migrate/main.go`
- Create: `cmd/inv-migrate/migrate_test.go`

(Precedent for one-off `cmd/` tools: `cmd/lint-plugin-manifests`, `cmd/holomush-cutover`.)

- [ ] **Step 1: Write the failing rewrite test**

Create `cmd/inv-migrate/migrate_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRewriteScopeRewritesOnlyRecordedTokens(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "presence.go")
	// INV-3 belongs to PRESENCE here; a stray INV-4 is NOT recorded and must be untouched.
	os.WriteFile(src, []byte("// INV-3: snapshot\nx := 1 // INV-4: unrelated\n"), 0o644)

	plan := []rewrite{{File: src, Token: "INV-3", Canonical: "INV-PRESENCE-1"}}
	changed, err := rewriteAll(plan)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Fatalf("want 1 file changed, got %d", changed)
	}
	got, _ := os.ReadFile(src)
	want := "// INV-PRESENCE-1: snapshot\nx := 1 // INV-4: unrelated\n"
	if string(got) != want {
		t.Errorf("rewrite wrong:\n got %q\nwant %q", got, want)
	}
}

func TestRewriteIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "p.go")
	os.WriteFile(src, []byte("// INV-3: x\n"), 0o644)
	plan := []rewrite{{File: src, Token: "INV-3", Canonical: "INV-PRESENCE-1"}}
	if _, err := rewriteAll(plan); err != nil {
		t.Fatal(err)
	}
	changed, err := rewriteAll(plan) // second run: token already gone
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Errorf("re-run should change 0 files, changed %d", changed)
	}
}

func TestRewriteRefusesMissingFile(t *testing.T) {
	plan := []rewrite{{File: "/nope/missing.go", Token: "INV-3", Canonical: "INV-PRESENCE-1"}}
	if _, err := rewriteAll(plan); err == nil {
		t.Fatal("want error for missing recorded file, got nil")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- ./cmd/inv-migrate/`
Expected: FAIL — `rewrite` / `rewriteAll` undefined.

- [ ] **Step 3: Implement the rewrite logic**

Create `cmd/inv-migrate/migrate.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Command inv-migrate rewrites in-code invariant annotations from a legacy
// token to the canonical INV-<SCOPE>-N id, driven entirely by the registry's
// recorded {file, token} refs. It NEVER matches on bare INV-N across the tree;
// it only touches sites the registry recorded. See
// docs/superpowers/specs/2026-06-01-invariant-registry-migration-redesign.md.
package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
)

// rewrite is one recorded site: replace whole-token Token with Canonical in File.
type rewrite struct {
	File      string
	Token     string
	Canonical string
}

// rewriteAll applies each rewrite to its file. Whole-token match only (so INV-3
// never matches inside INV-31). Idempotent: a file whose Token is already absent
// is left unchanged. Returns the count of files actually modified. A recorded
// file that does not exist is an error (the registry is stale — fail loud).
func rewriteAll(plan []rewrite) (int, error) {
	changed := 0
	for _, r := range plan {
		data, err := os.ReadFile(r.File) //nolint:gosec // G304: path comes from the in-repo registry, not user input
		if err != nil {
			return changed, fmt.Errorf("recorded ref unreadable %s: %w", r.File, err)
		}
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(r.Token) + `\b`)
		out := re.ReplaceAll(data, []byte(r.Canonical))
		if bytes.Equal(out, data) {
			continue
		}
		if err := os.WriteFile(r.File, out, 0o644); err != nil { //nolint:gosec // G306: source files are 0644 by repo convention
			return changed, fmt.Errorf("write %s: %w", r.File, err)
		}
		changed++
	}
	return changed, nil
}
```

- [ ] **Step 4: Run to verify the rewrite tests pass**

Run: `task test -- ./cmd/inv-migrate/`
Expected: PASS (all three tests).

- [ ] **Step 5: Add the CLI entrypoint**

Create `cmd/inv-migrate/main.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
package main

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ref struct {
	File  string `yaml:"file"`
	Token string `yaml:"token"`
}
type entry struct {
	ID    string `yaml:"id"`
	Scope string `yaml:"scope"`
	Refs  []ref  `yaml:"refs"`
}
type doc struct {
	Invariants []entry `yaml:"invariants"`
}

func main() {
	scope := flag.String("scope", "", "scope to migrate, e.g. INV-PRESENCE")
	regPath := flag.String("registry", "docs/architecture/invariants.yaml", "registry path")
	dry := flag.Bool("dry-run", false, "print planned rewrites without writing")
	flag.Parse()
	if *scope == "" {
		fmt.Fprintln(os.Stderr, "inv-migrate: -scope is required")
		os.Exit(2)
	}
	data, err := os.ReadFile(*regPath) //nolint:gosec // G304: in-repo registry path
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var d doc
	if err := yaml.Unmarshal(data, &d); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var plan []rewrite
	for _, e := range d.Invariants {
		if e.Scope != *scope {
			continue
		}
		for _, rf := range e.Refs {
			plan = append(plan, rewrite{File: rf.File, Token: rf.Token, Canonical: e.ID})
		}
	}
	if *dry {
		for _, r := range plan {
			fmt.Printf("%s: %s -> %s\n", r.File, r.Token, r.Canonical)
		}
		return
	}
	n, err := rewriteAll(plan)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("inv-migrate %s: %d files rewritten\n", *scope, n)
}
```

- [ ] **Step 6: Verify build + tests**

Run: `task build` then `task test -- ./cmd/inv-migrate/`
Expected: build succeeds; tests PASS.

- [ ] **Step 7: Commit**

`jj commit -m "feat(invariants): inv-migrate registry-driven rename tool (holomush-hz0v4.14)"`

---

## Phase 3: Provenance guard

### Task 4: Extend the meta-test into the deterministic provenance guard

**Files:**

- Modify: `test/meta/invariant_registry_test.go`
- Test: same file (`TestProvenanceGuard`, `TestProvenanceGuardFailsOnMislabel`)

- [ ] **Step 1: Write the failing guard + negative tests**

First add `"fmt"` to the import block of `test/meta/invariant_registry_test.go`
(the existing file imports `io io/fs os path/filepath regexp strings testing` +
`gopkg.in/yaml.v3` but NOT `fmt`; `checkProvenance` below uses `fmt.Sprintf`).
Then add (`loadRegistry` is already defined in Task 1 Step 3; `skipDirs` is the
package-level set in `inv_binding_test.go`, relocated to `meta_helpers_test.go`
in Task 16):

```go
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
			data, err := os.ReadFile(filepath.Join(root, r.File)) //nolint:gosec // G304: in-repo path from registry
			if err != nil {
				findings = append(findings, fmt.Sprintf("%s: recorded ref unreadable (%v)", e.ID, err))
				continue
			}
			if !regexp.MustCompile(`\b`+regexp.QuoteMeta(e.ID)+`\b`).Match(data) {
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
					return nil // missing/!dir globs (file-specific owned_paths) are fine
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
	// A scene file mislabeled with a CRYPTO id, CRYPTO owns only crypto paths.
	os.MkdirAll(filepath.Join(dir, "plugins", "core-scenes"), 0o755)
	scene := filepath.Join("plugins", "core-scenes", "board.go")
	os.WriteFile(filepath.Join(dir, scene), []byte("// INV-CRYPTO-1: mislabeled\n"), 0o644)
	reg := registryDoc{
		Scopes: []scopeRecord{{Name: "INV-CRYPTO", Status: "migrated", OwnedPaths: []string{"internal/eventbus/crypto/**"}}},
		Invariants: []registryEntry{{ID: "INV-CRYPTO-1", Scope: "INV-CRYPTO", Refs: []ref{{File: scene, Token: "INV-CRYPTO-1"}}}},
	}
	f := checkProvenance(dir, reg)
	if len(f) == 0 {
		t.Fatal("guard must fail on a scene file stamped INV-CRYPTO-1, but passed")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- -run 'TestProvenanceGuard' ./test/meta/`
Expected: FAIL — `pathOwnedBy` undefined (compile error).

- [ ] **Step 3: Implement `pathOwnedBy` (glob match)**

Add to `test/meta/invariant_registry_test.go`:

```go
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
```

- [ ] **Step 4: Run to verify both tests pass**

Run: `task test -- -run 'TestProvenanceGuard' ./test/meta/`
Expected: PASS — `TestProvenanceGuard` (no migrated scopes yet → no refs to check → empty findings) and `TestProvenanceGuardFailsOnMislabel` (synthetic mislabel produces a finding).

- [ ] **Step 5: Commit**

`jj commit -m "test(invariants): deterministic provenance guard (site-match + owned_paths) + negative test (holomush-hz0v4.14)"`

---

## Phase 4: Per-scope migration (one bead/PR each, easiest first)

Each task follows the **same procedure** (the engine does the work; the per-scope effort is classification + review). Default order — easiest/cleanest first to validate the engine end-to-end, `P7` CRYPTO/PLUGIN split **last**; finalize against Task 2's derived map:

`PRESENCE → SESSION → STORE → TELEMETRY → PRIVACY → ACCESS → CLUSTER → EVENTBUS → SCENE → PLUGIN → CRYPTO`
(`BRANDING`/`DOCS` have no in-code bare `INV-N`; skip unless Task 2 finds refs.)

**Per-scope procedure — the 6 steps every Task 5–15 below executes, substituting its `<SCOPE>` (and the `<FAMILY>` set from Task 2's map):**

- [ ] **Step 1: Enumerate this scope's legacy refs**

```bash
# Prefixed family refs for this scope (from Task 2's map), and bare INV-N in owned paths:
rg -n 'INV-<FAMILY>-[0-9]+' internal/ test/ plugins/
rg -n '\bINV-[0-9]+\b' <each owned_path>
```

- [ ] **Step 2: Add registry entries**

For each invariant in `<SCOPE>`, add to `invariants.yaml` under `invariants:` an entry with `id: INV-<SCOPE>-<freshN>`, `scope: INV-<SCOPE>`, `origin_spec`, `legacy: [<old ids>]`, `summary`, `binding: <test path>|pending`, and `refs:` listing every `{file, token}` site (token = the legacy token to anchor on). Number `N` freshly per scope (1..k).

- [ ] **Step 3: Dry-run the migration**

Run: `go run ./cmd/inv-migrate -scope INV-<SCOPE> -dry-run`
Expected: prints exactly the `{file: token -> canonical}` lines you recorded — eyeball that no unintended file appears.

- [ ] **Step 4: Apply + flip status**

Run: `go run ./cmd/inv-migrate -scope INV-<SCOPE>` then set that scope's `status: migrated` in `invariants.yaml`, then run `task invariants:render` to regenerate `invariants.md` from the YAML and commit the result. Do NOT hand-edit `invariants.md` — its tables are generated (holomush-hz0v4.15).

- [ ] **Step 5: Guard + lint + test green**

Run: `task test -- -run 'TestProvenanceGuard|TestOwnedPathsPartition|TestEveryRegistryInvariantHasBinding' ./test/meta/` and `task lint:invariants` (runs `inv-render -check` — generate-and-diff) and `task lint`
Expected: all green; no bare `INV-N` remains in this scope's owned paths.

- [ ] **Step 6: Commit (one scope per commit/PR)**

`jj commit -m "refactor(invariants): migrate INV-<SCOPE> to canonical ids (holomush-hz0v4.14)"`

Each scope below is its own task → its own bead → its own PR. Each task runs the
6-step procedure for its `<SCOPE>`; the per-task content is the scope-specific
family set, owned-paths, ref volume, and ordering. Family→scope assignments are
the Task-2 hypotheses (verify there). `TS`/`WS`/`RA`/`ROPS` (test-infra families)
land under whichever scope Task 2's evidence assigns them to.

### Task 5: Migrate INV-PRESENCE

**Scope:** `INV-PRESENCE` · **Order 1** (smallest/cleanest — validates the engine end-to-end first) · **Legacy:** bare `INV-N` in presence files.
**Files:** Modify `docs/architecture/invariants.yaml`, `docs/architecture/invariants.md`, and `INV-PRESENCE` `owned_paths` files (rewritten by `cmd/inv-migrate`).

- [ ] Run the Phase 4 per-scope procedure (Steps 1–6) for `INV-PRESENCE`: enumerate refs, add registry entries (fresh `INV-PRESENCE-1..k` + `legacy` + `refs`), `go run ./cmd/inv-migrate -scope INV-PRESENCE -dry-run`, apply, flip `status: migrated`, guard+lint+consistency green, one commit/PR.

### Task 6: Migrate INV-SESSION

**Scope:** `INV-SESSION` · **Order 2** · **Legacy:** bare `INV-N` in session files.
**Files:** `invariants.yaml`, `invariants.md`, `INV-SESSION` `owned_paths` files.

- [ ] Run the Phase 4 per-scope procedure (Steps 1–6) for `INV-SESSION`.

### Task 7: Migrate INV-STORE

**Scope:** `INV-STORE` · **Order 3** · **Legacy:** bare `INV-N` in store + any `TS` test-infra refs Task 2 assigns here.
**Files:** `invariants.yaml`, `invariants.md`, `INV-STORE` `owned_paths` files.

- [ ] Run the Phase 4 per-scope procedure (Steps 1–6) for `INV-STORE`.

### Task 8: Migrate INV-TELEMETRY

**Scope:** `INV-TELEMETRY` · **Order 4** · **Legacy:** bare `INV-N` in telemetry/observability.
**Files:** `invariants.yaml`, `invariants.md`, `INV-TELEMETRY` `owned_paths` files.

- [ ] Run the Phase 4 per-scope procedure (Steps 1–6) for `INV-TELEMETRY`.

### Task 9: Migrate INV-PRIVACY

**Scope:** `INV-PRIVACY` · **Order 5** · **Legacy:** `I-PRIV-N` family + bare `INV-N` in history/privacy files.
**Files:** `invariants.yaml`, `invariants.md`, `INV-PRIVACY` `owned_paths` files.

- [ ] Run the Phase 4 per-scope procedure (Steps 1–6) for `INV-PRIVACY`.

### Task 10: Migrate INV-ACCESS

**Scope:** `INV-ACCESS` · **Order 6** · **Legacy:** bare `INV-N` in `internal/access` (ABAC).
**Files:** `invariants.yaml`, `invariants.md`, `INV-ACCESS` `owned_paths` files.

- [ ] Run the Phase 4 per-scope procedure (Steps 1–6) for `INV-ACCESS`.

### Task 11: Migrate INV-CLUSTER

**Scope:** `INV-CLUSTER` · **Order 7** · **Legacy:** bare `INV-N` in `internal/cluster` + crypto-invalidation/wire-coordination refs (per the `INV-CRYPTO` boundary note, invalidation pings → `CLUSTER`).
**Files:** `invariants.yaml`, `invariants.md`, `INV-CLUSTER` `owned_paths` files.

- [ ] Run the Phase 4 per-scope procedure (Steps 1–6) for `INV-CLUSTER`.

### Task 12: Migrate INV-EVENTBUS

**Scope:** `INV-EVENTBUS` · **Order 8** (large) · **Legacy:** `GW` (~96) + `ROPS` (~20) + bare `INV-N` in `internal/eventbus` (~45) + colon-eradication refs + any `WS`/`RA` Task 2 assigns here.
**Files:** `invariants.yaml`, `invariants.md`, `INV-EVENTBUS` `owned_paths` files.

- [ ] Run the Phase 4 per-scope procedure (Steps 1–6) for `INV-EVENTBUS`.

### Task 13: Migrate INV-SCENE

**Scope:** `INV-SCENE` · **Order 9** (largest) · **Legacy:** `P4` (~118) + `P5` (~175) + `P6` (~85) + `FS` (~27) + `FW` + phase-8 bare `INV-N`.
**Files:** `invariants.yaml`, `invariants.md`, `INV-SCENE` `owned_paths` files.

- [ ] Run the Phase 4 per-scope procedure (Steps 1–6) for `INV-SCENE`.

### Task 14: Migrate INV-PLUGIN (P7 split, part 1)

**Scope:** `INV-PLUGIN` · **Order 10** · **Legacy:** `PC` (~29) + pluginauthz bare `INV-N` + the **PLUGIN half of `P7`**.
**Files:** `invariants.yaml`, `invariants.md`, `INV-PLUGIN` `owned_paths` files.

- [ ] **P7 split prerequisite:** before migrating, classify each `INV-P7-N` ref as CRYPTO-origin or PLUGIN-origin by its annotation text + origin spec; record the PLUGIN ones here and the CRYPTO ones in Task 15. Then run the Phase 4 per-scope procedure (Steps 1–6) for `INV-PLUGIN`.

### Task 15: Migrate INV-CRYPTO (P7 split, part 2 — last)

**Scope:** `INV-CRYPTO` · **Order 11** (last; hardest) · **Legacy:** `RB` (~180) + crypto bare `INV-1..52` + the **CRYPTO half of `P7`**.
**Files:** `invariants.yaml`, `invariants.md`, `INV-CRYPTO` `owned_paths` files.

- [ ] Using the P7 classification from Task 14, run the Phase 4 per-scope procedure (Steps 1–6) for `INV-CRYPTO`. This is the original Task-4 territory — the closed-world tool + provenance guard make the crypto rename safe this time.

---

## Phase 5: Cleanup & verification

### Task 16: Retire the per-family meta-tests (helper-safe)

**Files:**

- Create: `test/meta/meta_helpers_test.go` (new home for shared helpers `findRepoRoot` + `skipDirs` — see Step 2)
- Modify: `test/meta/inv_binding_test.go` (remove the shared `findRepoRoot` AND `skipDirs` before deletion is even considered)
- Delete: the empirically-confirmed subsumed set (Step 1) — candidates: `test/meta/i_priv_coverage_test.go` (`I-PRIV-*`), `test/meta/i_pres_coverage_test.go` (`I-PRES-*`), `test/meta/inv_binding_test.go`, `test/meta/scenes_phase6_invariants_test.go`
- Modify: `test/quarantine.yaml` if any retired test is referenced there

> **GROUNDED WARNING (do not skip Step 2).** Both `findRepoRoot`
> (`test/meta/inv_binding_test.go:125`) and `skipDirs` (`~:42`) are defined in
> `inv_binding_test.go` and used by surviving files — `skipDirs` by
> `invariant_registry_test.go:102`, `findRepoRoot` by **14** `test/meta` files,
> most of which are NOT retired (`liveness_invariants_test.go`,
> `proto_doc_comments_test.go`, `pr_prep_fast_lane_test.go`,
> `focus_delta_gate_test.go`, `depguard_config_test.go`,
> `ci_required_jobs_test.go`, `quarantine_registry_test.go`,
> `grpc_api_coverage_test.go`, `tooling_no_mandatory_int_test.go`,
> `plugin_config_invariants_test.go`, and others). Deleting `inv_binding_test.go`
> without first relocating `findRepoRoot` breaks compilation of the entire
> `test/meta` package. There are **no** `inv_p4_coverage_meta_test.go` /
> `inv_p5_coverage_meta_test.go` files — the P4/P5 invariants live only as
> `INV-P4-*`/`INV-P5-*` annotations migrated under SCENE in Phase 4, not as
> standalone meta-tests.

- [ ] **Step 1: Determine the actual retirement set empirically**

```bash
# Per-family coverage/binding meta-tests still present (legacy single-I + binding):
rg -l 'I-PRIV-|I-PRES-' test/meta/
rg -ln 'func Test.*[Bb]inding|func Test.*[Cc]overage' test/meta/inv_binding_test.go test/meta/i_priv_coverage_test.go test/meta/i_pres_coverage_test.go test/meta/scenes_phase6_invariants_test.go
```

Record the confirmed set (only files whose assertions are now subsumed by the
provenance guard + `TestEveryRegistryInvariantHasBinding`). Do NOT include any
file that another surviving test needs to compile (see Step 2).

- [ ] **Step 2: Relocate shared helpers to a surviving file FIRST**

Move `findRepoRoot` (and any other package-level helper defined in a
to-be-deleted file but used elsewhere) into a new `test/meta/meta_helpers_test.go`:

**Both `findRepoRoot` AND `skipDirs` must move** — both are defined in
`inv_binding_test.go` (`skipDirs` at ~line 42, `findRepoRoot` at line 125) and
both are referenced by the surviving `invariant_registry_test.go`
(`skipDirs` at line 102) and ~14 other test/meta files. The bodies below are
copied verbatim from `inv_binding_test.go` on the `invariant-registry-infra`
branch — paste exactly, then delete both definitions from `inv_binding_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"testing"
)

// skipDirs are directories the meta-test walkers MUST NOT descend into.
// Relocated from inv_binding_test.go (holomush-hz0v4.14) so it survives the
// per-family meta-test retirement; used by invariant_registry_test.go and others.
var skipDirs = map[string]struct{}{
	".git":         {},
	".jj":          {},
	".worktrees":   {},
	"vendor":       {},
	"node_modules": {},
	"bin":          {},
	"build":        {},
	"dist":         {},
}

// findRepoRoot walks upward from the test's working directory until it finds a
// directory containing go.mod, which marks the repository root. Relocated from
// inv_binding_test.go (holomush-hz0v4.14); used by ~14 test/meta files.
func findRepoRoot(t *testing.T) string {
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
			t.Fatalf("could not find repo root (no go.mod found in any parent of %q)", dir)
		}
		dir = parent
	}
}
```

Then delete BOTH the `skipDirs` and `findRepoRoot` definitions from
`inv_binding_test.go`. (Re-verify their exact current bodies on the
`invariant-registry-infra` branch before pasting — preserve any logic they
actually contain.)

- [ ] **Step 3: Verify the package still compiles with the helper relocated**

Run: `task test -- ./test/meta/`
Expected: PASS (no `undefined: findRepoRoot`), proving the relocation is correct BEFORE any deletion.

- [ ] **Step 4: Confirm the guard subsumes each retirement candidate, then delete**

For each candidate, confirm its invariants are in the registry with `refs` and a
`migrated` scope (`rg '<family>' docs/architecture/invariants.yaml`). Delete only
the confirmed, helper-free set:

```bash
rm test/meta/i_priv_coverage_test.go test/meta/i_pres_coverage_test.go \
   test/meta/inv_binding_test.go test/meta/scenes_phase6_invariants_test.go
```

- [ ] **Step 5: Verify the suite still builds and coverage is not regressed**

Run: `task test -- ./test/meta/` and `task test:int`
Expected: PASS; the provenance guard + `TestEveryRegistryInvariantHasBinding` enforce the same IDs the retired tests did.

- [ ] **Step 6: Commit**

`jj commit -m "test(invariants): retire per-family meta-tests subsumed by the provenance guard; relocate findRepoRoot (holomush-hz0v4.14)"`

---

### Task 17: Final verification

**Files:** none (verification only)

- [ ] **Step 1: No bare `INV-N` remains in any migrated scope**

Run: `rg -n '\bINV-[0-9]+\b' internal/ test/ plugins/`
Expected: only matches inside `shared_files` pending entries (if any scope is still `pending`); zero in `migrated` scopes.

- [ ] **Step 2: Registry complete + all gates green**

Run: `task test -- ./test/meta/`, `task lint:invariants` (runs `inv-render -check`), `task lint`, `task test`, `task test:int`
Expected: all green; every `scopes:` entry `status: migrated`; `TestEveryRegistryInvariantHasBinding` green (pending entries tolerated per `.10`).

- [ ] **Step 3: Confirm the negative test still bites**

Run: `task test -- -run TestProvenanceGuardFailsOnMislabel ./test/meta/`
Expected: PASS (the guard still fails on a seeded mislabel — the F2 defense is live).

- [ ] **Step 4: Close-out commit**

`jj commit -m "chore(invariants): final verification — registry complete, provenance guard live (holomush-hz0v4.14)"`

### Verification result (2026-06-05, `holomush-hz0v4.14.17`) — CERTIFIED

- **Step 1 — no residual.** Zero bare `INV-N` in any `migrated`-scope `owned_paths` file
  (mechanical sweep across all 12 migrated scopes). Letter-prefixed legacy tokens
  (`INV-L/A/B/LP`, `INV-GW` fixtures, …) are accounted for in the redesign spec's
  **Residual classification** record — every `INV-*` in the tree is classified.
- **Step 2 — gates green.** `task test -- ./test/meta/` (53), `task lint:invariants`
  (`inv-render -check`), `task lint`, build all green. Registry holds **300 invariant
  entries** across **14 scopes**; `INV-CRYPTO` is `1..115` (the whole crypto epic —
  master + RB + P7 + sub-epics D (→68..87) / E (→88..115) / F (→53..67, `.14.23`) — unified under one scope).
- **Step 3 — F2 defense live.** `TestProvenanceGuardFailsOnMislabel` +
  `…FailsOnResidualLegacyToken` + `…FailsOnMalformedGlob` + `TestOwnedPathsPartitionSemantics`
  all bite. The guard fails loudly on a seeded mislabel, a leftover legacy token, a
  malformed glob, and an ambiguous ownership overlap.

**Scope status:** 12 scopes `migrated`; **2 scopes (`INV-BRANDING`, `INV-DOCS`) remain
`pending` by design** — these are separate per-scope migrations of local-numbered subsystem
invariants the original §2.1 family map did not cover, deferred and tracked as their own
beads. Their pending `owned_paths` are not residual-walked, so their bare `INV-N` is expected
and guard-inert. The migration epic (`holomush-hz0v4.14`) is **COMPLETE** for every
cross-cutting and crypto family; BRANDING/DOCS are acknowledged future work, not a gap.

> **Scope expansion (`.14.27`–`.32`).** A census during `.14.28` found large prefixed
> families the §2.1 map missed — crypto sub-epics **D** (`INV-D`→`INV-CRYPTO-68..87`) and
> **E** (`INV-E`→`INV-CRYPTO-88..115`), migrated in `.14.29`/`.14.30`; the guard was
> hardened to catch leftover *legacy* tokens + walk file-globs + detect semantic ownership
> overlaps (`.14.20`/`.14.21`); per-spec local families (`INV-L/A/B/LP`, world, auth,
> settings, web, meta-tests) were classified exempt (`.14.27`/`.14.31`). The redesign spec's
> Residual classification section is the authoritative completeness record.

---

## Out of scope

- `// Verifies:` binding backfill for binding-less invariants → `holomush-hz0v4.11` (`binding: pending` tolerates the gap).
- Public-facing curated subset (`site/docs/reference/invariants.md`) → deferred per the original design.
<!-- adr-capture: sha256=f660e1df6cc61a05; session=cli; ts=2026-06-01T15:03:24Z; adrs= -->
