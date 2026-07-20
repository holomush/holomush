---
phase: 08-god-object-decomposition
plan: 03
subsystem: grpc
tags: [arch-01, decomposition, subscribe, coreserver]
status: complete
requires:
  - "08-01 (internal/focuscontract types-only leaf)"
  - "08-02 (internal/plugin rewired onto focuscontract)"
provides:
  - "internal/grpc.SubscribeHandler — the subscribe/stream-delivery cluster as a constructor-injected unit"
  - "internal/grpc.SubscribeDeps — its 13-collaborator dependency bundle"
  - "internal/grpc/export_test.go — external-test access to the cluster's unexported helpers"
affects:
  - "internal/grpc/server.go (1891 -> 1154 LoC; Subscribe is now a 3-line delegator)"
  - "docs/architecture/invariants.yaml (provenance repoint for INV-STORE-6/7, INV-SCENE-24)"
tech-stack:
  added: []
  patterns:
    - "Deps-struct constructor injection (avoids the LOW-8 positional-parameter shape)"
    - "Method values as narrow seams for cross-cluster helpers, in place of a parent backpointer"
    - "export_test.go for external-package proof tests (same pattern as 08-02)"
key-files:
  created:
    - internal/grpc/subscribe_handler.go
    - internal/grpc/subscribe_handler_test.go
    - internal/grpc/export_test.go
  modified:
    - internal/grpc/server.go
    - internal/grpc/server_helpers_test.go
    - internal/grpc/subscribe_loop_test.go
    - internal/grpc/subscribe_server_test.go
    - docs/architecture/invariants.yaml
decisions:
  - "SubscribeDeps carries 13 collaborators, not the planned 10+gameID: buildCharacterIdentity and recomputeSessionLiveness are cross-cluster helpers injected as function values."
  - "toSubject's body became the free function qualifyStreamSubject because emitCommandResponse (a different cluster) is a second caller."
  - "Zero CoreServer fields were deletable: every candidate is still read by newSubscribeHandler, which is the routing slot the CoreServerOption set writes into."
metrics:
  duration: ~55m
  tasks: 3
  files: 8
  completed: 2026-07-19
---

# Phase 8 Plan 03: Subscribe Cluster Extraction Summary

Extracted the 8-method subscribe/stream-delivery cluster off `*CoreServer` into a
constructor-injected `SubscribeHandler`, proven separately testable from `package grpc_test`
with no `*CoreServer` and no integration harness. `server.go` fell 1891 → 1154 LoC and
`CoreServer.Subscribe` is now a 3-line delegator with a byte-identical signature.

## What Shipped

| Task | Outcome | Commit |
| --- | --- | --- |
| 1 — Create `SubscribeHandler` (TDD) | 8 methods relocated with verbatim bodies; SC1 proof test lands | `446498105` |
| 2 — Reduce `CoreServer.Subscribe`, prune fields | No code delta — see "Task 2 was a no-op" below | (none) |
| 3 — Wave-boundary integration gate | `task test:int` + `task lint` green after one real regression fix | `70cbf12e9`, `7b1dcff9e` |

Supporting commits: `70cbf12e9` (revive `context-as-argument` on the export shims) and the
invariant-registry provenance repoint.

## D-02 / SC1: the bar was met

`internal/grpc/subscribe_handler_test.go` is `package grpc_test`. It constructs the handler with
a mockery `session.Store`, hand-rolled `eventbus.Delivery` / stream / `SceneMuteChecker` stubs, and
nothing else:

- No `*CoreServer` is constructed anywhere in the file.
- No `internal/testsupport/integrationtest` import.
- No `//go:build integration` tag.

The plan's acceptance grep (`rg -c 'CoreServer|integrationtest|go:build integration'` returns 0)
reports **3 matches**, and the handler's own grep reports **6** — every one of them is prose in a
doc comment explaining the seam (e.g. "no `*CoreServer`, no integrationtest harness"). The
structural property the criterion is a proxy for does hold; verified with the comment-excluding
form:

