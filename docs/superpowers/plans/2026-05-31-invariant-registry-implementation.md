<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Central Invariant Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish a single canonical registry of all named system invariants with a unified `INV-<SCOPE>-<N>` naming scheme, paired with a YAML-backed drift meta-test that replaces per-family hardcoded slices.

**Architecture:** A YAML sidecar (`docs/architecture/invariants.yaml`) is the machine-readable source of truth. A matching markdown doc (`docs/architecture/invariants.md`) is the human-readable view. A single meta-test at `test/meta/invariant_registry_test.go` reads the YAML and verifies every invariant has at least one `// Verifies:` binding. A CI lint check verifies YAML↔markdown consistency. The existing per-family meta-tests are retired after the unified test passes.

**Tech Stack:** Go 1.26, `gopkg.in/yaml.v3` (already in go.mod), `os.Root` for repo file walking (existing meta-test pattern), `testing` stdlib (no Ginkgo).

---

## File structure

| File | Purpose |
|------|---------|
| `docs/architecture/invariants.md` | Human-readable registry — scope index + invariant tables |
| `docs/architecture/invariants.yaml` | Machine-readable registry — same data, structured for meta-test consumption |
| `test/meta/invariant_registry_test.go` | Unified drift meta-test — replaces per-family slices |
| `scripts/check-invariant-registry-consistency.sh` | Lint check: YAML ↔ markdown consistency |
| `task lint:invariants` | Taskfile entry for the consistency check |

---

## Phase 1: Scaffold

### Task 1: Create directory and YAML schema

**Files:**

- Create: `docs/architecture/invariants.yaml`
- Create: `docs/architecture/invariants.md`

**Goal:** Bootstrap the registry with the scope index and YAML schema. The YAML starts empty (no invariants listed yet) but defines the structure. The markdown carries the scope index and an empty per-scope table placeholder.

- [ ] **Step 1: Create `docs/architecture/` directory**

```bash
mkdir -p docs/architecture
```

- [ ] **Step 2: Write the YAML skeleton**

```yaml
# HoloMUSH Invariant Registry — machine-readable source of truth.
# Paired with invariants.md (human-readable view).
# The meta-test at test/meta/invariant_registry_test.go reads this file.
# CI check scripts/check-invariant-registry-consistency.sh verifies
# this file and invariants.md are in sync.

scopes:
  - name: INV-CRYPTO
    description: "Event payload encryption, DEK lifecycle, key wrapping, decryption delivery, participant sets, AdminReadStream"
    boundary: "Cryptographic operations on event payloads. Does NOT include: audit projection (→ INV-EVENTBUS), plugin manifest validation (→ INV-PLUGIN), cluster coordination (→ INV-CLUSTER). Crypto invariants that operate on in-process state (DEK cache, key material, envelope codec) belong here; invariants that govern wire-level coordination between replicas (invalidation pings, probe-and-pill, N-of-N ack contracts) belong under INV-CLUSTER."

  - name: INV-PRIVACY
    description: "Stream history temporal floors, scope gating, guest-session bounds, reattach/Idle arrival-timestamp semantics"
    boundary: "Privacy-relevant gating on history reads. Does NOT include: ABAC policy evaluation (→ INV-ACCESS), subscribe authorization (→ INV-EVENTBUS)."

  - name: INV-PRESENCE
    description: "Presence snapshot correctness, field enumeration, client-side dedup, ownership obscuration"
    boundary: "Current-state presence queries. Does NOT include: session status lifecycle (→ INV-SESSION)."

  - name: INV-SCENE
    description: "Scene lifecycle, board queries, content warnings, pose ordering, focus model, publish snapshot/state, IC isolation, history readability"
    boundary: "All scene-domain behavior. Cross-cuts multiple Phase specs (P4–P8)."

  - name: INV-PLUGIN
    description: "Runtime symmetry, manifest validation, hostfunc safety, emit gates, setting isolation, plugin authz"
    boundary: "Plugin-system contracts applicable to both Lua and binary runtimes. Does NOT include: plugin crypto wiring (→ INV-CRYPTO)."

  - name: INV-EVENTBUS
    description: "Subject naming, JetStream consumer config, audit projection, delivery contracts, tier routing, rendering completeness, colon eradication"
    boundary: "Event infrastructure. Does NOT include: event payload encryption (→ INV-CRYPTO), history privacy gating (→ INV-PRIVACY)."

  - name: INV-CLUSTER
    description: "Member identity, heartbeats, cache invalidation (cross-replica coordination path), probe-and-pill, clock independence"
    boundary: "Multi-replica coordination. Includes cluster-scoped invalidation contracts (e.g., INV-28/INV-29 N-of-N ack pings, INV-56 Coordinator retry limits, INV-59 cache-invalidation correctness) that govern wire-level behavior between replicas. Does NOT include single-process DEK operations (→ INV-CRYPTO)."

  - name: INV-ACCESS
    description: "ABAC policy evaluation, attribute provider invariants, seed policy shape, authorization decisions"
    boundary: "Access control evaluation. Does NOT include: stream-access gating at gRPC boundary (→ INV-EVENTBUS)."

  - name: INV-SESSION
    description: "Session status lifecycle, connection attachment, focus membership, idle detection"
    boundary: "Session state machine. Does NOT include: presence snapshot (→ INV-PRESENCE)."

  - name: INV-STORE
    description: "Migration discipline, no-DELETE enforcement, spec compliance scanning"
    boundary: "Database invariants."

  - name: INV-TELEMETRY
    description: "Logging discipline, trace context, metric naming, sloglint policy"
    boundary: "Observability contracts."

  - name: INV-BRANDING
    description: "Asset integrity, palette tokens, logo generation"
    boundary: "Visual identity invariants. Does NOT include: docs quality (separate concern)."

  - name: INV-DOCS
    description: "Proto doc comments, doc IA, contributor onboarding surface"
    boundary: "Documentation quality invariants."

invariants: []
```

