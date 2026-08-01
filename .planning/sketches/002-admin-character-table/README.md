---
sketch: 002
name: admin-character-table
question: "With only four sortable/filterable columns permitted by SPEC §11.3, how should the dense admin list surface row actions and its non-data states?"
winner: "A"
requires_spec_amendments: [A1-last-active, A2-sort-by-player, A3-search-usernames]
tags: [table, density, row-actions, empty-state, phase-6, spec-amendment]
---

# Sketch 002: Admin Character Table

## Design Question

The character list is the **only working section** in v0.13's admin portal, so it
carries the whole weight of "does this feel like a real admin tool". Two things
constrain it hard, and neither is a matter of taste:

1. **SPEC §11.3 enumerates what may sort and filter.** Conventional admin-table
   instincts (sort by anything, facet everything) are prohibited.
2. **The schema is narrower than the UI wants to be.** See below.

## How to View

```
open .planning/sketches/002-admin-character-table/index.html
```

The **state** dropdown in the top-right cycles `data` / `loading` / `no results` /
`zero characters` — all four are live in every variant. Viewport buttons still
demonstrate the inherited C2 collapse.

## Variants

- **A: Inline row actions ★ WINNER** — hover reveals Edit / Retire / ⋯ at the row
  end. Fewest clicks for a single-row operation, which is the common case here.
- **B: Row → detail pane** — clicking a row opens a 330px right-hand pane showing
  the administrative detail and carrying the mutations. Preserved for comparison.
- ~~**C: Multi-select + bulk bar**~~ — **removed** at maintainer request. Removing
  it also retires this sketch's open question about bulk operations having no
  SPEC backing: §9's admin RPCs are all singular, so a bulk bar would have
  implied either N sequential calls with partial-failure UX or a batch RPC the
  census does not contain. Not needed, so not a problem.

Click a **player name** to apply the equality filter, and any column header to
sort — both are wired.

---

# ⚠ THREE SPEC AMENDMENTS ARE REQUIRED BEFORE PHASE 6 BUILDS THIS

The table as it now stands **exceeds what SPEC §11.3 permits**. That is a
deliberate, maintainer-directed choice, recorded here so nobody implements it
believing it is already sanctioned.

### A1 — `last_active_at` does not exist and cannot be derived

Requested: a "last active / logged in" column. It is not in the schema, and the
obvious derivations do not work:

| Candidate source | Why it fails |
| --- | --- |
| `sessions.updated_at` | Sessions are **reaped**. `idx_sessions_active_character` is a partial unique index over `status IN ('active','detached')`; once a session ends the row does not survive as history. |
| `session_connections.last_seen_at` (`000046`) | A **gateway lease**, refreshed while the socket is open and reaped by the lease sweep (`internal/session/reaper.go:29`). Dies with the connection. |

Both answer *"online now"*, not *"last active"*. Required:

1. A durable column — `characters.last_active_at BIGINT NOT NULL DEFAULT 0`
   (Phase 2 migration; nullable-or-defaulted per the migration rules, and note
   the repo stores epoch **nanoseconds** as `BIGINT` after `000042`).
2. A write path at **session start** — not on every lease refresh, which would be
   a hot write per character per lease interval.
3. A §11.3 row permitting sort + filter on it. It qualifies on §11.3's own
   terms: intrinsic row metadata carrying no profile content, exactly like
   `created_at`.

**`0` / never must render as `never`, not as a blank or as the epoch.** In this
sketch, `never` sorts to the **end in both directions** — it is an absence, not a
very-old value, and burying it under a descending sort hides precisely the rows
an admin is hunting for (created-but-never-played characters).

### A2 — sorting by player

§11.3's `characters.player_id` row reads Sort: **No** — *"Equality filter only —
grouping a player's alts — never an ordering."*

The distinction that matters: **what this column sorts is `players.username`, not
`characters.player_id`.** Ordering by an opaque ULID is useless; ordering by
username is what an admin means. `players.username` is on a different table that
§11.3 never enumerates, so it falls under "every other column is excluded" by
default rather than under the `player_id` row's explicit prohibition.

An amendment is well-founded on §11.3's own stated safety test: *"the admin list
is the permitted surface precisely because its audience already sees every field
it could order by — there is no withheld value for an ordering to disclose."* An
admin already sees usernames. The amendment should say so explicitly rather than
silently relaxing the `player_id` row, which remains correct as written.

