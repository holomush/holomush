<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Audit Subsystem Rework — Source-Agnostic Facility + Plugin Emit Path

**Status:** Draft
**Date:** 2026-04-07
**Author:** seanb4t (via Claude Opus 4.6)
**Bead:** `holomush-ggbz`
**Blocks:** `holomush-0sc.12` (channel plugin rework)
**Related:**

- `docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md` — plugin ABAC hardening that made handler-side decision auditing feasible
- `docs/specs/2026-04-03-channels-architecture.md` — the immediate downstream consumer
- `docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md` — plugin trust boundary this spec extends

## RFC2119 Keywords

| Keyword        | Meaning                                    |
| -------------- | ------------------------------------------ |
| **MUST**       | Absolute requirement                       |
| **MUST NOT**   | Absolute prohibition                       |
| **SHOULD**     | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended                            |
| **MAY**        | Optional                                   |

## Problem

The audit subsystem currently records authorization decisions for exactly one source: the ABAC policy engine. This is baked into the package name (`internal/access/policy/audit/`), the type shape (`Entry` with `PolicyID`/`PolicyName` fields), and the emit discipline (only the engine calls `auditLogger.Log`). Any authorization decision made outside the engine is invisible to the audit log.

This is a problem for any plugin that does handler-side state checks. The canonical example is the channels plugin rework (`holomush-0sc.12`): under the "Option C" hybrid architecture, some denials (membership, ban, mute) live in plugin handler code because they require point lookups the ABAC attribute resolver cannot make efficiently. Those denials never touch the policy engine — and therefore never appear in the audit log.

The immediate options are unsatisfying:

- **Silent handler denials** — accept the audit gap, document it. Ops lose the ability to answer "why was character X denied on channel Y" from a single audit trail.
- **Fabricate audit entries from inside the plugin** — impossible for binary plugins, which are separate processes with no in-memory access to `audit.Logger`. Even for in-process Lua plugins, the engine-shaped `Entry` forces plugins to lie about `PolicyID` / `PolicyName`.
- **Structured slog emit from plugin handlers** — loses WAL durability, sync guarantees, mode routing, and creates a second audit trail operators must query independently.

The correct answer is to stop treating the audit subsystem as policy-engine-owned. Any authorization source — the ABAC engine today, plugins today, future subsystems tomorrow — MUST be able to emit audit entries through the same facility, with the same durability guarantees and the same mode routing, while remaining trustworthy (no source can claim to be a different source, no plugin can claim decisions against arbitrary subjects).

### Constraints

The rework MUST satisfy several non-negotiable properties:

| Constraint | Reason |
|---|---|
| `internal/audit/` MUST NOT import anything plugin-specific | Plugins must not leak into core; core MAY know plugins exist as a category, but core MUST NOT know about specific plugins |
| Existing `audit.Logger` durability semantics (WAL fallback, sync writes for denials, async writes for allows, partial replay) MUST be preserved | Ops depends on these; regressing would be worse than the current state |
| No release has shipped yet | Field renames and package moves are acceptable; this is a one-time opportunity to lock in the right shape |
| Lua plugins MUST be supported from day one | 6 of 8 current code plugins are Lua; shipping binary-only would miss the majority of production plugins |
| Binary plugin path MUST NOT require a host↔plugin round-trip per audit emit | Audit emits may be frequent; an extra RPC per denial would materially impact dispatcher latency |
| The trust boundary MUST NOT allow a plugin to claim it is another plugin, to claim it is the engine, or to claim decisions against subjects it is not currently handling | This is the core integrity property of the audit log |

## Decisions

### Decision A: Package move + type rename

`internal/access/policy/audit/` moves to `internal/audit/`. The package stops being policy-engine-owned and becomes a general-purpose decision-recording facility. The type formerly known as `Entry` becomes `Event`, matching the new package role ("record an event that has occurred" rather than "record a policy evaluation entry").

**Alternatives considered:**

