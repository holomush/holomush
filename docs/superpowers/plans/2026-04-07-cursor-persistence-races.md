<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Cursor Persistence Races Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the three cursor persistence races in `holomush-43nd` (stale-cursor window, goroutine-reorder regression, lost writes on shutdown) by making cursor commits synchronous inside `replayAndSend` and adding a SQL-level monotonicity CAS to `PostgresSessionStore.UpdateCursors`. Close the orthogonal `BroadcastSystemMessage` ULID-generator hazard the CAS depends on, and enforce the ULID-generator distinction in CI via a new `gocritic` ruleguard rule.

**Architecture:** `replayAndSend` commits the cursor inline (with a 1-second bounded timeout on a fresh `context.Background()`) so the live loop's own gRPC handler goroutine is the only writer. `persistCursorAsync` is deleted. `UpdateCursors` in both `PostgresSessionStore` and `MemStore` gains a per-key monotonicity guard — Postgres uses a `WHERE event_cursors->>$key IS NULL OR ($-COLLATE "C") < $new::text COLLATE "C"` predicate; memstore compares `String()` values. `BroadcastSystemMessage` switches to `core.NewULID()`. A new `gorules/rules.go` file enables a `ruleguard` check in `gocritic` that fails the build on any `core.Event{}` literal using `idgen.New()` and on any `ulid.Make()` call — replacing the bash check in `Taskfile.yaml`.

**Tech Stack:** Go 1.x, `github.com/oklog/ulid/v2`, `github.com/samber/oops` for errors, `github.com/jackc/pgx/v5` for Postgres, `testcontainers-go` for integration tests, Ginkgo/Gomega for BDD specs, testify for unit tests, golangci-lint v2 with gocritic+ruleguard, jj (Jujutsu) VCS in a colocated git repo.

**Spec:** [docs/superpowers/specs/2026-04-07-cursor-persistence-races-design.md](../specs/2026-04-07-cursor-persistence-races-design.md)

**Bead:** holomush-43nd

---

## Context for the Implementing Engineer

