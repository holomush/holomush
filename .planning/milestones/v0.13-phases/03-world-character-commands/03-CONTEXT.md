# Phase 3: World Character Commands - Context

**Gathered:** 2026-08-06
**Status:** Ready for planning — AFTER the ROADMAP amendment in D-44 lands

> **Decision numbering continues Phase 2's sequence (D-01..D-30) deliberately.**
> This phase amends Phase 2 decisions D-24 and D-30 and must not collide with
> them when both files are read together.

<domain>
## Phase Boundary

`world.Service` gains a soft **`RetireCharacter`** / **`UnretireCharacter`** pair at
the domain layer — version-guarded, emitting through the transactional outbox
in-transaction — plus the host-side reactor that makes "a retired character leaves
active play" actually true, and the `writeCommands` census rows and taxonomy kinds
in the same change.

The phase also closes Phase 2's deferred `last_active_at` write seam (D-24) with a
**separate, general-purpose** character-activity subsystem (D-42). It is listed here
because D-24 assigned the seam to this phase, **not** because it belongs to
retirement — retirement merely reads the attribute. Treat the two subsystems as
independent work: they share no code and their only relationship is landing in the
same phase.

**`RenameCharacter` is NOT in this phase and NOT in this milestone.** See D-44. The
phase goal, success criterion 1, and requirements IDENT-03 (and the rename half of
IDENT-10) move to the backlog linked to Phase 999.6 Character Rostering & Transfer.

Requirements remaining in scope: **IDENT-04** (soft retire), **IDENT-10** (the
`expected_version` + in-transaction outbox guarantee, retire/unretire half).

</domain>

<decisions>
## Implementation Decisions

### Retire command shape

- **D-31:** Two task-based commands — **`RetireCharacter`** and
  **`UnretireCharacter`**, neither taking a status argument. Rejected: a single
  `SetCharacterLifecycle(status)`. Sketch 004 pins the UI contract as "sends
  `AdminRetireCharacter` / `AdminUnretireCharacter` — never a status value", and a
  set-state command would make "retire and idle-out stay distinct operations"
  caller discipline rather than a type-level property.
  — **Reversibility:** costly — each command is a `writeCommands` census row and a
  published taxonomy kind; collapsing them later rewrites the census bijection and
  orphans an emitted kind.

- **D-32:** **Two new taxonomy kinds and two new census rows** —
  `character_retired`, `character_unretired`. This is **forced, not chosen**:
  `test/meta/world_envelope_census_test.go:84-88` fails with *"kind %q has two
  in-Service producers … the in-Service bijection is one-producer-of-record"*, so
  two commands cannot share a kind, and reusing `character_updated` collides with
  `UpdateCharacterDescription`. (Originally three kinds; `character_renamed` is
  withdrawn with D-44.)
  — **Reversibility:** one-way — a taxonomy kind is a published wire contract with
  durable audit rows carrying it.

- **D-33:** Retire is a **narrow write**: `status`, version bump, and one envelope
  in one transaction. Session teardown and grid removal are NOT in the command's
  transaction — they are the reactor's job (D-36). Rejected: pulling the session
  store into a `world.Service` mutation transaction.

  **CORRECTION (2026-08-06, from 03-RESEARCH.md — the decision stands, its cited
  precedent was wrong).** The narrow-write *intent* is intact, but
  `CharacterRepository.Rename` is **NOT** the precedent to copy and the
  "MUST NOT route through `worldMutator.mutate`" rule recorded for Rename is
  **Rename-specific — it MUST NOT be generalized to Retire.**

  `TestWorldEnvelopeCensusMatchesServiceMutatingMethods`
  (`test/meta/world_envelope_census_test.go:187-207`) is a **set-equality** check
  in BOTH directions between `world.WriteCommands()` and the `go/ast` set of
  `*Service` methods in `internal/world/service.go` whose body contains the
  selector `s.mutator` (detector: `serviceMutatingMethods` :115-141 →
  `bodyReferencesSelector(fn.Body, recvName, "mutator")` :136). Success criterion 4
  requires census rows, so **`RetireCharacter`/`UnretireCharacter` MUST be
  `*Service` methods in `service.go` that route through `s.mutator`.** A registered
  descriptor whose method does not reference `s.mutator` fails the census, and a
  method that does reference it without a descriptor fails it too.

  The Rename shape would not even compile from `Service`: `Service.characterRepo`
  is a read-only `CharacterReader` (`internal/world/service.go:101`).

  **The correct precedent is `Service.UpdateCharacterPreferences`
  (`internal/world/service.go:856-890`)** — it satisfies every property D-33 wants
  simultaneously: a `*Service` method, a narrow single-concern write, routed
  through `s.mutator`, reading `char.Version` as the CAS guard, surfacing
  `ErrConcurrentEdit` → `CodeConcurrentEdit` (success criterion 3), and owning its
  own taxonomy kind `kindCharacterPreferencesUpdate` (`internal/world/mutator.go:99`)
  — which is exactly the D-32 two-distinct-kinds shape.