| Option | Description | Rejected because |
|---|---|---|
| Keep the package path | `internal/access/policy/audit/` unchanged | Signals that audit is policy-owned; misleads future contributors into adding policy-specific assumptions |
| Rename in place (new fields, old package) | Add `Source`/`Component`/`Message` without moving | Leaves the path lie in place; the constraint violation ("plugins must not leak into core" implied by a policy-owned package that knows about plugins) persists |
| Type rename: `Decision` instead of `Event` | More literal — it IS a decision | `Event` is consistent with the broader event-sourcing vocabulary used elsewhere in HoloMUSH; reads well without stutter (`audit.Event.ID`) |

### Decision B: Source is a defined-string-type category, not an enum

The package gains a `RationaleCategory`-shaped discriminator field. After considering typed closed-set `Effect`-style `iota` enum, free-form `string`, and `type Source string` (defined type), the chosen option is a **defined string type** with package-level constants:

```go
type EventSource string

const (
    SourceEngine EventSource = "engine"
    SourcePlugin EventSource = "plugin"
    SourceSystem EventSource = "system"
)
```

Rationale: nothing in the audit package switches on `Source` to change behavior — mode routing uses `Effect`, WAL routing uses `Effect`. `Source` exists purely for record-keeping and operator queries. A closed-enum would force us to update a switch table every time a new source is added without producing any type-level correctness gain. A bare `string` loses call-site documentation at function signatures. A defined type preserves self-documenting signatures, enables future method attachment (e.g., `IsValid()`), and doesn't constrain extension.

**Explicitly rejected:** adding `EffectPluginDeny` or any other new `Effect` value. Plugin denials are semantically `EffectDeny` — they *are* denials. The four existing effect values (`EffectDefaultDeny`, `EffectAllow`, `EffectDeny`, `EffectSystemBypass`) cover every authorization decision the system can make. Adding a fifth to encode "the denial came from somewhere other than the engine" confuses effect semantics with emitter identity. The discriminator is `Source`.

### Decision C: Component field for subsystem-level granularity

`Source` identifies the *kind* of emitter. `Component` identifies the *specific* emitter within that kind. For engine entries, `Component = "abac"`. For plugin entries, `Component = "core-channels"` or whichever specific plugin name. For system entries, `Component = "session-reaper"` or similar.

The split lets operators query at two granularities:

- "All plugin denials": `WHERE source = 'plugin'`
- "All denials from core-channels specifically": `WHERE source = 'plugin' AND component = 'core-channels'`

Without `Component`, the plugin name would have to live in the free-form `Attributes` map (or be parsed out of a magic prefix in `ID`), both of which are worse for operator ergonomics.

The audit package does not know or care that `"core-channels"` is a plugin name — it stores the string and makes no semantic claims about it. The plugin-specific knowledge lives in the dispatcher (which stamps `Component` from the authenticated plugin identity) and in operator queries.

### Decision D: New `Message` field for per-firing description

Existing `PolicyID`/`PolicyName` are static per-rule: every firing of the same policy carries identical `PolicyID` and `PolicyName`. For policy denials this is fine because the policy's text is the explanation.

For plugin handler denials, the *explanation* is often context-dependent: "player not in channel members (channel is public)" vs "player is banned from this channel" are different runtime paths in the same handler. The plugin SHOULD be able to attach a per-firing human-readable description that the audit log preserves.

The new `Message` field is this per-firing slot. Engine entries MAY leave it empty (or populate it with policy evaluation context). Plugin entries SHOULD populate it with the same human-readable explanation the user sees in the error response, so the audit log and the user's experience stay aligned.

### Decision E: `Event` shape (final)

```go
package audit

type EventSource string

const (
    SourceEngine EventSource = "engine"
    SourcePlugin EventSource = "plugin"
    SourceSystem EventSource = "system"
)

type Event struct {
    // Identity of the decision
    ID        string      // stable slug: "permit-write-own-room", "not_member"
    Name      string      // human label: "Write to Own Room"
    Message   string      // per-firing description: "player not in channel members"
    Source    EventSource // kind of emitter
    Component string      // subsystem within source: "core-channels", "abac"

    // What was attempted
    Subject  string
    Action   string
    Resource string
    Effect   types.Effect // unchanged: Allow/Deny/DefaultDeny/SystemBypass

    // Context
    Attributes map[string]any
    DurationUS int64
    Timestamp  time.Time
}
```

