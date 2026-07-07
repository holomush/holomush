<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# HoloMUSH Development Guide

This document provides instructions for AI coding assistants working on HoloMUSH.

> **`AGENTS.md` is a relative symlink to this file** — single source of truth, so the two AI-tooling entry points (Claude Code reads `CLAUDE.md`; OpenAI/Codex reads `AGENTS.md`) cannot drift. Always edit `CLAUDE.md`. `task lint:docs-symmetry` enforces the symlink integrity (`holomush-f7t2`).

## Project Overview

HoloMUSH is a modern MUSH platform with:

- Go core with event-oriented architecture
- Dual protocol support (telnet + web)
- Plugin system: Lua (gopher-lua), binary (hashicorp/go-plugin), and setting plugins
- PostgreSQL for all data
- SvelteKit PWA for web client

**Architecture Reference**: [docs/plans/2026-01-18-holomush-roadmap-design.md](docs/plans/2026-01-18-holomush-roadmap-design.md)

**EventBus Design**: [docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md](docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md)

---

## Documentation Structure

| Directory                | Purpose                                      | Audience                |
| ------------------------ | -------------------------------------------- | ----------------------- |
| `site/src/content/docs/` | Public documentation website (Astro Starlight) | All users             |
| `docs/roadmap.md`        | Strategic themes spanning multiple epics (substrate-and-uses framing) | Contributors (internal) |
| `docs/plans/`            | Implementation plans, in-progress work       | Contributors (internal) |
| `docs/specs/`            | Design specifications, architectural designs | Contributors (internal) |
| `docs/superpowers/plans/`| AI-generated implementation plans (superpowers skill) | Contributors (internal) |
| `docs/superpowers/specs/`| AI-generated design specifications (superpowers skill) | Contributors (internal) |

**Site documentation** (`site/src/content/docs/`) is organized by audience:

- `guide/` — For players and game designers
- `operating/` — For people running HoloMUSH servers
- `extending/` — For plugin developers building on HoloMUSH
- `contributing/` — For people contributing to the HoloMUSH codebase
- `reference/` — Auto-generated API and event references

**Branding:** The software brand (logo, favicon, palette) is defined in
`.claude/rules/branding.md` and `site/CLAUDE.md` — cyan tile + `>holomush_`
wordmark, amber cursor accent only.

**Build commands:**

```bash
task docs:setup   # Install documentation dependencies
task docs:serve   # Start local dev server
task docs:build   # Build static site
```

For sandbox operations at `game.holomush.dev`, see
[site/src/content/docs/operating/sandbox-operations.md](site/src/content/docs/operating/sandbox-operations.md)
and [site/src/content/docs/operating/sandbox-restore.md](site/src/content/docs/operating/sandbox-restore.md).

---

## ⚠️ Protected Branch Policy

**`main` is a protected branch.** Direct commits to main are not allowed.

| Requirement                        | Description                                         |
| ---------------------------------- | --------------------------------------------------- |
| **MUST** create feature branch     | All work happens on feature branches, not main      |
| **MUST** submit PR for review      | All changes to main require a pull request          |
| **MUST** pass CI checks            | Tests and linting must pass before merge            |
| **MUST** use squash merge          | All PRs are squash merged to maintain clean history |
| **MUST NOT** push directly to main | Branch protection enforces this                     |

**See:** [Pull Request Guide](site/src/content/docs/contributing/pr-guide.md) for the complete workflow.

---

## Development Principles

### Test-Driven Development

- Tests MUST be written before implementation
- Tests MUST pass before any task is complete
- Use table-driven tests for comprehensive coverage
- Mock external dependencies (database, network)

### Spec-Driven Development

- Work MUST NOT start without a spec/design/plan
- Specs live in `docs/specs/` or `docs/superpowers/specs/`
- Plans live in `docs/plans/` or `docs/superpowers/plans/`
- The `docs/superpowers/` subdirectories are used by AI tooling (superpowers skills) and are equally valid
- All specs and plans MUST use RFC2119 keywords
- When a spec introduces or changes a **system-level invariant**, capture it in the registry (`docs/architecture/invariants.yaml`) and consult existing entries first — see `.claude/rules/invariants.md`. Do NOT mint ad-hoc invariant families in spec prose.