You are working in a **jj workspace** under `.worktrees/holomush_holomush-43nd`, based on the `holomush-u37v` branch (PR #197) — not `main`. That PR adds the integration test file `test/integration/session/session_persistence_integration_test.go` that this work depends on; it is not yet merged. The jj skill in `fzymgc-house-skills/jj` governs all VCS operations here. **Use `jj`, not `git`, for all commit operations.** Every `jj` command in this plan uses `--no-pager`.

**Mandatory commands for this project:**

- `task test` — runs unit tests (fast, no Docker)
- `task test:int` — runs integration tests (requires Docker for testcontainers)
- `task test -- ./internal/store/... -run TestFoo` — targeted unit test
- `task test:int` — targeted integration test (runs all integration packages; use `-ginkgo.focus` passthrough only during iterative dev, not in committed scripts)
- `task lint` — runs all linters including the new ruleguard rule
- `task fmt` — formats Go/YAML/Markdown
- `task pr-prep` — the full CI mirror; **must be green before any push to the PR branch** (non-negotiable per project rules)

**Do NOT run `go test`, `go build`, `golangci-lint`, etc. directly.** Always go through `task`.

**Critical project rules** (from `CLAUDE.md` and `MEMORY.md`):

- Use `crypto/rand`, never `math/rand`
- Always use enum constants and named values — no magic literals
- Error wrapping uses `oops`; method accessor calls in `oops.With` must include `()`
- Markdown tables escape `|` as `\|`; run `task fmt` after markdown edits
- **Beads auto-flushes** — do NOT manually run `bd export`; trust `bd sync` and clean `git status`
- All source files need SPDX headers (auto-added by lefthook pre-commit hook)
- `task pr-prep` is the ONLY acceptable pre-push verification — never approximate with subset checks

**VCS operations in this workspace:**

```bash
# From within the workspace
cd /Users/sean/Code/github.com/holomush/.worktrees/holomush_holomush-43nd

# Check state
jj --no-pager st

# Commit current working copy and start a new empty change
jj --no-pager commit -m "feat(grpc): ..."

# View log
jj --no-pager log -r 'main..@' --limit 10
```

**Never use `git commit`, `git checkout`, `git branch`, `git rebase` — blocked by a PreToolUse guard.** You may use `gh` for GitHub API operations.

**Current workspace state when this plan starts:** the spec commit (`muo`) exists as the parent of `@`, containing only the design doc. All implementation tasks below produce new commits on top of `muo`.

---

## File Structure

### Files to CREATE

| Path | Purpose |
| --- | --- |
| `gorules/rules.go` | Build-tagged (`//go:build ruleguard`) file containing `EventIDMustBeMonotonic` and `ULIDMakeForbidden` ruleguard rules loaded by gocritic |

### Files to MODIFY

| Path | Change |
| --- | --- |
| `internal/grpc/server.go` | Delete `persistCursorAsync`; add `cursorCommitTimeout` const; replace async call in `replayAndSend` with sync commit |
| `internal/store/session_store.go` | Rewrite `UpdateCursors` to add per-key monotonicity CAS + multi-key refusal |
| `internal/store/session_store_test.go` | Update `TestPostgresSessionStore_UpdateCursors` to expect new SQL shape; add regression-reject + multi-key-reject test cases |
| `internal/store/session_store_integration_test.go` | Add "rejects monotonicity regression" spec under `Describe("UpdateCursors", ...)` |
| `internal/session/memstore.go` | Add per-key monotonicity guard to `MemStore.UpdateCursors` |
| `internal/session/memstore_test.go` | Add unit test for the new guard |
| `internal/command/types.go` | `BroadcastSystemMessage` event ID uses `core.NewULID()` instead of `idgen.New()` |
| `internal/command/types_test.go` | Add test asserting `BroadcastSystemMessage` produces monotonic event IDs across two calls |
| `internal/core/ulid.go` | Expand doc comment on `NewULID` per spec |
| `internal/core/ulid_test.go` | Add `TestNewULIDIsStrictlyMonotonicInTightLoop` |
| `internal/idgen/id.go` | Expand doc comment on `New` per spec |
| `test/integration/session/session_persistence_integration_test.go` | Add three new `It` specs covering Findings 1, 2 (E2E variant), 3 |
| `.golangci.yaml` | Enable `ruleguard` check under `gocritic.settings`; add rules path |
| `Taskfile.yaml` | Remove `lint:ulid-make` task and its entry in `lint:` aggregator |
| `CLAUDE.md` | Add "ULID Generation" subsection under "Random Number Generation" |
| `go.mod` / `go.sum` | `go mod tidy` will add `github.com/quasilyte/go-ruleguard/dsl` as a module requirement for the build-tagged rules file |

### Files to DELETE

None. All changes are additions, deletions inside existing files, or edits.

---

## Task Ordering Rationale

Tasks follow strict TDD with these principles:

1. **Failing test first.** Every implementation task is preceded by a test that fails against the current code.
2. **One finding's fix per commit.** Each commit lands one logical change that passes its own tests.
3. **Dependencies first.** The ULID-generator fixes (Tasks 1–3) must land before the CAS (Task 4) because the CAS's correctness depends on monotonic event IDs.
4. **Lint last.** The ruleguard rule (Task 10) lands after every source change is clean, so the rule doesn't flag intermediate states.

**Order:**

1. `core.NewULID` docs + monotonicity unit test (the invariant)
2. `idgen.New` docs (the boundary)
3. `BroadcastSystemMessage` fix + test (closes the latent Replay bug)
4. `PostgresSessionStore.UpdateCursors` CAS + tests (fixes Finding 2)
5. `MemStore.UpdateCursors` guard + tests (parity)
6. `replayAndSend` sync commit + delete `persistCursorAsync` (fixes Findings 1 and 3)
7. Integration tests in `session_persistence_integration_test.go` for all three findings
8. `CLAUDE.md` update
9. `gorules/rules.go` + `.golangci.yaml` + `Taskfile.yaml` cleanup
10. Full `task pr-prep` gate

---

### Task 1: Document `core.NewULID` as the monotonic event-ID generator + add strict monotonicity unit test

**Files:**

- Modify: `internal/core/ulid.go` (lines 20–25 of the current file)
- Modify: `internal/core/ulid_test.go` (append a new test)

**Background:** The spec requires `core.NewULID` to be explicitly documented as "the generator for event IDs and anything whose lex order must match arrival order," and that the monotonicity invariant is anchored by a unit test. The current doc comment is a single sparse line.

- [ ] **Step 1: Write the failing monotonicity test**

  Append to `internal/core/ulid_test.go`:

  ```go
  func TestNewULIDIsStrictlyMonotonicInTightLoop(t *testing.T) {
      // core.NewULID must be monotonic across calls, including within
      // the same millisecond. Two downstream invariants depend on this:
      //   1. PostgresEventStore.Replay uses `WHERE id > afterID ORDER BY id`,
      //      which silently skips events whose IDs sort below a preceding event.
      //   2. PostgresSessionStore.UpdateCursors uses a monotonicity CAS that
      //      rejects cursor writes lex-smaller than the stored value.
      // A non-monotonic generator produces lex-inverted IDs within a millisecond
      // under load, breaking both invariants.
      const n = 10_000
      var prev ulid.ULID
      for i := 0; i < n; i++ {
          cur := NewULID()
          if i > 0 {
              require.True(t, prev.String() < cur.String(),
                  "non-monotonic ULIDs at index %d: prev=%s cur=%s",
                  i, prev.String(), cur.String())
          }
          prev = cur
      }
  }
  ```

  The file already imports `testing`, `testify/assert`, `testify/require`. You also need to add `github.com/oklog/ulid/v2` to the imports (alias-free) — check with `goimports` or let `task fmt` handle it.

- [ ] **Step 2: Run test to verify it passes on current code**

  Run: `task test -- -run TestNewULIDIsStrictlyMonotonicInTightLoop ./internal/core/`

  Expected: PASS (current `NewULID` already uses `ulid.Monotonic(rand.Reader, 0)` with a mutex, so the invariant holds — this test is a regression guard, not a failing-first test).

  If it fails, something is already wrong with the generator and you should stop and investigate before continuing.

- [ ] **Step 3: Expand the `NewULID` doc comment**

  Replace lines 20–25 of `internal/core/ulid.go`:

  ```go
  // NewULID generates a monotonic-within-millisecond ULID using crypto/rand.
  //
  // Use this for: event IDs (core.Event.ID), session IDs, and any identifier
  // whose lexicographic order MUST match arrival order. The PostgresEventStore
  // relies on this property — Replay uses `WHERE id > afterID ORDER BY id` and
  // PostgresSessionStore.UpdateCursors uses a per-key monotonicity CAS on the
  // cursor JSONB value. A non-monotonic event ID can produce a lex-inverted
  // pair within the same millisecond, which silently breaks both replay
  // (the second event is skipped) and cursor advances (the second cursor is
  // rejected by the CAS).
  //
  // Do NOT use idgen.New() for these. The ruleguard rule EventIDMustBeMonotonic
  // in gorules/rules.go enforces this for core.Event{} struct literals.
  func NewULID() ulid.ULID {
      entropyLock.Lock()
      defer entropyLock.Unlock()
      return ulid.MustNew(ulid.Timestamp(time.Now()), entropy)
  }
  ```

  Leave the function body unchanged.

- [ ] **Step 4: Run tests and linters**

  Run: `task test -- ./internal/core/`
  Expected: PASS (all existing `TestNewULID*` + `TestParseULID*` + the new test)

  Run: `task lint`
  Expected: PASS (the new doc comment is well-formed)

- [ ] **Step 5: Commit**

  ```bash
  jj --no-pager commit -m "docs(core): expand NewULID doc comment with event-ID guarantee (holomush-43nd)

  The PostgresEventStore.Replay (ORDER BY id, WHERE id > afterID) and the
  upcoming cursor monotonicity CAS in PostgresSessionStore.UpdateCursors
  depend on event-ID lex order matching arrival order. core.NewULID already
  satisfies this via ulid.Monotonic(rand.Reader, 0) + mutex; this commit
  makes the contract explicit in the doc comment and adds a 10k-iteration
  regression test anchoring the invariant."
  ```

---

### Task 2: Document `idgen.New` boundary (do NOT use for event IDs)

**Files:**

- Modify: `internal/idgen/id.go` (lines 18–27 of the current file)

**Background:** The spec requires `idgen.New` to be explicitly documented as "for entity primary keys, NOT for event IDs." The current comment only mentions `ulid.Make()` avoidance.

- [ ] **Step 1: Expand the `New` doc comment**

  Replace the comment block before `func New() ulid.ULID { ... }` in `internal/idgen/id.go`:

  ```go
  // New generates a ULID with fresh crypto/rand entropy on every call.
  //
  // Use this for: entity primary keys (players, sessions, locations,
  // characters, exits, objects, policies, audit rows) where the ID is pure
  // identity and there is no requirement for IDs minted in temporal order to
  // also sort in temporal order.
  //
  // Do NOT use this for event IDs (core.Event.ID). Two calls in the same
  // millisecond produce IDs in random lexicographic order, which silently
  // breaks PostgresEventStore.Replay (ORDER BY id, WHERE id > afterID) and
  // PostgresSessionStore.UpdateCursors monotonicity. Use core.NewULID()
  // instead. The ruleguard rule EventIDMustBeMonotonic in gorules/rules.go
  // enforces this for core.Event{} struct literals.
  //
  // Panics if the system's cryptographic random source is unavailable,
  // which indicates an unrecoverable OS-level failure.
  func New() ulid.ULID {
      id, err := ulid.New(ulid.Timestamp(time.Now()), cryptorand.Reader)
      if err != nil {
          panic("id: crypto/rand unavailable: " + err.Error())
      }
      return id
  }
  ```

  Leave the function body unchanged.

- [ ] **Step 2: Run tests and linters**

  Run: `task test -- ./internal/idgen/`
  Expected: PASS

  Run: `task lint`
  Expected: PASS

- [ ] **Step 3: Commit**

  ```bash
  jj --no-pager commit -m "docs(idgen): document boundary — entity IDs only, not events (holomush-43nd)

  idgen.New uses fresh crypto/rand entropy per call and is NOT monotonic
  within a millisecond. Safe for entity primary keys (pure identity), unsafe
  for event IDs that flow through PostgresEventStore.Replay and cursor CAS.
  Documents this explicitly so future callers do not mistake identity-grade
  entropy for ordering-grade entropy."
  ```

---

### Task 3: Fix `BroadcastSystemMessage` to use `core.NewULID()` + regression test

**Files:**

- Modify: `internal/command/types.go` (line 602)
- Modify: `internal/command/types_test.go` (append new test)

**Background:** `Services.BroadcastSystemMessage` currently uses `idgen.New()` for its event ID, which is non-monotonic. This is a latent bug in `Replay` (two same-ms broadcasts can produce lex-inverted IDs; the second is skipped on replay) and a blocker for the cursor CAS. The spec identifies this as in-scope for holomush-43nd.

- [ ] **Step 1: Write the failing test for monotonic broadcast IDs**

  Append to `internal/command/types_test.go`. First find an existing test that constructs a `*Services` with a mock event store to see the pattern; reuse it. If you cannot find one, use this minimal pattern:

  ```go
  func TestBroadcastSystemMessageProducesMonotonicEventIDs(t *testing.T) {
      // BroadcastSystemMessage must mint monotonic ULIDs so two consecutive
      // broadcasts to the same stream do not produce lex-inverted event IDs
      // (which would cause PostgresEventStore.Replay to skip the second one
      // on reconnect and PostgresSessionStore.UpdateCursors CAS to reject
      // its cursor advance).
      captured := &captureEventStore{}
      svc := NewTestServices(ServicesConfig{
          Events: captured,
      })
      ctx := context.Background()

      svc.BroadcastSystemMessage(ctx, "stream:test", "first")
      svc.BroadcastSystemMessage(ctx, "stream:test", "second")

      require.Len(t, captured.events, 2)
      first := captured.events[0].ID.String()
      second := captured.events[1].ID.String()
      require.True(t, first < second,
          "BroadcastSystemMessage must produce monotonic IDs, got first=%s second=%s",
          first, second)
  }

  // captureEventStore is a minimal core.EventStore fake that records every
  // Append call for assertion. Subscribe/Replay/LastEventID are not used.
  type captureEventStore struct {
      mu     sync.Mutex
      events []core.Event
  }

  func (c *captureEventStore) Append(_ context.Context, ev core.Event) error {
      c.mu.Lock()
      defer c.mu.Unlock()
      c.events = append(c.events, ev)
      return nil
  }

  func (c *captureEventStore) Subscribe(context.Context, string) (<-chan ulid.ULID, <-chan error, error) {
      return nil, nil, errors.New("not implemented")
  }

  func (c *captureEventStore) Replay(context.Context, string, ulid.ULID, int) ([]core.Event, error) {
      return nil, errors.New("not implemented")
  }

  func (c *captureEventStore) LastEventID(context.Context, string) (ulid.ULID, error) {
      return ulid.ULID{}, errors.New("not implemented")
  }
  ```

  Imports needed: `context`, `errors`, `sync`, `testing`, `github.com/oklog/ulid/v2`, `github.com/stretchr/testify/require`, `github.com/holomush/holomush/internal/core`.

  If `NewTestServices` does not exist or has a different signature, use whatever constructor the existing tests use. The essential element is a captured `core.EventStore` fake.

- [ ] **Step 2: Run test to verify it may fail**

  Run: `task test -- -run TestBroadcastSystemMessageProducesMonotonicEventIDs ./internal/command/`

  Expected: may PASS or FAIL depending on whether both `idgen.New()` calls happened to land in different milliseconds. If you add a `runtime.Gosched()` between them the test may pass; in a tight loop on a fast machine the test SHOULD fail often. To make it deterministic pre-fix, loop the broadcast 1000 times inside the test and assert *all* pairs are monotonic:

  ```go
  // Replace the two-call block with a 1000-iteration loop
  const n = 1000
  for i := 0; i < n; i++ {
      svc.BroadcastSystemMessage(ctx, "stream:test", "msg")
  }
  require.Len(t, captured.events, n)
  for i := 1; i < n; i++ {
      prev := captured.events[i-1].ID.String()
      cur := captured.events[i].ID.String()
      require.True(t, prev < cur,
          "non-monotonic at i=%d: prev=%s cur=%s", i, prev, cur)
  }
  ```

  Run again: expected FAIL on the current code (at least one pair will be lex-inverted within a ms).

- [ ] **Step 3: Change `BroadcastSystemMessage` to use `core.NewULID()`**

  In `internal/command/types.go` around line 601, change:

  ```go
  event := core.Event{
      ID:        idgen.New(),
  ```

  to:

  ```go
  event := core.Event{
      // Event IDs MUST be monotonic for PostgresEventStore.Replay
      // (WHERE id > afterID ORDER BY id) and PostgresSessionStore cursor
      // CAS. See internal/core/ulid.go.
      ID:        core.NewULID(),
  ```

  Update imports: remove `"github.com/holomush/holomush/internal/idgen"` if no other code in this file uses it (check with the Grep tool), and ensure `"github.com/holomush/holomush/internal/core"` is imported.

- [ ] **Step 4: Run test to verify it passes**

  Run: `task test -- -run TestBroadcastSystemMessageProducesMonotonicEventIDs ./internal/command/`
  Expected: PASS (all 1000 pairs are monotonic under `core.NewULID`).

  Run: `task test -- ./internal/command/`
  Expected: PASS (no other test regresses).

- [ ] **Step 5: Commit**

  ```bash
  jj --no-pager commit -m "fix(command): BroadcastSystemMessage uses core.NewULID for monotonic IDs (holomush-43nd)

  idgen.New() uses fresh crypto/rand entropy per call, producing lex-inverted
  IDs within the same millisecond. PostgresEventStore.Replay uses ORDER BY id
  and WHERE id > afterID, so lex-inverted same-ms broadcasts were silently
  skipped on reconnect replay. Also blocks the upcoming cursor monotonicity
  CAS, which depends on event-ID lex order matching arrival order.

  Adds a 1000-iteration regression test that asserts every pair of
  consecutive BroadcastSystemMessage events is strictly lex-monotonic."
  ```

---

### Task 4: Add SQL-level monotonicity CAS to `PostgresSessionStore.UpdateCursors`

**Files:**

- Modify: `internal/store/session_store.go` (lines 341–355)
- Modify: `internal/store/session_store_test.go` (update `TestPostgresSessionStore_UpdateCursors` table + add new cases)
- Modify: `internal/store/session_store_integration_test.go` (add "rejects monotonicity regression" spec)

**Background:** The CAS is the actual fix for Finding 2. It prevents regression in the multi-Subscribe case (two browser tabs for the same session can each write cursors in parallel against different gRPC handler goroutines). The CAS is per-key and uses `COLLATE "C"` so it is independent of the database collation.

- [ ] **Step 1: Write the failing integration test (real Postgres)**

  In `internal/store/session_store_integration_test.go`, add a new `It` block inside the existing `Describe("UpdateCursors", ...)` (currently at line 454):

  ```go
  It("rejects a cursor regression for the same stream key", func() {
      // The CAS guard in UpdateCursors must preserve the highest cursor
      // ever stored for a (session, stream) pair. Regression attempts
      // (e.g., from a concurrent Subscribe that observed an earlier
      // event) must be silently ignored — RowsAffected==0 is not an
      // error, it just means another writer won with a higher cursor.
      ctx := context.Background()
      info := newTestSession("sess-cas-regression")
      Expect(sessionStore.Set(ctx, info.ID, info)).To(Succeed())

      // Mint two cursors with core.NewULID so they are strictly monotonic.
      // The second one is the lex-larger ("higher") cursor.
      earlier := core.NewULID()
      time.Sleep(1 * time.Millisecond)
      later := core.NewULID()
      Expect(earlier.String()).To(BeNumerically("<", later.String()))

      streamKey := "location:room-cas"

      // First write: `later` (the higher cursor).
      Expect(sessionStore.UpdateCursors(ctx, info.ID, map[string]ulid.ULID{
          streamKey: later,
      })).To(Succeed())

      // Second write: `earlier` (a regression). Must not error, but
      // must not overwrite the stored `later`.
      Expect(sessionStore.UpdateCursors(ctx, info.ID, map[string]ulid.ULID{
          streamKey: earlier,
      })).To(Succeed(),
          "regression attempts must not be errors — CAS rows_affected==0 is normal")

      got, err := sessionStore.Get(ctx, info.ID)
      Expect(err).NotTo(HaveOccurred())
      Expect(got.EventCursors[streamKey]).To(Equal(later),
          "stored cursor must remain the higher (later) value")
  })

  It("rejects multi-key cursor writes with UNSUPPORTED", func() {
      // Current production code (replayAndSend) always writes exactly
      // one key per call. Multi-key writes cannot be handled by a
      // single-statement per-key CAS, and silently applying CAS to only
      // one key would be a correctness hole. Fail loudly so a future
      // caller that assumes multi-key works gets a clear signal.
      ctx := context.Background()
      info := newTestSession("sess-cas-multikey")
      Expect(sessionStore.Set(ctx, info.ID, info)).To(Succeed())

      err := sessionStore.UpdateCursors(ctx, info.ID, map[string]ulid.ULID{
          "location:room-a": core.NewULID(),
          "location:room-b": core.NewULID(),
      })
      Expect(err).To(HaveOccurred())
      Expect(err.Error()).To(ContainSubstring("multi-key cursor updates are not supported"))
  })
  ```

  You will need to add the `internal/core` import to this test file if it is not already present: `github.com/holomush/holomush/internal/core`. Do NOT add testify — this file uses pure Ginkgo/Gomega and mixing them is a style violation.

  **Verify** the existing `It("merges new cursors with existing", ...)` test at line 455 still makes sense — it writes to a *different* key than it initially set, so the CAS (per-key) should not reject it. Under the new semantics, that test still passes because the key `location:room2` has no prior value, and the `IS NULL` branch of the CAS allows the write.

- [ ] **Step 2: Run integration test to verify it fails**

  Run: `task test:int` (all integration packages; during iterative dev you may use `go test -race -v -tags=integration -run TestRunIntegrationSuite ./internal/store/... -ginkgo.focus "rejects a cursor regression"` to focus on this spec, but `task test:int` is the authoritative gate)

  Note: if `TestRunIntegrationSuite` is not the correct entry point, use `rg 'var _ = Describe' internal/store/session_store_integration_test.go -B3` to find how the suite is run. Every Ginkgo suite in this project has a `<package>_suite_test.go` with `TestMain` or `RunSpecs`.

  Expected: FAIL — the current SQL does not have the CAS, so the regression write *succeeds* and the assertion `got.EventCursors[streamKey]` equals `earlier`, not `later`.

  The multi-key test also fails: the current code silently applies the JSONB merge to both keys.

- [ ] **Step 3: Rewrite `PostgresSessionStore.UpdateCursors`**

  Replace the current `UpdateCursors` (lines 341–355 of `internal/store/session_store.go`) with:

  ```go
  // UpdateCursors updates event cursors via JSONB merge with a per-key
  // monotonicity guard. A write is applied only if the new cursor is
  // strictly greater (lexicographic, COLLATE "C") than the stored value
  // for the key being written. Writes that lose the CAS race — i.e.,
  // another writer committed a higher cursor first — are silently
  // dropped (RowsAffected==0 is not an error, it is the correct outcome).
  //
  // The CAS depends on cursor values being monotonic ULIDs (core.NewULID),
  // not random ULIDs (idgen.New). A non-monotonic cursor can produce a
  // lex-inverted value within the same millisecond, causing legitimate
  // cursor advances to be silently rejected. The ruleguard rule
  // EventIDMustBeMonotonic in gorules/rules.go enforces monotonic event
  // IDs for core.Event{} struct literals.
  //
  // Multi-key writes are rejected with UNSUPPORTED. The only current
  // caller (CoreServer.replayAndSend) always passes one key, and a
  // single-statement per-key CAS cannot be expressed cleanly for multiple
  // keys. A future multi-key caller should refactor this function to
  // apply per-key CAS across multiple UPDATE statements in a transaction.
  func (s *PostgresSessionStore) UpdateCursors(ctx context.Context, id string, cursors map[string]ulid.ULID) error {
      if len(cursors) == 0 {
          return nil
      }
      if len(cursors) != 1 {
          return oops.Code("UNSUPPORTED").
              With("operation", "update cursors").
              With("session_id", id).
              With("key_count", len(cursors)).
              Errorf("multi-key cursor updates are not supported")
      }
      var streamKey string
      var newCursor ulid.ULID
      for k, v := range cursors {
          streamKey, newCursor = k, v
      }
      cursorsJSON, err := json.Marshal(cursors)
      if err != nil {
          return oops.With("operation", "marshal cursors").With("session_id", id).Wrap(err)
      }
      _, err = s.pool.Exec(ctx,
          `UPDATE sessions
              SET event_cursors = event_cursors || $1::jsonb,
                  updated_at = now()
            WHERE id = $2
              AND (
                  event_cursors->>$3 IS NULL
                  OR (event_cursors->>$3) COLLATE "C" < ($4::text) COLLATE "C"
              )`,
          cursorsJSON, id, streamKey, newCursor.String())
      if err != nil {
          return oops.With("operation", "update cursors").With("session_id", id).Wrap(err)
      }
      // RowsAffected==0 is intentional: another writer beat us with a higher
      // cursor. Do not surface it as an error.
      return nil
  }
  ```

- [ ] **Step 4: Update the unit test file**

  `internal/store/session_store_test.go` has `TestPostgresSessionStore_UpdateCursors` at line ~695. The existing test uses `pgxmock` with regex patterns for the SQL — those patterns will no longer match the new query. You must update them:

  - Change the `partial update` case regex from:

    ```text
    `UPDATE sessions SET event_cursors = event_cursors \|\| \$1::jsonb, updated_at = now\(\) WHERE id = \$2`
    ```

    to the new CAS pattern (the regex must escape every `(`, `)`, `|`, and match the multi-line `WHERE … AND (…)` structure):

    ```text
    `UPDATE sessions\s+SET event_cursors = event_cursors \|\| \$1::jsonb,\s+updated_at = now\(\)\s+WHERE id = \$2\s+AND \(\s+event_cursors->>\$3 IS NULL\s+OR \(event_cursors->>\$3\) COLLATE "C" < \(\$4::text\) COLLATE "C"\s+\)`
    ```

  - The `WithArgs(pgxmock.AnyArg(), "sess-abc")` call must now have **four** args: `pgxmock.AnyArg()` (the JSONB), `"sess-abc"` (the id), `"location:room-1"` (the stream key extracted from `cursors`), and `cursorID.String()` (the new cursor). The existing test uses `cursors := map[string]ulid.ULID{"location:room-1": cursorID}` at line ~697, so:

    ```go
    WithArgs(pgxmock.AnyArg(), "sess-abc", "location:room-1", cursorID.String()).
    ```

  - Apply the same arg-count update to the `database error` case.

  Add two new test cases to the same table:

  ```go
  {
      name:    "empty cursors map is a no-op",
      id:      "sess-abc",
      cursors: map[string]ulid.ULID{},
      setupMock: func(mock pgxmock.PgxPoolIface) {
          // no ExpectExec — the function should return early
      },
  },
  {
      name: "multi-key cursors returns UNSUPPORTED",
      id:   "sess-abc",
      cursors: map[string]ulid.ULID{
          "location:room-1": cursorID,
          "location:room-2": core.NewULID(),
      },
      setupMock: func(mock pgxmock.PgxPoolIface) {
          // no ExpectExec — the function should reject before querying
      },
      wantErr: true,
      errMsg:  "multi-key cursor updates are not supported",
  },
  ```

  Make sure the `internal/store/session_store_test.go` imports include `github.com/holomush/holomush/internal/core` if it isn't already there (it's used via `core.NewULID()` at line 696).

