# HoloMUSH Repository Setup Design

**Status:** Draft
**Date:** 2026-01-17
**Version:** 0.1.0

## Overview

This document defines the repository structure, tooling, and development workflow for HoloMUSH. The project is AI-written and uses beads for task tracking.

## Development Environment

### Target Setup

- Primary: Single developer, AI-assisted
- Future: Open source with community contributors
- Editors: VS Code, GoLand, Cursor (editor-agnostic config)

### Core Tools

| Tool | Purpose |
|------|---------|
| Go 1.22+ | Server implementation |
| Task | Build automation (replaces Make) |
| golangci-lint | Go linting |
| Lefthook | Pre-commit hooks |
| Beads | Task/issue tracking |
| GitHub Actions | CI/CD |
| goreleaser | Release automation |
| release-please | Changelog and versioning |
| Dependabot | Dependency updates |

### Linting Stack

| Tool | Targets |
|------|---------|
| golangci-lint | `*.go` |
| markdownlint-cli2 | `*.md` |
| yamllint | `*.yaml`, `*.yml` |
| dprint | md, json, toml, yaml (formatting) |
| actionlint | `.github/workflows/*.yaml` |

## Directory Structure

```
holomush/
├── cmd/
│   └── holomush/
│       └── main.go              # Server entry point
├── internal/                    # Private server internals
│   ├── core/                    # Core engine (events, sessions, world)
│   ├── telnet/                  # Telnet adapter
│   ├── web/                     # WebSocket adapter (future)
│   ├── store/                   # PostgreSQL, event store impls
│   └── wasm/                    # Plugin host (wazero)
├── pkg/                         # Public APIs for plugins
│   ├── plugin/                  # Plugin SDK types/interfaces
│   └── api/                     # Game API exposed to plugins
├── plugins/                     # Core plugins (ship with server)
│   ├── core-commands/           # look, say, pose, quit, etc.
│   ├── channels/                # Channel system
│   └── building/                # @dig, @create, etc. (future)
├── web/                         # SvelteKit app (future)
├── docs/
│   ├── plans/                   # Implementation plans
│   ├── specs/                   # Specifications
│   └── reference/               # API/user documentation
├── scripts/                     # Build/dev scripts
├── .beads/                      # Beads issue storage
├── .github/
│   ├── workflows/
│   │   ├── ci.yaml              # Lint, test, build
│   │   └── release.yaml         # release-please + goreleaser
│   ├── dependabot.yaml          # Dependency updates
│   └── CODEOWNERS               # For future contributors
├── .editorconfig                # Editor settings
├── .gitignore                   # Go + Node ignores
├── .golangci.yaml               # Linter config
├── .goreleaser.yaml             # Release automation
├── .markdownlint.yaml           # Markdown linting rules
├── .yamllint.yaml               # YAML linting rules
├── dprint.json                  # Formatter config
├── lefthook.yaml                # Pre-commit hooks
├── Taskfile.yaml                # Build automation
├── Dockerfile                   # Container build
├── LICENSE                      # Apache 2.0
├── README.md                    # Project overview
├── CLAUDE.md                    # AI coding instructions
├── CONTRIBUTING.md              # For future contributors
└── go.mod                       # Go module
```

### Key Distinctions

- `internal/` - Server implementation, not importable by plugins
- `pkg/` - Stable API that plugins depend on
- `plugins/` - Core plugins (WASM modules built from Go/Rust/etc.)

## Workflow

### Beads-Driven Development

All work follows this flow:

```
Spec (docs/specs/)
    ↓
Epic (bd-xyz) - represents the spec
    ↓
Implementation Plan (docs/plans/)
    ↓
Tasks (bd-xyz.1, bd-xyz.2, ...) - child tasks of epic
    - Dependencies based on file overlap
    - Dependencies based on conceptual overlap
```

### Example

```
Spec: event-system.md
Epic: bd-a1b2 "Event System"
    ├── bd-a1b2.1 "Event type definitions"
    ├── bd-a1b2.2 "EventStore interface"
    ├── bd-a1b2.3 "PostgresEventStore" (depends on .1, .2)
    ├── bd-a1b2.4 "Event replay logic" (depends on .2, .3)
    └── bd-a1b2.5 "Integration tests" (depends on .3, .4)
```

### Daily Workflow

1. `bd ready` - Find unblocked tasks
2. Select task to work on
3. Write failing tests
4. Implement until tests pass
5. Update documentation
6. `bd close <task>` - Mark complete
7. Repeat

## Configuration Files

### Taskfile.yaml