### RFC2119 Keywords

| Keyword        | Meaning                                    |
| -------------- | ------------------------------------------ |
| **MUST**       | Absolute requirement                       |
| **MUST NOT**   | Absolute prohibition                       |
| **SHOULD**     | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended                            |
| **MAY**        | Optional                                   |

## Workflow

Work is tracked in `bd` (see `.claude/rules/beads-project.md` and `bd prime`). Specs live in `docs/specs/` or `docs/superpowers/specs/`; plans in `docs/plans/` or `docs/superpowers/plans/`.

### Stage-gated workflow (multi-task work)

| Stage | Skill / Action                                  | Gate before next stage         |
| ----- | ----------------------------------------------- | ------------------------------ |
| 1     | `dev-flow:brainstorming`                        | (conversation only)            |
| 2     | (writes spec from brainstorming)                | `design-reviewer` — READY      |
| 3     | `dev-flow:writing-plans`                        | `plan-reviewer` — READY        |
| 4     | `dev-flow:plan-to-beads` (auto-fired by writing-plans on READY; preceded by `dev-flow:capture-adrs`) | user reviews dry-run manifest before materialization |
| 5     | `dev-flow:subagent-driven-development`          | `code-reviewer` (+ `crypto-reviewer` / `abac-reviewer` when applicable) before push |
| 6     | `gh pr create`                                  | `task pr-prep` green; `/autofix <PR#>` for CodeRabbit |

Detail on each gate is in `## Pre-Push Review Gates`. Skipping requires explicit user override.

**Skip the chain** for small fixes (typo, dependency bump, single-file bug) — direct bead → implementation → review → PR is the right shape.

### Code review

All tasks MUST be reviewed before completion via `pr-review-toolkit:review-pr`. Workflow detail at [Pull Request Guide](site/src/content/docs/contributing/pr-guide.md).

| Requirement                                | Description                                          |
| ------------------------------------------ | ---------------------------------------------------- |
| **MUST** use `pr-review-toolkit:review-pr` | Launch comprehensive review using specialized agents |
| **MUST** address all findings              | Fix issues or document why not applicable            |
| **MUST NOT** skip review                   | Even for "simple" changes                            |

**Responding to PR review comments:** address **every** review thread, not just
CodeRabbit's — after `/autofix` handles CodeRabbit, check for unresolved threads
from other reviewers (the `octopus-fzymgc` bot, humans). Reply to **each thread
individually** with its outcome — fixed (what changed), won't-fix (why), or
deferred (bead id) — so each one resolves. A single summary comment does **not**
resolve the individual threads.

### Plan → bd materialization

Plans drive bd state via `dev-flow:plan-to-beads`, which reads the plan's task table (each `### Task N:` heading inside a `## Phase N:` section) and materializes the epic + child beads + dependency graph in one pass. **Plans do NOT carry a `## Bead chain structure` section** — bd is the source of truth for graph topology (per the `dev-flow:plan-to-beads` skill spec Rule 4). The ancestor `bead-chain-design` / `bead-chain-from-plan` convention is superseded.

Per Rule 3, each task bead's `--description` is **narrative only** (Goal, Plan reference, Files touched, Out of scope). Acceptance criteria, verification commands, dependencies, and labels live in their dedicated bd flags (`--acceptance`, `--deps`, `--labels`, `--skills`).

## Strategic Themes

Multi-epic work clusters are tracked via `theme:<slug>` bd labels paired with a narrative section in [`docs/roadmap.md`](docs/roadmap.md). The label provides cross-epic queryability; the doc carries the **why** (substrate-and-uses framing, sequencing rationale, risks) alongside the code.

This complements `bd` (single-task tracking) and the stage-gated workflow above (per-task lifecycle). Themes are the layer for "what cluster of work is this part of, and why now?"

