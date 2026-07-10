# Phase 3: Platform Hardening & Deployment Scaling - Research

**Researched:** 2026-07-10
**Domain:** External/clustered NATS deployment, multi-node crypto-invalidation verification, audit dead-letter queue, operator runbook — all extending shipped substrate
**Confidence:** HIGH (brownfield; every claim grounded in in-tree `path:line`)

<user_constraints>

## User Constraints (from CONTEXT.md)

### Locked Decisions (D-01…D-16)

- **D-01** New `event_bus:` koanf section: `mode: embedded | external` + `url`, `credentials`, `tls {ca, cert, key}`, `provision`. Embedded stays zero-config default. `mode: external` with no URL fails at config-validation time. Follow existing koanf section pattern.
- **D-02** Fail closed at boot: external NATS unreachable → refuse to start with clear error; orchestrator owns retry. Mirror mandatory-KEK-to-boot. nats.go reconnect handles transient drops of an established connection.
- **D-03** Server provisions JetStream streams idempotently in external mode (same code path as embedded). `provision: false` seam for locked-down clusters → server *verifies* stream existence/config and **fails closed on mismatch** instead of creating.
- **D-04** Auth via NATS `.creds` file (JWT/NKey decentralized auth) + optional `tls` block for mTLS/private CAs. user:pass-in-URL works implicitly for dev.
- **D-05** Two proofs: (a) integration tests running N replica instances each with its own NATS connection + `cluster.Registry` member against a **real external-NATS testcontainer** (protocol proof); (b) one compose-based multi-process smoke (2 core replicas + NATS container) as deployment-shaped capstone (topology proof).
- **D-06** External-NATS tests join the existing `//go:build integration` tier. Amend the "embedded NATS is correct at every test tier" rule in the same change: embedded remains correct for everything EXCEPT external-mode-specific behavior. One runner, CI-required.
- **D-07** Bind inherently multi-replica invariants via new harness: **INV-CLUSTER-1, -2, -4, -9**. Audit the rest (3/5/6/7/8/10): annotate `// Verifies:` only where an existing test genuinely asserts; else leave `binding: pending` + file coverage issue. **No fabricated bindings.**
- **D-08** Verify (a) rotation with all replicas acking, and (b) one hung/dead replica → probe-and-pill fires, cluster proceeds with N-1, rotation completes. Partitions/split-brain/chaos deferred (999.14).
- **D-09** On final delivery attempt, consumer publishes full message to a dedicated DLQ stream, then Terms the original. **If the DLQ publish itself fails, Nak instead of Term** — nothing is ever dropped. DLQ failure domain independent of Postgres.
- **D-10** Build final-attempt capture as a reusable helper in the eventbus package tree; wire into host audit projection this phase. Plugin audit consumer DLQ = documented follow-up reusing same helper (file issue this phase).
- **D-11** Prometheus counter for DLQ'd messages + runbook section (`nats` CLI inspection) + a small **replay tool** re-driving DLQ entries through the projection write.
- **D-12** DLQ stream size/age-capped (default ~30d, configurable in `event_bus:` section); Prometheus counter alerts before anything ages out.
- **D-13** Ship ALL of: (a) NATS account templates under `deploy/nats/`; (b) runbook-documented **verification script** (connects with NON-server cred, asserts publish/subscribe denied on game topics); (c) a **boot-time self-check** in the server. Complementary, not alternatives.
- **D-14** `compose.cluster.yaml` overlay on `compose.prod.yaml` adding a NATS service (JetStream, file-storage) + a second core replica. Runbook's working example AND the substrate the multi-node E2E smoke drives.
- **D-15** Sandbox (`game.holomush.dev`) migration to external NATS deferred — follow-up after runbook settles.
- **D-16** One end-to-end operator runbook under `site/src/content/docs/operating/how-to/`: provision → mint creds → configure → cut over from embedded (explicit data stance: Postgres audit is durable record; EVENTS stream starts fresh) → run scoping script → DLQ inspect/replay → rollback to embedded.

### Claude's Discretion (the 6+ open questions — answered below in `## Open-Question Recommendations`)

Mode-switch seam; exact `event_bus:` key names/shape; DLQ stream/subject layout + capture hook + helper location + Term-vs-Nak; testcontainer NATS shape (single vs 3-node); replay-tool placement; metrics naming + external-mode exporter; whether to mint new `INV-CLUSTER-N`/`INV-EVENTBUS-N` for external-boot-fail-closed / DLQ-never-drops.

### Deferred Ideas (OUT OF SCOPE)

Sandbox migration to external NATS; plugin-audit-consumer DLQ wiring (file issue); read-only operator NATS account (`holomush-operator-read`); k8s/helm topology; full chaos/partition matrix (999.14); Postgres mirror of DLQ; remote KMS / VaultTransitProvider (999.13); web/telnet surface changes.

</user_constraints>

<phase_requirements>

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CLUSTER-01 | Deploy event bus against external/clustered NATS instead of only embedded | `event_bus:` config surface already has `Mode`/`ClusterURL`/`CredentialsFile` stubs (`internal/eventbus/config.go:8-60`); mode branch lands in `Subsystem.Start` (`internal/eventbus/subsystem.go:82`). See OQ-1, OQ-2. |
| CLUSTER-02 | Single-principal subject scoping (`events.>`, `audit.>`, `internal.>`) in external mode | Account-scoping design = phase3d-grounding spec. Assets = `deploy/nats/` templates + verification script + boot self-check (D-13). See OQ-7 / Validation Architecture. |
| CLUSTER-03 | Crypto invalidation propagates across real multi-node replicas | Substrate SHIPPED (`internal/cluster/`, `internal/eventbus/crypto/invalidation/coordinator.go`) using CORE NATS pub/sub via `natsconn.Conn` — works unchanged on external NATS. Current test shares ONE conn (gap). See OQ-4, Validation Architecture, D-07 binding plan. |
| CLUSTER-04 | Audit messages exhausting MaxDeliver land in DLQ, not silently dropped | Hook = `projection.handle` (`internal/eventbus/audit/projection.go:216`); `TODO (Phase B)` at `subsystem.go:59`. `jetstream.Msg` API verified (Metadata/Term/Nak). See OQ-3. |
| CLUSTER-05 | Documented external-NATS deployment runbook | `site/src/content/docs/operating/how-to/` (sandbox-ops precedent). See OQ-5, D-16. |

