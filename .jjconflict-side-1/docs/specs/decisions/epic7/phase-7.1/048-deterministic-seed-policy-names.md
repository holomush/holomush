<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 48. Deterministic Seed Policy Names

> [Back to Decision Index](../README.md)

**Review finding (I2):** Seed policies used descriptive comments
(`// player-powers: self access`) but had no stable, deterministic name for
idempotent seeding. Without deterministic names, server restart could create
duplicate seeds.

**Decision:** All seed policies use the naming convention `seed:<purpose>`
where the purpose is a kebab-case description of the policy's intent:

- `seed:player-self-access`
- `seed:player-location-read`
- `seed:player-character-colocation`
- `seed:player-object-colocation`
- `seed:player-stream-emit`
- `seed:player-movement`
- `seed:player-basic-commands`
- `seed:builder-location-write`
- `seed:builder-object-write`
- `seed:builder-commands`
- `seed:admin-full-access`
- `seed:property-public-read`
- `seed:property-private-read`
- `seed:property-admin-read`

**Rationale:** Deterministic names enable idempotent seeding (upsert by name)
and allow admins to identify seed policies via `policy list`. The `seed:`
prefix prevents accidental collision with admin-created policies and enables
`policy list --source=seed` filtering.
