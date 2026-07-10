<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# HoloMUSH Development Guide

This document provides instructions for AI coding assistants working on HoloMUSH.

> **`AGENTS.md` is a relative symlink to this file** — single source of truth, so the two AI-tooling entry points (Claude Code reads `CLAUDE.md`; OpenAI/Codex reads `AGENTS.md`) cannot drift. Always edit `CLAUDE.md`. `task lint:docs-symmetry` enforces the symlink integrity (`holomush-f7t2`).

## Project Overview

HoloMUSH is a modern MUSH platform: Go core with event-oriented architecture, dual protocol (telnet + web), plugin system (Lua/gopher-lua, binary/hashicorp/go-plugin, setting), PostgreSQL for all data, SvelteKit PWA web client.

**Architecture Reference**: [docs/plans/2026-01-18-holomush-roadmap-design.md](docs/plans/2026-01-18-holomush-roadmap-design.md) · **EventBus Design**: [docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md](docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md)

---

## Documentation Structure

`site/src/content/docs/` is the public Astro-Starlight website, by audience: `guide/` (players/designers), `operating/` (server operators), `extending/` (plugin devs), `contributing/` (codebase contributors), `reference/` (auto-generated API/event refs). Internal contributor docs: `docs/roadmap.md` (strategic themes), `docs/plans/` + `docs/superpowers/plans/` (plans), `docs/specs/` + `docs/superpowers/specs/` (specs); the `docs/superpowers/` subdirs are AI-tooling-generated and equally valid.

**Branding:** software brand (logo, favicon, palette) defined in `.claude/rules/branding.md` + `site/CLAUDE.md` — cyan tile + `>holomush_` wordmark, amber cursor only.

**Build:** `task docs:setup`/`docs:serve`/`docs:build`. Sandbox ops (`game.holomush.dev`): [sandbox-operations.md](site/src/content/docs/operating/how-to/sandbox/sandbox-operations.md), [sandbox-restore.md](site/src/content/docs/operating/how-to/sandbox/sandbox-restore.md).

---

## ⚠️ Protected Branch Policy

**`main` is a protected branch** — no direct commits.

| Requirement                        | Description                                         |
| ---------------------------------- | --------------------------------------------------- |
| **MUST** create feature branch     | All work happens on feature branches, not main      |
| **MUST** submit PR for review      | All changes to main require a pull request          |
| **MUST** pass CI checks            | Tests and linting must pass before merge            |
| **MUST** use squash merge          | All PRs are squash merged to maintain clean history |
| **MUST NOT** push directly to main | Branch protection enforces this                     |

---

## Development Principles

### Test-Driven Development

Tests MUST be written before implementation and MUST pass before a task is complete. Use table-driven tests; mock external dependencies (database, network).

### Spec-Driven Development

Work MUST NOT start without a spec/design/plan. Specs live in `docs/specs/` or `docs/superpowers/specs/`; plans in `docs/plans/` or `docs/superpowers/plans/` (the `docs/superpowers/` subdirs are AI-tooling and equally valid). All specs and plans MUST use RFC2119 keywords. When a spec introduces or changes a **system-level invariant**, capture it in the registry (`docs/architecture/invariants.yaml`), consulting existing entries first (`.claude/rules/invariants.md`) — do NOT mint ad-hoc invariant families.

### RFC2119 Keywords

| Keyword | Meaning |
| --- | --- |
| **MUST** | Absolute requirement |
| **MUST NOT** | Absolute prohibition |
| **SHOULD** | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended |
| **MAY** | Optional |

## Workflow

Planning and execution run on **GSD** (`/gsd-*` commands; artifacts in `.planning/`). Issue tracking lives in **GitHub Issues** (`gh issue`; always pass `-R holomush/holomush` from a worktree). The two are complementary: GSD owns the phase loop, its artifacts, and the strategic backlog (`.planning/ROADMAP.md` → `## Backlog`, 999.x entries); GitHub Issues own discrete work items, bugs, and their labels.

### GSD-native loop (multi-task work)

| Stage | Command / Action                          | Gate before next stage                         |
| ----- | ----------------------------------------- | ---------------------------------------------- |
| 1     | `/gsd-new-project` / `/gsd-new-milestone` → roadmap | roadmap reviewed                       |
| 2     | `/gsd-discuss-phase`                       | gray areas surfaced + captured                 |
| 3     | `/gsd-plan-phase`                          | `gsd-plan-checker` — plan achieves phase goal  |
| 4     | `/gsd-execute-phase`                       | `gsd-verifier` — phase goal met                |
| 5     | domain gates (when applicable)             | `crypto-reviewer` / `abac-reviewer` — READY before push |
| 6     | `/gsd-ship` → `gh pr create`               | `task pr-prep` green; `/autofix <PR#>` for CodeRabbit |

