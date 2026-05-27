<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Gateway Verb Registry Sourcing — Phase 1.6 Companion Spec

## Status

**DRAFT** — design proposal pending implementation plan.

See also: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
(Phase 1 of the event-payload-crypto landing series; this spec adds the
gateway-side resolution that Phase 1 implementation surfaced as a gap).
That spec currently lives on sibling bookmark `docs/event-payload-crypto-spec`,
off `main`. Both specs land to `main` independently; cross-references
resolve once both are merged.

## Authors

- Sean Brandt
- Claude (collaborator)

## Date

2026-04-26

---

## Context

Phase 1 of the event-payload-crypto landing series (`holomush-k18g`) migrated
plugin-owned `EventType` constants out of `internal/core/event.go` into each
plugin's package. As part of that work the manifest grammar was extended with
`crypto.emits` and `verbs:` blocks, and the plugin loader at
`internal/plugin/manager.go:782-805` was wired to register each plugin's
declared verbs into a `core.VerbRegistry` instance.

That phase landed implementation-complete on `feat/crypto-phase1` (commit
`lxn`, 16 commits) with all unit and integration tests green. `task pr-prep`
fails at E2E with seven test failures in `web/e2e/terminal.spec.ts` because
the *gateway* process — which runs as a separate binary connected to the
core via gRPC — has its own disconnected `VerbRegistry` instance, seeded
only by `core.RegisterBuiltinTypes`. Plugin-declared verbs (`say`, `pose`,
`whisper`, `object_*`, etc.) are absent from the gateway's registry, so
`internal/web/translate.go:48-49` lookups miss, fall back to
`category="system"`, and the web client's `EventRenderer` routes those
events to `FallbackRenderer` instead of `CommunicationRenderer`. Say events
stop rendering with their content; the E2E suite fails.

A deeper finding emerged during context exploration: in current production
*neither* the core gRPC server nor the plugin manager actually wires a
`VerbRegistry` instance. Both have `WithVerbRegistry` options that are
unused outside the gateway. The plugin loader's verb-registration code at
`manager.go:782-805` silently no-ops because `m.verbRegistry == nil`. The
gateway is the only live consumer, and it sees only host builtins.

This spec defines how rendering metadata flows from plugin manifests
through the core process to the gateway and to clients, in a way that:

- Eliminates the gateway's local `VerbRegistry` entirely (the gateway has
  no business holding domain state).
- Survives multi-host deployments where the gateway runs in a separate
  container or on a separate host from the plugin filesystem.
- Stays compatible with the crypto Phase 3 emit/decrypt path —
  rendering metadata becomes part of AEAD's AAD when payload encryption
  ships.
- Closes `holomush-k18g.5` and unblocks `feat/crypto-phase1` for merge.

### Project context

- Single-node deployment, ~200 concurrent users, ~1k events/sec target.
- `feat/crypto-phase1` is implementation-complete; this spec lands as
  Phase 1.6 on top of it (commit `lxn` is the foundation).
- The crypto Phase 3 emit/decrypt path is not yet implemented; AAD coverage
  of rendering metadata is forward-declared (`INV-GW-12`).
- No production users, no released versions, no migration surface to
  preserve.

### Out of scope (non-goals)

See Section 9 for the full list. Headlines:

- `ListVerbs` RPC on `CoreService` — not needed under Approach B.
- Host-pseudo-manifest (deleting `RegisterBuiltinTypes` entirely) —
  filed as `holomush-k18g.7`, not load-bearing for closing this gap.
- Sweeping `protovalidate` annotations across existing `core/v1` fields —
  filed as `holomush-k18g.8`.
- Backwards compatibility for pre-1.6 `events_audit` rows — N/A by user
  decision.

---

## Goals

1. **Eliminate the gateway's `VerbRegistry`.** The gateway process is a
   protocol-translation + connection-management layer. It MUST NOT hold
   domain-aware caches.
2. **Single source of truth for verb metadata.** Core's verb registry,
   populated at boot from host builtins and at plugin-load from manifest
   `verbs:` blocks, is the authority.
3. **Wire rendering metadata onto every `EventFrame`.** Core's
   `RenderingPublisher.Publish` stamps a `RenderingMetadata` sub-message at emit time;
   subscribers and history readers deliver what's stored.
4. **Codify gateway thinness as a CI-enforced invariant.** An import-graph
   tripwire test prevents the gateway from accidentally taking on domain
   dependencies.
5. **Preserve audit/historical fidelity.** Old events keep the rendering
   they were emitted with, even after a plugin reloads with a tweaked
   verb. `source_plugin_version` makes drift visible.
6. **Stay compatible with crypto Phase 3.** Rendering metadata lives in
   the cleartext metadata band; AEAD's AAD covers it so it cannot be
   silently rewritten on the wire.
7. **Close `holomush-k18g.5`.** `task pr-prep` E2E goes green.

---

## Section 1 — Architecture

### Approach: enrich at emit, embed in `EventFrame`

The chosen architecture is to add a `RenderingMetadata` sub-message to the
`EventFrame` proto and have `RenderingPublisher.Publish` populate it once at emit time
from the core-side `VerbRegistry`. JetStream stores the stamped frame; the
`events_audit` projection mirrors it byte-for-byte (existing `INV-21` from
the crypto spec). Subscribers and history readers deliver what's stored.
The gateway has zero registry — `internal/web/translate.go` and
`internal/telnet/gateway_handler.go` read fields directly off
`ev.GetRendering()`.

### Why this shape

| Property | Value |
|---|---|
| Gateway state | None — fully stateless w.r.t. domain |
| Plugin-reload race | Impossible by construction (no cache) |
| Startup ordering between gateway and core | None — first event carries metadata |
| Wire cost | ~30–80 bytes per event (~30–80 KB/s at 1k events/sec) |
| Cold-tier read path | No special-case — same fields are stored in `events_audit` |
| Crypto Phase 3 fit | Rendering metadata joins AAD; payload encryption is orthogonal |
| Multi-host gateway | Already supported — gRPC is the only transport |
| Historical fidelity | Old events keep old rendering; `source_plugin_version` exposes drift |

### Components

| Component | Responsibility | Process / location |
|---|---|---|
| `core.VerbRegistry` (existing, extended) | Map event type → rendering metadata + source plugin info | In-process, core only |
| `core.BootstrapVerbRegistry(hostVersion string)` (new) | Single canonical seeded constructor | Package `core` |
| `core.registerBuiltinTypes()` (renamed, unexported) | Register host-owned event types into a registry | Package `core`, internal |
| `eventbus.RenderingPublisher` (new) | Wrap `eventbus.Publisher`; look up verb registration, stamp `Rendering`, validate, delegate to the underlying publisher | In-process, core only |
| `eventbus.Event` (Go struct, extended) | Add `Rendering *RenderingMetadata` field carrying enriched metadata | In-process |
| `eventbusv1.Event` (proto, extended) | JetStream wire format gains `rendering` sub-message | Wire format |
| `corev1.EventFrame` (proto, extended) | gRPC Subscribe wire format gains `rendering` sub-message | Wire format |
| `corev1.RenderingMetadata` / `eventbusv1.RenderingMetadata` (proto, new) | Rendering metadata sub-message (defined once in `corev1`, imported by `eventbusv1`) | Wire format |
| `corev1.EventChannel` (proto, lifted from `webv1`) | Display target enum | Wire format |
| `events_audit` schema (extended, migration `000012`) | New `rendering` JSONB NOT NULL column | PostgreSQL |
| Audit projection (extended) | Serialize `Event.Rendering` into `events_audit.rendering` | In-process, core only |
| `internal/plugin.PluginEventEmitter` (existing, unchanged at the seam) | Builds `eventbus.Event` from `EmitIntent`; receives the `RenderingPublisher` as its underlying publisher | In-process, core only |
| Gateway translation (web + telnet) | Read `ev.GetRendering()`; build client wire format | Gateway process |
| Import-graph tripwire test | Enforce gateway thinness | Static (compile/test) |

### Trust boundaries

The gateway thinness invariant operates as a structural separation between:

- **Core process:** owns `VerbRegistry`, executes `RenderingPublisher.Publish`, makes
  domain decisions.
- **Gateway process:** translates protocol formats. Reads `RenderingMetadata`
  off the wire. Holds no registry, no event store, no domain logic.