</phase_requirements>

## Summary

This is a **verification-and-extension** phase, not a greenfield build. The cluster membership/health substrate (`internal/cluster/`) and the DEK-cache invalidation coordinator (`internal/eventbus/crypto/invalidation/coordinator.go`) are already shipped, wired into `productionSubsystems`, and — critically — built entirely on **core NATS pub/sub** through the narrow `natsconn.Conn` seam (`internal/eventbus/natsconn/natsconn.go:57`), which `*nats.Conn` satisfies structurally. Nothing in the cluster protocol touches JetStream or assumes in-process transport. That means switching from embedded to external NATS is almost entirely an `eventbus.Subsystem` connection-establishment change: everything downstream (`Conn()`, `JS()`, cluster, invalidation, audit) keeps working because they consume the connection, not the server.

The four code deltas are: (1) a `mode: external` branch inside `Subsystem.Start` that dials an external URL with creds/TLS instead of embedding a server (CLUSTER-01); (2) a boot-time single-principal self-check plus shipped `deploy/nats/` account templates and an external verification script (CLUSTER-02); (3) an external-NATS integration harness that gives each replica its **own** `*nats.Conn` (today the multi-member test at `test/integration/crypto/cache_invalidation_test.go:41` shares ONE `h.Embedded.Conn` — that is the exact gap CLUSTER-03 closes) to bind INV-CLUSTER-1/2/4/9 multi-node (CLUSTER-03); and (4) a DLQ capture at the audit consumer's final delivery attempt with a stream-independent failure domain plus a replay tool (CLUSTER-04). The runbook (CLUSTER-05) plus a `compose.cluster.yaml` overlay tie it together.

**Primary recommendation:** Refactor `Subsystem.Start` to split "obtain a `*nats.Conn` + `jetstream.JetStream`" (mode-dependent) from "declare the EVENTS stream + start exporter" (mode-independent), keeping a single `EnsureStream`/provision path. Add `eventbus.Config.Validate()` for the fail-closed `mode:external` + empty-url check. Reconcile the pre-existing `ModeCluster="cluster"` / `ClusterURL` / `CredentialsFile` names against D-01's `external`/`url`/`credentials` vocabulary **as an explicit early task** — there is naming drift the planner must resolve before writing config code.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| NATS connection establishment (embed vs dial) | Core / EventBus subsystem | Config | `Subsystem.Start` owns transport; config selects mode |
| JetStream stream provisioning / verification | Core / EventBus subsystem | — | `EnsureStream` is already mode-agnostic (operates on `s.js`) |
| Cluster membership + probe/pill | Core / cluster subsystem | NATS (transport) | Pure core NATS pub/sub via `natsconn.Conn`; transport-agnostic |
| DEK cache cross-replica invalidation | Core / invalidation coordinator | cluster.Registry | Request-reply over `natsconn.Conn`; N-of-N ack protocol |
| Audit projection + DLQ capture | Core / audit subsystem | JetStream, Postgres | Consumer loop owns Term/Nak/DLQ decision |
| Single-principal subject scoping | NATS server config (external) | Core boot self-check | Enforcement is server-side (accounts); server only self-verifies |
| DLQ replay | CLI tool (cobra) | NATS + Postgres | Re-drives DLQ stream through the projection write path |
| Deployment topology | Compose / ops docs | — | `compose.cluster.yaml` overlay; runbook |

## Standard Stack (all in-tree; no new external dependencies)

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/nats-io/nats.go` | v1.52.0 | NATS client + `jetstream` sub-package | Already the bus client `[VERIFIED: go list -m]` |
| `github.com/nats-io/nats-server/v2` | in go.mod | Embedded server (embedded mode only) | Existing embedded boot `[VERIFIED: subsystem.go:12]` |
| `github.com/testcontainers/testcontainers-go` | v0.43.0 | Container-backed int tests | Postgres precedent `test/testutil/postgres.go` `[VERIFIED: go.mod:38]` |
| `github.com/knadh/koanf/v2` | in go.mod | YAML config + posflag overrides | Existing `config.Load` `[VERIFIED: config.go:18]` |
| `github.com/prometheus/client_golang` | in go.mod | DLQ counter metric | Existing cluster/invalidation metrics `[VERIFIED: core.go:491]` |
| `github.com/spf13/cobra` | in go.mod | Replay-tool subcommand | Existing `crypto`/`plugin events` groups `[VERIFIED: root.go:37-43]` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Generic `testcontainers.GenericContainer` for NATS | A dedicated NATS testcontainer module | No NATS module is in go.mod; adding one is avoidable — the generic container running `nats:2-alpine -js` + a wait strategy is sufficient and mirrors how Postgres helper is built. `[VERIFIED: go.mod has no nats testcontainer module]` |
| NATS DLQ stream (D-09) | Postgres mirror of DLQ | Deferred (D-09/deferred): stream-only keeps failure domain independent of Postgres, which is the most likely cause of audit dead letters. |

**Version verification:** `go list -m github.com/nats-io/nats.go` → `v1.52.0` `[VERIFIED]`. `jetstream.Msg`, `MsgMetadata`, `PublishMsg` all confirmed against `$GOMODCACHE/.../nats.go@v1.52.0/jetstream/{message.go,jetstream.go}` (see OQ-3). No new package installs required for any CLUSTER-NN.

## Package Legitimacy Audit

**No external packages are installed by this phase.** Every library the plans call (nats.go, testcontainers-go, koanf, prometheus client, cobra, pgx) is already a direct dependency in `go.mod` and in production use. No `npm/pip/cargo/go get` step is planned. Package-legitimacy gate is **N/A** — no discovery of new registry packages occurred.

## Architecture Patterns

### System Architecture Diagram (external mode)

```text
                       ┌──────────────── external NATS cluster ─────────────────┐
                       │  account: holomush-server                              │
                       │  scoped pub/sub: events.>  audit.>  internal.>         │
                       └───────▲───────────────▲───────────────▲───────────────┘
                               │ dial(url,creds,tls)            │ (same URL, per-replica conn)
        ┌──────────────────────┴───────┐            ┌───────────┴──────────────────┐
        │ core replica A               │            │ core replica B               │
        │  eventbus.Subsystem.Start    │            │  eventbus.Subsystem.Start    │
        │    mode=external → *nats.Conn│            │    mode=external → *nats.Conn│
        │    → jetstream.New(conn)     │            │    → jetstream.New(conn)     │
        │    → EnsureStream/verify     │            │    → EnsureStream/verify     │
        │  cluster.Registry (conn)     │◀── internal.<id>.member.* heartbeats ──▶│
        │  invalidation.Coordinator    │◀── internal.<id>.cache_invalidate.dek ─▶│
        │  audit.projection (JS pull)  │            │  audit.projection (JS pull)  │
        │    persist→Postgres          │            │    persist→Postgres          │
        │    final attempt→DLQ stream  │            │                              │
        └──────────────┬───────────────┘            └──────────────────────────────┘
                       │ persist / DLQ-replay
                       ▼
                  PostgreSQL events_audit  (durable record; survives EVENTS reset on cutover)
