---
phase: 04
slug: world-model-resilience-investigation-decision-f1
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-07-11
---

# Phase 04 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
>
> Phase 04 is an **investigation + decision** phase. It ships no production
> request-path code — only a nightly/opt-in integration test harness
> (`internal/testsupport/integrationtest/`, `test/integration/resilience/`),
> evidence documents, and an Accepted ADR (`holomush-i4784`). The threat
> register below was authored at plan time (a `<threat_model>` STRIDE block in
> each `04-0N-PLAN.md`) and verified at L1 grep-depth against the merged diff.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| CI test-execution gate | `quarantinetest.Enabled()` env gate (`HOLOMUSH_RUN_QUARANTINED=1`) fences the two-replica resilience suite off the gating `Integration Test` lane | CI compute resources (Docker: real NATS + Postgres + two in-process CoreServer replicas) |
| Test-support ↔ production | depguard forbids production packages importing `internal/testsupport/{quarantinetest,natstest}` and `eventbustest`; all new code is `//go:build integration` | Test-only helpers (allow-all engine, chaos injectors) must not reach a production build |
| ADR decision authority | The Accepted ADR records the world-state security posture (ABAC `checkAccess` stays pre-write) that Phase 5 implementation must honor | Security-relevant design contract handed to the implementation phase |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-04-01 | Elevation of Privilege | integrationtest allow-all engine / new `StartOptions`; test worldBusAppender | medium | mitigate | All new code is `//go:build integration` under `internal/testsupport/` + `test/`; depguard forbids production imports of `testsupport/quarantinetest`/`natstest`/`eventbustest` (`.golangci.yaml:145-155`) — no depguard change this phase | closed |
| T-04-02 | Denial of Service | required `Integration Test` PR gate (CI resources) | high | mitigate | `resilience_suite_test.go:50` gates on `quarantinetest.Enabled()` and `t.Skipf`s **before** `RunSpecs`; with the env unset the suite executes 0 specs (verified Task acceptance) | closed |
| T-04-03 | Tampering (supply chain) | Go module dependencies | low | accept | `go.mod`: `github.com/moby/moby/client v0.4.0` promoted **indirect→direct** at the same pinned version already in the graph — zero new packages entered `go.sum`; official Docker client, consumed only by the integration harness | closed |
| T-04-04 | Tampering | ADR mis-recording the security posture of the chosen mechanism (e.g. an outbox relay/consumer imagined as bypassing ABAC) | medium | mitigate | ADR `holomush-i4784` Consequences (line 101): *"`world.Service` `checkAccess` remains pre-write… no outbox relay or feed consumer bypasses it — the relay publishes already-authorized, already-committed facts"* | closed |
| T-04-05 | Information Disclosure | resilience specs reading world rows | low | accept | Specs read only rows they created via direct SQL; test-only package under `//go:build integration`; depguard keeps `testsupport` out of production | closed |
| T-04-06 | Repudiation | verdict document vs. actual run results | medium | mitigate | `f1-resilience-verdict.md` §Reproduction quotes verbatim verdict lines from a single canonical `HOLOMUSH_RUN_QUARANTINED=1` run judged by exit code, with the reproduction command documented for independent re-derivation | closed |
| T-04-07 | Elevation of Privilege | MODEL-01 decision made without the human decider | medium | mitigate | ADR `holomush-i4784` `Status: Accepted`, `Deciders: Sean Brandt`; plan `04-04` is `autonomous: false` with a blocking `checkpoint:decision` — the executor could not self-select an option | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on (high) count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-04-01 | T-04-03 | `moby/moby/client v0.4.0` was already a pinned indirect dependency (Docker/testcontainers transitive); this phase only promotes it to a direct require for the chaos harness — no new package, same version, no `go.sum` addition. Official Docker client; integration-test scope only. | Sean Brandt | 2026-07-11 |
| AR-04-02 | T-04-05 | Resilience specs read only world rows they themselves create via direct SQL, inside `//go:build integration` test code that depguard keeps out of production builds — no production information-disclosure surface. | Sean Brandt | 2026-07-11 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-07-11 | 7 | 7 | 0 | /gsd-secure-phase (L1 grep-depth; register authored at plan time; ASVS L1) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-07-11
