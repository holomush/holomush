# Phase 6: Admin Portal Shell & Character Administration - Context

**Gathered:** 2026-08-13
**Status:** Ready for planning

<domain>
## Phase Boundary

Wire the **already-shipped** admin-section authorization substrate to a real RPC
surface and a web console: character administration as the one working section,
six planned sections gated-and-refusing, audit emission on every admin mutation,
and the portal shell with its nav, table, edit Sheet and not-found page.

**The phase is not "build the gate."** Phase 2 already built it. `internal/admin/section/`
ships the seven-entry registry (`registry.go:102-116`), the mandatory descriptor with
boot validation wired into `setup.BootstrapSubsystem.Prepare` (`boot.go:33`), the shared
gate helper `AssertSectionAccess` (`gate.go:234`), the resource-type-scoped
`seed:admin-section-access` policy (`internal/access/policy/seed.go:987`), and the
set-equality meta-test (`registry_test.go:44-54`). **`AssertSectionAccess` has zero
production callers.** Phase 6 wires what exists.

**Consequence for planning — verify before rebuilding (rule `7zy1161fh1`):**
EXT-01, EXT-03 and EXT-04 appear **already satisfied** by Phase 2, and ADMIN-01/ADMIN-02's
*mechanism* exists. The planner MUST confirm each against the tree rather than
re-implementing it. What is genuinely absent is the RPC surface, the interceptor,
the audit emission, and every web artifact.

