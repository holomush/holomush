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

## Subject naming

Subjects are NATS dot-delimited:

```text
events.<game_id>.<domain>.<entity-id>[.<facet>...]
```

- `<domain>` is plugin-owned (e.g., `scene`) or host-owned (`location`, `character`, `session`)
- Legacy colon-style subjects (`scene:01ABC`) are translated at the EventSink boundary by `internal/eventbus/subjectxlate/` — do NOT emit colon style from new code

## Event identity vs ordering

| Concern | Owner |
|---------|-------|
| Identity / dedup key | `core.Event.ID` (ULID) — set as `Nats-Msg-Id` for JetStream dedup |
| Ordering | JetStream's per-stream `uint64` sequence — **never** rely on ULID lex order |

## Event construction

- MUST use `core.NewEvent()` — it stamps a monotonic ULID via `core.NewULID()`
- MUST NOT use `core.Event{}` struct literals
- MUST NOT supply `Event.ID` manually (e.g., from `idgen.New()` which is for entity primary keys, not events)

## Plugin event types

- Plugin-owned event types (event constants, verb registrations) belong in the plugin package — NEVER in `internal/core/`
- Host-side ABAC infra (e.g., `ResourceChannel`, channel resource type, action validations) IS allowed in `internal/`
- Rationale: the boundary keeps `internal/core/` independent of any specific plugin's event vocabulary. Plugin authors don't need to modify `internal/` to add new event types or verbs.

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