Settings: `/gsd-config` (`--advanced`, `--integrations`, `--profile <name>`); toggles via `/gsd-settings`. **Skip the loop** for small fixes (typo, dependency bump, single-file bug) — `/gsd-quick` (atomic-commit guarantees) or `/gsd-fast` (trivial), or a direct issue → implementation → review → PR.

### Code review

Primary code review is **`/gsd-code-review`** (GSD's `gsd-code-reviewer`). Two domain-specialized adversarial gates layer on top for their surfaces (see `## Pre-Push Review Gates`).

| Requirement                     | Description                                                   |
| ------------------------------- | ------------------------------------------------------------ |
| **MUST** review before merge    | Run `/gsd-code-review` over the phase's changed files        |
| **MUST** run domain gates       | `crypto-reviewer` / `abac-reviewer` when their surfaces are touched |
| **MUST** address all findings   | Fix issues or document why not applicable                    |
| **MUST NOT** skip review        | Even for "simple" changes                                    |

**Responding to PR review comments:** address **every** thread, not just CodeRabbit's; after `/autofix`, check other reviewers (`octopus-fzymgc` bot, humans). Reply to **each thread individually** (fixed / won't-fix / deferred-issue link) so each resolves — a summary comment does **not** resolve individual threads.

### Issue tracking (GitHub Issues)

GitHub Issues track discrete work items independently of GSD's phase artifacts. File discovered work with `gh issue create -R holomush/holomush` (labels: `bug`/`enhancement`, `priority::critical|high|medium|low`, topical + `theme:*`). Strategic not-yet-scheduled clusters live in the ROADMAP `## Backlog` (promote with `/gsd-review-backlog`), not as issues. The **beads (bd) tracker was retired 2026-07-09**: the full export, triage verdicts, and bead-id → issue mapping live in `.planning/archive/beads/`; historical `holomush-xxxx` ids in docs/commits resolve there.

## Strategic Themes

Multi-epic clusters use `theme:<slug>` GitHub issue labels + a narrative section in [`docs/roadmap.md`](docs/roadmap.md) (the **why**: substrate-and-uses framing, sequencing, risks).

| Requirement                       | Description                                                                                                                                  |
| --------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| **SHOULD** add a theme            | When 2+ epics or a 5+ issue cluster share a strategic frame (e.g., `theme:social-spaces` covers scenes + channels + forums + discord)        |
| **MUST** keep `docs/roadmap.md` current | When adding a `theme:*` label to any issue, also add or update the section in `docs/roadmap.md`; when a theme's work completes, move the section to "Completed themes" with a date |
| **MUST NOT** orphan labels        | If a `theme:*` label exists with no narrative section in `docs/roadmap.md`, either add the section or drop the label                         |
| **SHOULD** capture an ADR         | Record the theme framing as an ADR in `docs/adr/` alongside the roadmap edit; the ADR carries enduring rationale                             |
| **SHOULD** refresh after pivots   | After major architectural pivots or audit cleanups, re-read active themes and verify they still match reality; demote/retire stale ones      |
| **MUST NOT** use GitHub Projects   | Until team size or external visibility makes the double-entry cost worthwhile; issue labels + roadmap doc is the project-management surface  |

## Pre-Push Review Gates

GSD's own gates run inside the loop: `gsd-plan-checker` (plan quality, before execute) and `gsd-verifier` (phase-goal achievement, after execute). Two **domain-specialized** adversarial sub-agents layer on top BEFORE the PR surface — GSD has no equivalent for these. Agent/command/memory locations: [pr-guide](site/src/content/docs/contributing/how-to/pr-guide.md).

| Agent             | Fires before                                                                           | Invocation                              |
| ----------------- | -------------------------------------------------------------------------------------- | --------------------------------------- |
| `crypto-reviewer` | `git push` / PR creation, for changes touching `internal/eventbus/crypto/`, `internal/eventbus/codec/`, `internal/eventbus/history/dispatcher.go`, `internal/eventbus/history/cold_postgres.go`, `internal/plugin/event_emitter.go::Emit`, `internal/eventbus/audit/projection.go`, plugin manifest `crypto.emits` declarations, or migrations on `crypto_keys` / `events_audit` | `/holomush-dev:review-crypto` or auto via `remind-pre-action-review.sh` |
| `abac-reviewer`   | `git push` / PR creation, for changes touching `internal/access/`                      | `/holomush-dev:review-abac` or auto via `remind-pre-action-review.sh` |

