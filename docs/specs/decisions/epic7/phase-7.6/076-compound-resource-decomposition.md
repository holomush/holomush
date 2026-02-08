<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 76. Compound Resource Decomposition During Migration

> [Back to Decision Index](../README.md)

**Question:** The codebase uses compound resource strings like
`location:<id>:characters` (`service.go:470`). The engine's prefix parser splits
on the first `:`, producing `type=location`, `id=01ABC:characters` — which is
incorrect. How should compound resources be handled during migration?

**Options Considered:**

1. **Decompose into action** — `location:<id>:characters` becomes
   `resource=location:<id>` with `action=list_characters`.
2. **New prefix** — Introduce `location-characters:` as a distinct prefix.
3. **Multi-segment parsing** — Extend parser to handle `prefix:id:qualifier`.

**Decision:** Option 1. Decompose compound resources during migration. The call
site `location:<id>:characters` becomes `resource=location:<id>` with
`action=list_characters`. This aligns with the ABAC model where the action
captures the intent (what you want to do) and the resource identifies the target
(which location).

**Rationale:** The ABAC model separates concerns: the resource identifies *what*
is being accessed, the action identifies *how*. "List characters in a location"
is naturally `action=list_characters, resource=location:<id>`. A new prefix
(option 2) proliferates prefix types. Multi-segment parsing (option 3) adds
parser complexity for a pattern used in only one call site.

**Cross-reference:** `internal/world/service.go:470`, spec AccessRequest
(lines 305-314), Phase 7.6 migration plan. Bead: `holomush-5k1.275`.
