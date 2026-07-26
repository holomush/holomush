---
phase: 09-test-quality-code-health-sweep
plan: 01
subsystem: testing
tags: [coverage, codecov, docker-compose, go-cover, e2e, playwright, taskfile]

# Dependency graph
requires:
  - phase: 06-security-and-ops-hardening
    provides: "the codecov project ratchet (target: auto, threshold: 1%) that this plan re-points onto a true baseline"
provides:
  - "A working E2E binary-coverage chain: instrumented build -> container GOCOVERDIR write -> graceful-stop flush -> covdata textfmt -> coverage-e2e.out"
  - "Three guards in test:e2e:cover that make an empty or all-zero coverage profile fail the task loudly"
  - "cmd/holomush/core.go and cmd/holomush/sub_grpc.go measured rather than ignored"
  - "A codecov project baseline (78.28%) sourced from codecov's own API, with the method recorded in-file"
affects: [09-02, 09-10, 09-17, 09-19, any-plan-reading-a-coverage-number]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "stop_grace_period on coverage-overlay services so -cover binaries reach their exit hook"
    - "Coverage guards assert COVERED-statement count, never mere profile non-emptiness"

key-files:
  created:
    - .planning/phases/09-test-quality-code-health-sweep/deferred-items.md
  modified:
    - compose.e2e.cover.yaml
    - Taskfile.yaml
    - .codecov.yml

key-decisions:
  - "Root cause is the 10s docker-compose stop grace, not the bind-mount uid: a live writability probe as the runtime user returned WRITE_OK, so only the grace-period fix was applied"
  - "Guard 3 asserts a non-zero covered-statement count rather than a non-empty profile — the plan's specified emptiness guard cannot fail on the confirmed root cause"
  - "The two wiring-file ignore entries were removed in the FULL form; Task 1 succeeded, so the reduced fallback form did not apply"
  - "The codecov branch API is the only authoritative project-coverage source; `go tool cover -func` tails are explicitly disclaimed in-file"

patterns-established:
  - "Coverage-chain guard: assert what the artifact PROVES (covered statements), not what it merely looks like (bytes on disk)"
  - "Verify gates carry a negative control — the gate is run against a deliberately violating file to prove it can fail"

requirements-completed: [QUAL-02]

coverage:
  - id: D1
    description: "The instrumented E2E stack produces a coverage profile carrying real, non-zero statement counts for the core server's production wiring"
    requirement: QUAL-02
    verification:
      - kind: e2e
        ref: "task test:e2e:cover (exit 0; 104 specs passed; 9,854 covered statements)"
        status: pass
      - kind: other
        ref: "go tool cover -func=coverage-e2e.out | rg 'cmd/holomush/core.go' | rg -v '\\s0.0%$' -> 12 functions"
        status: pass
    human_judgment: false
  - id: D2
    description: "An empty, stale, malformed, or all-zero E2E coverage profile fails the task instead of passing silently"
    requirement: QUAL-02
    verification:
      - kind: other
        ref: "guard 1: stale profile planted, conversion broken, task run -> stale marker line absent, 0 bytes remain"
        status: pass
      - kind: other
        ref: "guard 2: covdata -i=<nonexistent> -> covdata exit 1 -> task exit 201 (non-zero)"
        status: pass
      - kind: other
        ref: "guard 3 predicate vs header-only (10B), meta-only (2,692,028B/32,375 lines), and good profile -> 0/0/9854 covered"
        status: pass
    human_judgment: false
  - id: D3
    description: "The .codecov.yml ignore list and project-status baseline comment describe reality"
    requirement: QUAL-02
    verification:
      - kind: other
        ref: "full-form gate: 2 positive controls + 2 negative assertions -> exit 0; negative control (re-add ignore entry) -> exit 1"
        status: pass
      - kind: other
        ref: "curl --data-binary @.codecov.yml https://codecov.io/validate -> 'Valid!'"
        status: pass
    human_judgment: false

# Metrics
duration: 55min
completed: 2026-07-26
status: complete
---

# Phase 9 Plan 01: E2E Coverage Chain Repair Summary

**The instrumented E2E containers were being SIGKILLed before Go's coverage exit hook could run, so every E2E run shipped a full-size profile in which every count was zero — fixed with `stop_grace_period`, and guarded by a covered-statement assertion because the plan's own emptiness guard could not have caught it.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-07-26T16:06Z
- **Completed:** 2026-07-26T17:01Z
- **Tasks:** 2 of 2
- **Files modified:** 3 (+1 planning artifact)

## Accomplishments

