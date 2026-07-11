# Phase 3: Platform Hardening & Deployment Scaling - Pattern Map

**Mapped:** 2026-07-10
**Files analyzed:** 14 create/modify targets (from CONTEXT D-01..D-16 + RESEARCH file table)
**Analogs found:** 12 / 14 (2 net-new with partial analog)

This is a **verification-and-extension** phase. Almost every new file has a strong
in-tree analog; the dominant pattern is "branch/extend an existing subsystem," not
"build new." RESEARCH.md already carries grounded `path:line` citations — this map
turns them into per-file copy-from assignments.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/eventbus/subsystem.go` (MODIFY) | subsystem/boot | request-response (connect) | itself — embedded `Start` path `subsystem.go:82-190` | exact (self-refactor) |
| `internal/eventbus/config.go` (MODIFY) | config | config | itself — `Config`/`Defaults` `config.go:31-106` | exact (self-extend) |
| `internal/eventbus/natsdial.go` (CREATE, opt) | utility | request-response (dial) | embedded connect block `subsystem.go:120-146` | role-match |
| `internal/eventbus/scopecheck.go` (CREATE) | boot self-check | request-response | fail-closed boot in `Start` (KEK precedent) | role-match |
| `internal/eventbus/audit/subsystem.go` (MODIFY) | config/subsystem | config | itself — `Config`/`DefaultMaxDeliver` `subsystem.go:55-92` | exact (self-extend) |
| `internal/eventbus/audit/projection.go` (MODIFY) | consumer | event-driven (JS pull) | itself — `handle`/`persist` `projection.go:216-249` | exact (self-extend) |
| `internal/eventbus/audit/dlq.go` (CREATE) | helper | pub (JS publish) | `EnsureStream` `subsystem.go:193-211` + `persist` metadata read | role-match |
| `internal/testsupport/natstest/` (CREATE) | test-harness | request-response | `test/testutil/postgres.go:1-50` (testcontainer) | role-match |
| `cmd/holomush/core.go` (MODIFY) | wiring | request-response | itself — config-load + `productionSubsystems` | exact (self-extend) |
| `cmd/holomush/cmd_audit.go` (CREATE) | CLI (cobra) | CRUD/batch (replay) | `cmd_crypto_rekey.go:67-111` (`NewCryptoCmd` group) | role-match |
| `test/integration/crypto/cache_invalidation_test.go` (MODIFY) | test | event-driven | itself — `newCoordOnMember` `:30-63` | exact (self-rebind) |
| `test/integration/cluster/cluster_test.go` (MODIFY) | test | event-driven | itself + `clustertest.Harness` | exact (self-extend) |
| `deploy/nats/` (CREATE) | config/ops asset | n/a | no in-tree analog (see No Analog) | none |
| `compose.cluster.yaml` (CREATE) | ops overlay | n/a | `compose.prod.yaml` / `compose.e2e.yaml` | role-match |
| `site/src/content/docs/operating/how-to/...` (CREATE) | doc | n/a | `sandbox/sandbox-operations.md` | role-match |
| `docs/architecture/invariants.yaml` (MODIFY) | registry | n/a | INV-CLUSTER-1..10 entries `:2176-2306` | exact (self-extend) |

## Pattern Assignments

### `internal/eventbus/subsystem.go` (mode branch — CLUSTER-01)

**Analog:** itself. The embedded boot is fully inline in `Start`. Per OQ-1, extract
a private `connect(ctx) (*nats.Conn, jetstream.JetStream, error)` with `switch s.cfg.Mode`.

**Embedded connect block to preserve verbatim as the `embedded` case** (`subsystem.go:120-146`):
```go
conn, err := nats.Connect(
    "",
    nats.InProcessServer(s.server),
    nats.Name(embeddedClientName),
    nats.DrainTimeout(readyTimeout),
)
if err != nil {
    s.server.Shutdown()
    s.server.WaitForShutdown()
    s.server = nil
    return oops.Code("EVENTBUS_CONNECT_FAILED").Wrap(err)
}
s.conn = conn