- [ ] **Step 5: Run unit tests**

  Run: `task test -- -run TestPostgresSessionStore_UpdateCursors ./internal/store/`

  Expected: PASS — all table cases (partial update, database error, empty cursors, multi-key UNSUPPORTED) green. If the regex fails to match, `pgxmock` will report "ExpectedExec does not match actual query" — tune the regex whitespace handling (ripgrep / real pgxmock strip whitespace, but the regex must still be syntactically valid).

- [ ] **Step 6: Run integration tests**

  Run: `task test:int` (authoritative gate; for iterative focus use `go test -race -v -tags=integration ./internal/store/... -ginkgo.focus "UpdateCursors"` during development only)

  Expected: PASS — the regression-rejection test now passes because the CAS prevents the regression; the multi-key test passes because the function returns UNSUPPORTED; the original "merges new cursors with existing" still passes.

- [ ] **Step 7: Commit**

  ```bash
  jj --no-pager commit -m "fix(store): add per-key monotonicity CAS to UpdateCursors (holomush-43nd)

  Finding 2 from holomush-43nd: the previous JSONB merge was last-write-wins
  per key with no row lock, no version column, and no monotonicity guard.
  Two concurrent Subscribes for the same session could write cursors for
  the same (session, stream) pair out of order, silently moving the
  persisted cursor backward in time.

  The CAS predicate (event_cursors->>\$key IS NULL OR stored COLLATE \"C\"
  < new COLLATE \"C\") makes regression impossible at the storage layer.
  Uses COLLATE \"C\" so the comparison is independent of the database's
  default collation. Multi-key writes are rejected with UNSUPPORTED because
  no current caller needs them and silently applying CAS to a subset of
  keys would be a correctness hole.

  Integration test: monotonicity-regression case against real Postgres
  via testcontainers. Unit test: pgxmock-backed coverage of the new SQL
  shape, the empty-map early return, and the multi-key rejection path."
  ```