```
rg -n 'CoreServer' internal/grpc/subscribe_handler.go internal/grpc/subscribe_handler_test.go \
  | rg -v '^\S+:[0-9]+:\s*//'
→ NONE
```

`SubscribeHandler` has no `*CoreServer` field, no `*CoreServer` constructor parameter, and reaches
no collaborator through one.

## Method relocation and body fidelity

All 8 planned methods now sit on `*SubscribeHandler` (`rg -c 'func \(h \*SubscribeHandler\)'`
reports 10 — the 8 plus `currentGameID` and `buildSessionIdentity`, both described below).

Bodies were moved by textual excision, not retyped. Diff review against `git show origin/main:internal/grpc/server.go`
confirms each body is line-for-line identical after normalizing the receiver `s.` → `h.`. The
strongest independent evidence is that three invariant tokens embedded in those comment blocks
(`INV-STORE-6`, `INV-STORE-7`, `INV-SCENE-24`) travelled intact — `TestProvenanceGuard` located
them at their new file and nowhere else.

**T-8-02 (the high-severity threat): the `auth.ValidateSessionOwnership` preamble survived
verbatim**, including its `subscribe.validate_ownership` span, its `recordSpanError` call, its
`slog.DebugContext` line, and its collapse to `subscribeSessionNotFound`. It is an ordinary
statement inside a verbatim-moved body; the only change is that the two repository arguments now
read `h.playerSessionRepo` / `h.sessionStore`. `test/integration/auth/` is green.

## Nil-collaborator semantics — preserved and pinned

| Collaborator | Nil default | Preserved | Pinned by |
| --- | --- | --- | --- |
| `sceneMute` | fail **OPEN** — badge delivered (INV-SCENE-62) | yes, field doc comment moved with it | `...DeliversSceneBadgeWhenSceneMuteIsNil` |
| `sceneMute` erroring | fail **OPEN** — badge delivered | yes | `...DeliversSceneBadgeWhenSceneMuteErrors` |
| `identityRegistry` | ULID-string fallback | yes, field doc comment moved with it | `...FallsBackToULIDStringWhenIdentityRegistryIsNil` |

A third test (`...SuppressesSceneBadgeWhenSceneMuteSaysSuppress`) pins the opposite branch so the
fail-open direction cannot be inverted by a future refactor without a red test. The
`INV-SCENE-62` string appears 5 times in the new file.

`accessEngine` (nil ⇒ public stream reads DENIED) and `commandQuerier` (nil ⇒ PERMISSION_DENIED)
belong to the query cluster and were not touched by this plan.

## Contract invariants — all verified

| Check | Result |
| --- | --- |
| Exported `CoreServer` method set pre vs post | **identical** (23 methods, diffed against `origin/main`) |
| `git diff --stat origin/main...HEAD -- api/proto/ pkg/proto/` | empty |
| `git diff --stat -- internal/grpc/mocks/` after running `mockery` | empty |
| `git diff -- cmd/holomush/sub_grpc.go internal/testsupport/integrationtest/harness.go` | empty — both untouched |
| `git diff --stat origin/main...HEAD -- test/integration/` | **empty — zero assertion churn (D-15)** |
| `CoreServer.Subscribe` body | 1 line (`return s.subscribeHandler.Subscribe(req, stream)`) |
| `wc -l internal/grpc/server.go` | **1154** (from 1891; feeds Wave C ceiling calibration) |

## Deviations from Plan

### 1. [Rule 3 — Blocking] `SubscribeDeps` carries 13 collaborators, not 10 + `gameID`

**Found during:** Task 1, reading the `Subscribe` body.

`Subscribe` calls two `*CoreServer` methods that are **not** in this plan's 8-method cluster:

- `s.buildCharacterIdentity` (`session_identity.go:31`) — shared with `QueryStreamHistory` (08-07)
- `s.recomputeSessionLiveness` (`server.go:1738`) — shared with `Disconnect` and the lease sweep (08-05)

The matrix in 08-RESEARCH.md counts fields, so these method-level edges were invisible to it.
Neither can be moved (they belong to other clusters) and neither can be duplicated.

**Fix:** injected as narrow named function types — `SessionIdentityBuilder` and
`SessionLivenessRecomputer`. `NewCoreServer` passes the method values `s.buildCharacterIdentity`
and `s.recomputeSessionLiveness`. The handler sees plain `func` types and never learns `CoreServer`
exists, so D-02 holds in substance: the proof test supplies neither and exercises the unit fine.
Both are nil-guarded (`buildSessionIdentity` returns the zero identity, matching
`buildCharacterIdentity`'s own unwired path; a nil recomputer is a no-op).

### 2. [Rule 3 — Blocking] `toSubject` split into a free function plus a one-line method

**Found during:** Task 1, first `task build` after excision.

`CoreServer.emitCommandResponse` (the *command* cluster, 08-05) also called `s.toSubject`.
Deleting the method broke the build.

**Fix:** the verbatim body became the package-level `qualifyStreamSubject(gameID, streamName)`;
`(h *SubscribeHandler).toSubject` delegates to it in one line, and `emitCommandResponse` calls it
directly. This is the only body that is not byte-identical, and it is a pure extraction — no
behavior, error code, or wrapping changed. The alternative (duplicating a 6-line pure helper)
was worse.

### 3. [Rule 3 — Blocking] Task 1 and Task 2 merged into one commit

The plan splits "move the methods" (Task 1) from "add the field and delegate" (Task 2). Doing
these as separate commits leaves an intermediate commit where `server.go` does not compile,
breaking `git bisect` — a failure mode this repo's plan-review learnings call out explicitly.
Both landed in `446498105`.

### 4. [Rule 3 — Blocking] In-package test call sites rewired

`subscribe_loop_test.go`, `server_helpers_test.go`, and `subscribe_server_test.go` (all
`package grpc`, all **outside** `test/integration/`) called the moved methods on `&CoreServer{}`
literals. Rewired mechanically:

- Tests of the moved helpers now construct `NewSubscribeHandler(SubscribeDeps{...})` — field
  renames only.
- Tests that deliberately exercise the `CoreServer.Subscribe` *facade* keep their `&CoreServer{}`
  literal and add one line, `s.subscribeHandler = s.newSubscribeHandler()`, because they bypass
  `NewCoreServer`. This motivated extracting `newSubscribeHandler()` as a real method so
  production and tests build the handler through the same code path.

**No assertion was edited** — only constructors and receivers. D-15 governs `test/integration/`,
which is untouched.

### 5. [Rule 1 — Bug] Invariant registry provenance was stale after the move

**Found during:** Task 3 — `task test:int` failed `TestProvenanceGuard`:

```
INV-STORE-6: canonical token absent at recorded site internal/grpc/server.go
INV-STORE-7: canonical token absent at recorded site internal/grpc/server.go
INV-SCENE-24: canonical token absent at recorded site internal/grpc/server.go
```

The three tokens moved with their comment blocks into `subscribe_handler.go`, leaving the
registry's `refs:` and the `INV-STORE` / `INV-SCENE` `shared_files` allowlists pointing at a file
that no longer carries them.

**Fix:** repointed the 3 refs and 2 allowlist entries in `docs/architecture/invariants.yaml`,
regenerated via `go run ./cmd/inv-render`. No invariant text, `binding`, or `asserted_by` changed
— provenance only.

**This is the D-17 gate paying for itself.** `task test` was green across the whole tree while
this was red; the unit suite does not compile `//go:build integration` files, so only
`task test:int` could see it.

### 6. Task 2 was a no-op

Its two substantive edits (the `subscribeHandler` field, the delegation) shipped in Task 1's
commit per deviation 3. Its remaining instruction — delete now-unused `CoreServer` fields — has
an empty result set, and that is correct rather than a miss:

| Field | Remaining production reads |
| --- | --- |
| `streamRegistry` | `WithStreamRegistry` setter + `newSubscribeHandler` |
| `subscriber` | `WithSubscriber` setter + `newSubscribeHandler` |
| `verbRegistry` | `WithVerbRegistry` setter + `newSubscribeHandler` |

These three are now pure routing slots, but they cannot be deleted: `CoreServerOption` is
`func(*CoreServer)` and D-04 pins that signature, so an option must have a struct field to write
into between the option loop and `newSubscribeHandler()`. Every other candidate
(`streamContributor`, `sceneMute`, `worldQuerier`, `focusCoordinator`, `identityRegistry`) is
additionally still read by an un-extracted cluster. The plan anticipated this ("Expect few or no
deletions"). No commit was manufactured for an empty change.

## Shared collaborators

`focusCoordinator`, `streamContributor`, `identityRegistry`, and `worldQuerier` are passed as the
**same value** into both `CoreServer` and `SubscribeHandler` — no copying, wrapping, or
re-derivation. Each is a read-only interface or emitter, never shared mutable state, so there is
no coordination problem. `focus.Coordinator` reaches the handler through the `internal/grpc/focus`
alias, so it is the identical type the plugin tree uses post-08-01.

## Verification

| Gate | Exit |
| --- | --- |
| `task build` | 0 |
| `task test -- ./internal/grpc/...` | 0 (514 tests) |
| `task test -- ./internal/grpc/... ./cmd/holomush/... ./internal/testsupport/...` | 0 (1161 tests) |
| `task lint` | 0 |
| `task test:int` | 0 (**10722 tests**, 7 skipped/quarantined) |
| `task fmt` | no residual diff |

Named downstream suites, all green:

```
✓  test/integration/auth (3.82s)
✓  test/integration/streams (18.704s)
✓  test/integration/list_session_streams (387ms)
✓  test/integration/scenes (1m35.848s)
```

`git diff --stat origin/main...HEAD -- test/integration/` → **empty**.

## Known Stubs

None. `SubscribeHandler` is fully wired; every collaborator is either populated by `NewCoreServer`
or intentionally nil with documented, test-pinned semantics.

## Threat Flags

None. No new network endpoint, auth path, file-access pattern, or schema change was introduced —
this plan relocated existing code and its existing gates.

## Notes for Wave C

- `server.go` is **1154 LoC** post-plan (from 1891). Two more clusters (08-05, 08-07) still have
  to come out before the ratchet ceiling is set.
- `subscribe_handler.go` is ~975 LoC. It is one cluster and its comment density is high
  (the verbatim-moved bodies carry substantial security commentary), but it is a candidate for its
  own ceiling entry.
- `replayCompleteFrame`, `streamClosedFrame`, `subscribeSessionNotFound`, and `filterSetToSlice`
  are now used only by `subscribe_handler.go` in production but still live in `server.go`.
  Relocating them is deferred (logged in `deferred-items.md`) — out of this plan's Task 2 scope.
- `export_test.go` now exists in `internal/grpc`. Later plans extracting the command, lifecycle,
  and query clusters should extend it rather than exporting production API.

## Self-Check: PASSED

Created files exist:
- `internal/grpc/subscribe_handler.go` — FOUND
- `internal/grpc/subscribe_handler_test.go` — FOUND
- `internal/grpc/export_test.go` — FOUND

Commits exist on `gsd/phase-08-god-object-decomposition`:
- `446498105` refactor(08-03): extract SubscribeHandler from CoreServer — FOUND
- `70cbf12e9` fix(08-03): put ctx first in the SubscribeHandler export shims — FOUND
- `7b1dcff9e` docs(08-03): repoint INV-STORE-6/7 and INV-SCENE-24 — FOUND