`Writer` interface and `Logger` semantics (mode routing, WAL fallback, sync/async paths, partial replay) are unchanged except that methods now accept `Event` instead of `Entry`.

### Decision F: D-inline transport for binary plugin emits

Binary plugins emit audit hints by piggybacking on the existing `CommandResponse` return channel rather than by making a separate host RPC per emit.

**The proto change:** add `repeated AuditDecisionHint audit_hints = N;` to `pluginv1.CommandResponse`, plus a new `AuditDecisionHint` message:

```proto
message AuditDecisionHint {
  string id                = 1;  // plugin's rule slug
  string name              = 2;  // human label for the rule
  string message           = 3;  // per-firing description
  string effect            = 4;  // "deny" or "allow"
  string action_qualifier  = 5;  // appended to host-stamped base action
  string resource          = 6;  // plugin-provided, host-validated
  map<string, AttributeValue> attributes = 7;  // plugin-provided context
}
```

**The SDK flow:**

1. Plugin handler calls `pluginsdk.Audit(ctx).Deny(id, message, attrs...)`
2. SDK pushes a hint onto an in-process context-bound slice (`context.WithValue` key `auditHintsKey`)
3. When the handler returns, SDK harvests the slice and serializes it into `CommandResponse.audit_hints`
4. Dispatcher receives the response on the host side, extracts hints, and routes them as described in Decision G

**Alternatives considered:**

| Option | Description | Rejected because |
|---|---|---|
| **D-rpc** — Separate `WriteAuditEntry` RPC on `HostFunctionsService` | Plugin makes one gRPC call per emit | Extra round-trip per audit emit; requires standing up the first concrete `HostFunctionsServiceServer` from scratch; more implementation surface |
| **Hybrid** — both D-inline and D-rpc | Inline for command flow, RPC for deferred | YAGNI; nothing in scope requires deferred audit emits |

D-rpc is not closed off — the SDK exposes `pluginsdk.Audit(ctx)` as an interface abstraction, so a future D-rpc implementation can slot in without changing how plugin authors call it. A plugin that requires deferred audit (e.g., a scene auto-archival timer) would use D-rpc later; plugins that emit during command flow keep using D-inline.

### Decision G: Dispatcher-side hint collection + flush

The dispatcher owns a context-bound `[]audit.Event` slice for each in-progress command dispatch. Every emit path converges at this slice; a single flush step drains it to `auditLogger.Log`.

**Flow:**

1. Before dispatching, the dispatcher attaches an empty `*[]audit.Event` to the per-dispatch context via `context.WithValue`.
2. During dispatch:
   - Binary plugin path: dispatcher calls `pluginDeliverer.DeliverCommand`, which returns a `CommandResponse` with `audit_hints`. The dispatcher extracts each hint, stamps the host-controlled fields (see Decision H), and pushes the resulting `audit.Event` onto the context-bound slice.
   - Lua plugin path: the Lua `audit` capability (see Decision I) reads the dispatcher context off the `LState`, constructs an `audit.Event`, and pushes it directly onto the slice during handler execution — no proto round-trip because Lua runs in the host process.
   - Engine path: the engine's existing `auditLogger.Log` call is preserved as-is. Engine emits do NOT go through the dispatcher's context-bound slice — they flush eagerly inside the engine, same as today.
3. After dispatching completes (handler returned, response ready to send to user), the dispatcher drains the context-bound slice. For each event, it calls `auditLogger.Log(ctx, event)`. Errors are handled per Decision J.

The dispatcher NEVER inspects hint contents for authorization purposes. It treats hints as opaque records to be routed to the audit logger after stamping identity fields.

### Decision H: Trust boundary — what plugin provides vs what host stamps

For each field on `audit.Event`, the dispatcher either trusts the plugin's provided value, stamps its own, or composes the two:

| Field | Source | Notes |
|---|---|---|
| `Subject` | Host-stamped from dispatch context | Plugin cannot claim decisions against characters other than the one currently being dispatched |
| `Action` | Host-stamped base + plugin-provided qualifier | Host has the command name; plugin appends a sub-action (e.g., `"speak"` becomes `"channel:speak"`) via a `:` separator |
| `Resource` | Plugin-provided, host-validated shape | Only the plugin knows the specific instance; host validates the `<type>:<id>` form but trusts the value |
| `Effect` | Plugin-provided | Plugin decides `deny` / `allow` |
| `Source` | Host-stamped | Always `SourcePlugin` for inline hints; plugin cannot spoof |
| `Component` | Host-stamped from authenticated plugin identity | Host knows which plugin just returned the response; plugin cannot spoof |
| `ID`, `Name`, `Message` | Plugin-provided | Plugin's internal vocabulary for its rules |
| `Attributes` | Plugin-provided + host-overlay | Host merges dispatch-context keys (e.g., `command.invoked_as`) on top; host keys win on collision; convention: plugin keys are namespaced (e.g., `"channel.type"`) to avoid collisions |
| `DurationUS` | Host-measured | Host clock, from handler invocation to hint processing |
| `Timestamp` | Host-stamped | Host clock authoritative |

**Explicit trust gap:** `Resource` is plugin-provided. A misbehaving plugin could claim a denial against a resource it does not own (e.g., channels plugin claiming a denial for `location:01XYZ`). This is an accepted gap. Mitigation: the `Component` field makes it obvious in audit-log queries that the denial came from that specific plugin, so cross-type claims are visible. A tighter boundary (host validates `Resource` type against the plugin's declared `resource_types`) is a plausible follow-up but out of scope for v1.

### Decision I: Lua capability module

Lua plugins emit audit hints via a new `internal/plugin/hostfunc/cap_audit.go` `Capability` module that injects an `audit` global into the Lua state at plugin load:

```lua
audit.deny("not_member", "player not in channel members", {channel_type = "public"})
audit.allow("speak", "message delivered", {channel_id = "01XYZ"})
```

The Go function behind the Lua global:

1. Validates argument types (string, string, optional table)
2. Converts the Lua attributes table to `map[string]any` via the existing `luaTableToMap` helpers
3. Reads the dispatcher context from the `LState` via gopher-lua's `L.Context()` method
4. Constructs an `audit.Event` with host-stamped fields (`Source = SourcePlugin`, `Component = pluginName`, etc.) and the plugin-provided fields
5. Pushes the event onto the context-bound slice — the same slice the binary plugin path uses

**Prerequisite verification during implementation:** the dispatcher MUST attach the per-invocation context to the `LState` via `L.SetContext(ctx)` before calling the Lua handler, so that `L.Context()` returns the right context when the Lua capability runs. If the Lua host does not currently do this, wiring it is a small additional task on the implementation plan.

The capability is registered at plugin load time based on the plugin's manifest `requires:` declaration (consistent with how other Lua capabilities like `session`, `alias`, `world_query` are gated). The service name is `holomush.plugin.v1.AuditService` for symmetry with other capability service names.

### Decision J: Failure mode — log, metric, continue

Audit write failures in the dispatcher's flush step follow the pattern the engine already uses:

1. `auditLogger.Log(ctx, event)` returns an error
2. Dispatcher increments a new `audit.RecordPluginAuditFailure()` Prometheus counter (mirrors the existing `audit.RecordEngineAuditFailure()`)
3. Dispatcher logs the failure via `slog.Error` with event context
4. Dispatcher continues processing remaining events in the slice
5. The user receives their command response normally, independent of audit pipeline state

The user's command is NEVER failed because of an audit infrastructure hiccup. Rationale: the authorization decision has already been made (and enforced — the user either saw their success response or saw the handler's error message); failing the response after the fact because the audit DB had a bad moment creates correlated user-visible failures every time the audit pipeline degrades. The existing WAL fallback in `audit.Logger.writeSync` catches transient DB failures before they become audit-pipeline failures; only if BOTH the DB and the WAL fail does the error reach the dispatcher.

## Architecture

### Package layout after rework