---

### Task 5: Add monotonicity guard to `MemStore.UpdateCursors`

**Files:**

- Modify: `internal/session/memstore.go` (lines 184–202)
- Modify: `internal/session/memstore_test.go` (append new test)

**Background:** MemStore is used by unit tests (and only unit tests per the `//go:build !integration` tag on `store_memory.go`). Adding the same guard keeps in-memory semantics in sync with production, so unit tests for gRPC code cannot accidentally rely on last-write-wins behavior that would break in production.

- [ ] **Step 1: Find an existing memstore test for `UpdateCursors` or create a new file**

  Run: `rg 'UpdateCursors' internal/session/memstore_test.go`

  If there is no test file covering `UpdateCursors`, the new test goes into `internal/session/memstore_test.go` as a top-level function. If there is, add it alongside.

- [ ] **Step 2: Write the failing test**

  ```go
  func TestMemStoreUpdateCursorsRejectsRegression(t *testing.T) {
      ctx := context.Background()
      store := NewMemStore()

      sess := &Info{
          ID:            "sess-mem-cas",
          CharacterID:   ulid.Make(),
          CharacterName: "TestChar",
          LocationID:    ulid.Make(),
          Status:        StatusActive,
          EventCursors:  map[string]ulid.ULID{},
      }
      require.NoError(t, store.Set(ctx, sess.ID, sess))

      later := mustNewULID(t)
      time.Sleep(1 * time.Millisecond)
      earlier := mustNewULID(t)
      // mustNewULID mints via core.NewULID but we cannot import core here
      // (import cycle risk). Use oklog/ulid/v2 directly; swap ordering if
      // the mint order does not produce strictly-increasing IDs.
      if earlier.String() > later.String() {
          later, earlier = earlier, later
      }

      // Write the higher cursor first.
      require.NoError(t, store.UpdateCursors(ctx, sess.ID, map[string]ulid.ULID{
          "stream:x": later,
      }))
      // Attempt a regression — must be silently ignored.
      require.NoError(t, store.UpdateCursors(ctx, sess.ID, map[string]ulid.ULID{
          "stream:x": earlier,
      }))

      got, err := store.Get(ctx, sess.ID)
      require.NoError(t, err)
      assert.Equal(t, later, got.EventCursors["stream:x"],
          "MemStore must preserve the higher cursor on regression attempts")
  }

  // mustNewULID mints a ulid.ULID via ulid.Make — MemStore tests cannot
  // import internal/core without creating a cycle (core depends on session
  // transitively). The test only needs distinct, lex-sortable IDs.
  func mustNewULID(t *testing.T) ulid.ULID {
      t.Helper()
      return ulid.Make()
  }
  ```

  Imports needed: `context`, `testing`, `time`, `github.com/oklog/ulid/v2`, `github.com/stretchr/testify/assert`, `github.com/stretchr/testify/require`. Check the file's current imports before adding.

  **Warning:** `ulid.Make()` is non-monotonic. The test arranges the ordering with an `if earlier.String() > later.String() { swap }` so the "higher" cursor is written first regardless. This is the ONLY legitimate test use of `ulid.Make()` here — the production code uses `core.NewULID()`.

  The `gorules/rules.go` file (added in Task 10) uses a build tag and only scans production files. `ulid.Make()` in `_test.go` files is allowed.

- [ ] **Step 3: Run test to verify it fails**

  Run: `task test -- -run TestMemStoreUpdateCursorsRejectsRegression ./internal/session/`

  Expected: FAIL — the current MemStore does last-write-wins and stores `earlier` over `later`.

- [ ] **Step 4: Add the monotonicity guard**

  Replace the body of `MemStore.UpdateCursors` (lines 184–202 in `internal/session/memstore.go`):

  ```go
  // UpdateCursors updates event cursors with a per-key monotonicity guard.
  // Writes with a cursor lex-smaller-or-equal to the stored value are
  // silently ignored — this mirrors the PostgresSessionStore CAS so unit
  // tests exercise the same contract as production.
  func (m *MemStore) UpdateCursors(_ context.Context, id string, cursors map[string]ulid.ULID) error {
      m.mu.Lock()
      defer m.mu.Unlock()

      info, ok := m.sessions[id]
      if !ok {
          return oops.Code("SESSION_NOT_FOUND").
              With("session_id", id).
              Errorf("session not found")
      }
      if info.EventCursors == nil {
          info.EventCursors = make(map[string]ulid.ULID)
      }
      for k, v := range cursors {
          existing, hasExisting := info.EventCursors[k]
          if hasExisting && existing.String() >= v.String() {
              // Regression or no-op; preserve the existing higher cursor.
              continue
          }
          info.EventCursors[k] = v
      }
      return nil
  }
  ```