| Task | Purpose |
|------|---------|
| `task lint` | Run all linters |
| `task fmt` | Run all formatters |
| `task test` | Run go test |
| `task build` | Build server binary |
| `task dev` | Run server in dev mode |
| `task plugin:build` | Build WASM plugins |
| `task spec` | Create new spec from template |
| `task plan` | Create new plan from template |

### lefthook.yaml

| Hook | Runs |
|------|------|
| pre-commit | `task lint`, `task fmt --check` |
| commit-msg | cocogitto (`cog verify`) |

### cog.toml

Cocogitto configuration for conventional commit validation only. Release-please handles versioning and changelog generation.

### golangci-lint Linters

| Category | Linters |
|----------|---------|
| Bugs | errcheck, govet, staticcheck, nilerr |
| Style | revive, gofumpt, misspell |
| Performance | prealloc, unconvert |
| Security | gosec |
| Error handling | errorlint, wrapcheck |
| Maintenance | unparam, gocritic, nolintlint |

### .editorconfig

```ini
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
trim_trailing_whitespace = true
indent_style = space
indent_size = 2

[*.go]
indent_style = tab

[*.md]
trim_trailing_whitespace = false

[Makefile]
indent_style = tab
```

## CI/CD

### ci.yaml Workflow

Triggers: push to main, pull requests

| Job | Steps |
|-----|-------|
| lint | Install tools, run `task lint` |
| test | Run `task test` with coverage |
| build | Run `task build`, upload artifact |

### release.yaml Workflow

Triggers: push to main (release-please), tag push (goreleaser)

| Job | Purpose |
|-----|---------|
| release-please | Create release PR, update changelog |
| goreleaser | Build binaries, Docker images, publish |

### Dependabot

- Go modules: weekly updates
- GitHub Actions: weekly updates

## CLAUDE.md Structure

```markdown
# HoloMUSH Development Guide

## Project Overview
- Brief description, link to architecture design
- Tech stack summary

## Development Principles
- TDD: Tests MUST pass before completion
- Spec-driven: No work without spec
- RFC2119 keywords in all specs/plans
- Beads for all task tracking

## Workflow
1. Spec → Epic (`bd create "..." --epic`)
2. Plan → Tasks (`bd create "..." -p <epic>`)
3. `bd ready` → find next task
4. Test → Implement → Document
5. `bd close` → next task

## Code Conventions
- Go idioms (accept interfaces, return structs)
- Error handling patterns
- Logging conventions
- Naming conventions

## Testing
- Table-driven tests
- Test file placement
- Mocking patterns

## Commands
- `task lint/fmt/test/build`
- `bd` commands reference

## Patterns
- (Evolves as project grows)
```

## License

Apache 2.0 - permissive with patent protection.

## Files to Create

| File | Purpose |
|------|---------|
| `.editorconfig` | Editor settings |
| `.gitignore` | Go + Node + build artifacts |
| `.golangci.yaml` | Go linter config |
| `.goreleaser.yaml` | Release automation |
| `.markdownlint.yaml` | Markdown lint rules |
| `.yamllint.yaml` | YAML lint rules |
| `cog.toml` | Cocogitto commit validation |
| `dprint.json` | Formatter config |
| `lefthook.yaml` | Pre-commit hooks |
| `Taskfile.yaml` | Build automation |
| `Dockerfile` | Container build |
| `LICENSE` | Apache 2.0 |
| `README.md` | Project overview |
| `CLAUDE.md` | AI coding instructions |
| `CONTRIBUTING.md` | Contributor guide |
| `.github/workflows/ci.yaml` | Lint, test, build |
| `.github/workflows/release.yaml` | release-please + goreleaser |
| `.github/dependabot.yaml` | Dependency updates |

## Requirements

- All config files MUST be created before Phase 1 implementation begins
- Beads MUST be initialized (`bd init`) before creating tasks
- Pre-commit hooks MUST be installed via Lefthook
- CI MUST pass before merging any PR
- All commits MUST follow conventional commit format

---

## Appendix: Tool Installation

```bash
# Task
go install github.com/go-task/task/v3/cmd/task@latest

# golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Lefthook
go install github.com/evilmartians/lefthook@latest

# Beads
go install github.com/steveyegge/beads/cmd/bd@latest

# markdownlint-cli2
npm install -g markdownlint-cli2

# yamllint
pip install yamllint

# dprint
curl -fsSL https://dprint.dev/install.sh | sh

# actionlint
go install github.com/rhysd/actionlint/cmd/actionlint@latest

# goreleaser
go install github.com/goreleaser/goreleaser@latest
```
