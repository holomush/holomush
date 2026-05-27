<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Test-Tier Taxonomy & Test-Only-Construct Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make test-only constructs (the in-memory event store, the embedded NATS harness) compile/lint-provably absent from production code, delete the fragile `test:int` package list, and reconcile the test-tier taxonomy/terminology across the docs.

**Architecture:** Extract `MemoryEventStore` from `package core` into a new test-only package `internal/core/coretest` and drop its `//go:build !integration` tag (the lone tag forcing package enumeration). Enforce "no production import of test-only constructs" via a new **depguard** rule (package-granular, which is why extraction is required). With the tag gone, `test:int` runs `./...` and the explicit list is deleted. A meta-test guards the depguard rule against silent deletion. Docs adopt the four-tier integration model with "E2E" reserved for Playwright.

**Tech Stack:** Go 1.x, `golangci-lint` v2 (depguard linter), Taskfile, `gotestsum`.

**Spec:** `docs/superpowers/specs/2026-05-25-test-tier-taxonomy-design.md`
**Design bead:** holomush-1eps2 (absorbs holomush-bmtd)

---

## File Structure

| File | Responsibility | Action |
| --- | --- | --- |
| `internal/core/coretest/store_memory.go` | The in-memory event store, test-only, no build tag | Create (moved from `internal/core/store_memory.go`) |
| `internal/core/store_memory.go` | (removed) | Delete |
| `internal/core/engine_test.go`, `engine_end_session_test.go`, `engine_end_session_ctx_test.go` | white-box core tests | Modify (import + qualify `coretest.NewMemoryEventStore`) |
| `internal/auth/auth_service_test.go`, `internal/command/handlers/shutdown_test.go`, `internal/command/types_test.go`, `internal/grpc/auth_handlers_test.go`, `internal/grpc/dispatcher_test.go`, `internal/grpc/pipeline_rendering_test.go` | consumers of the store | Modify (qualify import) |
| `internal/core/engine.go:42`, `internal/plugin/host.go:115` | stale comments referencing `MemoryEventStore` | Modify (comment text) |
| `.golangci.yaml` | enable + configure depguard | Modify |
| `Taskfile.yaml` (`test:int`) | drop the explicit package list | Modify |
| `test/meta/depguard_config_test.go` | meta-tests: assert depguard deny rules present (INV-1/2) + test:int has no package list (INV-3) | Create (in existing `package meta`) |
| `.claude/rules/testing.md`, `CLAUDE.md`, `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`, `site/docs/contributing/integration-tests.md` | tier taxonomy + terminology | Modify |

---

### Task 1: Extract `MemoryEventStore` into `internal/core/coretest`

**Files:**

- Create: `internal/core/coretest/store_memory.go`
- Delete: `internal/core/store_memory.go`

- [ ] **Step 1: Create the new package file**

Create `internal/core/coretest/store_memory.go` with the store moved out of
`package core`, the `//go:build !integration` tag dropped, and the three
package-internal references (`Event`, `EventAppender`, `ErrStreamEmpty`)
qualified with `core.`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package coretest provides test-only implementations of internal/core
// interfaces. Production code MUST NOT import this package; the prohibition
// is enforced by the depguard rule in .golangci.yaml (see holomush-1eps2).
package coretest

import (
	"context"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/core"
)

// MemoryEventStore is an in-memory event store for unit testing.
// It implements core.EventAppender and provides Replay/ReplayTail/LastEventID
// as test-inspection helpers.
type MemoryEventStore struct {
	mu      sync.RWMutex
	streams map[string][]core.Event
}

// NewMemoryEventStore creates a new in-memory event store.
func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{
		streams: make(map[string][]core.Event),
	}
}

// Compile-time check: MemoryEventStore satisfies core.EventAppender.
var _ core.EventAppender = (*MemoryEventStore)(nil)

// Append persists an event to the in-memory store.
func (s *MemoryEventStore) Append(_ context.Context, event core.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streams[event.Stream] = append(s.streams[event.Stream], event)
	return nil
}