- [ ] **Step 5: Run test to verify it passes**

  Run: `task test -- ./internal/session/`
  Expected: PASS — the new test and all existing MemStore tests remain green.

- [ ] **Step 6: Commit**

  ```bash
  jj --no-pager commit -m "fix(session): add per-key monotonicity guard to MemStore.UpdateCursors (holomush-43nd)

  Mirrors the Postgres CAS in MemStore so unit tests exercise the same
  monotonicity contract as production. Previously MemStore did last-write-
  wins merging, which meant unit tests for gRPC cursor code could silently
  pass with regression-inducing inputs that would fail against Postgres."
  ```

---

### Task 6: Replace `persistCursorAsync` with sync cursor commit in `replayAndSend`

**Files:**

- Modify: `internal/grpc/server.go` (lines 42 area for `const`, 418–429 for `persistCursorAsync`, 484–510 for `replayAndSend`)

**Background:** Fix Findings 1 and 3. Removing the spawned goroutine closes the stale-read window (Finding 1) and eliminates the "writes pending at shutdown" category entirely (Finding 3) — `GracefulStop` already waits for handler goroutines, which now do the commit inline.

- [ ] **Step 1: Add the `cursorCommitTimeout` constant**

  Near the existing `defaultMaxReplay` constant around line 42 of `internal/grpc/server.go`, add:

  ```go
  // cursorCommitTimeout bounds the synchronous cursor commit inside
  // replayAndSend. 1 second is ~1000x the healthy-DB latency, well below
  // the "chat feels broken" threshold, and covers typical pool-wait hiccups.
  // On timeout, the write is dropped (logged at error level) and the live
  // loop continues — failure mode degrades to today's "duplicate-on-reconnect"
  // behavior, never worse.
  const cursorCommitTimeout = 1 * time.Second
  ```

- [ ] **Step 2: Delete `persistCursorAsync`**

  Remove lines 418–429 of `internal/grpc/server.go` (the entire function block):

  ```go
  // persistCursorAsync persists a cursor update to the session store in a background
  // goroutine (best-effort, non-blocking). Uses context.Background() intentionally:
  // the request ctx may be cancelled before the goroutine runs, but we still want
  // the durable cursor write to complete.
  func (s *CoreServer) persistCursorAsync(sessionID, streamName string, eventID ulid.ULID) {
      go func() {
          if err := s.sessionStore.UpdateCursors(context.Background(),
              sessionID, map[string]ulid.ULID{streamName: eventID}); err != nil {
              slog.Warn("cursor persist failed", "session_id", sessionID, "error", err)
          }
      }()
  }
  ```

- [ ] **Step 3: Replace the async call in `replayAndSend` with a sync commit**

  In `internal/grpc/server.go` starting at line 506, change:

  ```go
  if last != afterID {
      s.persistCursorAsync(info.ID, streamName, last)
  }
  return last, nil
  ```

  to:

  ```go
  if last != afterID {
      // Synchronous, bounded-timeout cursor commit. Uses a fresh context
      // so a client disconnect (which cancels `ctx`) does not abort the
      // write, matching the semantics the old persistCursorAsync intended.
      // The commit happens here so that any subsequent action — next loop
      // iteration, return-from-handler, or GracefulStop — observes the
      // cursor as durably reflecting `last`. Commit failures are logged
      // but do not break the client: they degrade gracefully to today's
      // "duplicate-on-reconnect" behavior.
      commitCtx, cancel := context.WithTimeout(context.Background(), cursorCommitTimeout)
      if updateErr := s.sessionStore.UpdateCursors(commitCtx, info.ID,
          map[string]ulid.ULID{streamName: last}); updateErr != nil {
          slog.ErrorContext(ctx, "cursor commit failed",
              "session_id", info.ID,
              "stream", streamName,
              "last_event", last.String(),
              "error", updateErr)
      }
      cancel()
  }
  return last, nil
  ```

  **Do NOT put `defer cancel()` inside the `if` block** — a `defer` inside a block does not fire until the enclosing function returns, which would leak the context across the rest of `replayAndSend`'s execution (none in this case, but it is a code-smell and a readability trap). Call `cancel()` explicitly after the commit.

- [ ] **Step 4: Verify imports**

  The function already imports `context`, `time`, `slog`, and `ulid`. The `time` package is needed for the new constant. No new imports required.

- [ ] **Step 5: Run unit tests**

  Run: `task test -- ./internal/grpc/`
  Expected: PASS. Existing tests do not exercise `persistCursorAsync` directly (it was only called from `replayAndSend`), so removing it is safe. If any test uses a mock session store whose `UpdateCursors` returns an error and the test asserts no error propagation, it still passes because we log-and-continue here.

- [ ] **Step 6: Run the integration test suite locally**

  Run: `task test:int`
  Expected: PASS — the existing "Reconnect flow" spec should get **faster** because the strict cursor-equality poll collapses to immediate satisfaction. Observe the runtime: it should drop from ~16s of cursor-waiting to <1s.

- [ ] **Step 7: Commit**

  ```bash
  jj --no-pager commit -m "fix(grpc): synchronous cursor commit in replayAndSend (holomush-43nd)

  Closes Findings 1 and 3 from holomush-43nd by removing the
  persistCursorAsync goroutine entirely. The live loop's own gRPC handler
  goroutine now commits the cursor inline before replayAndSend returns,
  using a fresh context.Background() with a 1-second bounded timeout so
  a client disconnect does not abort the write and a slow DB cannot stall
  the live loop for more than a perceptible moment.

  Finding 1 (stale window on fast reconnect) is gone because there is no
  longer a window between Send and commit — the commit runs before the
  loop can accept the next notification. Finding 3 (lost writes on
  shutdown) is gone by construction because GracefulStop already waits
  for handler goroutines to return, and the handler now performs every
  cursor write.

  The existing integration test in test/integration/session runs faster
  under this fix because its strict cursor-equality poll (added in
  PR #197) becomes an immediate-satisfaction assertion."
  ```

---

### Task 7: Add integration tests for Findings 1, 3 (Finding 2 has a store-level test already)

**Files:**

- Modify: `test/integration/session/session_persistence_integration_test.go`

**Background:** The existing "Reconnect flow" spec covers the happy path (missed events are replayed correctly). Finding 1's failing-first test is a deterministic contract check: the cursor reflects the latest sent event *immediately* after the subscription handler returns, with no polling. Finding 3's failing-first test asserts that `grpcServer.GracefulStop()` cannot return before the latest cursor write is durable.

- [ ] **Step 1: Add the Finding 1 integration test**

  In `test/integration/session/session_persistence_integration_test.go`, add a new `It` block inside the existing `Describe("Reconnect flow", ...)` (at line 219):

  ```go
  It("commits cursor synchronously — fast reconnect does not re-deliver the latest event", func() {
      sessionID, _ := loginAsGuest(testCtx, grpcCli)

      subCtx, subCancel := context.WithCancel(testCtx)
      stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
          SessionId: sessionID,
      })
      Expect(err).NotTo(HaveOccurred())

      _, err = grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
          SessionId: sessionID,
          Command:   "say hello",
      })
      Expect(err).NotTo(HaveOccurred())

      // Drain until the live `say` event arrives, capturing its ID.
      var liveSayID string
      Eventually(func() string {
          ev, recvErr := stream.Recv()
          if recvErr != nil {
              return ""
          }
          if frame := ev.GetEvent(); frame != nil && frame.GetType() == "say" {
              liveSayID = frame.GetId()
              return frame.GetType()
          }
          return ""
      }).WithTimeout(5 * time.Second).Should(Equal("say"))

      // Cancel the subscription, then drain the stream until EOF.
      // Under the fix, the cursor commit runs BEFORE the handler returns,
      // so by the time Recv() reports EOF, the cursor is already durable.
      subCancel()
      for {
          if _, recvErr := stream.Recv(); recvErr != nil {
              break
          }
      }

      // Read the cursor with NO polling. A polling Eventually here would
      // hide a regression to the async-commit behavior.
      locationStream := world.LocationStream(startLocation)
      sess, getErr := env.sessionStore.Get(testCtx, sessionID)
      Expect(getErr).NotTo(HaveOccurred())
      Expect(sess.EventCursors[locationStream].String()).To(Equal(liveSayID),
          "cursor must equal the latest sent event ID immediately after the handler exits — no polling")

      // Re-subscribe with replay_from_cursor=true. There must be ZERO
      // say events on the location stream — the cursor was already at
      // the live say event, so there is nothing to replay.
      replayCtx, replayCancel := context.WithTimeout(testCtx, 2*time.Second)
      defer replayCancel()
      replayStream, err := grpcCli.Subscribe(replayCtx, &corev1.SubscribeRequest{
          SessionId:        sessionID,
          ReplayFromCursor: true,
      })
      Expect(err).NotTo(HaveOccurred())

      sawReplayComplete := false
      sayCount := 0
      for !sawReplayComplete {
          ev, recvErr := replayStream.Recv()
          if recvErr != nil {
              break
          }
          if frame := ev.GetEvent(); frame != nil &&
              frame.GetType() == "say" && frame.GetStream() == locationStream {
              sayCount++
          }
          if ctrl := ev.GetControl(); ctrl != nil &&
              ctrl.Signal == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE {
              sawReplayComplete = true
          }
      }
      Expect(sawReplayComplete).To(BeTrue(), "must receive REPLAY_COMPLETE control frame")
      Expect(sayCount).To(Equal(0),
          "fast reconnect must not re-deliver the say event; cursor was already at it")
  })
  ```

