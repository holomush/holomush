---
created: 2026-08-06T21:20:00.000Z
title: Should INV-WORLD-4 distinguish operational writers from world-state writers
area: docs
severity: minor
files:
  - docs/architecture/invariants.yaml:5057-5083
  - internal/store/migrations/000054_character_identity_and_lifecycle.sql
  - internal/world/character.go:35-38
  - .planning/phases/03-world-character-commands/03-CONTEXT.md
---

## Problem

`INV-WORLD-4` confines raw world-table mutation SQL to `internal/world/postgres` and
enumerates the sanctioned exceptions — currently **exactly THREE** out-of-world
writers (character-genesis, character-reaping, and the operator name-resolution
command added by 02-12). The small count is the point: it is a **scarcity
mechanism**, so each addition has to be a conscious, argued act rather than a quiet
one. 02-12 amended it TWO→THREE deliberately, reasoning that "what was false was the
enumeration and not the guarantee".

Phase 3's `last_active_at` flusher (03-CONTEXT.md D-42) will make it **FOUR**. That
amendment is fine on its own terms and is scoped into Phase 3.

The question this raises is not "is a fourth writer acceptable" — it is what the
fence is protecting. Look at what the fourth writer writes: `last_active_at` is
**operational telemetry about sessions**, not world state. It falls inside the fence
only because Phase 2 put the column on the `characters` table
(`000054_character_identity_and_lifecycle.sql`) as a schema primitive. A fence built
to protect world-model integrity — envelope-per-mutation, version guards, the outbox
contract — is now also counting a writer that participates in none of those concerns.

Two things make this worth asking rather than shrugging at:

1. **An enumeration amended on every new writer stops being a fence and becomes a
   changelog.** Its value comes from being hard to add to.
2. **`INV-WORLD-6` just demonstrated the failure mode.** Its enumeration ("exactly
   TWO paths") drifted out of truth and its binding test never caught it, because
   the test asserted the paths it knew about rather than the property. An
   enumeration that grows by amendment is exactly the shape that drifts.

## Solution

Two candidate answers; pick one, or establish that the status quo is right and record
why.

1. **Move the column out of the world tables.** If `last_active_at` lived outside
   `characters`, `INV-WORLD-4` would not apply at all — no fourth writer, no
   dilution, and the fence keeps meaning "world state". **Cost:** the column shipped
   in Phase 2; `world.Character.LastActiveAt` (`internal/world/character.go:35-38`)
   reads it, `character_repo.go` SELECTs it in three places, the roster sorts on it,
   and D-24 rated its removal **one-way** (removing a shipped column is a migration).
   Do not treat this as cheap.
2. **Split the enumeration by kind.** Keep one fence but have it distinguish
   *world-state* writers (which must emit an envelope atomically) from *operational*
   writers (which must not, and whose obligation is different). Preserves the
   scarcity property where it matters and stops operational additions from inflating
   the world-state count.

Either way the invariant summary should state the *property* it protects, not only
the list of paths that currently satisfy it — that is the specific lesson from
`INV-WORLD-6` (see the sibling todo, `fix-inv-world-6-false-rename-claim-and-coverage-gap`).

**Not blocking Phase 3.** Phase 3 amends the enumeration to four as part of D-42;
this todo is about whether the enumeration is the right instrument, and should be
answered before a fifth writer appears.