- **D-34:** When the retiring character is the player's `players.default_character_id`,
  retire **clears that pointer in the same transaction**. The FK is
  `ON DELETE SET NULL`, so it self-heals only on hard delete; a soft retire would
  otherwise leave the login paths (`internal/telnet/gateway_handler.go`,
  `internal/web/auth_handlers.go`) reading a pointer to a retired character.
  Consequence to honor: retire now writes the `players` row too.

- **D-35 [informational]:** ~~Rename is status-agnostic.~~ **SUPERSEDED by D-44** — rename leaves
  the milestone, so this decision has no subject. Recorded because it was reached
  and then withdrawn; it must not be silently re-derived.

### The retirement reactor

- **D-36:** The fanout is an **event-driven host subsystem** consuming
  `character_retired` off `events.*.character.>`, NOT a synchronous call from an
  out-of-Service application service. Chosen over the synchronous form (which
  would have mirrored `internal/auth/auth_service.go:248-256`'s eviction fanout)
  because it is the seam every future lifecycle consumer plugs into and cannot be
  bypassed by a new caller of the domain command.
  **Accepted consequences, stated explicitly:** `SubsystemID` 18→19 and its
  compile cascade; at-least-once JetStream redelivery, so the handler MUST be
  idempotent; retirement becomes eventually consistent.
  **Grounding:** the durable-consumer pattern already exists —
  `internal/eventbus/audit/projection.go:108-129` creates its consumer via
  `createConsumerWithRetry`.

  **CORRECTION (2026-08-06, from 03-RESEARCH.md) — two claims above were false:**

  1. **"5-site compile cascade" is wrong — it is 13 sites across 5 files.** It
     includes two *fixed-size* `[18]stubSubsystem` array declarations, three
     separate `18` length assertions, two real-constructor lists, and an **exact
     ordered 18-element pinned start sequence** (`core_topo_order_test.go:194-213`)
     into which each new ID must be inserted at a topologically-correct position.
     The current count is **18**, verified from the const block. Grep the sites;
     do not trust this number either (memory `e2nxxx9v5d`).

  2. **`createConsumerWithRetry` is NOT reachable as shared substrate.** It is
     **unexported** (`internal/eventbus/audit/projection.go:167`) and both
     production callers are inside `package audit` (`projection.go:109`,
     `plugin_consumer.go:194`). Its doc comment's "shared by" means shared
     *within that package*. "Only the subsystem wiring is new" was therefore
     false. Resolved by **D-46**.
  — **Reversibility:** costly — a registered lifecycle subsystem with a durable
  JetStream consumer name; removing it strands the consumer.

- **D-37:** A retired character lands at the **configured starting location** —
  the already-resolved `starting_location_id` (`internal/bootstrap/setting.go:133-144`,
  exposed by `setup.StartLocationID()`; "The Nexus" under `setting-crossroads`,
  "The Void" under `setting-skeleton`). No new manifest key, no new schema.
  Consequence to honor: retired characters accumulate at the new-player arrival
  point in `characters.location_id`; they are NOT *present* there, because presence
  is session-derived.

- **D-38:** The move runs **through `world.Service.MoveCharacter` as a system
  subject** (precedent: `"system:bootstrap"` at `internal/bootstrap/setting.go:134`),
  not a direct location write. Keeps the write inside the sanctioned world-mutation
  path rather than adding a fourth out-of-world writer under INV-WORLD-4, and gives
  the `MoveCharacter → MovementHook → UpdateLocationOnMove` pipeline a genuine
  production exercise — window #2 / issue #4788 records it as currently covered
  only by SIMULATED moves. Consequence: retirement emits two envelopes
  (`character_retired`, then `character_moved`), and the system subject must
  satisfy the character-write ABAC gate.

- **Reactor v1 scope fence:** sessions only — `EmitLeave` to the OLD location,
  `EmitSessionEnded`, session teardown, then the move. Scenes, channels, and any
  future pages/stories consumers are explicitly NOT in v1 and are iterated on later.

  **CORRECTION (2026-08-06, from 03-RESEARCH.md):** the CONTEXT's earlier count of
  **four** existing leave-fanouts is wrong — there are **eight** `EmitLeave` call
  sites across seven flows. `command_handler.go:268`, `command_handler.go:330`, and
  `auth_handlers.go:716` were missed. Survey all eight before deciding what the
  reactor reuses versus duplicates.

- **D-46 (2026-08-06, maintainer decision — resolves the D-36 correction):**
  `createConsumerWithRetry` and its `consumerCreateBackoffs` table **move to a
  neutral shared package** (e.g. `internal/eventbus/consumer`), and the two
  existing audit callers (`projection.go:109`, `plugin_consumer.go:194`) are
  updated to use it. This makes D-36's "shared substrate" claim actually true.
  Rejected: **exporting in place** (a character-retirement reactor importing
  `internal/eventbus/audit` is a layering inversion — retirement is not audit, and
  it invites every future non-audit consumer to do the same); **duplicating the
  ~20-line helper** (two retry implementations that drift, and the backoff
  schedule is exactly the operational constant that must not diverge);
  **no retry** (loses the bounded-retry resilience the audit projector
  deliberately has, on a subsystem whose silent failure stops retirement fanout
  entirely).
  Consequence to honor: this phase touches `package audit` and its tests. Preserve
  each caller's existing error wrapping — the host wraps with
  `AUDIT_CONSUMER_CREATE_FAILED` via `wrapConsumerCreateError`, the plugin path
  with `AUDIT_PLUGIN_CONSUMER_CREATE_FAILED` via `wrapPluginConsumerCreateError`
  (`projection.go:162-166`). The move MUST NOT change either code.

### Authorization

- **D-39:** IDENT-04's "their **own** character" is enforced in **ABAC policy, not
  in the command**. The commands call `checkAccess` and assert no ownership
  predicate of their own. Admin capability therefore becomes a policy grant with
  zero new domain code.
  Consequence to honor: "their own" is true only as long as the seed says so —
  nothing in the type system prevents a future over-broad permit. This is the same
  failure shape D-29 deferred out of Phase 2; the Phase 4 projection-narrowing work
  should treat it as related.

- **D-40:** **Distinct ABAC actions** — `retire` and `unretire` against
  `CharacterResource` — not a reuse of the existing `write` action. Now that
  ownership is policy-enforced (D-39), action granularity is the only lever policy
  has. Splitting retire from unretire is deliberate: it permits a future policy
  where a player may retire their own character but only an admin may un-retire.
  (A `rename` action is withdrawn with D-44.)

- **D-41 [informational]:** ~~Admins may rename; §9.3's census gains `AdminRenameCharacter`.~~
  **DEFERRED with D-44.** The question — *"Does the admin portal expose rename?"*,
  open in sketch 009's table and flagged by the ROADMAP as must-answer-in-this-phase
  — moves to the backlog item with rename. Sketch 004's `Rename…` affordance is
  therefore **not** live in v0.13, and sketch 009's finding #5 ("names are
  reserved, not permanent") is **false for v0.13** and must be corrected.

- **D-45:** SUPERSEDED 2026-08-07 by D-47. The decision below is recorded because it was
  reached, acted on, and withdrawn; it MUST NOT be silently re-derived. Its premise —
  that the reactor needs a *synthetic principal* whose authority is described by static
  policy — was wrong. See D-47 for what replaced it and why. The struck text follows.

- ~~**D-45 (2026-08-06, maintainer decision — closes the gap D-38 flagged):** the
  retirement reactor authorizes its `MoveCharacter` call with a **seeded `system:`
  string subject plus a new ABAC seed permit** for `principal is system` ×
  `resource is character`. Precedent to copy: `"system:bootstrap"` +
  `seed:system-bootstrap-world` / `seed:system-bootstrap-exits`
  (`internal/access/policy/seed.go:206-217`).~~

  **Why this was a real gap, not a formality.** The only `principal is system`
  permits that exist today are `resource is location` (`seed.go:209`) and
  `resource is exit` (`seed.go:215`). ABAC is default-deny, so absent a new seed
  the reactor's move is **denied** and success criterion 2 cannot pass.

  **Rejected: the `access.WithSystemSubject` bypass.** That route works —
  `internal/access/policy/engine.go:91-105` returns `EffectSystemBypass` and skips
  policy evaluation entirely — and has a live precedent at
  `internal/grpc/location_follow.go:197`. It was rejected because it puts a brand-new
  subsystem *outside* the default-deny chokepoint and grants it unchecked
  world-write authority. Also rejected: bypass-now-seed-later (leaves an ABAC hole
  that later phases inherit).

  **Mechanics to honor.** The bypass at `engine.go:92-93` requires **both**
  `req.Subject == "system"` (the bare literal) **and** `access.IsSystemContext(ctx)`;
  a bare `"system"` subject WITHOUT the context marker is a hard
  `SYSTEM_SUBJECT_REJECTED` error (the S1 defense). The seeded route deliberately
  does neither — it uses a *prefixed* subject (e.g. `"system:retirement"`, which is
  not `"system"`) so evaluation proceeds through normal policy, exactly as
  `"system:bootstrap"` does. Do NOT stamp `WithSystemSubject` on the reactor's
  context.

  Consequence to honor: this phase adds an ABAC seed, so the **`abac-reviewer` gate
  fires before push** (`/holomush-dev:review-abac`). Keep the permit as narrow as
  the DSL allows; a blanket `action in ["read","write"]` on every character
  resource is the ceiling, not the target.

- **D-47 (2026-08-07, maintainer decision — SUPERSEDES D-45):** the reactor's
  authorization is **not Phase 3's to solve**. It is deferred to a new
  **Phase 02.1 — Background-Job Authorization Model**, which Phase 3 now depends on.

  > **Renumbered later the same day (2026-08-07): the Background-Job Authorization
  > Model is now Phase 02.2, not 02.1.** Its `/gsd-discuss-phase` session split the
  > world *caller model* out ahead of it into a new **Phase 02.1 — World Caller
  > Model**, because `world.Service`'s `subjectID string` argument cannot carry
  > execution context at all and that defect predates background jobs. Phase 3 now
  > depends on 02.2 directly and 02.1 transitively. See
  > `.planning/phases/02.2-background-job-authorization-model/02.2-CONTEXT.md`
  > D-56. Every "Phase 02.1" in the D-47 text below means **02.2**.

  **Why D-45 was wrong.** It asked "what should we call this principal?" and then
  designed static policy vocabulary to describe the answer. That conflates **ABAC
  policy definition** with **the runtime state policy evaluates against**. The correct
  question is what attributes the job carries *while it is executing*. Three candidates
  were examined against the tree and all three fail:

  1. **Synthetic `system:retirement` principal (D-45's answer) — unnarrowable.**
     `parseEntityType` (`internal/access/policy/engine.go:542-548`) returns the prefix
     only, so `principal is system` matches `system:bootstrap` identically. Any permit
     written for the reactor also grants the setting-bootstrapper. A `when { principal.id
     == "retirement" }` guard *does* work mechanically (`resolver.go:197-198` stamps
     `bags.Subject["id"]` provider-independently; `evaluator.go:406,434`) — but it
     conditions authorization on an **identity string**, which is the same shoe-horn one
     level up: rename the subject and the permit silently stops matching.
  2. **Borrow the originating actor off the envelope — over-grants.** The envelope does
     carry it (`Envelope.Actor`, set from the same `subjectID` that passed `checkAccess`,
     `service.go:1079`; the whole envelope is serialized into `Event.Payload`,
     `outbox/wire.go:147,176-177`). But a player authorized to *retire their own
     character* was never authorized to end sessions, emit to a location, or move a
     character. Propagating either over-grants or fails — neither is correct.
  3. **`access.WithSystemSubject` — outside the chokepoint.** It works
     (`engine.go:91-105` → `EffectSystemBypass`) and `wire.go:175-176`'s
     "already-authorized, already-committed facts" contract arguably endorses it, but it
     puts every future background consumer outside default-deny.

  **What Phase 02.1 builds instead:** a first-class job identity **plus per-execution
  attributes the policy engine can test**. The carrier already exists and is unused on
  this path — `types.NewAccessRequest(subject, action, resource, attrs)`
  (`types/types.go:143`) overlays per-call attrs onto `bags.Action` (`engine.go:258-265`),
  reachable in the DSL as `action.*`. The load-bearing blocker is that
  `world.Service.checkAccess` hardcodes `nil` (`service.go:214`). Together they make
  **instance-scoped** authority expressible — *"this job may write only the aggregate
  whose event it is currently handling"* — which no static grant list can express, and
  which is the concrete reason a grants-only design was rejected.

  **Consequence for Phase 3:** the ABAC seed work leaves this phase entirely. The 5 plans
  produced on 2026-08-06 predate this decision and MUST be reconciled against it before
  execution — 03-01's seed tasks and 03-04's authorization path are the affected surfaces.

### The `idle` state

- **D-43:** Phase 3 writes **no** `idle` transition. This is settled by a bound
  invariant, not by preference: **INV-WORLD-5** states *"v0.13 ships the 'idle'
  value with no transition into it, so the binding test MUST construct a character
  directly in 'idle' (bypassing the absent transition) and assert the exclusion."*
  01-SPEC §4.4:573 agrees — `idle-out` is "**Not implemented in v0.13** — the value
  ships, the transition does not." Retire, idle-out and purge remain three distinct
  operations (PORTAL-04).

### Rename leaves the milestone

- **D-44:** **`RenameCharacter` is removed from Phase 3 and from the v0.13
  milestone**, and moves to the backlog **linked to Phase 999.6 Character
  Rostering & Transfer**, carrying this discussion as its rationale.

  **Why.** Rename cannot be specified correctly until the character identity model
  gains an **approval dimension**, which does not exist:
  1. `characters.status` today is `active | retired | idle` only
     (`000054_character_identity_and_lifecycle.sql`); there is no `not_approved` /
     `approved` concept anywhere in the codebase.
  2. The intended rule — *rename is permitted only before a character is approved
     for play; after that the core name is immutable* — has no state to read. The
     honest substitute ("has this character ever emitted a comm payload") means
     querying the audit log to authorize a write.
  3. Designing that dimension touches a bound invariant (INV-WORLD-5's closed
     vocabulary and exhaustive-switch rule), 01-SPEC §4.4, PORTAL-04, character
     creation's initial state, and an approving actor — which is an admin surface,
     i.e. Phase 6. That is milestone-scale and must not be absorbed into a
     two-command phase. (Phase 2's three failed review cycles came from exactly
     this shape; memory `2sgg7pvmbh`.)

  **Two frictions recorded for the backlog item's design work:**
  - The session/lifecycle split is **already structural** — session status lives on
    the session row (`internal/session/session.go:21`:
    `StatusActive | StatusDetached | StatusExpired`), character lifecycle on
    `characters.status`. The roster's "two vocabularies" problem is a *rendering*
    problem, not a missing column. A `session_status` column on `characters` would
    duplicate an existing one.
  - The proposed value `rostered` **collides with a reserved meaning**:
    REQUIREMENTS v2:269 defines rostering as *"a distinct transition out of
    retired, which is why retire must not release the name."* And `retired` is
    already a lifecycle value that 01-SPEC §4.4 deliberately keeps distinct from
    `purge`. The approval axis and the lifecycle axis should not be merged.

  **What stays true regardless:** retire, unretire and the reactor have **zero**
  dependency on approval and land in this phase. IDENT-04 closes in v0.13.
  — **Reversibility:** reversible — this is a scope decision recorded in ROADMAP
  and REQUIREMENTS; nothing is built or unbuilt by it.

### The `last_active_at` write seam

- **D-42:** `last_active_at` is written through a NATS JetStream KV buffer with a
  periodic flush, in its OWN general-purpose subsystem. Resolved 2026-08-06.

  **Why a buffer rather than a throttled direct `UPDATE`.** Both shapes are
  correct; the direct form (`UPDATE … WHERE id = $1 AND last_active_at < $2`) is
  bounded by the throttle window, not by event volume, so at this scale Postgres
  load was never the deciding factor. The buffer wins because the **emit path never
  touches Postgres at all**, which is a latency property on the hot path and leaves
  real headroom. Do not re-argue this on DB-load grounds.

  **The listener** covers session start/end AND character-generated activity —
  every event already carries an `Actor` whose kind is `character`, so a broad
  subscription gives "this character did something" directly. Same subscription
  shape the audit projector uses. It MUST NOT hook
  `internal/session/session.go:485` `RefreshConnection` (a hot write per lease
  interval — the sketch finding and D-24 both forbid it).

  **The KV bucket MUST set `Storage: jetstream.FileStorage` explicitly.** A KV
  bucket carries its own storage config and does **not** inherit the stream's.
  Production already runs file-backed JetStream — `internal/eventbus/subsystem.go:63-65`
  ("FileStorage is the default; tests override via NewSubsystemWithStorage") with a
  resolved `StoreDir` at `:214-222` — but that is the *stream*. A bucket left at the
  default in a memory-configured test harness will silently lose unflushed writes.
  This is the **first use of JetStream KV in the codebase**; there is no in-repo
  precedent to copy, only the stream config above.

  **CORRECTION (2026-08-06, from 03-RESEARCH.md) — the decision stands, the stated
  hazard is INVERTED.** Setting `Storage` explicitly is still right, but NOT for
  the reason recorded above. In nats.go v1.52.0 (the pinned version),
  `FileStorage StorageType = iota` (`stream_config.go:610-611`) — so **`FileStorage`
  is the ZERO VALUE.** A bucket that omits `Storage` is therefore file-backed
  *everywhere*, including in tests. The real hazard is the mirror image of the one
  recorded: **file-backed KV residue leaking into a `MemoryStorage` test harness**,
  not silently-lost unflushed writes in a memory-configured one.

  Two consequences for the plan:
  - The new subsystem needs a `…WithStorage` seam (mirroring the eventbus
    subsystem's `NewSubsystemWithStorage` test override) so tests can force
    `MemoryStorage`.
  - The API is `js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{…})`;
    read the pinned config fields from `kv.go:210-275` rather than from memory.

  Also corrected: the CONTEXT cites `subsystem.go:214-222` as `resolveStoreDir` —
  **:214 is the call site**; the function itself is at **:490-501**.

  **The flusher is its OWN subsystem — NOT part of the D-36 retirement reactor.**
  `last_active_at` is a general-purpose character attribute that retirement merely
  *happens* to read; co-locating them would couple a shared concern to one consumer.
  Consequence to honor: **Phase 3 adds TWO subsystems, `SubsystemID` 18→20, i.e.
  two separate 5-site compile cascades** (const-block END, stringer regeneration,
  `productionSubsystems` named params, the fixed-size `stubSubsystem` arrays,
  `core_topo_order_test` `require.Len`). Grep the count; do not trust a plan's
  arithmetic (memory `e2nxxx9v5d`).

  **The flusher is a fourth out-of-world writer under INV-WORLD-4** (which
  enumerates exactly THREE and was already amended TWO→THREE in 02-12). Amend the
  enumeration deliberately, in the same change, exactly as 02-12 did — "what was
  false was the enumeration and not the guarantee". The deeper question this raises
  — whether the world-writer fence should distinguish world-state writers from
  operational ones at all — is filed as a separate todo and is NOT Phase 3's to
  answer.
  — **Reversibility:** costly — a registered subsystem, a durable KV bucket, and an
  amended bound invariant.

  **Left to the planner** (values, not design): the throttle window (research
  precedent is 5 min – 1 hr; 5 min is a reasonable default), the flush interval, and
  the bucket name. Readers needing real-time accuracy must consult KV, not the
  column — the column lags by up to one flush interval by construction.

### Claude's Discretion

- Reactor fanout ordering: `EmitLeave` at the OLD location BEFORE the move, so the
  notification names the place they left.
- A new `SessionEndedCause` constant for retirement, alongside
  `quit | logout | guest_end | kicked | reaped | evicted`
  (`internal/core/session_ended_payload.go:25-30`).
- Reactor idempotency by checking current state, so a JetStream redelivery no-ops
  rather than double-emitting `leave`.
- `UnretireCharacter` on an already-active character returns a typed error rather
  than silently succeeding (house fail-loudly pattern).
- Payload and error-code shapes follow the existing `BuildCharacterUpdatePayload` /
  `CodeConcurrentEdit` precedent in `internal/world/service.go:799-836`.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Normative for this phase — read first
- `.planning/phases/01-portal-spec/01-SPEC.md` §4.4 (lines 566-591) — the lifecycle
  model: `retire`, `idle-out`, `purge` as three distinct operations; `purge` is NOT
  a state; `purge` MUST NOT be wired to a player-facing affordance.
- `.planning/phases/01-portal-spec/01-SPEC.md` §5 (lines 714-760) — the
  **name-capture surface inventory**: six `historical` (frozen) sites vs five `live`
  sites, with a per-site verdict. Required reading before any future rename work.
- `.planning/REQUIREMENTS.md` — IDENT-04, IDENT-10 (in scope); IDENT-03 (removed,
  see D-44); PORTAL-04 (lifecycle states); PROFILE-12 (history is not retroactive).
- `.planning/phases/02-abac-schema-vocabulary/02-CONTEXT.md` — D-24/D-25
  (`last_active_at` primitive and its `0` sentinel), D-30 (confusable enforcement by
  serialization), D-28 (the `charname/syntax` leaf).

### Invariants this phase touches
- `docs/architecture/invariants.yaml` **INV-WORLD-4** — the writer boundary and the
  enumeration of exactly THREE sanctioned out-of-world writers (already amended
  TWO→THREE by 02-12). Relevant to D-42.
- `docs/architecture/invariants.yaml` **INV-WORLD-5** — lifecycle-exhaustive reads
  over the closed vocabulary; `idle` ships with no transition. Settles D-43.
- `docs/architecture/invariants.yaml` **INV-WORLD-6** — retire preserves the name
  reservation. The **retire half is unaffected and stands**; see the deferred item
  about its rename half.
- `.claude/rules/invariants.md` — how to define/respect/bind registry invariants.

### Code the phase modifies or must match
- `internal/world/service.go:745-856` — `DeleteCharacter` /
  `UpdateCharacterDescription`, the nearest-analog guarded mutations.
- `internal/world/mutator.go:60-115` — `WriteCommandDescriptor` and the explicit
  closed `writeCommands` set.
- `internal/world/outbox/taxonomy.go:52-56` — the character kind constants.
- `test/meta/world_envelope_census_test.go:62-88` — the in-Service bijection that
  forces D-32.
- `internal/world/lifecycle.go` — `Status`, `ParseStatus`, `NeverActive`.
- `internal/eventbus/audit/projection.go:108-129` — the durable-consumer pattern
  D-36 copies (`createConsumerWithRetry`).
- `internal/presence/emitter.go:125-175` — `EmitArrive` / `EmitLeave`
  (publishes to `location.<id>`); `internal/presence/session_ended.go` —
  `EmitSessionEnded`.
- `internal/auth/auth_service.go:248-256`, `internal/grpc/lifecycle_handler.go:199-207`,
  `cmd/holomush/sub_grpc.go:844-870` — the four existing leave+session-ended fanouts.
- `internal/bootstrap/setting.go:133-144`, `internal/bootstrap/setup/subsystem.go:257-299`
  — starting-location resolution for D-37.
- `internal/lifecycle/subsystem.go` + `cmd/holomush/core.go:870-884` — the
  `SubsystemID` cascade D-36 incurs.

### Repo rules that constrain the implementation
- `.claude/rules/event-conventions.md` — subject naming, `eventbus.NewEvent`, kinds.
- `.claude/rules/testing.md` — TDD, ACE naming, Ginkgo for integration.
- `.claude/rules/logging.md` — `*Context` slog variants.
- `.claude/rules/database-migrations.md` — if D-42 lands a migration.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **The whole leave/session-ended fanout** already exists in four production
  variants; the reactor is a fifth consumer of `presence.Emitter`, not new machinery.
- **`createConsumerWithRetry`** is already shared between the host audit projector
  and the plugin consumer manager — the reactor's JetStream consumer is a third user.
- **`starting_location_id`** is already resolved at boot and exposed; D-37 needs no
  new config.
- **`CharacterRepository.Rename`** (`internal/world/postgres/character_repo.go:212`)
  is fully built — version-predicated CAS, `guardSkeleton` with self-exclusion,
  outbox envelope written inside its own transaction. It is now **unused by this
  phase** (D-44) but is the substrate the backlog rename item will build on. Its
  doc comment names the rule: *"Rename MUST NOT be routed through
  `worldMutator.mutate()`"* — routing it there emits two envelopes for one rename.

### Established Patterns
- Guarded mutation: `checkAccess` → `repo.Get` → build payload → `buildIntent(kind,
  aggregate, id, subjectID, payload)` → per-operation mutator executor → map
  `ErrConcurrentEdit` to `CodeConcurrentEdit`.
- The census is an **explicit closed list**, not inference — a command without a
  registered kind fails `test/meta/world_envelope_census_test.go`.
- Presence is **session-derived**, never read off `characters.location_id`.

### Integration Points
- New subsystem registers in `productionSubsystems` and the `SubsystemID` iota.
- Reactor subscribes `events.*.character.>` and switches on event `Type`.
- Envelope subject is built by `internal/world/outbox/wire.go:154` as
  `eventbus.Qualify(gameID, "<aggregate>.<id>")`.

</code_context>

<specifics>
## Specific Ideas

- The retirement experience the maintainer described: *"a move off the IC grid and
  a notification to the IC grid location the character was in about the retirement
  (if they were IC and online)"*. D-36/D-37/D-38 implement exactly this.
- The identity model the maintainer wants to reach, recorded verbatim for the
  backlog item: *"Stephen Strange will always be Stephen Strange, but maybe Dr
  Strange, or Joe Bob in particular IC situations"* — an immutable core name plus
  situational display aliases, with renames permitted only before approval.

</specifics>

<deferred>
## Deferred Ideas

- **Rename + the approval dimension** (D-44) — backlog, linked to Phase 999.6.
  Carries: the approval-state design (avoiding the `rostered` / `retired` collisions
  noted in D-44), whether admins may rename (sketch 009's open row), sketch 004's
  `Rename…` affordance, and the profile-URL key question sketch 009 flags as
  "settle before Phase 5 routes anything".
- **Immutable core name + situational display aliases** — a milestone-scale identity
  model. Present in neither v1 nor v2 requirements; `plugins/core-aliases` is a
  *command* alias plugin, unrelated. Every one of 01-SPEC §5's six frozen sites
  stamps a name, so "which name was captured" becomes a per-site question.
- **Former-names reservation table** — the mechanism that would close identity
  inheritance (a new character claiming a freed name and inheriting the prior
  character's frozen history) while still permitting rename. Compatible with any
  later alias model because it constrains reuse rather than mutation.
- **INV-WORLD-6's rename half is already false in production** — its text says a
  name becomes claimable again *only* through a tombstone-emitting hard delete via
  exactly two paths, but the shipped operator CLI `holomush character name set`
  (`cmd/holomush/cmd_character_name.go:439`, which supplies `Kind:
  "character_updated"`) frees the old name today. Its binding test
  (`test/integration/world/character_lifecycle_test.go:221`) never exercises rename,
  so nothing fails — the registry summary claims more than its binding proves.
  **File this**; it is a real registry defect independent of D-44.
- **Reactor consumers beyond sessions** — scenes, channels, and future pages/stories
  reacting to lifecycle events. The D-36 subsystem is the seam they plug into.
- **A `character_renamed` taxonomy kind** — withdrawn with D-44. Note the operator
  CLI currently emits rename as `character_updated`; whenever rename returns, that
  spelling should be reconciled rather than duplicated.

</deferred>

---

*Phase: 3-World Character Commands*
*Context gathered: 2026-08-06*