```

### Recommended structure of new/changed code

```text
internal/eventbus/
├── subsystem.go            # MODIFY: split conn-establishment (mode branch) from stream/exporter
├── config.go               # MODIFY: reconcile Mode/ClusterURL names; add TLS/creds/provision; add Validate()
├── natsdial.go             # CREATE (opt): external dial helper (url+creds+tls → *nats.Conn)
├── scopecheck.go           # CREATE: boot-time single-principal self-check (CLUSTER-02)
└── audit/
    ├── subsystem.go        # MODIFY: DLQ config knobs join Config; remove Phase-B TODO
    ├── projection.go       # MODIFY: handle() final-attempt → DLQ-then-Term / Nak
    └── dlq.go              # CREATE: reusable DLQ capture helper (D-10) + DLQ stream ensure
internal/testsupport/  (or test/testutil/)
└── natstest/               # CREATE: external-NATS testcontainer helper (per-replica conns)
cmd/holomush/
├── core.go                 # MODIFY: pass eventBusConfig through; no productionSubsystems arity change (see OQ-6)
└── cmd_audit.go            # CREATE: `holomush audit dlq {list,show,replay}` cobra group (OQ-5)
deploy/nats/                # CREATE: accounts template + nsc walkthrough + verification script (D-13)
compose.cluster.yaml        # CREATE: overlay (NATS service + 2nd core replica) (D-14)
site/src/content/docs/operating/how-to/  # CREATE: external-nats runbook (D-16)
test/integration/cluster/   # ADD multi-node external specs; test/integration/crypto/ rebind
```

### Pattern 1: mode branch splits transport from stream declaration
**What:** `Subsystem.Start` currently does embed-server → connect → jetstream → EnsureStream → exporter, all inline (`subsystem.go:82-190`). Factor the first three (mode-dependent) behind a `connect(ctx) (*nats.Conn, jetstream.JetStream, error)` step; keep `EnsureStream` (`subsystem.go:193`) and the exporter block mode-aware.
**When to use:** CLUSTER-01.
**Key seam:** `Conn()` (`subsystem.go:277`) and `JS()` (`subsystem.go:274`) are the ONLY things downstream consumers read — cluster (`core.go:502` `Conn: eventBusSub.Conn()`), invalidation (`core.go:902`), audit (`core.go:523`). Preserve their contracts and nothing downstream changes.

### Pattern 2: provision opt-out = CreateOrUpdateStream vs verify-or-fail (D-03)
**What:** `EnsureStream` uses `js.CreateOrUpdateStream` (`subsystem.go:197`). For `provision: false`, branch to `js.Stream(ctx, StreamName)` + compare returned `StreamInfo.Config` against the desired `StreamConfig`; on mismatch return a coded fail-closed error. Same treatment for the new DLQ stream.

### Pattern 3: DLQ capture at final delivery attempt (D-09)
**What:** in `projection.handle` (`projection.go:216`), when `persist` fails, read `meta, _ := msg.Metadata(); meta.NumDelivered`; if `>= cfg.MaxDeliver`, publish the original message (subject+headers+data) to the DLQ stream via `p.js.PublishMsg`, then `msg.Term()`. If the DLQ publish errors, `msg.Nak()` (redelivery continues — never dropped). Below the cap, keep the existing no-ack behavior (AckWait redelivery gives backoff — see `projection.go:238-243`). `p.js` is already retained on the projection (`projection.go:46`).

### Anti-Patterns to Avoid
- **Reaching into JetStream for cluster transport.** Cluster/invalidation are core NATS pub/sub by design (`natsconn.go:4-14`); do not "upgrade" them to JS.
- **Nak on the poison path.** The existing projection deliberately does NOT Nak on persist error to avoid instant-redeliver storms (`projection.go:238-243`). The DLQ path only Terms after successful DLQ publish; Nak is reserved for the DLQ-publish-failed fallback.
- **Adding a param to `productionSubsystems`.** It is a 15-named-param function with a count-asserting test cascade (`core_subsystems_test.go:32-73` asserts `len==15`). DLQ/external mode need NO new subsystem — they extend existing ones. Avoid touching the arity (see OQ-6).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| External NATS reconnect after established | custom reconnect loop | nats.go built-in reconnect (D-02) | Only boot-time connect fails closed; nats.go handles transient drops |
| Multi-replica test membership | mock registries sharing a conn | real per-replica `*nats.Conn` to a testcontainer | The shared-conn shortcut is exactly the gap CLUSTER-03 closes (`cache_invalidation_test.go:41`) |
| DLQ delivery-count tracking | a redelivery counter table | `msg.Metadata().NumDelivered` (JetStream server-tracked) | Server owns the count; app just reads it |
| Header/body preservation into DLQ | re-encoding the event | `js.PublishMsg(ctx, &nats.Msg{Subject,Header,Data})` | Preserves `Nats-Msg-Id` for replay dedup + all audit headers |
| Single-principal scoping enforcement | app-level ACL checks | NATS account subject permissions (server-side) | Enforcement belongs at the cluster admin layer (phase3d design); server only self-checks |

**Key insight:** Almost all of this phase is *wiring and verification of existing primitives*. The temptation to rebuild the cluster protocol or invent a redelivery tracker is the trap — the substrate exists and is transport-agnostic.

## Open-Question Recommendations (the Claude's-Discretion items — grounded)

### OQ-1 — Mode-switch seam
**Recommendation:** Branch **inside** `eventbus.Subsystem.Start`, not separate impls behind an interface. Rationale: `Start` already contains all rollback machinery and the mode-independent tail (`EnsureStream` at `subsystem.go:193`, exporter at `subsystem.go:163`); a second impl would duplicate that. Extract a private `connect(ctx) (*nats.Conn, jetstream.JetStream, error)` with an internal `switch s.cfg.Mode`. Embedded path = current `subsystem.go:92-146`; external path = `nats.Connect(url, opts...)` + `jetstream.New(conn)`. Keep the exporter block guarded by mode (embedded-only — see OQ-7). `[VERIFIED: subsystem.go:82-190]`

### OQ-2 — `event_bus:` koanf section shape
**Recommendation & CRITICAL naming reconciliation:** The section is already loaded as `event_bus` (`core.go:137`) into `eventbus.Config`. **`eventbus.Config` ALREADY declares conflicting names** the planner MUST reconcile as the first config task:
- `Mode Mode` with `ModeEmbedded="embedded"` and **`ModeCluster="cluster"`** (`config.go:8-17`) — D-01 says `external`, not `cluster`.
- `ClusterURL string koanf:"cluster_url"` and `CredentialsFile string koanf:"credentials_file"` (`config.go:54-55`) — D-01 says `url` and `credentials`.

Recommended target shape (koanf keys under `event_bus:`):
```yaml
event_bus:
  mode: external            # koanf:"mode" — add ModeExternal="external"; keep ModeCluster as legacy alias or rename
  url: nats://nats:4222     # koanf:"url"  — supersedes cluster_url
  credentials: /run/holomush.creds  # koanf:"credentials" — .creds file (D-04)
  tls: {ca: ..., cert: ..., key: ...}   # koanf:"tls" (nested struct)
  provision: true           # koanf:"provision" (D-03; default true)
  dlq: {max_age: 720h, max_bytes: ...}  # koanf:"dlq" (D-12)
