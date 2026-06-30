---
name: pr-prep
description: Run the appropriate pr-prep lane (fast by default; full for int+e2e) and surface the first failure clearly
disable-model-invocation: true
---

Run the **fast** `task pr-prep` before push — it's the mandatory gate (schema,
license, lint, fmt, unit, build, bats; no Docker, no flock). Run
`task pr-prep:full` when your diff touches the integration / E2E surface; it runs
the full pipeline (everything fast does, plus integration + E2E) behind a
machine-global flock. Integration + E2E also run in CI as required checks
(`Integration Test`, `E2E Test`), so they gate the PR there regardless.

Whichever lane you run, run it as a single command end-to-end — never approximate
by running individual steps and claiming pr-prep passed.

Reason for the no-shortcut rule: in April 2026 a partial check (lint + unit tests
only) was misrepresented as "pr-prep green" and pushed broken code. The
integration tests caught a compilation error in
`test/integration/access/concurrent_engine_test.go` that unit tests couldn't see
(integration tests use `//go:build integration`). This wasted user time and
money; that's why running the chosen lane end-to-end is non-negotiable.

## Procedure

1. **Pick the lane:**
   - Default: `task pr-prep` (fast — mandatory before every push).
   - `task pr-prep:full` if the diff touches `test/integration/**`, `web/e2e/**`, or integration-tagged packages (recommended; the full gate is flock-serialized).
   - Docs-only diffs auto-route to the docs lane.

2. **Run** — single invocation, foreground:

   ```bash
   task pr-prep          # fast lane (mandatory)
   # or, when you touched int/e2e:
   task pr-prep:full     # full int+e2e gate (flock-serialized)
   ```

   Output is large; if running interactively, prefer the background and read the
   result file. Each run prints `▸ pr-prep result: <path>` and writes a file with
   `status=`/`lane=`/`exit=` lines — read that file (match the `▸ pr-prep result:`
   prefix) for the authoritative verdict rather than grepping stdout. Exit code
   first: `0` = pass. go-task collapses every non-zero exit to `201`, so on the
   full lane distinguish lock contention (`lane=full`, `status=contention`,
   returns in ~2s) from a real gate failure via the result file — never by a
   status substring.

3. **On success** — report green with the run summary line and the `lane=` value.

4. **On failure** — report:
   - **Which job failed first** (often the earliest red is the root cause)
   - **The actual error text** from that job
   - **A short suggested next step** — e.g.,
     - lint failure → `task lint` to reproduce, then fix; line-scoped `//nolint:<rule>` only
     - fmt failure → `task fmt` and re-run
     - schema regen failure → run the cited `go generate`, commit the regenerated file, retry
     - test failure → reproduce with `task test -- ./<package>` (or `task test:int -- ./<package>`)

5. **Anti-patterns** — never do these:
   - Run `task lint && task test` and call that "pr-prep passed"
   - Trust a sub-agent's report that pr-prep is green — verify yourself. Sub-agents can't catch schema-regeneration side-effects (e.g., `go generate` updating `schemas/plugin.schema.json`) that must be committed before the PR is current.
   - Drive a retry loop off the string `another pr-prep is running` — pr-prep's own `pr-prep-lock.bats` self-test surfaces that exact string on healthy runs; read the exit code + result file instead.

## Scope hint

`$ARGUMENTS` may carry context (e.g., `--background`); honor it but don't substitute for the actual run.
