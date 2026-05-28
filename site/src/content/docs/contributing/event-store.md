---
title: "Event Store & EventBus"
---

This page describes how events flow through HoloMUSH after the JetStream
EventBus cutover (F1-F7, `feat/eventbus-cutover`).

**Design spec**: [docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md](https://github.com/holomush/holomush/blob/main/docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md)

---

## Overview

All game events are published to an embedded NATS JetStream server
(`internal/eventbus`). JetStream owns ordering via a per-stream `uint64`
sequence. PostgreSQL is a projection and forever-archive target, not the
primary event log.

```text
Plugin / Host code
    │
    │  eventSink.Emit(ctx, EmitIntent{Subject, Type, Payload})
    ▼
EventSink (host validates subject, stamps ID + Actor + Timestamp)
    │
    │  bus.Publish(ctx, event)
    ▼
JetStream stream "EVENTS" (subjects: events.>)
    │
    ├──▶ Session consumer (durable, per-sessionID) → gRPC Send → client
    │
    ├──▶ host_audit_projection consumer → events_audit (PostgreSQL)
    │
    └──▶ plugin_audit_<name> consumer → plugin-owned audit table (PostgreSQL)
```

---

## EventBus interfaces

The bus is split into three narrow interfaces (§4 of the design spec):

```go
// Publisher — emit path, used by EventSink.
type Publisher interface {
    Publish(ctx context.Context, event Event) error
}

// Subscriber — long-lived session streams, used by the gRPC Subscribe handler.
type Subscriber interface {
    OpenSession(ctx context.Context, sessionID string, filters []Subject) (SessionStream, error)
}

// HistoryReader — paginated history, used by the gRPC QueryHistory handler.
type HistoryReader interface {
    QueryHistory(ctx context.Context, q HistoryQuery) (HistoryStream, error)
}
```

Each handler depends only on the interface it needs. The concrete `eventBus`
struct satisfies all three. Tests depend on the narrow interface and mock it
independently.

`HistoryReader.QueryHistory` transparently serves events from JetStream (recent,
within 30-day retention) or PostgreSQL `events_audit` (older). Callers never
see the boundary; the cursor is always a ULID.

---

## Current-state queries

Some UX surfaces need a "snapshot of who/what is here right now" rather
than a stream of historical events. These bypass `HistoryReader` entirely:
they hit the relevant store (e.g., session store for presence) and return
a current-state list. See `CoreService.ListFocusPresence` (`holomush-5b2j`)
as the canonical example. Snapshots are NOT subject to the I-PRIV-1
temporal floor — by design they reflect current state, not history.

---

## Subject naming convention

Subjects follow NATS dot-delimited conventions (spec §1c):

```text
events.<game_id>.<domain>.<entity-id>[.<facet>...]
```

`<game_id>` is set via `event_bus.game_id` in server config (default: `main`).
Single-game deployments use a literal; the game-id segment is reserved for
future multi-tenancy.

| Subject | Owner | Examples |
| ------- | ----- | -------- |
| `events.<game>.location.<ULID>` | host | location-scoped events |
| `events.<game>.character.<ULID>` | host | personal stream (DMs, responses) |
| `events.<game>.session.<ULID>.lifecycle` | host | session lifecycle events |
| `events.<game>.notifications.character.<ULID>` | host | cross-scene notifications |
| `events.<game>.scene.<ULID>.ic` | core-scenes plugin | in-character poses/says |
| `events.<game>.scene.<ULID>.ooc` | core-scenes plugin | out-of-character chat |
| `events.<game>.scene.<ULID>.lifecycle` | core-scenes plugin | scene created/ended |

`*` matches one token; `>` matches one or more (must be last). Subject depth
SHOULD stay below 16 tokens.

**Legacy colon-style subjects** (e.g., `scene:01ABC...`) are translated at the
EventSink boundary by `internal/eventbus/subjectxlate/`. All new code MUST use
dot-delimited subjects.

---

## How plugins emit events

Plugins emit via `EventSink`, unchanged from the pre-cutover API:

```go
// In a plugin event handler (Go binary plugin example):
err := eventSink.Emit(ctx, plugin.EmitIntent{
    Subject: "events.main.scene.01JABC...XYZ.ic",
    Type:    "scene.pose",
    Payload: marshaledProtoBytes,
})
```

The host validates `Subject` against the plugin's manifest `emits` declarations
before publishing. Invalid subjects are rejected pre-publish.

**Subject MUST use dot-delimited form.** The `Type` field is plugin-declared and
opaque to the host; it is stored verbatim and exposed to subscribers via the
`App-Event-Type` NATS header for filter-without-decode.

**Payload MUST be proto-marshaled bytes.** The host applies the codec (identity
by default) before writing to JetStream. Plugins never interact with the codec
directly.

---

## Declaring audit subjects in `manifest.yaml`

Plugins that own a subject namespace declare it in their manifest so the host
can route audit projection to the plugin's own consumer (spec §1c, §6):

```yaml
# plugins/core-scenes/manifest.yaml
name: core-scenes
type: binary
version: "0.1.0"

emits:
  - "events.*.scene.*.ic"
  - "events.*.scene.*.ooc"
  - "events.*.scene.*.lifecycle"

audit:
  - subjects: ["events.*.scene.>"]
    schema:   plugin_core_scenes
    table:    scene_log
```

The `audit:` block tells the host to route events matching
`events.*.scene.>` to a durable consumer named
`plugin_audit_core_scenes`. On each delivered message, the host calls
the plugin's `PluginAuditService.AuditEvent` RPC. The plugin INSERTs
into its own `plugin_core_scenes.scene_log` table and acks.

Host-owned subjects (everything not covered by a plugin `audit:` declaration)
are audited by the `host_audit_projection` consumer into `events_audit`.

Subject ownership resolution is **longest-prefix-wins, token-aligned**. Startup
fails with a fatal error if two plugins declare the same exact prefix.

---

## Running NATS: embedded (default) vs cluster (future)

### Embedded mode (default — Phase B)

HoloMUSH runs a NATS server in-process. No external NATS installation is
needed. The server binds no network port (`DontListen: true`) — it is only
reachable via the in-process connection.

Key config knobs:

```yaml
event_bus:
  mode: embedded          # "embedded" (default) | "cluster" (future)
  game_id: "main"         # first subject token after "events."
  store_dir: ""           # blank = $XDG_DATA_HOME/holomush/jetstream
  stream_max_age: 720h    # 30-day retention in JetStream
  dupe_window: 30m        # Nats-Msg-Id dedup window
  monitor_port: 0         # 0 = disabled; set to expose NATS HTTP monitoring
  prometheus_exporter: true
```

JetStream storage lives at `$XDG_DATA_HOME/holomush/jetstream/`. The directory
is lock-exclusive — only one process per directory. Do not share it between
instances.

### Cluster mode (future)

External NATS cluster is planned but not yet implemented. The `eventbus.Bus`
interface is the seam — switching from embedded to cluster NATS requires only a
different transport configuration, not interface changes. The `cluster_url`,
`credentials_file`, and `tls` config keys are reserved.

---

## Design lesson: the runaway-redelivery guardrail

During the F4 tier-crossover implementation, a missing cursor-advancement bug
caused the host audit projection worker to redeliver the same batch of events
in a tight loop. The result was a large volume of redundant audit inserts before
the bug was caught by the idempotent `ON CONFLICT DO NOTHING` guard in
`events_audit`.

**The guard that saved it:** every audit insert is:

```sql
INSERT INTO events_audit(...) VALUES (...) ON CONFLICT (id) DO NOTHING;
```

Because `id` (ULID) is the primary key and each event has a unique ULID, a
runaway redelivery loop produces zero duplicate rows — only duplicate no-op
inserts. Data integrity is preserved.

**What to watch for:** `audit_projection_lag_seconds` spiking without a
corresponding increase in unique rows in `events_audit` is the observable
signature of a redelivery loop. Alert threshold: > 5s. The lag metric is
emitted by the `SubsystemAuditProjection` health reporter.

**Structural prevention:** `MaxAckPending = 64` per consumer caps inflight
unacked messages. A stuck consumer stalls at 64 messages rather than
accumulating unbounded backpressure. `AckPolicy = AckExplicit` means the
consumer only advances its cursor after the handler explicitly acks — a handler
crash leaves the cursor in place and triggers redelivery, which the idempotent
insert absorbs cleanly.

---

## Related

- Design spec: `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`
  (§1 event model, §3 publish, §4 subscribe, §5 history, §6 PostgreSQL role)
- `internal/eventbus/` — bus implementation
- `internal/eventbus/subjectxlate/` — legacy subject translation
- `internal/eventbus/eventbustest/` — test harness (`New(t)`)
- `pkg/plugin/event_sink.go` — plugin-facing emit API
