---
description: Run task pr-prep to completion and surface the first failure clearly — no shortcuts, no sub-agent delegation
---

Run `task pr-prep` to full completion. This mirrors all CI jobs (lint, format,
schema, license, unit, integration, E2E). MUST run as a single command — never
approximate by running individual steps and claiming pr-prep passed.

Reason for the no-shortcut rule: in April 2026 a partial check (lint + unit
tests only) was misrepresented as "pr-prep green" and pushed broken code.
The integration tests caught a compilation error in
`test/integration/access/concurrent_engine_test.go` that unit tests couldn't
see (integration tests use `//go:build integration`). This wasted user time
and money; that's why this rule is non-negotiable.

## Procedure

1. **Run** — single invocation, foreground:
   ```bash
   task pr-prep
   ```
   Output is large; if running interactively, prefer running it in the
   background and tailing the result. The Bash tool can run with
   `run_in_background: true` and check the result file when notified.

2. **On success** — report green with the run summary line.

3. **On failure** — report:
   - **Which job failed first** (often the earliest red is the root cause)
   - **The actual error text** from that job
   - **A short suggested next step** — e.g.,
     - lint failure → `task lint` to reproduce, then fix per `.claude/rules/grpc-errors.md` if it's a wrapcheck issue, line-scoped `//nolint:<rule>` only
     - fmt failure → `task fmt` and re-run
     - schema regen failure → run the cited `go generate`, commit the regenerated file, retry
     - test failure → reproduce with `task test -- ./<package>` (or `task test:int -- ./<package>`)

4. **Anti-patterns** — never do these:
   - Run `task lint && task test` and call that "pr-prep passed"
   - Trust a sub-agent's report that pr-prep is green — verify yourself. Sub-agents can't catch schema-regeneration side-effects (e.g., `go generate` updating `schemas/plugin.schema.json`) that must be committed before the PR is current.
   - Skip pr-prep because "only docs changed". The `feedback_pr_prep` rule is "no exceptions, no shortcuts" — always run `task pr-prep` end-to-end before push.

## Scope hint

`$ARGUMENTS` may carry context (e.g., `--background`); honor it but don't substitute for the actual run.