### A3 — searching player usernames

`AdminSearchCharacters` (§9.2) *"searches **names**, not profile prose"* — meaning
character names. Filtering/searching by player username extends its scope. Small,
but it is a census-visible RPC contract and should not drift silently.

---

## What to Look For

- **Does the four-column table feel thin?** It is narrower than a conventional
  admin grid *by construction* (see Findings). If it reads as under-built, the
  fix is a SPEC conversation, not a UI one.
- **A vs B for the common case.** Most admin work here is single-character
  (rename, retire, inspect). A does it in one click; B costs a click but shows
  `version`, which every mutation needs. Does the pane earn its 330px?
- **C's bulk bar** — is bulk retire a real need at 412 characters, or is it a
  moderation-era feature arriving three phases early?
- **The `Player` header.** It deliberately has no sort affordance while its
  neighbours do. Does that read as intentional or as broken?
- **`no results` vs `zero characters`** are deliberately different states with
  different copy and different actions. Compare them.

## Findings

### 1. `last seen` does not exist — and sketch 001 fabricated it

`characters` is `id, player_id, name, description, location_id, created_at,
version, preferences` (`000001_baseline.up.sql:68-75`, plus `000049` adding
`version` and `000045` adding `preferences`); Phase 2 adds `status`. **There is no
last-seen or last-login column.** Presence is *current*-only
(`session.Store.ListActiveByLocation`), so a real "last seen" needs new storage
and a write path that does not exist.

Sketch 001's first draft carried a `Last seen` column anyway. That was invented,
and it has been corrected to `Ver` (`version`) in the same commit as this sketch.
**This is the single most likely column for Phase 6 to invent by reflex** — it is
the default instinct for any admin list — and §11.3's enumeration would exclude
it from sorting even if it existed.

### 2. What the table does now, versus what §11.3 says today

| Column | §11.3 today | This sketch | Needs |
| --- | --- | --- | --- |
| `name` | Sort ✓ Filter ✓ (matches the **normalized** name, §6.1.3) | sortable | — |
| player (`players.username`) | `player_id`: Sort ✗, Filter ✓ equality | **sortable** + click-to-filter | **A2, A3** |
| `status` | Sort ✓ Filter ✓ | sortable | — |
| `last_active_at` | **not in schema, not in §11.3** | **sortable** | **A1** |
| `created_at` | Sort ✓ Filter ✓ | sortable | — |
| `description`, `location_id`, all `profile.*` | excluded | absent | — |

Still normative and still honored: **no sort dropdown and no facet panel.** §11.3
names "a sort control whose options are drawn from the §7.2 field list" as *the
specific warning sign*, because that list is the privacy-bearing set. So sorting
is click-header only and filters are inline controls. The amendments above widen
*which intrinsic columns* may be ordered; none of them touches a `profile.*` row,
so §11.2's three reasons are untouched.

### 3. A count is safe *here* specifically

§11.3's "notably absent" list forbids `total_count` on a **privacy-partitioned**
list. The admin list is not privacy-partitioned — its audience "already sees every
field it could order by", which is precisely why it is the one permitted surface.
So `12 of 412` is safe here and would **not** be safe on the public directory.
Worth stating, because the next surface to want a count will cite this one.

### 4. `version` earns a place in the UI

Every mutation must send `expected_version` (§9.4), and `CreateCharacter` is the
sole carve-out. Surfacing `version` in the detail pane (B) makes the optimistic-
concurrency contract visible rather than a hidden 409. A stale value is how a
concurrent edit gets refused instead of silently overwriting.

## Components this implies adding

Beyond sketch 001's list: nothing new. This sketch exercises `table`,
`pagination`, `empty`, `skeleton`, `select`, `checkbox` (already installed),
`badge` (installed), and `separator` (installed).

## Resolved

**Bulk operations** — closed by dropping variant C. §9's admin RPCs are all
singular, and no bulk surface is wanted, so no batch RPC or partial-failure UX is
needed.

## Still open

**Where the `last_active_at` write lands.** Session start is the right hook, but
the specific seam is a Phase 2/3 decision: the session store's create path is the
obvious candidate, and it must **not** be the lease-refresh path
(`RefreshConnection`, `session.go:485`) or every character becomes a hot write
every lease interval.