```text
internal/audit/                    # moved from internal/access/policy/audit/
├── logger.go                       # Logger, Writer, Mode, Event type
├── logger_test.go
├── writer_postgres.go              # existing postgres writer, updated
├── writer_postgres_test.go
└── replay.go                       # WAL replay (unchanged semantics)

internal/access/policy/             # policy engine keeps importing audit
└── engine.go                       # call sites updated to new field names

internal/command/
└── dispatcher.go                   # new: context-bound hint slice + flush logic

internal/plugin/
└── hostfunc/
    ├── cap_audit.go                # NEW: Lua audit capability
    └── cap_audit_test.go

pkg/plugin/                         # binary plugin SDK
├── audit.go                        # NEW: Audit(ctx) recorder, AuditAttrs helper
├── audit_test.go
└── sdk.go                          # updated to serialize hints into CommandResponse

api/proto/holomush/plugin/v1/
└── plugin.proto                    # CommandResponse gains audit_hints field
```

### Call-site changes summary

| Location | Change |
|---|---|
| `internal/access/policy/audit/` (entire package) | Moved to `internal/audit/`; imports throughout the codebase updated |
| `internal/audit/logger.go` | `Entry` → `Event`, `PolicyID` → `ID`, `PolicyName` → `Name`, new fields (`Message`, `Source`, `Component`), new `EventSource` defined type + constants |
| `internal/access/policy/engine.go` | Call sites constructing audit entries updated to new field names; `Source = SourceEngine`, `Component = "abac"` stamped on every engine emit |
| `internal/command/dispatcher.go` | New context-bound hint collection; new flush step after dispatch; host field stamping for extracted hints |
| `pkg/plugin/audit.go` (new) | `Audit(ctx) AuditRecorder` API; `AuditRecorder.Deny/Allow`; context-bound slice storage; serialization helper |
| `pkg/plugin/sdk.go` | `HandleCommand` flow: after user handler returns, SDK harvests context-bound hints and attaches them to `CommandResponse.audit_hints` |
| `api/proto/holomush/plugin/v1/plugin.proto` | `CommandResponse` gains `repeated AuditDecisionHint audit_hints` field; new `AuditDecisionHint` message |
| `internal/plugin/hostfunc/cap_audit.go` (new) | Lua capability registering the `audit` global; handles type conversion + context lookup |
| `internal/plugin/manager.go` | Register the new capability with the existing `CapabilityRegistry` |
| Database | Migration to rename `policy_id` / `policy_name` columns on the audit log table; add `source`, `component`, `message` columns |

## Test Plan

Tests are named per the ACE framework (Action + Condition + Expectation).

### Layer 1 — `internal/audit/` unit tests

| ID | Name | What it proves |
|---|---|---|
| T1 | `TestEventZeroValueHasEmptyFields` | Zero-value Event is inert |
| T2 | `TestEventSourceConstantsHaveExpectedValues` | Constants match spec |
| T3 | `TestLoggerRoutesPluginDenialViaSyncWriter` | Plugin-sourced denials flow through the same sync path as engine denials |
| T4 | `TestLoggerRoutesPluginAllowViaAsyncWriterInModeAll` | Plugin-sourced allows flow through async path in `ModeAll` |
| T5 | `TestLoggerIgnoresPluginAllowInModeDenialsOnly` | Mode routing honors source-independent effect semantics |
| T6 | `TestLoggerFallsBackToWALWhenPluginDenialDBWriteFails` | WAL fallback applies to plugin entries identically to engine entries |
| T7 | `TestReplayWALRestoresPluginEventsWithAllFields` | New fields survive WAL marshal/unmarshal cycle |
| T8 | `TestEventMarshalsAttributesMapAsJSONObject` | Per-entry context map round-trips |
| T9 | `TestLoggerPreservesDurationAndTimestampInPluginEvents` | Timing fields are not dropped |
| T10 | `TestExistingEngineAuditPathUnchangedAfterRename` | Regression coverage — engine emits still work |

### Layer 2 — `internal/command/dispatcher/` unit tests