The `internal/core` package is shared (both processes need proto types
and connection primitives). The tripwire forbids domain packages
(`internal/world`, `internal/access`, `internal/store`, `internal/plugin`,
`internal/eventbus`, `internal/auth/service`, `internal/command`) from
gateway-binary imports.

### Emit flow

The seam is `eventbus.RenderingPublisher`, a wrapper around the
underlying `eventbus.Publisher`. All emit sites — both
`internal/plugin.PluginEventEmitter` (plugin emits) and host-direct
emit sites — receive the wrapped publisher. Enrichment is transparent.

```text
Emit caller (PluginEventEmitter or host code) builds an eventbus.Event
                          │
                          ▼
                eventbus.Publisher.Publish(ctx, event)
                          │
              (interface implemented by RenderingPublisher)
                          │
                          ▼
RenderingPublisher.Publish(ctx, event)
  ├── Lookup: reg, ok := verbRegistry.Lookup(string(event.Type))
  ├── If !ok:   return EMIT_UNKNOWN_VERB                          (INV-GW-3)
  ├── Build:    event.Rendering = &eventbus.RenderingMetadata{
  │                 Category:             reg.Category,
  │                 Format:               reg.Format,
  │                 Label:                reg.Label,
  │                 DisplayTarget:        reg.DisplayTarget,
  │                 SourcePlugin:         reg.Source,
  │                 SourcePluginVersion:  verbRegistry.SourceVersion(reg.Source),
  │             }                                                  (INV-GW-2)
  ├── HeaderJSON: renderingJSON = json.Marshal(event.Rendering)
  │               event.Headers["App-Rendering"] = renderingJSON
  ├── Validate: protoEv := toProtoEvent(event)
  │             if err := validator.Validate(protoEv); err != nil
  │                 return EMIT_VALIDATION_FAILED                  (INV-GW-4)
  └── Delegate: inner.Publish(ctx, event)
                  ├── JetStreamPublisher.Publish:
                  │     ├── Build eventbusv1.Event proto from event,
                  │     │     INCLUDING Rendering field             (INV-GW-3a)
                  │     ├── Marshal envelope, attach App-Rendering
                  │     │     header to NATS message
                  │     └── JetStream stores envelope + headers
                  └── AuditProjection reads App-Rendering header and writes
                      events_audit.rendering JSONB column            (INV-GW-13)
```

The Go-side `eventbus.Event` struct gains a `Rendering *RenderingMetadata`
field, mirroring the proto sub-message. All emit-site builders stop short of
populating it; `RenderingPublisher` is the only writer.

### Subscribe / history flow

No enrichment on the read path. The wire-side `corev1.EventFrame` builders
in `internal/grpc/server.go:toProtoSubscribeResponse` and
`internal/grpc/query_stream_history.go` copy `event.Rendering` through to
`EventFrame.Rendering` when serializing. Cold-tier `QueryStreamHistory`
reads `events_audit.rendering` from PostgreSQL and reconstructs the same
shape. The hot/cold tier crossover from F4 (existing) requires no other
changes for this work.

### Gateway translation flow

```text
Gateway receives EventFrame ev
  ├── If ev.Rendering == nil:
  │       drop event
  │       increment holomush_gateway_dropped_nil_rendering_total
  │       log error                                          (INV-GW-5)
  └── Else:
        build client wire format (web GameEvent or telnet ANSI)
        from ev.Rendering.{Category, Format, Label, DisplayTarget}
        + ev.Type, ev.Timestamp, ev.ActorType, ev.ActorId, ev.Payload
```

The gateway holds no registry, performs no lookup, and has no fallback
rendering for nil-`Rendering`. A nil `Rendering` reaching the gateway is a
contract violation upstream; surfacing it loudly (drop + metric + log) is
strictly correct.

---

## Section 2 — Proto changes

### `RenderingMetadata` sub-message (new)

```proto
// In api/proto/holomush/core/v1/core.proto

message RenderingMetadata {
  option (buf.validate.message).cel = {
    id: "rendering_metadata.label_required_for_speech"
    message: "label must be set when format is 'speech'"
    expression: "this.format != 'speech' || this.label != ''"
  };

  // Category drives client-side renderer routing.
  // Open-string by design: plugin authors may introduce new categories
  // without proto schema changes. Uniqueness/existence is enforced by
  // VerbRegistry.Register at the Go layer.
  string category = 1 [(buf.validate.field).string.min_len = 1];

  // Format drives within-category presentation.
  string format = 2 [(buf.validate.field).string.min_len = 1];

  // Label provides type-specific display text. Required when format == "speech".
  string label = 3;

  // DisplayTarget routes the event to TERMINAL, STATE, or BOTH on the client.
  EventChannel display_target = 4 [(buf.validate.field).enum = {
    defined_only: true
    not_in: [0]
  }];

  // SourcePlugin names the plugin that owns this event type, or "builtin"
  // for host-owned types. Recorded for historical/audit fidelity.
  string source_plugin = 5 [(buf.validate.field).string.min_len = 1];

  // SourcePluginVersion is the manifest's version field, or "host-<binary
  // version>" for builtins. Recorded for historical/audit fidelity and to
  // make verb-definition drift visible to operators.
  string source_plugin_version = 6 [(buf.validate.field).string.min_len = 1];
}
```

### `EventChannel` (introduced in `corev1`; `webv1` retained)

```proto
// In api/proto/holomush/core/v1/core.proto

enum EventChannel {
  EVENT_CHANNEL_UNSPECIFIED = 0;
  EVENT_CHANNEL_TERMINAL    = 1;
  EVENT_CHANNEL_STATE       = 2;
  EVENT_CHANNEL_BOTH        = 3;
}
```

`webv1.EventChannel` continues to exist with the same values. The two
enums are kept in lockstep by an enum-parity test. The translation
boundary in `internal/web/translate.go` converts
`corev1.EventChannel` → `webv1.EventChannel` when shaping the
`webv1.GameEvent` for the web client.

**Why not lift fully and delete `webv1.EventChannel`?** A full lift
would force migration of:

- `internal/core/registry.go:11,20` — `VerbRegistration.DisplayTarget`
  is `webv1.EventChannel`.
- `internal/plugin/manager.go:1242-1252` —
  `displayTargetFromString` returns `webv1.EventChannel`.
- `internal/web/translate_test.go:42-54` — every fixture uses
  `webv1.EventChannel_*` constants.
- `internal/telnet/gateway_handler_test.go` — same.
- `pkg/proto/holomush/web/v1/web_pb.go:343` — `GameEvent.DisplayTarget`
  field type.
- `web/src/lib/connect/holomush/web/v1/web_pb.ts` — generated TS
  bindings on the web client.

Most of these sites are gateway-side or test-side and would need
coordinated updates. The web-client TS regeneration alone is cross-cutting
enough to risk scope creep. Keeping both enums and converting at the
translation boundary contains the change to the proto layer plus
`translate.go`'s conversion helper.

**Internal Go-side migration:** `internal/core/VerbRegistration.DisplayTarget`
migrates from `webv1.EventChannel` to `corev1.EventChannel`.
`internal/plugin/manager.displayTargetFromString` returns
`corev1.EventChannel`. Test fixtures in
`internal/web/translate_test.go`, `internal/telnet/gateway_handler_test.go`,
and `cmd/holomush/gateway_test.go` migrate to `corev1.EventChannel_*`
constants. `webv1.EventChannel` survives only at the
`webv1.GameEvent.DisplayTarget` boundary, populated via the
boundary conversion in `translate.go`.

**Conversion helper:**

```go
// internal/web/translate.go (or a small adjacent file)

func toWebV1EventChannel(c corev1.EventChannel) webv1.EventChannel {
    return webv1.EventChannel(c) // values match; enum-parity test asserts
}
```

**Enum parity test (`INV-GW-16`):**

```go
func TestEventChannelEnumsInLockstep(t *testing.T) {
    // Asserts: same set of values, same numeric assignments,
    // same names. Fails on any drift.
}
```

`webv1.EventChannel` is filed for full removal as `holomush-k18g.10`
(P3, after the web client TS bindings can be regenerated as part of a
larger refactor).

### `EventFrame` extension