| Requirement                       | Description                                                                                                                                  |
| --------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| **SHOULD** add a theme            | When 2+ epics or a 5+ bead cluster share a strategic frame (e.g., `theme:social-spaces` covers scenes + channels + forums + discord)         |
| **MUST** keep `docs/roadmap.md` current | When adding a `theme:*` label to any bead, also add or update the section in `docs/roadmap.md`; when a theme's work completes, move the section to "Completed themes" with a date |
| **MUST NOT** orphan labels        | If a `theme:*` label exists with no narrative section in `docs/roadmap.md`, either add the section or drop the label                         |
| **SHOULD** file a decision bead   | Use `bd create -t decision -l theme:<slug>` to record the framing alongside the roadmap edit; the bead carries enduring rationale            |
| **SHOULD** refresh after pivots   | After major architectural pivots or audit cleanups, re-read active themes and verify they still match reality; demote/retire stale ones      |
| **MUST NOT** use GitHub Projects   | Until team size or external visibility makes the double-entry cost worthwhile; bd labels + roadmap doc is the project-management surface     |

**Query a theme** (includes `in_progress`, not just `open`):

```bash
bd list -l theme:<slug> --limit 0 --json | jq -r '.[] | select(.status != "closed") | "\(.id) [P\(.priority)] \(.title)"'
```

Plain `bd list --status open` excludes `in_progress` beads, so use the JSON filter when surfacing active theme work.

## Pre-Push Review Gates

Three adversarial read-only sub-agents gate hand-offs BEFORE the PR surface.
These complement `pr-review-toolkit:review-pr` (which runs on the PR itself)
by providing an earlier, in-session review pass.

| Agent             | Fires before                                                                           | Invocation                              |
| ----------------- | -------------------------------------------------------------------------------------- | --------------------------------------- |
| `design-reviewer` | `dev-flow:writing-plans` is invoked on a spec                                       | `/review-design [<spec-path>]` or auto  |
| `plan-reviewer`   | `dev-flow:executing-plans` or `dev-flow:subagent-driven-development` runs a plan | `/review-plan [<plan-path>]` or auto    |
| `code-reviewer`   | `bd close`, `jj git push`, or PR creation                                              | `/holomush-dev:review-code [<target>]` or auto       |
| `crypto-reviewer` | `code-reviewer` (runs FIRST), for changes touching `internal/eventbus/crypto/`, `internal/eventbus/codec/`, `internal/eventbus/history/dispatcher.go`, `internal/eventbus/history/cold_postgres.go`, `internal/plugin/event_emitter.go::Emit`, `internal/eventbus/audit/projection.go`, plugin manifest `crypto.emits` declarations, or migrations on `crypto_keys` / `events_audit` | `/holomush-dev:review-crypto` or auto via `remind-pre-action-review.sh` |
| `abac-reviewer`   | `code-reviewer` (runs alongside), for changes touching `internal/access/`              | `/holomush-dev:review-abac` or auto via `remind-pre-action-review.sh` |

| Requirement                         | Description                                                                             |
| ----------------------------------- | --------------------------------------------------------------------------------------- |
| **MUST** produce grounded findings  | Every finding cites `path:line` for code, `section` for docs, or a verified external source |
| **MUST** produce a binary verdict   | READY or NOT READY — no "mostly ready with minor concerns"                              |
| **MUST NOT** apply fixes            | Read-only by construction (`permissionMode: plan` + explicit tool allowlist)            |
| **MAY** be skipped                  | Only with explicit justification in the commit message or PR description                |

Agent definitions live in `.claude/agents/`; slash commands in
`.claude/commands/`; persistent memory in `.claude/agent-memory/`
(checked into VCS).

## Code Conventions

### Invariant Registry

Named system invariants live in one place: `docs/architecture/invariants.yaml`
(source of truth) → `docs/architecture/invariants.md` (generated by
`go run ./cmd/inv-render`, never hand-edited). A test proves an invariant by
carrying a `// Verifies: INV-<SCOPE>-N` annotation, which flips its registry
entry from `binding: pending` to `bound`. Defining new invariants (at spec time),
respecting existing ones, and the binding workflow are governed by
`.claude/rules/invariants.md` (auto-loads on the registry, its tooling, and specs).
Guarded by `test/meta/invariant_registry_test.go`. **Never** fabricate a binding
on a test that doesn't actually assert the invariant.

