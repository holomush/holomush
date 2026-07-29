---
phase: 09-test-quality-code-health-sweep
plan: 07
subsystem: testsupport/integrationtest
tags: [test-harness, determinism, depguard, lint-config, eventbus]
status: complete

requires:
  - "internal/testsupport/integrationtest/session.go (existing EmitDirectEvent + QueryStreamHistoryBounded)"
  - "eventbus.NewEvent — the mandated event constructor (.claude/rules/event-conventions.md)"
provides:
  - "(*Session).EmitDirectEventAt — timestamped direct emit returning the event identifier; unblocks plans 09-12, 09-13, 09-14, 09-15"
  - "A depguard deny entry for internal/testsupport/integrationtest, pinned by the meta-test"
  - "A privacy-suite regression lock proving the chosen instant is honoured on write and on bounded read"
affects:
  - "Lint config: a fifth depguard deny entry; production code importing the harness now fails lint explicitly, not only via the build tag"
  - "test/meta/depguard_config_test.go: pinned set widened from 3 to 5 and the needle tightened"

tech-stack:
  added: []
  patterns:
    - "Caller-chosen Event.Timestamp set AFTER eventbus.NewEvent, never via a struct literal (event-conventions.md)"
    - "Negative-control falsification: break the implementation, observe the spec fail, revert"

key-files:
  created: []
  modified:
    - internal/testsupport/integrationtest/session.go
    - test/integration/privacy/privacy_test.go
    - .golangci.yaml
    - test/meta/depguard_config_test.go

decisions:
  - "Sibling method, not a variadic option on EmitDirectEvent — only a new method can also add the identifier return the floor specs need; the existing method is byte-identical (zero deleted lines), so all 36 existing call sites are untouched."
  - "`at` controls Event.Timestamp ONLY — NOT the ULID (identity/dedup, always real-clock) and NOT the JetStream sequence (which owns ordering). Publish order still decides sequence order; `at` decides time-window visibility. Stated in the doc comment so no downstream plan mistakes it for an ordering control."
  - "Smoke-spec instants are POSITIVE offsets from a base captured after connect. Back-dating below the session's arrival would be filtered by the server-side scope floor (streamScopeFloor / INV-PRIVACY-1), making the spec pass for the wrong reason."
  - "Meta-test needle tightened from the bare package path to `- pkg: <path>` (Rule 2) — a bare-path Contains is satisfied by the name appearing in a comment, so a deleted entry whose name survived in prose would have passed the pin."
  - "depguard desc cites `phase 09 QUAL-04` rather than a retired beads token, per the plan; the EmitDirectEventAt backdating detail lives in the helper's doc comment and this summary instead, keeping the desc to the surrounding entries' one-line form."
  - "QUAL-04 left Pending — this plan builds the harness seam the session-lifecycle matrix needs; 09-12/13/14/15 write the matrix itself."

metrics:
  duration: ~40m
  tasks: 3
  files: 4
  completed: 2026-07-26
---

# Phase 09 Plan 07: Timestamped Direct-Emit Harness Seam Summary

The integration harness can now place an event at a caller-chosen instant and hand back that event's identifier, so the history-floor specs in plans 09-12/13/14/15 can straddle an arrival or expiry boundary deterministically instead of separating emits with `time.Sleep`.

## Final method signature (verbatim — plans 09-12, 09-13, 09-14 call it)

```go
func (s *Session) EmitDirectEventAt(
	ctx context.Context,
	stream, evType string,
	payload []byte,
	at time.Time,
) (string, error)
```

Returns the emitted event's ULID as a string. It matches `corev1.EventFrame.GetId()` on the read side, so a spec can assert on a specific event rather than counting frames.

## What Was Built

**Task 1 — the helper** (`18c320df6`). A sibling of `EmitDirectEvent`, not a change to it. Same domain-relative stream qualification via `eventbus.Qualify`, same canonical `eventbus.NewEvent` construction, same nil-publisher guard, same production publisher (`eventbus.Subsystem.Publisher.Publish`) — so JetStream persistence and audit semantics are identical to production. The only additions are `event.Timestamp = at` after construction and the identifier return.

`git diff -U0` on the file shows **zero deleted lines**: the change is purely additive, so none of the 36 existing `EmitDirectEvent` call sites moved.

**Task 2 — the regression lock** (`e7c096848`). One Ginkgo spec appended to the privacy integration suite (which already drives the harness start and session plumbing). It emits two events at two chosen, separated instants, reads back with `QueryStreamHistoryBounded` over a window that includes the earlier and excludes the later, and asserts three things: the earlier event's returned identifier is present, the later's is absent, and the stored timestamp *is* the chosen instant (`BeTemporally("~", insideAt, time.Millisecond)`).

**Task 3 — the depguard entry** (`3b5f268cf`). Detailed below; this is where the plan's own threat model was wrong.

## The premise check the briefing asked for

Following the phase pattern (three of the six prior plans carried a falsified premise), each load-bearing plan assertion was checked rather than trusted.