// Replay returns events from a stream starting after the given ID.
// Test-inspection helper; not part of any production interface.
func (s *MemoryEventStore) Replay(_ context.Context, stream string, afterID ulid.ULID, limit int) ([]core.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.streams[stream]
	if len(events) == 0 {
		return nil, nil
	}

	startIdx := 0
	if afterID.Compare(ulid.ULID{}) != 0 {
		found := false
		for i, e := range events {
			if e.ID == afterID {
				startIdx = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, nil
		}
	}

	endIdx := min(startIdx+limit, len(events))

	result := make([]core.Event, endIdx-startIdx)
	copy(result, events[startIdx:endIdx])
	return result, nil
}

// LastEventID returns the most recent event ID for a stream.
// Test-inspection helper; not part of any production interface.
func (s *MemoryEventStore) LastEventID(_ context.Context, stream string) (ulid.ULID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.streams[stream]
	if len(events) == 0 {
		return ulid.ULID{}, core.ErrStreamEmpty
	}
	return events[len(events)-1].ID, nil
}

// maxReplayTailCount is the server-side cap for ReplayTail count parameter.
const maxReplayTailCount = 501

// ReplayTail returns the most recent count events on stream, ascending by ID.
// Events with timestamps before notBefore are excluded. If beforeID is non-zero,
// events with ID >= beforeID are excluded. Count is capped at maxReplayTailCount.
// Test-inspection helper; not part of any production interface.
func (s *MemoryEventStore) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]core.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if count > maxReplayTailCount {
		count = maxReplayTailCount
	}
	if count <= 0 {
		return nil, nil
	}

	events := s.streams[stream]
	if len(events) == 0 {
		return nil, nil
	}

	var eligible []core.Event
	for i := len(events) - 1; i >= 0 && len(eligible) < count; i-- {
		e := events[i]
		if !beforeID.IsZero() && e.ID.Compare(beforeID) >= 0 {
			continue
		}
		if !notBefore.IsZero() && e.Timestamp.Before(notBefore) {
			continue
		}
		eligible = append(eligible, e)
	}

	for i, j := 0, len(eligible)-1; i < j; i, j = i+1, j-1 {
		eligible[i], eligible[j] = eligible[j], eligible[i]
	}
	return eligible, nil
}
```

- [ ] **Step 2: Delete the old file**

Run: `rm internal/core/store_memory.go`

- [ ] **Step 3: Verify the new package compiles**

Run: `task build`
Expected: PASS (the new package compiles; call sites are still broken — fixed in Task 2, so `task test` will fail until then, but `task build` compiles non-test code).

- [ ] **Step 4: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`.
Message: `refactor(core): extract MemoryEventStore to internal/core/coretest (holomush-1eps2)`

---

### Task 2: Update the 9 test call sites to use `coretest`

**Files:**

- Modify: `internal/core/engine_test.go`, `internal/core/engine_end_session_test.go`, `internal/core/engine_end_session_ctx_test.go` (package `core`, unqualified calls)
- Modify: `internal/auth/auth_service_test.go`, `internal/command/handlers/shutdown_test.go`, `internal/command/types_test.go`, `internal/grpc/auth_handlers_test.go`, `internal/grpc/dispatcher_test.go`, `internal/grpc/pipeline_rendering_test.go`

- [ ] **Step 1: Find every call site**

Run: `rg -n 'NewMemoryEventStore|core\.MemoryEventStore|MemoryEventStore' --type go -g '*_test.go'`
Expected: matches in the 9 files above. Note for each whether the call is
`NewMemoryEventStore(` (unqualified — the `package core` files) or
`core.NewMemoryEventStore(` (qualified — the others).

- [ ] **Step 2: Add the import and re-qualify in each file**

In every listed test file, add to the import block:

```go
"github.com/holomush/holomush/internal/core/coretest"
```

Then replace references:

- In the `package core` files (`engine_test.go`, `engine_end_session_test.go`, `engine_end_session_ctx_test.go`): `NewMemoryEventStore(` → `coretest.NewMemoryEventStore(`, and any `*MemoryEventStore` type reference → `*coretest.MemoryEventStore`.
- In the other six files: `core.NewMemoryEventStore(` → `coretest.NewMemoryEventStore(`, and `*core.MemoryEventStore` → `*coretest.MemoryEventStore`.

- [ ] **Step 3: Run goimports/format**

Run: `task fmt`
Expected: import blocks reordered; no errors.

- [ ] **Step 4: Run the affected unit tests**

Run: `task test -- ./internal/core/ ./internal/auth/ ./internal/command/... ./internal/grpc/`
Expected: PASS.

- [ ] **Step 5: Commit**

Message: `refactor(tests): point MemoryEventStore call sites at coretest (holomush-1eps2)`

---

### Task 3: Fix the two stale production comments

**Files:**

- Modify: `internal/core/engine.go:42`
- Modify: `internal/plugin/host.go:115`