```
Add `func (c Config) Validate() error` (there is none today — only `Defaults()` at `config.go:92`) returning a `CONFIG_INVALID`/`EVENTBUS_CONFIG_*` oops error when `Mode==external && url==""`. Call it right after `config.Load(...,"event_bus")` at `core.go:137-139` (mirrors `coreConfig.Validate()` at `core.go:86`, invoked at `core.go:231`). posflag overrides: the existing `Load` prefixes flags with the section (`config.go:265`), so `--event-bus-mode`-style flags map to `event_bus.mode` automatically if registered on the core command. `[VERIFIED: config.go:8-60,92; core.go:86,137,231]`

### OQ-3 — DLQ stream/subject layout, capture hook, helper, Term-vs-Nak semantics
**API verification (the CONTEXT reference to `msg.Info().NumDelivered` is the LEGACY nats.go API — the audit projection uses the NEW `jetstream` package):**
- Delivery count: `meta, err := msg.Metadata(); n := meta.NumDelivered` — `MsgMetadata.NumDelivered uint64` `[VERIFIED: nats.go@v1.52.0/jetstream/message.go:97; consumer already calls msg.Metadata() at projection.go:290]`
- Terminate: `msg.Term()` / `msg.TermWithReason(reason)`; Negative-ack: `msg.Nak()`; both are methods on `jetstream.Msg` `[VERIFIED: jetstream/message.go:66,78,87]`
- DLQ publish preserving headers: `p.js.PublishMsg(ctx, &nats.Msg{Subject: dlqSubj, Header: msg.Headers(), Data: msg.Data()})` `[VERIFIED: jetstream/jetstream.go:79; nats.Header at nats.go:4235]`

**Recommendation:**
- **DLQ stream name:** `EVENTS_AUDIT_DLQ`. **Subject:** `internal.<game_id>.audit.dlq.>` (or `audit.dlq.<game_id>.>` — but `audit.>` is in the CLUSTER-02 scoped prefixes, so `audit.dlq` stays inside the server account's permissions). Provision it idempotently alongside `EnsureStream` (its own `CreateOrUpdateStream` with `MaxAge`/`MaxBytes` from `event_bus.dlq` config, D-12).
- **Capture hook:** `projection.handle` (`projection.go:216`). On `persist` error: `if meta.NumDelivered >= p.cfg.MaxDeliver { dlqErr := dlq.Capture(...); if dlqErr != nil { msg.Nak() } else { msg.Term() } } return` (below cap: unchanged no-ack). `MaxDeliver` is already on the consumer config (`projection.go:116`, `Config.MaxDeliver` at `subsystem.go:82`, default 10 at `subsystem.go:61`).
- **Helper location (D-10):** `internal/eventbus/audit/dlq.go` — a `dlqPublisher{js, subject, counter}` with `EnsureStream` + `Capture(ctx, msg)`. Keep it in the audit package (the plugin-audit consumer, `plugin_consumer.go`, reuses it in the follow-up issue).
- **Metric:** `holomush_audit_dlq_messages_total` counter (register via `prometheus.DefaultRegisterer`, mirroring `cluster.NewPillMetrics` at `core.go:491`).
`[VERIFIED: projection.go:46,116,216,238-248,290; subsystem.go:59-92]`

### OQ-4 — Testcontainer NATS shape (single vs 3-node)
**Recommendation: single external NATS node (JetStream enabled), NOT a 3-node NATS cluster.** The invariants that bind (INV-CLUSTER-1/2/4/9) are about **HoloMUSH replica** membership and N-of-N ack over subjects — they need N separate *holomush* processes each with its own `*nats.Conn`, all pointed at one NATS server. A NATS-server *cluster* would only test NATS's own replication, which is out of scope (999.14). The current gap is concretely: `test/integration/crypto/cache_invalidation_test.go:41` wires every member's Coordinator to the **same** `h.Embedded.Conn`; the new harness gives member `i` its own `nats.Connect(container.URL)`. Build the container with generic `testcontainers.GenericContainer` running `nats:2-alpine` with `-js -sd /data` and a `wait.ForListeningPort("4222/tcp")` (or `wait.ForLog("Server is ready")`); model the helper on `test/testutil/postgres.go` (imports `testcontainers-go` + `wait`). `[VERIFIED: cache_invalidation_test.go:41; clustertest/harness.go:88-92; test/testutil/postgres.go:1-45; go.mod:38]`

### OQ-5 — Replay-tool placement
**Recommendation: a new `cmd/holomush/cmd_audit.go` cobra group — `holomush audit dlq {list,show,replay}` — registered in `root.go` alongside the existing groups.** CONTEXT's note "there is no existing plugin/audit cobra group" is stale: there ARE strong precedents — `NewCryptoCmd`/`crypto rekey …` (`cmd_crypto_rekey.go:66-110`) and `plugin events list/show` (`cmd_plugin_events.go:61-62`), all registered via `NewRootCmd`'s `cmd.AddCommand(...)` (`root.go:37-43`). The replay command dials NATS (read DLQ stream) + Postgres (write `events_audit` via the same projection persist path) — model the Postgres side on `migrate.go` and the NATS side on the eventbus dial helper. It does NOT need the admin UDS (unlike crypto). Add exactly one `cmd.AddCommand(NewAuditCmd())` line to `root.go` (no arity cascade — `root.go` is a flat list). `[VERIFIED: root.go:21-43; cmd_crypto_rekey.go:66-110; cmd_plugin_events.go:61-62; migrate.go:283-285]`

### OQ-6 — `productionSubsystems` wiring blast radius
**Recommendation: do NOT change `productionSubsystems` arity.** External mode and DLQ extend EXISTING subsystems (EventBus, AuditProjection) — no new subsystem is introduced. `productionSubsystems` is a 15-named-param function (`core.go:1396-1421`) with a hard count-asserting test cascade: `allStubs()` returns a `[15]stubSubsystem` (`core_subsystems_test.go:32-50`) and `TestProductionSubsystemsIncludesCluster` asserts `len(subs)==15` (`core_subsystems_test.go:71-73`), plus `TestSubsystemAdminSocketConstantExists` enumerates all `SubsystemID`s (`core_subsystems_test.go:78-107`). The only `core.go` changes are: (a) plumb the reconciled `eventBusConfig` into `eventbus.NewSubsystem` (already happens at `core.go:450`), (b) plumb DLQ config into `audit.Config{}` (`core.go:523`), (c) invoke the CLUSTER-02 boot self-check after `eventBusSub.Start` (`core.go:458`) in external mode. **If** the planner decides the boot self-check or DLQ needs its own lifecycle subsystem (it should not), that would trigger the full 5-test cascade + a new `lifecycle.SubsystemID` iota (append at END per prior learnings) — avoid it. `[VERIFIED: core.go:450,458,523,1396-1421; core_subsystems_test.go:32-107]`

### OQ-7 — External-mode Prometheus exporter
**Recommendation: embedded-only; external mode points the runbook at the cluster's own `/varz`/`/jsz` monitoring.** The in-process `prometheus-nats-exporter` scrapes the *embedded* server's HTTP monitor endpoint via `s.server.MonitorAddr()` (`subsystem.go:163-188`) — there is no embedded server in external mode, so `s.server` is nil and the exporter block must be skipped when `mode==external`. An external NATS cluster exposes its own monitoring port; the runbook (D-16) documents scraping that. The HoloMUSH-side DLQ counter (OQ-3) is a normal application metric on `prometheus.DefaultRegisterer`, independent of the NATS exporter, and works in both modes. `[VERIFIED: subsystem.go:15,44,58,163-188]`

### OQ-8 — New invariants for external boot / DLQ
**Recommendation:** Mint two, both `binding: pending` unless the harness genuinely asserts them this phase:
- `INV-EVENTBUS-N` — "external-mode boot fails closed: `mode:external` with an unreachable NATS server or (provision:false) stream-config mismatch refuses to Start." (bindable by an int test).
- `INV-EVENTBUS-N+1` — "DLQ capture never drops: a message exhausting MaxDeliver is Term'd only after a successful DLQ publish; a failed DLQ publish Naks (redelivery continues)." (bindable by an int test simulating a persist failure + DLQ-publish failure).
Follow `.claude/rules/invariants.md`: add to `docs/architecture/invariants.yaml` under the `INV-EVENTBUS` scope (next free N — current EVENTBUS entries start at line 2308), regenerate with `go run ./cmd/inv-render`, and NEVER fabricate a binding (D-07). `[CITED: .claude/rules/invariants.md; invariants.yaml:2308]`

## Runtime State Inventory

> This phase adds a deployment mode; it does not rename/migrate existing state. Cutover data stance is a documented operator decision (D-16), not a code migration.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | Postgres `events_audit` is the durable audit record and is mode-independent — it survives an embedded→external cutover unchanged (D-16). The JetStream **EVENTS** stream (`StreamName="EVENTS"`, `subsystem.go:24`) is NOT migrated: on external cutover it starts fresh on the new cluster (D-16 explicit stance). | Runbook doc only; no data migration. |
| Live service config | External NATS accounts (`holomush-server` scoped to `events.>`/`audit.>`/`internal.>`) live in the NATS server config / nsc, NOT in git. | Ship `deploy/nats/` templates + nsc walkthrough (D-13). |
| OS-registered state | None. Restart policy (exit-code-125 pill → supervisor restart) already documented (`core.go:487-490`). | None — verified by reading core.go wiring. |
| Secrets/env vars | `.creds` file path + optional TLS cert/key become new config; `DATABASE_URL` unchanged. Compose creds via mounted file vs env is a discretion item (document in runbook). | New config keys; document secret handling in runbook. |
| Build artifacts | None — no package rename; no egg-info/binary artifacts embed a renamed string. | None. |

## Common Pitfalls

### Pitfall 1: Using the legacy `msg.Info()` JetStream API
**What goes wrong:** CONTEXT and older nats.go docs reference `msg.Info().NumDelivered`. The audit projection uses the NEW `jetstream` package where that method does not exist.
**How to avoid:** Use `meta, err := msg.Metadata(); meta.NumDelivered` (`jetstream/message.go:37,97`). The projection already calls `msg.Metadata()` at `projection.go:290` — reuse that shape.
**Warning signs:** compile error `msg.Info undefined`.

### Pitfall 2: Sharing one NATS connection across "replicas" in the CLUSTER-03 harness
**What goes wrong:** copying `cache_invalidation_test.go`'s `Conn: h.Embedded.Conn` (`:41`) gives every member the same connection — this passes today but does NOT prove multi-node behavior. It is the exact gap CLUSTER-03 exists to close.
**How to avoid:** each member gets `nats.Connect(container.URL)`. INV-CLUSTER-1/2/4/9 only genuinely bind when replicas have independent connections to a real external server.

### Pitfall 3: Nak-ing on the ordinary poison path
**What goes wrong:** `msg.Nak()` triggers INSTANT redelivery; on a persistent DB outage that storms the database. The projection deliberately no-acks (AckWait backoff) below the cap (`projection.go:238-243`).
**How to avoid:** only Term (after DLQ publish) or Nak (only if DLQ publish itself failed) at the final attempt; leave sub-cap behavior untouched.

### Pitfall 4: Fabricating an invariant binding
**What goes wrong:** annotating `// Verifies: INV-CLUSTER-1` on a test that doesn't genuinely assert N-of-N acks (INV-CLUSTER-1 is currently `pending`, `invariants.yaml:2182`). The meta-test `TestBoundInvariantsAreGenuinelyAsserted` and provenance guard reject fabricated/`pending`-with-`asserted_by` entries.
**How to avoid:** D-07 — bind only where the multi-node harness genuinely asserts; for INV-CLUSTER-2/4/9 (already `bound` to single-conn tests) ADD the multi-node harness as additional `asserted_by`; file a coverage issue for anything left pending. Regenerate via `go run ./cmd/inv-render`.