**Explicitly NOT in this phase:** role mutation (PORTAL-08, #4899); admin impersonation;
break-glass identifiers; a SQL console (all four excluded by §10.8). There is **no
`AdminDeleteCharacter`** — `world.Service.DeleteCharacter` MUST NOT be wired to an admin
button (§4.4, §10.6). There is **no rename** in v0.13 (D-44); sketch 004's `Rename…`
affordance is not live.

</domain>

<decisions>
## Implementation Decisions

### Authorization plumbing

- **D-99:** **The gate is carried by a method→section descriptor table plus an
  interceptor**, not by a per-handler call. Mirrors the shipped `internal/plugin/hostcap/`
  pattern: a descriptor table (`descriptor.go:69`), an interceptor enforcing it before
  dispatch (`interceptor.go:191`), and a meta-test making an undeclared method **fail
  closed** (`descriptor_completeness_test.go:21,41`, binding INV-PLUGIN-52/53). An admin
  method with no section declaration is refused (`ADMIN_SECTION_NOT_DECLARED`), never
  defaulted. Rationale: ADMIN-02's per-handler redundancy protects against a human
  forgetting; a fail-closed descriptor **removes** the forgetting rather than surviving
  it — the same argument Phase 4 made in D-79 (a compile fence beats a lint rule).
  The interceptor mounts **core-side only**: `.claude/rules/gateway-boundary.md` and
  §10.4 both put an `internal/web/` authorization decision in the wrong process.
  — **Reversibility:** costly — reverting to per-handler assertion means touching every
  admin handler and deleting the descriptor table plus its two meta-tests; the ADMIN-02
  amendment would also have to be withdrawn.

- **D-100:** **Two registry RPCs, section id as a parameter — never as a method name.**
  `AdminListSections` returns the viewer-permitted sections with their `status`;
  `AdminGetSection(section_id)` gates on the supplied id, then returns
  `SECTION_NOT_IMPLEMENTED` for the six planned and OK for `characters`. §10.2's
  seven denial assertions iterate `section.All()` against this **one** endpoint, each
  with its paired positive control, so the non-vacuity claim survives without seven stub
  RPCs. Rationale (maintainer, this discussion): a section is an **ABAC resource family**
  (`internal/access/prefix.go:71`), not a menu — but manufacturing seven RPCs whose only
  job is to be denied binds the wire surface to UI ordering. Id-as-data keeps the
  enumeration and drops the coupling. D-06's gate-then-distinguish ordering is preserved:
  `AssertSectionAccess` runs **before** `section.Lookup`.
  — **Reversibility:** one-way — both RPCs and their `Web`-prefixed proxies are published
  wire contract and census members; removing either is a breaking change to every caller
  and a §3.4 census edit.

- **D-101:** **The admin nav is a server-filtered projection of `AdminListSections`.**
  No mirrored section registry in `web/src/`. Rationale: §10.1 names a client-only
  registry as Pitfall 7's hazard, and a server-returned set survives a future
  per-section grant with no client change (today `seed:admin-section-access` is
  resource-**type** scoped, so the permitted set is all-or-nothing).
  — **Reversibility:** reversible — swapping to a client mirror is a web-side change plus
  a cross-language census test.

- **D-102:** **`repeated string roles` still ships on `WebCheckSessionResponse`**
  (ADMIN-08 / §10.5.1.1 as written) — player-scoped and singular, **never** a
  per-character map. It exists so the rail can decide whether to draw the `/admin`
  entry at all without an `AdminListSections` round trip on every authed layout load
  for the sessions that are not admins. It changes only what is drawn and is **never**
  the authorization boundary; drawing a link the viewer may not use still denies at the
  RPC.
  — **Reversibility:** one-way — §10.5.1.1 states explicitly that reshaping this field
  later is a wire-compat change to every caller.

### Audit emission

- **D-103:** **Before-values are split by field kind.** The world payload taxonomy
  deliberately carries neither before- nor after-values for profile prose:
  `BuildCharacterProfileUpdatePayload` (`internal/world/payloads.go:445-466`) carries the
  changed attribute **names** only, because *"Profile prose is player-authored personal
  content and the taxonomy's payload rule is new-values-only AND erasure-safe"*, and
  `payloads_test.go:976` pins the convention. Every one of §10.6's 13 allowlisted paths
  is prose. Therefore:
  - `AdminUpdateCharacter` emits **changed attribute names only** — no prose values,
    before or after. The before-**version** rides the outbox delta
    (`internal/world/outbox/wire.go:28`), where it already travels.
  - `AdminRetireCharacter` / `AdminUnretireCharacter` **do** carry the before-`status`,
    because a lifecycle enum is not player-authored content and is the one before-value
    that is both meaningful and safe.
  - Every admin envelope carries the evaluated `section` and `action` (§10.7).

  Rationale: ADMIN-06's *"the before-values are the whole point"* would copy player prose
  into `events_audit`, a **retained** table, undoing the erasure-safe property already
  designed in. Encrypting it was considered and rejected — retained-is-still-retained,
  encryption is not erasure, and crypto-shredding is a separate mechanism this milestone
  does not ship. An auditor still learns who, when, which character, which fields, and
  for lifecycle transitions what the value was.
  — **Reversibility:** one-way — widening the payload later is additive and easy;
  **narrowing** it is not, because prose already written into `events_audit` partitions
  is retained and cannot be recalled.

- **D-104:** **The envelope `Actor` is `player:<id>` and the acting character is
  omitted.** `Caller.subject` is carried verbatim into the envelope Actor and its byte
  identity *"is an audit requirement, not only an authz one"* (`internal/world/caller.go:42-45`),
  so the required player id travels there — the payload need **not** duplicate it.
  §10.7's "record both" is struck: §10.5.1.2 makes the acting character
  authorization-irrelevant (switching alts changes nothing), so recording it writes a
  durable player↔alt linkage into a retained table for no audit value — exactly the
  disclosure Phase 4's D-27 kept off the read path.
  — **Reversibility:** one-way — same retention argument as D-103.

- **D-105:** Emission goes through the **transactional outbox seam** in the same
  transaction as the state change (§9.3, `INV-WORLD-1`), reusing the shape
  `UpdateCharacterDescription` already uses. The `events_audit` row is **projected**
  from that envelope by the asynchronous audit projection, which stays the only writer
  to that table. **No direct `INSERT INTO events_audit`** — §14 row 9 is explicit that no
  transactional write path into that table exists and that building one would bypass the
  codec / `dek_ref` / dedup contract `writeAuditRow` maintains.

### Character administration surface

- **D-106:** **`AdminSearchCharacters` does substring matching on both
  `characters.normalized_name` and the joined `players.username`**, backed by a new
  `pg_trgm` GIN migration. Chosen over prefix matching (maintainer, this discussion) for
  operator ergonomics — finding a character whose name is only half-remembered. Query is
  normalized through §6.1's pipeline before matching, and matches the **stored normalized
  name**, not the display name (§11.3). §11.3's prose-search prohibition is untouched:
  `username` is an OOC identity column the `admin` audience already sees, not profile
  content. **Planner notes:** `pg_trgm` must be creatable in the target deployment (not
  universally available on managed Postgres without a pre-approved extension list); use a
  plain `CREATE INDEX` inside goose's transaction rather than `CONCURRENTLY`, which would
  force `-- +goose NO TRANSACTION` and give up atomicity for a table this size.
  — **Reversibility:** costly — undoing means a down-migration dropping the extension and
  indexes, plus reverting the A3 SPEC amendment.

- **D-107:** **`last_active_at` renders as coarse relative text** (`2h ago`, `3d ago`,
  `never`), not an absolute timestamp — the value lags by up to one flush interval
  (accepted as AR-03-03 in Phase 3), and coarse text is honest about that without needing
  a disclaimer, where a precise stamp reads as more authoritative than it is. The `0`
  sentinel renders `never` and **sorts last in BOTH directions** (A1) — a most-recent-first
  ordering gets it free as the column minimum, but oldest-first needs an explicit
  `ORDER BY (last_active_at = 0), last_active_at ASC`. Column label is **`Last active`**,
  never `Online` or `Last seen`: sketch 008 found two "status" vocabularies colliding on
  the roster and this column must not join them. This is the **first surface to expose the
  column** — Phase 5 did not — and the `admin` audience is the safest place for it.

- **D-108:** **The admin retire confirm carries retire-specific copy**, not Phase 5's
  player-facing not-retroactive string. Phase 5's copy addresses someone choosing what to
  reveal about their own profile, which an admin is not. The honest content for this actor
  corrects the likelier misconception — that retire is a takedown. It states: retiring
  takes the character out of active play; it does **not** hide them (the public profile
  stays visible and already-published poses and scenes are unchanged); the **name stays
  reserved**; and it can be undone. This is how PROFILE-12's retirement half (D-91) is
  discharged in v0.13.

### Web surface (from sketch findings — carried, not re-decided)

Locked before this discussion; recorded so the planner does not reopen them:

- Three-column frame; the admin nav **merges into the section rail** at 768–1023px
  (`.rail-btn.is-context`); below 768px both collapse to a `.mobilebar` + drawer holding
  rail and admin sections under two group labels. Container queries (`@container vp`),
  never media queries.
- Dense data table with **inline hover row actions**. No multi-select, no bulk operations.
  Click-header sort only — **no sort dropdown, no facet panel** (§11.3 names these as the
  warning sign).
- Planned-section empty state is **minimal**: glyph, name, `Registered and gated. No
  handler yet.` No gate trace, no scope preview.
- Edit surface is a **Sheet**, two groups: `Managed elsewhere` **first and collapsed**,
  then `Editable here`. `version` is header metadata — never a row, never editable.
  Status is a **transition picker that never sends a status value**; `idle` is shown but
  never selectable.
- Sheet is a **380px right overlay** at every band except `<768px`, where exactly one
  `@container vp (max-width:767px)` block flips it to `side="bottom"`. It stays an
  **overlay, never a route** — which deliberately keeps deep-linkable edit surfaces and
  their not-found obligation out of scope.
- `/admin` is **invisible without permission** — no rail icon, no nav entry, and a deep
  link renders the **ordinary** not-found page, never a redirect and never a bespoke
  "forbidden" page. Indistinguishability is **per-viewer, not global** (an admin seeing
  their own section resolve is the gate working).
- Not-found copy is **`Home`** — never "Back to HoloMUSH" (branding INV-6). The game's own
  display name reaches no web surface (#4905), so it cannot be used.
- Ten shadcn components to add: `table`, `pagination`, `empty`, `alert`, `avatar`,
  `breadcrumb`, `skeleton`, `select`, `field`, `sonner`.

### Additions to the above from this discussion

- **D-109:** **The bottom-sheet grab handle is dropped.** 006-B shipped a grabber, which
  promises drag-to-dismiss, without the gesture. Closing is backdrop tap, Escape, and the
  explicit Cancel/×. Rationale: `bits-ui`'s Sheet has no swipe-dismiss, so honoring it
  means hand-rolling pointer-drag or adding a drawer dependency — disproportionate for one
  gesture on one band of a gated console — and the milestone's posture is to never show an
  affordance it cannot honor (sketch 009 refuses to promise name availability on the same
  grounds).

- **D-110:** **The mutation loop sequence.** On success: the row enters a pending state,
  the **Sheet closes**, the row updates **in place from the response** (never a refetch),
  and the toast **names the RPC** — the toast is the receipt for a finished action, so a
  Sheet still open behind it is ambiguous, and the Sheet's `version` header metadata would
  be stale. On `Aborted` (version conflict): the **Sheet stays open**, keeps the typed
  text, shows the conflict inline with the fresh version, and focuses the first conflicting
  field — no toast, because nothing finished. This mirrors Phase 5's D-93 behavior exactly,
  verified live in `05-UAT.md` test 3.

### Claude's Discretion

- Where the method→section descriptor table lives (alongside `internal/admin/section/`
  versus a new package), and the exact `ADMIN_SECTION_NOT_DECLARED` code spelling —
  `gate.go` already documents a six-code taxonomy to extend.
- Whether `AdminListSections` and `AdminGetSection` land on `CharacterAccessService` or a
  new admin-facing service. The census (§3.4) must gain both plus their `Web`-prefixed
  proxies either way.
- Relative-time granularity buckets for D-107, and the exact confirm-copy wording for
  D-108 (the four clauses are fixed; the phrasing is not).

</decisions>

<amendments_owed>
## Amendments Owed

**File as issues; do NOT hand-edit `ROADMAP.md` / `REQUIREMENTS.md` / `STATE.md`** — they
are tool-owned generated artifacts and no `gsd-tools` verb rewrites an existing phase's
success criteria (rule `a32nfcekfc`; the same reason #4963 exists).

| # | Artifact | Amendment | Source |
| --- | --- | --- | --- |
| 1 | `REQUIREMENTS.md` ADMIN-02 + `01-SPEC.md` §10.4 | "Every admin RPC re-asserts its own gate … as its first statement" no longer describes what ships. The gate is asserted **once, by an interceptor**, over a fail-closed method→section declaration. §10.4's "the redundancy is the point" is replaced by a structural guarantee. | D-99 |
| 2 | `REQUIREMENTS.md` ADMIN-06 + `01-SPEC.md` §10.7 | Before-values are scoped **away from prose**: name-only for the 13 allowlisted paths, real before-`status` for lifecycle transitions. Erasure-safety reason stated. | D-103 |
| 3 | `01-SPEC.md` §10.7 | Strike "record both" — the acting character is not recorded. | D-104 |
| 4 | `01-SPEC.md` §9.2 and §11.3 | Sketch **A3**: `AdminSearchCharacters` "searches names" → "searches character names and player usernames". Never landed in §14 despite being ACCEPTED as the design (ROADMAP Phase 6 sketch-findings line, D-81). §11.3's prose-search prohibition is unchanged. | D-106 |
| 5 | `01-SPEC.md` §9.2/§9.3 + §3.4 census | Add `AdminListSections` and `AdminGetSection` and their `Web`-prefixed proxies as census members. | D-100 |
| 6 | `ROADMAP.md` Phase 6 `Requirements:` line | Add **PROFILE-12** — it currently lists only ADMIN-01..08 + EXT-01..04, so D-91's retirement half has no home (flagged in `05-VERIFICATION.md:176`). | D-108 |
| 7 | `ROADMAP.md` Phase 6 success criterion 3 | Says an admin mutation "writes an `events_audit` row in the same transaction". §14 row 9 already amends this — the envelope is transactional, the row is **projected**. Ensure the criterion is read through that amendment and not to its letter. | D-105 |

**Already open and inherited:** #4963 (four Phase-5 text amendments, two of which — items 2
and 4 — are this phase's), #4903 (`+error.svelte` does not exist), #4905 (game display name
reaches no web surface), #4899 (player-wide vs per-character role semantics — the reason
role mutation is excluded).

**Settled, do NOT reopen:** #4904 (the §10.3/§10.4 denial-code oracle) is **CLOSED**. Phase 2's
D-06 gate-then-distinguish — ABAC evaluates **before** the registry lookup — means a denied
caller always receives `DENY_ADMIN_SECTION`; only a permitted caller can ever see
`DENY_ADMIN_SECTION_UNREGISTERED`. Pinned by `INV-PRIVACY-11` with a differential
string-equality assertion demonstrated RED against a lookup-first ordering.

</amendments_owed>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The normative source for this phase
- `.planning/phases/01-portal-spec/01-SPEC.md` §10 — Admin Information Architecture: the
  registry, the mandatory descriptor, gate-after-order, the authorization shape, the
  per-player verdict, the field-mask allowlist, audit emission, and the four exclusions.
  **§10 is normative and a planner writes the registry from it without choosing a gating
  model.**
- `.planning/phases/01-portal-spec/01-SPEC.md` §9.2–§9.3 — the admin read and mutation RPC
  surface (`AdminListCharacters`, `AdminSearchCharacters`, `AdminGetCharacter`,
  `AdminUpdateCharacter`, `AdminRetireCharacter`, `AdminUnretireCharacter`) and the
  `Web`-prefixed proxy pairing.
- `.planning/phases/01-portal-spec/01-SPEC.md` §9.4–§9.5 — `expected_version` as an `int32`
  scalar on every guarded mutation (absent or zero is **rejected**, never "skip the guard"),
  and the four update-mask rules.
- `.planning/phases/01-portal-spec/01-SPEC.md` §11.3 — the six permitted sort/filter fields
  and nothing else; the "no sort dropdown, no facet panel" prohibition.
- `.planning/phases/01-portal-spec/01-SPEC.md` §4.4–§4.5 — retire vs idle-out vs purge;
  retire MUST NOT release the name; what a retired character looks like.
- `.planning/phases/01-portal-spec/01-SPEC.md` §14 — the amendments table. **Rows 9 and 12
  are load-bearing for this phase** (the `events_audit` projection boundary; the two added
  §11.3 sort rows).
- `.planning/phases/01-portal-spec/01-SPEC.md` §12 — Verification Integrity, and §2.6's
  census-by-set-equality discipline that §10.1 applies to the registry.

### Prior-phase context that constrains this phase
- `.planning/phases/02-abac-schema-vocabulary/02-CONTEXT.md` — D-06 (gate-then-distinguish),
  D-24/D-25 (`last_active_at` column and its `0` sentinel), D-26 (the §11.3 amendment).
- `.planning/phases/04-shared-facade-helpers-characteraccessservice/04-CONTEXT.md` — D-27
  (scalar-`owner` audience split; why alt linkage stays off the read path), D-72/D-77
  (privacy by absence-at-the-descriptor), D-79 (a compile fence beats a lint rule), D-81.
- `.planning/phases/05-character-identity-ui-public-profiles/05-CONTEXT.md` — D-85 (`/c/[id]`
  outside `(authed)`), D-91 (PROFILE-12's retirement half moved here), D-93 (the section
  boundary is the save boundary — the conflict behavior D-110 mirrors).
- `.planning/phases/05-character-identity-ui-public-profiles/05-VERIFICATION.md:176` — the
  flag that Phase 6's `Requirements:` line omits PROFILE-12.

### Design findings
- `.claude/skills/sketch-findings-holomush/SKILL.md` — all ten sketches. Read
  `references/anti-patterns.md` (17 entries) **before drawing anything**, plus
  `references/shell-and-navigation.md`, `references/data-tables.md`,
  `references/gating-and-absence.md`, `references/forms-and-destructive-actions.md`.
- `.planning/sketches/MANIFEST.md` — the locked-decisions table and the A1/A2/A3/D1 ledger.

### Repo rules that bind this phase
- `.claude/rules/gateway-boundary.md` — protocol translation only; **structural writes use
  typed RPCs, never `sendCommand`**. Every admin mutation in this phase is a structural
  write.
- `.claude/rules/grpc-errors.md` — never leak inner errors past the boundary; translate at
  **one** layer; assert **top-level** codes via `oops.AsOops(err).Code()` for opacity
  contracts, not chain-walking `errutil.AssertErrorCode`.
- `.claude/rules/database-migrations.md` — one `NNNNNN_name.sql` per version carrying both
  directions; idempotent; `BIGINT` epoch-nanoseconds, never `TIMESTAMPTZ` (INV-STORE-1).
- `.claude/rules/invariants.md` — a GSD milestone SPEC is **outside** the orphan check's walk
  root, so any new `INV-<SCOPE>-N` this phase mints MUST be hand-registered in
  `docs/architecture/invariants.yaml`.
- `.claude/rules/testing.md` — ACE naming; `// Verifies: INV-…` bindings; never bind a
  Skip/placeholder test.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable assets — already shipped, do NOT rebuild
- `internal/admin/section/registry.go:102-116` — the seven-entry registry in §10.1's row
  order, with the descriptor **derived** from the id via `access.AdminSectionResource`
  rather than written literally, and re-derived by `validateEntries` so a hand-written
  literal is caught rather than trusted.
- `internal/admin/section/gate.go:234` — `AssertSectionAccess`, the shared helper.
  **Zero production callers.** Documents a six-code taxonomy: `DENY_ADMIN_SECTION`,
  `DENY_ADMIN_SECTION_UNREGISTERED`, `ADMIN_SECTION_REGISTRY_INVALID`,
  `ADMIN_SECTION_DESCRIPTOR_MISMATCH`, `ADMIN_SECTION_ACTION_NOT_DECLARED`,
  `SECTION_NOT_IMPLEMENTED` — plus `ADMIN_SECTION_EVALUATION_FAILED`, which deliberately
  does **not** collapse into `DENY_ADMIN_SECTION` so an outage never renders as an
  authorization answer (§8.10).
- `internal/admin/section/boot.go:33` — `ValidateAtBoot`, already called from
  `setup.BootstrapSubsystem.Prepare`; a non-nil return aborts startup.
- `internal/access/policy/seed.go:987` — `seed:admin-section-access`, scoped by resource
  **type** (`resource is admin_section`), which is what buys "every future section at zero
  policy cost" (EXT-07). Pinned by exact-DSL assertion including a `NotContains` on
  `admin_section:` to prove it is not enumerated.
- `internal/access/prefix.go:71,296` — `ResourceAdminSection` and `AdminSectionResource`,
  which panics on an empty section id.
- `internal/plugin/hostcap/` — **the pattern D-99 transposes.** `descriptor.go:69`
  (`Descriptors` table), `interceptor.go:191` (`NewCapabilityInterceptor`),
  `descriptor_test.go:19` (`TestEveryServedCapabilityHasADescriptor`),
  `descriptor_completeness_test.go:21,41` (completeness + fail-closed proof).
- `internal/admin/auth/operator_admin.go:37-64` — `AssertOperatorAdmin`, the in-tree
  first-statement precedent §10.4 cites, with its lockstep rationale at
  `internal/admin/auth/ingame.go:117-118`.
- `web/src/lib/nav/sections.ts:35-47,63-67` — the `as const satisfies` registry shape §10.1
  says to mirror, and `visibleSections`, the single gate the rail and palette both flow
  through (ADR `holomush-stds8`).
- `web/src/lib/components/shell/SectionRail.svelte` — rail geometry, active-bar treatment,
  drawer variant.

### Established patterns that constrain this phase
- **New-values-only, erasure-safe envelope payloads** — `internal/world/payloads.go:445-466`
  and `payloads_test.go:976`. The reason D-103 exists.
- **`Caller.subject` byte identity is an audit requirement** — `internal/world/caller.go:42-45`.
  The reason D-104 works without a payload field.
- **Same-transaction outbox emit** — `internal/world/service.go:816-828`
  (`buildIntent` → `mutator.*`), the shape §9.3 mandates. Character kinds already defined at
  `service.go:52-58`, with the command→kind parity table at `mutator.go:100-105`.
- **`events_audit` has exactly one writer** — `internal/eventbus/audit/projection.go:434`
  (`writeAuditRow`), plus the retention-partition mover. A second writer is forbidden.
- **Set-equality censuses over hand-maintained lists** — the milestone's recurring
  discipline (`registry_test.go:44-54`, `gate_test.go:114-152` iterating `All()` with paired
  positive controls, never hard-coded).

### Integration points
- **Proto:** `api/proto/holomush/characteraccess/v1/` — six admin RPCs plus the two section
  RPCs to add; `api/proto/holomush/web/v1/web.proto:802-822` — `WebCheckSessionResponse`
  gains `repeated string roles` (D-102). `task proto && task web:generate`, and commit
  `pkg/proto/**/*.pb.go` + web `*_pb.ts` in the same change or CI fails the stale-diff check.
- **Storage:** a new migration for `pg_trgm` + GIN indexes (D-106). Existing:
  `characters_normalized_name_key` UNIQUE btree (`000056:68-69`), `last_active_at BIGINT
  NOT NULL DEFAULT 0` (`000054:33`), `characters.version INTEGER` (`000049:22`).
- **Repository:** `internal/world/postgres/character_repo.go` has `ListByPlayer` (`:340`,
  `ORDER BY name`) and `ListAll` (`:708`) — **no** join to `players.username` and no search
  method exist yet.
- **Web:** no `+error.svelte` exists **anywhere** under `web/src/routes/` (#4903). Phase 6
  builds the single root boundary and ships a meta-test asserting **exactly one** — a second
  boundary destroys the indistinguishability three surfaces rest on with nothing failing.
- **Gates:** `abac-reviewer` MUST run (this phase touches `internal/access/`).
  `crypto-reviewer` is **not** triggered by D-103, which deliberately keeps prose out of the
  payload — but it fires if that decision is revisited toward the encrypted option.

</code_context>

<specifics>
## Specific Ideas

- **"Why would we bind RPCs to UI layout/ordering concerns?"** — the maintainer's pushback
  that produced D-100. It is the reason the section id is a **parameter** rather than a
  method name, and it should be treated as a standing test for any later addition to this
  surface: if the wire shape would change when the menu is reordered, it is wrong.
- The admin retire confirm's four required clauses (D-108): out of active play; **not**
  hidden — profile stays visible and published history is unchanged; the name stays
  reserved; this can be undone.
- The `never`-sorts-last requirement (D-107) is a **both-directions** property. Most-recent-first
  gets it free because `0` is the column minimum; oldest-first needs the explicit
  `ORDER BY (last_active_at = 0), last_active_at ASC`. A test that only exercises one
  direction passes under the bug.

</specifics>

<deferred>
## Deferred Ideas

- **Drag-to-dismiss on the phone bottom-sheet** — D-109 drops the handle rather than
  building the gesture. If a later milestone adds a drawer dependency for other reasons,
  the handle and its gesture can return together.
- **The audit log viewer** — the `audit` section stays `planned` and refuses after its gate.
  Emission ships now (D-103/D-105) precisely so the viewer has history when it is built
  (backlog 999.8).
- **Role mutation and the `players` section** — excluded by PORTAL-08 / §10.8 while
  `PlayerHasRole` is player-wide. Belongs in the deferred `players` section after #4899 is
  decided.
- **Prose/content search over profile fields** — a filter over privacy-bearing rows wearing
  a different name; §11.1's prohibition reaches it. D-106's substring search is confined to
  `normalized_name` and `players.username`.
- **Player-initiated self-retire** — IDENT-04 defers it beyond v0.13; only the admin path
  ships here.
- **Character rename** — left v0.13 by D-44 for backlog Phase 999.20; cannot be specified
  until the identity model gains an approval dimension.
- **Exposing the game's display name to the web** (#4905) — blocks any player-facing game
  identity. Phase 6 sidesteps it by using `Home` as the not-found copy.

</deferred>

---

*Phase: 6-admin-portal-shell-character-administration*
*Context gathered: 2026-08-13*