```proto
// In api/proto/holomush/core/v1/core.proto

message EventFrame {
  string id = 1;
  string stream = 2;
  string type = 3;
  google.protobuf.Timestamp timestamp = 4;
  string actor_type = 5;
  string actor_id = 6;
  bytes payload = 7;
  bytes cursor = 8;

  // Rendering metadata — cleartext band, populated by RenderingPublisher
  // from the verb registry. Joined to AAD when payload is encrypted
  // (Phase 3+, INV-GW-12). MUST be present on every frame produced by
  // this server (INV-GW-2). The gateway treats absence as a contract
  // violation and drops the event with a metric (INV-GW-5).
  RenderingMetadata rendering = 9;
}
```

Field numbers 10+ are reserved for future top-level frame additions (e.g.,
crypto codec metadata in Phase 3+).

### `eventbusv1.Event` extension (JetStream wire format)

```proto
// In api/proto/holomush/eventbus/v1/eventbus.proto

import "holomush/core/v1/core.proto";  // for RenderingMetadata

message Event {
  bytes id = 1;
  string subject = 2;
  string type = 3;
  google.protobuf.Timestamp timestamp = 4;
  Actor actor = 5;
  bytes payload = 6;

  // Rendering metadata, populated by RenderingPublisher.Publish before
  // marshaling for JetStream. Persisted in JetStream message storage
  // alongside the rest of the event for full historical fidelity.
  // Mirrors the corev1.RenderingMetadata used on the gRPC Subscribe wire.
  holomush.core.v1.RenderingMetadata rendering = 7;
}
```

The same `RenderingMetadata` message is shared across `corev1` (gRPC
Subscribe wire) and `eventbusv1` (JetStream wire) via proto import. One
schema, two transports.

### `events_audit` schema migration

```sql
-- internal/store/migrations/000012_events_audit_rendering.up.sql

-- Three-step add so the migration is safe on non-empty events_audit and
-- idempotent under repeated application: nullable add → backfill → SET
-- NOT NULL. Going straight to ADD COLUMN ... NOT NULL would fail on any
-- pre-existing row.
ALTER TABLE events_audit
  ADD COLUMN IF NOT EXISTS rendering JSONB;

UPDATE events_audit
SET rendering = '{}'::jsonb
WHERE rendering IS NULL;

ALTER TABLE events_audit
  ALTER COLUMN rendering SET NOT NULL;
```

```sql
-- internal/store/migrations/000012_events_audit_rendering.down.sql

ALTER TABLE events_audit DROP COLUMN IF EXISTS rendering;
```

The `rendering` column stores the canonical JSON encoding of
`RenderingMetadata`.

**Transport between `RenderingPublisher` and the audit projection.** The
audit projection at `internal/eventbus/audit/projection.go:166-255` is
purely header-driven: it reads metadata exclusively from NATS message
headers and stores `msg.Data()` (the codec-encoded envelope bytes) as
the audit row's `payload` column. It never proto-unmarshals
`eventbusv1.Event`. To populate `events_audit.rendering` without
introducing a codec/key dependency in the projection (which would couple
audit-write to crypto Phase 3 unnecessarily), `RenderingPublisher` stamps
a NATS header carrying the JSON-encoded `RenderingMetadata` *before*
delegating to the underlying publisher. The projection reads that header
and writes it to `events_audit.rendering`.

**Canonical JSON form: `protojson`.** The header value is the
deterministic `protojson` encoding of `corev1.RenderingMetadata` —
**not** stdlib `encoding/json` over the Go-side mirror struct, which
would emit enum integers and ad-hoc field names.

```go
// internal/eventbus/rendering_publisher.go (sketch)

var renderingJSONOpts = protojson.MarshalOptions{
    UseProtoNames: true,    // snake_case field names matching the proto
    UseEnumNumbers: false,  // emit "EVENT_CHANNEL_TERMINAL", not 1
    EmitUnpopulated: true,  // stable shape across producers
}

func renderingHeaderJSON(md *corev1.RenderingMetadata) ([]byte, error) {
    return renderingJSONOpts.Marshal(md)
}
```

Header bytes example (after `protojson.Marshal`):

```text
Header name:   App-Rendering
Header value:  {"category":"communication","format":"speech",
                "label":"says","display_target":"EVENT_CHANNEL_TERMINAL",
                "source_plugin":"core-communication",
                "source_plugin_version":"0.1.0"}
```

This means `RenderingMetadata` is on the wire in two places:

1. **Inside the proto envelope** (`eventbusv1.Event.Rendering` field) —
   for subscribe-path consumers that proto-unmarshal the envelope and
   project it into `corev1.EventFrame.Rendering`.
2. **As a NATS header** (`App-Rendering`) — for the audit projection,
   which does not proto-unmarshal envelopes.

The `protojson` form in the header is the canonical wire form for audit
storage. The proto inside the envelope is the canonical wire form for
the gRPC subscribe path. `RenderingPublisher` is the one writer that
populates both, ensuring they cannot drift. A unit test (`INV-GW-15`)
asserts that for any given event:

```go
// Decode the header bytes back into a corev1.RenderingMetadata,
// re-marshal both via the same protojson options, and byte-compare:
headerMD := &corev1.RenderingMetadata{}
require.NoError(t, protojson.Unmarshal(headerBytes, headerMD))
envelopeMD := envelope.GetRendering()
expected, _ := renderingJSONOpts.Marshal(envelopeMD)
got, _      := renderingJSONOpts.Marshal(headerMD)
assert.Equal(t, expected, got)
```

The `events_audit.rendering` JSONB column also stores `protojson`
output; a JSONB column makes the storage form queryable via
PostgreSQL's `->`, `->>`, `@?` operators (see Section 3 SQL recipe
for plugin-version drift inspection).

Rationale for `NOT NULL` without `DEFAULT`: every freshly-projected row
carries the publisher-stamped rendering blob, so a column-level DEFAULT
would only mask host bugs that fail to populate the field. The migration
itself adds the column nullable, backfills any pre-existing rows with
`'{}'::jsonb`, then promotes to `NOT NULL` — that pattern is safe on
non-empty `events_audit` (older deployments, replay/restore scenarios)
and is idempotent under repeated application. Once the migration
completes, every subsequent insert MUST supply rendering or the writer
fails loudly. CI/test environments fresh-create the database via
`task test:int` / testcontainers; the three-step migration is also safe
there because no rows pre-date it.

### Go-side `eventbus.Event` struct extension

```go
// internal/eventbus/types.go

type Event struct {
    ID        ulid.ULID
    Seq       uint64
    Subject   Subject
    Type      Type
    Timestamp time.Time
    Actor     Actor
    Payload   []byte

    // Rendering is populated by RenderingPublisher.Publish before marshaling.
    // Callers MUST NOT populate this field directly; the field is reserved
    // for the publisher chain.
    Rendering *RenderingMetadata

    // Headers carries pre-publish NATS headers stamped by the publisher
    // chain (e.g. App-Rendering by RenderingPublisher). JetStreamPublisher
    // merges these into the outgoing nats.Msg headers alongside the
    // system-stamped ones (Nats-Msg-Id, App-Codec, App-Schema-Version,
    // App-Event-Type, App-Actor-Kind, App-Actor-ID, App-Actor-Legacy-ID,
    // plus OTEL traceparent/tracestate). Callers other than the publisher
    // chain MUST NOT populate this field directly. Out-of-band: not part
    // of the proto-marshaled envelope, not covered by AAD, not copied
    // into events_audit.payload — header transport only.
    //
    // Reserved-keys rule: caller-written keys MUST start with "App-" and
    // MUST NOT be in the system-reserved set above. Keys starting with
    // "Nats-" are reserved unconditionally. Violation panics in dev,
    // logs a warning and the system value wins in prod.
    //
    // Cold-tier reads: this field is publish-path only. The cold-tier
    // history reader reconstructs Event from events_audit rows and
    // leaves Headers nil. Subscribers MUST NOT depend on Headers being
    // populated at read time; they read Event.Rendering directly.
    Headers map[string]string
}

type RenderingMetadata struct {
    Category             string
    Format               string
    Label                string
    DisplayTarget        EventChannel  // mirror of corev1.EventChannel
    SourcePlugin         string
    SourcePluginVersion  string
}
```

**Why a `Headers` map and not a single `RenderingHeaderJSON []byte` field:**
keeping the Go-side seam generic preserves the option for future
pre-publish headers (e.g. distributed-tracing context, debug
correlation IDs) without re-extending `Event` each time. A type-checked
slot for *just* rendering would be tighter but offers no real benefit
since the publisher chain is the only writer.

