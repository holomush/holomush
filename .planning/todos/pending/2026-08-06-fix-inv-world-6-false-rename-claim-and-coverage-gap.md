---
created: 2026-08-06T20:55:18.033Z
title: Fix INV-WORLD-6 false rename claim and coverage gap
area: docs
severity: major
files:
  - docs/architecture/invariants.yaml:5095-5107
  - test/integration/world/character_lifecycle_test.go:180-225
  - cmd/holomush/cmd_character_name.go:436-440
  - internal/world/postgres/character_repo.go:212-266
  - .planning/phases/01-portal-spec/01-SPEC.md:714-760
  - .planning/phases/03-world-character-commands/03-CONTEXT.md
---

## Problem

`INV-WORLD-6` (`binding: bound`) claims:

> "a character name becomes claimable again **only** through a sanctioned
> tombstone-emitting hard delete, and there are exactly TWO such paths —
> `world.Service.DeleteCharacter` (the in-world authorized delete) and
> `auth.CharacterReapingService` … Retire MUST NOT release the name."

**The "only … exactly TWO" claim is false in production today.** Renaming a
character overwrites the live row's `normalized_name`, so the old value leaves
the uniqueness index and the next creation can claim it — a third release path,
reached without any tombstone. This is not hypothetical: the shipped operator CLI
`holomush character name set` does it, building its envelope at
`cmd/holomush/cmd_character_name.go:436-440` with `Kind: "character_updated"` and
writing through `CharacterRepository.Rename`
(`internal/world/postgres/character_repo.go:212`).

**The binding does not catch it.** `test/integration/world/character_lifecycle_test.go:221`
asserts only "retire keeps the name reserved, and BOTH hard deletes release it".
It never exercises rename, so the suite is green while the registry summary claims
more than the test proves. That is the precise failure mode
`.claude/rules/invariants.md` calls a false-green — worse here than an unbound
entry, because `bound` launders an unverified claim through the one mechanism
whose purpose is verification.

**This is the second falseness in this entry.** The first — `character_reaping.go:263`
being a second release path the text did not enumerate — was caught *before*
binding (memory `e2nxxx9v5d`). This one shipped.

**Why it matters.** The invariant's rationale is a real hazard, not pedantry: a
freed name claimed by a different character inherits every display name already
frozen into immutable payloads. `01-SPEC.md` §5's name-capture inventory
(lines 714-760) enumerates six frozen sites, including `actor_display_name` on
**every** say/pose/OOC line (`pkg/plugin/comm/builder.go:41,48,55`) and
`published_scenes.participants_snapshot` / `content_entries[].speaker`, which are
served **unauthenticated**.

Nothing in v0.13 Phase 3 depends on the false half — `RenameCharacter` was removed
from the milestone (03-CONTEXT.md D-44) — so this blocks nothing today. But
backlog Phase 999.20 will reason directly from this entry and MUST NOT inherit a
claim that is already untrue.

## Solution

Pick one; all three are recorded with tradeoffs in `03-CONTEXT.md`:

1. **Narrow it to lifecycle transitions.** Amend the summary to "retire and idle
   MUST NOT release the name"; rename releasing a name becomes explicit sanctioned
   behavior. Makes the text true with the smallest change. Leaves the
   identity-inheritance hazard open for rename.
2. **Widen the enumeration.** List rename as a third release path with the hazard
   explicitly accepted and recorded — the precedent `INV-WORLD-4` set when it went
   TWO→THREE in 02-12 ("what was false was the enumeration and not the guarantee").
3. **Make the code match the text.** A former-names table keeps every prior
   `normalized_name`/skeleton reserved and the gate checks it alongside the live
   unique index; a hard delete drops that character's rows, so the two tombstone
   paths remain the *only* release paths. The literal text becomes true for rename
   as well as retire, and the hazard genuinely closes.

**Whichever is chosen, the coverage hole must close too** — extend the binding test
to exercise rename, or the same silence recurs. Verify with:
`task test -- -run 'TestEveryRegistryInvariantHasBinding|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`
then `go run ./cmd/inv-render`.

Related open question for the same design pass: `INV-WORLD-4` currently enumerates
exactly THREE sanctioned out-of-world writers, and 03-CONTEXT.md's D-42
(`last_active_at` write seam) may add a fourth.
