# Event Emit Pipeline

This document describes how events flow from a plugin (or host code) through
the publisher chain, onto JetStream, and ultimately to the audit log and
subscribers.

## Publisher chain

```text
plugin/host emit site
    ↓
RenderingPublisher.Publish()      ← single enrichment site
    ↓
JetStreamPublisher.Publish()      ← NATS transport
    ↓
NATS JetStream (EVENTS stream)
    ↓
audit projection → events_audit   ← PostgreSQL forever-archive
    ↓
subscribers (gRPC Subscribe)
```

The chain is assembled at boot in `cmd/holomush/sub_grpc.go` via
`(*grpcSubsystem).wrapPublisher`:

```go
rawPublisher := s.cfg.EventBus.Publisher()
publisher, err := s.wrapPublisher(rawPublisher)
// publisher is *eventbus.RenderingPublisher wrapping a *JetStreamPublisher
```

Both consumers — `pluginManager.ConfigureEventEmitter` and `busEventAppender`
— receive the wrapped publisher. Any new publisher consumer MUST receive
the wrapped publisher, never the raw one.

## What RenderingPublisher does

On every `Publish` call, `RenderingPublisher`:

1. Looks up `event.Type` in the `VerbRegistry` (seeded by `BootstrapVerbRegistry`
   at boot + plugin verbs from manifests).
2. Returns `EMIT_UNKNOWN_VERB` if the type is not registered — unknown types
   are a publisher contract violation.
3. Stamps `event.Rendering` with category, format, label, display target,
   and source plugin + version.
4. Marshals the proto form as protojson and stamps `event.Headers["App-Rendering"]`.
5. Runs `protovalidate` against the rendering proto.
6. Delegates to the underlying publisher.

## Two transports for RenderingMetadata

`RenderingMetadata` travels on two parallel channels per event:

| Transport | Field | Consumer |
|---|---|---|
| JetStream message header | `App-Rendering` (protojson) | Audit projection (avoids proto decode) |
| Proto envelope field | `eventbusv1.Event.rendering` | Subscribers (decoded from proto) |

Both carry the same value. `INV-GW-15` enforces parity via round-trip test.

The audit projection (`internal/eventbus/audit/projection.go`) reads the
header and writes it to `events_audit.rendering` (JSONB NOT NULL). This
avoids decoding the full proto envelope in the hot-path audit consumer.

## Historical fidelity

Each event is stamped with the rendering metadata in effect at emit time.
`source_plugin_version` records the plugin version that declared the verb.
After a plugin reload with a changed verb, old events keep their original
rendering; new events carry the new version. See
`site/docs/operating/plugin-reloads.md`.

## See also

- Gateway boundary: `site/docs/contributing/gateway-boundary.md`
- Verb registration: `site/docs/extending/verb-registration.md`
- Spec: `docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md`