| Requirement                         | Description                                                                             |
| ----------------------------------- | --------------------------------------------------------------------------------------- |
| **MUST** produce grounded findings  | Every finding cites `path:line` for code, `section` for docs, or a verified external source |
| **MUST** produce a binary verdict   | READY or NOT READY — no "mostly ready with minor concerns"                              |
| **MUST NOT** apply fixes            | Read-only by construction (`permissionMode: plan` + explicit tool allowlist)            |
| **MAY** be skipped                  | Only with explicit justification in the commit message or PR description                |

## Code Conventions

### Invariant Registry

Named system invariants live in one place: `docs/architecture/invariants.yaml` (source of truth) → `docs/architecture/invariants.md` (generated by `go run ./cmd/inv-render`, never hand-edited). A test proves an invariant via a `// Verifies: INV-<SCOPE>-N` annotation (flips `binding: pending` → `bound`). Defining, respecting, and binding invariants are governed by `.claude/rules/invariants.md` (auto-loads); guarded by `test/meta/invariant_registry_test.go`. **Never** fabricate a binding on a test that doesn't assert the invariant.

### Random Number Generation

Always use `crypto/rand`, never `math/rand`. For slice picks use a `crypto/rand`+`math/big` helper (`internal/naming.cryptoIntN(n)` is canonical).

### ULID Generation

Two ULID generators; the choice matters for correctness:

| Use case | Generator | Why |
| --- | --- | --- |
| **Event IDs** (`core.Event.ID`), session IDs | `core.NewULID()` | Identity/dedup key (set as `Nats-Msg-Id` for JetStream dedup, stable across rebuilds). Ordering is JetStream's per-stream `uint64` seq — **not** ULID lex order. |
| **Entity primary keys** (players, locations, characters, exits, objects, policies) | `idgen.New()` | Identity, not ordering; fresh `crypto/rand` entropy per call. |

`core.Event{}` struct literals MUST use `core.NewEvent()` rather than a raw literal — `NewEvent()` stamps a monotonic ULID via `core.NewULID()`. Never supply `Event.ID` manually (e.g., from `idgen.New()`).

### Error Handling

Use `oops` for structured errors (`oops.With(k,v).Wrap(err)`, `oops.Errorf(...)`, `oops.Code("CODE").Wrap(err)` at boundaries); log with `errutil.LogError`/`LogErrorContext`; test with `errutil.AssertErrorCode`/`AssertErrorContext`. **Method-value gotcha:** always call accessor methods with `()` (e.g. `decision.Reason()`) — without parens Go makes a method value that compiles silently in `...any` params (`oops.With`, `slog`).

### Structured Logging

| Requirement | Description |
| ----------- | ----------- |
| **MUST** use context-carrying variants | `slog.InfoContext(ctx, …)` / `WarnContext` / `ErrorContext` / `DebugContext` and `errutil.LogErrorContext(ctx, …)` — **never** bare `slog.Info(…)` / `logger.Warn(…)` / `errutil.LogError(…)` — whenever a `context.Context` is in scope |
| **MUST NOT** drop the context | If a `ctx` is reachable (parameter, struct field, or derivable), it MUST be threaded into the log call |
| **MAY** use bare variants | Only when no `ctx` exists *and* one cannot reasonably be plumbed (init/`main`, bare goroutines, pure helpers with no caller context) — this is the "absolutely impossible" carve-out |

**Why:** trace context (`trace_id`/`span_id`) lives on the `context.Context`; only `*Context` variants propagate it into OpenTelemetry/Loki/Grafana/Sentry, so bare calls orphan the log line. Full rationale + `sloglint` `context: scope` enforcement: [logging.md](.claude/rules/logging.md).

### Database Migrations

`internal/store/migrations/`, embedded at compile time. Sequential numbering, paired `.up.sql` + `.down.sql`, idempotent (`IF NOT EXISTS`), no triggers/functions (all logic in Go). Full guide: [database-migrations.md](site/src/content/docs/contributing/how-to/database-migrations.md).

### License Headers

| Requirement                         | Description                                         |
| ----------------------------------- | --------------------------------------------------- |
| **MUST** include SPDX header        | `.go`, `.sh`, `.proto` files (Apache-2.0)           |
| **SHOULD** include SPDX header      | YAML configs where appropriate                      |
| **MUST NOT** add to generated files | Skip `*.pb.go`                                      |