### Random Number Generation

Always use `crypto/rand`, never `math/rand`. For picking from slices, use a `crypto/rand` + `math/big` helper. `internal/naming.cryptoIntN(n)` is the canonical example.

### ULID Generation

Two ULID generators exist; the choice matters for correctness.

| Use case | Generator | Why |
| --- | --- | --- |
| **Event IDs** (`core.Event.ID`), session IDs | `core.NewULID()` | Identity and dedup key. Set as `Nats-Msg-Id` header for JetStream dedup within the dedup window; stable across JetStream rebuilds. Ordering is owned by JetStream's per-stream `uint64` seq — **not** ULID lex order. |
| **Entity primary keys** (players, locations, characters, exits, objects, policies) | `idgen.New()` | Identity, not ordering. Fresh `crypto/rand` entropy per call. |

`core.Event{}` struct literals MUST use `core.NewEvent()` rather than a raw literal — `NewEvent()` stamps a monotonic ULID via `core.NewULID()`. Never supply `Event.ID` manually (e.g., from `idgen.New()`).

### Error Handling

Use `oops` for structured errors: `oops.With(k, v).Wrap(err)`, `oops.Errorf(...)`, `oops.Code("CODE").Wrap(err)` at API boundaries. Log with `errutil.LogError(logger, msg, err)`. Test with `errutil.AssertErrorCode` / `AssertErrorContext`.

**Method-value gotcha:** when using accessor methods (e.g., `decision.Reason()`), always include `()`. Without parens, Go creates a method value (func pointer) that compiles silently when passed to `...any` parameters (`oops.With`, `slog`).

### Structured Logging

| Requirement | Description |
| ----------- | ----------- |
| **MUST** use context-carrying variants | `slog.InfoContext(ctx, …)` / `WarnContext` / `ErrorContext` / `DebugContext` and `errutil.LogErrorContext(ctx, …)` — **never** bare `slog.Info(…)` / `logger.Warn(…)` / `errutil.LogError(…)` — whenever a `context.Context` is in scope |
| **MUST NOT** drop the context | If a `ctx` is reachable (parameter, struct field, or derivable), it MUST be threaded into the log call |
| **MAY** use bare variants | Only when no `ctx` exists *and* one cannot reasonably be plumbed (init/`main`, bare goroutines, pure helpers with no caller context) — this is the "absolutely impossible" carve-out |

**Why:** trace context (`trace_id`/`span_id`) lives on the `context.Context`. Only the `*Context` variants propagate it into the OpenTelemetry log pipeline, so bare calls produce orphaned log lines that can't be correlated with the trace/span they belong to in Loki, Grafana, or Sentry. See [logging.md](.claude/rules/logging.md) for the full rationale and the `sloglint` `context: scope` enforcement (now active).

### Database Migrations

`internal/store/migrations/`, embedded at compile time. Sequential numbering, paired `.up.sql` + `.down.sql`, idempotent (`IF NOT EXISTS`), no triggers/functions (all logic in Go). Full guide: [database-migrations.md](site/src/content/docs/contributing/database-migrations.md).

### License Headers

| Requirement                         | Description                                         |
| ----------------------------------- | --------------------------------------------------- |
| **MUST** include SPDX header        | `.go`, `.sh`, `.proto` files (Apache-2.0)           |
| **SHOULD** include SPDX header      | YAML configs where appropriate                      |
| **MUST NOT** add to generated files | Skip `*.pb.go`                                      |
| **Applied** by `task fmt`            | `task fmt` adds headers via `license-eye`; `task license:check` / CI verify |

Directories checked: `api/`, `cmd/`, `internal/`, `pkg/`, `plugins/`, `scripts/`.

### Proto Doc Comments

