# Phase 4: World-Model Resilience Investigation & Decision (F1) - Context

**Gathered:** 2026-07-11
**Status:** Ready for planning

<domain>
## Phase Boundary

Empirically characterize the world-model concurrency/dual-write risk (**OPS-05**),
then commit an ADR deciding the world-state model and naming the concrete mechanism
Phase 5 will implement (**MODEL-01**). This is a **decision gate** — the deliverable
is evidence + a decision, not a feature. Phase 5 (MODEL-02/03/04) depends on the ADR's
named mechanism.

**In scope:**
- A reproducible two-replica resilience/chaos harness (concurrent commands + NATS broker
  flap + replica restart + client reconnect) — #4791.
- A documented, reproducible verdict on whether last-write-wins (M12) actually corrupts
  world state under concurrency.
- A committed ADR (`docs/adr/`) recording the world-model decision (build real event
  sourcing **vs.** CRUD-canonical + optimistic-concurrency + transactional-outbox),
  grounded in the F1 archaeology + the harness evidence — #4784.
- The ADR names the concrete mechanism MODEL-03/MODEL-04 will implement in Phase 5.

**Out of scope (belongs to Phase 5+):**
- The doc corrections (MODEL-02), the version guard implementation (MODEL-03), and the
  dual-write elimination (MODEL-04). Phase 4 only *decides* the mechanism.
- Actually *building* event sourcing if the ADR chooses (A) — a large ES build is a
  follow-on milestone per REQUIREMENTS.md "Out of Scope", not Phase 4/5.
</domain>

<decisions>
## Implementation Decisions

### ADR openness (MODEL-01)
- **D-01:** The ADR is **genuinely open — (A) build real event sourcing vs. (B)
  CRUD-canonical weighed equally, with no starting lean.** The user explicitly overrode
  the F1 doc's recommended lean toward (B). Consequence: research MUST genuinely cost out
  option (A) — what a real projection/rebuild path for world state (locations, exits,
  characters, objects) would actually entail, whether replayability / auditable
  state-reconstruction is wanted, and its effort — not merely confirm CRUD. The harness
  evidence and the (A)-cost analysis together drive the decision.
- **D-02:** The ADR MUST record *intent* (was world-state event sourcing ever meant to be
  real, or was "event sourcing" always shorthand for "event-driven + audit log"?). The F1
  archaeology already concludes "architectural **drift by default** — a stated principle
  never realized for the world model, no ADR ever recorded the divergence"; the ADR
  formalizes and closes that gap (the missing ADR *is* the finding).

### Resilience harness fidelity (OPS-05)
- **D-03:** Reproduce the two-replica deployment with a **real single-node NATS JetStream
  container (`internal/testsupport/natstest`) + a Postgres testcontainer + two in-process
  `CoreServer` replicas** sharing the one broker/DB. This is the documented external-NATS
  integration tier — multi-replica / external-broker behavior MUST use a real NATS
  container per `.claude/rules/testing.md`, NOT the shared embedded `eventbustest`
  connection. Broker flap = pause/restart the NATS container; replica restart = recreate
  one `CoreServer`; client reconnect = re-open the gRPC/session stream.