- **Root cause established with evidence, not narrowed by guesswork.** Both hypotheses were tested; one was eliminated by direct observation and only the confirmed fix was applied.
- **The E2E lane now measures the two files it was previously credited with exercising**: `cmd/holomush/core.go` at **70.1%** and `cmd/holomush/sub_grpc.go` at **66.0%**, from the E2E lane alone.
- **A latent defect in the plan's guard design was found and corrected** — see Deviations. The specified guard would have passed on the exact failure it existed to catch.
- **The falsified `~54.6%` baseline is replaced** with codecov's own 78.28%, together with the curl+jq that produced it and an in-file warning against quoting a `go tool cover -func` tail.

## Confirmed root cause (with the evidence that confirmed it)

**Hypothesis 2 — stop grace period — CONFIRMED. Hypothesis 1 — bind-mount uid mismatch — ELIMINATED.**

| Observation | Value |
|---|---|
| Container runtime user | `uid=1000(holomush) gid=1000(holomush)` |
| Host `.coverdata/{core,gateway}` owner | `uid=501 gid=20`, mode `drwxr-xr-x` |
| `/coverdata` **as the container sees it** | `drwxr-xr-x 2 1000 1000` — Docker Desktop maps bind-mount ownership |
| Live writability probe, runtime user, running container | `touch /coverdata/probe-live` → **WRITE_OK** |
| Container exit codes after default `docker compose stop` | core **137**, gateway **137**, `oomkilled=false` |
| core SIGTERM → SIGKILL interval | 16:19:30.25 → 16:19:40.26 = **exactly 10.0s** (the compose default; no `stop_grace_period` existed in any compose file) |
| `.coverdata/core` after the SIGKILLed stop | `covmeta.*` (581,968 B) only — **no `covcounters.*`** |
| Container exit codes after `docker compose stop -t 120` | core **0**, gateway **0** |
| Graceful shutdown time | core **~14.4s**, gateway **~7.6s** |
| `.coverdata/core` after the graceful stop | `covmeta.*` **plus** `covcounters.*` (10,067 B) |

The shutdown tail is the OTLP exporter's own 5s export timeout draining to an
`otel-collector` that is not up under the E2E profile
(`telemetry SDK error ... exporter export timeout ... produced zero addresses`,
logged 5.0s after the last subsystem stopped). The subsystem teardown itself
completes in ~90ms; the exporter is what pushes core past the 10s default.

**"Both were changed to be safe" was explicitly avoided.** Only
`stop_grace_period: 60s` was added. No `user:` key was introduced, so the
production image's non-root posture (`adduser -D -g '' holomush`, `USER holomush`,
the pre-chowned `/home/holomush`) is untouched — as is the coverage overlay's,
since nothing about the container user changed.

## What `covdata textfmt` actually does in each situation — observed, not assumed

This is the finding that changed the implementation. The three shapes are **not** equivalent:

| `.coverdata` contents | covdata exit | Profile produced | `test -s` | body lines | covered stmts |
|---|---|---|---|---|---|
| meta + counters (**good**) | 0 | 2,692,028 B | passes | 32,375 | **9,854** |
| **meta only** (the confirmed root cause) | **0** | **2,692,028 B** | **passes** | **32,375** | **0** |
| empty directory | 0 (with a `warning:` line) | 0 B | fails | 0 | 0 |
| malformed `covmeta.*` | **1** | 0 B | fails | 0 | 0 |

The root-cause shape yields a profile **byte-for-byte the size of a healthy one**,
with a full 32,375-line body, and covdata exits **0**. `covmeta.*` enumerates every
statement in the binary; without `covcounters.*` each is simply emitted with count 0.

## Which protection catches the confirmed root cause

**None of the three as the plan specified them — only guard 3 in its corrected form.**

| Guard | Catches |
|---|---|
| 1 — `rm -f coverage-e2e.out` **before** the run | A stale profile from an earlier, different run satisfying the later guards |
| 2 — propagate `covdata textfmt` exit status | **Malformed** coverage data (covdata exits 1, writes 0 bytes) |
| 3 — require ≥1 **covered statement** | **Both** an empty coverage-data directory *and* the confirmed SIGKILL-before-flush shape |