| ID | Name | What it proves |
|---|---|---|
| T11 | `TestDispatcherAttachesEmptyHintSliceToDispatchContext` | Setup is correct |
| T12 | `TestDispatcherExtractsHintsFromCommandResponse` | Binary path wire format |
| T13 | `TestDispatcherStampsSourceAsPluginOnExtractedHints` | Anti-spoofing: Source is host-set |
| T14 | `TestDispatcherStampsComponentFromPluginIdentity` | Anti-spoofing: Component is host-set |
| T15 | `TestDispatcherStampsSubjectFromDispatchContext` | Anti-spoofing: Subject is host-set |
| T16 | `TestDispatcherComposesActionAsBaseColonQualifier` | Action composition rule |
| T17 | `TestDispatcherValidatesResourceFormat` | `<type>:<id>` shape check |
| T18 | `TestDispatcherMergesHostAttributesOverPluginAttributes` | Host keys win on collision |
| T19 | `TestDispatcherFlushesHintsToAuditLoggerAfterDispatch` | End-to-end flush |
| T20 | `TestDispatcherContinuesFlushingRemainingHintsWhenOneFails` | Per-hint failure isolation |
| T21 | `TestDispatcherBumpsFailureCounterOnAuditWriteError` | Metric coverage |
| T22 | `TestDispatcherReturnsUserResponseEvenWhenAuditFlushFails` | Failure mode |
| T22a | `TestDispatcherSkipsHintWithUnknownEffectStringAndLogsWarning` | MUST — negative: unknown effect value (not "deny" or "allow") |
| T22b | `TestDispatcherValidatesMalformedResourceRefs` (table-driven: empty, no colon, `"type:"`, `":id"`, multi-colon, whitespace) | SHOULD — boundary: resource format |
| T22c | `TestDispatcherFlushIsNoOpWhenAuditLoggerIsNil` | SHOULD — boundary: no logger configured |
| T22d | `TestDispatcherConcurrentDispatchesDoNotCrossContaminateAuditContexts` | SHOULD — invariant: context-per-dispatch isolation |

### Layer 3 — `pkg/plugin/` SDK unit tests

| ID | Name | What it proves |
|---|---|---|
| T23 | `TestAuditRecorderDenyAccumulatesHintOnContext` | Shape 1 context recorder |
| T24 | `TestAuditRecorderAllowAccumulatesHintWithCorrectEffect` | Allow path |
| T25 | `TestSDKSerializesAccumulatedHintsIntoCommandResponse` | SDK → proto marshaling |
| T26 | `TestSDKClearsContextSliceAfterSerialization` | No leak across handler invocations |
| T27 | `TestAuditRecorderAttributesAreCopiedNotReferenced` | Map safety |
| T27a | `TestAuditRecorderDenyWithEmptyIDIsSilentlyDroppedAndLogged` | SHOULD — boundary: empty ID would fail proto min_len=1 at marshal; SDK rejects early and logs a warning |

### Layer 4 — `internal/plugin/hostfunc/cap_audit` unit tests

| ID | Name | What it proves |
|---|---|---|
| T28 | `TestCapAuditRegistersAuditGlobalOnLuaState` | Module registration |
| T29 | `TestCapAuditDenyAcceptsStringStringTableArguments` | Lua argument parsing |
| T30 | `TestCapAuditConvertsLuaTableToGoAttributesMap` | Type conversion |
| T31 | `TestCapAuditReadsContextFromLState` | Context propagation from dispatcher |
| T32 | `TestCapAuditPushesHintToContextBoundSlice` | End-to-end in-process emit |
| T33 | `TestCapAuditStampsPluginNameAsComponent` | Anti-spoofing in Lua path |
| T34 | `TestCapAuditRejectsInvalidLuaArgumentTypes` | Input validation |
| T34a | `TestCapAuditSkipsNonStringKeysInAttributesTable` | SHOULD — negative: Lua table with int/bool keys. Non-string keys are silently dropped per the attr map contract |

### Layer 5 — Integration tests

Integration tests MUST use a real PostgreSQL database via testcontainers, run the full migration set (including 000005), and wire `audit.NewPostgresWriter(db)` into the `audit.Logger`. In-memory writers are NOT acceptable at this layer — the tests must verify the data round-trips through the `access_audit_log` table with all new columns (`source`, `component`, `event_id`, `event_name`, `message`) populated correctly.