| Plan assertion | Verdict |
|---|---|
| The depguard deny list does not name the harness package (Task 3's whole reason to exist) | **HELD.** Four entries: `eventbustest`, `coretest`, `quarantinetest`, `natstest`. The harness was absent. |
| The meta-test pins three of the four configured entries | **HELD.** `natstest` was configured but unpinned. |
| `EmitDirectEvent` exists and takes no timestamp | **HELD.** 38 importers, all `_test.go`. |
| The bounded query filters on the event's own `Timestamp` | **HELD.** `history/hot_jetstream.go:402` — `ev.Timestamp.After(q.NotAfter)`, inclusive by `Timestamp`. `publisher.go:194` marshals it; `hot_jetstream.go:549` decodes it back. |
| `rg -c 'time\.Sleep' test/integration/privacy/` measures added sleeps | **FALSIFIED.** See "Deviations". |
| A deliberate production import makes `task lint` fail *naming depguard* | **FALSIFIED.** See "Findings". |

## Deviations from Plan

### 1. [Rule 1 — unfalsifiable guard] The sleep-count acceptance criterion counts prose, not calls

Task 2's criterion is: *"`rg -c 'time\.Sleep' test/integration/privacy/` does not increase relative to the pre-change count."* The pre-change count was **0**. After the change it is **2** — and I added no sleep. Both matches are the phrase `time.Sleep` inside `//` comment lines explaining *why the helper exists* (it names the `#4665` backlog). A purely textual grep cannot tell a call from a mention, so the criterion fires on documentation and would equally stay silent if a sleep were introduced via an aliased import.

Replaced with a guard that distinguishes the two, and both were run:

```
rg -n 'time\.Sleep\(' test/integration/privacy/     → exit 1 (zero call sites)
rg -n 'time\.Sleep'  test/integration/privacy/     → 2 matches, both on lines beginning '//'
```

The substantive requirement — no sleep in the spec — is met. The plan's *measurement* of it was not usable.

### 2. [Rule 2 — strengthened control] Meta-test needle tightened to the YAML entry form

The plan asked only to extend the pinned list. The existing assertion was `require.Contains(t, cfg, pkg)` against the whole file text, which is satisfied by the package name appearing anywhere — including a comment. Since the test's stated purpose is to stop a deny entry being *silently deleted*, a deletion that left the name in prose would have passed it. The needle is now `"- pkg: " + pkg`, which can only match a configured entry. Both failure modes were then demonstrated (below).

### 3. [Rule 1 — formatter interaction] depguard description shortened

The first `desc:` carried the full T-09-07-01 rationale and exceeded the YAML formatter's width, so `task fmt` folded it across three continuation lines and `task lint:yaml` failed the unformatted form. Shortened to a one-liner matching the four sibling entries; the threat detail now lives in the helper's doc comment and in this summary, which is where a reader looking for it will be.

## Findings

### The depguard entry is a second line of defence, not the first — and the demonstration proves the opposite of what the plan expected

The plan required observing `task lint` fail *naming the dependency-guard rule* on a deliberate production import. It does not, and the reason matters.

Every file in `internal/testsupport/integrationtest` carries `//go:build integration`. `task lint` runs with **no build tags** (`./bin/custom-gcl run`, no `--build-tags`, nothing in `.golangci.yaml`). So a production import of the harness fails at **typecheck**, before depguard is ever consulted. Verbatim, from a deliberate `_ "…/integrationtest"` import added to `cmd/holomush/core.go`:

```
task lint EXIT=201
cmd/holomush/core.go:7:4: could not import github.com/holomush/holomush/internal/testsupport/integrationtest
  (-: build constraints exclude all Go files in internal/testsupport/integrationtest) (typecheck)
```

To show the deny entry itself is well-formed and load-bearing, the same violation was linted **with** the tag so the package type-checks:

```
$ ./bin/custom-gcl run --build-tags=integration ./cmd/holomush/...
EXIT=1
cmd/holomush/core.go:7:2: import 'github.com/holomush/holomush/internal/testsupport/integrationtest'
  is not allowed from list 'no-test-only-constructs-in-production': in-process integration harness;
  production code MUST NOT import it (phase 09 QUAL-04) (depguard)
```

Both temporary imports were reverted (`rg -c 'LOAD-BEARING' … → exit 1`, working tree clean).

The honest reading: **the build tag is the first-line control and remains so.** The deny entry is explicit, is now pinned, and survives if the tag is ever relaxed — but the plan's threat model should not claim depguard as the active gate on the default lane. It is not. This is recorded rather than papered over, because "a threat model citing a control that does not exist is worse than one citing none" is this plan's own prohibition, and the corrected claim would have been wrong in a second way.

An earlier attempt used `internal/presence/emitter.go` and produced `import cycle not allowed in test` — the harness transitively imports nearly everything, so only a top-of-tree production package (`cmd/holomush`) can host the demonstration at all.

### Importer classification (required by Task 3's acceptance criteria)

38 importers of `internal/testsupport/integrationtest`, **zero genuine violations**:

| Class | Count | Detail |
|---|---|---|
| test-support-tree exempt | 2 | `internal/testsupport/integrationtest/harness_test.go`, `harness_smoke_test.go` — doubly exempt: the rule's `files:` selector excludes `**/internal/testsupport/**`, and both are `_test.go`. |
| `_test.go` exempt | 36 | 35 under `test/integration/**` (privacy, scenes, presence, resilience, streams, wholesystem, channels, comm, plugincrypto, cursor_bounded_backfill) + `plugins/core-scenes/publish_event_emission_integration_test.go`. |
| **genuine violation** | **0** | `rg -l … --type go \| rg -v '_test\.go$'` → exit 1. |

The entry therefore landed green with no code change and no weakening of the deny form.

## Falsifiability demonstrations

Every guard this plan added was broken on purpose and observed failing.

| Guard | Injected defect | Observed |
|---|---|---|
| Task 2 smoke spec | Replaced `event.Timestamp = at` with `_ = at` (wall-clock stamping, i.e. what `NewEvent` does) | `task test:int` **EXIT=201**, failing on the exclusion assertion: *"the event emitted at 2026-07-26 18:34:03 UTC MUST NOT be readable inside a window ending at 2026-07-26 18:33:03 UTC — its presence means the caller-chosen instant was ignored and the wall clock used instead"* |
| Meta-test pin | Deleted the new deny entry | `task test -- -run TestDepguard ./test/meta/` **EXIT=201**: *"depguard deny rule for … missing from .golangci.yaml"* |
| Meta-test needle | Deleted the entry, left the package name in a YAML comment (would pass the old bare-path check) | **EXIT=201**, same message — the tightened needle rejects it |
| depguard entry | Deliberate production import | see Findings above |

`-run` was additionally checked for vacuity: `task test:verbose -- -run TestDepguard ./test/meta/` shows `=== RUN TestDepguardTestOnlyConstructRulesPresent` / `--- PASS` / `DONE 1 tests` — the pattern matches a real test, so the exit-0 is not the empty-match kind.

## Verification

| Gate | Result |
|---|---|
| `task test:int` (full lane — the only lane that compiles the harness) | **exit 0** — 10792 tests, 7 skipped |
| `task test:int -- ./test/integration/privacy/...` | **exit 0** |
| `task test` | **exit 0** — 10372 tests, 4 skipped |
| `task lint` | **exit 0** |
| `task test -- -run TestDepguard ./test/meta/` | **exit 0**, 1 test actually run |
| Plan's Task 3 verify chain (4 positive assertions + meta-test + lint, `set -o pipefail`) | **exit 0** |
| Pinned set == configured set | 5 `- pkg:` entries, 5 pinned |
| Existing `EmitDirectEvent` unchanged | `git diff -U0` → zero deleted lines |
| No struct-literal event construction | `rg -c 'eventbus\.Event\{' …/session.go` → exit 1 |
| No file deletions in any commit | `git diff --diff-filter=D` empty for all three |
| Untracked files | none |

All exit codes read directly from `$?` under `set -o pipefail`, never inferred from matching a string in stdout.

## Requirement status

**QUAL-04 left `Pending`.** It reads *"A session-lifecycle test matrix covers the connect / reconnect / multi-character / idle-timeout paths."* This plan builds the harness seam that matrix needs and nothing else — no matrix arm exists yet. Plans 09-12/13/14/15 write it. Marking it complete here would assert a property no artifact in this plan demonstrates, which is the same protocol the four prior plans followed with QUAL-05.

## Known Stubs

None. The three `TODO` strings and one `placeholder` mention that `rg` finds in the touched files (`session.go:264`, `:927`, `:931`; `privacy_test.go:29`) are all pre-existing and untouched by this plan.

## Notes for downstream plans (09-12, 09-13, 09-14, 09-15)

- `at` sets `Event.Timestamp`. It does **not** set the ULID and does **not** set the JetStream sequence. If a spec needs *ordering*, publish order is what decides it; if a spec needs *time-window visibility*, `at` is what decides it.
- Keep `at` above the session's `LocationArrivedAt` (or the relevant scope floor). `streamScopeFloor` raises the query's `NotBefore` to it, so an event placed below the floor is invisible regardless of the window — a spec that back-dates without accounting for this will pass for the wrong reason.
- Keep `at` within JetStream retention. The hot tier declines events older than `now - streamMaxAge + safetyMargin` and routes such a read to the lagging cold tier.
- The smoke spec in `test/integration/privacy/privacy_test.go` is the harness's regression lock. Delete it only if a downstream spec supersedes it with a stronger assertion on the same property.

## Self-Check: PASSED

Files:
- FOUND: `internal/testsupport/integrationtest/session.go` (contains `func (s *Session) EmitDirectEventAt`)
- FOUND: `test/integration/privacy/privacy_test.go` (contains the smoke spec)
- FOUND: `.golangci.yaml` (contains the `- pkg: …/integrationtest` deny entry)
- FOUND: `test/meta/depguard_config_test.go` (pins 5 packages)

Commits:
- FOUND: `18c320df6` — feat(09-07): add timestamped direct-emit helper to the integration harness
- FOUND: `e7c096848` — test(09-07): prove the timestamped emit lands at the caller's chosen instant
- FOUND: `3b5f268cf` — chore(09-07): deny production imports of the integration harness