Every proto element needs a Go-grounded leading comment; no name-echo. Enforced
by buf `COMMENTS` (ratcheted in `buf.yaml`) + name-echo gate. Full guide:
`.claude/rules/proto-doc-comments.md` (auto-loads on `api/proto/**`).

## Testing

Detail in `.claude/rules/testing.md` (auto-loads when editing test files): coverage targets, test naming (ACE), table-driven patterns, assertions, mockery, ginkgo/gomega integration tests, EventBus test harness, ABAC test engines.

| Always-on rule                                    | Description                                                                                                              |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| **MUST** write tests before impl                  | TDD — see `dev-flow:test-driven-development`                                                                          |
| **MUST** maintain >80% coverage                   | Per-package; verify with `task test:cover`                                                                               |
| **MUST** use Ginkgo/Gomega for full-stack integration tests | Build tag `//go:build integration`; runs via `task test:int`                                               |
| **MUST** run `task test:int` on refactors         | `task test` does NOT compile integration files — refactors of shared types break silently otherwise                      |
| **MUST NOT** import `eventbustest`/`coretest` in production code | Production code MUST NOT import `eventbustest`/`coretest` (depguard-enforced); embedded NATS is correct at every test tier |
| Canonical tier taxonomy                           | Lives in `.claude/rules/testing.md`.                                                                                     |

### Session-store testing (Docker required)

Tests in `internal/grpc/`, `internal/grpc/focus/`, `internal/command/handlers/`, and `internal/session/` that exercise `session.Store`-touching logic require Docker even under `task test` — they use the `internal/testsupport/sessiontest.NewStore(t)` helper, which is backed by a fresh database on the shared Postgres testcontainer. This is the **deliberate exception** to the "SharedPostgres tests MUST be `//go:build integration`" convention (`session.Store` has exactly one implementation — `store.PostgresSessionStore` — so there is no in-memory fake to test against). See [docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md](docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md) for the rationale.

| Requirement                                | Description                                                                                              |
| ------------------------------------------ | -------------------------------------------------------------------------------------------------------- |
| **MUST** use `sessiontest.NewStore(t)`     | For any test needing a `session.Store` — never construct one ad hoc.                                     |
| **MUST** seed FK parents when needed       | Sessions Set with a non-zero `PlayerSessionID` need `sessiontest.NewStoreWithPool(t)` + `SeedPlayerSession(t, pool, ps)` (the `sessions.player_session_id` FK is enforced). |
| **MUST NOT** add `//go:build integration`  | The `sessiontest` package is the deliberate exception; Ginkgo suites pass their captured `suiteT`.       |
| **MUST** have Docker running               | Absence surfaces as testcontainers container-start errors at test runtime, not compile failures.         |

### Integration test harness (`internal/testsupport/integrationtest`)

`internal/testsupport/integrationtest/` is the canonical integration-test harness — a real in-process holomush stack (Postgres testcontainer + embedded NATS JetStream + production `CoreServer`) used by privacy/presence/scene/session integration tests. Build-tag-gated (`//go:build integration`); never linked into production binaries. See the package doc-comment in `harness.go` for the full helper catalog and the [integration-tests contributor guide](site/src/content/docs/contributing/integration-tests.md).

| When to use                                | When NOT to use                                                 |
| ------------------------------------------ | --------------------------------------------------------------- |
| Integration tests asserting wire behavior  | Unit tests — use `mockery`-generated mocks                      |
| Privacy / presence / floor invariants      | Bus-only tests — use `eventbustest.Embedded` directly           |
| Tests needing real `CoreServer` RPC paths  | Tests that only need a `CoreServer` field stubbed (use struct literal) |

Default ABAC engine is allow-all. Tests needing denial-path coverage pass `WithPolicyEngine(policytest.DenyAllEngine())` — see `test/integration/privacy/privacy_test.go` for examples.

## Commands

### Task Commands (Required)

**MUST use `task` for all build, test, lint, and format operations.** Do NOT run `go build`, `go test`, `golangci-lint`, etc. directly.