- [ ] **Step 2: Add the Finding 3 integration test**

  Also inside `Describe("Reconnect flow", ...)` (after the Finding 1 test):

  ```go
  It("commits cursor before grpcServer.GracefulStop returns (no lost writes on shutdown)", func() {
      sessionID, _ := loginAsGuest(testCtx, grpcCli)

      subCtx, subCancel := context.WithCancel(testCtx)
      stream, err := grpcCli.Subscribe(subCtx, &corev1.SubscribeRequest{
          SessionId: sessionID,
      })
      Expect(err).NotTo(HaveOccurred())

      _, err = grpcCli.HandleCommand(testCtx, &corev1.HandleCommandRequest{
          SessionId: sessionID,
          Command:   "say hello",
      })
      Expect(err).NotTo(HaveOccurred())

      // Drain until the live `say` arrives.
      var liveSayID string
      Eventually(func() string {
          ev, recvErr := stream.Recv()
          if recvErr != nil {
              return ""
          }
          if frame := ev.GetEvent(); frame != nil && frame.GetType() == "say" {
              liveSayID = frame.GetId()
              return frame.GetType()
          }
          return ""
      }).WithTimeout(5 * time.Second).Should(Equal("say"))

      // Cancel the client sub and drain to EOF to free the handler goroutine.
      subCancel()
      for {
          if _, recvErr := stream.Recv(); recvErr != nil {
              break
          }
      }

      // GracefulStop blocks until in-flight RPC handlers return. The
      // Subscribe handler returns after committing the cursor inline,
      // so by the time GracefulStop unblocks, the commit is durable.
      grpcServer.GracefulStop()
      grpcServer = nil // prevent AfterEach from calling GracefulStop twice

      // Read the cursor through the same sessionStore handle — it uses
      // the shared pgxpool so a fresh Get sees the committed state.
      locationStream := world.LocationStream(startLocation)
      sess, getErr := env.sessionStore.Get(testCtx, sessionID)
      Expect(getErr).NotTo(HaveOccurred())
      Expect(sess.EventCursors[locationStream].String()).To(Equal(liveSayID),
          "cursor must reflect the latest sent event after GracefulStop returns")
  })
  ```

  **Important:** this test sets `grpcServer = nil` so the `AfterEach` block's `grpcServer.GracefulStop()` call does not panic on a nil-pointer dereference. Check `session_persistence_integration_test.go:204-217` to confirm `AfterEach` guards with `if grpcServer != nil`; it does. Good.

- [ ] **Step 3: Run the integration suite against the new tests**

  Run: `task test:int` (authoritative gate; during iterative dev you may use `go test -race -v -tags=integration ./test/integration/session/... -ginkgo.focus "Reconnect flow"` to narrow output)

  Expected: all three `It` blocks pass (original + 2 new). The original "replays missed events when client resubscribes after disconnect" still passes because the fix is strictly stronger than the behavior it exercised.

  If the Finding 1 test fails with `Expect(sess.EventCursors[locationStream].String()).To(Equal(liveSayID))` and the stored value is zero or an older event, Task 6 was not applied correctly — the commit is still async. Re-check `replayAndSend` in `internal/grpc/server.go`.

- [ ] **Step 4: Commit**

  ```bash
  jj --no-pager commit -m "test(session): integration tests for Findings 1 and 3 (holomush-43nd)

  Finding 1 test: asserts the cursor equals the latest sent event ID
  IMMEDIATELY after subscription teardown, with no polling. A polling
  Eventually here would hide a regression to async-commit behavior.

  Finding 3 test: asserts the cursor is durable after GracefulStop returns.
  This works because GracefulStop blocks on handler return, and the handler
  now commits the cursor inline before exiting. Pre-fix, the cursor write
  goroutine could be scheduled after GracefulStop unblocks.

  Finding 2 is already covered by the store-level integration test
  added in the previous commit; an E2E variant would only duplicate
  the coverage without adding signal."
  ```

---

### Task 8: Add CLAUDE.md guidance for ULID generator choice

**Files:**

- Modify: `CLAUDE.md` (insert new subsection after the existing "Random Number Generation" section around line 165)

**Background:** The spec requires human-readable guidance in the project's top-level instructions file so the next person (or AI agent) making a change that involves event IDs sees the rule before writing code.

- [ ] **Step 1: Add the new subsection**

  Open `CLAUDE.md` and locate the "Random Number Generation" subsection (around line 161–165). After it (but before "Error Handling"), insert:

  ````markdown
  ### ULID Generation

  Two ULID generators exist; the choice matters because the event store relies
  on lex order matching arrival order.

  \| Use case \| Generator \| Why \|
  \| --- \| --- \| --- \|
  \| **Event IDs** (`core.Event.ID`), session IDs \| `core.NewULID()` \| Monotonic within a millisecond. `PostgresEventStore.Replay` uses `WHERE id > afterID ORDER BY id`; cursor advances use a SQL monotonicity CAS. Non-monotonic event IDs silently break both. \|
  \| **Entity primary keys** (players, locations, characters, exits, objects, policies) \| `idgen.New()` \| Identity, not ordering. Fresh `crypto/rand` entropy per call. \|

  Enforced by the `EventIDMustBeMonotonic` ruleguard rule in `gorules/rules.go`
  (loaded via `gocritic`). New `core.Event{}` literals using `idgen.New()` will
  fail `task lint`.
  ````

  **Important for markdown editing:** the `|` characters in the table must be escaped as `\|` per the project's markdown conventions (from `MEMORY.md`). Run `task fmt` after the edit to let `rumdl` realign the table pipes.

- [ ] **Step 2: Run markdown format**

  Run: `task fmt`

  This will run `rumdl fmt` which realigns the table. If `rumdl` unescapes the `|` characters inside table cells (which is correct — they only need escaping in non-table contexts), the final output will have plain `|` in the table but still be valid. Trust the formatter.

- [ ] **Step 3: Run markdown lint**

  Run: `task lint`

  Expected: PASS. If markdown lint complains about the new table, inspect and fix; this is usually a whitespace issue that `task fmt` should have handled.

- [ ] **Step 4: Commit**

  ```bash
  jj --no-pager commit -m "docs(claude-md): ULID generator choice rule (holomush-43nd)

  Sibling to the existing 'Random Number Generation' section. Makes the
  event-ID vs entity-ID boundary explicit for both humans and AI agents
  so the next change touching event IDs does not accidentally reintroduce
  the BroadcastSystemMessage-style hazard."
  ```

---

### Task 9: Add `gorules/rules.go` with EventIDMustBeMonotonic + ULIDMakeForbidden; enable ruleguard in golangci-lint; delete `Taskfile.yaml` bash check

**Files:**

- Create: `gorules/rules.go`
- Modify: `.golangci.yaml` (add `enabled-checks: [ruleguard]` and `settings.ruleguard` under `gocritic`)
- Modify: `Taskfile.yaml` (remove `lint:ulid-make` task definition and its entry in the `lint:` aggregator)
- Modify: `go.mod` / `go.sum` (regenerated by `go mod tidy`)

**Background:** Replace the out-of-band bash check in `Taskfile.yaml:346` with a first-class `gocritic` ruleguard rule that runs as part of `task lint`. The rule catches both the pre-existing `ulid.Make()` hazard and the new `core.Event{... ID: idgen.New() ...}` hazard.

