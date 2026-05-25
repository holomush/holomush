# Centralized Blast-Radius Code-Intelligence MCP — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Empirically pick a Go-accurate blast-radius code-intelligence tool, build the holomush-side index-generation + a manifest-DAG cross-boundary fill, and produce a deployment handoff contract for the homelab repo to stand up the central MCP server.

**Architecture:** A central, cluster-hosted code-intelligence MCP serves one shared index per repo-identity/SHA to all ephemeral jj workspaces over remote MCP (acceptance #2/#3). Phase 0 is a bake-off whose accuracy gate (Go interface-dispatch blast radius) decides the tool before any build work. holomush owns index generation, the manifest requires/provides fill, and the handoff contract; the homelab repo owns cluster manifests/ingress/GitOps (out of scope here).

**Tech Stack:** CKB (`@tastehub/ckb`, SCIP via `scip-go`) as the provisional winner; Joern (CPG + ArangoDB) as the conditional fallback contender; Go (`internal/plugin` manifest parsing) for the fill extractor; Taskfile + CI for index generation.

**Spec:** `docs/superpowers/specs/2026-05-25-code-intel-blast-radius-mcp-design.md` (bead holomush-i9599, design-reviewer READY).

---

## File Structure

| Path | Responsibility | Phase |
| --- | --- | --- |
| `docs/codeintel/2026-05-25-blast-radius-fixtures.md` | Hand-verified ground-truth caller sets (3 difficulty tiers), cited `path:line` | 0 |
| `docs/codeintel/2026-05-25-bakeoff-scorecard.md` | Accuracy/latency/footprint/ops scorecard + winner decision + gap doc | 0 |
| `internal/codeintel/manifestfill/fill.go` | Build a cross-boundary fill graph from plugin manifests (requires↔provides edges) | 1 |
| `internal/codeintel/manifestfill/fill_test.go` | Table tests for the fill graph builder | 1 |
| `cmd/codeintel-fill/main.go` | CLI entrypoint: scan plugins dir → emit fill graph JSON | 1 |
| `Taskfile.yaml` (modify) | `codeintel:index` task (scip-go + ckb index + fill) | 1 |
| `.github/workflows/*` (modify or add) | CI step producing the index + fill artifact at a SHA | 1 |
| `docs/codeintel/2026-05-25-deployment-handoff-contract.md` | The artifact that crosses to the homelab repo | 1 |

The PoC extractor (`internal/codeintel/manifestfill/`) is **tool-independent** — it parses
`plugin.yaml` files regardless of which code-intelligence tool wins — so it may begin
alongside Phase 0. Everything that composes the fill into a specific tool's impact output
(Task 7) and the index-generation wiring (Task 5) is gated on the Phase-0 winner.

---

## Phase 0: Bake-off & tool decision (hard blocker)

No deployment or wiring work begins until Task 4 records a winner that passes the
interface-dispatch accuracy gate.

### Task 1: Author the ground-truth blast-radius fixture set

**Files:**

- Create: `docs/codeintel/2026-05-25-blast-radius-fixtures.md`

- [ ] **Step 1: Pick the three fixture symbols (one per difficulty tier)**

Use `mcp__probe__search_code` / `mcp__probe__extract_code` to identify real symbols and
confirm they exist:

- Direct-call control: an ordinary exported function with a small, knowable caller set.
- Shared type (wide blast): `core.NewEvent` (per CLAUDE.md, all `core.Event` literals route through it).
- Interface dispatch (hard): a `ServiceRegistry` or `EventBus` interface method.

- [ ] **Step 2: Establish ground truth by hand**

For each fixture symbol, find the transitive caller set with probe and `rg`, and record
each caller at `path:line`. This is the human-verified answer the tools are scored against.
Document the method used (queries run) so it is reproducible.

- [ ] **Step 3: Write the fixtures doc**

The doc MUST contain, per fixture: the symbol's fully-qualified name, its `path:line`, the
difficulty tier, the hand-verified transitive caller set (each at `path:line`), and the
exact probe/`rg` queries used to derive it.

- [ ] **Step 4: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`.
Suggested message: `docs(codeintel): ground-truth blast-radius fixtures (holomush-i9599)`.

**Acceptance:** Three fixtures spanning direct-call / shared-type / interface-dispatch tiers,
each with a hand-verified caller set cited at `path:line` and reproducible queries.

**Depends on:** none.

---

### Task 2: Stand up CKB, index holomush, score against fixtures

**Files:**

- Modify: `docs/codeintel/2026-05-25-bakeoff-scorecard.md` (create on first write)

- [ ] **Step 1: Install the toolchain**

Run:

```bash
go install github.com/sourcegraph/scip-go@latest
npm install -g @tastehub/ckb
command -v scip-go && command -v ckb && ckb --version
```

Expected: both `command -v` lines print a path and `ckb --version` prints a version. If
`scip-go` does not resolve on `PATH` (e.g. `$(go env GOPATH)/bin` not exported), STOP — a
missing `scip-go` makes `ckb index` silently produce an empty SCIP index in Step 2.

- [ ] **Step 2: Index the holomush repo**

From the holomush repo root (`/Volumes/Code/github.com/holomush/holomush`):

```bash
ckb init
time ckb index
```

Expected: `.ckb/` is created containing `config.json`, `ckb.db`, and `index.scip`.
Record the wall-clock cold-index time and the on-disk size of `.ckb/` in the scorecard.

- [ ] **Step 3: Query each fixture symbol and capture results**

For each of the three Task-1 fixtures:

```bash
ckb impact "<symbolId>" --depth=3
```

(Resolve `<symbolId>` via `ckb` symbol search first; record the resolution step.)
Capture each tool result alongside the Task-1 ground truth.

- [ ] **Step 4: Score accuracy per tier**

For each fixture, compute precision and recall of the tool's transitive callers vs the
hand-verified set. Record the three tiers **separately** — the interface-dispatch tier is
the discriminator. Note any garbage/hallucinated callers explicitly.

- [ ] **Step 5: Record footprint + the semantic-recall control**

Record idle RSS of the CKB process and disk use. Run one "how does Y work" query and confirm
`probe` remains the better semantic-search tool (acceptance #6) — record the comparison.

- [ ] **Step 6: Commit**

Commit the scorecard. Suggested message:
`docs(codeintel): CKB bake-off results vs ground-truth fixtures (holomush-i9599)`.

**Acceptance:** Scorecard records CKB's per-tier precision/recall, cold-index time, query
latency, RSS, disk, and the probe semantic-recall comparison.

**Depends on:** Task 1.

---

### Task 3: Verify index-keying granularity (REQ-1 / REQ-2)

**Files:**

- Modify: `docs/codeintel/2026-05-25-bakeoff-scorecard.md`

- [ ] **Step 1: Stand up CKB in remote index-server mode**

Write a minimal index-server config and start the server:

```toml
# /tmp/ckb-index-config.toml
[index_server]
enabled = true
max_page_size = 10000

[[repos]]
id = "holomush/holomush"
name = "HoloMUSH"
path = "/Volumes/Code/github.com/holomush/holomush"

[default_privacy]
expose_paths = true
expose_docs = true
expose_signatures = true
```

```bash
export CKB_AUTH_TOKEN="$(openssl rand -hex 16)"   # --index-server locks mutating endpoints without a token
ckb serve --port 8080 --index-server --index-config /tmp/ckb-index-config.toml
```

Expected: server listens on `:8080`; the repo registers under id `holomush/holomush`.
(Read/query endpoints answer for the keying measurement even if the token is unset, but
`--index-server` logs a warning and locks mutating endpoints without one.)

- [ ] **Step 2: Query the same index from a second jj workspace WITHOUT re-indexing**

From a second workspace (`task workspace:new -- ci-keying-probe`), point a client at the
running server (HTTP) and run the same fixture impact query. Confirm it answers from the
shared server with **no second `ckb index`** in the new workspace.

- [ ] **Step 3: Record the keying verdict**

Document whether CKB keys on repo-identity (`id`) vs filesystem path, whether a second
checkout/workspace reuses the one index (REQ-2 PASS/FAIL), and how SHA is represented (one
index per registered path at a time, or multiple SHAs). This is the empirical answer the
desk research could not give.

- [ ] **Step 4: Commit**

Commit the keying-verdict section. Suggested message:
`docs(codeintel): CKB index-keying verdict (REQ-1/REQ-2) (holomush-i9599)`.

**Acceptance:** Scorecard records a PASS/FAIL on REQ-2 (second workspace reuses one index,
no re-index) and documents the keying model (identity vs path, SHA handling).

**Depends on:** Task 2.

---

### Task 4: Decision gate + gap doc (conditional Joern fallback)

**Files:**

- Modify: `docs/codeintel/2026-05-25-bakeoff-scorecard.md`

- [ ] **Step 1: Apply the accuracy gate**

Read Task 2's per-tier scores. If CKB returns accurate, non-garbage transitive callers on
the **shared-type AND interface-dispatch** fixtures (manually verified), record `winner=CKB`
and skip to Step 3. If CKB fails the interface-dispatch tier, proceed to Step 2.

- [ ] **Step 2: (Conditional) Bake off Joern**

Only if CKB failed the gate: stand up Joern + ArangoDB (per the candidate's docs), index
holomush with `gosrc2cpg`, and re-run Tasks 2–3's fixture queries and keying check against
Joern. Record Joern's per-tier scores and ops cost (ArangoDB + JVM) in the scorecard. Pick
the better of {CKB, Joern} on the interface-dispatch tier; record the winner.

- [ ] **Step 3: Write the gap doc (acceptance #5)**

From the measured results, record which couplings the winning static graph misses —
event-bus emit/subscribe, plugin gRPC boundary, Lua dispatch, interface dispatch — citing
the specific fixture queries that under-reported. State how the manifest requires/provides
DAG (Task 6), the subject-owner map, and OTel telemetry would fill each gap.

- [ ] **Step 4: Commit**

Commit the decision + gap doc. Suggested message:
`docs(codeintel): bake-off winner + static-graph gap doc (holomush-i9599)`.

**Acceptance:** Scorecard names a winner justified by interface-dispatch accuracy, and the
gap doc enumerates seam-misses with the fill that addresses each (REQ-5).

**Depends on:** Task 2, Task 3.

---

## Phase 1: holomush-side machinery (blocked on Phase 0 winner)

### Task 5: Index-generation task + CI step (scip-go @ SHA → artifact)

**Files:**

- Modify: `Taskfile.yaml`
- Modify or create: a CI workflow under `.github/workflows/`

- [ ] **Step 1: Add a `codeintel:index` Taskfile task**

Add a task that runs `scip-go` + `ckb index` against the current checkout and writes the
index bundle (`.ckb/index.scip` + the Task-6 fill graph) to a known output path, stamped
with the current SHA. Mirror existing Taskfile task style (see neighboring tasks for the
SPDX header and `desc:` convention).

- [ ] **Step 2: Wire a CI step that produces the artifact on `main`**

Add a workflow (or a job in an existing one) that runs `task codeintel:index` on push to
`main` and publishes the resulting bundle as a build artifact tagged by SHA. Do NOT push to
the cluster — publishing the artifact is the boundary (the homelab repo pulls it).

> **Workflow-edit note:** editing a `.github/workflows/*.yml` triggers the security
> first-touch reminder hook; the verbatim retry applies — do not rewrite to appease it.

- [ ] **Step 3: Verify locally**

Run: `task codeintel:index`
Expected: an index bundle is produced at the output path, named/tagged with the SHA.

- [ ] **Step 4: Commit**

Suggested message: `ci(codeintel): generate SCIP index + fill artifact per SHA (holomush-i9599)`.

**Acceptance:** `task codeintel:index` produces a SHA-tagged bundle locally; CI publishes it
as an artifact on `main`. No cluster interaction.

**Depends on:** Task 4.

---

### Task 6: Manifest-DAG PoC fill extractor (cross-boundary edges)

This is the one durable Go deliverable. It reads plugin manifests and emits the
requires↔provides edges the static call graph cannot see (REQ-8). Tool-independent; may
start alongside Phase 0.

**Files:**

- Create: `internal/codeintel/manifestfill/fill.go`
- Create: `internal/codeintel/manifestfill/fill_test.go`
- Create: `cmd/codeintel-fill/main.go`

- [ ] **Step 1: Write the failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package manifestfill_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/codeintel/manifestfill"
	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestBuildEmitsProvidesToRequiresEdge(t *testing.T) {
	manifests := []*plugins.Manifest{
		{Name: "provider", Type: plugins.TypeBinary, Provides: []string{"holomush.world.v1.WorldService"}},
		{Name: "consumer", Type: plugins.TypeLua, Requires: []string{"holomush.world.v1.WorldService"}},
	}

	g := manifestfill.Build(manifests)

	require.Len(t, g.Edges, 1, "exactly one requires→provides edge expected")
	e := g.Edges[0]
	assert.Equal(t, "consumer", e.FromPlugin, "consumer depends on the provider")
	assert.Equal(t, "provider", e.ToPlugin)
	assert.Equal(t, "holomush.world.v1.WorldService", e.ViaService)
}

func TestBuildIgnoresUnsatisfiedRequires(t *testing.T) {
	manifests := []*plugins.Manifest{
		{Name: "consumer", Type: plugins.TypeLua, Requires: []string{"svc-nobody-provides"}},
	}

	g := manifestfill.Build(manifests)

	assert.Empty(t, g.Edges, "a requires with no provider yields no edge (host-satisfied or missing)")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./internal/codeintel/manifestfill/`
Expected: FAIL — package `manifestfill` does not exist yet.

- [ ] **Step 3: Write the minimal implementation**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package manifestfill builds the cross-boundary "fill" graph that a static
// Go call graph cannot see: plugin service-dependency edges derived from the
// manifest requires/provides declarations. Changing a symbol in a plugin that
// PROVIDES service S impacts every plugin that REQUIRES S, even with zero
// direct Go call between them.
package manifestfill

import plugins "github.com/holomush/holomush/internal/plugin"

// Edge is a directed cross-boundary dependency: FromPlugin depends on
// ToPlugin because FromPlugin requires a service ToPlugin provides.
type Edge struct {
	FromPlugin string `json:"from_plugin"`
	ToPlugin   string `json:"to_plugin"`
	ViaService string `json:"via_service"`
}

// FillGraph is the set of cross-boundary edges for a plugin set.
type FillGraph struct {
	Edges []Edge `json:"edges"`
}

// Build computes the requires→provides fill graph for the given manifests.
// A requires entry with no in-set provider yields no edge (it is satisfied by
// the host or genuinely missing — neither is a plugin-to-plugin edge).
func Build(manifests []*plugins.Manifest) *FillGraph {
	providers := make(map[string]string) // service -> providing plugin name
	for _, m := range manifests {
		for _, svc := range m.Provides {
			providers[svc] = m.Name
		}
	}

	g := &FillGraph{}
	for _, m := range manifests {
		for _, svc := range m.Requires {
			if provider, ok := providers[svc]; ok && provider != m.Name {
				g.Edges = append(g.Edges, Edge{
					FromPlugin: m.Name,
					ToPlugin:   provider,
					ViaService: svc,
				})
			}
		}
	}
	return g
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- ./internal/codeintel/manifestfill/`
Expected: PASS (both tests).

- [ ] **Step 5: Add a directory-scanning constructor + its test**

Add to `fill.go`:

```go
import (
	"os"
	"path/filepath"

	"github.com/samber/oops"
)

// BuildFromDir scans a plugins directory (each subdir holding a plugin.yaml),
// parses every manifest, and returns the fill graph. Parse failures are
// returned wrapped — a malformed manifest must not silently drop edges.
func BuildFromDir(pluginsDir string) (*FillGraph, error) {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil, oops.In("manifestfill").With("dir", pluginsDir).Wrap(err)
	}

	var manifests []*plugins.Manifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(pluginsDir, entry.Name(), "plugin.yaml")
		data, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			continue // not a plugin dir
		}
		if readErr != nil {
			return nil, oops.In("manifestfill").With("path", path).Wrap(readErr)
		}
		m, parseErr := plugins.ParseManifest(data)
		if parseErr != nil {
			return nil, oops.In("manifestfill").With("path", path).Wrap(parseErr)
		}
		manifests = append(manifests, m)
	}
	return Build(manifests), nil
}
```

Add to `fill_test.go`:

```go
import (
	"os"
	"path/filepath"
)

func TestBuildFromDirParsesPluginYAMLs(t *testing.T) {
	dir := t.TempDir()
	writeManifest := func(name, body string) {
		sub := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(sub, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(sub, "plugin.yaml"), []byte(body), 0o644))
	}
	writeManifest("provider", "name: provider\nversion: 1.0.0\ntype: binary\nbinary-plugin:\n  executable: p\nprovides:\n  - holomush.world.v1.WorldService\n")
	writeManifest("consumer", "name: consumer\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua\nrequires:\n  - holomush.world.v1.WorldService\n")

	g, err := manifestfill.BuildFromDir(dir)
	require.NoError(t, err)
	require.Len(t, g.Edges, 1)
	assert.Equal(t, "consumer", g.Edges[0].FromPlugin)
	assert.Equal(t, "provider", g.Edges[0].ToPlugin)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `task test -- ./internal/codeintel/manifestfill/`
Expected: PASS (all three tests).

- [ ] **Step 7: Write the CLI entrypoint**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Command codeintel-fill scans a plugins directory and prints the
// cross-boundary manifest fill graph (requires→provides edges) as JSON.
// These edges augment a static code-intelligence tool's blast-radius output
// with couplings the Go call graph cannot see (holomush-i9599, REQ-8).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/holomush/holomush/internal/codeintel/manifestfill"
)

func main() {
	dir := flag.String("plugins-dir", "plugins", "path to the plugins directory")
	flag.Parse()

	g, err := manifestfill.BuildFromDir(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "codeintel-fill:", err)
		os.Exit(1)
	}
	out, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "codeintel-fill:", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}
```

- [ ] **Step 8: Run against the real plugins dir and demonstrate a cross-boundary edge (REQ-8)**

Run: `go run ./cmd/codeintel-fill --plugins-dir plugins`
(This direct `go run` is a one-off PoC demonstration; Task 5's `codeintel:index` Taskfile
task wraps `codeintel-fill` for the durable invocation, per the repo's `task`-runner rule.)
Expected: JSON listing at least one edge `{from_plugin, to_plugin, via_service}` between two
real in-tree plugins where no direct Go call exists. Record this edge in the gap doc as the
PoC demonstration. (If the in-tree plugin set has no requires/provides pair yet, capture the
edge from a fixture manifest pair and note that in the gap doc.)

- [ ] **Step 9: Lint + commit**

Run: `task lint:go`
Then commit. Suggested message:
`feat(codeintel): manifest requires/provides fill-graph extractor (holomush-i9599)`.

**Acceptance:** `manifestfill.Build` / `BuildFromDir` are tested green; `codeintel-fill`
emits the fill graph; at least one cross-boundary edge with no direct Go call is demonstrated
(REQ-8).

**Depends on:** Task 1 (tool-independent; may run alongside Phase 0).

---

### Task 7: Compose the fill into impact output + query-client config

**Files:**

- Create: a small composer (location depends on the Phase-0 winner's output format — for CKB,
  a post-processor that merges `manifestfill.FillGraph` edges into `ckb impact` JSON). Place
  under `internal/codeintel/` (e.g. `internal/codeintel/compose/`).
- Create: an example client config (`.mcp.json` fragment) pointing at the tunnel URL placeholder.

- [ ] **Step 1: Write the failing test for the composer**

Test that, given a tool impact result missing a cross-boundary dependent and a `FillGraph`
edge naming that dependent, the composed result includes the dependent flagged as
`source: "manifest-fill"`. (Write the exact struct/JSON shape against the winner's real
`ckb impact` output captured in Task 2.)

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- ./internal/codeintel/compose/`
Expected: FAIL — composer not implemented.

- [ ] **Step 3: Implement the composer**

Merge `FillGraph` edges into the tool's impact result, tagging fill-sourced dependents so a
reader can distinguish static-graph hits from manifest-derived ones. (Full code written
against the captured Task-2 output shape.)

- [ ] **Step 4: Run to verify it passes**

Run: `task test -- ./internal/codeintel/compose/`
Expected: PASS.

- [ ] **Step 5: Write the query-client config**

Add an example `.mcp.json` fragment that points a Claude Code session at the cluster MCP over
HTTP/SSE (tunnel hostname as a documented placeholder, filled by the homelab side). Document
that this is additive to `probe` (acceptance #6), not a replacement.

- [ ] **Step 6: Commit**

Suggested message: `feat(codeintel): compose manifest fill into impact output + client config (holomush-i9599)`.

**Acceptance:** Composer test is green and tags fill-sourced dependents; an example client
config points at the tunnel URL placeholder and documents the additive-to-probe stance.

**Depends on:** Task 4 (winner output shape), Task 6 (the fill graph).

---

### Task 8: Deployment handoff contract (terminal deliverable)

**Files:**

- Create: `docs/codeintel/2026-05-25-deployment-handoff-contract.md`

- [ ] **Step 1: Write the contract**

Document, for the homelab repo to consume:

- **Tool + version** — the Phase-0 winner's image/binary + pinned version.
- **Index-pull requirement** — the cluster MUST pull the index bundle by SHA. State the
  options (OCI artifact in the existing registry — recommended; GH release asset; Postgres
  large-object) and leave the mechanism choice to the homelab owner.
- **Artifact schema** — the exact contents/layout of the bundle Task 5 publishes: the SCIP
  index, the serialized `manifestfill.FillGraph`, and a SHA stamp. **Locked here.**
- **Postgres DSN** — for index metadata / optional coverage-telemetry persistence.
- **Ingress route** — the CF-tunnel hostname the server is served on.
- **Re-index trigger** — main-push CI publish → cluster pull.
- **Resource requests** — RAM/CPU/disk envelope from the Task-2 footprint measurement.

- [ ] **Step 2: File the homelab tracking bead**

Run `bd create` for a follow-on bead describing the cluster standup (the homelab repo's
work), referencing this contract path. Note its ID in the contract.

- [ ] **Step 3: Self-check the contract against the spec**

Confirm every contract field maps to a spec requirement (REQ-1 keying, REQ-3 remote MCP,
REQ-7 boundary). Fix gaps inline.

- [ ] **Step 4: Commit**

Suggested message: `docs(codeintel): deployment handoff contract for homelab (holomush-i9599)`.

**Acceptance:** A complete contract the homelab repo can act on without further design from
this side; artifact schema locked; homelab tracking bead filed and referenced.

**Depends on:** Task 5 (artifact), Task 7 (client integration + composer).

---

## Notes for the implementer

- **Spike honesty:** Phase 0 tasks are measurement tasks, not TDD code. Record real numbers;
  if CKB fails the interface-dispatch gate, the conditional Joern bake-off (Task 4 Step 2)
  is real work, not a formality.
- **Boundary:** never write cluster manifests, CF-tunnel routes, or GitOps wiring here
  (REQ-7). The deliverable that crosses the boundary is the contract + the published artifact.
- **probe stays primary** for "where is X / how does Y work" (REQ-6). This tooling is additive.
- **Search ladder:** use `mcp__probe__search_code` / `extract_code` for Go symbols, `rg` for
  text (never bare `grep`), per `.claude/rules/search-tools.md`.
