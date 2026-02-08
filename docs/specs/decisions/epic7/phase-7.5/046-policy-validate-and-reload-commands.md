<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 46. `policy validate` and `policy reload` Commands

> [Back to Decision Index](../README.md)

**Review finding (S1, S2):** The command set had no dry-run validation for
policies (admins had to create a policy to discover syntax errors) and no way
to force-refresh the cache when LISTEN/NOTIFY was potentially down.

**Decision:** Add two commands:

1. **`policy validate <dsl-text>`** — Parses and validates DSL without
   creating a policy. Returns success or detailed error with line/column
   information. Available to admins and builders (builders can validate
   hypothetical policies without creating them).

2. **`policy reload`** — Forces an immediate full reload of the in-memory
   policy cache from PostgreSQL. Admin-only. Intended for emergency use when
   LISTEN/NOTIFY may be disconnected.

**Rationale:** `policy validate` closes the feedback loop — admins can iterate
on policy syntax without creating throwaway policies. `policy reload` provides
a manual override for the automatic cache invalidation system, ensuring admins
are never stuck waiting for reconnection during an emergency.