### Pitfall 5: Touching `productionSubsystems` arity
**What goes wrong:** any new param breaks 5 tests (`core_subsystems_test.go`) that hard-assert `len==15` and enumerate the stub array.
**How to avoid:** extend existing subsystems (OQ-6); do not add a subsystem.

## Code Examples (verified in-tree shapes the plans will call)

### Reading delivery count + DLQ decision (audit consumer)
```go
// Source: internal/eventbus/audit/projection.go:216 (handle) + nats.go@v1.52.0/jetstream/message.go:37,66,78
if err := p.persist(msg); err != nil {
    meta, mErr := msg.Metadata() // MsgMetadata.NumDelivered — jetstream/message.go:97
    if mErr == nil && meta.NumDelivered >= uint64(p.cfg.MaxDeliver) {
        if dlqErr := p.dlq.Capture(p.workerCtx, msg); dlqErr != nil {
            _ = msg.Nak() // DLQ publish failed → keep redelivering; never drop (D-09)
            return
        }
        _ = msg.Term() // captured → stop redelivery (D-09)
        return
    }
    return // below cap: AckWait redelivery (existing behavior, projection.go:238-243)
}
_ = msg.Ack()
```

### DLQ publish preserving headers/body
```go
// Source: nats.go@v1.52.0/jetstream/jetstream.go:79 (PublishMsg); nats.go:4235 (Header)
_, err := js.PublishMsg(ctx, &nats.Msg{
    Subject: dlqSubject,       // internal.<game_id>.audit.dlq.<original-subject-or-msgid>
    Header:  msg.Headers(),    // preserves Nats-Msg-Id + all audit headers for replay dedup
    Data:    msg.Data(),
})
```