Guard 3 as written in the plan ("fail when the produced profile has no body, a
profile carrying only its `mode:` header line being an empty profile") would have
**passed** the root-cause shape, whose body is 32,375 lines. It was strengthened to
count body lines whose execution count is non-zero. See Deviations.

## Guard demonstrations (each performed once, then reverted)

- **Guard 1.** A stale 51-byte profile carrying a `STALE/from/an/earlier/run.go` marker line was planted, the conversion was broken (`-i=.coverdata/DELIBERATELY-BROKEN`), and the task was run. Afterwards `rg 'STALE/from/an/earlier/run' coverage-e2e.out` exits **1** — the stale content is gone. What remains is a fresh **0-byte** file created by the failing conversion itself, which guard 3 would also reject.
- **Guard 2.** Same broken run: `go tool covdata textfmt failed (exit 1)` and the task exited **201** (non-zero) even though the failure was not the suite's.
- **Guard 3.** The exact Taskfile predicate was run against a header-only profile (10 B → 0 covered → **fails the task**), the real meta-only profile captured from the SIGKILLed run (2,692,028 B, 32,375 body lines → 0 covered → **fails the task**), and the good profile (9,854 covered → passes). `test -s` passes the first two.

The deliberate break was reverted and re-verified (`rg -n 'covdata textfmt -i='` shows the original inputs restored).

## Task Commits

1. **Task 1: Diagnose and repair the E2E binary-coverage flush, end to end** — `3a2bd2f5c` (fix)
2. **Task 2: Stop ignoring the wiring files and correct the falsified baseline** — `a1fae0323` (chore)

## Files Created/Modified

- `compose.e2e.cover.yaml` — `stop_grace_period: 60s` on `core` and `gateway`, each with a comment recording the measured shutdown time and why the default 10s was fatal.
- `Taskfile.yaml` — `test:e2e:cover`: pre-run `rm -f coverage-e2e.out`; `COVDATA_EXIT` capture and propagation; a covered-statement guard with an operator-facing diagnostic pointing at exit 137 and `stop_grace_period`. Each guard carries a comment naming the failure it exists to catch.
- `.codecov.yml` — the two wiring-file `ignore:` entries and their rationale blocks removed (replaced by a short do-not-re-add note); the project-status baseline comment rewritten around the measured 78.28%.
- `.planning/phases/09-test-quality-code-health-sweep/deferred-items.md` — one out-of-scope discovery (below).

## Baseline figure for downstream plans

| Figure | Value | Source |
|---|---|---|
| Project coverage, `main` @ `497748c6d` | **78.28%** (57,480 lines, 44,997 hits, 9,343 misses, 3,140 partials, `sessions: 2`) | `curl -s https://api.codecov.io/api/v2/github/holomush/repos/holomush/branches/main/ \| jq .head_commit.totals.coverage` |
| `cmd/holomush` package, **E2E lane alone** | **37.3%** (969 / 2,597 statements) | `coverage-e2e.out` |
| `cmd/holomush/core.go`, E2E lane alone | **70.1%** (260 / 371) | `coverage-e2e.out` |
| `cmd/holomush/sub_grpc.go`, E2E lane alone | **66.0%** (188 / 285) | `coverage-e2e.out` |
| Combined statements the un-ignore adds to the denominator | **656** (448 of them covered) | `coverage-e2e.out` |

**Plan 09-10 reads the `cmd/holomush` figure as its starting baseline.** Two cautions:
the 37.3% is the **E2E lane in isolation** and will rise once merged with the unit and
integration uploads; and it is a **statement** ratio computed from the profile, whereas
codecov reports a **line** ratio over a different denominator. They are not
interchangeable. The plan's own "roughly 656 uncovered statements" estimate for the
un-ignore was exactly right on count and wrong on direction — 448 of those 656 are
covered, so the change raises the measured figure rather than lowering it.

`sessions: 2` on the codecov side is the pre-fix state and is expected to become 3 once
a CI run uploads a non-empty `e2e` flag. **That has not yet been observed** — this plan
proves the artifact locally; confirming the upload lands is 09-19's assertion.

## Decisions Made

1. **Compose-side `stop_grace_period`, not a Taskfile `stop -t`.** Scoped to the coverage overlay, so neither the dev stack nor the plain E2E lane changes behaviour. 60s against a measured ~14.4s worst case.
2. **Only the confirmed fix was applied.** The uid hypothesis was disproven on this host and left alone rather than "fixed anyway".
3. **Guard 3 asserts covered statements.** The single most important change in the plan; the specified form was provably defeatable by the confirmed root cause.
4. **Full form for Task 2.** Task 1 succeeded, so both ignore entries were removed. The reduced fallback form and its five-step escalation were **not** executed and no tracking issue was filed for the chain.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] The specified emptiness guard cannot fail on the confirmed root cause**

