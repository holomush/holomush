# Data Tables & List States — the admin character list

Validated in sketch **002-admin-character-table**. Winner: **A — inline row actions**.

The character list is the **only working section** in v0.13's admin portal, so it carries
the whole weight of "does this feel like a real admin tool".

## Design Decisions

### Row actions are inline on hover

Hover reveals Edit / Retire / ⋯ at the row end. Fewest clicks for a single-row operation,
which is the common case here (rename, retire, inspect).

**Rejected:**
- **B — row → 330px detail pane.** Preserved for comparison. Costs a click, but does
  surface `version`, which every mutation needs. Did not earn its width.
- **C — multi-select + bulk bar. Removed at maintainer request.** Removing it also retired
  the open question about bulk operations having no SPEC backing: **§9's admin RPCs are all
  singular**, so a bulk bar would have implied either N sequential calls with
  partial-failure UX, or a batch RPC the census does not contain.

**There is no multi-select and no bulk operation in this portal.**

### Sorting is click-header only; filters are inline controls

SPEC §11.3 names *"a sort control whose options are drawn from the §7.2 field list"* as
**the specific warning sign**, because that list is the privacy-bearing set.

So: **no sort dropdown, no facet panel.** Conventional admin-table instincts (sort by
anything, facet everything) are prohibited by construction, not by taste.

### A total count is safe *here* specifically

§11.3's "notably absent" list forbids `total_count` on a **privacy-partitioned** list. The
admin list is **not** privacy-partitioned — its audience already sees every field it could
order by, which is precisely why it is the one permitted surface.

So `12 of 412` is safe here and would **not** be safe on the public directory. State this
explicitly wherever it appears, because the next surface to want a count will cite this one.

### `version` belongs in the UI

Every mutation must send `expected_version` (§9.4); `CreateCharacter` is the sole carve-out.
Surfacing `version` makes the optimistic-concurrency contract visible rather than a hidden
409. A stale value is how a concurrent edit gets refused instead of silently overwriting.

### Four distinct non-data states

`data` / `loading` / `no results` / `zero characters`. **`no results` and `zero characters`
are deliberately different** — different copy, different actions. A filtered-to-nothing list
offers "clear filters"; an empty game offers something else entirely. Do not collapse them.

## Column contract — what §11.3 permits vs. what this sketch does

| Column | §11.3 today | This sketch | Needs |
| --- | --- | --- | --- |
| `name` | Sort ✓ Filter ✓ (matches the **normalized** name, §6.1.3) | sortable | — |
| player (`players.username`) | `player_id`: Sort ✗, Filter ✓ equality | **sortable** + click-to-filter | **A2, A3** |
| `status` | Sort ✓ Filter ✓ | sortable | — |
| `last_active_at` | **not in schema, not in §11.3** | **sortable** | **A1** |
| `created_at` | Sort ✓ Filter ✓ | sortable | — |
| `description`, `location_id`, all `profile.*` | excluded | absent | — |

None of the amendments touches a `profile.*` row, so §11.2's three reasons are untouched.

## ⚠ Three SPEC amendments — NOT yet sanctioned

The table as sketched **exceeds what SPEC §11.3 permits**. This was a deliberate,
maintainer-directed choice. **Do not implement these believing they are already sanctioned
— they must be amended into `01-SPEC.md` before Phase 6 builds them.**

### A1 — `last_active_at` does not exist and cannot be derived

| Candidate source | Why it fails |
| --- | --- |
| `sessions.updated_at` | Sessions are **reaped**. `idx_sessions_active_character` is a partial unique index over `status IN ('active','detached')`; once a session ends the row does not survive as history. |
| `session_connections.last_seen_at` (`000046`) | A **gateway lease**, refreshed while the socket is open, reaped by the lease sweep (`internal/session/reaper.go:29`). Dies with the connection. |

Both answer **"online now"**, not "last active". Required:

1. A durable column — `characters.last_active_at BIGINT NOT NULL DEFAULT 0` (Phase 2
   migration; nullable-or-defaulted per the migration rules; the repo stores epoch
   **nanoseconds** as `BIGINT` after `000042`).
2. A write path at **session start** — **not** on lease refresh
   (`RefreshConnection`, `session.go:485`), which would be a hot write per character per
   lease interval.
3. A §11.3 row permitting sort + filter. It qualifies on §11.3's own terms: intrinsic row
   metadata carrying no profile content, exactly like `created_at`.

**`0` / never must render as `never`** — not blank, not the epoch — and must sort to the
**end in both directions**. It is an absence, not a very-old value; burying it under a
descending sort hides precisely the rows an admin is hunting for (created-but-never-played
characters).

### A2 — sorting by player

§11.3's `characters.player_id` row reads Sort: **No** — *"Equality filter only — grouping a
player's alts — never an ordering."*

The distinction that matters: **what this column sorts is `players.username`, not
`characters.player_id`.** Ordering by an opaque ULID is useless; ordering by username is
what an admin means. `players.username` is on a different table §11.3 never enumerates, so
it falls under "every other column is excluded" by default rather than under the
`player_id` row's explicit prohibition.

Justified by §11.3's own safety test: *"the admin list is the permitted surface precisely
because its audience already sees every field it could order by."* **Leave the `player_id`
row as written — add a new row.** Silently relaxing it would be wrong; it is correct as-is.

### A3 — searching player usernames

`AdminSearchCharacters` (§9.2) *"searches **names**"* — meaning character names. Filtering
or searching by player username extends its scope. Small, but it is a census-visible RPC
contract and should not drift silently.

## HTML / interaction structures

- The **`Player` header deliberately has no sort affordance** while its neighbours do
  (pending A2). If that reads as broken rather than intentional, that is a SPEC
  conversation, not a UI fix.
- Player name is **click-to-filter** (equality), which is what §11.3 already permits.
- Column headers are the only sort affordance.
- Columns 4 and 5 drop below 768px (see `shell-and-navigation.md`).

## What to Avoid

- **Inventing a `Last seen` / `Last login` column.** It is not in the schema. This is
  flagged as *"the single most likely column for Phase 6 to invent by reflex"* — it is the
  default instinct for any admin list, and §11.3 would exclude it from sorting even if it
  existed. See `anti-patterns.md`.
- **A sort dropdown or facet panel.** §11.3 names these as the specific warning sign.
- **Multi-select, bulk bars, batch retire.** No SPEC backing; all admin RPCs are singular.
- **`total_count` on the public directory.** Safe here *only* because this list is not
  privacy-partitioned.
- **Collapsing `no results` into `zero characters`.** Different states, different copy.
- **Writing `last_active_at` on lease refresh.** Hot write per character per lease interval.

## Still open

**Where the `last_active_at` write lands.** Session start is the right hook, but the
specific seam is a Phase 2/3 decision — the session store's create path is the obvious
candidate.

## Origin

Synthesized from sketch: **002**
Source file: `sources/002-admin-character-table/index.html`
(state dropdown cycles all four non-data states in every variant)