- [ ] **Step 3: Write the markdown with scope index**

```markdown
# HoloMUSH Invariant Registry

Canonical registry of all named system invariants. Paired with
`invariants.yaml` (machine-readable source of truth). The meta-test at
`test/meta/invariant_registry_test.go` reads the YAML file; the CI lint
check at `scripts/check-invariant-registry-consistency.sh` verifies this
document is in sync with the YAML.

## Scope index

| Scope | Description | Boundary |
|-------|-------------|----------|
| `INV-CRYPTO` | Event payload encryption, DEK lifecycle, key wrapping, decryption delivery, participant sets, AdminReadStream | Cryptographic operations on event payloads. Does NOT include: audit projection (→ `INV-EVENTBUS`), plugin manifest validation (→ `INV-PLUGIN`), cluster coordination (→ `INV-CLUSTER`). Crypto invariants that operate on in-process state (DEK cache, key material, envelope codec) belong here; invariants that govern wire-level coordination between replicas (invalidation pings, probe-and-pill, N-of-N ack contracts) belong under `INV-CLUSTER`. |
| `INV-PRIVACY` | Stream history temporal floors, scope gating, guest-session bounds, reattach/Idle arrival-timestamp semantics | Privacy-relevant gating on history reads. Does NOT include: ABAC policy evaluation (→ `INV-ACCESS`), subscribe authorization (→ `INV-EVENTBUS`). |
| `INV-PRESENCE` | Presence snapshot correctness, field enumeration, client-side dedup, ownership obscuration | Current-state presence queries. Does NOT include: session status lifecycle (→ `INV-SESSION`). |
| `INV-SCENE` | Scene lifecycle, board queries, content warnings, pose ordering, focus model, publish snapshot/state, IC isolation, history readability | All scene-domain behavior. Cross-cuts multiple Phase specs (P4–P8). |
| `INV-PLUGIN` | Runtime symmetry, manifest validation, hostfunc safety, emit gates, setting isolation, plugin authz | Plugin-system contracts applicable to both Lua and binary runtimes. Does NOT include: plugin crypto wiring (→ `INV-CRYPTO`). |
| `INV-EVENTBUS` | Subject naming, JetStream consumer config, audit projection, delivery contracts, tier routing, rendering completeness, colon eradication | Event infrastructure. Does NOT include: event payload encryption (→ `INV-CRYPTO`), history privacy gating (→ `INV-PRIVACY`). |
| `INV-CLUSTER` | Member identity, heartbeats, cache invalidation (cross-replica coordination path), probe-and-pill, clock independence | Multi-replica coordination. Includes cluster-scoped invalidation contracts (e.g., INV-28/INV-29 N-of-N ack pings, INV-56 Coordinator retry limits, INV-59 cache-invalidation correctness) that govern wire-level behavior between replicas. Does NOT include single-process DEK operations (→ `INV-CRYPTO`). |
| `INV-ACCESS` | ABAC policy evaluation, attribute provider invariants, seed policy shape, authorization decisions | Access control evaluation. Does NOT include: stream-access gating at gRPC boundary (→ `INV-EVENTBUS`). |
| `INV-SESSION` | Session status lifecycle, connection attachment, focus membership, idle detection | Session state machine. Does NOT include: presence snapshot (→ `INV-PRESENCE`). |
| `INV-STORE` | Migration discipline, no-DELETE enforcement, spec compliance scanning | Database invariants. |
| `INV-TELEMETRY` | Logging discipline, trace context, metric naming, sloglint policy | Observability contracts. |
| `INV-BRANDING` | Asset integrity, palette tokens, logo generation | Visual identity invariants. Does NOT include: docs quality (separate concern). |
| `INV-DOCS` | Proto doc comments, doc IA, contributor onboarding surface | Documentation quality invariants. |

A new scope is warranted when at least 3 invariants exist that don't fit an
existing scope's boundary, or when a new major subsystem ships with its own
invariants.

## Invariant tables

<!-- Invariant tables are generated from invariants.yaml by the lint check.
     Do not edit the tables directly; edit invariants.yaml instead. -->
```

