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