**Go ↔ proto conversion helpers** live alongside the existing
`actorToProto` helper at `internal/eventbus/publisher.go` (or a sibling
`types_proto.go` if the plan prefers separation). Two functions:
`renderingToProto(*RenderingMetadata) *corev1.RenderingMetadata` and
`renderingFromProto(*corev1.RenderingMetadata) *RenderingMetadata`,
exercised by the `INV-GW-14` parity test.

**`JetStreamPublisher.Publish` change:** at `internal/eventbus/publisher.go:200-214`
(the existing header-construction site), after stamping the system
headers (`Nats-Msg-Id`, `App-Codec`, etc.), the publisher iterates
`event.Headers` and copies each entry into the `nats.Msg.Header`. A
collision between a caller-supplied header key and a system-stamped
header key is a host bug. Detection branches on `testing.Testing()`
(Go 1.21+, available since this repo is on Go 1.26+): in tests it
panics so the bug surfaces immediately; in production binaries it
logs a warning and the system value wins (system headers are
load-bearing for downstream replay). This requires importing
`"testing"` from `internal/eventbus/publisher.go` — a known small
trade-off (the `testing` flag set is registered at init in production
binaries). Acceptable for the strict-failure intent.

`EventChannel` in package `eventbus` is a small mirror of the proto enum.
(The `eventbus` package already depends on `pkg/proto/holomush/eventbus/v1`
for envelope marshaling, so the mirror is not for dependency hygiene; it
exists for ergonomic reasons — host-side test fixtures and emit-site
struct literals don't have to import the proto package just to express a
display target.) The two are kept in sync by a unit test (`INV-GW-14`).
If the mirror proves more friction than benefit during execution, the
plan may drop it and use `corev1.EventChannel` directly in
`eventbus.RenderingMetadata`.

### Validation enforcement points

1. **Proto layer (compile-time):** `buf.validate.field` annotations on
   each `RenderingMetadata` field, plus the message-level CEL rule for
   the `label`-when-`format=="speech"` cross-field constraint.
2. **Go layer (`VerbRegistry.Register`):** existing checks at
   `internal/core/registry.go:44-69` continue to enforce non-empty
   category/format and `label`-when-`speech`. These are belt-and-braces
   against bugs that bypass the proto layer (e.g., manually constructed
   `RenderingMetadata` values that skip protovalidate).
3. **Emit layer (`RenderingPublisher.Publish`):** runs `protovalidate.Validate(ev)`
   on the stamped frame before publishing. Failure returns
   `EMIT_VALIDATION_FAILED` (`INV-GW-4`).

**CEL precedent.** Search of `api/proto/` shows no existing message-level
`buf.validate.message.cel` rules in this codebase — only field-level
rules. The `RenderingMetadata` cross-field constraint introduces the
first message-level CEL rule. The `protovalidate` library version in
`go.mod` (`buf.build/go/protovalidate v1.1.3`) supports
`buf.validate.message`; verifying that `buf generate` succeeds on the
new proto is a Plan Task 1 step (the very first proto-regen run).
Treat this as a known-but-low-risk new pattern.

### Scope of protovalidate adoption

This spec only annotates the new `RenderingMetadata` fields. Existing
`EventFrame` fields (`id`, `type`, `actor_*`, etc.) are not annotated in
this phase; that's filed as `holomush-k18g.8`.

---

## Section 3 — Enrichment flow

### Single enrichment site

`internal/eventbus.RenderingPublisher.Publish` is the only place rendering
metadata gets stamped onto an event. There is no enrichment on the
subscribe path, no enrichment on the history-read path, no enrichment on
the gateway side. One place, one source of truth.

`RenderingPublisher` is a wrapper around the underlying `eventbus.Publisher`
(JetStream-backed). All emit-site callers — both `PluginEventEmitter` for
plugin emits and host-direct emits in core code — receive
`RenderingPublisher` as their `eventbus.Publisher` dependency. The wrap
is invisible to callers.

### `VerbRegistry` extensions

`internal/core/registry.go` gains:

```go
// SourceInfo records origin metadata for a verb registration.
type SourceInfo struct {
    Source  string // "builtin" or plugin manifest name
    Version string // host build version for builtin, manifest version for plugins
}

// Register (existing) accepts an additional version-bearing call site
// via a new method:
func (r *VerbRegistry) RegisterWithSource(reg VerbRegistration, version string) error

// SourceVersion returns the version recorded for a given source name.
// Returns "" if source is unknown.
func (r *VerbRegistry) SourceVersion(source string) string
```

The existing `Register(reg VerbRegistration)` becomes a thin wrapper that
calls `RegisterWithSource(reg, "")` for tests that don't care about
versioning. Production callers (the plugin loader and `registerBuiltinTypes`)
use `RegisterWithSource` with a real version.

### Plugin loader changes

`internal/plugin/manager.go:782-805` becomes:

```go
// Register plugin-declared verbs in the VerbRegistry.
// VerbRegistry is required (INV-GW-10). Construction has already
// failed if it is nil.
for _, vs := range dp.Manifest.Verbs {
    regErr := m.verbRegistry.RegisterWithSource(core.VerbRegistration{
        Type:          vs.Type,
        Category:      vs.Category,
        Format:        vs.Format,
        Label:         vs.Label,
        DisplayTarget: displayTargetFromString(vs.DisplayTarget),
        Source:        dp.Manifest.Name,
    }, dp.Manifest.Version)
    if regErr != nil {
        // Existing rollback path stays unchanged.
        ...
    }
}
```

The `if m.verbRegistry != nil` guard is removed. A nil registry is a
construction-time error (`INV-GW-10`).

### `BootstrapVerbRegistry` and `registerBuiltinTypes`

```go
// In internal/core/builtins.go (renamed file optional)

// BootstrapVerbRegistry returns a VerbRegistry seeded with host-owned
// event types. This is the single public path for obtaining a seeded
// registry in production. Use NewVerbRegistry() for tests that need an
// empty registry.
//
// hostVersion is the build-time version of the holomush core binary
// (e.g., "0.4.2-rc1" or "dev"). The bootstrapper records each builtin
// registration with source "builtin" and version "host-" + hostVersion
// so plugin-version drift is visible in events_audit replays.
func BootstrapVerbRegistry(hostVersion string) (*VerbRegistry, error) {
    r := NewVerbRegistry()
    if err := registerBuiltinTypes(r, hostVersion); err != nil {
        return nil, err
    }
    return r, nil
}

// registerBuiltinTypes (unexported) registers host-owned event types in
// the given registry. Called only by BootstrapVerbRegistry.
func registerBuiltinTypes(r *VerbRegistry, hostVersion string) error {
    sourceVersion := "host-" + hostVersion
    builtins := []VerbRegistration{
        // ... existing eight registrations from internal/core/builtins.go ...
    }
    for _, b := range builtins {
        if err := r.RegisterWithSource(b, sourceVersion); err != nil {
            return err
        }
    }
    return nil
}
```

### Core process bootstrap wiring

`cmd/holomush/core.go` (the core process entry point) is extended to:

1. Call `core.BootstrapVerbRegistry(version)` once during startup, passing the package-level `version` variable from `cmd/holomush/main.go` (`"dev"` in development, ldflags-injected at release).
2. Thread the registry into `pluginsetup.Subsystem` (so the plugin
   manager registers plugin verbs into it).
3. Construct `eventbus.RenderingPublisher{inner: realPublisher, registry: verbRegistry, validator: protoValidator}` and use it for all publisher-injection sites listed below.

This is **new wiring** — it's how the core-side registry comes into
existence in production for the first time. The current implementation
has zero production callers; both the gRPC server's and the plugin
manager's `WithVerbRegistry` options are unused.

### Publisher wiring sites — definitive list

The single `EventBus.Publisher()` call point is `cmd/holomush/sub_grpc.go:144`.
That value feeds two downstream consumers in production today, both of
which MUST receive the `RenderingPublisher`-wrapped publisher (not the
raw `JetStreamPublisher`):

| Consumer | Site | What flows through it |
|---|---|---|
| Plugin manager event emitter | `cmd/holomush/sub_grpc.go:149` (`pluginManager.ConfigureEventEmitter(publisher, …)`) | All plugin emits via `PluginEventEmitter.Emit` |
| Bus event appender | `cmd/holomush/sub_grpc.go:156-159` (`busEventAppender{publisher: publisher, …}` passed into `core.NewEngine(eventStore)`) | All host-direct emits (movement, location_state, exit_update, command_response, command_error, system) — i.e. host-owned event types from `core/builtins.go` |

