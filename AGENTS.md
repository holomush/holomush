# HoloMUSH Development Guide

This document provides instructions for AI coding assistants working on HoloMUSH.

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
| `site/docs/`             | Public documentation website (zensical)      | All users               |
| `docs/plans/`            | Implementation plans, in-progress work       | Contributors (internal) |
| `docs/specs/`            | Design specifications, architectural designs | Contributors (internal) |
| `docs/superpowers/plans/`| AI-generated implementation plans (superpowers skill) | Contributors (internal) |
| `docs/superpowers/specs/`| AI-generated design specifications (superpowers skill) | Contributors (internal) |

**Site documentation** (`site/docs/`) is organized by audience:

- `guide/` — For players and game designers
- `operating/` — For people running HoloMUSH servers
- `extending/` — For plugin developers building on HoloMUSH
- `contributing/` — For people contributing to the HoloMUSH codebase
- `reference/` — Auto-generated API and event references

**Build commands:**

```bash
task docs:setup   # Install documentation dependencies
task docs:serve   # Start local dev server
task docs:build   # Build static site
```

For sandbox operations at `game.holomush.dev`, see
[site/docs/operating/sandbox-operations.md](site/docs/operating/sandbox-operations.md)
and [site/docs/operating/sandbox-restore.md](site/docs/operating/sandbox-restore.md).

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

**See:** [Pull Request Guide](site/docs/contributing/pr-guide.md) for the complete workflow.

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

All tasks MUST be reviewed before completion via `pr-review-toolkit:review-pr`. Workflow detail at [Pull Request Guide](site/docs/contributing/pr-guide.md).

| Requirement                                | Description                                          |
| ------------------------------------------ | ---------------------------------------------------- |
| **MUST** use `pr-review-toolkit:review-pr` | Launch comprehensive review using specialized agents |
| **MUST** address all findings              | Fix issues or document why not applicable            |
| **MUST NOT** skip review                   | Even for "simple" changes                            |

### Plan → bd materialization

Plans drive bd state via `dev-flow:plan-to-beads`, which reads the plan's task table (each `### Task N:` heading inside a `## Phase N:` section) and materializes the epic + child beads + dependency graph in one pass. **Plans do NOT carry a `## Bead chain structure` section** — bd is the source of truth for graph topology (per the `dev-flow:plan-to-beads` skill spec Rule 4). The ancestor `bead-chain-design` / `bead-chain-from-plan` convention is superseded.

Per Rule 3, each task bead's `--description` is **narrative only** (Goal, Plan reference, Files touched, Out of scope). Acceptance criteria, verification commands, dependencies, and labels live in their dedicated bd flags (`--acceptance`, `--deps`, `--labels`, `--skills`).

## Pre-Push Review Gates

Three adversarial read-only sub-agents gate hand-offs BEFORE the PR surface.
These complement `pr-review-toolkit:review-pr` (which runs on the PR itself)
by providing an earlier, in-session review pass.

| Agent             | Fires before                                                                           | Invocation                              |
| ----------------- | -------------------------------------------------------------------------------------- | --------------------------------------- |
| `design-reviewer` | `dev-flow:writing-plans` is invoked on a spec                                       | `/review-design [<spec-path>]` or auto  |
| `plan-reviewer`   | `dev-flow:executing-plans` or `dev-flow:subagent-driven-development` runs a plan | `/review-plan [<plan-path>]` or auto    |
| `code-reviewer`   | `bd close`, `jj git push`, or PR creation                                              | `/review-code [<target>]` or auto       |
| `crypto-reviewer` | `code-reviewer` (runs FIRST), for changes touching `internal/eventbus/crypto/`, `internal/eventbus/codec/`, `internal/eventbus/history/dispatcher.go`, `internal/eventbus/history/cold_postgres.go`, `internal/plugin/event_emitter.go::Emit`, `internal/eventbus/audit/projection.go`, plugin manifest `crypto.emits` declarations, or migrations on `crypto_keys` / `events_audit` | `/review-crypto` or auto via `remind-pre-action-review.sh` |
| `abac-reviewer`   | `code-reviewer` (runs alongside), for changes touching `internal/access/`              | `/review-abac` or auto via `remind-pre-action-review.sh` |

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

### Database Migrations

`internal/store/migrations/`, embedded at compile time. Sequential numbering, paired `.up.sql` + `.down.sql`, idempotent (`IF NOT EXISTS`), no triggers/functions (all logic in Go). Full guide: [database-migrations.md](site/docs/contributing/database-migrations.md).