- **D-04 (rejected):** A full two-OS-process / docker-compose stack (higher fidelity but
  slower + flakier to orchestrate) and a lighter single-process simulated-concurrency
  harness (cheaper but doesn't exercise true two-replica separation) were both considered
  and NOT chosen.

### Harness disposition
- **D-05:** The harness is **kept in-tree but excluded from the gating CI / PR lane** —
  opt-in via the quarantine mechanism (`HOLOMUSH_RUN_QUARANTINED=1` /
  `quarantinetest.Enabled()`; runs nightly + locally), documented and reproducible on
  demand. It survives as a standing regression check for the Phase 5 MODEL-03 version
  guard **without** flaking the required `Integration Test` PR gate. Rationale: CONCERNS.md
  flags two-replica chaos as CI-resource-sensitive (the `ConcurrentUp`/`holomush-pqzv`
  testcontainer-port-map timeout quarantine entry) and thin CI scale headroom.
  - NB per `.claude/rules/testing.md`: quarantine is normally for *known-flaky specs with
    an open issue*, not for a deliberately-gated investigation harness. Planner MUST decide
    the exact opt-in seam (a dedicated `HOLOMUSH_*` env flag vs. reusing the quarantine
    marker + a `test/quarantine.yaml` row citing #4791) and honor the marker↔registry
    bijection meta-test if the quarantine idiom is reused.

### Corruption verdict scope
- **D-06:** The harness MUST **empirically reproduce actual last-write-wins state
  corruption for M12** (concurrent commands mutating the same world entity → a silently
  lost update) — OR prove it cannot occur — under the two-replica deployment. This is
  success-criterion #2's load-bearing verdict.
- **D-07:** For **M2 (dual-write non-atomicity — a world change commits to the DB but its
  post-commit event/notification is lost on a NATS blip, `move_succeeded:true`)**, the
  harness **characterizes / proves the race window exists** on a broker flap; it need NOT
  force a deterministic observable lost move. M2's mechanism is already well-established by
  the F1 archaeology (events are a post-commit notification, so a flap loses the
  notification while the DB write persists).

### Claude's Discretion
- ADR file naming/slug follows the existing `docs/adr/NNNN-<slug>.md` convention (197
  ADRs; sequential 4-digit prefix) — planner/executor pick the next number + slug.
- Which world entities the harness exercises (locations/exits/characters/objects) and the
  concrete concurrent-command pair used to trigger M12 — planner's call, grounded in the
  actual `world.Service` write path.
- Whether the ADR also proposes the (A)-vs-(B) decision framework / scoring rubric shape.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The decision input (F1 archaeology)
- `docs/reviews/arch-review/2026-07-11/verification/f1-eventsourcing-why.md` — THE
  grounding doc for the ADR. Establishes: world state was always CRUD; no world-state
  rebuild path ever existed; no ADR ever recorded a CRUD-vs-ES choice ("drift by
  default"); CRUD-not-ES is the root cause of M2 (dual-write) + M12 (last-write-wins);
  spells out option (A) build-ES vs (B) CRUD-canonical + optimistic-concurrency +
  transactional-outbox, and the reframed "investigate + decide, then reconcile" action.
- `docs/reviews/arch-review/2026-07-11/REPORT.md` — the parent L7 arch-review report
  (F1/#4784 and the §7 resilience follow-up #4791 in context).

### World-model write path (what the harness exercises)
- `internal/world/event_store_adapter.go` — `EventStoreAdapter.Emit` builds a
  `core.NewEvent(...)` and calls `store.Append(...)`; append-only, one-directional
  notification/audit — no read-back reconstructs state. Uses `core.WorldServiceActorULID`.
- `internal/world/events.go` — world event emission surface.
- `internal/store/postgres.go` §33-34 + `cmd/holomush/sub_grpc.go` §768-770 — confirm the
  PG store holds no event table; host-engine `Append` goes straight to the bus (F7 cutover).

### Event-sourcing background (cost-out option A)
- `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` — the F-series
  deletions (`EventWriter`, `cursor_lock.go`, `replay.go`, `internal/grpc/replay.go`) were
  all in the gRPC `Subscribe` **client-catch-up** path, NOT world-state rebuild. Needed to
  scope what building real (A) event sourcing of world state would actually cost.

### Test-tier + harness substrate
- `.claude/rules/testing.md` — the test-tier table; external-NATS tier MUST use a real
  NATS container for multi-replica / external-broker behavior (§"Embedded NATS … EXCEPT");
  quarantine idiom + `test/quarantine.yaml` marker↔issue bijection.
- `internal/testsupport/natstest/nats.go` — real single-node NATS JetStream container
  harness (external-mode tier).
- `internal/testsupport/integrationtest/harness.go` + `options.go` — in-process
  Postgres + NATS + `CoreServer` stack (`//go:build integration`); base to extend to two
  replicas.
- `internal/testsupport/quarantinetest/quarantinetest.go` — `EnvVar =
  "HOLOMUSH_RUN_QUARANTINED"`, `Enabled()`; the opt-in/nightly gating mechanism for D-05.

### Issues
- #4784 (MODEL-01 ADR), #4791 (OPS-05 resilience pass), #4798 (M12 last-write-wins),
  and M2 (dual-write non-atomicity, tracked under F1/#4784).
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/testsupport/integrationtest` — the canonical in-process stack (Postgres +
  NATS + production `CoreServer`). Base to extend to a two-replica shape rather than
  building a harness from scratch.
- `internal/testsupport/natstest` — real NATS JetStream container; provides the external
  broker two independent replicas each dial (the shape `eventbustest`'s shared connection
  cannot express).
- `internal/testsupport/quarantinetest` — ready-made opt-in/nightly gating (`Enabled()` +
  `HOLOMUSH_RUN_QUARANTINED`) for D-05.

### Established Patterns
- World writes are **CRUD-canonical with a post-commit event notification** — the event is
  NOT the write (`event_store_adapter.go`). This is the exact property the harness probes
  (M2) and the ADR decides whether to keep.
- Integration tests are Ginkgo/Gomega, `//go:build integration`, run via `task test:int`;
  external-mode-specific tests use `natstest`.
- ADRs: `docs/adr/NNNN-<slug>.md`, sequential 4-digit prefix (197 existing).

### Integration Points
- The harness drives real commands through `CoreServer` (session/gRPC surface) against the
  `world.Service` write path; corruption is observed by reading world state back after
  concurrent writes.
- The ADR lands in `docs/adr/` and is cross-linked from #4784; its named mechanism is the
  input contract for Phase 5 (MODEL-03 version guard, MODEL-04 dual-write elimination).
</code_context>

<specifics>
## Specific Ideas

- User explicitly wants the ADR **genuinely open** (A vs B), overriding the F1 doc's
  CRUD lean — the strongest signal in this discussion. The research/ADR must give building
  real event sourcing a fair, costed hearing, not treat CRUD as foregone.
- Harness must reflect the **two-replica production topology** faithfully enough to trust
  the M12 verdict, but stay off the gating CI lane so it can't destabilize the PR gate.
</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.
</deferred>

---

*Phase: 4-world-model-resilience-investigation-decision-f1*
*Context gathered: 2026-07-11*