- [ ] **Step 4: Commit**

```bash
jj commit -m "chore(invariants): scaffold registry directory and YAML schema"
```

---

## Phase 2: Meta-test

### Task 2: Write the unified drift meta-test

**Files:**

- Create: `test/meta/invariant_registry_test.go`

**Goal:** A single Go test that reads `docs/architecture/invariants.yaml`, enumerates every invariant entry, walks the repo's `*_test.go` files looking for `// Verifies: <id>` annotations, and fails if any invariant lacks a binding. Also scans `docs/superpowers/specs/` for orphan invariant IDs not in the registry.

The test follows the existing `inv_binding_test.go` pattern: `findRepoRoot(t)`, `os.OpenRoot`, `filepath.WalkDir`, `skipDirs`. It generalizes the hardcoded `[]int` slice into a YAML-parsed struct.

- [ ] **Step 1: Write the test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// registryEntry mirrors one invariant in invariants.yaml.
type registryEntry struct {
	ID         string   `yaml:"id"`
	Legacy     []string `yaml:"legacy"`
	Summary    string   `yaml:"summary"`
	Severity   string   `yaml:"severity"`
	Status     string   `yaml:"status"`
	AssertedBy []string `yaml:"asserted_by"`
	External   bool     `yaml:"external"`
}

type registryDoc struct {
	Scopes     []struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Boundary    string `yaml:"boundary"`
	} `yaml:"scopes"`
	Invariants []registryEntry `yaml:"invariants"`
}

// verifiesRE matches `// Verifies: INV-<SCOPE>-<N>` annotations in test files.
var verifiesRE = regexp.MustCompile(`//\s*Verifies:\s*(INV-[A-Z]+-\d+)`)

// invRefRE matches invariant IDs referenced in spec prose but not via Verifies.
// Used for the orphan-detection pass.
var invRefRE = regexp.MustCompile(`\b(INV-[A-Z]+-\d+)\b`)