Applied by `task fmt` (via `license-eye`; `task license:check` / CI verify). Directories checked: `api/`, `cmd/`, `internal/`, `pkg/`, `plugins/`, `scripts/`.

### Proto Doc Comments

Every proto element needs a Go-grounded leading comment (no name-echo); enforced by buf `COMMENTS` + name-echo gate. Guide: `.claude/rules/proto-doc-comments.md` (auto-loads on `api/proto/**`).

## Testing

Detail in `.claude/rules/testing.md` (auto-loads on test files): coverage, ACE naming, table-driven patterns, assertions, mockery, ginkgo/gomega, EventBus harness, ABAC engines.

| Always-on rule                                    | Description                                                                                                              |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| **MUST** write tests before impl                  | TDD — write the test first. GSD `tdd_mode` enforces RED/GREEN/REFACTOR gates when enabled (toggle via `/gsd-settings`) |
| **MUST** maintain >80% coverage                   | Per-package; verify with `task test:cover`                                                                               |
| **MUST** use Ginkgo/Gomega for full-stack integration tests | Build tag `//go:build integration`; runs via `task test:int`                                               |
| **MUST** run `task test:int` on refactors         | `task test` does NOT compile integration files — refactors of shared types break silently otherwise                      |
| **MUST NOT** import `eventbustest`/`coretest`/`natstest` in production code | Production code MUST NOT import `eventbustest`/`coretest`/`natstest` (depguard-enforced); embedded NATS is correct at every test tier EXCEPT external-mode-specific behavior (external dial/fail-closed boot, single-principal scoping, multi-node per-replica invalidation, DLQ against a real broker), which MUST use a real NATS container via `internal/testsupport/natstest` |

Session-store tests need Docker even under `task test` — MUST use `sessiontest.NewStore(t)`; the deliberate SharedPostgres exception. Details: [integration-tests how-to](site/src/content/docs/contributing/how-to/integration-tests.md).

### Integration test harness (`internal/testsupport/integrationtest`)

`internal/testsupport/integrationtest/` is the canonical in-process stack (Postgres + embedded NATS JetStream + production `CoreServer`) for integration tests; build-tag-gated `//go:build integration`. When-to-use, helpers, whole-system tier, `DenyAllEngine` denial-path: [integration-tests how-to](site/src/content/docs/contributing/how-to/integration-tests.md).

## Commands

### Task Commands (Required)

**MUST use `task` for all build, test, lint, and format operations.** Do NOT run `go build`, `go test`, `golangci-lint`, etc. directly.

```bash
task lint      # core loop: lint,
task fmt       #   format,
task test      #   unit tests,
task build     #   build,
task dev       #   dev server
task plugin:build-all                             # compile all binary plugins (linux/amd64)
task plugin:build -- core-scenes                  # one binary plugin
task test -- ./internal/command/                  # scope to a package
task test -- -run TestCapability ./internal/command/   # scope to a test
task test:verbose -- ./internal/command/          # full verbose output
task test:int                                     # integration tests (needs Docker)
```

| Requirement                            | Description                                       |
| -------------------------------------- | ------------------------------------------------- |
| **MUST** use `task`                    | Never run Go/lint/fmt commands directly            |
| **MUST** run `task test`               | Before claiming any implementation is complete     |
| **MUST** run `task lint`               | Before committing changes                          |
| **MUST NOT** disable lint/format rules | Without explicit user confirmation                 |
| **SHOULD** run `task fmt`              | Before committing to ensure consistent formatting  |
| **MUST** delegate verbose task runs | Dispatch `local-check` for `task test\|lint\|build\|test:int\|test:cover` (and `local-pr-prep` for pr-prep iteration) instead of inline Bash — hook-enforced; `# offload-exempt` overrides when raw output is genuinely needed |
| **MUST** run final gate inline      | A `local-check` PASS satisfies "run `task test` before claiming complete"; the FINAL `task pr-prep` before a push still runs inline in the parent |

> **`task fmt` mutates files** (SPDX headers, reflowed tables) — **commit those edits**. Uncommitted `fmt` output is a common cause of red CI (`license:check` / markdown lint) on an otherwise-green PR. Editing an aligned Go `const`/`var`/`struct` block (inserting a longer name or a mid-block comment) can pass `task build` + unit tests yet fail `task fmt:check` in CI — run `task fmt` after touching any aligned Go block.

**MUST** run `task pr-prep` (fast lane) before creating a PR / pushing a PR branch; docs-only diffs auto-delegate to `task pr-prep:docs`. `task pr-prep:full` (opt-in; `HOLOMUSH_PR_PREP_FORCE_FULL=1` forces) adds integration + E2E in Docker. `Integration Test` + `E2E Test` are required CI checks protecting `main`. Lanes + lock/contention: [pr-prep how-to](site/src/content/docs/contributing/how-to/pr-prep.md).