Each test MUST query `access_audit_log` directly after the dispatch flow completes and assert on the actual row contents.

| ID | Name | What it proves |
|---|---|---|
| T35 | `"binary plugin emitting a deny hint writes a row to access_audit_log with all host-stamped fields"` | End-to-end binary path with real plugin binary AND real Postgres; SELECT asserts source='plugin', component=plugin name, subject stamped, event_id/name/message/attributes round-trip |
| T36 | `"Lua plugin calling audit.deny writes a row to access_audit_log with all host-stamped fields"` | End-to-end Lua path with real Lua VM AND real Postgres; same SELECT assertions as T35 |
| T37 | `"host stamped Component equals the emitting plugin name for both binary and Lua rows"` | Trust boundary holds across transports, verified against the DB row contents |
| T38 | `"audit write failure on one hint does not prevent subsequent hints from being written or fail the plugin command response"` | Failure mode at integration level; final row count in access_audit_log equals number of successful writes; dispatcher returns nil error |
| T39 | `"multiple hints from a single command dispatch all appear as separate rows in access_audit_log"` | Multi-hint handling; SELECT count returns N for N emitted hints |
| T40 | `"migration 000005 applies cleanly and access_audit_log has the expected column shape"` | Schema migration verified with information_schema queries against a real Postgres testcontainer |
| T40a | `"migration 000005 down rollback returns schema to original shape"` | MUST — invariant: reversibility. Apply up, apply down, assert event_id/event_name/source/component/message are absent and policy_id/policy_name are restored |
| T40b | `"migration 000005 backfills existing rows with default source=engine and component=abac"` | SHOULD — invariant: existing data preservation. Seed rows into `access_audit_log` via baseline migration INSERTs, apply 000005, assert backfill defaults populated those rows correctly |

### Layer 6 — Regression

| ID | Name | What it proves |
|---|---|---|
| T41 | `TestEngineAuditEmitsContainSourceEngineAndComponentAbac` | Engine path updated consistently |
| T42 | Every existing audit test under the old package path | Must pass after move + rename |

## Out of Scope

- **Channel plugin rework** — tracked separately as `holomush-0sc.12`; depends on this work landing first
- **D-rpc transport** (separate host RPC for audit emits) — SDK interface abstraction keeps the door open; not built in v1
- **Background-goroutine audit emits** — plugins that want to audit deferred decisions (timers, subscription callbacks) have no path in v1; YAGNI until a concrete use case emerges
- **Per-plugin rate limiting on audit emits** — no current abuse vector; a well-behaved plugin emits at most one hint per command; operators have metrics to detect pathological cases
- **Subject attribute reference validation** — orthogonal to source discrimination; handled (or not) by the ABAC policy engine independently
- **Tightened resource trust boundary** (host validating Resource type against plugin's declared resource_types) — plausible follow-up, not v1
- **Schema registry for plugin audit reason codes** — plugin authors pick their own ID/Name/Message strings with no cross-plugin validation; collisions are the operator's problem to grep around, not a correctness issue

## Acceptance Criteria

- All 51 tests in the plan land and pass (42 baseline + 9 boundary/negative/invariant additions)
- `task pr-prep` passes green (lint, fmt, schema, license, unit, integration, E2E)
- `internal/access/policy/audit/` no longer exists; `internal/audit/` is the canonical location
- `internal/audit/` imports contain no plugin-specific packages (verified by a static test or an import list assertion)
- Engine audit emits carry `Source = SourceEngine`, `Component = "abac"` after the rewrite
- The test-abac-widget binary plugin can emit an audit hint via `pluginsdk.Audit(ctx).Deny(...)` and have it appear in the audit log (integration test T35)
- A test Lua plugin can emit an audit hint via the Lua `audit.deny(...)` global and have it appear in the audit log (integration test T36)
- `holomush-0sc.12` (channel plugin rework) can build on top of this landing — verified by the channels bead being updated to note the dependency is met
- Documentation updated: audit package README, plugin author docs in `site/docs/extending/` covering both the Go SDK API and the Lua `audit` global