If only one site is wrapped, the unwrapped path silently emits events with
nil `Rendering`, which the gateway will drop per `INV-GW-5`. Both must be
wrapped or the design is broken.

Acceptance criterion (added to Section 10): both production publisher
consumers receive the `RenderingPublisher`-wrapped publisher, verified by
a unit test that asserts the publisher passed to each constructor is of
type `*eventbus.RenderingPublisher`.

If new publisher consumers are added in future work, contributors must
remember to wire them through the wrapper. A doc note in
`site/docs/contributing/event-emit-pipeline.md` (added by Section 8)
calls this out.

### What changes on the gateway side

`cmd/holomush/gateway.go:285-289`:

```go
// Build the verb registry for event formatting (shared by web + telnet).
verbRegistry := core.NewVerbRegistry()
if regErr := core.RegisterBuiltinTypes(verbRegistry); regErr != nil {
    return oops.Code("VERB_REGISTRY_INIT_FAILED").Wrap(regErr)
}
```

is **deleted**. The `WithVerbRegistry` options on `web.NewHandler` and
the telnet handler constructor are removed. `internal/web/translate.go`
and `internal/telnet/gateway_handler.go` read off `ev.GetRendering()`
directly.

### Plugin reload semantics

When a plugin reloads with a tweaked verb (e.g., relabeled `say` from
"says" to "speaks"):

- Old events in `events_audit` keep their old `rendering.label = "says"`
  and old `source_plugin_version`.
- New emits get the new `rendering.label = "speaks"` with the new
  `source_plugin_version`.
- Operators inspecting history see both labels in the same scrollback
  — historically faithful, with the version field making drift visible.

SQL recipe (for the operator runbook):

```sql
SELECT rendering->>'source_plugin' AS plugin,
       rendering->>'source_plugin_version' AS version,
       rendering->>'label' AS label,
       count(*)
FROM events_audit
WHERE type = 'core-communication:say'
GROUP BY 1, 2, 3
ORDER BY count(*) DESC;
```

---

## Section 4 — Gateway thinness invariant

### RFC2119 statement

The gateway process MUST be limited to:

1. **Protocol translation** — telnet/ConnectRPC ↔ core gRPC.
2. **Connection management** — register, deregister, idle timeouts, TLS
   handshake, keepalive.
3. **Static asset serving** — the embedded web bundle.
4. **Reading rendering metadata off `EventFrame`** — and shaping it for
   the wire format the client expects.

The gateway MUST NOT:

- Import `internal/world`, `internal/access`, `internal/store`,
  `internal/plugin`, `internal/eventbus`, `internal/auth/service`, or
  `internal/command`.
- Maintain a `VerbRegistry`, `EventStore`, or any domain-aware cache.
- Translate event payloads using business rules (e.g., "if X then label
  as Y") — translation is purely structural (proto → JSON for web,
  proto → ANSI for telnet).

### Tripwire test

`cmd/holomush` is a single Go package (`package main`) housing both the
core process entry point (`core.go`) and the gateway entry point
(`gateway.go`). `core.go` legitimately imports every forbidden package on
the list; `golang.org/x/tools/go/packages` does not expose per-file
imports — only per-package imports — so we cannot enforce gateway-only
imports against `cmd/holomush` itself with `packages.Load`.

The tripwire uses a single mechanism — `packages.Load` with
`NeedName | NeedFiles | NeedSyntax | NeedImports | NeedTypes` — for both
`internal/web/...`, `internal/telnet/...`, and `cmd/holomush`. `NeedSyntax` returns per-file `*ast.File` values via
`pkg.Syntax`, which is what we need to enforce per-file imports inside
the single `package main` of `cmd/holomush`. No `parser.ParseFile`, no
filesystem path resolution, no path-relative-to-cwd issues.

The polarity is **default-deny for gateway files, allowlist for core**:
every `.go` file in `cmd/holomush` is treated as gateway-side and
checked against the forbidden list, **except** files explicitly named
in `coreOnlyFiles`. This way, adding a new gateway helper file is
automatically covered (no manual allowlist edit), while accidentally
adding a domain-aware import to an existing gateway file fails the test.

The test file lives in `cmd/holomush/gateway_imports_test.go` and uses
`package main` (matches every other test in `cmd/holomush`):

