---
phase: 3
slug: platform-hardening-deployment-scaling
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on (high) severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-07-10
---

# Phase 3 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Register authored at plan time (all 9 PLAN files carried a `<threat_model>`
> block); verified at ASVS L1 (grep-depth) per `workflow.security_block_on: high`.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| operator/orchestrator → core config | Untrusted YAML/flags select `mode` and carry the external URL / creds paths | Config, credential paths |
| core process ↔ external NATS | Network hop; authn via `.creds`/TLS; the server account is the sole game-topic principal | Event stream, audit + internal subjects |
| operator config → provisioning authority | `provision` flag controls whether the server may run `$JS.API` stream admin | Stream-admin capability |
| audit projection ↔ Postgres | Persist failures (DB outage) are the primary cause of audit dead letters | Durable audit records |
| audit DLQ stream (data at rest) | Captured dead-letter messages carry event payloads (encrypted-at-rest envelope) | Encrypted event payloads |
| replica ↔ replica (cross-cluster) | Multi-node crypto-invalidation subjects; a rogue/misconfigured member could inject stale rotation pings | Rotation / invalidation pings |
| rogue principal → game topics | Any non-server NATS account attempting pub/sub on `events.>`/`audit.>`/`internal.>` | Game-topic pub/sub |
| server account scope → cluster | The server's own grant breadth (least-privilege) | Account permissions |
| operator CLI → NATS + Postgres | The `holomush audit dlq` replay tool reads the DLQ stream and writes the durable audit table | DLQ contents, audit rows |
| compose stack ↔ external NATS + concurrent bootstrap | Two core replicas share one NATS + one Postgres; two processes could race seed migrations | Shared infra, migration state |
| operator ← runbook guidance | The runbook is the authoritative deployment procedure; inaccurate guidance is an operational risk | Operational guidance |
| test process → ephemeral NATS container | Test-only; container torn down at process exit | Test data |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-03-01 | Denial of Service | `mode:external` with empty URL | high | mitigate | `Config.Validate()` fails closed — `internal/eventbus/config.go:173` (`mode is external but url is empty`); no silent embedded fallback | closed |
| T-03-02 | Tampering | stale `cluster`/`cluster_url` keys in operator YAML after rename | medium | accept | Rename is repo-internal; mode was never deployed, so no external YAML references it. Runbook documents canonical keys | closed |
| T-03-03 | Tampering | `natstest` imported by production code | medium | mitigate | Under `internal/testsupport/`; depguard parity with `eventbustest`/`coretest` — production imports fail lint | closed |
| T-03-04 | Denial of Service | leaked containers exhaust CI Docker resources | low | mitigate | `Terminate` + `t.Cleanup` reclaim-on-failure (mirrors postgres helper) | closed |
| T-03-05 | Spoofing / Info-disclosure | external NATS connection without authn/encryption | high | mitigate | `.creds` (JWT/NKey) via `nats.UserCredentials` + optional mTLS (nats.go RootCAs/ClientCert); no hand-rolled crypto (ASVS V2/V6) | closed |
| T-03-06 | Denial of Service / policy bypass | external NATS unreachable → insecure degraded fallback | high | mitigate | Fail closed at boot — `internal/eventbus/natsdial.go:59` (`EVENTBUS_EXTERNAL_CONNECT_FAILED`); no embedded fallback; orchestrator owns retry | closed |
| T-03-07 | Elevation of Privilege | server given `$JS.API` stream-admin it should not hold | medium | mitigate | `provision:false` verify-or-fail seam lets operators deny stream admin and still boot | closed |
| T-03-08 | Tampering | stream config drift on a locked-down cluster silently accepted | medium | mitigate | `provision:false` compares `StreamInfo.Config`, fails closed (`EVENTBUS_STREAM_CONFIG_MISMATCH`) | closed |
| T-03-09 | Repudiation | audit dead letter silently dropped → audit-trail gap | high | mitigate | Capture-then-Term; never-drop guaranteed — `internal/eventbus/audit/dlq.go` (`Capture`) + `projection.go` (leave-un-acked at MaxDeliver ceiling, WR-01); replay restores to `events_audit`. Crypto-reviewer READY | closed |
| T-03-10 | Denial of Service (disk exhaustion) | poison flood fills the DLQ stream | high | mitigate | Bounded `MaxAge`/`MaxBytes` — `internal/eventbus/audit/dlq.go:31` (30d default); `dlq_messages_total` metric alerts long before age-out | closed |
| T-03-11 | Info-disclosure | DLQ preserves headers/payload of sensitive events | medium | accept | Payloads are already the encrypted-at-rest envelope (per-field DEK); DLQ inherits the same `internal.>` account scoping (CLUSTER-02). No new plaintext surface (crypto-reviewer READY) | closed |
| T-03-12 | Tampering | Nak-storm on ordinary poison path | medium | mitigate | Nak reserved for the DLQ-publish-failed fallback only; sub-cap path keeps AckWait backoff | closed |
| T-03-13 | Tampering | cross-cluster message injection (wrong cluster_id) | high | mitigate | cluster_id-prefixed subjects; members drop mismatched payloads — `internal/cluster/heartbeat.go:269` (INV-CLUSTER-4), proven over per-replica conns | closed |
| T-03-14 | Repudiation | fabricated invariant binding hides an unverified guarantee | high | mitigate | No-fabricated-bindings (D-07); `TestBoundInvariantsAreGenuinelyAsserted` gates CI; left-pending gaps filed as issues | closed |
| T-03-15 | Denial of Service | a hung replica stalls rotation cluster-wide | medium | mitigate | Probe-and-pill proceeds with N-1; the pilled member self-terminates (exit 125 → supervisor restart) | closed |
| T-03-16 | Spoofing / Tampering / Info-disclosure | rogue principal publishes/subscribes on game topics | high | mitigate | Account subject permissions restrict game topics to `holomush-server`; `deploy/nats/verify-scoping.sh` proves lockout (ASVS V4) | closed |
| T-03-17 | Elevation of Privilege | server account over-scoped (privilege creep) | high | mitigate | Boot-time self-check refuses to start if the server can reach beyond the three prefixes — `internal/eventbus/scopecheck.go:46` (`VerifyAccountScoping` → `EVENTBUS_ACCOUNT_OVERSCOPED`) | closed |
| T-03-18 | Availability / policy bypass | self-check false-negative lets an over-scoped account boot | medium | mitigate | Self-check fails CLOSED (permitted probe ⇒ refuse); per-probe channel/handler (code-review IN-01); Case A/B integration assertions | closed |
| T-03-19 | Repudiation | replay creates duplicate/forged audit rows | medium | mitigate | Reuses projection persist path (`ON CONFLICT DO NOTHING` on `Nats-Msg-Id`); idempotency proven by the double-replay test | closed |
| T-03-20 | Info-disclosure | replay CLI exposes DLQ contents to an unauthorized operator | low | accept | CLI runs on the operator host with NATS creds + `DATABASE_URL`; same trust level as existing crypto/migrate CLIs (no new surface) | closed |
| T-03-21 | Tampering / data corruption | concurrent seed bootstrap on two replicas races migrations | high | mitigate | Only the primary seeds; the second replica uses `--skip-seed-migrations` — `cmd/holomush/core.go:163` | closed |
| T-03-22 | Info-disclosure | NATS service exposed to the host/public network | medium | mitigate | NATS stays on the backend network with no host port mapping; additionally a single-principal scoped account | closed |
| T-03-23 | Denial of Service | leaked smoke containers/volumes exhaust CI resources | low | mitigate | Teardown trap (`down -v`) on every exit path + temp-dir cleanup (verified: no leaks) | closed |
| T-03-24 | Repudiation / data loss | operator misunderstands cutover data stance, expects EVENTS-stream migration | high | mitigate | Explicit runbook callout — `.../external-nats-deployment.md:16` (`Read the cutover data stance first`): Postgres audit durable, EVENTS starts fresh; rollback path documented | closed |
| T-03-25 | Tampering | runbook documents a wrong/nonexistent command | medium | mitigate | Accuracy pass grounds every command in a shipped artifact; `task docs:build` gates the page | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above `high` count toward `threats_open`*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-03-01 | T-03-02 | Config-key rename is repo-internal; the pre-rename mode was never implemented or deployed, so no external operator YAML references the stale keys. Canonical keys are documented in the runbook. | Phase 3 plan (D-01) | 2026-07-10 |
| AR-03-02 | T-03-11 | DLQ-captured payloads are already the per-field-DEK encrypted-at-rest envelope, and the DLQ stream inherits the same single-principal `internal.>` account scoping (CLUSTER-02). No new plaintext surface — confirmed READY by crypto-reviewer. | Phase 3 plan (D-09) + crypto-reviewer | 2026-07-10 |
| AR-03-03 | T-03-20 | The `holomush audit dlq` replay CLI runs on the operator host with NATS creds + `DATABASE_URL` — the same trust level as the existing crypto/migrate operator CLIs. No new exposure surface. | Phase 3 plan (Plan 07) | 2026-07-10 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-07-10 | 25 | 25 | 0 | /gsd-secure-phase (ASVS L1 grep audit, Claude Opus 4.8) |

Evidence basis: register authored at plan time (9/9 PLAN `<threat_model>` blocks);
SUMMARY `## Threat Flags` (all mitigated, no new trust-boundary surface); L1
grep-confirmation of every high-severity mitigation at `path:line` (above);
crypto-reviewer verdict READY on the DLQ/audit crypto-gated surface; deep
`/gsd-code-review` clean; `task pr-prep` fast lane green. No auditor deep-dive
required — short-circuit satisfied (`threats_open: 0` ∧ register authored at plan
time ∧ ASVS L1).

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-07-10
