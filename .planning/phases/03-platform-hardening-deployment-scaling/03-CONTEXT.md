# Phase 3: Platform Hardening & Deployment Scaling - Context

**Gathered:** 2026-07-10
**Status:** Ready for planning

<domain>

## Phase Boundary

Operator-facing platform hardening — **no player-visible features**. Extends
the already-shipped EventBus/crypto substrate so HoloMUSH can be deployed as a
horizontally-scaled multi-node cluster with a durable audit pipeline, closing
the single-node ceiling flagged in `.planning/codebase/CONCERNS.md` § Scaling
Limits.

**In scope (CLUSTER-01…05):**

- **External/clustered NATS mode (CLUSTER-01):** a config-selected `external`
  mode for the event bus alongside the existing embedded in-process mode
  (`internal/eventbus/subsystem.go` boots only embedded today;
  `internal/config/config.go` has zero NATS keys).
- **Single-principal account scoping (CLUSTER-02):** shipped assets + checks
  making the phase3d-grounding design real — only the `holomush-server` NATS
  account may publish/subscribe on game topics (`events.>`, `audit.>`,
  `internal.>`) in external mode; enforcement lives at the cluster admin
  layer.
- **Multi-node crypto invalidation verification (CLUSTER-03):** the shipped
  Phase-3c cluster substrate (`internal/cluster/`,
  `internal/eventbus/crypto/invalidation/`) verified against real multi-node
  replicas — it has only ever run single-node embedded.
- **Audit dead-letter queue (CLUSTER-04):** close the
  `TODO (Phase B): wire a DLQ` at `internal/eventbus/audit/subsystem.go` —
  messages that exhaust `MaxDeliver` land in a DLQ instead of silently
  stopping redelivery.
- **Operator runbook (CLUSTER-05):** full-lifecycle external-NATS deployment
  runbook under `site/src/content/docs/operating/`.

**Out of scope:** remote KMS / VaultTransitProvider (backlog 999.13); load/
chaos harness + SLOs (999.14); k8s/helm assets; sandbox migration (deferred,
see below); plugin-consumer DLQ wiring beyond the seam (follow-up); web/telnet
surface changes.

</domain>

<decisions>

## Implementation Decisions

### Mode selection & boot behavior (CLUSTER-01)

- **D-01 (explicit mode key):** New `eventbus:` koanf config section:
  `mode: embedded | external` plus `url`, `credentials`, `tls {ca, cert, key}`,
  `provision`. **Embedded stays the zero-config default** — external requires
  deliberate opt-in. `mode: external` with no URL fails at config-validation
  time. Follows the existing koanf YAML section pattern in
  `internal/config/config.go`.
- **D-02 (fail closed at boot):** External NATS unreachable at boot → refuse
  to start with a clear error; the orchestrator (compose restart policy /
  k8s) owns retry. Matches the mandatory-KEK-to-boot precedent. Once
  connected, nats.go's built-in reconnect handles transient drops of an
  established connection.
- **D-03 (server provisions, opt-out seam):** The server idempotently
  creates/updates JetStream streams in external mode exactly as embedded mode
  does (same code path, config parity). A `provision: false` config seam
  supports locked-down clusters where the server account lacks `$JS.API`
  stream-admin permissions — then the server verifies stream existence/config
  and **fails closed on mismatch** instead of creating. Ship-now + seam.
- **D-04 (creds-file auth):** Authentication via NATS `.creds` file (JWT/NKey
  decentralized auth — the account-native mechanism the CLUSTER-02 scoping
  design assumes) plus an optional `tls` block for mTLS/private CAs.
  User:pass-in-URL implicitly works for dev clusters (URL passes straight to
  nats.go).

### Multi-node verification (CLUSTER-03)

- **D-05 (integration tier + E2E smoke):** Two proofs: (a) integration tests
  running N replica instances — each with its own NATS connection and
  `cluster.Registry` member — against a **real external-NATS testcontainer**
  (protocol proof, binds invariants); (b) one compose-based multi-process
  smoke (2 core replicas + NATS container) as the deployment-shaped capstone
  (topology proof).