- [ ] **Step 1: Update engine.go comment**

Read `internal/core/engine.go:40-44`. The comment references
`(*MemoryEventStore)(nil)` as an example of a typed-nil interface value. Update
the example to `(*coretest.MemoryEventStore)(nil)` (or drop the parenthetical
example if `coretest` would read oddly in a production-file comment — prefer a
generic phrasing like "a typed-nil concrete pointer").

- [ ] **Step 2: Update host.go comment**

Read `internal/plugin/host.go:113-117`. The comment says the interface is
"Satisfied by MemoryEventStore in ...". Update to "Satisfied by
`coretest.MemoryEventStore` in tests" so it no longer implies a production
dependency on a moved symbol.

- [ ] **Step 3: Verify build**

Run: `task build`
Expected: PASS (comments only).

- [ ] **Step 4: Commit**

Message: `docs(comments): update MemoryEventStore references after coretest move (holomush-1eps2)`

---

### Task 4: Delete the `test:int` package list

**Files:**

- Modify: `Taskfile.yaml` (the `test:int` target, currently lines ~152-171)

- [ ] **Step 1: Rewrite the recipe**

Read the current `test:int` target. Replace the explicit package-list `cmds`
entry with `./...` honoring `CLI_ARGS`, and replace the load-bearing comment
(which explained the enumeration) with a note that the enumeration is no longer
needed:

```yaml
  test:int:
    desc: Run integration tests (needs Docker for testcontainers)
    cmds:
      - task: plugin:build-all
      # No package enumeration: the only //go:build !integration helper
      # (the in-memory event store) was extracted to internal/core/coretest
      # (holomush-1eps2), so ./... compiles cleanly under -tags=integration.
      # -coverpkg=./... makes integration tests contribute coverage to every
      # production package they exercise (see .codecov.yml).
      - "{{.GO_TOOL}} gotestsum --format pkgname -- -race -tags=integration -count=1 -coverprofile=coverage-int.out -covermode=atomic -coverpkg=./... {{.CLI_ARGS | default \"./...\"}}"
```

- [ ] **Step 2: Verify `./...` compiles and runs under the integration tag**

Run: `task test:int`
Expected: PASS — every integration-tagged package compiles and runs; no
"undefined: NewMemoryEventStore" errors. (Docker must be running.)

- [ ] **Step 3: Verify CLI_ARGS passthrough (the bmtd ergonomics fix)**

Run: `task test:int -- -run TestSomething ./test/integration/privacy/`
Expected: runs only the named package/test (no longer silently runs the full
default list).

- [ ] **Step 4: Commit**

Message: `build(taskfile): drop test:int package list; ./... works post-coretest (holomush-1eps2, absorbs holomush-bmtd)`

---

### Task 5: Enable and configure the depguard rule

**Files:**

- Modify: `.golangci.yaml` (add `depguard` to `linters.enable`; add `linters.settings.depguard`)

- [ ] **Step 1: Add depguard to the enable list**

In `.golangci.yaml`, under `linters.enable`, add (in the "Maintenance" group):

```yaml
    - depguard
```

- [ ] **Step 2: Add the depguard settings block**

Under `linters.settings:` add a rule that denies the test-only packages,
scoped to production files only (test files and the test-support harness
packages are exempt). Use depguard's `$test` file keyword plus directory
negations:

```yaml
    depguard:
      rules:
        no-test-only-constructs-in-production:
          # Apply only to production files: not *_test.go, and not the
          # test-support packages that legitimately wire harnesses in
          # non-_test.go files.
          files:
            - "!$test"
            - "!**/internal/testsupport/**"
            - "!**/internal/cluster/clustertest/**"
          deny:
            - pkg: github.com/holomush/holomush/internal/eventbus/eventbustest
              desc: "embedded NATS test harness; production code MUST NOT import it (holomush-1eps2)"
            - pkg: github.com/holomush/holomush/internal/core/coretest
              desc: "in-memory test event store; production code MUST NOT import it (holomush-1eps2)"
```

- [ ] **Step 3: Verify clean tree passes**

Run: `task lint`
Expected: PASS (no production package imports either denied package today).

- [ ] **Step 4: Verify the rule actually catches a violation**

Temporarily add `import _ "github.com/holomush/holomush/internal/core/coretest"`
to a production file (e.g. top of `internal/core/engine.go`).
Run: `task lint`
Expected: FAIL with a depguard error naming `coretest` and the `desc` text.
Then **remove** the temporary import and re-run `task lint` → PASS.