### External connect (mode branch inside Start)
```go
// Source: nats.go Connect opts; mirrors subsystem.go:120-146 embedded connect
conn, err := nats.Connect(cfg.URL,
    nats.Name("holomush-server"),
    nats.UserCredentials(cfg.Credentials),        // .creds file (D-04)
    // nats.ClientCert(cfg.TLS.Cert, cfg.TLS.Key), nats.RootCAs(cfg.TLS.CA) — optional mTLS
    // nats.MaxReconnects(-1) — nats.go handles transient drops post-connect (D-02)
)
if err != nil { return oops.Code("EVENTBUS_EXTERNAL_CONNECT_FAILED").Wrap(err) } // fail closed (D-02)
js, err := jetstream.New(conn)
```

## State of the Art

| Old Approach | Current Approach | Where | Impact |
|--------------|------------------|-------|--------|
| Legacy `nats.go` JetStream (`msg.Info()`, `js.Subscribe`) | New `jetstream` package (`jetstream.Msg`, `Consume`, `msg.Metadata()`) | `internal/eventbus/audit/projection.go` | Use `Metadata().NumDelivered`, `Term()`, `Nak()`, `PublishMsg` |
| Multi-member test over one shared in-process conn | per-replica `*nats.Conn` to real external NATS | CLUSTER-03 harness | The verification delta of this phase |
| Embedded-only bus (`ModeCluster` reserved, unimplemented) | `mode: external` implemented | `internal/eventbus/config.go:16` says "Reserved for a future phase; not implemented in Phase A" — this phase implements it |

**Deprecated/outdated in CONTEXT:** the `msg.Info().NumDelivered` reference (use `msg.Metadata()`); the "hard-coded `test:int` package list" note (the task uses `./...` — no enumeration, `Taskfile.yaml` test:int); the "no plugin/audit cobra group" note (crypto + plugin-events groups exist).

## Validation Architecture

