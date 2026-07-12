# Phase 5: World-Model Integrity Fixes (M2 / M12) - Context

**Gathered:** 2026-07-12
**Status:** Ready for planning

<domain>
## Phase Boundary

Implement the MODEL-01 ADR (`holomush-i4784`) decision — **Option B: CRUD-canonical
world state + optimistic-concurrency version guard + transactional outbox / ordered
atomic feed** — to close the two proven world-model correctness findings and correct
the stale docs:

- **MODEL-03** — version guard closes last-write-wins (M12, #4798).
- **MODEL-04** — transactional outbox / ordered atomic feed closes dual-write
  non-atomicity (M2).
- **MODEL-02** — downgrade the false "event sourcing / state derives from replay"
  doc principle at its ~6 sites to "event-driven with an append-only audit log."

The **mechanism is NORMATIVE and fully specified** by the ADR and the consensus
one-pager (panel-ratified). Discussion did NOT re-open the mechanism; it clarified
the scope-boundary calls the ADR explicitly deferred to "Phase 5's spec."

**Fixed by the ADR/one-pager (NOT open for planning to re-decide):**
- Version-predicated CAS on **writes AND deletes** (`… WHERE id=$1 AND version=$2`);
  any zero-row result classified by a **locked follow-up read in the same transaction**
  (conflict vs concurrent-delete vs not-found).
- `version INTEGER NOT NULL DEFAULT 1` on all four world tables (locations, exits,
  characters, objects); `Version` field added to the corresponding Go structs.
- Exactly **one semantic envelope per successful externally-visible command**, committed
  in the **same transaction** as the state change; intent-level, new-values-only payloads;
  one tombstone per aggregate on delete.
- Commit-ordered, gap-free `feed_position` from a **locked per-game counter** (NOT
  `BIGSERIAL`, NOT insert-time allocation — the commit-order proof depends on this).
- A **single leased relay** publishing strictly in position order, `Nats-Msg-Id` = event
  ULID for dedup, `LISTEN/NOTIFY` wakeup + periodic sweep, halt-and-alert poison posture.
- **Compile-time write-requires-envelope seam** (`mutate(ctx, entity, expectedVersion,
  envelope)` — an envelope-less write is a type error) + census meta-test + lint fence
  forbidding raw world-table SQL outside `internal/world/postgres`.
- Conflicts surfaced strictly as typed `WORLD_CONCURRENT_EDIT`; **no automatic retry in
  the first slice** (compare-before-retry is explicitly deferred, telemetry-gated).
- Four invariants to **register AND bind** in this phase's spec: `INV-WORLD-ATOMIC-FEED`,
  `INV-WORLD-DELTA-PARITY`, `INV-WORLD-FEED-ORDER`, `INV-WORLD-WRITER-BOUNDARY`.
- The two-replica resilience suite is the **permanent D-05 regression gate**, extended
  with fault injection (relay crash around PubAck, dual relay, duplicate delivery, broker
  downtime) and per-aggregate race tests.
- **Zero product projections in Phase 5** — only a *reference* idempotent consumer +
  bootstrap harness. Genesis snapshot events emitted once at cutover; feed epoch/reset
  procedure for DB restores/backfills.
- The post-commit emit path (`EmitMoveEvent`, `EVENT_EMITTER_MISSING`) is **deleted
  outright**.

**Out of scope (deferred by the ADR/one-pager):**
- Real event sourcing / state replay / rebuild for world state (permanently forgone).
- Compare-before-retry conflict semantics (later, narrowly, if telemetry justifies).
- Any *product* feed consumer/projection (Phase 5 ships only the reference consumer).
- ARCH-04 unified event-model collapse (Phase 7 — Phase 5's taxonomy registry is its input).

</domain>

<decisions>
## Implementation Decisions

### Emission rollout scope (MODEL-04 slice 3)
- **D-01:** **Full mechanical emission rollout in Phase 5** — the outbox envelope is
  wired through **every** world write command (~15-20 types), plus the versioned taxonomy
  schema registry and the census meta-test that enumerates every write command → declared
  envelope kind. This matches the NORMATIVE one-pager slice 3 literally. The
  write-requires-envelope seam + census meta-test structurally forbid leaving any command
  un-migrated (no allow-list of pending commands). This is the largest slice; plan its
  waves accordingly.

### Conflict surfacing depth (MODEL-03)
- **D-02:** Phase 5 stops at the **typed `WORLD_CONCURRENT_EDIT` error at the
  `world.Service` boundary.** The telnet-message / web-retry-affordance mapping table is
  a **separate UX slice, NOT in Phase 5 scope** (captured under Deferred). Phase 5 proves
  the integrity mechanic; presentation wiring comes later. The typed error + its
  error-code registration ARE in scope.

### WR-01 pre-existing test finding
- **D-03:** **Fold WR-01 into MODEL-04 slice 2.** Slice 2 deletes the post-commit emit
  path (`EmitMoveEvent` / `EVENT_EMITTER_MISSING`) and rewrites
  `test/integration/resilience/m2_dualwrite_test.go` to assert the **new outbox behavior**
  — so WR-01's wrong-mechanism assertion (it asserts `EVENT_EMIT_FAILED` where the real
  chain is `EVENTBUS_PUBLISH_EXPIRED`/`EVENTBUS_PUBLISH_FAILED` via a go-retry ctx-expiry
  accident) disappears with the code it described. No separate pre-Phase-5 fix/PR. The
  planner MUST also correct the M2 "Mechanism" paragraph in
  `f1-resilience-verdict.md` when the emit path is deleted, so the evidence doc stays true.

### Delivery / PR structure
- **D-04:** **One phase PR** — all four slices verify, then the phase lands as a single
  reviewable PR. Slices remain the internal wave/commit ordering (guard → outbox+MoveCharacter
  → taxonomy+rollout → doc downgrade), but there is no slice-per-PR ceremony. The
  crypto/abac gates apply to the whole diff at push time (likely neither triggers — this is
  world-model + migrations, not `internal/access/` or `internal/eventbus/crypto/`; the
  planner MUST confirm the outbox relay path doesn't touch a crypto-gated file).

### World-write scope boundary (round-4 review resolution — `scene_participants` is OUT)
- **D-05:** The world-integrity mechanism (version guard + outbox + SQL fence + INV-WORLD-4)
  covers the **four core world tables ONLY** — `locations`, `exits`, `characters`, `objects` —
  **plus** folding `internal/store/character_settings_repo.go`'s `UPDATE characters` (a genuine
  envelope-less `public.characters` writer) into the guarded/versioned path. `scene_participants`
  is **explicitly OUT of scope**, resolving the round-4 Codex "plugin escapes the fence" finding
  as a schema-blind false positive: there are **two separate `scene_participants` tables** —
  `plugin_core_scenes.scene_participants` (core-scenes plugin's own schema, migration
  `plugins/core-scenes/migrations/000003`, plugin-owned, written only by the plugin's own store)
  and `public.scene_participants` (core baseline `000001`, world layer) which has **no live
  production write path outside `internal/world`** (the plugin manages scenes in its own schema;
  `world.Service.AddSceneParticipant` has no prod callers outside `internal/world`). Consequences
  the planner MUST honor:
  - The AST SQL fence MUST be scoped to the **core/world schema + the four tables (+ `characters`)**
    and MUST **exclude the `plugins/` tree and `scene_participants`** — do not flag the plugin's
    own-schema writes, and do not add `scene_participants` to the guarded/version/outbox surface.
  - A **follow-up GitHub issue** (NOT Phase 5) captures: verify whether the world-layer
    `internal/world/postgres/scene_repo.go` / `public.scene_participants` is live or vestigial
    legacy, and either model it or remove it; and any future "outbox for plugin-owned tables" work.
  - This keeps Phase 5 at its original CONTEXT scope (four tables + zero product projections) — it
    does NOT reach into a product plugin's storage.

### Claude's Discretion
- Internal wave decomposition within each slice, migration numbering, exact Go
  package placement of the outbox relay + mutation wrapper, and the reference-consumer
  shape — all left to research + planning against the NORMATIVE one-pager.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The MODEL-01 decision (NORMATIVE mechanism — read first)
- `docs/adr/holomush-i4784-world-state-model-decision.md` — the Accepted decision;
  Decision + Consequences sections define Phase 5's input contract and the four
  ordered slices.
- `docs/reviews/arch-review/2026-07-11/verification/model-01-consensus-onepager.md` —
  **NORMATIVE** mechanism shape. "Core mechanics", "Enforcement", "Consistency/UX/lifecycle",
  and "Phase 5 first slice (ordered)" are the implementation spec. Where this and the ADR
  differ in detail, this one-pager is authoritative for the mechanism.

### Empirical grounding (the findings Phase 5 must close)
- `docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md` — the
  empirical M12 (reproduced) / M2 (characterized) verdict; its "Mechanism" M2 paragraph
  MUST be corrected when slice 2 deletes the emit path (D-03).
- `docs/reviews/arch-review/2026-07-11/verification/f1-eventsourcing-why.md` — archaeology
  of *why* world state is CRUD-not-event-sourced (grounds MODEL-02 doc downgrade).

### Requirements + invariants governance
- `.planning/REQUIREMENTS.md` — MODEL-02/03/04 acceptance wording; OPS-05 (done).
- `.claude/rules/invariants.md` — the register/bind ratchet the four `INV-WORLD-*`
  invariants MUST follow (mint in this phase's spec; NOT before).
- `.claude/rules/database-migrations.md` — idempotent paired up/down; the version-column
  migrations follow this.
- `.claude/rules/event-conventions.md` / `.claude/rules/event-interfaces.md` — subject
  naming, `core.NewEvent()`/ULID identity-vs-ordering, `Nats-Msg-Id` dedup.

### MODEL-02 doc-downgrade sites (~6)
- root `CLAUDE.md`, `site/src/content/docs/contributing/explanation/architecture.md`,
  coding-standards doc, public `site/src/content/docs/.../index.mdx` (planner: grep the
  "state derives from replay" phrasing to enumerate the exact current set).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/store/Transactor.InTransaction` (used by `DeleteLocation`, totp repo) — the
  same-transaction seam for the outbox write (state change + envelope in one tx).
- `access_policies.version INTEGER NOT NULL DEFAULT 1` + `access_policy_versions` — the
  in-schema optimistic-concurrency precedent to mirror for the four world tables.
- `internal/eventbus/audit/projection.go` — the halt-and-alert / DLQ poison posture the
  relay's poison handling mirrors; also the `events_audit` write path (NOT world tables).
- The two-replica resilience harness under `test/integration/resilience/` +
  `internal/testsupport/integrationtest` seams (`WithExternalNATS`/`WithSharedDatabase`) —
  the D-05 gate to extend with fault injection.

### Established Patterns
- `internal/world/service.go` `checkAccess` is **pre-write** and stays there — ABAC runs
  before any guarded write; the relay publishes already-authorized, already-committed facts.
- `internal/world/mutator.go` (the ADR calls it `entity_mutator.go` — drift; real path is
  `mutator.go`) + `UpdateCharacterDescription` are the read-modify-write sites that must
  thread the read version into the guarded `UPDATE`.
- `internal/world/events.go` — today only ~4 events emit (move/examine/object_create/
  object_give); the full-rollout (D-01) inverts this to one envelope per write command.

### Integration Points
- Production `world.Service` is constructed in `internal/world/setup/subsystem.go` with
  **no `EventEmitter`** — the move-notification leg is currently dead code. Phase 5 replaces
  it with the outbox relay wiring, not an emitter.
- Outbox cleanup coordinates through the **OPS-02 (Phase 6) RetentionWorker** machinery —
  Phase 5 lands the outbox + prune-after-PubAck; retention bounding is OPS-02.
- The versioned taxonomy schema registry is the designated **ARCH-04 (Phase 7) input**.

</code_context>

<specifics>
## Specific Ideas

- Slice order is fixed by the one-pager and MUST be preserved: (1) version columns +
  guarded repos + strict conflict surfacing, resilience M12 specs flipped to assert
  surfaced conflicts; (2) outbox + envelope + positioned relay, `MoveCharacter` end-to-end
  + reference idempotent consumer + bootstrap harness + relay lag/halt alerting **in this
  same slice** (a halted ordered feed is otherwise silent); (3) taxonomy registry + census
  meta-test + invariants registered, then mechanical emission rollout across the remaining
  commands + genesis emission at cutover; (4) MODEL-02 doc downgrade.
- Relay lag/halt **alerting lands in slice 2**, not deferred — an ordered feed that halts
  is silent without it.

</specifics>

<deferred>
## Deferred Ideas

- **Conflict-surfacing UX slice** — telnet message + web retry affordance for
  `WORLD_CONCURRENT_EDIT` (the mapping table). Its own future slice/phase (D-02).
- **Compare-before-retry conflict semantics** — explicitly deferred by the one-pager;
  add later, narrowly, only if telemetry justifies (retry only if the original field is
  unchanged, re-run `checkAccess` + validation).
- **Product feed consumers/projections** — Phase 5 ships only the reference consumer;
  real consumers are follow-on.
- **ARCH-04 unified event-model collapse** — Phase 7; consumes Phase 5's taxonomy registry.
- **Real event sourcing / world-state rebuild** — permanently forgone per the ADR.

</deferred>

---

*Phase: 5-World-Model Integrity Fixes (M2 / M12)*
*Context gathered: 2026-07-12*
