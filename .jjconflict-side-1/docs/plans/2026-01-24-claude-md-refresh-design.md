# CLAUDE.md Refresh Design

**Status:** Ready for implementation
**Date:** 2026-01-24

## Overview

Comprehensive refresh of CLAUDE.md files to reflect current codebase state. The plugin
system moved from WASM/Extism to Lua (gopher-lua), new packages were added (world, access),
and the documentation site moved to zensical.

## Goals

- **Accurate:** Remove outdated WASM references, update directory structure
- **Streamlined:** Remove duplication, reduce stale-prone detail
- **Enriched:** Document new stable systems (world model, access control)

## Key Decisions

| Decision       | Choice                                         | Rationale                                              |
| -------------- | ---------------------------------------------- | ------------------------------------------------------ |
| WASM removal   | Replace with minimal Lua testing principles    | Focus on stable principles, not implementation details |
| File structure | Keep `docs/CLAUDE.md`, create `site/CLAUDE.md` | Separation of concerns                                 |
| New content    | Document world + access, minimal plugin docs   | Document stable systems, skip evolving ones            |
| Architecture   | Reference roadmap doc only                     | Keep CLAUDE.md workflow-focused                        |

## Files

### site/CLAUDE.md (Create)

Zensical-specific guidance:

- Site structure (`site/docs/`, `zensical.toml`)
- Audience directories (contributors, developers, operators)
- Build commands (`task docs:*`)
- Navigation conventions

~80 lines.

### docs/CLAUDE.md (Update)

Changes:

- **Remove:** RFC2119 section (duplicated from root)
- **Remove:** Reference section mention (no `docs/reference/` exists)
- **Keep:** File naming, markdown standards, document structure, lint errors

~140 lines (from 174).

### CLAUDE.md (Root - Major Update)

#### Removals

| Section                                     | Reason                  |
| ------------------------------------------- | ----------------------- |
| "WASM plugin system via Extism" in overview | No longer accurate      |
| WASM Tests section (65 lines)               | `internal/wasm` removed |
| `internal/wasm` in directory structure      | Package removed         |
| `plugins/` as "Core plugins (WASM)"         | Now Lua plugins         |
| Lua/Python license header examples          | WASM plugin languages   |
| Coverage reference to `internal/wasm`       | Package removed         |

#### Updates

| Section                | Change                                                  |
| ---------------------- | ------------------------------------------------------- |
| Project Overview       | "WASM plugin system" → "Lua plugin system (gopher-lua)" |
| Architecture Reference | Point to roadmap doc                                    |
| Directory Structure    | Add world, access, plugin; remove wasm, proto           |
| License headers        | Keep Go, Shell, Protobuf, YAML only                     |

#### Additions

| Section        | Content                             |
| -------------- | ----------------------------------- |
| World Model    | Brief overview of `internal/world`  |
| Access Control | Brief overview of `internal/access` |
| Plugin Testing | Minimal Lua testing principles      |

~400 lines (from 540).

## Implementation Order

1. `site/CLAUDE.md` - Create new (no conflicts)
2. `docs/CLAUDE.md` - Minor edits (remove duplication)
3. `CLAUDE.md` (root) - Major update (largest change)

Each file committed separately for clean history.

## Post-Implementation

- [ ] Run `task lint:markdown` to verify all files pass
- [ ] Review directory structure matches actual `internal/` packages
- [ ] Verify all referenced docs exist