> `.planning/config.json` — nyquist validation treated as enabled (no explicit `false` found). This section is MANDATORY and maps each success criterion to its validation mechanism per D-05/D-06/D-07.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` + Ginkgo/Gomega for integration (`//go:build integration`); Playwright reserved for browser E2E (not used here) |
| Int runner | `task test:int` — runs `gotestsum ... -tags=integration ./...` (NO package list; new packages auto-included) `[VERIFIED: Taskfile.yaml test:int]` |
| Container backend | `testcontainers-go` v0.43.0 (Postgres precedent `test/testutil/postgres.go`; new NATS helper analogous) |
| Quick run | `task test -- ./internal/eventbus/...` (unit) |
| Full int | `task test:int` (needs Docker) |

### Phase Requirement → Validation Map
| Req | Behavior | Validation Type | Mechanism / Command | Exists? |
|-----|----------|-----------------|---------------------|---------|
| CLUSTER-01 | `mode:external` dials + provisions; `mode:external`+no-url fails at config validation | unit + boot self-check | `eventbus.Config.Validate()` unit test; `Subsystem.Start` external-connect int test against NATS container | ❌ Wave 0 |
| CLUSTER-01/D-03 | `provision:false` verifies stream, fails closed on mismatch | integration | int test: pre-create mismatched stream, expect fail-closed | ❌ Wave 0 |
| CLUSTER-02 | server account not over-scoped (self-check) AND non-server principal denied on game topics | boot self-check + external verification script | boot self-check int test (over-scoped account → refuse); `deploy/nats/verify-scoping.sh` run in runbook + optionally in E2E smoke | ❌ Wave 0 |
| CLUSTER-03 | N-of-N acks on rekey (INV-CLUSTER-1), Rotate/Rekey (INV-CLUSTER-2), cluster_id filtering (INV-CLUSTER-4), ParticipantsCache invalidation (INV-CLUSTER-9) — across per-replica conns | integration testcontainer proof | new external-NATS harness, N replicas own conns; `// Verifies:` annotations bind INV-CLUSTER-1/2/4/9 | partial (single-conn today: `test/integration/crypto/cache_invalidation_test.go`, `test/integration/cluster/cluster_test.go`) |
| CLUSTER-03/D-08 | one hung/dead replica → probe-and-pill → N-1 completes | integration | int spec killing a replica conn mid-rotation (extends existing probe-pill specs) | ❌ Wave 0 |
| CLUSTER-04 | final-attempt → DLQ-then-Term; DLQ-publish-fail → Nak (never drop) | integration | int test: poison message (missing header) exhausts MaxDeliver → assert DLQ stream has it + counter incremented; simulate DLQ-publish failure → assert redelivery | ❌ Wave 0 |
| CLUSTER-04 | replay re-drives DLQ through projection write | integration | int test: seed DLQ, run `audit dlq replay`, assert `events_audit` row present | ❌ Wave 0 |
| CLUSTER-05 | runbook accuracy | doc + smoke | `compose.cluster.yaml` multi-process E2E smoke exercises the runbook's happy path (D-05b); `task lint:markdown` on the doc | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `task test -- ./internal/eventbus/... ./internal/cluster/...` (unit) + `task lint`.
- **Per wave merge:** `task test:int` (external-NATS + DLQ specs; Docker required).
- **Phase gate:** full `task test:int` green + `go run ./cmd/inv-render` clean (no invariant drift) + `task pr-prep` before push.

### Wave 0 Gaps
- [ ] `internal/testsupport/natstest/` (or `test/testutil/nats.go`) — external NATS testcontainer helper with per-replica conns (models `test/testutil/postgres.go`).
- [ ] `internal/eventbus/config_test.go` additions — `Validate()` fail-closed cases.
- [ ] `internal/eventbus/audit/dlq_integration_test.go` — DLQ capture + Nak-on-DLQ-fail + replay.
- [ ] `test/integration/crypto/cache_invalidation_test.go` — re-point/extend to external-NATS multi-conn harness; add `// Verifies:` INV-CLUSTER-1/2/9.
- [ ] `test/integration/cluster/cluster_test.go` — multi-node cluster_id-filtering (INV-CLUSTER-4) + hung-replica probe-pill (D-08) over external conns.
- [ ] Boot single-principal self-check test (CLUSTER-02).
- [ ] Amend CLAUDE.md / `.claude/rules/testing.md` "embedded NATS is correct at every tier" rule (D-06) — same change as the new tests.

## Security Domain

> `security_enforcement` treated as enabled. CLUSTER-02 is itself a security control (least-privilege subject scoping).

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | NATS JWT/NKey `.creds` decentralized auth (D-04); mTLS via TLS block |
| V4 Access Control | yes | Single-principal account subject scoping (`events.>`/`audit.>`/`internal.>`), default-deny at NATS account layer (CLUSTER-02) |
| V6 Cryptography | yes (indirect) | DEK/KEK model unchanged; TLS-in-transit for external NATS; do NOT hand-roll — nats.go TLS + existing crypto substrate |
| V7 Errors/Logging | yes | Fail-closed boot errors are coded oops errors; audit DLQ preserves the audit trail (no silent drop) |

