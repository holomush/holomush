<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Full ABAC Design Decisions

> **This document has been split into individual decision files.**
>
> See: [docs/specs/decisions/epic7/](decisions/epic7/README.md)

The 95 design decisions from this document have been reorganized into
individual files under `docs/specs/decisions/epic7/`, organized by phase:

- `general/` — Cross-cutting decisions (engine approach, format, performance)
- `phase-7.1/` — Policy schema (property model, visibility, audit schema)
- `phase-7.2/` — DSL and compiler (grammar, parser, operators)
- `phase-7.3/` — Engine and providers (resolution, caching, session, lifecycle)
- `phase-7.4/` — Seed policies (bootstrap, builder permissions)
- `phase-7.5/` — Locks and admin commands (lock policies, validation)
- `phase-7.6/` — Migration (adapter removal, shadow mode removal)
- `phase-7.7/` — Resilience (async audit, WAL, eventual consistency)

**Related:** [Full ABAC Architecture Design](2026-02-05-full-abac-design.md)