js, err := jetstream.New(conn)
```

**External case to add** (mirror shape; RESEARCH "External connect" example, fail-closed per D-02):
```go
conn, err := nats.Connect(cfg.URL,
    nats.Name("holomush-server"),
    nats.UserCredentials(cfg.Credentials),   // .creds (D-04)
    // nats.RootCAs(cfg.TLS.CA), nats.ClientCert(cfg.TLS.Cert, cfg.TLS.Key) — optional mTLS
)
if err != nil { return oops.Code("EVENTBUS_EXTERNAL_CONNECT_FAILED").Wrap(err) }
js, err := jetstream.New(conn)
```

**Mode-independent tail stays as-is:** `EnsureStream` (`subsystem.go:193-211`) operates
only on `s.js` — no change needed for external mode. The exporter block
(`subsystem.go:163-188`) reads `s.server.MonitorAddr()`; it MUST be guarded so it is
**skipped when `mode==external`** (`s.server` is nil — OQ-7).

**Provision opt-out (D-03)** — copy the `EnsureStream` `CreateOrUpdateStream` call
(`subsystem.go:197-206`) and add a `provision:false` branch: `js.Stream(ctx, StreamName)`
+ compare `StreamInfo.Config` to desired, fail closed with a coded oops error on mismatch.

**Coded-error + rollback convention** (copy from every branch in `Start`): `oops.Code("EVENTBUS_...").Wrap(err)` / `.With(k,v).Errorf(...)`, and on mid-Start failure close conn / nil the fields.

---

### `internal/eventbus/config.go` (eventbus config section — CLUSTER-01, D-01)

**Analog:** itself. CRITICAL naming reconciliation (OQ-2, A5): the struct **already**
declares conflicting names the planner must reconcile as the first config task.

**Current shape to reconcile** (`config.go:8-17`, `54-55`):
```go
const (
    ModeEmbedded Mode = "embedded"
    ModeCluster  Mode = "cluster"   // D-01 says "external", not "cluster"
)
// ...
ClusterURL      string `koanf:"cluster_url"`       // D-01 says url
CredentialsFile string `koanf:"credentials_file"`  // D-01 says credentials
```

**Defaults() pattern to extend** (`config.go:92-106`) — zero-value guards only, no side effects:
```go
func (c Config) Defaults() Config {
    if c.Mode == "" { c.Mode = ModeEmbedded }
    // ...
    return c
}
```

**New: add `Validate()`** — there is none today. Return a coded oops error when
`Mode==external && URL==""` (fail at config-validation time, D-01). Model the call site
on `coreConfig.Validate()` (invoked `core.go:231`); call right after
`config.Load(...,"event_bus")` at `core.go:137`.

**TLS/creds/provision/dlq nested structs** — follow the existing `CryptoConfig`
nested-struct + `koanf:"..."` tag pattern (`config.go:59,68-73`).

---

### `internal/eventbus/audit/projection.go` + `dlq.go` (DLQ capture — CLUSTER-04, D-09)

**Analog:** `projection.handle` / `persist` (`projection.go:216-249,259-298`).

**The exact seam to modify** — current no-ack poison path (`projection.go:237-243`):
```go
if err := p.persist(msg); err != nil {
    // Deliberate no-ack: JetStream will redeliver after AckWait.
    // We do not Nak() because Nak triggers INSTANT redelivery...
    return
}
```

**Change to** (RESEARCH "Reading delivery count + DLQ decision"; note `msg.Metadata()`
is already used at `projection.go:290`, NOT the legacy `msg.Info()`):
```go
if err := p.persist(msg); err != nil {
    meta, mErr := msg.Metadata()  // MsgMetadata.NumDelivered
    if mErr == nil && meta.NumDelivered >= uint64(p.cfg.MaxDeliver) {
        if dlqErr := p.dlq.Capture(p.workerCtx, msg); dlqErr != nil {
            _ = msg.Nak()   // DLQ publish failed → keep redelivering; never drop (D-09)
            return
        }
        _ = msg.Term()      // captured → stop redelivery (D-09)
        return
    }
    return  // below cap: unchanged AckWait redelivery
}
```

**`dlq.go` helper (D-10)** — copy the `EnsureStream` idempotent-provision shape
(`subsystem.go:197-206`) for a `EVENTS_AUDIT_DLQ` stream (MaxAge/MaxBytes from
`event_bus.dlq` config, D-12), plus a `Capture(ctx, msg)` doing header-preserving publish
(RESEARCH "DLQ publish preserving headers"):
```go
_, err := js.PublishMsg(ctx, &nats.Msg{
    Subject: dlqSubject,     // internal.<game_id>.audit.dlq.>
    Header:  msg.Headers(),  // preserves Nats-Msg-Id + audit headers for replay dedup
    Data:    msg.Data(),
})
```

**Metric:** a `holomush_audit_dlq_messages_total` prometheus counter; register on
`prometheus.DefaultRegisterer`, mirroring the existing audit counters (`SkippedPluginOwnedTotal.WithLabelValues(...).Inc()` at `projection.go:232`).

**`audit/subsystem.go` (MODIFY):** DLQ knobs join `Config` (`subsystem.go:64-92`), following
the existing field-plus-`Defaults()` idiom; remove the `TODO (Phase B): wire a DLQ` comment
at `subsystem.go:59-60`.

---

### `internal/testsupport/natstest/` (external-NATS testcontainer — CLUSTER-03, OQ-4)

**Analog:** `test/testutil/postgres.go:1-50`. Single external NATS node (JetStream), NOT a
3-node NATS cluster (OQ-4 — invariants bind on HoloMUSH replicas, not NATS replication).

**Imports + container-env struct pattern to copy** (`postgres.go:7-33`):
```go
import (
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/wait"
)
type PostgresEnv struct {
    Container testcontainers.Container
    ConnStr   string
    // ...
}
```

Build with generic `testcontainers.GenericContainer` running `nats:2-alpine` with
`-js -sd /data` and `wait.ForListeningPort("4222/tcp")` (no NATS testcontainer module in
go.mod — avoid adding one). Expose a `URL` so each replica does its **own**
`nats.Connect(container.URL)`.

---

### `test/integration/crypto/cache_invalidation_test.go` (multi-node rebind — CLUSTER-03, D-07)

**Analog:** itself — `newCoordOnMember` (`:30-63`). **The gap to close** (`:41`):
```go
}, invalidation.Deps{
    Conn:      h.Embedded.Conn,   // ← SHARED conn across all members: the CLUSTER-03 gap
    Registry:  h.Members[i].Registry,
    // ...
}
```

Give member `i` its own `nats.Connect(container.URL)` from the `natstest` helper.

**Invariant-binding annotation pattern to copy** (`:66`): `// Verifies: INV-CLUSTER-2`
immediately above the `Describe`/`It`. Per D-07: bind INV-CLUSTER-1/2/4/9 via the new
multi-conn harness; for already-`bound` entries ADD the multi-node test as an additional
`asserted_by`. **No fabricated bindings** — for 3/5/6/7/8/10 annotate only where a test
genuinely asserts; else leave `binding: pending` + file a coverage issue.

