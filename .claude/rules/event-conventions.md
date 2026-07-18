<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

---
paths:
  - "internal/eventbus/**"
  - "internal/core/**"
  - "**/*event*.go"
  - "plugins/**/*.go"
  - "plugins/**/*.lua"
---

# Event System Conventions

These conventions apply when emitting, declaring, or consuming events. The full design is in `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`.

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** in this document are to be interpreted as described in RFC 2119 and RFC 8174 (see the root `CLAUDE.md` "RFC2119 Keywords" table).

## Subject naming

Subjects are NATS dot-delimited:

```text
events.<game_id>.<domain>.<entity-id>[.<facet>...]
```

- `<domain>` is plugin-owned (e.g., `scene`) or host-owned (`location`, `character`, `session`)
- **Producers emit domain-relative dot references** (e.g. `location.<id>`, `character.<id>`,
  `scene.<id>.ic`); `eventbus.Qualify` prepends `events.<game_id>.` at the emit and
  read-entry boundaries. Classifiers, the bus, and the audit store see only
  fully-qualified dot subjects.
- **Colon-style subjects are eradicated** (`holomush-rops`). `internal/eventbus/subjectxlate/`
  is deleted. Do NOT use colon-style stream names anywhere. The only surviving
  colon usage is in ABAC policy DSL type-prefixes (`character:<id>` as a Cedar
  subject, `scene:<id>` as a resource ID) — those are correct and MUST NOT be
  changed to dot-style.

## Event identity vs ordering

| Concern | Owner |
|---------|-------|
| Identity / dedup key | `eventbus.Event.ID` (ULID) — set as `Nats-Msg-Id` for JetStream dedup |
| Ordering | JetStream's per-stream `uint64` sequence — **never** rely on ULID lex order |

## Event construction

- `eventbus.Event` is the single Event representation (ARCH-04 collapsed the former `core.Event`/`eventbus.Event` duplication)
- MUST use `eventbus.NewEvent()` — it stamps a monotonic ULID via `core.NewULID()`
- MUST NOT use `eventbus.Event{}` struct literals
- MUST NOT supply `Event.ID` manually (e.g., from `idgen.New()` which is for entity primary keys, not events)

## Plugin event types

- Plugin-owned event types (event constants, verb registrations) belong in the plugin package — NEVER in `internal/core/`
- Host-side ABAC infra (e.g., `ResourceChannel`, channel resource type, action validations) IS allowed in `internal/`
- Rationale: the boundary keeps `internal/core/` independent of any specific plugin's event vocabulary. Plugin authors don't need to modify `internal/` to add new event types or verbs.

## Event-type vocabularies (qualified wire / bare crypto)

An event type appears in three places, governed differently (INV-PLUGIN-40):

- **Wire type + `verbs[].type` + downstream stored type** MUST be plugin-qualified
  `<plugin>:<verb>` (one colon, non-empty verb). `RenderingPublisher.Lookup`
  resolves the wire type against `verbs[].type` and hard-fails `EMIT_UNKNOWN_VERB`
  on a miss. `Manifest.Validate()` rejects an unqualified `verbs[].type` at
  discovery with `PLUGIN_WIRE_TYPE_NOT_QUALIFIED`.
- **Registered-emit set (`RegisterEmitTypes` / `register_emit_type`) and
  `crypto.emits[].event_type`** stay **bare** `<verb>`. INV-PLUGIN-32 forces the
  two set-equal; `requests_decryption` refs are `<plugin>:<verb>` and
  `splitQualifiedRef` recovers the bare verb. Qualifying these trips
  `EVENT_TYPE_REGISTRY_MISMATCH` at load.
- `emitEntryMatchesWireType` (`internal/plugin/crypto_manifest.go`) is the single
  bridge: it matches a bare `crypto.emits` entry against the qualified wire type
  by composing the plugin name. Do not add other bare↔qualified shims.

## Emitting from plugins

- Lua: return events from handler functions; the host translates and publishes
- Binary: gRPC `EmitEvent` RPC. Both go through `internal/plugin/event_emitter.go::Emit` which enforces manifest gates (`actor_kinds_claimable`, `emits`, `crypto.emits`) for both runtimes
- See `.claude/rules/plugin-runtime-symmetry.md` for the symmetry invariant

## Audit and history

- Host-owned subjects audit to `events_audit` (PostgreSQL)
- Plugin-owned subjects audit to plugin-declared tables (e.g., `plugin_core_scenes.scene_log`) via `PluginAuditService.AuditEvent`
- `HistoryReader.QueryHistory` falls back from JetStream (recent) to PostgreSQL (older than JS retention) transparently — callers don't see the boundary

## Sensitive payloads

- If a payload is sensitive, the plugin MUST declare the event type in `crypto.emits` in its `plugin.yaml`
- Sensitive events get a per-event DEK; AAD bind to event ID + subject
- The crypto-reviewer agent gates any change to `crypto.emits` declarations