(If Step 4 does not fail as expected, the `files` negation semantics differ in
the pinned golangci-lint version — check the version in `go.tool.mod` and the
depguard docs for that version, and adjust the `files`/`$test` patterns until a
production-file violation is caught while `_test.go` and `testsupport/` imports
remain allowed.)

- [ ] **Step 5: Commit**

Message: `build(lint): enable depguard to keep test-only constructs out of production (holomush-1eps2)`

---

### Task 6: Add meta-tests guarding the invariants

**Files:**

- Create: `test/meta/depguard_config_test.go` (new file in the existing `package meta` — reuses that package's `findRepoRoot(t)` helper at `test/meta/inv_binding_test.go`)

- [ ] **Step 1: Write the meta-tests**

Create `test/meta/depguard_config_test.go`. It reuses the existing
`findRepoRoot(t)` helper (same package), reads the repo-root config files, and
asserts (a) both depguard deny packages are present (INV-1/INV-2) and (b) the
`test:int` recipe honors `CLI_ARGS` and enumerates no packages (INV-3) — so
neither guarantee can be silently deleted:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDepguardTestOnlyConstructRulesPresent guards INV-1/INV-2 against silent
// deletion — the exact failure mode (a config claim silently diverging from
// reality) this work was created to correct (holomush-1eps2).
func TestDepguardTestOnlyConstructRulesPresent(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".golangci.yaml"))
	require.NoError(t, err, "read .golangci.yaml")
	cfg := string(data)

	for _, pkg := range []string{
		"github.com/holomush/holomush/internal/eventbus/eventbustest",
		"github.com/holomush/holomush/internal/core/coretest",
	} {
		require.Contains(t, cfg, pkg,
			"depguard deny rule for %q missing from .golangci.yaml (holomush-1eps2 INV-1/INV-2)", pkg)
	}
}

// TestTaskfileIntHasNoPackageList guards INV-3: the test:int recipe must run
// ./... (honoring CLI_ARGS) and must NOT re-introduce an enumerated package
// list (holomush-1eps2, absorbs holomush-bmtd).
func TestTaskfileIntHasNoPackageList(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yaml"))
	require.NoError(t, err, "read Taskfile.yaml")
	tf := string(data)

	// Isolate the test:int recipe block: from its key line to the next
	// 2-space-indented task key.
	loc := regexp.MustCompile(`(?m)^  test:int:[ \t]*$`).FindStringIndex(tf)
	require.NotNil(t, loc, "test:int target not found in Taskfile.yaml")
	after := tf[loc[1]:]
	block := after
	if next := regexp.MustCompile(`(?m)^  \S`).FindStringIndex(after); next != nil {
		block = after[:next[0]]
	}

	require.Contains(t, block, "CLI_ARGS",
		"test:int must honor CLI_ARGS (holomush-1eps2 INV-3 / bmtd)")
	require.NotContains(t, block, "./internal/",
		"test:int must not enumerate packages; use ./... (holomush-1eps2 INV-3)")
}
```

- [ ] **Step 2: Run them to verify they pass**

Run: `task test -- ./test/meta/`
Expected: PASS (Task 4 removed the package list; Task 5 added both deny entries).

- [ ] **Step 3: Verify each fails when its guarantee regresses**

Temporarily delete the `coretest` deny line from `.golangci.yaml` →
`task test -- ./test/meta/` FAILs naming `coretest`; restore it.
Temporarily re-add `./internal/store/` to the `test:int` recipe →
`task test -- ./test/meta/` FAILs on the package-list assertion; restore it.

- [ ] **Step 4: Commit**

Message: `test(meta): guard depguard deny rules + test:int package-list erasure (holomush-1eps2)`

---

### Task 7: Reconcile the test-tier taxonomy across docs

**Files:**

- Modify: `.claude/rules/testing.md` (the "EventBus Test Harness" section, ~lines 139-147)
- Modify: `CLAUDE.md` (the Testing always-on table — the "Ginkgo/Gomega for E2E" + "MUST NOT use eventbustest in E2E" rows)
- Modify: `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` (§8 testing-strategy tier table)
- Modify: `site/docs/contributing/integration-tests.md`

- [ ] **Step 1: Rewrite the testing.md tier table**

Replace the "EventBus Test Harness" section with the canonical taxonomy (spec
§4) and the depguard invariant (spec §5). Use this table:

```markdown
## Test Tiers