---

### `cmd/holomush/cmd_audit.go` (DLQ replay CLI — CLUSTER-04, OQ-5)

**Analog:** `NewCryptoCmd` group (`cmd_crypto_rekey.go:67-111`) + registration in
`root.go:37-43`. CONTEXT's "no plugin/audit cobra group" note is stale.

**Parent-group + subcommand pattern to copy** (`cmd_crypto_rekey.go:69-77`):
```go
func NewCryptoCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "crypto", Short: "..."}
    cmd.AddCommand(newCryptoRekeyCmd(factory))
    return cmd
}
```
Produce `NewAuditCmd()` → `audit dlq {list,show,replay}`. Register with exactly one
`cmd.AddCommand(NewAuditCmd())` in `NewRootCmd` (`root.go:37-43` — flat list, no arity cascade).
Unlike crypto it does NOT need the admin UDS: dial NATS (read DLQ) + Postgres (write
`events_audit` via the projection persist path).

---

### `cmd/holomush/core.go` (wiring — OQ-6)

**Analog:** itself. **DO NOT change `productionSubsystems` arity** — it is a 15-named-param
function with a hard count cascade (`core_subsystems_test.go` asserts `len==15`). External
mode + DLQ extend EXISTING subsystems. Only changes: plumb reconciled `eventBusConfig` into
`eventbus.NewSubsystem` (`core.go:450`), DLQ config into `audit.Config` (`core.go:523`), and
invoke the CLUSTER-02 boot self-check after `eventBusSub.Start` (`core.go:458`) in external mode.
Call `eventBusConfig.Validate()` after load (mirror `coreConfig.Validate()` at `core.go:231`).

