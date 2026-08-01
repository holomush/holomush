---
sketch: 002
name: admin-character-table
question: "With only four sortable/filterable columns permitted by SPEC §11.3, how should the dense admin list surface row actions and its non-data states?"
winner: null
tags: [table, density, row-actions, empty-state, phase-6]
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

- **A: Inline row actions** — hover reveals Edit / Retire / ⋯ at the row end.
  Fewest clicks for a single-row operation.
- **B: Row → detail pane** — clicking a row opens a 330px right-hand pane showing
  the administrative detail and carrying the mutations. Scan left, act right.
- **C: Multi-select + bulk bar** — checkbox column; selecting rows raises a
  sticky bulk action bar.

Click a **player name** in any variant to apply the equality filter, and the
column headers to sort — both are wired.

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

### 2. §11.3 makes the affordances asymmetric, and the UI must show that

| Column | Sort | Filter | Rendered as |
| --- | --- | --- | --- |
| `name` | Yes | Yes (matches the **normalized** name, §6.1.3) | sortable header |
| `player_id` | **No** | Yes — equality only, "grouping a player's alts, never an ordering" | header with **no** sort affordance; the cell is a click-to-filter link |
| `status` | Yes | Yes | sortable header |
| `created_at` | Yes | Yes | sortable header |
| `description`, `location_id`, all `profile.*` | No | No | absent |

Also normative, and implemented here: **no sort dropdown and no facet panel.**
§11.3 names "a sort control whose options are drawn from the §7.2 field list" as
*the specific warning sign*, because that list is the privacy-bearing set. So
sorting is click-header only and filters are inline controls.

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

## Open question

**Bulk operations have no SPEC backing.** §9's admin RPCs are all singular —
`AdminUpdateCharacter`, `AdminRetireCharacter`, `AdminUnretireCharacter`. Variant
C's bulk bar implies either N sequential calls (each needing its own
`expected_version`, and partial failure is then a real UX problem) or a new
batch RPC that §9's census does not contain. If C wins, that is a SPEC amendment,
not an implementation detail.