- **D-06 (fold into `task test:int`):** External-NATS tests join the existing
  `//go:build integration` tier (Postgres testcontainers are precedent for
  container-backed int tests). The standing "embedded NATS is correct at every
  test tier" rule is **amended in the same change** (CLAUDE.md / testing
  rules): embedded remains correct for everything EXCEPT external-mode-specific
  behavior, which MUST use a real NATS container. One runner, CI-required.
- **D-07 (invariant binding — multi-node core + audit the rest):** Bind the
  inherently multi-replica invariants via the new harness: **INV-CLUSTER-1,
  INV-CLUSTER-2** (N-of-N acks on KEK/DEK rotation), **INV-CLUSTER-4**
  (cluster_id-prefix filtering), **INV-CLUSTER-9** (every live member's
  ParticipantsCache invalidated). Audit the remaining entries
  (3/5/6/7/8/10): annotate `// Verifies:` only where an existing test
  genuinely asserts the invariant; otherwise leave `binding: pending` and file
  a coverage issue (`gh issue create -R holomush/holomush`). **No fabricated
  bindings** (per `.claude/rules/invariants.md`).
- **D-08 (failure depth):** Verify (a) rotation with all replicas acking and
  (b) one hung/dead replica → probe-and-pill fires, cluster proceeds with
  N-1, rotation completes. Partitions, split-brain, flapping, and chaos
  scenarios are deferred (999.14 territory).

### Audit DLQ (CLUSTER-04)

- **D-09 (NATS DLQ stream, in-band capture):** On the final delivery attempt
  (`msg.Info().NumDelivered >= MaxDeliver`), the consumer publishes the full
  message to a dedicated DLQ stream, then Terms the original. **If the DLQ
  publish itself fails, Nak instead of Term** — redelivery continues and
  nothing is ever dropped. The DLQ's failure domain is independent of
  Postgres, whose outage is the most likely cause of audit dead letters. No
  advisory-stream plumbing needed.
- **D-10 (shared helper, audit wired now):** Build the final-attempt capture
  as a reusable helper in the eventbus package tree; wire it into the host
  audit projection this phase (CLUSTER-04's literal scope). The plugin audit
  consumer (same poison exposure) is a documented follow-up that reuses the
  same helper — file the issue during this phase.
- **D-11 (operator surface — metrics + runbook + replay tool):** Prometheus
  counter for DLQ'd messages (Grafana/alert assets exist under `docker/`),
  runbook section covering inspection via the `nats` CLI, plus a small
  **replay tool** that re-drives DLQ entries through the projection write
  once the outage is fixed. Rationale: dead letters you can't replay are just
  nicer-looking data loss.
- **D-12 (bounded retention with alerting):** DLQ stream is size/age-capped
  (default ~30d, configurable in the `eventbus:` section) so a poison flood
  cannot eat the disk; the Prometheus counter alerts operators long before
  anything ages out.

### Operator deployment story (CLUSTER-02 + CLUSTER-05)

- **D-13 (scoping assets — templates + script + boot self-check, ALL of
  them):** Ship (a) NATS account config templates under `deploy/nats/`
  (accounts block / nsc walkthrough granting `holomush-server`
  publish+subscribe on `events.>`, `audit.>`, `internal.>` and nothing else);
  (b) a runbook-documented **verification script** that connects with a
  NON-server credential and asserts publish/subscribe are denied on game
  topics; AND (c) a **boot-time self-check** in the server. The two checks are
  complementary: the self-check proves the server's own account is not
  over-scoped; the external script proves other principals are locked out —
  single-principal can only be fully proven from outside the server's account.
- **D-14 (reference topology — compose overlay):** A `compose.cluster.yaml`
  overlay on `compose.prod.yaml` adding a NATS service (JetStream,
  file-storage) and a second core replica. It is both the runbook's working
  example and the substrate the multi-node E2E smoke drives. Stays within the
  repo's compose-based deployment story (`compose.prod.yaml`,
  `compose.e2e.yaml` precedents).
- **D-15 (sandbox migration deferred):** The phase's proof is integration
  tests + compose E2E smoke + a verified runbook — all inside the repo/CI
  boundary. Migrating `game.holomush.dev` to external NATS is a follow-up
  operational task once the runbook has settled.
