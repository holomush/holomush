---
phase: 03
slug: world-character-commands
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-08-10
---

# Phase 03 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

Register origin: **authored at plan time** — all six `*-PLAN.md` files carried a
`<threat_model>` block, and all six `*-SUMMARY.md` files carried a
`## Threat mitigations applied` execution record. No retroactive-STRIDE
reconstruction was required.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| command entry → `world.Service` | caller identity crosses into the ABAC chokepoint via `checkAccess` (D-39: policy is the ONLY ownership control) | caller subject, character id, expected version |
| `world.Service` → PostgreSQL | status values and ids cross into SQL | typed `world.Status`, stringified ULIDs, integer version |
| audit consumer creation → shared helper | operational retry behavior for the durable audit pipeline moves packages | audit error codes, backoff schedule |
| host composition → new subsystems | two new inert subsystems enter the production boot graph | lifecycle dep edges only (no domain traffic) |
| JetStream delivery → reactor handler | at-least-once, unauthenticated-within-process event data drives privileged effects | `character_retired` envelopes |
| reactor → `world.Service` | a background job crosses the ABAC chokepoint (the ONLY gated effect — session/presence have no chokepoint today) | job provenance triple, character id |
| `seed.go` | policy text IS the ownership control (D-39) | DSL grant text |
| bus events → KV bucket | unauthenticated-within-process event actors drive buffered writes | character id, activity timestamp |
| flusher → `characters` table | a background writer crosses the writer-boundary fence with no ABAC chokepoint (sanctioned as INV-WORLD-4's fourth writer) | `last_active_at` only |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-03-01 | Elevation of Privilege | `Service.RetireCharacter`/`UnretireCharacter` | high | mitigate | `checkAccess` on distinct `retire`/`unretire` actions, no Go ownership predicate (`internal/world/service.go:942`, `:1042`) — **spot-verified** | closed |
| T-03-02 | Tampering | `SetStatus` SQL | low | mitigate | `status` typed const bound as `$2`; ids stringified ULIDs; `expected_version` numeric equality on the INTEGER column | closed |
| T-03-03 | Tampering | D-34 players write on the world conn | medium | mitigate | idempotent single-statement clear in the same tx as the CAS; widening documented at the method and in the fence-test doc block | closed |
| T-03-04 | Repudiation | envelope emission | low | mitigate | exactly-one-envelope via `mutate()`; census bijection meta-test enforces the closed set | closed |
| T-03-05 | Tampering | D-46 relocation | medium | mitigate | verbatim move; both audit error codes pinned by acceptance greps AND a new identity assertion that the helper wraps nothing; backoff schedule single-sourced | closed |
| T-03-06 | Denial of Service | subsystem boot graph | low | mitigate | both dep edge sets read LIVE by two independent real-constructor graphs; acyclicity property test + pinned 20-element topological order green | closed |
| T-03-07 | Repudiation | inert skeleton subsystems in production | low | accept | AR-03-01 — `Prepare`/`Activate` are documented no-ops carrying no domain traffic and binding no surface | closed |
| T-03-08 | Repudiation | IDENT-10 guarantee | medium | mitigate | the plan IS the mitigation: two-replica rejection proof + three-direction atomicity proof move the guarantee from asserted to demonstrated | closed |
| T-03-09 | Information Disclosure | test read-backs via shared pgxpool | low | accept | AR-03-02 — test-only substrate inside the integration harness; no production path | closed |
| T-03-10 | Elevation of Privilege | reactor authorization | high | mitigate | every world call goes through `world.JobCaller`; no `WithSystemSubject` anywhere in `internal/retirement/`; caller pinned by value equality in `internal/retirement/reactor_test.go` — **spot-verified** | closed |
| T-03-11 | Elevation of Privilege | `job:retirement` grant breadth | high | mitigate | D-54 instance scoping — both `action.job.trigger_event_type` and `action.job.trigger_subject` conjuncts present in `seed:job-retirement-instance-scoped` (`internal/access/policy/seed.go:563`), pinned by exact-DSL assertion — **spot-verified** | closed |
| T-03-12 | Tampering | handler-supplied provenance | medium | mitigate | D-55: triple stamped at the consumer boundary BEFORE handler logic; body-derived resource vs subject-derived provenance, with a spec feeding disagreeing values | closed |
| T-03-13 | Tampering / DoS | redelivery double-effects | medium | mitigate | per-effect observed-state gates; `TestProcessIsFullyIdempotentAcrossARedeliveryOfTheSameMessage` drives the same message twice and pins each effect count at exactly one — **spot-verified (test present)** | closed |
| T-03-14 | Tampering | eviction of an un-retired character | medium | mitigate | status guard with a denying default runs before any effect | closed |
| T-03-15 | Elevation of Privilege | over-broad human retire surface | high | mitigate | no new human-principal seed ships; the only permit naming retire is the job-scoped one, and admin reach is the pre-existing bare-action seed (`internal/access/policy/seed.go:107`); six evaluation specs with paired controls — **spot-verified** | closed |
| T-03-16 | Information Disclosure | `last_active_at` as a presence oracle | low | accept | AR-03-03 — the column lags by up to one flush interval BY CONSTRUCTION (D-42); no new surface exposes it | closed |
| T-03-17 | Denial of Service | unbounded KV growth on flusher failure | medium | mitigate | delete-after-flush + `History: 1`; failed keys retry next tick; malformed key names purged, malformed values dropped at their read revision | closed |
| T-03-18 | Tampering | fenced-table SQL escaping the writer boundary | medium | mitigate | the `UPDATE characters` literal lives in `internal/world/postgres` only (SQL fence green); `internal/charactivity/` imports no pg driver — **spot-verified** | closed |
| T-03-19 | Tampering | spurious envelopes corrupting the world feed | medium | mitigate | bare `UPDATE`, no executor; unit test plus two integration specs pin version + outbox-count unchanged | closed |
| T-03-20 | Elevation of Privilege | inventing flusher job provenance preempting 02.2's model | medium | mitigate | the reserved-interlock ANSWERED branch fired; 02.2's D-68 followed verbatim; no `trigger_kind` or tick-provenance shape minted | closed |
| T-03-21 | Repudiation | ROADMAP success criterion 2 | medium | mitigate | the plan IS the mitigation — 03-04's T-03-10/11/13 move from unit-asserted to observed through the real chain | closed |
| T-03-22 | Tampering | harness options masking production gaps | low | mitigate | real relay, real subsystem lifecycles, real presence emitter on the bus, one shared real `world.Service`; no-synthetic-event rule keeps the relay in the loop; AckFloor check keeps redelivery honest | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above `workflow.security_block_on` (high) count toward `threats_open`*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-03-01 | T-03-07 | Two inert skeleton subsystems enter the production boot graph before 03-04/03-05 activate them. `Prepare`/`Activate` are documented no-ops: they carry no domain traffic and bind no external surface, so the Prepare/Activate contract carve-out applies. | accepted at plan time (03-02) | 2026-08-10 |
| AR-03-02 | T-03-09 | Integration-harness tests read back through a shared `pgxpool`. Test-only substrate; no production code path reaches it. | accepted at plan time (03-03) | 2026-08-10 |
| AR-03-03 | T-03-16 | `characters.last_active_at` is a coarse presence oracle. It lags by up to one flush interval **by construction** (D-42), and no Phase 3 surface exposes it. Any surface that does expose it is a Phase 5 privacy decision, not Phase 3's. | accepted at plan time (03-05) | 2026-08-10 |
| AR-03-04 | criterion 3 coverage (UAT test 2) | The **two-replica** retire-concurrency proof (`test/integration/resilience/retire_concurrency_test.go`) is gated on `quarantinetest.Enabled()` (`resilience_suite_test.go:50`), so it does not run in the required `Integration Test` lane, and is currently red where it does run (#4953). The single-process guarantee IS covered in the gating lane. Ruled SCHEDULED-not-accepted-as-is: #4953 must close for the suite to rejoin the gating lane. | developer ruling, UAT test 2 | 2026-08-10 |

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-08-10 | 22 | 22 | 0 | `/gsd-secure-phase 03` (orchestrator, ASVS L1 short-circuit + high-severity spot-check) |

**Audit method.** The L1 short-circuit applied (`threats_open: 0`,
`register_authored_at_plan_time: true`, `asvs_level == 1`), so no
`gsd-security-auditor` subagent was spawned. Rather than accept the summaries'
self-reports unverified, the orchestrator independently re-ran the mechanical
claims behind all four `high`-severity threats plus T-03-13/T-03-18 against HEAD.
All six held. This guards the failure mode recorded for this phase: earlier
code-review iterations each asserted a guarantee about a code path no test
reached.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-08-10