| Tier | Dependencies | Runner | Build tag |
| --- | --- | --- | --- |
| unit | none | `task test` | (none) |
| bus-integration | embedded NATS (`eventbustest`) | `task test:int` | `//go:build integration` |
| audit-integration | embedded NATS + Postgres testcontainer | `task test:int` | `//go:build integration` |
| full-stack integration | embedded NATS + Postgres + `CoreServer` (+ optional in-tree plugins) | `task test:int` | `//go:build integration` |
| **E2E** | full Docker stack driven through a real client (browser) | `task test:e2e` | (Playwright) |

"E2E" means the Playwright browser suite — a test that crosses the real user
boundary. The Ginkgo `test:int` suite is **integration** (it calls Go/gRPC APIs
in-process), regardless of how much of the stack it stands up.

`eventbustest` provides the in-process embedded NATS server (`MemoryStorage`)
used at every non-unit tier. This matches production, which also runs embedded
NATS (`internal/eventbus/subsystem.go`); external/clustered NATS is unimplemented
(tracked in holomush-s5ts). Production code MUST NOT import `eventbustest` or
`internal/core/coretest` — enforced by the depguard rule in `.golangci.yaml`.
```

- [ ] **Step 2: Fix the CLAUDE.md Testing rows**

In `CLAUDE.md`, change "MUST use Ginkgo/Gomega for E2E" → "MUST use Ginkgo/Gomega
for full-stack integration tests"; change "MUST NOT use `eventbustest` in E2E"
→ "Production code MUST NOT import `eventbustest`/`coretest` (depguard-enforced);
embedded NATS is correct at every test tier". Add a one-line pointer: "Canonical
tier taxonomy lives in `.claude/rules/testing.md`."

- [ ] **Step 3: Reconcile the jetstream spec tier name**

In `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` §8, rename
the "E2E" Go tier to "full-stack integration" (or add a one-line note: "renamed
to full-stack integration per holomush-1eps2; E2E now denotes the Playwright
suite").

- [ ] **Step 4: Update the contributor guide**

In `site/docs/contributing/integration-tests.md`, adopt the tier vocabulary and
note that full-stack integration may load in-tree plugins (capability;
whole-system suite is a follow-up). Reserve "E2E" for Playwright.

- [ ] **Step 5: INV-4 decision (documented convention, not a machine check)**

Per spec §8 INV-4 (deferred to this plan): implement INV-4 as a **documented
convention** — the tier table above is the single source of truth; do NOT add a
brittle grep-lint over prose. Add one sentence to `.claude/rules/testing.md`:
"Use 'E2E' only for the Playwright suite; Go Ginkgo suites are 'integration'."

- [ ] **Step 6: Verify docs lane**

Run: `task fmt && task pr-prep:docs`
Expected: PASS (`task lint:docs-symmetry` green; markdown formatted).

- [ ] **Step 7: Commit**

Message: `docs(testing): canonical test-tier taxonomy; reserve E2E for Playwright (holomush-1eps2)`

---

## Wrap-up (post-implementation, before close)

- [ ] Close holomush-bmtd: `bd close holomush-bmtd --reason "Absorbed into holomush-1eps2 (PR <n>): coretest extraction + depguard + package-list deletion + CLI_ARGS passthrough."`
- [ ] Confirm the whole-system-plugin follow-up and holomush-f5t07 remain open and linked.
- [ ] Run the full gate: `task pr-prep` green before push.

## Self-Review Coverage Map (spec → task)

| Spec requirement | Task |
| --- | --- |
| §4 canonical taxonomy + E2E=Playwright | Task 7 |
| §5 depguard invariant (INV-1/INV-2) | Task 5 |
| §5.1 extract store, drop tag | Task 1, 2, 3 |
| §5.1 delete package list + CLI_ARGS | Task 4 |
| §8 meta-test (INV-1/2 + INV-3 guards) | Task 6 |
| §8 INV-3 (`./...` no enumeration) | Task 4 (recipe) + Task 6 (guard) |
| §8 INV-4 (documented convention) | Task 7 Step 5 |
| §9 doc updates | Task 7 |
| §10 bmtd closed as absorbed | Wrap-up |
| §6 full-stack plugins / §7 follow-ups | Out of scope (deferred beads; not implemented here) |

<!-- adr-capture: sha256=b4978f2f3553a9c9; session=cli; ts=2026-05-25T16:11:18Z; adrs=holomush-qti5d,holomush-1r2hp,holomush-vjg7z -->