- [ ] **Step 1: Create `gorules/rules.go`**

  ```go
  // SPDX-License-Identifier: Apache-2.0
  // Copyright 2026 HoloMUSH Contributors

  //go:build ruleguard
  // +build ruleguard

  // Package gorules contains custom go-ruleguard rules loaded by gocritic.
  //
  // These rules enforce project invariants that cannot be expressed via
  // standard linters. The file is build-tagged so it never compiles with
  // the rest of the project — gocritic loads it via its ruleguard checker
  // configured in .golangci.yaml.
  package gorules

  import "github.com/quasilyte/go-ruleguard/dsl"

  // EventIDMustBeMonotonic ensures core.Event{} literals use core.NewULID()
  // (monotonic-within-millisecond entropy), not idgen.New() (fresh random
  // per call). Non-monotonic event IDs silently break PostgresEventStore.Replay
  // (WHERE id > afterID ORDER BY id) and PostgresSessionStore cursor
  // monotonicity. See internal/core/ulid.go for the invariant documentation.
  func EventIDMustBeMonotonic(m dsl.Matcher) {
      m.Match(`core.Event{$*_, ID: idgen.New(), $*_}`).
          Report(`event IDs must use core.NewULID() (monotonic), not idgen.New() (random) — see internal/core/ulid.go`)
  }

  // ULIDMakeForbidden forbids ulid.Make() in production code. ulid.Make()
  // uses math/rand internally, violating the project-wide crypto/rand rule.
  // Use idgen.New() for entity IDs or core.NewULID() for event IDs. This
  // rule replaces the bash check previously at Taskfile.yaml:346.
  func ULIDMakeForbidden(m dsl.Matcher) {
      m.Match(`ulid.Make()`).
          Report(`use idgen.New() for entity IDs or core.NewULID() for event IDs; ulid.Make() uses math/rand`)
  }
  ```

  Do **not** add a `_test.go` file for `gorules/rules.go` — it is DSL, not regular Go, and cannot be exercised by `go test`.

- [ ] **Step 2: Update `.golangci.yaml`**

  Replace the `gocritic` block (lines 120–126 of the current file):

  ```yaml
  gocritic:
    enabled-tags:
      - diagnostic
      - style
      - performance
    disabled-checks:
      - hugeParam # Event struct (120 bytes) is passed by value by design
  ```

  with:

  ```yaml
  gocritic:
    enabled-tags:
      - diagnostic
      - style
      - performance
    enabled-checks:
      - ruleguard
    disabled-checks:
      - hugeParam # Event struct (120 bytes) is passed by value by design
    settings:
      ruleguard:
        rules: gorules/rules.go
  ```

- [ ] **Step 3: Remove the `lint:ulid-make` task from `Taskfile.yaml`**

  Delete the `lint:ulid-make` task definition at lines 346–357:

  ```yaml
  lint:ulid-make:
    desc: Prevent ulid.Make() in production code (use idgen.New() from internal/idgen)
    cmds:
      - |
        set -euo pipefail
        CALLS=$(grep -rn --include="*.go" --exclude="*_test.go" --exclude="*.pb.go" \
          'ulid\.Make()' internal/ pkg/ cmd/ plugins/ 2>/dev/null | grep -v '^\s*//' | grep -v 'internal/idgen/' || true)
        if [ -n "$CALLS" ]; then
          echo "ERROR: Found ulid.Make() in production code (use idgen.New() from internal/idgen):"
          echo "$CALLS"
          exit 1
        fi
  ```

  And remove the entry in the `lint:` aggregator at line 63:

  ```yaml
  - task: lint:ulid-make
  ```

- [ ] **Step 4: Run `go mod tidy` to pick up `go-ruleguard/dsl`**

  Run: `go mod tidy`

  Expected: `go.mod` gains a `require github.com/quasilyte/go-ruleguard vX.Y.Z // indirect` (or a direct requirement — depends on how golangci-lint v2 ships ruleguard). If the dependency does not appear, the build-tagged file is not being seen by `go mod tidy` — you can force it by adding `//go:build !skip` or simpler, temporarily remove the build tag and rerun, then re-add. Alternatively, add an explicit `require` line for the DSL package version that golangci-lint uses.

  If `task lint` later fails with "cannot load gorules/rules.go: missing import", look up the DSL package version in the golangci-lint release notes for your version and pin it with `go get github.com/quasilyte/go-ruleguard/dsl@vX.Y.Z`.

- [ ] **Step 5: Run `task lint` to verify ruleguard fires on existing code**

  Run: `task lint`

  Expected: PASS — every `core.Event{}` literal in the codebase now uses `core.NewULID` (after Task 3), and there should be no `ulid.Make()` in production code (the existing Taskfile check prevents it). If the ruleguard rule reports false positives or fails to load, check:
  - The build tag line matches exactly: `//go:build ruleguard` (no extra spaces)
  - The `.golangci.yaml` path is relative to repo root: `gorules/rules.go`
  - `gocritic` is enabled in `linters.enable` (it already is, at line 32)

- [ ] **Step 6: Verify the rule actually catches a violation (sanity check)**

  Temporarily add this line to `internal/command/types.go` (or any file that imports `core`):

  ```go
  var _ = core.Event{ID: idgen.New()}
  ```

  Run: `task lint`

  Expected: FAIL with `event IDs must use core.NewULID() (monotonic), not idgen.New() (random) — see internal/core/ulid.go`.

  **Remove the temporary line before committing.**

- [ ] **Step 7: Commit**

  ```bash
  jj --no-pager commit -m "chore(lint): gocritic ruleguard enforces ULID generator boundaries (holomush-43nd)

  Adds gorules/rules.go with two ruleguard rules loaded by gocritic:

    - EventIDMustBeMonotonic: fails the build on core.Event{} literals
      constructed with idgen.New() instead of core.NewULID(). Prevents
      reintroduction of the BroadcastSystemMessage hazard where
      non-monotonic event IDs silently broke Replay and the cursor CAS.

    - ULIDMakeForbidden: fails the build on ulid.Make() in production code.
      Replaces the bash-based lint:ulid-make task in Taskfile.yaml, which
      is removed in this commit.

  Consolidates two enforcement mechanisms (one bash, one Go-based) into a
  single ruleguard source file. Adds go-ruleguard/dsl as a module
  requirement for the build-tagged rules file."
  ```

---

### Task 10: Full `task pr-prep` gate + workspace-local sanity

**Files:** none modified.

**Background:** Project rule: `task pr-prep` must be green before any push. This is the CI mirror — lint, format, schema, license, unit, integration, and E2E. Docker must be available. No approximation with subset checks.

- [ ] **Step 1: Restore any `internal/web/dist/` churn**

  The default workspace had uncommitted `internal/web/dist/` churn at the start of this work. This workspace was created fresh from `holomush-u37v`, so it should not have any of that churn — but `task build` or `task web:embed` may regenerate it. If so, discard it before committing:

  ```bash
  jj --no-pager st
  # If internal/web/dist/ files appear: restore them
  jj --no-pager restore internal/web/dist/
  ```

  The `internal/web/dist/` churn does not belong in this PR per memory note.

- [ ] **Step 2: Run `task fmt`**

  Run: `task fmt`

  Expected: no-op (all edits during implementation were followed by `task fmt`).

- [ ] **Step 3: Run `task pr-prep`**

  Run: `task pr-prep`

  Expected: GREEN across all stages.

  Common failure modes and their fixes:
  - **Integration test timeout on first run** — testcontainers is pulling images. Re-run.
  - **License header missing** — the lefthook pre-commit hook should have added them. Run `task license:add` manually.
  - **ruleguard load error** — see Task 9 Step 4 about go.mod.
  - **Finding 1 test fails with cursor zero** — Task 6 was not applied correctly; recheck `replayAndSend`.
  - **Finding 2 test fails with the regression write succeeding** — Task 4 was not applied correctly; recheck the `UpdateCursors` SQL.

  If any failure persists after one retry, **do not approximate with subset checks** (per project rule). Stop, read the error, fix the root cause, rerun the full `pr-prep`.

- [ ] **Step 4: Final `jj st` sanity check**

  Run: `jj --no-pager st && jj --no-pager log -r 'main..@' --limit 12`

  Expected working copy:
  - `@` has no uncommitted changes
  - Log shows the spec commit plus 8 implementation commits (one per Task 1–9), all attributed to `seanb4t`, all with conventional-commits style messages

- [ ] **Step 5: Push to PR branch (handled in Task 11, not here)**

  Do not push yet. The next task handles bookmark creation and PR opening.

---

### Task 11: Open PR against main

**Files:** none modified.

**Background:** Per project workflow, PRs are squash-merged to main. The PR body must reference `holomush-43nd` explicitly and list the three findings it fixes. Do NOT close the bead on PR open — close only after the PR is squash-merged. Add a note to the bead with the PR URL.

- [ ] **Step 1: Create a bookmark for this branch**

  The jj workspace is based on `holomush-u37v`. The PR for this work should target `main`, but the branch must contain both the `holomush-u37v` commits and the new commits. Since PR #197 (`holomush-u37v`) is not yet merged, the new PR will be stacked on it.

  Run:

  ```bash
  jj --no-pager bookmark create holomush-43nd -r @-
  ```

  Note: `@` is an empty change (jj's convention after a commit is to create a new empty working-copy change). `@-` is the last real commit (Task 9's commit). The bookmark points at that commit.

- [ ] **Step 2: Push the bookmark**

  Run:

  ```bash
  jj --no-pager git push -b holomush-43nd
  ```

  Expected: the branch is created on origin. If the branch already exists (from a previous failed attempt), the push updates it.

