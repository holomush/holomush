# Phase 09 — Deferred Items

Out-of-scope discoveries logged during execution. Not fixed here (see the
scope-boundary rule: only auto-fix issues directly caused by the current
task's changes).

## `test:e2e:cover` deferred teardown never runs (pre-existing, found during 09-01 Task 1)

**Where:** `Taskfile.yaml`, the `test:e2e:cover` task.

```yaml
- defer: {docker compose -p holomush-e2e -f compose.yaml -f compose.e2e.yaml -f compose.e2e.cover.yaml down -v; rm -rf .coverdata;: ''}
```

**Observed:** after a fully successful `task test:e2e:cover` run on 2026-07-26,
`docker compose -p holomush-e2e ps -a` still reported `core:exited`,
`gateway:exited`, `postgres:running`, and `.coverdata/` was still on disk. The
teardown produced no output in the run log (no `Removed` lines), whereas an
explicit `docker compose ... down -v` immediately afterwards did.

**Suspected cause (NOT root-caused):** the value is a YAML *flow mapping*, so it
parses as a map whose single key is the command string and whose value is `''`.
go-task appears to accept and silently ignore it rather than treating it as a
`defer:` command string. This is the "silently dropped block" class already
recorded in `.claude/rules/references/design-review-learnings.md` for per-`cmd:`
`vars:`.

**Impact:** the next `task test:e2e:cover` invocation aborts on its own
already-running precheck, and `.coverdata/` accumulates between runs. It did
block a verification run during 09-01 and was worked around by an explicit
`down -v`. It does not affect CI, which runs on a fresh runner.

**Why not fixed here:** 09-01 Task 1's authorized action is the coverage-flush
repair and the three conversion guards. Changing `defer:` semantics is a
separate behavioural change to the same task and is not caused by this plan's
edits. Fix candidate: a plain string form
(`- defer: docker compose ... down -v && rm -rf .coverdata`) plus a bats
assertion that the teardown actually ran.

## `LocationProvider.ResolveResource` doc comment still prescribes sentinels (found during 09-03 Task 1)

**Where:** `internal/access/policy/attribute/location.go:40-42`, in the method
doc comment covering the `location:*` wildcard bypass (holomush-g776):

> If a future seed adds a `when` clause comparing `resource.location.X` and is
> expected to match the wildcard path, the provider **MUST populate sentinel
> values** for X (or the seed MUST narrow its target via `resource ==`).

**Observed:** 09-03 removed seven empty-string sentinels from this very file
because ADR holomush-ti1b forbids them. This comment, on a *different* code path
(the wildcard bypass, which returns `(nil, nil)` — no bag at all), still
instructs a future contributor to reintroduce exactly the pattern that was just
removed. The second half of the sentence ("or the seed MUST narrow its target")
remains correct.

**Impact:** documentation-only today — no code follows the instruction. The
hazard is that a future contributor implementing a wildcard-matching seed reads
it as sanction for the fail-open form, reopening issue #4793 on a path the
regression test added in 09-03 Task 2 does not cover (that test exercises the
resolved-ULID path, not the wildcard bypass).

**Why not fixed here:** 09-03's authorized scope is the seven `else`-branch
sentinel assignments; the wildcard bypass is a separate design decision
(holomush-g776) with its own rationale, and rewriting security guidance outside
the plan's scope without the `abac-reviewer` gate's input is the wrong order of
operations. **Surfaced deliberately for `abac-reviewer`** — see the 09-03
SUMMARY. Fix candidate: replace "MUST populate sentinel values for X" with
"the seed MUST narrow its target via `resource ==` or gate on the `has_X`
witness — populating a sentinel is forbidden by ADR holomush-ti1b".

## From 09-20 (QUAL-04 harness seams)

- **`TestProjectionCapturesPoisonToDLQAfterMaxDeliver` is load-dependent flaky.**
  `internal/eventbus/audit/dlq_capture_integration_test.go:105` failed once during a
  full `task test:int` lane (`holomush_audit_dlq_messages_total` did not increment;
  observed delta 0, expected 1) and passed in isolation (`task test:int --
  ./internal/eventbus/audit/...`, exit 0, 174 tests).
  Attribution: NOT caused by 09-20. The package has zero dependency on the changed
  package — `rg -q 'testsupport/integrationtest' internal/eventbus/audit/` exits 1.
  Root cause appears to be a test-side race: the test polls the DLQ *stream* until a
  message lands, then immediately reads the *metric*, which the projection increments
  on a separate step — so the read can precede the increment.
  Out of scope for this plan (SCOPE BOUNDARY): pre-existing, unrelated file.
  Not quarantined (no row in `test/quarantine.yaml`), no existing issue found.

- **`Admin Authenticate Lifecycle (full-stack E2E)` / `admin_read_stream_e2e_test.go:889`
  is load-dependent flaky.** Failed once during a full `task test:int` lane
  ("F-E1: exactly one crypto.system.operator_read_completed audit row
  (INV-CRYPTO-60)", got 0 want 1, after a 10s Eventually), and passed in
  isolation (`task test:int -- ./cmd/holomush/...`, exit 0, 547 tests).
  Attribution: NOT caused by 09-20 — `rg -q 'testsupport/integrationtest' cmd/holomush/`
  exits 1, so the package has no dependency on the changed one.
  Same class as the DLQ flake above: an audit-projection `Eventually` that is
  timing-sensitive under full-lane concurrency.
  Evidence it is non-deterministic rather than a regression: three full-lane runs
  over the same tree produced one DLQ failure, one admin-e2e failure, and one
  clean pass (exit 0, 10836 tests). Out of scope (SCOPE BOUNDARY); no issue filed
  as neither reproduces in isolation.
