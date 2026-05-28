---
title: "Event Emit Pipeline"
---

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

## Plugin-owned history read-back

Plugin-owned event subjects with `sensitivity:always` are stored as ciphertext
in the plugin's audit table. The `PluginDowngradeFence` (`internal/eventbus/history/plugin_downgrade_fence.go`)
gates every routed history read: rows that fail INV-P7-7 (downgrade detected)
or INV-P7-15 (DEK missing) are refused with a metadata-only frame.

**Fence-contract change (read-back decrypt):** Clean rows that pass both
INV-P7-7 and INV-P7-15 now flow through the shared `fenceCheckRow` primitive
(`internal/eventbus/history`) and are **decrypted before delivery**, not passed
through as ciphertext. The same `fenceCheckRow` primitive is also the entry
point for the snapshot direct-decrypt path (`DecryptOwnAuditRows`), so the
per-row fence semantics are identical on both the routed fence path and the
direct host RPC. Any change to `fenceCheckRow` must be evaluated for both
consumers.

Authorization for the direct-decrypt path requires two gates:

- **g1 — OwnerMap subject ownership:** only the plugin that owns the subject
  can request decryption of its rows.
- **g2 — Manifest `readback` flag:** the plugin must declare
  `crypto.emits[].readback: true` for the event type (default-deny; invalid on
  `sensitivity:never` types).

Every successful row decrypt emits an INV-19 `audit:plugin_decrypt` record.
The primitive fails closed: a missing audit emitter returns a refusal rather
than leaking plaintext.

See `site/docs/extending/plugin-crypto-readback.md` for the plugin-author view,
and `docs/superpowers/specs/2026-05-25-plugin-readback-decrypt-design.md` for
the full design rationale and invariant table (INV-RB-1 through INV-RB-12).

## See also

- Gateway boundary: `site/docs/contributing/gateway-boundary.md`
- Verb registration: `site/docs/extending/verb-registration.md`
- Spec: `docs/superpowers/specs/2026-04-26-gateway-verb-registry-sourcing.md`