```bash
task lint      # Run all linters
task fmt       # Format all files
task test      # Run unit tests (compact output via gotestsum)
task build     # Build binary
task dev       # Run dev server
task plugin:build-all              # Discover and compile all binary plugins for linux/amd64
task plugin:build -- core-scenes   # Build a single binary plugin
```

**Test commands accept arguments after `--`:**

```bash
task test                                        # All unit tests
task test -- ./internal/command/                  # Single package
task test -- -run TestCapability ./internal/command/  # Specific test
task test:verbose -- ./internal/command/          # Full verbose output
task test:int                                    # Integration tests (needs Docker)
```

| Requirement                            | Description                                       |
| -------------------------------------- | ------------------------------------------------- |
| **MUST** use `task`                    | Never run Go/lint/fmt commands directly            |
| **MUST** run `task test`               | Before claiming any implementation is complete     |
| **MUST** run `task lint`               | Before committing changes                          |
| **MUST NOT** disable lint/format rules | Without explicit user confirmation                 |
| **SHOULD** run `task fmt`              | Before committing to ensure consistent formatting  |

> **`task fmt` mutates files** (adds SPDX headers, reflows markdown tables). Those edits are part of your change — **commit them**. Uncommitted `fmt` output is a common cause of a red CI (`license:check` / markdown lint) on an otherwise-green PR.

**MUST** run `task pr-prep` (the **fast lane**) before creating a PR or
pushing to a PR branch. The fast lane runs bats → schema-check →
license:check → plugin:build-all → lint → fmt:check → unit tests → build.
It requires no Docker and holds no flock, so it is always safe to run
without coordination. On docs-only diffs it auto-delegates to
`task pr-prep:docs` (markdown lint, YAML lint, docs-symmetry check, fmt,
license). Note: docs detection relies on jj's snapshot of `@`; run `jj st`
first if you've made edits since the last `jj` command.

`task pr-prep:full` runs the flock-serialized **full lane** (everything
fast does, plus integration tests and E2E in Docker). It is opt-in —
recommended when your diff touches int/E2E surface (Ginkgo suites, Playwright
specs, or their shared helpers). The full lane is serialized machine-globally
per user. Use `HOLOMUSH_PR_PREP_FORCE_FULL=1 task pr-prep` to trigger it
from the auto-detecting entry point.

**`Integration Test` and `E2E Test` are required CI checks protecting `main`.**
They run on Namespace runners with Testcontainers Cloud in CI — not in the
mandatory local fast lane. See [pr-prep](site/src/content/docs/contributing/pr-prep.md)
for the full lanes reference and lock/contention behavior.