```go
//go:build !integration

package main

import (
    "go/ast"
    "path/filepath"
    "strings"
    "testing"

    "github.com/stretchr/testify/require"
    "golang.org/x/tools/go/packages"
)

// coreOnlyFiles are cmd/holomush files that legitimately import domain
// packages because they are part of the core process entry point, not
// the gateway. Every other .go file in cmd/holomush is treated as
// gateway-side and held to INV-GW-1.
//
// This list was last verified at the Phase 1.6 landing commit. New
// files added to cmd/holomush default to gateway-side enforcement;
// add an entry here only if the file legitimately needs domain imports
// for the core process and document why in the diff.
//
// Note: a few entries (core_test.go, sub_grpc_test.go, migrate_test.go)
// are listed preemptively even though they don't currently import
// forbidden packages — their non-test counterparts are core-only and
// these tests will naturally grow domain imports as core evolves.
var coreOnlyFiles = map[string]struct{}{
    // Core process bootstrap and dependency wiring.
    "core.go":                          {},
    "core_test.go":                     {},
    "deps.go":                          {},
    "deps_test.go":                     {},
    "sub_grpc.go":                      {},
    "sub_grpc_adapters_test.go":        {},
    "sub_grpc_test.go":                 {},
    // Database migration management (core-only).
    "automigrate_test.go":              {},
    "automigrate_integration_test.go":  {},
    "migrate.go":                       {},
    "migrate_test.go":                  {},
    // CLI subcommands that drive the core process (plugin events/validate).
    "cmd_plugin_events.go":             {},
    "cmd_plugin_validate.go":           {},
}

var forbidden = []string{
    "github.com/holomush/holomush/internal/world",
    "github.com/holomush/holomush/internal/access",
    "github.com/holomush/holomush/internal/store",
    "github.com/holomush/holomush/internal/plugin",
    "github.com/holomush/holomush/internal/eventbus",
    "github.com/holomush/holomush/internal/auth/service",
    "github.com/holomush/holomush/internal/command",
}

func TestGatewayImportsAreOnlyProtocolTranslation(t *testing.T) {
    pkgs, err := packages.Load(&packages.Config{
        Mode: packages.NeedName | packages.NeedFiles |
              packages.NeedSyntax | packages.NeedImports |
              packages.NeedTypes,
    },
        "github.com/holomush/holomush/cmd/holomush",
        "github.com/holomush/holomush/internal/web/...",
        "github.com/holomush/holomush/internal/telnet/...",
    )
    require.NoError(t, err)
    require.Empty(t, packages.PrintErrors(pkgs))

    for _, pkg := range pkgs {
        for _, file := range pkg.Syntax {
            // Use Fset.Position to derive the filename from the AST itself.
            // This is robust against pkg.Syntax / pkg.GoFiles index drift
            // under build-tag exclusions (Syntax aligns with CompiledGoFiles
            // and may be shorter than GoFiles).
            goFile := pkg.Fset.Position(file.Pos()).Filename
            checkFile(t, pkg.PkgPath, goFile, file)
        }
    }
}

func checkFile(t *testing.T, pkgPath, goFile string, file *ast.File) {
    base := filepath.Base(goFile)
    if pkgPath == "github.com/holomush/holomush/cmd/holomush" {
        if _, isCore := coreOnlyFiles[base]; isCore {
            return  // core-only file; skip
        }
    }
    for _, imp := range file.Imports {
        importPath := strings.Trim(imp.Path.Value, `"`)
        for _, bad := range forbidden {
            if importPath == bad || strings.HasPrefix(importPath, bad+"/") {
                t.Errorf("%s/%s imports forbidden domain package %s",
                    pkgPath, base, importPath)
            }
        }
    }
}
```

**File classification at landing (verified against repo state):**

| File | Status | Reason |
|---|---|---|
| `core.go`, `core_test.go`, `deps.go`, `deps_test.go`, `sub_grpc.go`, `sub_grpc_adapters_test.go`, `sub_grpc_test.go` | core-only | Core process bootstrap, gRPC server wiring |
| `automigrate_test.go`, `automigrate_integration_test.go`, `migrate.go`, `migrate_test.go` | core-only | DB migrations (need `internal/store`) |
| `cmd_plugin_events.go`, `cmd_plugin_validate.go` | core-only | CLI subcommands needing `internal/plugin` |
| `gateway.go`, `gateway_test.go` | gateway-side | The thing being protected |
| `cmd_plugin.go`, `cmd_plugin_test.go`, `cmd_plugin_events_test.go`, `cmd_plugin_validate_test.go` | gateway-clean | Tests + plugin parent command (no domain imports today) |
| `cert_poll.go`, `cert_poll_test.go` | gateway-clean | TLS plumbing shared by both |
| `main.go`, `main_test.go`, `root.go` | gateway-clean | Cobra root command, multi-call dispatch |
| `status.go`, `status_test.go`, `test_helper_test.go` | gateway-clean | No domain imports |

`golang.org/x/tools/go/packages` is already in `go.sum`. `go/ast` is
stdlib. We deliberately avoid `pkg.GoFiles[i]` / `pkg.Syntax[i]`
positional pairing because `pkg.Syntax` aligns with `pkg.CompiledGoFiles`
(which excludes build-tag-skipped files) and may be shorter than
`pkg.GoFiles`. Deriving the filename from the AST node's position via
`pkg.Fset.Position(file.Pos()).Filename` is index-free and robust.

The `packages.Load` mode bits include `NeedTypes` so that `Package.Fset`
is guaranteed populated per the documented contract — empirically `NeedSyntax`
populates it too, but the documented dependency is on `NeedTypes`.

`coreOnlyFiles` is the inverse allowlist: it's much smaller than the
gateway-files set and changes far less often (the core entry surface
is stable). A new file added to `cmd/holomush` defaults to
gateway-only enforcement; if the file legitimately needs domain
imports, the contributor adds it to `coreOnlyFiles` with a one-line
justification in the diff.

**Future option (filed as `holomush-k18g.9`):** split the gateway entry
point into its own command (e.g., `cmd/holomush-gateway/`), eliminating the
single-package overlap with the core entry point. That removes the need
for the per-file allowlist. Out of scope for Phase 1.6 because the binary
layout is currently a single multi-call binary; splitting it touches
build, packaging, and operator runbooks.

### Carve-outs

`internal/core` is allowed because the gateway needs proto types
(`corev1.EventFrame`, `corev1.RenderingMetadata`) and shared connection
primitives. The tripwire enforces "no domain packages," not "nothing
internal." If `internal/core` ever grows domain-aware code, that code
moves out before this rule changes.

### Where `EventChannel` lives matters

`corev1.EventChannel` becomes the canonical internal enum. `webv1.EventChannel`
continues to exist for the web wire format, kept value-for-value identical
by the enum-parity test (`INV-GW-16`). The gateway-side translator imports
`corev1` for the enum on the input side and converts to `webv1` only when
shaping `webv1.GameEvent`. This contains the change to the proto layer plus
the translator's boundary conversion.

---

## Section 5 — Host builtins

### Status quo retained, scoped down

Host-owned event types (`arrive`, `leave`, `move`, `location_state`,
`exit_update`, `command_response`, `command_error`, `system`) keep their
definitions in `internal/core/builtins.go`. They are emitted by core
itself, not by any plugin, and they need rendering metadata at emit
time exactly like plugin verbs.

### Function visibility changes

| Before | After |
|---|---|
| `RegisterBuiltinTypes(r *VerbRegistry) error` (public) | `registerBuiltinTypes(r *VerbRegistry) error` (private) |
| (no canonical seeded constructor) | `BootstrapVerbRegistry(hostVersion string) (*VerbRegistry, error)` (public) |
| `NewVerbRegistry() *VerbRegistry` (public) | unchanged |

### Test-call-site impact

| Test site | Today | After |
|---|---|---|
| `internal/core/registry_test.go` | calls `RegisterBuiltinTypes` (same package) | calls `registerBuiltinTypes` (same package — unexport is no problem) |
| `internal/plugin/manager_test.go` | `NewVerbRegistry()` for empty registry | unchanged |
| `internal/web/translate_test.go`, `internal/telnet/gateway_handler_test.go`, `cmd/holomush/gateway_test.go` | call `core.RegisterBuiltinTypes` | reworked: build `EventFrame` values with explicit `Rendering` sub-messages; do not touch the registry at all |
| `test/integration/plugin/verb_registration_test.go` | calls `core.RegisterBuiltinTypes` | calls `core.BootstrapVerbRegistry("test")` |

Net: post-rework, zero callers of the old exported `RegisterBuiltinTypes`
exist outside package `core`. Unexporting is clean.

### Plugin manager dependency tightening

`internal/plugin/manager.go`:

- `WithVerbRegistry(reg *core.VerbRegistry) ManagerOption` becomes
  required: a constructor-time check returns `ErrMissingVerbRegistry` if
  the registry is nil.
- The `if m.verbRegistry != nil` guard at line 783 is removed.

**Pre-flight survey (Plan task 0):** before tightening, the plan must
enumerate every `plugins.NewManager(...)` / `plugin.NewManager(...)`
construction site (production and test) and confirm each one passes a
registry. The survey covers at minimum:

- Production: `cmd/holomush/sub_grpc.go` and any other constructor
  invocation in `cmd/holomush`, `internal/plugin/setup`, and
  `internal/plugin/goplugin`.
- Tests: `internal/plugin/manager_test.go`,
  `internal/plugin/binary_plugin_test.go`,
  `internal/plugin/integration_test.go`,
  `test/integration/plugin/...`.

Any test site that currently relies on the nil-no-op behavior must be
updated before the tightening lands. **Choosing empty vs seeded depends
on what the test exercises:**

| Test pattern | What to pass | Reason |
|---|---|---|
| Pure manager construction / lifecycle (load, unload, dependency resolution) — no event emission | `core.NewVerbRegistry()` (empty) | Manager only needs a non-nil registry to register *its plugin's* declared verbs into. No emit path runs. |
| Event emission via `PluginEventEmitter.Emit` for any registered event type (e.g. `core-communication:say`) | `core.BootstrapVerbRegistry("test")` (seeded with builtins) **plus** explicit registration of plugin verbs the test exercises | After tightening, `RenderingPublisher.Publish` returns `EMIT_UNKNOWN_VERB` for un-registered types. Tests that emit must have the registry populated for the types being emitted. |
| Tests that load a plugin manifest and expect verb registrations to take effect | empty `core.NewVerbRegistry()`; the manager's plugin-load path will register verbs from the manifest into it | Manifest-driven registration is the production path; the registry starts empty and gets populated by load. |
| Tests that emit a host-owned event type (`arrive`, `leave`, `move`, `location_state`, `exit_update`, `command_response`, `command_error`, `system`) — directly or transitively (e.g. via session connect/disconnect) | `core.BootstrapVerbRegistry("test")` (seeded) | Builtins live only in `BootstrapVerbRegistry`; an empty registry will fail emit lookup with `EMIT_UNKNOWN_VERB`. |

**Default to seeded if uncertain.** Manifest-load tests that *only* exercise the manifest-declared verbs are the narrow case where empty is correct; any test path that triggers session lifecycle, command dispatch, or world events emits builtins and needs seeded.

The Plan Task 0 deliverable is a per-test-site decision: enumerate every
manager construction site, classify it, and apply the right registry
shape. `task pr-prep` is the gate after migration. Tightening lands in
the same PR as the migrations.

### Why not a host-pseudo-manifest now?

YAGNI. Folding host types into a synthetic plugin manifest would be a
~200-LOC refactor producing zero observable behavior change post-1.6.
Closing the gateway gap doesn't need it. Filed as `holomush-k18g.7` for
future consideration.

---

## Section 6 — Strict failure semantics

No production usage and no released versions mean there's no legacy
event population to handle. The protocol is being defined for the first
time. Consequently, all failure modes are strict.

### `RenderingPublisher.Publish` failure modes

| Failure | Behavior |
|---|---|
| `verbRegistry.Lookup(event.Type)` returns `(_, false)` | Return `EMIT_UNKNOWN_VERB` error (oops code). Caller propagates. |
| `protovalidate.Validate(ev)` fails on `Rendering` sub-message | Return `EMIT_VALIDATION_FAILED` error. Caller propagates. |
| `EventBus.Publish` fails | Existing emit-error path (unchanged). |

A loud failure on registry-miss or validation-fail is an actionable host
bug. There is no "lenient mode" — both tests and production fail the
same way.

### Gateway nil-rendering handling

`internal/web/translate.go` and `internal/telnet/gateway_handler.go`
treat `ev.GetRendering() == nil` as a contract violation. Behavior:

1. Drop the event (don't deliver to client).
2. Increment `holomush_gateway_dropped_nil_rendering_total{event_type=}`.
3. Log an error with `event.id`, `event.type`, and `event.stream`.

There is no fallback rendering. A nil `Rendering` reaching the gateway
means core's `RenderingPublisher.Publish` was bypassed somehow — that's a bug to
surface, not absorb.

### No legacy compatibility

- Pre-1.6 `events_audit` rows: do not exist (no production deployments
  at risk).
- Hot-tier JetStream messages mid-rollout: not a concern (no rolling
  updates over the cutover boundary).
- Misdeclared plugins from third parties: out of scope until production
  release; for now the manifest validator catches them at load time.

---

## Section 7 — Numbered invariants

Every protection claim has a numbered invariant, an RFC2119 statement,
a test type, and a test location. CI MUST fail if any of these tests
fail. A meta-test (`TestAllGatewayRegistryInvariantsHaveTests`)
enumerates IDs `INV-GW-1` through `INV-GW-11`, `INV-GW-3a`, `INV-GW-13`,
`INV-GW-14`, and `INV-GW-15` (skipping `INV-GW-12` until Phase 3) and
asserts each has at least one matching test by name. Adding an invariant
without adding a test fails CI. The exclusion of `INV-GW-12` is a
maintenance pointer: when Phase 3 lands, the exclusion list shrinks.

| ID | RFC2119 statement | Test type | Test location |
|---|---|---|---|
| `INV-GW-1` | The gateway process MUST NOT import `internal/world`, `internal/access`, `internal/store`, `internal/plugin`, `internal/eventbus`, `internal/auth/service`, or `internal/command`. | Static (import-graph) | `cmd/holomush/gateway_imports_test.go` |
| `INV-GW-2` | `RenderingPublisher.Publish` MUST stamp `event.Rendering` from the verb registry before publishing. | Unit | `internal/eventbus/rendering_publisher_test.go` |
| `INV-GW-3` | `RenderingPublisher.Publish` MUST return `EMIT_UNKNOWN_VERB` when the verb registry has no entry for `event.Type`. | Unit | `internal/eventbus/rendering_publisher_test.go` |
| `INV-GW-4` | `RenderingPublisher.Publish` MUST return `EMIT_VALIDATION_FAILED` when `protovalidate.Validate(ev)` fails on the stamped frame. | Unit | `internal/eventbus/rendering_publisher_test.go` |
| `INV-GW-5` | Gateway translation (web + telnet) MUST drop events with `Rendering == nil`, increment `holomush_gateway_dropped_nil_rendering_total`, and log an error. MUST NOT render fallback. | Unit | `internal/web/translate_test.go`, `internal/telnet/gateway_handler_test.go` |
| `INV-GW-6` | Every row in `events_audit` MUST have a non-nil `rendering` sub-message after a full E2E run. | Integration | `test/integration/eventbus_audit/rendering_completeness_test.go` |
| `INV-GW-7` | `RenderingMetadata.label` MUST be set when `format == "speech"`. Enforced at proto layer (CEL) and at `VerbRegistry.Register`. | Unit | `internal/core/registry_test.go`, generated protovalidate test |
| `INV-GW-8` | `RenderingMetadata.display_target` MUST NOT be `EVENT_CHANNEL_UNSPECIFIED`. Enforced at proto layer (`enum.not_in: [0]`). | Unit | generated protovalidate test |
| `INV-GW-9` | `RenderingMetadata.source_plugin` and `source_plugin_version` MUST be populated. For builtins, `source_plugin == "builtin"` and `source_plugin_version == "host-<binary version>"`. | Unit | `internal/eventbus/rendering_publisher_test.go` |
| `INV-GW-10` | The plugin manager MUST require a non-nil `VerbRegistry` at construction time. A nil registry returns `ErrMissingVerbRegistry`. | Unit | `internal/plugin/manager_test.go` |
| `INV-GW-11` | `BootstrapVerbRegistry()` MUST be the only public path that returns a registry seeded with host builtins. `RegisterBuiltinTypes` MUST be unexported. | Static (export check) | `internal/core/exports_test.go` |
| `INV-GW-12` | When `payload` is encrypted (Phase 3+), Phase 3 MUST extend `BuildAAD` (in `internal/eventbus/crypto/aad/`) to include a length-prefixed canonical encoding of `RenderingMetadata` AND bump the AAD version magic (`HMAAD\x01` → `HMAAD\x02`). Pre-1.6 events under identity codec do not exercise AAD, so the version bump is safe. **Trust model note:** AAD covers the `RenderingMetadata` *inside the proto envelope*, not the `App-Rendering` *NATS header*. The header transport for audit ingestion is trusted because the audit projection runs in-process with the publisher's JetStream-backing process; the threat model does not include a NATS-layer attacker who can rewrite headers without also rewriting the AEAD-protected envelope. If that threat model expands later, an audit-side cross-check (decode envelope, compare to header) becomes necessary; that change does not affect Phase 3 AAD design. | Forward-declared | Phase 3 codec tests |
| `INV-GW-3a` | `JetStreamPublisher.Publish` MUST copy `event.Rendering` into the `eventbusv1.Event.Rendering` proto field before `proto.Marshal`. Round-trip publish + JetStream consume MUST preserve `Rendering` byte-for-byte. | Unit | `internal/eventbus/publisher_test.go` |
| `INV-GW-13` | The audit projection writer MUST read the `App-Rendering` NATS header and write its JSON value into `events_audit.rendering`. The column is `NOT NULL`. A missing, empty, or malformed JSON header MUST fail the insert (PostgreSQL JSONB validation handles the malformed case naturally). | Integration | `test/integration/eventbus_audit/rendering_completeness_test.go` |
| `INV-GW-14` | The Go-side `eventbus.RenderingMetadata` struct and proto-side `corev1.RenderingMetadata` MUST stay in sync — same field set, same names. | Unit | `internal/eventbus/types_proto_sync_test.go` |
| `INV-GW-15` | For every event published through `RenderingPublisher`, the JSON value of the `App-Rendering` NATS header MUST encode the same `RenderingMetadata` as the `Rendering` field inside the proto envelope. The two transports cannot drift. | Unit | `internal/eventbus/rendering_publisher_test.go` |
| `INV-GW-16` | `corev1.EventChannel` and `webv1.EventChannel` MUST stay in lockstep — same enum values, same names, same numeric assignments. | Unit | `internal/web/translate_test.go` |

`INV-GW-12` is forward-declared and not enforced in this phase. Phase 3
of the crypto spec owns the AAD definition; this spec serves as the
hand-off marker.

---

## Section 8 — Documentation deliverables

Per the project's "spec invariants + docs as PR-blocking acceptance
criteria" rule, site/ docs ship in the same PR as the code, not as
follow-up.

| Doc | Audience | Path | What it says |
|---|---|---|---|
| Architecture diagram update | Contributors | `site/docs/contributing/architecture.md` | New diagram showing gateway-as-thin-translator + emit-time enrichment flow. Updates the existing "gateway boundary" section to reflect Approach B. |
| Gateway thinness rule | Contributors | `site/docs/contributing/gateway-boundary.md` (new) | Codifies `INV-GW-1` and the import allowlist. Includes the tripwire test reference and a "what the gateway is allowed to do" checklist. |
| Verb registry sourcing | Plugin authors | `site/docs/extending/verb-registration.md` (new or extended) | "How verb metadata gets from your manifest to the client." One-page explainer: manifest `verbs:` block → core registry → `RenderingPublisher.Publish` stamps `RenderingMetadata` → gateway reads it off the wire. |
| Operator note on plugin reloads | Operators | `site/docs/operating/plugin-reloads.md` (extended) | The historical-fidelity property: old events keep old labels, `source_plugin_version` makes drift visible. SQL snippet for inspecting version distribution in `events_audit`. |
| Event emit pipeline | Contributors | `site/docs/contributing/event-emit-pipeline.md` (new) | The publisher-wiring story: `EventBus.Publisher()` → `RenderingPublisher` → `JetStreamPublisher`, both `pluginManager` and `busEventAppender` consume the wrapped publisher, the rule that all new publisher consumers must wire through `RenderingPublisher`. |
| Proto reference | Plugin authors / SDK users | auto-generated | `RenderingMetadata` and `EventChannel` show up in the existing auto-generated proto reference. No manual write needed. |

No `CHANGELOG.md` entry — no production users, no released versions.

---

## Section 9 — Out of scope / non-goals

| Excluded | Why | Tracked as |
|---|---|---|
| `ListVerbs` RPC on `CoreService` | Approach B makes it unnecessary. The gateway has zero registry. The existing `holomush plugin events list/show` CLI runs in-process with core. | Not filed; raise if a concrete consumer appears. |
| Host-pseudo-manifest (delete `RegisterBuiltinTypes` entirely; treat host types as a synthetic plugin) | YAGNI for closing this gap. ~200-LOC refactor with zero observable behavior change post-1.6. | New bead `holomush-k18g.7` — *"Convert host-owned event types to a pseudo-plugin manifest."* P3. |
| Sweeping `protovalidate` annotations across existing `core/v1` fields (`EventFrame.id`, `type`, `actor_*`, etc.) | Existing fields are battle-tested without annotations; bolting them on now is unrelated to closing the gateway gap and risks scope creep. | New bead `holomush-k18g.8` — *"Annotate existing core/v1 fields with protovalidate."* P3. |
| Splitting gateway into its own `cmd/holomush-gateway/` binary | Out of scope; current binary is a single multi-call. Splitting touches build, packaging, and operator runbooks. | New bead `holomush-k18g.9` — *"Split gateway into its own command binary."* P3. |
| Removing `webv1.EventChannel` entirely | Out of scope; requires regenerating web client TS bindings and is cross-cutting beyond this gap. `corev1.EventChannel` becomes the canonical internal enum; both are kept in lockstep by `INV-GW-16`. | New bead `holomush-k18g.10` — *"Remove webv1.EventChannel after web TS bindings can be regenerated."* P3. |
| AAD coverage of `RenderingMetadata` for encrypted payloads | Phase 3 of the crypto spec owns the AAD canonicalization function. `INV-GW-12` specifies the contract Phase 3 must honor: extend `BuildAAD` to include length-prefixed canonical `RenderingMetadata` and bump the AAD magic version. | Crypto spec Phase 3. |
| Backwards compat for pre-1.6 `events_audit` rows | No production usage, no released versions. | Explicitly N/A. |
| Multi-host gateway TLS / network topology changes | Approach B is wire-format compatible across single-host and multi-host deployments. | N/A. |
| Web client (Svelte) changes | `webv1.GameEvent` already exposes the fields needed; only the gateway-side data source moves. | N/A. |
| Telnet client display changes | Telnet output is built from the same fields the gateway already uses, just sourced from `ev.Rendering`. No protocol change. | N/A. |
| `CHANGELOG.md` entry / migration notes | No production users. | N/A. |

---

## Section 10 — Acceptance criteria

This phase (1.6) is complete when **all** of the following hold:

1. `core.proto` defines `RenderingMetadata`, lifts `EventChannel` to
   `corev1`, and adds `EventFrame.rendering`. Generated Go is checked in.
2. `eventbus/v1/eventbus.proto` extends `Event` with
   `holomush.core.v1.RenderingMetadata rendering = 7;`.
3. `JetStreamPublisher.Publish` (`internal/eventbus/publisher.go`) is
   updated to copy `event.Rendering` into the proto envelope before
   `proto.Marshal`. Round-trip preservation tested under `INV-GW-3a`.
4. Migration `000012_events_audit_rendering` ships in
   `internal/store/migrations/`, adding `rendering JSONB NOT NULL`.
5. `RenderingPublisher` writes the `App-Rendering` entry into
   `event.Headers` (JSON encoding of `RenderingMetadata` via
   `protojson.Marshal` with `UseProtoNames: true, UseEnumNumbers: false`).
   `JetStreamPublisher.Publish` copies `event.Headers` into the
   outgoing `nats.Msg.Header` alongside the system-stamped headers.
6. The audit projection in `internal/eventbus/audit/projection.go`
   reads the `App-Rendering` header on every insert and writes its JSON
   value into `events_audit.rendering`. Missing header fails the insert.
7. `core.BootstrapVerbRegistry(hostVersion)` exists;
   `core.RegisterBuiltinTypes` is unexported as `registerBuiltinTypes`.
8. `eventbus.Event` (Go struct) gains a `Rendering *RenderingMetadata`
   field. A new `eventbus.RenderingPublisher` wraps the underlying
   `eventbus.Publisher` and performs lookup/stamp-on-event/stamp-header/
   validate/delegate.
9. `cmd/holomush/core.go` calls `BootstrapVerbRegistry(version)` once
   and threads the registry into a `RenderingPublisher` instance.
10. **Both** publisher consumers identified in Section 3 receive the
    `RenderingPublisher`-wrapped publisher: `pluginManager.ConfigureEventEmitter`
    at `cmd/holomush/sub_grpc.go:149` AND `busEventAppender.publisher` at
    `cmd/holomush/sub_grpc.go:156-159`. Verified by a unit test asserting
    the publisher passed to each constructor is `*eventbus.RenderingPublisher`.
11. `RenderingPublisher.Publish` performs registry lookup, stamps
    `Event.Rendering` and the `App-Rendering` header, runs
    `protovalidate.Validate` on the proto projection, and delegates —
    strict mode for both registry-miss and validation failures.
12. `cmd/holomush/gateway.go` no longer creates a `VerbRegistry`; the
    `WithVerbRegistry` options on `web.NewHandler` and the telnet
    handler are removed. The Prometheus metric registration at
    `gateway.go:263` (`command.CommandExecutions, command.CommandDuration,
    command.AliasExpansions`) is **deleted from `gateway.go`** — the
    same registration already exists at `core.go:237`, so a literal "move"
    would panic with `prometheus.AlreadyRegisteredError` on core startup.
    Verify the deletion is clean by `rg
    'command\.(CommandExecutions|CommandDuration|AliasExpansions)' cmd/holomush/`
    returning only `core.go`. After the deletion, `gateway.go` has no
    `internal/command` import.
13. `internal/web/translate.go` and `internal/telnet/gateway_handler.go`
    read off `ev.GetRendering()`; nil-rendering events are dropped with
    metric + log. Translator converts `corev1.EventChannel` →
    `webv1.EventChannel` at the `webv1.GameEvent` boundary.
14. The plugin manager rejects construction with a nil `VerbRegistry`,
    after the Plan Task 0 pre-flight survey migrates all current call
    sites.
15. The import-graph tripwire test (`INV-GW-1`) passes — covering
    `internal/web/...`, `internal/telnet/...`, and `cmd/holomush` via
    `packages.Load` with mode bits `NeedName | NeedFiles | NeedSyntax |
    NeedImports | NeedTypes`, using `coreOnlyFiles` as the inverse
    allowlist. `cmd/holomush/gateway.go` has zero forbidden imports
    (after the metric registration deletion per AC#12).
16. The meta-test `TestAllGatewayRegistryInvariantsHaveTests` passes for
    `INV-GW-1` through `INV-GW-11`, `INV-GW-3a`, `INV-GW-13`, `INV-GW-14`,
    `INV-GW-15`, and `INV-GW-16` (skipping `INV-GW-12` until Phase 3).
17. All six docs in Section 8 ship in the same PR.
18. `task pr-prep` is green — including the seven previously-failing
    E2E tests in `web/e2e/terminal.spec.ts`.
19. `holomush-k18g.5` is closed.

---

## References

- Crypto Phase 1 plan:
  [docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md](../plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md)
- Crypto design spec:
  [docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md](2026-04-25-event-payload-crypto-design.md)
- JetStream substrate spec:
  [docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md](2026-04-18-jetstream-event-log-design.md)
- Plugin boundary memory: `feedback_plugin_boundary`
- Spec invariants/docs as PR-blocking memory: `feedback_invariants_and_docs_as_spec_acceptance`