**Reading the result:** exit code first — go-task collapses failures to 201; contention vs failure by BEHAVIOR, never the lock string; authoritative verdict in the `▸ pr-prep result:` file. Full guide: [pr-prep how-to](site/src/content/docs/contributing/how-to/pr-prep.md).

### Generated code

Some generated output is committed; regenerate + commit it in the **same change** or CI fails a stale-diff check:

| After changing         | Run                               | Commit                                 |
| ---------------------- | --------------------------------- | -------------------------------------- |
| `api/proto/**` schemas | `task proto && task web:generate` | `pkg/proto/**/*.pb.go` + web `*_pb.ts` |

`task lint:proto` MUST be green after any proto change.

### Session isolation

Concurrent AI sessions MUST work in separate **git worktrees** so uncommitted edits never collide. Full guide (creation, cleanup, `gh -R`): [sessions how-to](site/src/content/docs/contributing/how-to/sessions.md).

| Requirement | Description |
|---|---|
| **MUST** isolate per session | `task workspace:new -- <name>` creates a git worktree at `<repo-parent>/.worktrees/<name>` (branched off `main@origin`) and prints its path; then `cd <printed-path>`. Raw equivalent: `git worktree add -b <branch> ../.worktrees/<name> origin/main`. |
| **MUST NOT** edit files in the primary worktree | The main checkout is for **read-only inspection only** (search, reads, answering questions). A `SessionStart` hook flags any session that starts there. If you intend to edit, isolate **first** (row above) before touching any file. |
| **MUST** clean up post-merge | After landing: `cd <repo-root> && git worktree remove <repo-parent>/.worktrees/<name>` (add `--force` if it holds throwaway artifacts), then `git branch -d <branch>`. The `cd` matters — `../.worktrees/<name>` is unsafe from any nested cwd. |

Sub-agents inherit the parent's worktree; the parent MUST NOT dispatch parallel `Task` calls that edit the same files. `gh` from a worktree: always pass `-R holomush/holomush`.

### Required session-start skills

`dev-flow:grepping` MUST be loaded via the `Skill` tool **before your first response** in any session (enforced by a `SessionStart` hook): the search-tool ladder (`mcp__probe__*` Go symbol/AST → `rg` text → `ast-grep` structural) prevents defaulting to bare `grep`/full-file reads. Pairs with `.claude/rules/search-tools.md`.

VCS is **native git** (no jj): use `git` directly — no VCS skill is required.

## Reference

- **Directory structure**: `tree -L 2` / `ls`; contributor layout in `site/src/content/docs/contributing/`.
- **Auto-loading `.claude/rules/`** (load on their paths): `event-interfaces.md` (`EventBus`/`ServiceRegistry`/`ServiceProvider`, eventbus/plugin code); `gateway-boundary.md` (gateway); `terminology.md` (`*.md` + domain code); `invariants.md` (invariant registry, its tooling, specs).

## Core Systems

Full architecture map (world model, plugin host, event bus, sessions, access control, data flow, plugin manifest/registry): [architecture explanation](site/src/content/docs/contributing/explanation/architecture.md). MUST-bearing essentials:

- **Plugin runtime symmetry** — Binary and Lua plugins MUST be treated identically by the host (every host-side trust check applies to both); `.claude/rules/plugin-runtime-symmetry.md`.
- **Command authorization** — two layers at dispatch: `engine.Evaluate(subject,"execute","command:<name>")` then `engine.CanPerformAction(subject,action,resource,scope)` per capability (`ScopeSelf`/`ScopeLocal`/`ScopeGlobal`).
- **HTTP middleware** — wrappers of `http.ResponseWriter` MUST implement `http.Flusher` + `Unwrap()` (ConnectRPC streaming calls `Flush()` per frame).
- **Event sourcing** — actions produce immutable ordered events; state derives from replay. **Access control** (`internal/access`) is ABAC, default-deny. Web client: SvelteKit patterns in `web/CLAUDE.md`.

## Landing the Plane (Session Completion)

Work is NOT complete until `git push` succeeds and a PR is open. Full checklist: `.claude/rules/landing-the-plane.md` (always loaded). Skip the loop only for small fixes (typo, dependency bump, single-file bug). **Pre-push rebase:** `git fetch origin && git rebase origin/main`, resolve conflicts, re-run `task pr-prep`, then `git push -u origin <branch>`.