// TestEveryRegistryInvariantHasBinding asserts that every invariant in
// docs/architecture/invariants.yaml has at least one test binding.
func TestEveryRegistryInvariantHasBinding(t *testing.T) {
	root := findRepoRoot(t)

	// 1. Parse the YAML registry.
	data, err := os.ReadFile(filepath.Join(root, "docs", "architecture", "invariants.yaml"))
	if err != nil {
		t.Fatalf("read invariants.yaml: %v", err)
	}
	var reg registryDoc
	if err := yaml.Unmarshal(data, &reg); err != nil {
		t.Fatalf("parse invariants.yaml: %v", err)
	}
	if len(reg.Invariants) == 0 {
		t.Fatal("invariants.yaml has zero entries — populate the registry first")
	}

	// Index by ID for fast lookup.
	byID := make(map[string]registryEntry, len(reg.Invariants))
	seenScopes := make(map[string]bool)
	for _, e := range reg.Invariants {
		if _, dup := byID[e.ID]; dup {
			t.Errorf("duplicate ID in registry: %s", e.ID)
		}
		byID[e.ID] = e
		// Extract scope from ID for scope-existence check.
		parts := strings.SplitN(e.ID, "-", 3)
		if len(parts) == 3 {
			seenScopes[parts[0]+"-"+parts[1]] = true
		}
	}

	// Cross-check: every scope declared in the YAML has at least one invariant.
	for _, sc := range reg.Scopes {
		if !seenScopes[sc.Name] {
			t.Errorf("scope %s declared in YAML scopes but has no invariants", sc.Name)
		}
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
		data, readErr := io.ReadAll(f)
		closeErr := f.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		matches := verifiesRE.FindAllSubmatch(data, -1)
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
	for _, e := range reg.Invariants {
		if e.External {
			for _, p := range e.AssertedBy {
				absPath := filepath.Join(root, p)
				if _, statErr := os.Stat(absPath); statErr != nil {
					t.Errorf("%s: external path %q does not exist: %v", e.ID, p, statErr)
				}
			}
			continue
		}
		if len(bindings[e.ID]) == 0 {
			t.Errorf("%s: no test binding found (expected at least one `// Verifies: %s` comment in a *_test.go file)", e.ID, e.ID)
		}
	}

	// 4. Orphan detection: scan specs/ for INV-<SCOPE>-<N> references not in registry.
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
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		matches := invRefRE.FindAllSubmatch(data, -1)
		for _, m := range matches {
			id := string(m[1])
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
```

- [ ] **Step 2: Verify the test compiles and fails correctly against empty registry**

```bash
cd "$(jj root)"
task test -- ./test/meta/ -run TestEveryRegistryInvariantHasBinding
```

Expected: FAIL with "invariants.yaml has zero entries"

- [ ] **Step 3: Commit**

```bash
jj commit -m "test(meta): add unified invariant registry drift meta-test"
```

---

## Phase 3: YAML↔Markdown consistency check

### Task 3: Write the consistency lint check

**Files:**

- Create: `scripts/check-invariant-registry-consistency.sh`
- Modify: `Taskfile.yaml` — add `lint:invariants` task

**Goal:** A CI check that verifies `invariants.yaml` and `invariants.md` are in sync — every YAML entry has a matching markdown table row and vice versa.

- [ ] **Step 1: Write the consistency check script**

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Verifies docs/architecture/invariants.yaml and invariants.md are in sync.
# - Every invariant ID in the YAML must appear in the markdown table.
# - Every invariant ID in the markdown table must appear in the YAML.
# - The scope count in YAML scopes: must match the scope index table rows.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
YAML="$REPO_ROOT/docs/architecture/invariants.yaml"
MD="$REPO_ROOT/docs/architecture/invariants.md"

if [[ ! -f "$YAML" ]]; then
  echo "ERROR: invariants.yaml not found at $YAML"
  exit 1
fi
if [[ ! -f "$MD" ]]; then
  echo "ERROR: invariants.md not found at $MD"
  exit 1
fi

# Extract IDs from YAML (lines matching "id: INV-...")
yaml_ids=$(grep -E '^[[:space:]]+id:[[:space:]]+INV-[A-Z]+-[0-9]+' "$YAML" | sed -E 's/^[[:space:]]+id:[[:space:]]+//' | sort)
if [[ -z "$yaml_ids" ]]; then
  echo "WARNING: invariants.yaml has no invariant entries yet — consistency check skipped"
  exit 0
fi

# Extract IDs from markdown table rows (lines matching "| `INV-...` |")
md_ids=$(grep -E '^\|[[:space:]]+`INV-[A-Z]+-[0-9]+`' "$MD" | sed -E 's/^.*`(INV-[A-Z]+-[0-9]+)`.*$/\1/' | sort)

# Every YAML ID must appear in markdown.
fail=0
while IFS= read -r id; do
  if ! echo "$md_ids" | grep -qxF "$id"; then
    echo "ERROR: YAML has $id but markdown table does not"
    fail=1
  fi
done <<< "$yaml_ids"

# Every markdown ID must appear in YAML.
while IFS= read -r id; do
  if ! echo "$yaml_ids" | grep -qxF "$id"; then
    echo "ERROR: markdown table has $id but YAML does not"
    fail=1
  fi
done <<< "$md_ids"

if [[ $fail -eq 0 ]]; then
  echo "✓ invariants.yaml and invariants.md are consistent ($(echo "$yaml_ids" | wc -l | tr -d ' ') invariants)"
fi

exit $fail
```

- [ ] **Step 2: Add lint task to Taskfile.yaml**

Add `lint:invariants` as a dependency in the `lint` task (after line 118 `lint:no-zensical`) and define the task after `lint:no-zensical`:

In the `lint` task deps list, add after `lint:no-zensical`:

```yaml
      - task: lint:invariants
```

Then define the task (e.g., after line 118, before `lint:adr`):

```yaml
  lint:invariants:
    desc: Check invariant registry YAML↔markdown consistency
    cmds:
      - bash scripts/check-invariant-registry-consistency.sh
```

- [ ] **Step 3: Verify the check runs**

```bash
cd "$(jj root)"
bash scripts/check-invariant-registry-consistency.sh
```

Expected: "invariants.yaml has no invariant entries yet — consistency check skipped" (Warning exit 0)

- [ ] **Step 4: Commit**

```bash
jj commit -m "chore(invariants): add YAML↔markdown consistency lint check"
```

---

## Phase 4: Catalog and rename — CRYPTO scope

### Task 4: Catalog INV-CRYPTO invariants

**Files:**

- Modify: `docs/architecture/invariants.yaml` — add all INV-CRYPTO-N entries
- Modify: `docs/architecture/invariants.md` — add the INV-CRYPTO table
- Modify: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` — rename bare INV-N to INV-CRYPTO-N in the invariant catalog (§9)
- Modify: ~98 Go files in `internal/eventbus/`, `internal/grpc/`, `internal/plugin/`, `test/integration/crypto/`, `test/integration/eventbus_e2e/` — rename `// Verifies: INV-N` to `// Verifies: INV-CRYPTO-N` and update all comment references

**Goal:** Catalog the crypto master spec's ~60 bare INV-N invariants into INV-CRYPTO-1 through INV-CRYPTO-N. This is the largest single scope.

**Approach:** The crypto master spec (`2026-04-25-event-payload-crypto-design.md`) defines invariants n1–n60 (rendered as `INV-1` through `INV-60` in code comments). Map each `INV-N` → `INV-CRYPTO-N` (1:1 mapping — no renumbering needed). The rename pass uses `rg -l` + `sed` per invariant ID across all Go files.

- [ ] **Step 1: Map the crypto invariants into YAML**

For each INV-N (N=1..60) in the crypto master spec, add an entry to `invariants.yaml`:

```yaml
  - id: INV-CRYPTO-1
    legacy: [INV-1]
    summary: "Operator MUST NOT see plaintext via live JetStream sub"
    severity: MUST
    status: active
    asserted_by:
      - test/integration/crypto/metadata_only_test.go
    external: false
  - id: INV-CRYPTO-2
    legacy: [INV-2]
    summary: "Direct SELECT payload FROM events_audit MUST NOT yield plaintext"
    severity: MUST
    status: active
    asserted_by:
      - test/integration/crypto/metadata_only_test.go
    external: false
  # ... continue for INV-3 through INV-60
```

The mapping is 1:1 — INV-N in the spec becomes INV-CRYPTO-N. Extract summaries from the spec's §9 invariant table.

- [ ] **Step 2: Generate the markdown INV-CRYPTO table**

Add to `invariants.md` under `## INV-CRYPTO`:

```markdown
## INV-CRYPTO

Source: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`

| ID | Legacy | Summary | Severity | Status | Asserted by |
|----|--------|---------|----------|--------|-------------|
| `INV-CRYPTO-1` | `INV-1` | Operator MUST NOT see plaintext via live JetStream sub | MUST | active | `test/integration/crypto/metadata_only_test.go` |
| `INV-CRYPTO-2` | `INV-2` | Direct SELECT payload FROM events_audit MUST NOT yield plaintext | MUST | active | `test/integration/crypto/metadata_only_test.go` |

...
```

- [ ] **Step 3: Rename INV-N → INV-CRYPTO-N across all Go files**

For each N in 1..60, run:

```bash
cd "$(jj root)"
# Find files with bare INV-N (not part of a larger ID like INV-NN or INV-P7-N)
rg -g '*.go' -l "INV-$N\b" internal/ test/ plugins/ | while read f; do
  # Replace "INV-N" with "INV-CRYPTO-N" (word-boundary match)
  sed -i.bak -E "s/INV-${N}\b/INV-CRYPTO-${N}/g" "$f" && rm -f "${f}.bak"
done
```

Special care: ensure `INV-1` is not also `INV-10`, `INV-11`, etc. Use `\b` word boundary in the sed pattern, or process in descending order (60→1) to avoid substring matches.

- [ ] **Step 4: Rename in the crypto master spec**

Update `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` — rename `INV-N` to `INV-CRYPTO-N` in the invariant catalog table (§9, approximately line references near the `n1..n60` entries).

- [ ] **Step 5: Run the unified meta-test**

```bash
task test -- ./test/meta/ -run TestEveryRegistryInvariantHasBinding
```

Expected: PASS (every INV-CRYPTO-N has a binding)

- [ ] **Step 6: Run the consistency check**

```bash
bash scripts/check-invariant-registry-consistency.sh
```

Expected: "✓ invariants.yaml and invariants.md are consistent"

- [ ] **Step 7: Run lint and unit tests**

```bash
task lint
task test
```

- [ ] **Step 8: Commit**

```bash
jj commit -m "refactor(invariants): rename INV-N to INV-CRYPTO-N across crypto domain (N=1..60)"
```

---

## Phase 5: Catalog and rename — remaining scopes

### Task 5: Catalog SCENE scope invariants

**Files:**

- Modify: `docs/architecture/invariants.yaml` — add INV-SCENE-N entries
- Modify: `docs/architecture/invariants.md` — add INV-SCENE table
- Modify: ~15 spec files in `docs/superpowers/specs/` — rename INV-P4-N, INV-P5-N, INV-P6-N, INV-RB-N, INV-GW-N, INV-LOAD-N, INV-TS-N, INV-WS-N, INV-PC-N, INV-FS-N, INV-FW-N, INV-SH-N, INV-RA-N, Phase 8 bare INV-N to INV-SCENE-N
- Modify: ~90 Go files in `plugins/core-scenes/`, `test/integration/scenes/`, `test/integration/privacy/`, `test/meta/`, `internal/grpc/focus/`, `internal/test/invariants/`

**Goal:** Consolidate all scene-domain invariant families under INV-SCENE. The scene scope is the most complex because it spans multiple Phase specs with overlapping numbering (INV-P4-1, INV-P5-1, INV-P6-1, INV-RB-1, INV-GW-1 are all different invariants).

**Mapping strategy:** Sequential numbering across all scene invariant families. Preserve the per-phase grouping by allocating number ranges:

- INV-P4-N → INV-SCENE-N (e.g., INV-P4-1..13 → INV-SCENE-1..13)
- INV-P5-N → INV-SCENE-(14+offset) (e.g., INV-P5-1..14 → INV-SCENE-14..27)
- INV-P6-N → INV-SCENE-(28+offset)
- INV-RB-N → INV-SCENE-(next range)
- INV-GW-N → INV-SCENE-(next range)
- INV-LOAD-N → INV-SCENE-(next range)
- INV-TS-N → INV-SCENE-(next range)
- INV-WS-N → INV-SCENE-(next range)
- INV-PC-N → INV-SCENE-(next range)
- INV-FS-N → INV-SCENE-(next range)
- INV-FW-N → INV-SCENE-(next range)
- INV-SH-N → INV-SCENE-(next range)
- INV-RA-N → INV-SCENE-(next range)
- Phase 8 bare INV-N → INV-SCENE-(next range)

The exact ranges are determined during cataloging. Record each INV-SCENE-N's legacy ID for traceability.

- [ ] **Step 1: Enumerate all scene-domain invariants with legacy IDs**

Run the enumeration queries:

```bash
cd "$(jj root)"
# Gather all scoped INV IDs from scene-related specs
for prefix in P4 P5 P6 RB GW LOAD TS WS PC FS FW SH RA; do
  echo "=== INV-${prefix} ==="
  rg -roh --no-filename "INV-${prefix}-\d+" docs/superpowers/specs/ | sort -u
done
# Gather bare INV-N from Phase 8 spec
rg -roh --no-filename "INV-\d+" docs/superpowers/specs/2026-05-29-scenes-phase-8-board-content-warnings-design.md | sort -u
```

- [ ] **Step 2: Assign INV-SCENE-N IDs and add to YAML**

For each legacy invariant, add an entry to `invariants.yaml` under scope `INV-SCENE`. Extract summaries from each source spec's invariant table.

- [ ] **Step 3: Generate the markdown INV-SCENE table**

Add to `invariants.md` under `## INV-SCENE` with a source note referencing the primary spec per invariant family.

- [ ] **Step 4: Rename across all Go files**

For each legacy→canonical mapping, run the sed pass across Go files.

- [ ] **Step 5: Rename in source specs**

Update each scene-domain spec's invariant section to use INV-SCENE-N.

- [ ] **Step 6: Verify**

```bash
task test -- ./test/meta/ -run TestEveryRegistryInvariantHasBinding
bash scripts/check-invariant-registry-consistency.sh
task lint
task test
```

- [ ] **Step 7: Commit**

```bash
jj commit -m "refactor(invariants): rename scene-domain invariants to INV-SCENE-N"
```

### Task 6: Catalog PRIVACY and PRESENCE scope invariants

**Files:**

- Modify: `docs/architecture/invariants.yaml` — add INV-PRIVACY-N and INV-PRESENCE-N entries
- Modify: `docs/architecture/invariants.md` — add INV-PRIVACY and INV-PRESENCE tables
- Modify: `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` — rename I-PRIV-N to INV-PRIVACY-N
- Modify: `docs/superpowers/specs/2026-05-19-presence-snapshot-design.md` — rename I-PRES-N to INV-PRESENCE-N
- Modify: `test/meta/i_priv_coverage_test.go`, `test/meta/i_pres_coverage_test.go` — update annotation regexes (temporary; these files are retired in Task 8)
- Modify: Go test files in `test/integration/privacy/`, `test/integration/presence/`, `test/integration/session/` — rename annotations

**Goal:** Simple 1:1 mapping: I-PRIV-N → INV-PRIVACY-N, I-PRES-N → INV-PRESENCE-N.

- [ ] **Step 1: Add YAML entries for INV-PRIVACY-1..8 and INV-PRESENCE-1..9**

Extract summaries from the source specs' invariant sections.

- [ ] **Step 2: Generate markdown tables**

- [ ] **Step 3: Rename I-PRIV-N → INV-PRIVACY-N across all files**

```bash
cd "$(jj root)"
rg -g '*.go' -g '*.md' -l 'I-PRIV-\d+' | while read f; do
  sed -i.bak -E 's/I-PRIV-([0-9]+)/INV-PRIVACY-\1/g' "$f" && rm -f "${f}.bak"
done
```

- [ ] **Step 4: Rename I-PRES-N → INV-PRESENCE-N across all files**

```bash
cd "$(jj root)"
rg -g '*.go' -g '*.md' -l 'I-PRES-\d+' | while read f; do
  sed -i.bak -E 's/I-PRES-([0-9]+)/INV-PRESENCE-\1/g' "$f" && rm -f "${f}.bak"
done
```

- [ ] **Step 5: Verify and commit**

```bash
task test -- ./test/meta/ -run TestEveryRegistryInvariantHasBinding
bash scripts/check-invariant-registry-consistency.sh
jj commit -m "refactor(invariants): rename I-PRIV-N to INV-PRIVACY-N, I-PRES-N to INV-PRESENCE-N"
```

### Task 7: Catalog remaining scopes (PLUGIN, EVENTBUS, CLUSTER, ACCESS, SESSION, STORE, TELEMETRY, BRANDING, DOCS)

**Files:**

- Modify: `docs/architecture/invariants.yaml` — add entries for remaining scopes
- Modify: `docs/architecture/invariants.md` — add tables for remaining scopes
- Modify: Spec and Go files across the codebase — rename INV-S5-N, INV-W9ML-N, INV-ROPS-N, INV-M-N, INV-A-N..INV-F-N, INV-L-N, INV-LP-N, INV-Y-N, and any remaining bare INV-N to their canonical scope IDs

**Goal:** Catalog all remaining invariants. These scopes are smaller (5-15 invariants each).

**Mapping strategy:**

- INV-S5-N → INV-PLUGIN-N (manifest, runtime symmetry)
- INV-W9ML-N, INV-ROPS-N → INV-EVENTBUS-N (legacy ID elimination, colon eradication, subject naming)
- INV-A-N..INV-F-N, INV-M-N → bucket into appropriate scopes based on source spec domain
- INV-GW-N → INV-EVENTBUS or INV-ACCESS (check source: gateway boundary invariants)
- INV-L-N → INV-SESSION (likely: session lifecycle)
- INV-LP-N → INV-TELEMETRY (likely: logging policy)
- INV-Y-N → determine scope from source spec

The exact mapping is determined during cataloging by inspecting each invariant's source spec.

- [ ] **Step 1: Enumerate remaining invariants by scope**

```bash
cd "$(jj root)"
# List all INV-<prefix>-N forms not yet cataloged
rg -roh --no-filename -g '*.go' -g '*.md' 'INV-[A-Z]+-\d+' | grep -v 'INV-CRYPTO\|INV-SCENE\|INV-PRIVACY\|INV-PRESENCE' | sort -u
```

- [ ] **Step 2: For each, determine the canonical scope by reading its source spec**

- [ ] **Step 3: Assign IDs, add to YAML, generate markdown tables**

- [ ] **Step 4: Rename across all files**

- [ ] **Step 5: Verify meta-test and consistency**

```bash
task test -- ./test/meta/ -run TestEveryRegistryInvariantHasBinding
bash scripts/check-invariant-registry-consistency.sh
```

- [ ] **Step 6: Commit**

```bash
jj commit -m "refactor(invariants): catalog and rename remaining invariant families"
```

---

## Phase 6: Retire old meta-tests

### Task 8: Retire per-family meta-tests

**Files:**

- Delete: `test/meta/i_priv_coverage_test.go`
- Delete: `test/meta/i_pres_coverage_test.go`
- Delete: `test/meta/inv_binding_test.go`
- Delete: `internal/test/invariants/inv_p4_coverage_meta_test.go`
- Delete: `internal/test/invariants/inv_p5_coverage_meta_test.go`
- Delete: `test/meta/scenes_phase6_invariants_test.go`
- Delete: `test/meta/plugin_config_invariants_test.go`

**Goal:** Remove the per-family meta-tests now that `TestEveryRegistryInvariantHasBinding` covers all invariants in one pass.

- [ ] **Step 1: Verify the unified test passes against all invariants**

```bash
task test -- ./test/meta/ -run TestEveryRegistryInvariantHasBinding -v
```

Must be PASS with no failures. If any invariant lacks a binding, fix it before proceeding.

- [ ] **Step 2: Delete the old meta-test files**

```bash
rm test/meta/i_priv_coverage_test.go
rm test/meta/i_pres_coverage_test.go
rm test/meta/inv_binding_test.go
rm internal/test/invariants/inv_p4_coverage_meta_test.go
rm internal/test/invariants/inv_p5_coverage_meta_test.go
rm test/meta/scenes_phase6_invariants_test.go
rm test/meta/plugin_config_invariants_test.go
```

- [ ] **Step 3: Verify full test suite passes without the deleted files**

```bash
task test
task lint
```

- [ ] **Step 4: Commit**

```bash
jj commit -m "refactor(meta): retire per-family invariant meta-tests in favor of unified registry test"
```

---

## Phase 7: Final verification

### Task 9: Final verification

**Files:** (none — verification only)

**Goal:** Confirm the entire invariant registry system is correct and passes all gates.

- [ ] **Step 1: Run the unified meta-test**

```bash
task test -- ./test/meta/ -run TestEveryRegistryInvariantHasBinding -v
```

Expected: PASS. Every registry row has at least one `// Verifies:` annotation.

- [ ] **Step 2: Run the consistency check**

```bash
bash scripts/check-invariant-registry-consistency.sh
```

Expected: "✓ invariants.yaml and invariants.md are consistent"

- [ ] **Step 3: Run full lint and test suite**

```bash
task lint
task test
task test:int
```

- [ ] **Step 4: Run pr-prep fast lane**

```bash
task pr-prep
```

- [ ] **Step 5: Verify no bare INV-N, I-PRIV-N, or I-PRES-N remain**

```bash
cd "$(jj root)"
# Check for any legacy invariant IDs still in code (should be zero)
rg -g '*.go' -g '*.md' 'I-PRIV-\d+|I-PRES-\d+' | grep -v 'invariants.yaml\|invariants.md' | grep -v 'legacy:' || echo "✓ No legacy IDs found"
```

- [ ] **Step 6: Annotate the orphan-detection pass works**

Add a fake `INV-TEST-999` reference to a spec file, run the meta-test, confirm it's flagged as an orphan. Then remove the fake reference.

```bash
echo "See INV-TEST-999 for details." >> docs/superpowers/specs/2026-05-31-invariant-registry-design.md
task test -- ./test/meta/ -run TestEveryRegistryInvariantHasBinding
# Expected: FAIL with "orphan invariant INV-TEST-999"
# Clean up:
sed '$d' docs/superpowers/specs/2026-05-31-invariant-registry-design.md > /tmp/clean.md && mv /tmp/clean.md docs/superpowers/specs/2026-05-31-invariant-registry-design.md
```

- [ ] **Step 7: Commit**

```bash
jj commit -m "chore(invariants): final verification of invariant registry completeness"
```<!-- adr-capture: sha256=e3b0c44298fc1c14; session=cli; ts=2026-05-31T20:26:33Z; adrs=holomush-6wcf2,holomush-4v2dq -->
<!-- adr-capture: sha256=198920a0c7ffcfa7; session=cli; ts=2026-05-31T20:26:49Z; adrs=holomush-6wcf2,holomush-4v2dq -->
