# Requirements: HoloMUSH — Milestone v0.12 (Foundation Hardening)

Pay down the highest-severity architecture and operational risks surfaced by the
2026-07-11 L7 architecture review (PR #4807, report at
`docs/reviews/arch-review/2026-07-11/`) and the associated backlog clusters 999.9
(architecture decomposition) and 999.10 (code health & test quality). This is an
**internal hardening milestone** — no new player-facing features. REQ-IDs restart
per milestone (v0.11 used CHAN/SCENEFWD/CLUSTER; those are archived).

## v0.12 Requirements

### Event Model & Concurrency Integrity

<!-- F1 #4784 is a decision gate: sequence early; MODEL-03/04 and ARCH-04 depend on its outcome. -->

- [x] **MODEL-01**: The event-sourcing-vs-CRUD divergence is resolved by a committed ADR that investigates whether world-state event sourcing was ever meant to be real and formally decides the model — build a real projection/outbox, or adopt CRUD-canonical + optimistic-concurrency/transactional-outbox — grounded in F1 (`docs/reviews/arch-review/2026-07-11/verification/f1-eventsourcing-why.md`, #4784)
- [x] **MODEL-02**: Every doc site stating the false "event sourcing / current state derives from replay" principle (the ~6 sites incl. root `CLAUDE.md`, `contributing/explanation/architecture.md`, `coding-standards.md`, and the public site `index.mdx`) is corrected to describe the decided model (MODEL-01)
- [x] **MODEL-03**: World-state writes (locations/exits/characters/objects) carry an optimistic-concurrency version guard so a concurrent writer cannot silently lose an update — closes last-write-wins M12 (#4798), verified to hold under the two-replica deployment
- [x] **MODEL-04**: The dual-write window (world event emitted after a non-atomic DB commit, `move_succeeded:true` while the notification is lost on a NATS blip) is eliminated per the MODEL-01 mechanism (transactional outbox or real projection) — closes M2

### Operational Hardening

<!-- The "true Highs" (F2/F4/F8) + F3 recoverability + the report's #1 follow-up resilience pass. -->

- [ ] **OPS-01**: The public gateway caps request-body size (`connect.WithReadMaxBytes`) and sets a read timeout, so an unauthenticated client cannot OOM the gateway with an unbounded body — closes F2 (#4785). **Ships first as a `/gsd-quick` fix (pre-Phase 4)** — a live DoS one-liner, too small for the full phase loop.
- [x] **OPS-02**: `events_audit` growth is bounded by extending the existing RetentionWorker (the sibling ABAC-audit table's machinery) to it, so the table cannot grow without limit — closes F4 (#4786)
- [x] **OPS-03**: The `nats-server` CVE is remediated (≥ v2.14.3) AND a govulncheck / vuln-scan CI gate is added so a vulnerable dependency is caught rather than merged blind — closes F8 (#4790)
- [x] **OPS-04**: The audit-DLQ replay CLI recovers for its target external-NATS deployment (the `game_id` split bridge is fixed) and its tautological coverage test is replaced with a genuine recovery assertion — closes F3 (#4787)
- [x] **OPS-05**: A resilience/concurrency pass reproduces concurrent commands + a NATS broker flap + a replica restart + client reconnect, empirically establishing whether M12 corrupts state under two-replica concurrency and confirming the MODEL-03 guard holds — the report's #1 recommended follow-up (#4791)

### Architecture Decomposition (999.9)

<!-- Structural debt from the beads migration (epics holomush-1bft/dj95/wm0fi/yvdm); behavior-preserving. -->

- [ ] **ARCH-01**: The `CoreServer` god object is decomposed into cohesive, separately-testable units with no behavior change (epics `holomush-1bft`/`wm0fi`)
- [ ] **ARCH-02**: The `plugin/manager` god object is decomposed similarly, with no behavior change (epic `holomush-dj95`)
- [ ] **ARCH-03**: Process bootstrap is migrated onto `lifecycle.Orchestrator`, unifying subsystem start/stop ordering (epic `holomush-yvdm`)
- [x] **ARCH-04**: The parallel `core.Event` / `eventbus.Event` models are collapsed to a single representation, coordinated with the MODEL-01 outcome
- [x] **ARCH-05**: Remaining gateway-boundary import violations are removed so `internal/web` / `internal/telnet` hold only protocol-translation dependencies (`.claude/rules/gateway-boundary.md`)

### Code Health & Test Quality (999.10)

<!-- F7 coverage governance + the code-health/test-quality batch (epics holomush-ec22/89o9). -->

- [x] **QUAL-01**: Per-package coverage and CI are reconciled — the >80% MUST is either enforced as a CI gate or corrected to match reality — so the documented bar and the enforced bar agree (F7 #4804; `main` last merged at 54.6% patch)
- [ ] **QUAL-02**: Packages under the reconciled bar (surfaced by a coverage audit) are backfilled with genuine behavioral tests
- [ ] **QUAL-03**: Skeleton/weak tests (zero-assertion, tautological) are remediated to assert real behavior, and ACE test-naming violations are corrected to the sentence convention
- [ ] **QUAL-04**: A session-lifecycle test matrix covers the connect / reconnect / multi-character / idle-timeout paths
- [ ] **QUAL-05**: A code-health & security-polish batch is applied — de-slop/humanization plus the arch-review Medium cluster (secure-cookie default, empty-string ABAC sentinels, silent audit-emitter drop, DEK read-cache, `sessions.location_id` index), triaged at phase time

## Future Requirements

Deferred to a later milestone — tracked, not in this roadmap.

### Deferred from the arch review (feature-shaped)

- **MOVE-01** (deferred): Wire a player-facing movement command + register `look`/`who` so clickable exits actually walk between locations — F5 #4788. *Product-readiness High, but a gameplay affordance, not hardening — belongs to a gameplay/web milestone.*
- **PWA-01** (deferred): Ship a real service worker / manifest / PWA dependency, or correct the "offline-capable PWA" doc claim — F6 #4803. *Belongs to the web/client-experience milestone.*

### Ops & DR resilience (999.13)

- Disaster-recovery + backup/restore guides, background DB sync to object storage, gateway-survival deploy strategy, Tailscale admin access, remote KMS (VaultTransitProvider + rotation CLIs). *Deferred whole-cluster; embedded KEK stays the default.*

## Out of Scope

Explicitly excluded from v0.12. Documented to prevent scope creep.

| Excluded | Reason |
|----------|--------|
| New social features — Forums (999.4), Discord bridge (999.5), char rostering (999.6), inventory/objects (999.7) | This is a hardening milestone; new gameplay/social features are a separate feature milestone |
| Web-portal completion (999.1) — offline/PWA, char profiles/creation UI, DMs, admin web UI | Separate web/client-experience milestone; F6 PWA deferred above |
| Remote KMS / Vault substrate + DR/backup (999.13) | Deferred whole-cluster (Future Requirements above); embedded KEK remains the default |
| Movement command / `look`/`who` (F5 #4788) | Feature-shaped (deferred to Future Requirements), not architecture/operational hardening |
| Building event sourcing itself, if the MODEL-01 ADR chooses CRUD | The ADR decides; any large ES *build* it may recommend is a follow-on milestone, not v0.12 |

## Traceability

Which phase covers which requirement — **populated by `gsd-roadmapper` during roadmap creation.**

| Requirement | Phase | Status |
|-------------|-------|--------|
| OPS-01 | Quick fix (pre-Phase 4) | Pending |
| MODEL-01 | Phase 4 | Complete |
| OPS-05 | Phase 4 | Complete |
| MODEL-02 | Phase 5 | Complete |
| MODEL-03 | Phase 5 | Complete |
| MODEL-04 | Phase 5 | Complete |
| OPS-02 | Phase 6 | Complete |
| OPS-03 | Phase 6 | Complete |
| OPS-04 | Phase 6 | Complete |
| QUAL-01 | Phase 6 | Complete |
| ARCH-03 | Phase 7 | Pending |
| ARCH-04 | Phase 7 | Complete |
| ARCH-05 | Phase 7 | Complete |
| ARCH-01 | Phase 8 | Pending |
| ARCH-02 | Phase 8 | Pending |
| QUAL-02 | Phase 9 | Pending |
| QUAL-03 | Phase 9 | Pending |
| QUAL-04 | Phase 9 | Pending |
| QUAL-05 | Phase 9 | Pending |

**Coverage:**

- v0.12 requirements: 19 total
- Mapped to phases: 19 ✓
- Unmapped: 0

---
*Requirements defined: 2026-07-11 for milestone v0.12 (Foundation Hardening)*
*Last updated: 2026-07-11 after initial definition*