### Known Threat Patterns
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Rogue principal publishes/subscribes on game topics | Spoofing / Tampering / Info-disclosure | Account subject permissions restrict to `holomush-server`; external verify script proves lockout (D-13) |
| Server account over-scoped (privilege creep) | Elevation | Boot-time self-check refuses to start if the server's own account can reach beyond the three prefixes (D-13) |
| Audit dead-letter silently dropped → audit-trail gap | Repudiation | DLQ capture + Nak-on-failure guarantees no drop (D-09); replay restores to `events_audit` |
| External NATS unreachable → degraded/insecure fallback | DoS / policy bypass | Fail-closed at boot (D-02); no embedded fallback |
| Cross-cluster message injection | Tampering | `cluster_id`-prefixed subjects; payload cluster_id mismatch dropped (INV-CLUSTER-4, `coordinator.go:346`, `heartbeat.go:268`) |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `audit.>` prefix (CLUSTER-02 scope) is where the DLQ subject should nest (`audit.dlq...` or `internal.<id>.audit.dlq`) | OQ-3 | Low — subject choice is internal; adjust to stay within the server account's granted prefixes. Confirm against phase3d-grounding spec's exact subject grant list. |
| A2 | Single external NATS node (not a NATS cluster) suffices to bind INV-CLUSTER-1/2/4/9 | OQ-4 | Medium — if an invariant secretly needs NATS-level replication the harness under-tests it; but the invariants are about HoloMUSH replicas, so low actual risk. Confirm by reading each invariant's summary (done: `invariants.yaml:2176-2306`). |
| A3 | nats.go's built-in reconnect covers all post-boot transient drops without extra config | OQ / D-02 | Low — verify default `MaxReconnects` behavior; set `nats.MaxReconnects(-1)` explicitly if infinite reconnect is desired. |
| A4 | The exact `.creds`/TLS koanf sub-shape and compose secret-mount strategy | OQ-2 | Low — cosmetic; document in runbook. |
| A5 | Reconcile `ModeCluster="cluster"` vs D-01 `external`: rename to `ModeExternal` (keeping `cluster` as a legacy alias is optional) | OQ-1/OQ-2 | Medium — pick one; the config keys `cluster_url`/`credentials_file` already exist and any external YAML written against them must be reconciled with D-01's `url`/`credentials`. This is a real decision the planner must lock. |

## Open Questions

1. **Exact NATS account grant subjects for `holomush-server`** — CONTEXT says `events.>`, `audit.>`, `internal.>`. Confirm the DLQ subject and the `_INBOX.>` reply-inbox needed by invalidation request-reply (`coordinator.go:253` `NewRespInbox`) are within granted permissions, or the account template must also grant `_INBOX.>`.
   - What we know: cluster + invalidation use `internal.<id>...` and request-reply inboxes.
   - What's unclear: whether default NATS `_INBOX.>` is implicitly permitted or must be granted in the template.
   - Recommendation: grant `_INBOX.>` (or the `allow_responses` account option) explicitly in `deploy/nats/` and cover it in the verification script.

2. **`compose.cluster.yaml` core-replica identity** — two core replicas share one Postgres and one NATS; confirm nothing in the core boot assumes single-instance (e.g., migration runner, seed bootstrap). `bootstrapSub` runs seed migrations — running it twice concurrently could race.
   - Recommendation: the smoke should start replicas with one running migrations/bootstrap (or `--skip-seed-migrations` on the second), documented in the runbook.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker | int/E2E testcontainers + compose smoke | assumed (CI has it for Postgres int) | — | none — int tier already requires Docker |
| `nats:2-alpine` image | external-NATS testcontainer + compose | pullable from Docker Hub | 2.x | pin a digest in compose/prod per repo convention |
| `nsc` CLI | operator account provisioning (docs only) | operator-side | — | document JWT/creds via `nsc` in runbook; not a build dep |
| testcontainers-go | int harness | ✓ | v0.43.0 | — |

**Missing with no fallback:** none — all build/test deps are in-tree. The only new *runtime* artifact is an external NATS server, provided by the testcontainer (tests) and `compose.cluster.yaml` (smoke/ops).

## Sources

### Primary (HIGH confidence — in-tree, verified this session)
- `internal/eventbus/subsystem.go:12,24,82-190,193-211,274-299` — embedded boot, EnsureStream, Conn()/JS() seams, exporter caveat
- `internal/eventbus/config.go:8-60,92-106` — Mode/ClusterURL/CredentialsFile stubs; Defaults (no Validate)
- `internal/eventbus/audit/subsystem.go:55-92` — MaxDeliver default + Phase-B DLQ TODO + Config struct
- `internal/eventbus/audit/projection.go:46,99-120,216-249,259-356` — consumer create, handle() hook, persist, retained js
- `internal/eventbus/natsconn/natsconn.go:4-14,57-91` — cluster/invalidation are core-NATS-only, transport-agnostic
- `internal/cluster/registry.go`, `types.go`, `internal/eventbus/crypto/invalidation/coordinator.go:69-406` — shipped protocol; core pub/sub
- `cmd/holomush/core.go:86,137,231,450,458,498-523,880-926,1396-1421` — config load, wiring, productionSubsystems
- `cmd/holomush/core_subsystems_test.go:32-107` — 15-count test cascade
- `cmd/holomush/root.go:37-43`, `cmd_crypto_rekey.go:66-110`, `cmd_plugin_events.go:61-62` — cobra group precedents
- `docs/architecture/invariants.yaml:2176-2306` — INV-CLUSTER-1..10 (1 & 8 pending; 2/3/4/5/6/7/9/10 bound to single-conn tests)
- `test/integration/crypto/cache_invalidation_test.go:41` — shared-conn gap; `internal/cluster/clustertest/harness.go:88-92`
- `test/testutil/postgres.go:1-45` — testcontainer helper pattern
- `Taskfile.yaml` test:int — `./...`, no package list
- nats.go@v1.52.0 `jetstream/message.go:37,66,78,87,97` + `jetstream/jetstream.go:74,79,136,144` + `nats.go:4235` — Metadata/Term/Nak/PublishMsg/Header

### Secondary (MEDIUM — design specs, cited not re-derived)
- `docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md` — single-principal account scoping (CLUSTER-01/02)
- `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` — bus design embedded-now/external-later
- `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` — cluster invalidation invariant origin
- `.claude/rules/invariants.md` — binding ratchet (no fabricated bindings)

## Metadata

**Confidence breakdown:**
- Standard stack / APIs: HIGH — every signature verified against the module cache and in-tree call sites
- Architecture / mode seam: HIGH — grounded in the actual `Start` shape and downstream consumers
- DLQ semantics: HIGH — jetstream.Msg API and existing handle() behavior read directly
- Invariant binding plan: HIGH — read the 10 CLUSTER entries and their current binding state
- Config naming reconciliation: MEDIUM — the drift is real (verified); the *choice* of resolution is a planner decision (A5)

**Research date:** 2026-07-10
**Valid until:** ~2026-08-10 (stable brownfield; nats.go/testcontainers versions pinned)
