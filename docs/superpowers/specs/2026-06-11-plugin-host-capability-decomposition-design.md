<!--
SPDX-License-Identifier: Apache-2.0
-->

# Plugin Host Capability Decomposition — Design

> Sub-spec 2 of epic `holomush-eykuh` (Plugin capability & dependency
> architecture). Bead: `holomush-eykuh.1`. Depends on the foundation
> (`holomush-oeb4d`, merged PR #4426) which locked the *model*; this spec fills
> in the *taxonomy* and the proto contracts. It MUST NOT reopen the foundation's
> invariants (INV-PLUGIN-41/42/43/44/45) — only implement them.

## Overview

The host exposes its capabilities to plugins through a single 23-RPC
`PluginHostService` god-service (`api/proto/holomush/plugin/v1/plugin.proto:66`).
Because a plugin's manifest declares dependencies at *service* granularity, a
plugin that needs to (say) read world state must be granted the entire
god-service — emit, ABAC eval, audit decryption, focus mutation, settings
writes, and all. Service-grain declarations therefore cannot express least
privilege.

This spec decomposes that surface into **capability-scoped proto contracts**: a
controlled vocabulary of capability tokens, each backing exactly one small proto
service in a dedicated `holomush.plugin.host.v1` namespace. Declaring
`capability: focus` grants exactly the focus service's RPCs and nothing else.
The god-service is **deleted**; every RPC is rehomed.

The capability surface is broader than the binary god-service: most capabilities
already exist as **Lua hostfuncs** (`internal/plugin/hostfunc/functions.go`) with
a richer surface than their binary counterparts, and some
(`kv`, `log`) are served *only* on the Lua side while their binary RPCs are
declared-but-unimplemented (`holomush-l6std`). The taxonomy is therefore the
**union** of both runtimes' surfaces — which simultaneously establishes the
contract parity the epic mandates.

## RFC2119 Keywords

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** in
this document are to be interpreted as described in RFC 2119 and RFC 8174 (see
the root `CLAUDE.md` "RFC2119 Keywords" table).

## Goals

1. Define the **controlled capability vocabulary** (the short tokens a manifest
   `requires` a `capability:` entry from) — the vocabulary the foundation
   (§1, §2) requires "exists and is validated" but deferred defining.
2. Decompose `PluginHostService` into **one small proto service per
   capability**, in a dedicated `holomush.plugin.host.v1` namespace, such that a
   capability grant is a meaningful least-privilege unit.
3. Produce a complete, checkable **rehoming map**: every one of the 23
   `PluginHostService` RPCs names its new capability service (or is explicitly
   retired). Deleting the god-service MUST lose nothing.
4. Fix the **runtime asymmetry** at the contract level: each capability's RPC set
   is the union of today's Lua + binary surface, becoming the single source both
   runtimes consume.
5. Draw the **ambient line**: classify which host functions are capability-grade
   authority (declared) versus runtime substrate (ungated, below the model).

## Non-Goals (deferred to later sub-specs)

| Deferred concern | Sub-spec / bead |
| --- | --- |
| Lua in-process gRPC transport **wiring** (bufconn/loopback, hot-path optimization) for consuming these contracts | 3 — `holomush-eykuh.2` |
| Least-privilege **enforcement** mechanics; the per-entry `access:` / `scope:` capability-scoping parameters and their ABAC semantics | 4 — `holomush-eykuh.3` |
| Manifest **migration**: gating the unconditional Lua injection and adding `requires` declarations to plugins that consume host functions without declaring them today (e.g. `core-building`) | 5 — `holomush-eykuh.4` |
| The **implementation status** of the revived `stream.subscription` RPCs (whether `AddSessionStream`/`RemoveSessionStream` get a live server) | 3 — `holomush-eykuh.2` |

This spec settles the **vocabulary, the contract boundaries, and the rehoming
map**. It defines proto *contracts* (services + their RPC membership), not the
field-level message wire format, which is mechanical proto authoring downstream.

**Binary vs Lua consumption — the sub-spec 2/3 line.** Deleting
`PluginHostService` (a hard requirement of the clean cutover) forces the
**binary plugin SDK** (`pkg/plugin/*_client.go`, `event_sink.go`, `audit.go`,
`sdk.go`; `plugins/core-scenes`; `cmd/holomush/core.go`) to be rewired from the
one `PluginHostServiceClient` to the per-capability service clients **in this
sub-spec** — the binary side already speaks gRPC through the broker, so the
rewire is mechanical and MUST move with the server to keep the build green. What
sub-spec 3 defers is specifically the **Lua** consumption path: Lua reaches host
capabilities through direct in-VM hostfuncs today and gains no gRPC transport
here. Therefore **the existing Lua hostfunc surface is left intact and untouched
by this sub-spec** (it keeps working exactly as today); only sub-spec 3 routes
Lua through the new contracts, and only sub-spec 5 gates/migrates it. In sub-spec
2 the new proto services and the legacy Lua hostfuncs coexist.

## Background — current state (grounded)

### The god-service (binary half)

`PluginHostService` (`api/proto/holomush/plugin/v1/plugin.proto:66-225`) declares
**23 RPCs** (the proto's own doc comment says "18" and the foundation spec said
"25" — both stale; the verified count on `main` is 23). The registered server
(`internal/plugin/goplugin/host_service.go`, `pluginHostServiceServer`) embeds
`UnimplementedPluginHostServiceServer` and serves only 17 of those 23; six are
declared-but-unserved (`Log`, `KVGet`, `KVSet`, `KVDelete`, `AddSessionStream`,
`RemoveSessionStream` — `holomush-l6std`).

Natural clusters in the 23:

| Cluster | RPCs |
| --- | --- |
| emit | `EmitEvent`, `RequestEmitToken` |
| focus | `JoinFocus`, `LeaveFocus`, `LeaveFocusByTarget`, `PresentFocus`, `SetConnectionFocus`, `GetConnectionFocus`, `AutoFocusOnJoin`, `IsAnyConnFocused` |
| settings | `GetSetting`, `SetSetting` |
| command-registry | `ListCommands`, `GetCommandHelp` |
| eval | `Evaluate` |
| audit | `DecryptOwnAuditRows` |
| stream-history | `QueryStreamHistory` |
| kv (unserved) | `KVGet`, `KVSet`, `KVDelete` |
| stream-subscription (unserved) | `AddSessionStream`, `RemoveSessionStream` |
| log (unserved) | `Log` |

`QuerySessionStreams` lives on `PluginService` (the **plugin-implemented**
host→plugin delivery half), not `PluginHostService` — it is not a host
capability and is out of scope.

### The Lua hostfunc surface (richer, unconditional)

`internal/plugin/hostfunc/functions.go:228` `Register` installs the host-function
surface on every Lua plugin's per-VM `holomush` module table **unconditionally**,
*before* the `requires`-gated `InjectRequired` guard (`functions.go:308`). The
installed functions:

| Lua function(s) | Capability domain |
| --- | --- |
| `log` | (ambient) |
| `new_request_id` | (ambient) |
| `kv_get`, `kv_set`, `kv_delete` | `kv` |
| `query_location`, `query_character`, `query_location_characters`, `query_object`, `find_location` (read — note `functions.go` groups `find_location` under "World mutations" by proximity; it is a lookup, so it homes to `world.query`) | `world.query` |
| `create_location`, `create_exit`, `create_object` | `world.mutation` |
| `get_property`, `set_property` | `property` |
| `session.find_by_name`, `session.set_last_whispered`, `session.list_active` (`cap_session.go` `SessionCapability`, the emerging capability module) | `session` |
| `session.broadcast` (BroadcastSystemMessage → all sessions), `session.disconnect` (DisconnectSession, forcible) | `session.admin` |
| `list_commands`, `get_command_help` | `command-registry` |
| `evaluate` | `eval` |
| `get_setting`, `set_setting` | `settings` |
| `add_session_stream`, `remove_session_stream` (`stdlib_streams.go`) | `stream.subscription` |
| focus funcs (`RegisterFocusFuncs`) | `focus` |
| audit decrypt funcs (`RegisterAuditFuncs`) | `audit` |
| `register_emit_type` | `emit` |
| `holo.*` stdlib (fmt), `holomush.config` accessors | (ambient) |

This unconditional injection is the asymmetry the model kills: Lua plugins get
the whole surface regardless of declarations, while binary plugins consume host
capabilities as discrete gRPC services through the grpcbroker. Note `kv` and
`log` are *live* on the Lua side but unserved on the binary side — the union
surface (Goal 4) revives them as real contracts.

An emerging per-capability abstraction already exists in the hostfunc package
(`hostfunc/cap_session.go` `SessionCapability` registers **five** `session.*`
functions — `find_by_name`, `set_last_whispered`, `list_active`, `broadcast`,
`disconnect` — under the `session` global, exercised by `cap_session_test.go`);
this taxonomy formalizes and generalizes that shape. The `session` capability
has no binary god-service RPC today — it is Lua-only, so (like `world.query`,
and like `stream.subscription`'s unserved RPCs from the other direction) its
`host.v1` contract is a fresh service derived from the `SessionCapability`
surface, not a rehomed RPC.

A **legacy shadow path** exists: `stdlib_session.go` `RegisterSessionFuncs`
installs `find_by_name` + `set_last_whispered` under the `holo.session.*` stdlib
sub-table — older and narrower than `cap_session.go`'s five-function `session`
global. The new `SessionService` contract is the single source; **retiring the
`holo.session.*` shadow path is the migration's concern (sub-spec 5)**, not this
spec's — flagged here so a plan author does not mistake the duplicate for two
distinct capabilities.

### What the foundation already locked

- Typed `requires` entries: each is exactly one of `capability:` (short vocab)
  or `service:` (proto path). (INV-PLUGIN-41/42.)
- The capability vocabulary is host-provided, decoupled from proto naming, and
  validated; the foundation registered the **minimal** set (`session`,
  `property`, `world.query`) for its boot-bug reclassification and deferred the
  full taxonomy here.
- One brokered-gRPC consumption path for both runtimes, gated by declaration,
  authorized as `PluginSubject`. (INV-PLUGIN-44.)
- `alias` is **not** a capability — it is delivered at the command layer
  (`command.AliasCache`), and the foundation dropped `core-aliases`' phantom
  `requires` (foundation §4). This spec mints **no** `alias` capability.

## Design

### §1 — The capability vocabulary

A capability token is a lowercase, dot-hierarchical identifier (a hyphen joins
words within one segment). The full vocabulary:

```text
world.query   world.mutation   property   session   session.admin   focus
eval          emit             settings   kv        stream.history   stream.subscription
audit         command-registry
```

Fourteen tokens. Each maps to exactly one proto service in
`holomush.plugin.host.v1` (§2). The mapping is a **host-owned registry** — a
static table the resolver consults to (a) validate that a `capability:` entry
names a real token (INV-PLUGIN-42), and (b) bind the token to the service the
broker grants. The concrete Go representation of that table is an implementation
detail; this spec fixes its *contents* (the table below).

### §2 — The capability services

All in one proto package, `holomush.plugin.host.v1` (new), one file per service
under `api/proto/holomush/plugin/host/v1/`:

| Token | Service | RPCs (union surface) |
| --- | --- | --- |
| `world.query` | `WorldQueryService` | `QueryLocation`, `QueryCharacter`, `QueryLocationCharacters`, `QueryObject`, `FindLocation` |
| `world.mutation` | `WorldMutationService` | `CreateLocation`, `CreateExit`, `CreateObject` |
| `property` | `PropertyService` | `GetProperty`, `SetProperty` |
| `session` | `SessionService` | `FindByName`, `ListActive`, `SetLastWhispered` |
| `session.admin` | `SessionAdminService` | `Broadcast`, `Disconnect` |
| `focus` | `FocusService` | `JoinFocus`, `LeaveFocus`, `LeaveFocusByTarget`, `PresentFocus`, `SetConnectionFocus`, `GetConnectionFocus`, `AutoFocusOnJoin`, `IsAnyConnFocused` |
| `eval` | `EvalService` | `Evaluate` |
| `emit` | `EmitService` | `EmitEvent`, `RequestEmitToken`, `RegisterEmitType` |
| `settings` | `SettingsService` | `GetSetting`, `SetSetting` |
| `kv` | `KVService` | `Get`, `Set`, `Delete` |
| `stream.history` | `StreamHistoryService` | `QueryStreamHistory` |
| `stream.subscription` | `StreamSubscriptionService` | `AddSessionStream`, `RemoveSessionStream` |
| `audit` | `AuditService` | `DecryptOwnAuditRows` |
| `command-registry` | `CommandRegistryService` | `ListCommands`, `GetCommandHelp` |

Message types are carried into the new files from their current definitions
(`plugin.proto` for the binary RPCs; the Lua-only surfaces — `world.query`,
`world.mutation`, `property`, `session`, `session.admin`, `kv` — get message
types derived from the existing hostfunc Go signatures, since they have no prior
proto). The
`PluginHostService...` request/response name prefix is dropped in favour of
service-local names (e.g. `EmitService.EmitEvent` takes `EmitEventRequest`).

Each service's existing host-side authorization semantics are **preserved
verbatim** — they are not redesigned here. In particular:

- `EmitService.EmitEvent` keeps the dispatch-token forgery fence and the
  manifest `emits` / `actor_kinds_claimable` / `crypto.emits` gates
  (INV-PLUGIN-29).
- `EmitService.RequestEmitToken` keeps mTLS-bound self-token issuance (the
  plugin identity comes from the server struct, never the wire).
- `EvalService.Evaluate` keeps token→actor subject recovery (no subject on the
  wire, INV-PLUGIN-22).
- `AuditService.DecryptOwnAuditRows` keeps the two-gate ownership + readback
  check (INV-CRYPTO-27) and per-row `RowResult` semantics (INV-CRYPTO-37).
- `SettingsService.SetSetting` keeps host-bound owner partition and the
  GAME-scope operator-authorization requirement.

### §3 — The splitting principle (when one capability becomes two services)

Selective mutation-axis split, codified:

> A domain is split into **separate services** only when it contains operation
> subsets with a **materially different blast radius** that **plausibly
> independent consumers** need separately. A tight get/set pair on one resource
> (same blast class) stays a **single service**; read-vs-write least privilege
> for it is then expressed by sub-spec 4's per-entry `access:` parameter on the
> one token, not by a second service.

The split axis is always **within one domain** (domain-primary is preserved):
it sub-divides a domain along its natural authority seam. This is **not** the
rejected trust-tier structural axis — that axis is a *cross-domain* partition
(one "DangerousOps" service lumping focus-writes with disconnects with
settings-writes). Splitting `session` into lookup vs moderation does not cross a
domain boundary; lumping all moderation across domains would.

Applied:

| Domain | Decision | Why |
| --- | --- | --- |
| world | **split** (`world.query` / `world.mutation`) | 5 query RPCs vs 3 create RPCs — genuinely different surfaces; `core-objects` reads world without mutating; foundation already pre-committed both tokens |
| stream | **split** (`stream.history` / `stream.subscription`) | reading past events vs mutating live subscriptions are different operations on different concerns |
| session | **split** (`session` / `session.admin`) | lookup (`FindByName`, `ListActive`, `SetLastWhispered`) is everyday; `Broadcast` (mass-message every session) and `Disconnect` (forcibly drop a session) are high-blast moderation. A name-lookup consumer (e.g. whisper routing) MUST NOT also get force-disconnect-anyone — bundling them would recreate the god-service problem in miniature |
| property | **one service** | tight `Get`/`Set` pair on one resource; read-only scoping deferred to `access:` |
| settings | **one service** | tight `Get`/`Set` pair; `Set` already op-auth-gated at GAME scope |
| focus | **one service** | atomic domain — its 8 RPCs are one coherent authority; read-only focus scoping deferred to `access:` |

### §4 — The ambient line (substrate below the model)

Some host functions are **not capabilities**: they are the runtime substrate
every plugin gets, conveying no host authority and reaching nothing the plugin
does not already own. They are never a `requires` entry and never appear in
`holomush.plugin.host.v1`.

| Function | Why ambient |
| --- | --- |
| `log` | write-only observability to the host logger; conveys no state back, grants no reach. The host *wants* every plugin observable — gating it is anti-host. (Lua: `holomush.log` hostfunc; binary: go-plugin stderr capture.) |
| `new_request_id` | pure ULID utility — zero authority, zero data |
| `holo.*` stdlib (fmt/string helpers) | pure in-process helpers |
| `holomush.config` accessors | the plugin's **own** merged config |

The deciding test: **does the call cross the host trust boundary to touch
something the plugin does not already own?** `kv` is self-namespaced yet is
brokered host *persistence*, so it is a capability; `log` writes to the host
logger but is write-only observability, so it is substrate. "Self-scoped" is not
the test; "brokered host authority" is.

The `Log` RPC on the god-service is therefore **deleted, not rehomed** — binary
plugins log via go-plugin's framework-native stderr capture, Lua via the
ambient `holomush.log` hostfunc.

### §5 — Rehoming map (the god-service is deleted)

Every `PluginHostService` RPC's destination. After this map is applied,
`PluginHostService` has no remaining members and the `service` block is removed
from `plugin.proto`.

| Old `PluginHostService` RPC | New home |
| --- | --- |
| `EmitEvent` | `EmitService.EmitEvent` |
| `RequestEmitToken` | `EmitService.RequestEmitToken` |
| `JoinFocus` | `FocusService.JoinFocus` |
| `LeaveFocus` | `FocusService.LeaveFocus` |
| `LeaveFocusByTarget` | `FocusService.LeaveFocusByTarget` |
| `PresentFocus` | `FocusService.PresentFocus` |
| `SetConnectionFocus` | `FocusService.SetConnectionFocus` |
| `GetConnectionFocus` | `FocusService.GetConnectionFocus` |
| `AutoFocusOnJoin` | `FocusService.AutoFocusOnJoin` |
| `IsAnyConnFocused` | `FocusService.IsAnyConnFocused` |
| `Evaluate` | `EvalService.Evaluate` |
| `DecryptOwnAuditRows` | `AuditService.DecryptOwnAuditRows` |
| `QueryStreamHistory` | `StreamHistoryService.QueryStreamHistory` |
| `ListCommands` | `CommandRegistryService.ListCommands` |
| `GetCommandHelp` | `CommandRegistryService.GetCommandHelp` |
| `GetSetting` | `SettingsService.GetSetting` |
| `SetSetting` | `SettingsService.SetSetting` |
| `KVGet` | `KVService.Get` (revived from unserved) |
| `KVSet` | `KVService.Set` (revived) |
| `KVDelete` | `KVService.Delete` (revived) |
| `AddSessionStream` | `StreamSubscriptionService.AddSessionStream` (revived; impl status → sub-spec 3) |
| `RemoveSessionStream` | `StreamSubscriptionService.RemoveSessionStream` (revived; impl status → sub-spec 3) |
| `Log` | **retired** — ambient substrate (§4) |
| `RegisterEmitType` *(Lua-only today)* | `EmitService.RegisterEmitType` |

**23 god-service RPCs: 22 rehomed + 1 retired (`Log`)** — no god-service member
left behind. Plus **1 Lua-only function** (`RegisterEmitType`) promoted into
`EmitService`, so the map has 24 rows for 23 RPCs.

### §6 — Relationship to existing domain protos

`holomush.world.v1.WorldService` (4 read RPCs, host-provided, broker-reachable
today by `core-scenes`) is a **domain service**, not a host capability, and
stays where it is. The `world.query` capability is a **fresh** `host.v1`
contract whose RPC set is the *Lua* query surface (`QueryLocation`,
`QueryCharacter`, `QueryLocationCharacters`, `QueryObject`, `FindLocation`),
which is richer than and named differently from `WorldService`'s
`GetLocation`/`ListExits`/etc. The two are allowed to coexist; reconciling or
retiring `WorldService` is **out of scope** (it would touch `core-scenes`'
broker consumption and belongs to the migration, sub-spec 5, if pursued at all).
This keeps the host-capability family auditable as one namespace, per the
namespace decision.

## Invariants (proposed)

To be registered in `docs/architecture/invariants.yaml` on spec finalization
(per `.claude/rules/invariants.md`); each is `binding: pending` until a test
asserts it. The registry currently maxes at INV-PLUGIN-46 (the foundation
follow-up `holomush-et5lz` / PR #4428 bound INV-PLUGIN-46, "exactly one provider
per proto service name"); these allocate the next free 47/48/49.

- **INV-PLUGIN-47 (no god-service / taxonomy completeness).** Every
  host-brokered capability function MUST map to exactly one capability-scoped
  service in `holomush.plugin.host.v1`; no `host.v1` service MUST span two
  capability domains. `PluginHostService` MUST NOT exist after this change.
- **INV-PLUGIN-48 (host.v1 is declared capabilities only).** Ambient runtime
  substrate (`log`, `new_request_id`, stdlib, config) MUST NOT be modeled as
  a capability: it MUST NOT appear in `holomush.plugin.host.v1` and MUST NOT be a
  valid `requires` `capability:` token.
- **INV-PLUGIN-49 (contract parity / single source).** A capability's RPC
  contract MUST be the single source both runtimes consume; there MUST NOT be a
  runtime-specific capability surface (the union resolves today's
  Lua/binary asymmetry). This is the contract-level expression of the
  `plugin-runtime-symmetry` rule for host capabilities.

INV-PLUGIN-49 is adjacent to the broader plugin-runtime-symmetry invariant and
to **INV-COMMAND-2** (`docs/architecture/invariants.yaml:3772`), which already
states `ListCommands`/`GetCommandHelp` must be reachable by both runtimes via the
same `commandquery.Querier`. INV-PLUGIN-49 generalizes that command-scoped
guarantee to the whole host-capability surface; its registry entry MUST
cross-reference INV-COMMAND-2 so the two read as general/specific, not duplicate.
It is stated narrowly (the *contract* is single-source) so a test can bind it to
the proto/registry rather than to runtime behavior.

**INV-COMMAND-2 also names `PluginHostService` in its summary** — deleting the
god-service makes that wording stale, so updating INV-COMMAND-2's summary (to name
`CommandRegistryService`) is a required deliverable of the implementation, not an
optional cleanup.

## Testing strategy

- **Registry/vocabulary test:** the host-owned token→service table is exhaustive
  over the 14 tokens and rejects unknown tokens (binds INV-PLUGIN-42's vocabulary
  half, and INV-PLUGIN-47/48).
- **Proto lint:** `task lint:proto` (buf `COMMENTS` + name-echo gate) on every
  new `host.v1` element — each RPC/message/service gets a Go-grounded doc comment
  (`.claude/rules/proto-doc-comments.md`). No ratchet exemption.
- **Rehoming completeness test (meta):** assert `PluginHostService` no longer
  exists in `plugin.proto` and that every former RPC name resolves to a `host.v1`
  service member (or the explicit `Log` retirement list) — the bijection that
  binds INV-PLUGIN-47. The deletion MUST also sweep stale textual references to
  `PluginHostService` (its own block doc comment claiming "18 RPCs"; the
  INV-COMMAND-2 summary; any contributor docs) — the plan greps for surviving
  `PluginHostService` mentions and updates or removes each.
- **No behavior change in this sub-spec:** the carve preserves each RPC's
  existing handler semantics; existing host-service handler tests are re-pointed
  at the new service names, not rewritten. New wiring/enforcement tests belong to
  sub-specs 3/4.

## Risks

| Risk | Mitigation |
| --- | --- |
| Carving 23 RPCs + 6 Lua-only surfaces into 14 services is a large mechanical proto change with churn in generated code and handler registration | The rehoming map (§5) makes it a checkable bijection; semantics are preserved, so the diff is rename/move, not redesign. Sub-spec 5 owns the manifest-side migration |
| `world.query` (`host.v1`) overlapping `WorldService` (`world.v1`) invites confusion / duplicate message types | §6 fixes the boundary: capability vs domain service, coexisting; reconciliation is explicitly out of scope |
| `stream.subscription` revives two RPCs that have no live server (`holomush-l6std`) | Contract is defined here; implementation status is explicitly deferred to sub-spec 3 — the proto existing does not assert it is served |
| INV-PLUGIN-49 may duplicate an existing symmetry invariant | Flagged for design-reviewer; stated narrowly to be independently bindable |

## References

- Foundation (model, locked): `docs/superpowers/specs/2026-06-11-plugin-capability-dependency-foundation-design.md`
- Epic decision: `holomush-eykuh.5`
- God-service: `api/proto/holomush/plugin/v1/plugin.proto:66-225`
- Lua hostfunc surface: `internal/plugin/hostfunc/functions.go:228`, `stdlib_session.go`, `stdlib_streams.go`, `cap_session.go`
- Binary host server: `internal/plugin/goplugin/host_service.go`
- Unserved RPCs: `holomush-l6std`
- grpcbroker (binary capability transport, reused): `docs/superpowers/specs/2026-04-06-grpcbroker-service-injection-design.md`
- Proto doc-comment conventions: `.claude/rules/proto-doc-comments.md`
- Plugin runtime symmetry: `.claude/rules/plugin-runtime-symmetry.md`
<!-- adr-capture: sha256=b4740ac355bdbaaf; session=cli; ts=2026-06-12T00:10:32Z; adrs=holomush-nbscl,holomush-e9go5,holomush-cryy2,holomush-2fb90 -->