---

### `docs/architecture/invariants.yaml` (MODIFY — D-07, OQ-8)

**Analog:** INV-CLUSTER entries `:2176-2306`. Entry shape to copy (`:2186-2198`):
```yaml
  - id: INV-CLUSTER-2
    scope: INV-CLUSTER
    origin_spec: "..."
    summary: "..."
    asserted_by:
      - "test/integration/crypto/cache_invalidation_test.go"
    binding: bound
```
Per OQ-8, optionally mint two `INV-EVENTBUS-N` entries (external-boot-fail-closed;
DLQ-never-drops) under the EVENTBUS scope (`:2308`). `binding: pending` unless the harness
genuinely asserts. Regenerate with `go run ./cmd/inv-render` in the same change; never fabricate.

## Shared Patterns

### Fail-closed boot (D-02, CLUSTER-01/02)
**Source:** every rollback branch in `subsystem.go:102-188` + `coreConfig.Validate()` at `core.go:86,231`.
**Apply to:** external connect, provision-mismatch, boot self-check.
Return `oops.Code("EVENTBUS_...").With(k,v).Wrap(err)`; refuse to Start — the orchestrator owns retry. No embedded fallback.

### Idempotent stream provisioning
**Source:** `EnsureStream` `js.CreateOrUpdateStream(...)` (`subsystem.go:197-206`).
**Apply to:** external EVENTS provisioning, `provision:false` verify-or-fail branch, DLQ stream ensure.

### koanf nested-struct config + Defaults()
**Source:** `Config`/`CryptoConfig`/`Defaults` (`config.go:31-106`).
**Apply to:** eventbus TLS/creds/provision/dlq sub-config; audit DLQ knobs.

### `jetstream.Msg` API (NEW package, not legacy)
**Source:** `projection.go:290` (`msg.Metadata()`), `projection.go:260` (`msg.Headers()`).
**Apply to:** DLQ capture. Use `Metadata().NumDelivered`, `Term()`, `Nak()`, `PublishMsg` —
NEVER the legacy `msg.Info()` (compile error; Pitfall 1).

### Invariant `// Verifies:` binding
**Source:** `cache_invalidation_test.go:66`.
**Apply to:** all new multi-node specs; add `asserted_by` in `invariants.yaml`, regenerate.

### Testcontainer helper
**Source:** `test/testutil/postgres.go:1-50`.
**Apply to:** `natstest` external-NATS helper.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `deploy/nats/` (account templates + nsc walkthrough + verify script) | ops asset | n/a | No existing NATS-account config or `deploy/nats/` tree; net-new. Ground content in `docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md` (grant list `events.>`/`audit.>`/`internal.>`, plus `_INBOX.>` per Open Question 1). Planner MUST confirm the DLQ + reply-inbox subjects fall within granted permissions. |
| `internal/eventbus/scopecheck.go` (boot self-check) | boot self-check | request-response | No prior "verify my own NATS account is not over-scoped" analog. Structural analog = fail-closed boot convention above; the check itself (attempt-publish-beyond-prefixes → refuse) is net-new logic. |

## Metadata

**Analog search scope:** `internal/eventbus/**`, `internal/eventbus/audit/**`,
`internal/cluster/**`, `cmd/holomush/**`, `test/integration/crypto|cluster/**`,
`test/testutil/**`, `docs/architecture/invariants.yaml`.
**Files scanned (read this session):** subsystem.go, config.go, audit/subsystem.go,
audit/projection.go, cache_invalidation_test.go, root.go, cmd_crypto_rekey.go,
postgres.go, invariants.yaml. Remaining citations inherited verbatim from 03-RESEARCH.md.
**Pattern extraction date:** 2026-07-10
