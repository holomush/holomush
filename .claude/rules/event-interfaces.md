<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

---
paths:
  - "internal/eventbus/**"
  - "internal/plugin/**"
  - "pkg/plugin/**"
  - "internal/grpc/**"
---

# Event System Key Interfaces

## EventBus (`internal/eventbus`)

The EventBus replaced the former `EventStore.Append` / `LISTEN`/`NOTIFY` stack as of the F1-F7 JetStream cutover. The old `EventStore` interface is deleted.

Three narrow interfaces cover the three consumer roles:

```go
// Publisher — used by EventSink (emit path from plugins and host).
type Publisher interface {
    Publish(ctx context.Context, event Event) error
}

// Subscriber — used by the gRPC Subscribe handler.
type Subscriber interface {
    OpenSession(ctx context.Context, sessionID string, identity SessionIdentity, filters []Subject, minFloor time.Time) (SessionStream, error)
}

// HistoryReader — used by the gRPC QueryHistory handler.
type HistoryReader interface {
    QueryHistory(ctx context.Context, q HistoryQuery) (HistoryStream, error)
}

// EventBus is the concrete implementation satisfying all three.
type EventBus interface {
    Publisher
    Subscriber
    HistoryReader
}
```

**Ordering** is owned by JetStream's per-stream `uint64` sequence. Event ULIDs (`core.Event.ID`) are identity and dedup keys, **not** ordering keys.

**Durable audit** lives in the `events_audit` PostgreSQL table (host-owned subjects) and in plugin-owned audit tables (plugin-declared subjects; e.g., `plugin_core_scenes.scene_log`). `HistoryReader.QueryHistory` transparently falls back from JetStream (recent) to PostgreSQL audit (older than JS retention) so callers never see the boundary.

**Subject naming** follows NATS dot-delimited conventions: `events.<game_id>.<domain>.<entity-id>[.<facet>...]`. Legacy colon-style subjects (e.g., `scene:01ABC`) are translated at the EventSink boundary by `internal/eventbus/subjectxlate/`.

**See:**
- `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` — full design (§3 publish, §4 subscribe, §5 history, §6 PostgreSQL role)
- `site/docs/contributing/event-store.md` — contributor examples (plugin emit, manifest audit declarations, embedded vs cluster NATS)

## Current-state RPCs

Some UX flows need "snapshot of current state" rather than event replay.
These bypass the EventBus entirely and read the relevant store directly:

| Surface     | RPC                              | Source                               |
| ----------- | -------------------------------- | ------------------------------------ |
| Presence    | `CoreService.ListFocusPresence`  | `session.Store.ListActiveByLocation` |

Adding a new current-state surface follows the same shape: a CoreServer
RPC + an ABAC action on the relevant resource + a narrow store query.
Do NOT reach for `HistoryReader.QueryHistory` when the requirement is
"what's the current state" — `HistoryReader` is for historical events.

## ServiceRegistry (`internal/plugin`)

Maps proto service names (e.g., `holomush.scene.v1.SceneService`) to registered service implementations. Used by the plugin loader to wire up service dependencies between plugins.

## ServiceProvider (`pkg/plugin`)

Interface implemented by binary plugins that provide gRPC services. The plugin host calls `RegisterServices` during plugin startup to let the plugin register its service implementations with the server.
