# JetStream Event Log + PostgreSQL Audit Projection — Design

## Status

**DRAFT** — design proposal pending implementation plan.

Supersedes: [docs/specs/2026-03-20-event-delivery-redesign.md](../../specs/2026-03-20-event-delivery-redesign.md)
(LISTEN/NOTIFY-based delivery; this design replaces both the event log and the
delivery transport).

## Authors

- Sean Brandt
- Claude (collaborator)

## Date

2026-04-18

---

## Context

The current event store (`internal/store/postgres.go`) uses a PostgreSQL
`events` table as the durable log and `LISTEN`/`NOTIFY` for live fan-out.
This architecture has produced a recurring class of timing races because
notification delivery is asynchronous relative to the durable `INSERT`:

- **Cursor persistence race** (Finding 1, PR #201) — `Send` → `UpdateCursors`
  gap mitigated but not eliminated.
  ([docs/plans/2026-04-07-cursor-lock-finding-1-closure.md](../../plans/2026-04-07-cursor-lock-finding-1-closure.md))
- **Quit-teardown race** — `holomush-h9fp` telnet E2E flake (a separate but related instance, `holomush-umxj`, was fixed independently in PR #237 — F3's Subscribe handler rewrite must not regress it).
- **Reconnect/replay flakes** — `holomush-0jzs` (four `terminal.spec.ts` tests).
- **Test ULID ordering crutches** — six+ `time.Sleep(1ms)` calls in
  `postgres_integration_test.go` to dodge ULID collision-in-same-ms.
- **Missed notifications** — `pg_notify` does not buffer; disconnected
  listeners require a 30-second stale-cache fallback.

Every one of these is a variant of *"notification delivery decoupled from
durable write."* `EventWriter` exists as a global serializer to enforce
monotonic ULIDs (Invariant I-14) — it is the bottleneck and the source of
test fragility, but cannot be removed without a different ordering primitive.

The current design also conflicts with the project boundary rules: 19
`EventType` constants live in `internal/core/event.go:39-70` despite the
plugin boundary rule that "plugin-owned types MUST NOT leak into
`internal/core/`".

This design replaces both the event log and the delivery transport with
NATS JetStream (embedded in-process today, externally clustered later) and
relegates PostgreSQL to identity, projection, and forever-archive audit.

### Project context

- Single-node deployment, ~200 concurrent users, ~1k events/sec target
- Pre-launch — clean cutover acceptable, no data migration required
- PostgreSQL remains MANDATORY for identity, ABAC, projections, and audit
- No Redis/Valkey
- No PostgreSQL triggers or stored procedures (per `CLAUDE.md`)
- Plugin boundary: plugin-owned types and event semantics MUST NOT live in
  `internal/core/`

### Adjacent in-flight work

- PR #233: session-lifecycle-events (this design depends on it for consumer GC)
- PR #234: LeaveFocusByTarget (parallel; not in conflict)
- Channel plugin (`holomush-0sc.12`, paused) — resumes after this cutover with
  subject names + audit declarations baked in
- Scene Phase 4 (`holomush-5rh.13`) — currently blocked on event store
  clarity; this design unblocks it

---

## Goals

- **MUST** eliminate the cursor / Send-vs-Ack race class by construction
- **MUST** eliminate `EventWriter`'s global serialization and the
  `time.Sleep(1ms)` test crutches
- **MUST** preserve cross-stream strict ordering (formerly Invariant I-14)
- **MUST** keep PostgreSQL as a durable audit and projection target
- **MUST** present a clean seam for cluster-mode NATS migration later
- **MUST** keep the plugin boundary intact: plugins emit via the existing
  `EventSink` facade; event types and subjects are plugin-declared
- **MUST** support encryption-at-rest seam without committing to a
  particular crypto implementation today
- **SHOULD** dramatically simplify the gRPC `Subscribe` handler, replay
  logic, and cursor lock machinery
- **SHOULD** make tests deterministic without `time.Sleep` or `Eventually`
  polling

## Non-goals

- Multi-node deployment today (deferred; design preserves the seam)
- Real encryption codec implementation (codec interface ships; first
  algorithm + key management is a separate workstream)
- Data migration from existing `events` table (clean cutover, pre-launch)
- Replacement of unrelated subsystems (ABAC, world model, plugin loader)

---

## Architecture overview

```text
                          ┌──────────────────────────────────────────┐
                          │   Embedded NATS server (single-node)     │
                          │   ─ JetStream stream: EVENTS             │
                          │   ─ Subjects: events.>                   │
                          │   ─ Storage: file (xdg.DataDir/jetstream)│
                          │   ─ Retention: LimitsPolicy, MaxAge=720h │
                          └─────────────┬────────────────────────────┘
                                        │
        ┌───────────────────┬───────────┼─────────────────┬───────────────┐
        │                   │           │                 │               │
        ▼                   ▼           ▼                 ▼               ▼
┌───────────────┐  ┌──────────────┐ ┌──────────┐ ┌──────────────┐ ┌──────────────┐
│  Plugin emit  │  │ Host emit    │ │ Session  │ │ Host audit   │ │ Plugin audit │
│  (EventSink)  │  │ (EventSink)  │ │ consumer │ │ projection   │ │ projection   │
│               │  │              │ │ (durable │ │ (durable     │ │ (durable     │
│ → host        │  │ → bus.Publish│ │  pull)   │ │  pull)       │ │  pull)       │
│   validates   │  │              │ │          │ │              │ │              │
│ → bus.Publish │  │              │ │          │ │ → events_    │ │ → plugin     │
│               │  │              │ │          │ │   audit (PG) │ │   schema     │
└───────────────┘  └──────────────┘ └────┬─────┘ └──────────────┘ └──────────────┘
                                          │
                                          ▼
                                    ┌─────────┐
                                    │ gRPC    │
                                    │ Send +  │
                                    │ Ack     │
                                    └─────────┘
```

**Key claims:**

- JetStream owns ordering: `uint64` server-assigned sequence per stream;
  ULIDs become identity (and `Nats-Msg-Id` dedup keys), not ordering keys.
- JetStream owns consumer state: each session has a durable consumer named
  `session_<sessionID>`; the host no longer tracks cursors.
- PostgreSQL becomes a *projection target*. The host's `events_audit` table
  is populated by an audit-projection consumer; plugin-owned subjects flow
  to plugin-owned audit schemas via per-plugin consumers.
- The `EventStore.Append` interface is replaced by `EventBus.Publish`. The
  `EventStore.SubscribeSession` shape is replaced by `EventBus.OpenSession`
  returning a `SessionStream` with `SetFilters(filters)`.
- `EventWriter`, `cursor_lock.go`, `replay.go`, the legacy
  `EventStore.Subscribe`, and the `event_cursors jsonb` column are all
  **deleted** (clean break).

---

## Detailed design

### 1. Event model

#### 1a. Event identity

```go
// Subject is a typed JetStream subject. Constructed via NewSubject which
// validates against the documented token rules (Section 1c). Prevents
// accidentally passing an unstructured string (e.g., a player ID) where a
// subject is expected.
type Subject string

// Type is a typed plugin-declared event type identifier. Constructed via
// NewType which validates against the manifest's declared types.
type Type string

type Event struct {
    ID        ulid.ULID  // identity, dedup key, stable across rebuilds
    Subject   Subject    // typed JS subject
    Type      Type       // plugin-declared, opaque to host
    Timestamp time.Time  // host-stamped at publish
    Actor     Actor      // host-stamped from session/plugin context
    Payload   []byte     // proto bytes (post-codec encoding)
}
```

| Property | Source | Purpose |
| --- | --- | --- |
| `ID` (ULID) | Host stamps via `core.NewULID` | Identity, `Nats-Msg-Id` dedup, stable across JS rebuilds |
| `Subject` | Plugin emits, host validates | JS routing primitive |
| `Type` | Plugin emits | Opaque to host; subscribers MAY filter on it |
| `Timestamp` | Host stamps at publish | Audit + display ordering |
| `Actor` | Host stamps from context | Authorship; never plugin-spoofable |
| `Payload` | Plugin marshals proto, host applies codec | Domain content |

`Event.ID` (ULID) **MUST** stay as the cross-system identity. JetStream
sequence (`uint64`) is internal to the bus and **MUST NOT** cross any
public API boundary. ULIDs survive JetStream rebuilds; sequences do not.

Typed `Subject` and `Type` add zero runtime cost (compile-time only) and
catch a class of accidental field-swap and free-string bugs that bit the
old `Event.Stream string` model.

#### 1b. Event Type and Payload — plugin-declared, opaque to host

The 19 `EventType` constants in `internal/core/event.go:39-70` **MUST** be
deleted. Each plugin declares its own type strings as constants in its own
package. The host treats `Type` as `string`, validated only against the
plugin's manifest declaration (existing `EventSink` mechanism, PR #207).

**The same boundary rule applies to payload schemas.** The plugin-domain
payload structs at `internal/core/event.go:74-143`
(`LocationStatePayload`, `ExitUpdatePayload`, `PagePayload`,
`WhisperPayload`, `WhisperNoticePayload`, `OOCPayload`, `PemitPayload`)
**MUST** also move out of `internal/core/`. Each lives in its owning
plugin's package, defined as proto messages in that plugin's proto namespace
(e.g., `api/proto/holomush/scenes/v1/`, `api/proto/holomush/dm/v1/`,
`api/proto/holomush/world/v1/` — final layout decided in F5 per plugin).

The host's `Event.Payload []byte` is opaque from end to end:

- Publishers (plugins) marshal their own proto to bytes
- Host applies codec, persists bytes to JetStream and audit
- Subscribers receive bytes, apply codec.Decode, then unmarshal to their
  own proto type
- The host **MUST NOT** import any plugin's payload type and **MUST NOT**
  attempt to interpret payload bytes beyond passing them through

This closes the boundary violation completely — neither the type
identifier nor the payload schema for plugin-owned events lives in
`internal/core/` after the cutover.

#### 1c. Subject naming — hierarchical, game-namespaced, plugin-namespaced

```text
events.<game-id>.<domain>.<entity-id>[.<facet>...]
```

The leading `events.<game-id>.` prefix is mandatory from day one. Single-game
deployments use a literal segment (config: `event_bus.game_id`). The cost
today is one extra subject token and one config knob; the cost of *not*
having it 18 months from now if multi-tenancy ever lands is a global
subject rename across every plugin, every audit row, and every consumer.

Concrete subjects (single-game default `<game-id> = main`):

| Subject | Owner | Examples |
| --- | --- | --- |
| `events.<game>.location.<ULID>` | host | location-scoped events |
| `events.<game>.character.<ULID>` | host | personal stream (DMs, command responses) |
| `events.<game>.session.<ULID>.lifecycle` | host | session lifecycle (PR #233) |
| `events.<game>.notifications.character.<ULID>` | host | cross-scene notifications |
| `events.<game>.scene.<ULID>.ic` | core-scenes plugin | in-character (poses, says) |
| `events.<game>.scene.<ULID>.ooc` | core-scenes plugin | out-of-character chat |
| `events.<game>.scene.<ULID>.lifecycle` | core-scenes plugin | created/paused/ended |
| `events.<game>.channel.<ULID>` | core-channels plugin (future) | channels |

Subjects use dot-delimited tokens; `*` matches one token, `>` matches the
remainder and **MUST** be the last token. Subject token depth **SHOULD**
stay below 16 to keep matching cost low.

Stream subject filter remains `events.>` so the JS topology is
game-agnostic; per-game routing happens via `FilterSubjects` on consumers
(`events.<game>.scene.>` for game-scoped scenes consumer, etc.).

Manifest declares both publish-allowed and audit-subscribed subjects:

```yaml
emits:
  - "events.*.scene.*.ic"           # * matches game id
  - "events.*.scene.*.ooc"
  - "events.*.scene.*.lifecycle"
audit:
  - subjects: ["events.*.scene.>"]
    schema:   plugin_core_scenes
    table:    scene_log
```

The wildcard at the game-id position lets a plugin serve multiple
games on a single server when multi-tenancy lands. For single-game
deployments today, the wildcard matches the literal `main`.

#### 1d. Wire format — proto + version header

Replace JSON-in-bytea with a proto `Event` message. Forces explicit schema
discipline; binary; backward-compatible field additions are free. 64 KiB
payload cap stays.

**Proto schema layout:**

- Host envelope (the `Event` message itself, plus `Actor`, `EventBus`
  service, `PluginAuditService` if not elsewhere): `api/proto/holomush/eventbus/v1/`
- Plugin payload schemas: each plugin's existing proto namespace
  (`api/proto/holomush/scenes/v1/`, `api/proto/holomush/dm/v1/`, etc.)

The host package depends on the envelope proto; **never** on plugin
payload protos (which are bytes from the host's perspective).

Headers on every published message:

| Header | Purpose | Required |
| --- | --- | --- |
| `Nats-Msg-Id` | ULID; dedup within window | yes |
| `App-Schema-Version` | proto schema major version | yes |
| `App-Event-Type` | plugin's `Type` string for filter-without-decode | yes |
| `App-Codec` | codec name (`identity`, `aes-gcm-v1`, ...) | yes — never empty |
| `traceparent` / `tracestate` | W3C trace context | when caller has active span |

---

### 2. Stream topology

A single JetStream stream named `EVENTS` with subject filter `events.>`
holds all events from all domains.

```text
StreamConfig{
    Name:        "EVENTS",
    Subjects:    []string{"events.>"},
    Retention:   LimitsPolicy,
    Storage:     FileStorage,
    Replicas:    1,                     // single-node; multi-node = planned cutover, not flag flip (§7, §10)
    MaxAge:      720h,                  // 30 days
    Duplicates:  30 * time.Minute,      // widened from default 5m to absorb host-restart retry window
    AllowDirect: true,
}
```

#### Why one stream

- **Global monotonic sequence** for free (replaces Invariant I-14
  enforcement)
- **Multi-subject filters per consumer compose freely** —
  `[events.character.<me>, events.scene.<focus>.ic, events.scene.<focus>.ooc]`
  on one durable consumer
- **One Raft group, one retention policy, one backup target**
- **No stream lifecycle drama** — plugins never create or destroy streams

#### Why not per-domain or per-entity

Per-domain streams lose cross-stream ordering and force consumer-per-stream
complexity. Per-entity streams cause stream proliferation in cluster mode.

#### Retention rationale

`LimitsPolicy` is the only retention type that supports independent
fan-out readers. `MaxAge` is preferred over `MaxBytes` to avoid the
memory-reservation gotcha
([nats-server#6993](https://github.com/nats-io/nats-server/issues/6993)).
30-day retention is a starting point — long enough for typical session
resume, short enough that JS storage stays manageable.

#### PostgreSQL audit as forever-archive

The audit projection (Section 6) writes every event to `events_audit`
(host) or to plugin-owned audit schemas. JS holds the recent past; PG
holds everything. `QueryHistory` (Section 5) abstracts the boundary. This
gives R=2-equivalent durability without R=2 cluster overhead.

#### Stale-cursor edge case

Session consumer `InactiveThreshold` **MUST** be less than stream
`MaxAge`. Default: `InactiveThreshold = 24h`, `MaxAge = 720h`. Sessions
disconnected longer than 24h re-establish via `QueryHistory` (which
transparently uses PG audit) rather than resume.

---

### 3. Publish path

#### Plugin-side contract (changed but minimal)

```go
// pkg/plugin/event_sink.go
eventSink.Emit(ctx, EmitIntent{
    Subject: "events.main.scene.01JABC...XYZ.ic",
    Type:    "scene.pose",
    Payload: marshaledProto,
})
```

`EmitIntent.Stream` is renamed to `EmitIntent.Subject` and **MUST** be a
JS subject string. Plugins **MUST** be updated as part of the cutover
(F5 in Section 10).

#### Host `EmitEvent` handler

1. Validate `Subject` against plugin manifest's declared `emits` patterns
2. Map nothing — subject is already in JS form
3. Stamp host-owned fields:
   - `ID = idgen.NewULID()`
   - `Timestamp = time.Now().UTC()`
   - `Actor.{Kind,ID}` from session or plugin context
4. Marshal `Event{...}` to proto bytes
5. `payload = codec.Encode(ctx, subject, protoBytes)` (codec resolved by
   subject policy; identity by default — see Section 6 / 9)
6. `js.PublishMsg(ctx, &nats.Msg{Subject, Data: payload, Header: ...})`
7. Wait for `PublishAck` — synchronous, returns seq + stream confirmation
8. Return ack to plugin

#### `EventWriter` deletion

`EventWriter` exists today to serialize appends globally for ULID
monotonicity. JetStream assigns its own monotonic sequence per stream
regardless of publish concurrency, so `EventWriter` **MUST** be deleted.
`core.NewULID`'s in-process mutex still produces monotonic ULIDs — that
operation is not a serialization bottleneck.

#### Idempotent publish — bounded window

`Nats-Msg-Id` = `event.ID.String()`. Within the dedup window (default 5
minutes, set via stream `Duplicates` config), JS silently drops duplicate
publishes with the same ID and returns the original ack.

**Honest delivery semantics:** at-least-once, with effective exactly-once
*within the dedup window*. After the window expires, a publish with the
same `Nats-Msg-Id` is treated as a fresh message and creates a duplicate
in the log. There is no "exactly-once" without bound.

**Required publisher contract** (enforced in `EventSink`):

- Plugin retry attempts for the same `EmitIntent` **MUST** complete within
  the dedup window. The host's publish RPC carries a deadline derived from
  `dupe_window − safety_margin` (default `5m − 30s = 4m30s`). After
  deadline, the host returns a `PublishExpired` error and the plugin
  **MUST NOT** retry the original intent — it must construct a new event
  (new ULID).
- Subscribers **MUST** dedup by ULID at the application boundary. Web and
  telnet adapters maintain a per-connection seen-ULID set with TTL
  ≥ `MaxAckPending × ack_wait` to absorb at-least-once redelivery from JS
  consumers.
- The dedup window is widened (`dupe_window: 30m`) when running embedded
  NATS so a worst-case host crash + restart + retry cycle stays inside
  the window.

**Host crash mid-publish scenario.** If the host crashes after JS has
acked the publish (filestore fsync complete) but before `EmitEvent`
returns to the plugin, the plugin sees a transport error on restart and
retries with the same ULID. JS dedup absorbs the duplicate within the
window. If the host is down longer than the window — i.e., the plugin
restart happened > `dupe_window` after the crash — the retry creates a
duplicate. Subscribers' dedup catches it; the log carries two rows for
the same ULID.

#### Failure modes

| Scenario | Behavior |
| --- | --- |
| JS unreachable | `PublishMsg` returns error; `EmitEvent` propagates to plugin; retry safe within dedup window |
| `PublishAck` timeout | Same as above |
| Manifest validation fails | Reject pre-publish; audit log; plugin error |
| Payload exceeds 64 KiB | Reject pre-publish; same cap as today |
| Plugin retry exceeds dedup window | Host returns `PublishExpired`; plugin must mint new ULID |
| Host crash > dedup window before retry completes | Duplicate in log; subscriber dedup absorbs (cost: one duplicate audit row) |

No silent fallbacks. With embedded NATS, JS down = process down. With
clustered NATS, quorum loss means publishes fail until quorum returns.

---

### 4. Subscribe path

#### Interface — split into three single-responsibility interfaces

```go
// Publisher writes events. Used by EventSink (which used to call
// EventStore.Append).
type Publisher interface {
    Publish(ctx context.Context, event Event) error
}

// Subscriber opens long-lived session streams.
// Used by gRPC Subscribe handler.
type Subscriber interface {
    OpenSession(ctx context.Context, sessionID string, filters []Subject) (SessionStream, error)
}

// HistoryReader serves paginated history reads. Used by QueryHistory
// gRPC handler and by plugin-side audit projection backfills.
type HistoryReader interface {
    QueryHistory(ctx context.Context, q HistoryQuery) (HistoryStream, error)
}

// EventBus is the concrete implementation that satisfies all three.
// Tests and consumers depend on the narrow interface they actually need.
type EventBus interface {
    Publisher
    Subscriber
    HistoryReader
}

// Delivery is a typed handle for a single message in flight from a
// SessionStream. Replaces the prior (Event, AckFunc, error) tuple shape
// — typed handles are easier to mock, log, extend (Nack, InProgress).
type Delivery interface {
    Event() Event
    Ack() error
    // Nack signals the message should be redelivered without affecting
    // ack-pending state semantics. Used for transient handler errors.
    Nack() error
    // InProgress extends the ack-wait timer for handlers expecting to
    // exceed the default. Use sparingly.
    InProgress() error
}

type SessionStream interface {
    // Next blocks until the next delivery is available or ctx is done.
    Next(ctx context.Context) (Delivery, error)
    // SetFilters atomically updates the underlying durable consumer's
    // FilterSubjects. Cursor is preserved by JS UpdateConsumer.
    SetFilters(ctx context.Context, filters []Subject) error
    Close() error
}
```

`AddStream` / `RemoveStream` / `Notifications` / `Errors` (current
`core.Subscription`) **MUST** be deleted. `AckFunc` closure shape is
not used; `Delivery` is the canonical handle.

**Composition at call sites:** the gRPC Subscribe handler depends on
`Subscriber`; the gRPC QueryHistory handler depends on `HistoryReader`;
the EventSink depends on `Publisher`. Each can be mocked independently.
The concrete `eventBus` struct satisfies all three.

#### `OpenSession` semantics

Creates or rebinds a durable consumer named `session_<sessionID>` on
stream `EVENTS`:

```text
ConsumerConfig{
    Durable:           "session_<sessionID>",
    FilterSubjects:    filters,
    AckPolicy:         AckExplicitPolicy,
    MaxAckPending:     256,
    InactiveThreshold: 24 * time.Hour,
    DeliverPolicy:     DeliverNew,    // first creation only
}
```

Subsequent rebinds resume from last ack automatically (durable consumer
property — no host-side cursor tracking).

#### `SetFilters` semantics

```text
js.UpdateConsumer(EVENTS, ConsumerConfig{
    Durable:        "session_<sessionID>",
    FilterSubjects: newFilters,
    ...
})
```

JS preserves cursor across the update. Focus changes are atomic with
respect to delivery. (`FilterSubjects` plural is supported in NATS server
≥ 2.10.)

#### gRPC Subscribe handler (new)

```text
1. ValidateSessionOwnership(ctx, playerID, sessionID, token)         // PR #225
2. focusPlan := focusCoordinator.RestorePlan(ctx, sessionID)          // existing
3. filters := focusPlan.Subjects()
4. stream, err := bus.OpenSession(ctx, sessionID, filters)
5. defer stream.Close()
6. for {
       delivery, err := stream.Next(ctx)
       if err != nil { return err }
       if err := grpcStream.Send(toProto(delivery.Event())); err != nil {
           // Send failure: don't ack. JS will redeliver on next bind.
           // Caller's existing dedup on ULID prevents duplicate render.
           return err
       }
       if err := delivery.Ack(); err != nil {
           logger.Warn("ack failed; will redeliver", "error", err)
       }
   }
```

#### Backpressure

`MaxAckPending` per session caps inflight unacked messages. Slow `Send`
→ ack stalls → JS pauses delivery → no buffer blowup, no dropped events.
Native flow control. No hand-rolled queue.

#### Session GC

`InactiveThreshold = 24h` deletes idle session consumers (and their
cursor state) automatically. Explicit cleanup on session quit: an
internal subscriber to `events.session.*.lifecycle` (PR #233) calls
`js.DeleteConsumer("session_<id>")` immediately. Best-effort; threshold
catches the rest.

#### Per-event ack

`AckExplicitPolicy` + ack-after-`Send`. Crash mid-`Send` → no ack →
redeliver → client dedups via ULID. Successful `Send` → ack → JS advances
cursor. The Send-vs-Ack race that powered `cursor_lock.go` does not
exist.

#### What gets deleted

- `internal/grpc/replay.go`
- `internal/grpc/cursor_lock.go`
- `core.Subscription` interface and all implementations
- `core.EventStore.SubscribeSession` and the legacy `Subscribe(stream)`
- `EventCursors map[string]ulid.ULID` from `session.Info`
- `event_cursors jsonb` column from `sessions` table
- All `time.Sleep(1ms)` integration test crutches
- `EventWriter`

---

### 5. Replay and history

#### Interface

```go
type HistoryQuery struct {
    Subject   Subject     // exact subject OR pattern with * / >
    After     ulid.ULID   // exclusive lower bound; zero = from start
    Before    ulid.ULID   // exclusive upper bound; zero = unbounded
    NotBefore time.Time   // optional time bound
    NotAfter  time.Time   // optional time bound
    Direction Direction   // Forward (older→newer) or Backward
    PageSize  int         // host caps at 200; default 50
    // Auth flows via context.Context (auth.WithSession), not via the query.
    // Putting it in two places invites the question "what if they disagree."
}

type HistoryStream interface {
    // Next returns the next event. io.EOF when exhausted.
    Next(ctx context.Context) (Event, error)
    Close() error
}
```

Server-streaming gRPC. Caller iterates `Next()` until `io.EOF`. For
next-page resume, the **caller records the ULID of the last `Event`
returned** and passes it as `After` on the next `QueryHistory` call. The
stream itself does not carry pagination state — there's no
`stream.Cursor()` method to invoke at the wrong time and silently get a
zero-value ULID. ULID is the cursor everywhere.

Auth uses `context.Context`; the gRPC handler injects session via
`auth.WithSession(ctx, sess)` before calling `QueryHistory`. The plugin
RPC carries the same context across the boundary.

#### JS / PG fallback

```text
1. Resolve subject ownership:
   - If subject prefix matches a plugin's audit declaration → delegate to plugin
   - Else: host serves from PG events_audit + JS for the recent tail

2. Tier selection:
   safetyMargin := 1h
   jsRetentionEdge := now() - JS_MaxAge + safetyMargin
   - if cursor implies start time < jsRetentionEdge: begin in PG
   - else: begin in JS
   - if PG path crosses jsRetentionEdge mid-page: continue from JS

3. Stream:
   - PG: SELECT ... FROM events_audit WHERE subject=$1 AND id > $cursor
         ORDER BY id [ASC|DESC] LIMIT $pageSize
   - JS: ephemeral consumer with FilterSubject + DeliverByStartSequence

4. Apply codec.Decode + proto.Unmarshal per event.
```

The "always read JS for the recent slice" rule means audit projection lag
is invisible to user-facing reads.

#### Plugin-owned audit routing

Manifest declaration (Section 6). When `bus.QueryHistory(q)` sees
`q.Subject` matches a plugin-owned prefix, it RPCs the plugin's
`QueryHistory` handler (`PluginAuditService.QueryHistory`) over the
existing PluginHostService channel. Plugin:

1. Performs domain authz (e.g., `store.GetSceneLog` membership check)
2. Reads from its own audit schema
3. Streams events back to host
4. Host applies `codec.Decode`, streams to original caller

If plugin RPC fails or times out: surface error to caller. **No silent
host fallback** — that would leak the privacy boundary.

#### Cursor model

ULID always. Stable across:

- JetStream rebuilds (sequences would change; ULIDs do not)
- Audit projection rebuilds
- Plugin schema migrations (PK is ULID)

#### Auth model

One check at entry. Subject → check map:

| Subject pattern | Authz check | Where |
| --- | --- | --- |
| `events.location.<id>` | session present at location id OR location is public | host |
| `events.character.<own-id>` | session belongs to character | host |
| `events.scene.<id>.>` | character is scene member | scenes plugin |
| `events.channel.<id>` | character is channel member OR public | channel plugin |
| `events.notifications.character.<own-id>` | session belongs to character | host |

ABAC is **not per-event** — a subscription authorized to start streams
freely until filters change.

#### Limits

- `PageSize` capped at 200 host-side
- gRPC server-streaming with deadline
- Bounded JS/PG per-batch buffers
- Plugin RPC timeout (default 5s per page); failure propagated

---

### 6. PostgreSQL's new role

#### Deletions (cutover migration)

```sql
DROP TABLE events;
ALTER TABLE sessions DROP COLUMN event_cursors;
```

The `events_immutable` meta-test (PR #227) **MUST** be repurposed against
`events_audit`.

#### Additions

```sql
CREATE TABLE events_audit (
    id           BYTEA       PRIMARY KEY,        -- ULID (16 bytes)
    subject      TEXT        NOT NULL,
    type         TEXT        NOT NULL,
    timestamp    TIMESTAMPTZ NOT NULL,
    actor_kind   TEXT        NOT NULL,
    actor_id     BYTEA,
    payload      BYTEA       NOT NULL,           -- codec.Encode output
    schema_ver   SMALLINT    NOT NULL,
    codec        TEXT        NOT NULL,           -- 'identity' | 'aes-gcm-v1' | ...
    js_seq       BIGINT      NOT NULL,
    inserted_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX events_audit_subject_id  ON events_audit (subject, id);
CREATE INDEX events_audit_subject_ts  ON events_audit (subject, timestamp);
CREATE INDEX events_audit_subject_pat ON events_audit (subject text_pattern_ops);
```

The `(subject, id)` index is the workhorse for `QueryHistory` PG fallback.

`codec` is **NOT NULL** — every row stores its codec name explicitly.
NULL is not "identity"; NULL means a bug. Validation enforced in Go
(only writer); no PG `CHECK` constraint to avoid migration churn when
adding codecs.

#### Unchanged

`players`, `characters`, `locations`, `exits`, `objects`, `sessions`
(minus `event_cursors`), ABAC tables, `command_history`,
`focus_memberships`, `presenting_focus`, all plugin-owned schemas
(additive only).

#### Audit projection worker (host)

```text
Subsystem:        SubsystemAuditProjection
DependsOn:        [Database, EventBus]
JS Consumer:      "host_audit_projection"
Stream:           EVENTS
FilterSubjects:   ["events.>"] minus plugin-owned subjects
AckPolicy:        AckExplicit
MaxAckPending:    64
DeliverPolicy:    DeliverAll on first creation; auto-resume thereafter
```

Per delivered message:

```sql
INSERT INTO events_audit(...)
VALUES (...)
ON CONFLICT (id) DO NOTHING;
```

Idempotent (PK on ULID). At-least-once + idempotent insert =
effectively exactly-once. PG outage = worker stalls = JS retains backlog
= projection catches up on recovery. Backpressure flows backward.

#### Plugin-owned audit projection

Per-plugin durable consumer registered at plugin start:

```text
JS Consumer:      "plugin_audit_<plugin_name>"
FilterSubjects:   from manifest
```

For each delivered message, host calls plugin via
`PluginAuditService.AuditEvent` RPC. Plugin INSERTs into its own schema,
acks; host forwards ack to JS.

Plugin stops → consumer worker exits but consumer stays durable in JS.
Plugin restart → resumes from last ack. No orphan state.

**Ciphertext at rest pattern**: plugin receives ciphertext, stores
ciphertext. Plaintext access for derivations (e.g., pose order) goes
through `host.codec.Decode` helper — codec stays host-owned.

#### Subject ownership map — longest-prefix-wins, token-aligned

At startup, host builds a subject-prefix → owner map from all manifests.

**Matching rule:** longest-prefix-wins, where "prefix" is *token-aligned*
(matches whole dot-delimited segments, not partial tokens).

| Manifest declarations | Subject `events.main.scene.X.lifecycle` resolves to |
| --- | --- |
| only `events.main.scene.>` | scenes plugin |
| `events.main.scene.>` and `events.main.scene.*.lifecycle` | scenes plugin (specific lifecycle plugin) — longer prefix wins |
| `events.main.scene.>` and `events.main.>` | scenes plugin (longer prefix) |
| `events.main.scene.lifecycle` (literal) and `events.main.scene.>` | scenes plugin (specific literal wins over wildcard at same depth) |
| Two manifests both declaring `events.main.scene.>` | **startup error** — exact-prefix conflict |

Conflicts at startup (two manifests with the same exact-prefix declaration)
are a fatal error. Manifest validation refuses to load.

A property test in §8 (invariants) asserts resolution determinism: for
any set of declared prefixes and any concrete subject, the resolved owner
is unique and deterministic.

#### Audit retention policy

Default: forever. ~600 MB/year at 200 users × 10 evt/min envelope. Cheap.
Plugins set their own retention on their audit tables (GDPR, scene TTLs,
etc.). Plugin's lifecycle, plugin's policy.

#### Operational surface

- Prometheus metric `audit_projection_lag_seconds`. Alert > 5s.
- Per-plugin lag metrics, same threshold.
- Reconciliation test in CI: seed 1000 events, drain projection, assert
  count.
- Backfill tool (`bin/holomush audit-backfill`): standalone command,
  ephemeral consumer at seq 1, idempotent re-projection.
- Drift detector (CI nightly): sample audit rows, fetch JS messages,
  byte-compare.

---

### 7. Bootstrap and lifecycle

#### `SubsystemEventBus`

```go
type Subsystem struct { ... }
func (s *Subsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemEventBus }
func (s *Subsystem) DependsOn() []lifecycle.SubsystemID { return nil }
```

**Start:**

```go
opts := &server.Options{
    ServerName: "holomush-embedded",
    JetStream:  true,
    StoreDir:   s.cfg.StoreDir,        // xdg.DataDir() + "/jetstream"
    DontListen: true,
    NoSigs:     true,
    LogtimeUTC: true,
    HTTPPort:   s.cfg.MonitorPort,     // 0 = disabled
}
s.server = server.NewServer(opts)
go s.server.Start()
if !s.server.ReadyForConnections(10 * time.Second) { return ... }

s.conn = nats.Connect("",
    nats.InProcessServer(s.server),
    nats.Name("holomush-host"),
)
s.js = jetstream.New(s.conn)

s.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
    Name: "EVENTS", Subjects: []string{"events.>"},
    Retention: LimitsPolicy, Storage: FileStorage,
    Replicas: 1, MaxAge: cfg.StreamMaxAge, Duplicates: cfg.DupeWindow,
    AllowDirect: true,
})
```

**Stop** (reverse):

```go
s.conn.Drain()
s.server.Shutdown()
s.server.WaitForShutdown()
```

`NoSigs: true` is mandatory — host owns signal handling. `DontListen:
true` eliminates port binding. `WaitForShutdown` is required for
filestore consistency.

#### `SubsystemAuditProjection`

```go
DependsOn: [Database, EventBus]
```

Start: register `host_audit_projection` consumer with `FilterSubjects =
["events.>"] minus plugin-owned prefixes`. Spawn worker via
`consumer.Consume(handler, jetstream.PullMaxMessages(64))`. Stop: cancel
context, drain inflight, unbind. Consumer stays durable in JS.

Health: report based on lag — `Warm` < 1s, `Degraded` < 30s, `Stale`
> 30s.

#### Subsystem dependency graph

```text
Database  → TLS  → ABAC  → Auth  → World
                                       ↓
                                  EventBus  →  AuditProjection
                                       ↓                ↓
                                   Plugins  →  Bootstrap  →  GRPC
```

Stop order is reverse of start. Plugins stop before AuditProjection (no
new emissions while audit drains). AuditProjection stops before EventBus
(needs JS to drain). AuditProjection stops before Database (needs PG to
drain).

#### Per-plugin audit consumers

Plugin subsystem `Start` registers its audit consumer if manifest
declares one. Plugin `Stop` tears down the worker; consumer stays durable.

#### Storage path

```text
xdg.DataDir()/jetstream/$G/streams/EVENTS/...
xdg.DataDir()/jetstream/$G/streams/EVENTS/obs/host_audit_projection/
xdg.DataDir()/jetstream/$G/streams/EVENTS/obs/plugin_audit_core_scenes/
xdg.DataDir()/jetstream/$G/streams/EVENTS/obs/session_<sessionID>/
```

`StoreDir` is lock-exclusive; only one process per directory.

#### Testing helpers

```go
// internal/eventbus/eventbustest/embedded.go
func New(t *testing.T) *Embedded { ... }   // MemoryStorage, t.TempDir, t.Cleanup
```

`MemoryStorage` keeps tests in single-digit milliseconds. `DontListen +
InProcessServer` means zero port-conflict risk in `t.Parallel()`.

#### Observability

- W3C `traceparent` / `tracestate` headers on every published message;
  OTEL spans wrap publish + deliver + audit-project + plugin-audit-deliver
- Prometheus via in-process `prometheus-nats-exporter` for NATS-server
  internals (stream messages, consumer pending, slow consumers)
- OTEL metrics via existing meter for application-level event-bus metrics
  (publish latency, audit lag, codec timing, per-subject rates)
- Both feed the existing `obsServer.MustRegister` pattern

#### Config surface

```yaml
event_bus:
  mode: embedded                    # "embedded" | "cluster"
  game_id: "main"                   # mandatory; first segment after "events."
  store_dir: ""                     # blank = xdg.DataDir()/jetstream
  stream_max_age: 720h
  dupe_window: 30m
  monitor_port: 0
  prometheus_exporter: true

  # cluster-mode only (future)
  cluster_url: ""
  credentials_file: ""
  tls:
    ca_file: ""
    cert_file: ""
    key_file: ""
    server_name: ""
```

#### NATS version pinning and upgrade discipline

NATS is now a third-party runtime dependency on the critical path. Pin to
the **latest GA minor** of `nats-server` (currently 2.12.x — pick the
latest patch verified clean of the 2.12.5-class regression). Embedded
import in `go.mod` matches the running server version.

**Renovate config trails GA bumps** to avoid landing the day-one bad
patch. Custom `renovate.json` rule for `github.com/nats-io/nats-server/v2`
and `github.com/nats-io/nats.go`:

```jsonc
{
  "packageRules": [
    {
      "matchPackageNames": [
        "github.com/nats-io/nats-server/v2",
        "github.com/nats-io/nats.go"
      ],
      "minimumReleaseAge": "14 days",   // wait 2 weeks after GA before opening PR
      "groupName": "nats",              // group together so we test/upgrade as a unit
      "schedule": ["after 9am on Monday"]
    }
  ]
}
```

Upgrade discipline:

- Renovate opens PR for grouped NATS bump 14+ days post-GA
- PR runs `task pr-prep` (lint, tests, integration, E2E)
- The chaos+soak nightly (§8) runs against the PR branch before merge
- Merge only if all gates green
- Rollback plan: if a bump regresses production behavior, revert the
  bump commit; the JS filestore format is forward-compatible within a
  major, so downgrade-restart is safe

`mode` is a config seam, **not a cutover plan**. Switching `embedded` →
`cluster` is a deliberate multi-step migration project (see §10). What the
config seam buys you: the application-side code (every consumer of
`EventBus`) stays unchanged across the cutover. What it does **not**
buy: any of the operational work below. Be honest with planning that the
cluster cutover, when it lands, is in the same scope class as a database
migration:

- Cluster sizing decision (3 / 5 nodes, R=3 quorum)
- External NATS deployment, network policy, DNS / discovery
- mTLS material lifecycle (issue, rotate, revoke)
- NKey or JWT-based per-host credentials, including per-plugin in cluster
  mode (defense-in-depth for subject permissions)
- Monitoring stack adapted (per-node metrics, leader-election alerts,
  Raft health)
- Backup / restore strategy (`nats account backup` is offline; `Mirror`
  streams are live but require coordination)
- **Filestore migration** of the existing single-node JetStream data —
  cannot be a raw `cp -r`. Two options: (a) restore from PG audit
  projection (clean but multi-hour at scale), (b) `Mirror` stream from
  embedded to new cluster, wait for lag → 0, flip publishers, deprecate
  source. Choose at planning time based on downtime tolerance.
- **Plugin audit consumers must be re-registered** against the new
  cluster topology (durable consumer state in old filestore is lost
  unless restored).

Treat the cutover as a multi-week project. The seam is the EventBus
interface boundary; the migration is the work.

---

### 8. Testing strategy

#### Layers

| Layer | Bounds | Deps | Speed |
| --- | --- | --- | --- |
| Unit | Pure logic | None | < 10ms |
| Bus integration | EventBus contract | Embedded NATS, MemoryStorage | < 100ms |
| Audit integration | Host + plugin projections | Embedded NATS + PG testcontainer | < 500ms |
| E2E | Full server, multi-protocol | Embedded NATS + PG + clients | seconds |

#### Controllable test seams (no hidden waits)

Several behaviors of the new design are inherently asynchronous (audit
projection drain, JS redelivery, ack timing, dedup-window expiry). The
strategy is **never `time.Sleep`, but `Eventually` is permitted only on
synchronization metrics, not on data**. Specifically:

- **Test harness MUST inject all timing knobs** as configuration, not
  hard-coded literals:
  - JS consumer `AckWait` defaults to 30s in production; tests set
    100ms so redelivery semantics are observable in bounded time.
  - JS stream `Duplicates` defaults to 30m in production; tests set 1s
    so dedup-window expiry tests run fast.
  - Audit projection `MaxAckPending` and worker batch sizes pinned for
    reproducibility.
- **A clock source (`func() time.Time`) is injected into the bus** so
  time-bound assertions (tier crossover, dedup window, `InactiveThreshold`)
  use a controllable test clock — not wall clock.
- **Synchronization points are observable metrics, not timers.** Pattern:

  ```go
  embedded.AwaitAuditLag(t, 0)            // blocks until audit_projection_lag_seconds == 0
  embedded.AwaitAckedSeq(t, "session_X", 42)  // blocks until consumer ack-state crosses 42
  embedded.AwaitConsumerInfo(t, "session_X", func(i jetstream.ConsumerInfo) bool { ... })
  ```

  These read JS / projection state directly, not wall-clock-bounded
  polls. They use `Eventually` internally with a hard upper bound (5s)
  whose only purpose is to fail loudly on a deadlock, not to add
  observation latency.
- **`time.Sleep` is banned in `internal/eventbus/`,
  `test/integration/eventbus_e2e/`, and `internal/grpc/` test files** by
  custom golangci-lint rule.
- **`Eventually` on raw subscriber data channels is also banned** — must
  go through `AwaitAckedSeq` or equivalent metric.

This makes the previously-hidden waits explicit and bounded, and makes
the test suite genuinely fast (single-digit ms per test).

#### Boundary tests

Subjects (length, depth, invalid chars), payload sizes (0, max, max+1),
filter list sizes (0, 1, 64, overlapping subset), ULID cursors (zero,
max, non-existent), page sizes (1, 200 cap, 201, 0), `InactiveThreshold`
limits, time bound inversions, JS↔PG retention boundary edge.

#### Fuzz tests

```text
FuzzSubjectValidator
FuzzCodecRoundTrip          (per codec)
FuzzEventUnmarshal
FuzzHistoryQueryValidate
FuzzManifestAuditDecl
FuzzHeaderParse
```

CI runs each for 30s as part of `task pr-prep`. Corpus persisted in
`testdata/fuzz/`.

#### Negative tests

Every error path asserts a *specific* error code, not just "non-nil".
Categories: invalid subject, oversize payload, missing required header,
unknown codec, manifest emit not permitted, filter unauthorized, invalid
cursor, plugin RPC timeout, decryption failure, manifest validation
failure, store-dir lock contention.

#### Invariant tests (property-based, `pgregory.net/rapid`)

- I-14 cross-subject sequence ordering under concurrent publishers
- Idempotent publish via `Nats-Msg-Id` within window; new event after window
- Audit row count == JS message count after drain
- Plugin audit table contains only declared subjects
- **Filter monotonicity under `SetFilters`** (load-bearing — see §4):
  emit N events while concurrently calling `SetFilters` with arbitrary
  subset sequences. After the test:
  - Every event whose subject was in the filter set *at the time of its
    publish* MUST be delivered exactly once (modulo ULID dedup)
  - No event from a removed subject MUST be delivered after the
    corresponding `SetFilters` returns
  - Cursor (last acked seq) MUST be preserved across every filter update
- **Subject-ownership-routing under concurrent loads**: register N
  manifests with overlapping prefixes, assert resolution is
  deterministic and matches the documented longest-prefix-wins rule.

Plus fixed invariants checked in CI:

- Every audit row's `codec` resolves in registry (drift detector)
- Every published message has non-empty `App-Codec` (publish-path lint)
- Subject ownership map has unique prefixes (startup test)
- Codec-name constants ↔ registry sync (meta-test)

#### Race-class regression tests

| Old race | Regression test |
| --- | --- |
| Send→UpdateCursors gap (Finding 1) | Crash subscriber 100× between Next and Ack; assert no event lost / spurious dup |
| Subscribe-before-Publish race | Open subscription, immediately publish, assert delivered |
| ULID collision in same ms | Concurrent publishers in tight loop; assert monotonic seq |
| Reconnect cursor drift | Open/publish/close/publish/reopen; assert receives only new events |

#### Full E2E matrix (`test/integration/eventbus_e2e/`)

| Scenario | Assertion |
| --- | --- |
| Cross-tier query (half aged, half recent) | All events in correct order, transparent crossover |
| Plugin audit isolation | Host audit empty for plugin subjects; plugin schema has them |
| Reconnect resume | Last-ack continuation, no dup, no loss |
| Audit drift detector | Tampered row reported with id |
| Backfill rebuild | `bin/holomush audit-backfill` produces matching counts |
| Plugin process crash mid-deliver | Restart drains; PK ON CONFLICT prevents dups |
| Embedded JS storage corruption | Rebuild from PG audit; ULIDs stable |
| Multi-protocol fan-out | Telnet + web in same scene see same pose |

#### JS↔PG hot/cold tier crossover suite (dedicated)

The tier-selection algorithm in §5 (`safetyMargin = 1h`,
`jsRetentionEdge = now() - JS_MaxAge + safetyMargin`) is the highest-risk
new code in the design. A dedicated test suite at
`internal/eventbus/history/tier_test.go` covers it explicitly using the
injected clock (no wall clock):

| Scenario | Setup | Assertion |
| --- | --- | --- |
| Cursor strictly within JS retention | All events recent | Served entirely from JS, zero PG queries |
| Cursor strictly older than retention | All events aged | Served entirely from PG, zero JS calls |
| Cursor exactly at the boundary edge | `cursor.timestamp == jsRetentionEdge` | No double-delivery, no skip |
| Page boundary crosses the edge | Half page in PG, half in JS | Continuous order, no gap, no overlap |
| Clock skew between PG `inserted_at` and JS publish time | Inject 5s skew | Crossover absorbs skew via safetyMargin |
| Forward direction crossing boundary | `Direction: Forward` | Older events first, newer continue from JS |
| Backward direction crossing boundary | `Direction: Backward` | Newer events first, older continue from PG |
| Empty PG side, full JS side | Audit projection lagging | Only JS events delivered, no error |
| Empty JS side (aged-out subject), full PG side | Subject aged out of JS | Only PG events delivered, no error |
| Both sides empty | Subject never had events | Empty result, no error |
| Cursor for a non-existent subject pattern | | Empty result, no error |
| Plugin-owned subject crosses the edge | Routes to plugin RPC | Plugin handles tier transparently in its own schema (host doesn't query both) |

This suite **MUST** run with the injected clock fast-forwarded across
the boundary; never with wall-clock waits. It is the single test suite
that proves the most subtle new behavior of the design.

#### Chaos and soak (CI nightly)

- Inject latency / errors on PG INSERT, NATS publish, plugin RPC
- 1k events/sec for 5 minutes; assert no goroutine leak (`goleak`),
  memory growth < 50 MB, audit lag p99 ≤ 5s, full event count

#### Performance budget (humane, justified)

**Context for the budget.** HoloMUSH is a text-based MUSH for human
players reading prose. It is not twitch gaming, not algorithmic trading,
not a real-time multiplayer FPS. The product priorities — in order — are
**Privacy > Stability > Extensibility > Resilience > Performance**.
Performance only matters insofar as it keeps prose interactions feeling
responsive to humans.

Human perception baselines for text interaction:

| Latency | Feel |
| --- | --- |
| < 100ms | Instant |
| 100-300ms | Snappy |
| 300-500ms | Acceptable for chat |
| 500-1000ms | Noticeable lag, still tolerable |
| 1-2s | Frustrating |
| > 2s | Broken |

**Budgets** (CI gates in chaos+soak nightly; breach blocks merge):

| Surface | p50 target | p99 ceiling | Why this number |
| --- | --- | --- | --- |
| End-to-end publish → subscriber `Send` (live UX) | ≤ 50ms | ≤ 200ms | The user-facing one. "Snappy" → "acceptable for chat." Anyone seeing 200ms+ on poses notices. |
| Plugin `AuditEvent` RPC (background) | ≤ 5ms | ≤ 50ms | Background; doesn't affect live UX. The 50ms ceiling catches plugin pathologies (sync I/O in handler) without forcing micro-optimization. |
| Plugin `QueryHistory` per-page RPC | ≤ 50ms | ≤ 250ms | User-perceived as "loading more scrollback." Half a click-budget; UI applies the rest. |
| Audit projection lag | ≤ 500ms | ≤ 5s | Doesn't affect live; matters for "did my pose make it into the log yet." 5s ceiling catches projection stalls before users notice. |
| Embedded NATS publish ack | ≤ 5ms | ≤ 25ms | Local in-process; budget is generous to absorb fsync hiccups under load. |

These are **breaches block merge** — not aspirations. A plugin or change
that violates the ceiling is a bug, not an architectural concern.

**Explicit non-budgets** — what we deliberately do *not* optimize for:

- Sub-millisecond anything (we will gladly trade 5ms for clearer code
  or better isolation)
- Throughput beyond 1k events/sec sustained (our scale envelope; cluster
  cutover when scale demands)
- Memory footprint optimization beyond avoiding leaks (50MB headroom is
  fine; cleverness here is wasted effort)
- CPU efficiency beyond avoiding pathologies (3% CPU cost of plugin RPC
  loop is acceptable; "saving" it via tighter coupling would be wrong
  per priority order)

**When in doubt** between two design choices, the priority order
decides. Plugin isolation that costs 5ms p99 wins over tighter coupling
that saves it. Synchronous codec calls that are clearer to audit win
over async codec calls that shave latency. Stability and clarity are
features; speed past the budget is not.

#### Coverage gates

- 80% per package (project rule)
- 90% for `internal/eventbus/`
- 95% for `internal/eventbus/codec/`
- 100% for the codec registry sync meta-test

#### Lint rules

- No `time.Sleep` in `*_test.go` under `internal/eventbus/` and
  `test/integration/eventbus_e2e/` (custom golangci-lint rule)

#### What gets deleted from the old test suite

All `time.Sleep(1ms)` crutches; pgnotify-specific assertions in
`postgres_integration_test.go`; `cursor_lock_test.go`; `replay_test.go`;
old "Variant A Go/No-Go" (replaced); `events_immutable_test.go`
(rewritten against `events_audit`); `EventCursors` JSONB merge tests;
pgx.Conn lifecycle tests.

---

### 9. Security

#### Trust boundaries

| Component | Trust |
| --- | --- |
| Host process | Trusted; owns keys, signs publishes |
| Plugin processes | Sandboxed; manifest-validated, schema-isolated, capability-limited |
| Embedded NATS (in-process) | Inherits host trust; `DontListen` = no network |
| Clustered NATS (future) | Untrusted transport; mTLS + per-node creds mandatory |
| PostgreSQL | Storage only; trusted at rest until codec encryption lands |
| Network clients (telnet/web) | Untrusted; TLS + session token (PR #225) |

#### NATS auth

- **Embedded mode**: no auth surface; `nats.InProcessServer` bypass
- **Cluster mode** (future): mTLS + per-host NKey/JWT credentials; per-plugin
  NATS users with subject permissions matching manifest (defense in depth)

#### Codec key management (deferred but designed)

Three narrow responsibilities, three interfaces:

```go
// Codec is a crypto primitive. It does not know about subjects or
// routing — keep crypto narrow.
type Codec interface {
    Name() Name
    Encode(ctx context.Context, plaintext []byte, key Key) ([]byte, error)
    Decode(ctx context.Context, ciphertext []byte, key Key) ([]byte, error)
}

// KeyProvider supplies keys to codecs. Implementations resolve from
// local file, env, KMS, etc.
type KeyProvider interface {
    Active(ctx context.Context, label KeyLabel) (Key, error)  // for encrypt
    ByID(ctx context.Context, id KeyID) (Key, error)          // for decrypt
}

// KeySelector is the policy layer. It maps a subject (or other context)
// to (codec name, key label) per the deployment's encryption rules.
// Lives upstream of Codec; calls into KeyProvider after resolution.
type KeySelector interface {
    SelectForEncrypt(ctx context.Context, subject Subject) (codec Name, label KeyLabel, err error)
    SelectForDecrypt(ctx context.Context, codec Name, keyID KeyID) (Key, error)
}
```

The publish path now reads (pseudo-Go):

```go
codecName, label, _ := selector.SelectForEncrypt(ctx, event.Subject)
key, _              := keys.Active(ctx, label)
codec               := registry.Resolve(codecName)
ciphertext, _       := codec.Encode(ctx, plaintext, key)
header.App-Codec    = string(codecName)
```

This keeps the codec implementation stateless and subject-agnostic. The
subject→key mapping lives in `KeySelector` where it belongs as a policy
concern.

Codec internal envelope: `[1-byte version][8-byte key_id][12-byte
nonce][N-byte ciphertext][16-byte auth tag]`. Key ID enables rotation.
Provider implementations (none ship initially): `LocalFileKeyProvider`,
`EnvKeyProvider`, `KMSKeyProvider`, `MultiProvider`.

Subject → key routing via config (consumed by `KeySelector`):

```yaml
encryption:
  rules:
    - subject_pattern: "events.<game>.scene.>"
      codec: aes-gcm-v1
      key_label: scene-content
    - subject_pattern: "events.<game>.character.*"
      codec: aes-gcm-v1
      key_label: dm-content
```

Decryption failure (auth tag invalid, codec name unknown, OR key not
loaded) = hard error, security event in audit, never silent.

#### Plugin audit boundary

- Subject ownership routing: host's `events_audit` never contains
  plugin-owned subjects. Test enforces.
- Plugin schema `REVOKE ALL ON SCHEMA public` (existing): only the
  plugin's own PG role can read its audit table.
- No host fallback for plugin-owned `QueryHistory`: failure = caller
  error.
- Plugin RPC carries `SessionContext`; plugin enforces membership.

#### Auth on subscribe and query

ABAC re-evaluated on every filter change, not per event:

- `OpenSession(filters)` — `ValidateSessionOwnership` + ABAC for each filter
- `SetFilters(filters)` — ABAC for new filter set; failure unbinds
- `QueryHistory` — entry-time check; route to plugin if owned

#### Security event auditing

Reuses existing `internal/audit` subsystem (PR #203). New event types:
`auth.subscribe_rejected`, `auth.filter_rejected`,
`auth.query_history_unauthorized`, `codec.unknown_codec_name`,
`codec.decryption_failed`, `manifest.subject_ownership_conflict`,
`emit.subject_not_in_manifest`, `plugin.audit_rpc_timeout`.

#### Network exposure

- Embedded NATS: `DontListen: true` → no socket bound
- HTTP monitoring port: bound to `127.0.0.1`, opt-in
- gRPC server: existing TLS via `internal/tls/`
- Cluster NATS (future): internal network bind, mTLS, no anonymous

#### Threat model

| Threat | Mitigation |
| --- | --- |
| Compromised plugin reads other plugins' subjects | Subject ownership routing + per-plugin NATS subperm (cluster) |
| Compromised plugin publishes to non-owned subjects | Manifest validation + per-plugin NATS pubperm (cluster) |
| DB admin reads scene IC content | Plugin schema REVOKE + future codec encryption |
| Network attacker on NATS port | `DontListen` embedded; mTLS cluster |
| Replay/duplicate publish | `Nats-Msg-Id` dedup |
| Client manipulating cursor | Server owns durable consumer |
| Cold-storage / backup leakage | Codec encryption (deferred) + NATS stream encryption (defense in depth) |
| Lost JS storage | PG audit projection rebuild |
| Session token theft | Existing PR #225 ValidateSessionOwnership |
| Decryption failure | Hard error + security event + alert |
| Codec class deprecated | Old codec stays in registry; rotate-don't-remove |
| Key compromise | Rotate `Active` for label; old keys retained for read |

---

### 10. Cutover and land plan

Strategy: build infrastructure incrementally on `main` while consumers
stay on the old path; do the consumer swap atomically on a feature branch.

#### Phase A — Additive prep (each lands as its own PR to main)

| Step | Title | Risk |
| --- | --- | --- |
| M1 | `internal/eventbus` skeleton (interfaces, types) | Zero |
| M2 | Codec interface + `IdentityCodec` + registry + meta-test | Zero |
| M3 | Migration: add `events_audit` table | Low |
| M4 | `SubsystemEventBus` registered (no consumers) | Low |
| M5 | `SubsystemAuditProjection` (drains empty stream) | Low |
| M6 | `PluginAuditService` proto + bindings | Zero |
| M7 | OTEL header propagation + Prom NATS exporter wiring | Zero |

Each PR < 500 LOC churn, full `task pr-prep` green, additive only,
production behavior unchanged.

#### Phase B — Atomic cutover (feature branch `feat/eventbus-cutover`)

| Step | Title | Touches |
| --- | --- | --- |
| F1 | `EventSink` rewrites to `bus.Publish` | `internal/plugin/event_emitter.go`, `pkg/plugin/event_sink.go` |
| F2 | Audit projection actually projects (exclusion list active) | `internal/eventbus/audit/` |
| F3 | gRPC Subscribe handler rewritten | `internal/grpc/server.go`; deletes `cursor_lock.go`, `replay.go` |
| F4 | gRPC `QueryHistory` handler rewritten with hot/cold tier | `internal/grpc/query_history.go` |
| F5 | Per-plugin updates: subject names, manifest decls, **EventType constants AND payload types moved out of `internal/core/` into plugin packages** (see §1b) | `plugins/core-scenes/`, others; deletes `internal/core/event.go:39-143` |
| F6 | Schema cutover: DROP `events`, DROP `event_cursors` | `internal/store/migrations/` |
| F7 | Old code deletion (`EventWriter`, pgnotify, legacy Subscribe) | wide |
| F8 | Test rewrite (delete obsolete; port "Variant A"; add E2E suite) | `internal/store/postgres_integration_test.go`, `test/integration/eventbus_e2e/` |
| F9 | Docs (`CLAUDE.md`, `site/docs/`, mark prior specs superseded) | docs |

Branch CI green on every push; rebase on main weekly. Single squash
merge to main = production cutover.

#### Acceptance gates

- Phase A: per-PR `task pr-prep` green; coverage gate met; no regression
- Phase B (pre-merge): all M phases in main; full `task pr-prep` green on
  branch state; all E2E tests pass; coverage gates met; performance
  smoke (1k events/sec, < 100ms p99 publish→deliver); manual reviewer
  E2E walkthrough; in-flight PRs rebased

#### In-flight coordination

| In-flight | Strategy |
| --- | --- |
| PR #233 (session-lifecycle-events) | Land first; depended on for consumer GC |
| PR #234 (LeaveFocusByTarget) | Parallel; small, low conflict |
| Channel plugin (`holomush-0sc.12`, paused) | Stays paused; resumes after cutover with new subjects baked in |
| Scene Phase 4 (`holomush-5rh.13`) | Currently blocked; this design unblocks; new beads file post-cutover |
| Files F3 / F6 / F7 deletes | ~2-week freeze window; coordinate or stage |

#### Rollback

- M phases: revert commit; production unaffected
- F branch pre-merge: discard branch
- F branch post-merge: revert merge commit; rerun prior down migration;
  acceptable pre-launch
- Pre-merge precaution: PG dump the night before

#### Estimated calendar

```text
Week 1:    M1, M2, M3 → main
Week 2:    M4, M5, M6, M7 → main
Week 3:    F1, F2, F3 (core swap on branch)
Week 4:    F4, F5.1, F6, F7
Week 5:    F8 test sweep, F9 docs, soak, manual E2E
Week 5 EOW: merge to main
Week 6:    Stabilization, follow-up beads, scene Phase 4 unblock
```

Realistic envelope: 5–7 weeks.

#### Post-merge follow-ups (filed at merge time)

- Scene Phase 4 implementation
- Channel plugin resumption
- Verify holomush-umxj telnet race fix (already landed in PR #237) holds under new bus — F3 rewrites the Subscribe handler and gateway path that hosted the race; regression test from PR #237 must stay green
- Real codec implementation (separate workstream)
- Multi-node cluster cutover (when scale demands)

---

## Open questions

All decisions surfaced by the independent architect review (2026-04-18)
have been resolved (below). The spec is unblocked for implementation
plan-writing.

### Decisions resolved (2026-04-18)

1. **NATS version pinning + upgrade discipline (Q1)** — pin to latest GA
   minor of `nats-server`. Renovate trails GA bumps by 14 days
   (`minimumReleaseAge: "14 days"`) to avoid landing day-one bad patches;
   bumps grouped between `nats-server` and `nats.go`. Discipline + config
   in §7. PR-time gates via `task pr-prep` + chaos+soak nightly.

2. **Codec key provider (Q2)** — deferred to encryption implementation
   workstream. The `KeyProvider` interface ships now (§9); concrete KMS
   choice (AWS / GCP / Vault / Local) when threat model + compliance
   requirements crystallize. Filed as follow-up bead.

3. **Event proto schema location (Q3)** — host envelope at
   `api/proto/holomush/eventbus/v1/`. Plugin payload protos in each
   plugin's existing namespace (`api/proto/holomush/scenes/v1/`, etc.).
   Codified in §1d.

4. **Typed `Subject` and `Type` strings (Q4)** — adopted. `type Subject
   string` and `type Type string` with validating constructors. Codified
   in §1a.

5. **Multi-tenancy `events.<game-id>.` prefix (Q5)** — adopted from day
   one. Single-game default `game_id: "main"` in `event_bus` config.
   Codified in §1c and §7.

6. **`EventBus` interface split (Q6)** — adopted. Three single-
   responsibility interfaces (`Publisher`, `Subscriber`, `HistoryReader`)
   composed by `EventBus`. Call sites depend only on what they need.
   Codified in §4.

7. **Typed `Delivery` handle (Q7)** — adopted. `(Delivery, error)` from
   `Next()`; `Delivery` has `Event()`, `Ack()`, `Nack()`, `InProgress()`.
   Replaces the `(Event, AckFunc, error)` tuple. Codified in §4.

8. **Longest-prefix-wins subject ownership routing (Q8)** — adopted,
   token-aligned. Property test for resolution determinism added to §8.
   Codified in §6.

9. **Drop `HistoryStream.Cursor()` (Q9)** — adopted. Caller records
   ULID from the last `Event` returned by `Next()`. No stateful side-
   channel. Codified in §5.

10. **Plugin-owned audit coupling (Q10)** — keep current design with an
    explicit performance budget (Section 8). Privacy boundary
    (credential separation, plugin-enforced authz, future plugin-side
    decryption) wins over coupling reduction. Performance concerns
    quantified and made into hard CI gates (§8 perf budget): plugin
    `AuditEvent` RPC p99 ≤ 50ms, plugin `QueryHistory` per-page RPC p99
    ≤ 250ms. Reasoning baked into §8: HoloMUSH is a text-based MUSH for
    humans reading prose, priorities are
    Privacy > Stability > Extensibility > Resilience > Performance.

11. **Codec interface signature (Q11)** — adopted. `Codec.Encode/Decode`
    takes `(ctx, bytes, key Key)`. Subject→key routing lives in a
    separate `KeySelector` interface upstream. Codified in §9.

12. **`HistoryQuery.SessionContext` via `context.Context` (Q12)** —
    adopted. Removed from the struct; carried via `auth.WithSession(ctx,
    sess)`. Codified in §5.

### Acknowledged (will be addressed during plan-writing or as
discovered-from beads)

- **Schema evolution under live load** (reviewer F3) — need explicit
  upgrade path for proto v1 → v2 including who handles old events in
  audit, subscribers, backfill tool. Plan-writing must define the
  versioning playbook (filed as discovered-from bead).
- **Codec key retention is forever** (reviewer F4) — `KeyProvider.ByID`
  implies any key ever used to encrypt persisted data must remain
  decryptable. Has compliance implications (key revocation impossible
  without re-encrypting all old data). Document explicitly in §9 and as
  follow-up bead.
- **Per-subject Prometheus cardinality discipline** (reviewer F6) —
  with `events.scene.<ULID>.ic` subjects, per-subject metrics would
  explode cardinality. Plan-writing must define the aggregation rule
  (per-domain, not per-subject). Filed as bead.
- **Future event-level ABAC** (reviewer F7) — JS doesn't natively
  support efficient per-event filtering; if ever needed, host pays
  delivery+ack cost for events users never see. Document the future
  cost; not a blocker today.
- **Cluster cutover discipline** — §7 now states honestly that this is a
  multi-week migration. Spec does not commit to when. When it lands,
  follows its own spec.
- **Phase A idleness verification** (Q6 from prior version) — startup
  test must assert: in M4 with no F-phase code, the embedded NATS server
  starts, no consumers attach, no events flow, no audit rows produced.
  Move from open question to plan-writing checklist item.

### Resolved by review (no further action)

- ULID stays as primary identity; JS seq is internal — confirmed sound.
- `EventWriter` deletion — confirmed sound; ULID monotonicity preserved
  via `core.NewULID`'s in-process mutex which is not a serialization
  bottleneck.
- Cursor race class elimination via per-event ack + ULID dedup — confirmed
  sound, with the caveat that subscribers need a "client dedup contract"
  (now documented in §3).
- Single `EVENTS` stream design — confirmed sound for our scale.

---

## Glossary

- **Event**: an immutable, durable record of something that happened.
- **Subject**: a hierarchical, dot-delimited routing key in JetStream.
  Plugin-namespaced (e.g., `events.scene.<ULID>.ic`).
- **Stream** (NATS): a JetStream stream is a persistent log of messages
  on a subject pattern. We use exactly one (`EVENTS`).
- **Stream** (legacy): the old `core.Event.Stream` field, free-form string
  like `location:<ULID>`. Deleted in this design.
- **Subscription** (legacy): the old `core.Subscription` interface.
  Deleted; replaced by `SessionStream`.
- **Consumer** (NATS JetStream): server-side delivery state for a
  subscriber, with cursor and acknowledgment tracking.
- **Durable consumer**: a consumer whose state survives across client
  reconnects.
- **Ephemeral consumer**: a consumer that dies when the client
  disconnects; used for `QueryHistory`.
- **Codec**: a host-owned encode/decode pair applied to event payload
  bytes. Identity by default; pluggable for encryption later.
- **Audit projection**: a worker that consumes from JetStream and writes
  to a PostgreSQL table for forever-archive and queryability.
- **Plugin-owned audit**: audit projection where the plugin owns the
  destination schema and the read path (privacy boundary).
- **Subject ownership map**: startup-built mapping of subject prefixes
  to either "host" or a specific plugin. Conflicts at load = error.

## References

- [docs/specs/2026-03-20-event-delivery-redesign.md](../../specs/2026-03-20-event-delivery-redesign.md)
  — superseded by this design
- [2026-04-05-plugin-architecture-rework-design.md](2026-04-05-plugin-architecture-rework-design.md)
  — proto-first plugin contracts; this design extends with `PluginAuditService`
- [2026-04-06-scenes-and-rp-design-v2.md](2026-04-06-scenes-and-rp-design-v2.md)
  — §11 plugin→host event emission contract resolved by this design (option A)
- [2026-04-08-plugin-session-stream-contribution-design.md](2026-04-08-plugin-session-stream-contribution-design.md)
  — `StreamContributor.QuerySessionStreams` integrates with `SetFilters`
- [2026-04-15-query-stream-history-rpc-design.md](2026-04-15-query-stream-history-rpc-design.md)
  — `QueryStreamHistory` reshaped as `bus.QueryHistory` with hot/cold tier
- [docs/plans/2026-04-07-cursor-lock-finding-1-closure.md](../../plans/2026-04-07-cursor-lock-finding-1-closure.md)
  — cursor race class eliminated by JS durable consumer
- PR #207 — `EventSink` unified emission (kept; underlying transport changes)
- PR #216 — host focus RPCs (kept; integrates via `SetFilters`)
- PR #225 — `ValidateSessionOwnership` choke-point (kept; called in `OpenSession`)
- PR #232 — `QueryStreamHistory` (semantically preserved as `bus.QueryHistory`)
- PR #233 — session-lifecycle events (depended on for consumer GC)
- [NATS multi-subject filters (server 2.10)](https://github.com/nats-io/nats.docs/blob/master/release_notes/whats_new_210.md)
- [NATS embedded server pattern](https://github.com/nats-io/nats-server)
- [nats-server #6993 — MaxBytes memory reservation](https://github.com/nats-io/nats-server/issues/6993)
- [nats-server #7779 — FilterSubjects subset overlap](https://github.com/nats-io/nats-server/issues/7779)