- **D-16 (full lifecycle runbook):** One end-to-end operator doc under
  `site/src/content/docs/operating/how-to/` (sandbox-ops precedent):
  provision NATS with the account templates → mint creds → configure
  holomush → **cut over from embedded** (with an explicit stance on stream
  data: Postgres audit is the durable record; the EVENTS stream starts fresh
  on the external cluster) → run the scoping verification script → DLQ
  inspection/replay → rollback to embedded.

### Claude's Discretion

- Architectural seam for the mode switch — branch inside
  `eventbus.Subsystem.Start` vs separate embedded/external implementations
  behind one interface. (Note: `productionSubsystems` in
  `cmd/holomush/core.go` is a named-param function with a count-asserting
  test cascade — factor that into the wiring plan.)
- Exact config key names/shape inside the `eventbus:` section; where creds
  paths/secrets are documented for compose (env vs mounted files).
- DLQ stream name/subject layout; replay-tool placement (cobra subcommand vs
  `cmd/` tool vs task target — note there is no existing plugin/audit cobra
  group; adding one needs its own wiring task).
- Testcontainer NATS shape for the int tier: single external node vs 3-node
  cluster (the protocol needs *external*, not necessarily *clustered*; pick
  what the invariants actually require).
- Metrics naming; whether the embedded-only NATS Prometheus exporter
  (`internal/eventbus/subsystem.go` scrapes the embedded monitor endpoint)
  gets an external-mode equivalent or the runbook points at the cluster's own
  monitoring.
- New invariants: if external mode / DLQ mint new named guarantees (e.g.
  "DLQ capture never drops a message", "external boot fails closed"),
  register `INV-CLUSTER-N` / `INV-EVENTBUS-N` (`binding: pending` or bound)
  in the same change per `.claude/rules/invariants.md`.

</decisions>

<canonical_refs>

## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### External-NATS / account-scoping design (governs CLUSTER-01/02)

- `docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md`
  — THE single-principal account-scoping design (holomush-server account
  scoped to `events.>`, `audit.>`, `internal.>`; enforcement at the cluster
  admin layer; optional read-only operator account explicitly deferred).
  CLUSTER-02 implements what this spec designed under `holomush-s5ts`.
- `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` — the bus
  design ("embedded in-process today, externally clustered later"); external
  NATS deployment/network policy/DNS were declared out of scope there — this
  phase picks them up.
- `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` — master
  crypto spec; origin of the cluster invalidation protocol invariants
  (per-cluster subject prefixing, N-of-N replica ack).

### Cluster substrate — the shipped code being verified (CLUSTER-03)

- `internal/cluster/` — registry.go, probe_pill.go, pill.go, heartbeat.go,
  types.go, clustertest/ — the Phase-3c cluster substrate.
- `internal/eventbus/crypto/invalidation/coordinator.go` — the invalidation
  coordinator (request-reply, probe-and-pill retry cycle).
- `docs/architecture/invariants.yaml` — **INV-CLUSTER-1..10** (the binding
  targets); plus `.claude/rules/invariants.md` for the binding ratchet rules.

### Code being extended (CLUSTER-01/04)

- `internal/eventbus/subsystem.go` — embedded NATS boot path (the mode branch
  lands here); embedded-only Prometheus exporter caveat.
- `internal/eventbus/audit/subsystem.go` — `DefaultMaxDeliver = 10` +
  `TODO (Phase B)` DLQ site; Config struct the DLQ knobs join.
- `internal/config/config.go` — koanf YAML config surface (new `eventbus:`
  section; posflag overrides).
- `cmd/holomush/core.go` — `productionSubsystems` wiring (named-param
  signature + test cascade gotcha).

### Testing

- `docs/superpowers/specs/2026-05-25-test-tier-taxonomy-design.md` — the tier
  taxonomy D-06 amends.
- `internal/eventbus/eventbustest/embedded.go` — the embedded harness (stays
  correct for non-external-specific tests).
- `internal/testsupport/integrationtest/` — the canonical in-process
  integration stack; Postgres testcontainer precedent for container-backed
  int tests.
- `compose.e2e.yaml` — compose-driven E2E precedent for the multi-process
  smoke.

### Deployment / operator docs

- `compose.prod.yaml` — the shipped prod topology the `compose.cluster.yaml`
  overlay extends (postgres/core/gateway/caddy/cloudflared/otel-collector).
- `site/src/content/docs/operating/how-to/sandbox/sandbox-operations.md` —
  operating-docs location + style precedent for the runbook.