**Reading the pr-prep result — exit code first, then disambiguate; never
grep the lock string.** Run it as the SOLE command (`task pr-prep` — no
`| tee` / `| tail` / trailing `echo`, which mask `$?`). Exit `0` = pass.
Non-zero = something stopped it, but **go-task collapses every non-zero exit
to `201`**, so the exit code alone canNOT tell lock contention apart from a
real gate failure. Distinguish them by behavior, not by a status substring:
**contention** returns in ~2s, runs no lane steps, and prints `ERROR: another
pr-prep is running` to stderr (retry-able — wait, then re-run); a **real
failure** runs lane steps (minutes) and fails a named check (`fmt:check`,
`lint:*`, a test) — do NOT retry, fix it. **MUST NOT** drive a retry loop off
the string `another pr-prep is running`: pr-prep's own `pr-prep-lock.bats`
self-test surfaces that exact string on **healthy** runs, so matching it loops
forever re-running the full serialized lane (May 2026 incident). The
final-line `✓ All PR checks passed.` confirms a pass; it is not a substitute
for the exit code. Each run also prints a line `▸ pr-prep result: <path>` and
writes that file with a `status=` line (`pass`/`fail`/`contention`) — match the
`▸ pr-prep result:` prefix (don't assume a line number) and read the file for
the authoritative verdict; the behavioral cues above are the fallback.

### Generated code

Some generated output is committed; regenerate and commit it in the **same
change** or CI fails a stale-diff check:

| After changing         | Run                               | Commit                                 |
| ---------------------- | --------------------------------- | -------------------------------------- |
| `api/proto/**` schemas | `task proto && task web:generate` | `pkg/proto/**/*.pb.go` + web `*_pb.ts` |

`task lint:proto` MUST be green after any proto change.

### Session isolation

This repo is developed primarily by concurrent AI agent sessions. Because jj snapshots the working copy on every command, two sessions sharing the same jj workspace will collide on uncommitted edits.

| Requirement | Description |
|---|---|
| **MUST** isolate per session | Agents: `task workspace:new -- <name>`, then `cd <printed-path>`. Humans: see [sessions guide](site/src/content/docs/contributing/sessions.md) for shell-function setup. |
| **MUST NOT** edit files in `default` | The shared workspace is for **read-only inspection only** (search, reads, answering questions). A `SessionStart` hook flags any session that starts there. If you are in `default` and intend to edit, isolate **first** (row above) before touching any file — concurrent sessions silently corrupt each other's uncommitted edits at every `jj` command boundary. |
| **MUST** clean up post-merge | After landing: `cd <repo-root> && jj workspace forget <name> && rm -rf <repo-parent>/.worktrees/<name>`. The `cd` matters — `../.worktrees/<name>` is unsafe from any nested cwd. |

`task workspace:new` is idempotent, runs `jj git fetch` first, and writes a `.beads/redirect` so `bd` works in the new workspace. New workspaces inherit `.claude/` (tracked in git), so all hooks fire identically. Sub-agents launched via the `Task` tool inherit the parent's workspace; the parent MUST NOT dispatch parallel `Task` calls that edit the same files.

**`gh` in a jj workspace** has no `.git` directory to auto-detect the repo, so it cannot infer the remote — always pass `-R holomush/holomush` explicitly (e.g. `gh pr view 123 -R holomush/holomush`, `gh pr create -R holomush/holomush ...`).

### Required session-start skills

Two skills MUST be loaded via the `Skill` tool **before your first response** in any session (both enforced by `SessionStart` hooks):

| Skill | Why |
|---|---|
| `jj:jujutsu` | jj-colocated repo — all VCS goes through jj (the jj plugin's guard hooks block mutating `git`). |
| `dev-flow:grepping` | The search-tool ladder (`mcp__probe__*` for Go symbol/AST → `rg` for text → `ast-grep` for structural) is non-obvious; loading it up front prevents defaulting to bare `grep`/full-file reads. Pairs with `.claude/rules/search-tools.md`. |

### Beads, jj

`bd` commands: see `.claude/rules/beads-project.md` and `bd prime`. `jj` workflow: see the `jj:jujutsu` skill.

**`.beads/interactions.jsonl` is git-tracked** (bd's interaction log — status/field-change history), distinct from the Dolt DB (live bead state, synced separately via `bd dolt push`). It accumulates as you run `bd` and is committed periodically. When committing or pushing other work, **include any pending `.beads/interactions.jsonl` change as needed** so the tracked log stays current — `bd dolt push` does NOT commit it.

## Reference

- **Directory structure**: see top-level `tree -L 2` or `ls`. Public layout overview lives in `site/src/content/docs/contributing/`.
- **Key interfaces** (`EventBus`, `ServiceRegistry`, `ServiceProvider`): `.claude/rules/event-interfaces.md` (auto-loads when editing eventbus / plugin code)
- **Gateway boundary invariant**: `.claude/rules/gateway-boundary.md` (auto-loads in `cmd/holomush/`, `internal/web/`, `internal/grpc/`)
- **Terminology** (location vs room, character vs player, etc.): `.claude/rules/terminology.md` (auto-loads on `*.md` and domain code)
- **Invariant registry** (defining, respecting, and binding named system invariants): `.claude/rules/invariants.md` (auto-loads on `docs/architecture/invariants.*`, the registry tooling, and specs)

## Core Systems

### World Model (`internal/world`)

The world model provides the spatial foundation:

- **Objects** - Base entity type with ULID identifiers
- **Locations** - Rooms/areas that contain objects
- **Exits** - Connections between locations (with optional locks)
- **Scenes** - RP scenes with participants and privacy settings

All world operations go through `WorldService` which validates constraints and
persists to PostgreSQL via the repository interface.

### Plugin System (`internal/plugin`)

Three plugin types:

| Type        | Runtime                  | Use case                                    |
| ----------- | ------------------------ | ------------------------------------------- |
| `lua`       | gopher-lua VM            | Lightweight event handlers, game logic      |
| `binary`    | hashicorp/go-plugin subprocess | Complex services with proto contracts  |
| `setting`   | Bootstrap only           | Configuration-only plugins (no runtime)     |

**Manifest schema** (`plugin.yaml`): Each plugin declares `requires` (proto
services it depends on), `provides` (proto services it implements), and
`storage` (database tables it needs). The plugin loader performs DAG dependency
resolution to determine load order and validates that all `requires` are
satisfied by another plugin's `provides`.

**Service registry**: Maps proto service names to implementations. Binary
plugins register services over gRPC; Lua plugins register via the Lua host.

**Plugin admin commands:**

```bash
plugin list            # List loaded plugins with name, type, version
plugin info <name>     # Detailed plugin info (requires, provides, storage, commands)
```

### Plugin Runtime Symmetry

<!-- BEGIN: plugin-runtime-symmetry -->

**Project invariant: Binary and Lua plugins MUST be treated identically by the host.**

Any host-side trust check, validation, or feature MUST apply to both binary and Lua plugins. Asymmetric behavior between plugin runtimes is forbidden — it creates a privilege gradient that violates the core plugin-system design.

Detail, rationale, and a worked example are in `.claude/rules/plugin-runtime-symmetry.md` (auto-loads in `internal/plugin/`, `pkg/plugin/`, `plugins/`, `internal/access/`).

<!-- END: plugin-runtime-symmetry -->

### Access Control (`internal/access`)

Attribute-Based Access Control (ABAC) with phased implementation:

- **Phase 1 (current):** Static evaluator with role-based permissions
- **Phase 2 (future):** Full ABAC with policies and attributes

```go
// Check if subject can perform action on resource
allowed := evaluator.Evaluate(ctx, subject, action, resource)
```

Default deny - explicit permission required for all operations.

### Command Authorization

Commands use two-layer authorization at dispatch time:

1. **Layer 1 — Command Execution:** `engine.Evaluate(subject, "execute", "command:<name>")` — can this character run this command?
2. **Layer 2 — Capability Pre-Flight:** `engine.CanPerformAction(subject, action, resource, scope)` per declared capability — does this character have the class of permissions this command needs?

Commands declare capabilities as structured objects:

```go
Capabilities: []command.Capability{
    {Action: "write", Resource: "location", Scope: command.ScopeLocal},
}
```

Scope: `ScopeSelf` (default, own character), `ScopeLocal` (current location), `ScopeGlobal` (server-wide).

## Patterns

### Event Sourcing

- All game actions produce events
- Events are immutable and ordered
- State is derived from event replay

### HTTP Middleware

When wrapping `http.ResponseWriter` (e.g., cookie middleware), the wrapper
MUST implement `http.Flusher` and `Unwrap()` — ConnectRPC server-streaming
calls `Flush()` after each frame and will error if the interface is missing.

### Web Client

See `web/CLAUDE.md` for SvelteKit-specific patterns including theme system
architecture, shadcn-svelte conventions, Tailwind v4 guidance, and Svelte 5
runes patterns.

## Landing the Plane (Session Completion)

Work is NOT complete until `jj git push` succeeds. The full session-completion checklist lives in `.claude/rules/landing-the-plane.md` (always loaded). Skip the chain only for small fixes (typo, dependency bump, single-file bug).

**Pre-push rebase:** use the chain-safe `-s` recipe — canonical copy in `.claude/rules/landing-the-plane.md` (Mandatory checklist, "Push" item) and the `jj:jujutsu` skill ("Pre-Push Rebase"); the `guard-jj-rebase-chain` hook blocks the truncation-prone `-r @` shape.