- **Found during:** Task 1, while characterising `covdata textfmt` per the plan's own instruction to observe rather than assume the empty and malformed situations are equivalent.
- **Issue:** The plan specified guard 3 as "fail when the produced profile has no body, a profile carrying only its `mode:` header line being an empty profile." The confirmed root cause does not produce a header-only profile. A meta-only `.coverdata` produces a **2,692,028-byte profile with 32,375 body lines** in which every count is 0, and `covdata` exits **0**. All three specified protections pass it: the pre-run `rm` is irrelevant (the file is freshly written), the exit status is 0, and the body is far from empty. The guard being built would have certified the exact breakage it was commissioned to detect — the phase's own named defect class, a verification that cannot fail.
- **Fix:** Guard 3 counts body lines whose execution count (`$NF`) is non-zero and fails when that count is 0. This strictly subsumes the specified check: a header-only profile has 0 body lines and therefore 0 covered, so the intended case is still caught. The all-zero case is now caught too. An explicit file-existence check and an awk-exit check were added ahead of it so a read failure cannot be mistaken for a pass — deliberately not the `|| echo 0` shape the plan's own verify note calls out as swallowing a failure into the passing side.
- **Files modified:** `Taskfile.yaml`
- **Verification:** The predicate was run against all four profile shapes (table above). Header-only → fails. Meta-only → fails. Good → passes. `test -s` passes the first two.
- **Committed in:** `3a2bd2f5c` (part of the Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 × Rule 2).
**Impact on plan:** The deviation strengthens the plan's central deliverable. Without it, the plan would have shipped a repaired chain plus a guard incapable of detecting that chain breaking again — the failure mode the plan describes as "the single most expensive failure available in this phase". No scope creep: the change is confined to the guard the plan asked for.

## Issues Encountered

**One out-of-scope discovery, logged not fixed.** `test:e2e:cover`'s deferred
teardown never runs: after a fully successful run, `core:exited`,
`gateway:exited`, `postgres:running` remained and `.coverdata/` was still on
disk, with no teardown output in the log. The `defer:` value is a YAML flow
mapping, which go-task appears to accept and silently ignore — the same
silently-dropped-block class already recorded in
`.claude/rules/references/design-review-learnings.md`. It blocked one
verification run here (the task's own already-running precheck aborted the
retry) and was worked around with an explicit `down -v`. It is pre-existing,
not caused by this plan's edits, and changing `defer:` semantics is outside
Task 1's authorized action, so it is recorded in
`.planning/phases/09-test-quality-code-health-sweep/deferred-items.md` with a
fix candidate. CI is unaffected — it runs on a fresh runner.

## Known Stubs

None. No stub, placeholder, skipped test, or unrun `<verify>` was introduced by
this plan. Every verification listed above was executed and its result recorded.

## Threat Flags

None. No new network endpoint, auth path, file-access pattern, or schema change
at a trust boundary. On the register: **T-09-01** (repudiation — a succeeding job
emitting an empty artifact) is now mitigated by a guard proven to fail on the
real failure shape rather than only on a header-only file. **T-09-02** (tampering
via the ignore list) is mitigated: two entries removed, none added. **T-09-03**
(elevation via a coverage-overlay container user) did not arise — the uid
hypothesis was eliminated, so no user override was introduced.

## Verification

| Check | Result |
|---|---|
| `task test:e2e:cover` | **exit 0**, 104 specs passed, 9,854 covered statements |
| `test -s coverage-e2e.out` | exit 0 (32,376 lines) |
| `cmd/holomush/core.go` functions above 0.0% | **12** (incl. `runCoreWithDeps` 78.5%) |
| `cmd/holomush/` entries in `go tool cover -func` | **197** (criterion: > 20) |
| Task 2 full-form gate | exit 0; negative control (re-added ignore entry) exit 1 |
| codecov config validator | `Valid!` |
| `task lint` | exit 0 |
| `task test` | exit 0 (10,366 tests, 4 skipped) |

Every result above is read from an **exit code** or a numeric artifact
measurement, never from matching a string in command output.

## Next Phase Readiness

The measurement chain every other QUAL-02 criterion depends on is proven working
by direct assertion on the produced artifact, and it now fails loudly if it
breaks again.

- **09-10** has its starting `cmd/holomush` baseline (with the lane and
  statement-vs-line caveats above).
- **09-19** can assert the E2E flag reports non-zero — the local artifact carries
  real counts; what remains unproven is that CI's upload lands as a third codecov
  session, which is 09-19's own assertion and not something a green job proves.
- **09-02 / 09-17** are unblocked; the tracer's stop condition did **not** fire, so
  no downstream assertion is skipped and no fallback applies.

One carried caution: the uid hypothesis was eliminated **on macOS Docker Desktop**,
which maps bind-mount ownership. A Linux CI runner does not, so if the CI `e2e`
flag is still empty after this change, the uid path is the next thing to test
there — it is disproven locally, not globally.

---
*Phase: 09-test-quality-code-health-sweep*
*Completed: 2026-07-26*

## Self-Check: PASSED

All three modified files, both planning artifacts, and all three commits
(`3a2bd2f5c`, `a1fae0323`, `8970e84c3`) verified present. Asserted against the
**committed** blobs, not the working tree: `git show HEAD:Taskfile.yaml` carries
3 guard blocks; `git show HEAD:.codecov.yml` matches `cmd/holomush` 0 times and
`78.28` once.