- [ ] **Step 3: Open the PR via `gh`**

  Run:

  ```bash
  gh pr create --base main --head holomush-43nd \
    --title "fix(session): cursor persistence races — sync commit + SQL CAS (holomush-43nd)" \
    --body "$(cat <<'EOF'
  Closes the three cursor persistence races identified in \`holomush-43nd\`, discovered while addressing CodeRabbit feedback on #197.

  ## Problem

  \`CoreServer.persistCursorAsync\` wrote session cursors as fire-and-forget goroutines with no ordering guarantee, no synchronization with the wire Send, and no shutdown drain. Three independent failure modes followed:

  1. **Stale-cursor window on fast reconnect** — \`grpcStream.Send\` fired before the cursor goroutine was even spawned, let alone committed. A client disconnect in that window followed by a reconnect with \`replay_from_cursor=true\` re-delivered the most recent event.

  2. **Cursor regression under goroutine reordering** — Two consecutive \`persistCursorAsync\` calls for the same \`(session, stream)\` could interleave, with the later-spawned goroutine winning the pool/scheduler and committing a *smaller* cursor after the earlier one. Last-write-wins JSONB merge had no monotonicity guard. The persisted cursor moved backward in time, making its stated purpose — "furthest event the client has seen" — a lie.

  3. **Lost writes on graceful shutdown** — \`persistCursorAsync\` used \`context.Background()\` deliberately but had no WaitGroup or drain. \`GracefulStop\` waited for in-flight RPCs, not for goroutines spawned during them. Pending cursor writes were silently dropped at shutdown.

  ## Fix

  **A + SQL CAS, paired** — each finding addressed at its own root cause:

  | Finding | Root cause | Fix |
  | --- | --- | --- |
  | 1 — stale window | Time-of-Send vs time-of-commit divergence | Sync cursor commit inside \`replayAndSend\` with a 1-second bounded timeout |
  | 2 — regression | \`UpdateCursors\` has no monotonicity guard; LWW JSONB merge | SQL-level CAS: \`WHERE event_cursors->>\$key IS NULL OR (event_cursors->>\$key) COLLATE "C" < \$new::text COLLATE "C"\` |
  | 3 — lost on shutdown | Untracked goroutines outliving the handler | Delete \`persistCursorAsync\` entirely; handler goroutine now commits inline, and \`GracefulStop\` already waits for handlers to return |

  The CAS is **not** belt-and-suspenders for the sync write — it is the actual fix for Finding 2 in the multi-Subscribe case (two browser tabs, or a reconnect that overlaps the old loop's teardown). Sync-in-loop alone does not close this.

  ## Discovered and fixed along the way

  \`Services.BroadcastSystemMessage\` was minting event IDs via \`idgen.New()\`, which is non-monotonic (fresh crypto/rand entropy per call). \`PostgresEventStore.Replay\` uses \`WHERE id > afterID ORDER BY id\`, so two same-ms broadcasts to the same stream could produce lex-inverted IDs and the second one was **silently skipped** on reconnect replay. This is a latent bug independent of the cursor work, and a blocker for the CAS which depends on event-ID lex order matching arrival order. Fixed to use \`core.NewULID()\`.

  A new \`gocritic\` ruleguard rule (\`gorules/rules.go\`) now fails the build on any \`core.Event{}\` literal constructed with \`idgen.New()\`, preventing reintroduction. The same file subsumes the existing \`ulid.Make()\` bash check from \`Taskfile.yaml\`, consolidating ULID-discipline enforcement in one place.

  Both ULID generators are now explicitly documented:
  - \`core.NewULID()\`: monotonic — for event IDs, session IDs, and anything whose lex order must match arrival order
  - \`idgen.New()\`: non-monotonic — for entity primary keys where the ID is pure identity

  \`CLAUDE.md\` gains a new "ULID Generation" subsection documenting the rule for humans and AI agents.

  ## Test plan

  Each finding has a deterministic failing-first test anchored to a contract, not a race:

  - [x] **Finding 2 (store-level integration):** \`internal/store/session_store_integration_test.go\` — "rejects a cursor regression for the same stream key" against real Postgres via testcontainers. CAS \`RowsAffected==0\` + direct read-back.
  - [x] **Finding 1 (E2E integration):** \`test/integration/session/session_persistence_integration_test.go\` — "commits cursor synchronously — fast reconnect does not re-deliver the latest event". Reads cursor with NO polling immediately after handler EOF; polling would hide a regression.
  - [x] **Finding 3 (E2E integration):** same file — "commits cursor before grpcServer.GracefulStop returns (no lost writes on shutdown)". Asserts the cursor is durable after \`GracefulStop\` unblocks.
  - [x] **ULID monotonicity unit test:** \`internal/core/ulid_test.go\` — 10k-iteration tight loop asserting strict monotonicity. Anchors the invariant the CAS depends on.
  - [x] **\`BroadcastSystemMessage\` regression test:** \`internal/command/types_test.go\` — 1000 consecutive broadcasts, all pairs strictly monotonic.
  - [x] **Multi-key UpdateCursors fails loud:** integration + unit test coverage.
  - [x] **MemStore parity:** unit test for the in-memory monotonicity guard.
  - [x] \`task pr-prep\` green locally with the full CI mirror.

  ## Observable behavior change

  The strict cursor-equality poll added in #197 becomes an immediate-satisfaction assertion. Test runtime drops from ~25s to ~9s. In production the behavior is "cursor is durable before the live loop processes the next notification," which is a strict improvement over "cursor is eventually durable."

  ## Spec

  \`docs/superpowers/specs/2026-04-07-cursor-persistence-races-design.md\`

  ## Links

  - Bead: \`holomush-43nd\`
  - Discovered from: \`holomush-u37v\` (#197 — PR this one is stacked on)
  EOF
  )"
  ```

  Expected: `gh` returns the PR URL.

- [ ] **Step 4: Annotate the bead with the PR URL**

  Do NOT close the bead. Just add a note:

  ```bash
  bd update holomush-43nd --notes "PR opened: <PR_URL_FROM_STEP_3>"
  ```

  (Run this from the default workspace if `bd` cannot find the database from inside `.worktrees/holomush_holomush-43nd`; the default workspace has `.beads/` initialized.)

- [ ] **Step 5: Verify PR rendered correctly**

  Run:

  ```bash
  gh pr view <PR_NUMBER> --json title,body,reviewDecision
  ```

  Sanity-check that the body renders the table correctly and the `holomush-43nd` reference is visible.

---

## Self-Review Checklist (run before declaring the plan done)

### Spec coverage

- [x] Finding 1 (stale window) → Task 6 (sync commit) + Task 7 (Finding 1 integration test)
- [x] Finding 2 (regression) → Task 4 (Postgres CAS + unit tests + integration test) + Task 5 (MemStore parity)
- [x] Finding 3 (lost on shutdown) → Task 6 (delete `persistCursorAsync`) + Task 7 (Finding 3 integration test)
- [x] `BroadcastSystemMessage` ULID hazard → Task 3
- [x] `core.NewULID` doc + monotonicity test → Task 1
- [x] `idgen.New` doc → Task 2
- [x] `gorules/rules.go` ruleguard rule → Task 9
- [x] `.golangci.yaml` ruleguard enablement → Task 9
- [x] `Taskfile.yaml` bash-check deletion → Task 9
- [x] `CLAUDE.md` ULID guidance → Task 8
- [x] `cursorCommitTimeout = 1 * time.Second` → Task 6
- [x] `COLLATE "C"` in CAS predicate → Task 4
- [x] Multi-key `UpdateCursors` rejection → Task 4
- [x] `task pr-prep` gate → Task 10
- [x] PR against main → Task 11
- [x] Bead annotation (not closure) → Task 11

### Placeholder scan

- No "TODO", "TBD", "fill in later", "add appropriate error handling", or "similar to Task N" strings. Every step has concrete code, concrete commands, and expected output.

### Type consistency

- `cursorCommitTimeout` — same name in Task 6 Step 1 (const declaration) and Task 6 Step 3 (use site). ✓
- `UpdateCursors` signature — unchanged across Tasks 4 and 5. ✓
- `EventIDMustBeMonotonic` and `ULIDMakeForbidden` — same names in Task 9 and in the CLAUDE.md reference in Task 8. ✓
- `gorules/rules.go` path — consistent across Tasks 8, 9, and the CLAUDE.md text. ✓
- `holomush-43nd` bead ID — consistent across all commit messages and the PR body. ✓

---

## Appendix: Quick Command Reference

```bash
# Navigate to workspace
cd /Users/sean/Code/github.com/holomush/.worktrees/holomush_holomush-43nd

# Run a targeted unit test
task test -- -run TestFooBar ./internal/package/

# Run all integration tests (requires Docker) — authoritative gate
task test:int

# Iterative-dev only: focus a Ginkgo spec (not for committed scripts)
# go test -race -v -tags=integration ./internal/store/... -ginkgo.focus "pattern"

# Full PR prep (required before push)
task pr-prep

# Commit via jj
jj --no-pager commit -m "type(scope): description

Long-form body."

# Check jj state
jj --no-pager st
jj --no-pager log -r 'main..@' --limit 12

# Push bookmark
jj --no-pager bookmark create <name> -r @-
jj --no-pager git push -b <name>
```