### License Headers

| Requirement                         | Description                                         |
| ----------------------------------- | --------------------------------------------------- |
| **MUST** include SPDX header        | `.go`, `.sh`, `.proto` files (Apache-2.0)           |
| **SHOULD** include SPDX header      | YAML configs where appropriate                      |
| **MUST NOT** add to generated files | Skip `*.pb.go`                                      |
| **Auto-applied** by lefthook        | `task license:add` runs on commit; `task license:check` verifies |

Directories checked: `api/`, `cmd/`, `internal/`, `pkg/`, `plugins/`, `scripts/`.

## Testing

Detail in `.claude/rules/testing.md` (auto-loads when editing test files): coverage targets, test naming (ACE), table-driven patterns, assertions, mockery, ginkgo/gomega integration tests, EventBus test harness, ABAC test engines.

| Always-on rule                       | Description                                                                  |
| ------------------------------------ | ---------------------------------------------------------------------------- |
| **MUST** write tests before impl     | TDD — see `dev-flow:test-driven-development`                              |
| **MUST** maintain >80% coverage      | Per-package; verify with `task test:cover`                                   |
| **MUST** use Ginkgo/Gomega for E2E   | Build tag `//go:build integration`; runs via `task test:int`                 |
| **MUST** run `task test:int` on refactors | `task test` does NOT compile integration files — refactors of shared types break silently otherwise |
| **MUST NOT** use `eventbustest` in E2E | Embedded NATS harness is unit/bus-integration only; E2E uses full stack    |

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

**MUST** run `task pr-prep` before creating a PR or pushing to a PR branch.
It auto-detects docs-only diffs (per `Taskfile.yaml` `vars.DOCS_ONLY_PATHS`)
and runs the `pr-prep:docs` fast lane in that case; for any non-docs path,
it runs the full pipeline mirroring all CI jobs (lint, format, schema,
license, unit, integration, E2E) and MUST pass with zero failures. Use
`HOLOMUSH_PR_PREP_FORCE_FULL=1 task pr-prep` to force the full lane.
Docker is always available — never skip E2E tests on the full lane. The
full lane is serialized per user — only one runs at a time on a given
machine. Note: docs detection relies on jj's snapshot of `@`; run `jj st`
first if you've made edits since the last `jj` command. See
[pr-prep](site/docs/contributing/pr-prep.md) for collision behavior and
the docs lane.

### Session isolation

This repo is developed primarily by concurrent AI agent sessions. Because jj snapshots the working copy on every command, two sessions sharing the same jj workspace will collide on uncommitted edits.

| Requirement | Description |
|---|---|
| **MUST** isolate per session | Agents: `task workspace:new -- <name>`, then `cd <printed-path>`. Humans: see [sessions guide](site/docs/contributing/sessions.md) for shell-function setup. |
| **SHOULD NOT** edit files in `default` | A `SessionStart` hook warns when a session begins there. Reserved for read-only inspection. |
| **MUST** clean up post-merge | After landing: `cd <repo-root> && jj workspace forget <name> && rm -rf <repo-parent>/.worktrees/<name>`. The `cd` matters — `../.worktrees/<name>` is unsafe from any nested cwd. |

`task workspace:new` is idempotent, runs `jj git fetch` first, and writes a `.beads/redirect` so `bd` works in the new workspace. New workspaces inherit `.claude/` (tracked in git), so all hooks fire identically. Sub-agents launched via the `Task` tool inherit the parent's workspace; the parent MUST NOT dispatch parallel `Task` calls that edit the same files.

### Beads, jj

`bd` commands: see `.claude/rules/beads-project.md` and `bd prime`. `jj` workflow: see the `jj:jujutsu` skill.

## Reference

- **Directory structure**: see top-level `tree -L 2` or `ls`. Public layout overview lives in `site/docs/contributing/`.
- **Key interfaces** (`EventBus`, `ServiceRegistry`, `ServiceProvider`): `.claude/rules/event-interfaces.md` (auto-loads when editing eventbus / plugin code)
- **Gateway boundary invariant**: `.claude/rules/gateway-boundary.md` (auto-loads in `cmd/holomush/`, `internal/web/`, `internal/grpc/`)
- **Terminology** (location vs room, character vs player, etc.): `.claude/rules/terminology.md` (auto-loads on `*.md` and domain code)

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

**Manifest schema** (`manifest.yaml`): Each plugin declares `requires` (proto
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