- `docker/prometheus/`, `docker/grafana/` — metrics/alerting assets the DLQ
  counter plugs into.
- `.planning/codebase/CONCERNS.md` § Scaling Limits + § Missing Critical
  Features — the phase's founding citations (single-node ceiling, DLQ TODO).

</canonical_refs>

<code_context>

## Existing Code Insights

### Reusable Assets

- **`internal/cluster/` + `invalidation.Coordinator`** — the cluster protocol
  is SHIPPED and designed cluster-ready (cluster_id-prefixed subjects,
  request-reply acks, probe-and-pill); this phase verifies it multi-node, not
  rebuilds it.
- **`eventbustest` embedded harness + Postgres testcontainer precedent** —
  the int-tier pattern the external-NATS testcontainer tests slot into.
- **NATS client plumbing in `internal/eventbus/subsystem.go`** — connection,
  JetStream context, readiness wait, drain-on-stop; external mode swaps the
  server-embedding for a dial, keeping the JetStream surface identical.
- **`docker/prometheus` + `docker/grafana`** — alerting assets for the DLQ
  counter.
- **`compose.prod.yaml` / `compose.e2e.yaml`** — overlay + compose-driven
  test precedents for D-14/D-05.

### Established Patterns

- koanf YAML config sections with posflag overrides (`internal/config/`).
- Fail-closed boot (mandatory-KEK-to-boot precedent) — D-02 mirrors it.
- Ship-now + seam (Phases 1–2) — D-03 provision opt-out, D-10 shared DLQ
  helper.
- Container-backed `//go:build integration` tests via `task test:int`
  (hard-coded package list in Taskfile — extend it when adding new int
  packages).
- Invariant binding discipline: `// Verifies: INV-…` annotations + registry
  flip + `go run ./cmd/inv-render` in the same change.

### Integration Points

- `internal/eventbus/subsystem.go` `Start` — the mode branch.
- `internal/config/config.go` + config validation — the `eventbus:` section.
- `cmd/holomush/core.go` `productionSubsystems` — wiring (watch the
  named-param/test-cascade gotcha).
- `internal/eventbus/audit/subsystem.go` consumer loop — final-attempt DLQ
  capture hook.
- `Taskfile.yaml` `test:int` package list; CI workflow required checks.
- `compose.prod.yaml` (overlay), `deploy/` (new `deploy/nats/`),
  `site/src/content/docs/operating/how-to/` (runbook + docs sidebar).

</code_context>

<specifics>

## Specific Ideas

- User explicitly chose **both** halves for CLUSTER-02 verification
  ("1 + 2"): external verification script AND boot-time self-check — treat
  them as complementary, not alternatives (D-13).
- DLQ capture contract in one sentence: *publish-to-DLQ then Term; if the DLQ
  publish fails, Nak — nothing is ever dropped* (D-09).
- Replay rationale to preserve in the runbook: "dead letters you can't replay
  are just nicer-looking data loss" (D-11).
- Runbook must state the cutover data stance explicitly: Postgres audit is
  the durable record; the EVENTS stream starts fresh on the external cluster
  (D-16).

</specifics>

<deferred>

## Deferred Ideas

- **Sandbox (`game.holomush.dev`) migration to external NATS** — follow-up
  operational task after the runbook settles (D-15).
- **Plugin audit consumer DLQ wiring** — reuses the D-10 shared helper; file
  a GitHub issue during this phase.
- **Read-only operator NATS account** (`holomush-operator-read`,
  `subscribe: events.>` only) — explicitly deferred by the phase3d grounding
  spec; note it in the runbook as a future option.
- **Kubernetes/helm reference topology** — compose overlay only this phase.
- **Full adversarial/chaos matrix** (partitions, split-brain, restart-during-
  rotation, pill storms) — load/chaos-harness territory (backlog 999.14).
- **Postgres mirror of the DLQ for SQL inspection** — stream-only this phase
  (D-09); a PG drain could layer on later without changing the capture path.
- **Remote KMS / VaultTransitProvider** — adjacent ops-resilience work,
  untouched (backlog 999.13).

</deferred>

---

*Phase: 3-Platform Hardening & Deployment Scaling*
*Context gathered: 2026-07-10*
