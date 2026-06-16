<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Lua Binding Stub Generator (editor/LSP dev-aid) — Design

- **Bead:** holomush-eykuh.9
- **Epic:** holomush-eykuh (Plugin capability & dependency architecture)
- **Status:** design
- **Date:** 2026-06-16
- **Follow-on to:** sub-spec 3 / holomush-eykuh.2 (descriptor-driven Lua host-cap bindings)

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as described in RFC 2119 and RFC 8174.

## 1. Problem & goal

Plugin authors writing Lua against the host call surface get no editor support:
the `namespace.Method{…}` bridge bindings and the ambient `holomush.*` stdlib are
invisible to lua-language-server (LuaLS), so there is no autocomplete, hover, or
go-to-definition for host calls.

**Goal:** emit a committed LuaLS **definition file** (`---@meta`) describing the
Lua host-call surface, purely as an editor/LSP discoverability aid. The generated
artifact is **off the runtime path** — it MUST NOT be loaded by any plugin runtime
and MUST NOT change plugin behavior. It is a pure developer-experience aid.

### Surfaces covered

1. **`host.v1` capability namespaces** — the descriptor-driven `namespace.Method{…}`
   bridge surface. Each capability is injected as a **bare Lua global** named by its
   capability token (`emit`, `focus`, `kv`, `eval`, `settings`, `stream_history`,
   `stream_subscription`, `audit`, `command_registry`) via the generated
   `bindings_gen.go` (`L.SetGlobal("<token>", tbl)`). These are generated from the
   **statically registered** `host.v1` FileDescriptors — the same walk that produces
   `bindings_gen.go`.
2. **Ambient stdlib** — the always-available in-process host functions, injected as
   **two distinct Lua globals**. These are NOT descriptor-driven, so their
   annotations come from a structured Go declaration table (§4.2):
   - **`holomush.*`** (assembled by `hostfunc.Register()`, `functions.go`): `log`,
     `new_request_id`, `list_commands`, `get_command_help`, `add_session_stream`,
     `remove_session_stream`, `query_stream_history`, `decrypt_own_audit_rows`,
     `register_emit_type`, plus the **`holomush.config`** subtable (typed accessors:
     `string`/`int`/`bool`/`duration` and their `require_*` variants, from
     `registerConfigTable()`).
   - **`holo.*`** (assembled by `hostfunc.RegisterStdlib()` in `stdlib.go`, which is
     itself invoked from the production `hostfunc.Register()`): `holo.fmt.*` (`bold`,
     `italic`, `dim`, `underline`, `color`, `list`, `pairs`, `table`, `separator`,
     `header`, `parse`) and `holo.emit.*` (`location`, `character`, `global`,
     `flush`). The `holo.emit.*` functions are always *registered* as fields (they
     raise at call-time only if the emitter is unwired), so they belong in the stub.

   > **`kv_*` is NOT ambient.** The legacy `holomush.kv_get`/`kv_set`/`kv_delete`
   > hostfuncs were retired in the atomic capability cutover (ADR `holomush-05f3v`)
   > and are now only wired in tests. KV access is the `kv` capability namespace
   > (surface 1), not an ambient function. The decl table MUST NOT include them.
   >
   > **`holo.session.*` is NOT ambient.** It is added only by
   > `hostfunc.RegisterSessionFuncs()`, which has **no production caller** (test-only)
   > and requires a `session.Access` — i.e. it is capability-gated, not always-on. It
   > is out of scope (see "Out of scope" below), NOT in the ambient decl table.

   The decl table is the **single source of truth** for the ambient annotations; the
   set of declared names is held equal to the actually-registered names by the
   parity test (§5.2). The authoritative registration sites are
   `internal/plugin/hostfunc/functions.go` (`holomush.*` + `holomush.config`; it also
   calls `RegisterStdlib`) and `stdlib.go` (`holo.fmt`/`holo.emit`).

### Out of scope (YAGNI)

- **Provider (plugin→plugin) service stubs.** Provider descriptors exist only at
  plugin **load time**, per-plugin; a static build-time generator cannot see them.
  Deferred until a concrete need exists; would require a separate load-time path.
- **Capability-gated hand-written hostfunc families** — e.g. `holo.session.*`
  (`RegisterSessionFuncs`, capability-gated, no production always-on call) and the
  `cap_*.go` Lua surfaces (`world_query`, `property`, …). These are neither the
  descriptor-driven `host.v1` bridge (surface 1) nor always-on ambient (surface 2);
  they are hand-written, capability-gated functions. Covering them is a separate
  follow-on (their type metadata is also imperative, with the same drift concerns).
  Note the **descriptor-driven** `session` host.v1 namespace (the `session` bare
  global from `bindings_gen.go`) IS still covered by surface 1 — only the
  hand-written `holo.session.*` hostfuncs are excluded.
- **LuaLS configuration scaffolding** beyond documenting the `Lua.workspace.library`
  setting authors point at the stub directory.
- **Any runtime, loader, or binding behavior change.** This change adds a generator
  output and a doc note; it touches no runtime code path.

## 2. Grounding

- **Existing generator (prior art):** `internal/plugin/luabridge/gen/main.go` walks
  the registered `host.v1` FileDescriptors via `protoregistry.GlobalFiles`, keys
  services by **capability token**, collects unary methods, and emits
  `bindings_gen.go` via `text/template`. The stub generator reuses this walk.
- **Ambient stdlib registration:** the ambient functions are registered
  imperatively (`L.SetField(tbl, "<name>", L.NewFunction(...))`). The production
  entrypoint is `hostfunc.Register()` (`functions.go`), which builds the `holomush.*`
  global (plus the `holomush.config` subtable via `registerConfigTable()`) **and**
  calls `hostfunc.RegisterStdlib()` (`stdlib.go`), which builds the `holo.*` global
  (`holo.fmt` + `holo.emit`). `RegisterSessionFuncs()` (`holo.session`) is **not**
  called from this production path (test-only; capability-gated) and is out of scope.
  There is **no** structured param/return type metadata to introspect — hence the
  decl-table source (§4.2).
- **Authoritative ambient-surface boundary (read this before classifying):** the
  doc comment at `internal/plugin/hostfunc/functions.go:268-287` states that, as of
  the atomic capability cutover (ADR `holomush-05f3v`), the **ten host capabilities**
  — `kv`, `world.query`, `world.mutation`, `property`, `session`, `session.admin`,
  `focus`, `eval`, `settings`, `emit` — are **no longer injected as ambient
  functions**; they flow exclusively through the host-brokered `RegisterHostCaps`
  path. What remains ambient is logging, request-id, the `holo.*` stdlib (`fmt`/
  `emit`), `register_emit_type`, `holomush.config`, and the command-registry / stream
  / audit read-back surfaces. The exported `Functions.RegisteredFunctionsForAudit()`
  (same file, ~line 396) is a **structured enumeration** of exactly this ambient
  `holomush.*` set and is the canonical anchor. Consequence: `kv_*` and the
  `session` family (incl. `holo.session.*`) are **NOT ambient** — do not add them to
  the decl table regardless of the existence of `kvGetFn`/`RegisterSessionFuncs`
  symbols (those are wired only via `export_test.go` / capability paths, never the
  production `Register()` body at lines 288-335).
- **Output format (LuaLS):** per the lua-language-server wiki
  (`https://luals.github.io/wiki/annotations`, `/definition-files`): `---@meta`
  marks a definitions-only file; `---@class Name` + `Name = {}` declares a
  namespace; `---@field name type doc`; `---@param`/`---@return` document
  functions; `---@alias` declares type aliases. Optional via `name?`, unions via
  `A|B`, arrays via `T[]`, maps via `table<K,V>`.

## 3. Output format

A single committed LuaLS definition file. Each capability namespace becomes a
`---@class`; each unary RPC becomes a typed function; each proto message referenced
as a request/response becomes its own `---@class` with one `---@field` per field.

```lua
---@meta holomush

---@class holomush.host.emit.EmitEventRequest
---@field stream string
---@field event_type string
---@field payload string

---@class holomush.host.emit.EmitEventResponse

---@class holomush.host.emit
local emit = {}

---Emit an event from the plugin.
---@param req holomush.host.emit.EmitEventRequest
---@return holomush.host.emit.EmitEventResponse
function emit.EmitEvent(req) end
```

> **Naming note:** capability namespaces are accessed at runtime as **bare Lua
> globals** (`emit.EmitEvent(...)`, `kv.Get(...)`) — the `emit` global is injected by
> `bindings_gen.go` via `L.SetGlobal("emit", …)`. The `holomush.host.<token>.*`
> class-name prefix above is **only** a LuaLS class-naming convention to avoid
> collisions in the `@class` namespace; it is not how the table is reached in Lua.
> The generated `local <token> = {}` table MUST be exported so LuaLS resolves the
> bare global (e.g. annotate it as the global, not a file-local). Ambient globals
> `holomush` and `holo` are likewise declared as their bare global names.

### Proto → Lua type mapping (normative)

| Proto | Lua annotation |
| --- | --- |
| `string`, `bytes` | `string` |
| `bool` | `boolean` |
| `int32/int64/uint*/sint*/fixed*` | `integer` |
| `float`, `double` | `number` |
| `enum` | `integer` (MAY emit an `---@alias` of the enum value names in a later iteration; not required for v1) |
| message `M` | a generated `---@class` for `M` |
| `repeated T` | `T[]` |
| `map<K,V>` | `table<K, V>` |
| proto3 `optional` field | `name?` (optional field) |
| `oneof { a; b; c }` | each variant emitted as a **mutually-exclusive optional** `---@field` (`a?`, `b?`, `c?`), with a comment naming the oneof group. (e.g. `CreateObjectRequest.placement` → `location_id?`/`character_id?`/`container_id?`.) A stricter union type is NOT required for v1. |
| well-known types (`google.protobuf.Timestamp`/`Duration`/etc.) | mapped to a generated `---@class` like any other message (their fields are emitted). If none appear in the covered `host.v1` protos, no special handling is needed; the generator MUST NOT crash on encountering one. |

- The generator MUST emit a `---@class` for every message reachable as a request or
  response of a covered RPC (transitively, for nested message fields).
- Class names MUST be namespaced to avoid collisions (e.g.
  `holomush.host.<token>.<MessageName>`).
- The generator SHOULD carry the proto leading doc-comment (if present in the
  descriptor) onto the corresponding function/field as the annotation description.

## 4. Components

### 4.1 Generator extension — `internal/plugin/luabridge/gen`

`gen/main.go` is extended so that, after emitting `bindings_gen.go`, it runs a
second emitter over (a) the already-collected `host.v1` services and (b) the
ambient decl table (§4.2), writing the `.lua` stub. The descriptor walk
(`collectServices`/`collectMethods`) is **reused**; the new code is a `text/template`
plus a proto→Lua type-mapping function. The two outputs share one `go:generate`
invocation (`go run ./gen`).

### 4.2 Ambient decl table — `internal/plugin/luabridge/gen/ambient.go`

A Go slice declaring the ambient stdlib surface, the single source of truth for its
annotations. Because the ambient surface spans nested submodules across two globals
(`holomush`, `holomush.config`, `holo.fmt`, `holo.emit`), each entry
carries its **dotted module path**:

```go
type ambientFn struct {
    Module  string         // "holomush" | "holomush.config" | "holo.fmt" | "holo.emit"
    Name    string         // "register_emit_type"
    Doc     string         // leading description
    Params  []ambientParam // {Name, Type}  (Type is a LuaLS type string)
    Returns []string       // LuaLS return type strings
}
```

The decl table MUST cover the always-on ambient surface: `holomush.*` +
`holomush.config.*` (from `functions.go` / `registerConfigTable()`) and
`holo.fmt.*` / `holo.emit.*` (from `stdlib.go`, invoked by `Register()`). It MUST
NOT include `holo.session.*` (capability-gated, out of scope). The set of
`(Module, Name)` pairs MUST equal the set actually registered by the production
entrypoint at runtime (enforced by §5.2). Adding or removing an ambient hostfunc
without updating this table MUST fail CI.

### 4.3 Output home

The generated stub MUST be committed at a stable in-repo path:
`pkg/plugin/luastubs/holomush.lua`. **This directory is new** — it does not exist
today and is created by this change (along with the generated file). A single
`---@meta` file holds all surfaces (capability namespaces + both ambient globals).
Plugin authors point lua-language-server's `Lua.workspace.library` at
`pkg/plugin/luastubs/`. A short contributor doc note (under
`site/src/content/docs/extending/`) MUST document the setting. The generated file
MUST carry a "generated; do not edit" header.

## 5. Verification

### 5.1 Drift check (regenerate-and-diff) — pr-prep

The existing generated-artifact verification in `pr-prep:run` (which already
regenerates `bindings_gen.go` and fails on diff) MUST be extended so that
`go run ./gen` leaving **either** `bindings_gen.go` **or**
`pkg/plugin/luastubs/holomush.lua` modified fails the check. This guarantees the
committed stub never drifts from the descriptors + decl table.

### 5.2 Ambient decl-table drift test

There is **no** exported API that returns the registered ambient names, and
registration is spread across many imperative `SetField` calls. The parity test
therefore MUST derive the truth set **dynamically from a live runtime**, not from a
hand-kept Go list:

1. Construct a real `gopher-lua` `LState`.
2. Build the ambient globals by invoking the **production entrypoint**
   `hostfunc.Register(...)` — which installs `holomush` + `holomush.config` and
   internally calls `RegisterStdlib` (→ `holo.fmt` + `holo.emit`). The test MUST
   invoke exactly the entrypoint(s) production uses, and MUST NOT call
   `RegisterSessionFuncs` (it is not in the production ambient path; calling it would
   pollute the truth set with the out-of-scope `holo.session.*`). Pass test doubles /
   zero values for `Register`'s dependencies: the set of `SetField`'d field **names**
   does not depend on the backing implementations, only on the registration code
   being exercised.
3. Recursively walk the resulting `holomush` and `holo` `LTable`s, collecting every
   field whose value is an `*LFunction`, keyed by `(dotted-module-path, name)`.
4. Assert that set is **exactly equal** to the `(Module, Name)` set in the decl
   table — a registered function missing from the table, or a table entry with no
   live registration, MUST fail.

If an assembly entrypoint cannot be invoked in a unit test without heavy wiring,
that is itself a finding to resolve during planning (e.g. extract a thin
name-only registration shim) — the test MUST exercise real registration code, not a
duplicated name list, or it provides no anti-drift value.

### 5.3 Type-mapper unit test

A table-driven Go test MUST cover the proto→Lua type mapping (§3): scalars,
`repeated`, `map`, message references, and proto3 `optional`.

### 5.4 Generator output sanity

A test SHOULD assert the generated stub parses as a valid LuaLS `---@meta` file at a
structural level (begins with `---@meta`, every referenced `@class` is declared).
A full LuaLS-runs-clean assertion is NOT required (no LuaLS binary in CI).

## 6. Risks & non-goals

- **Drift is the main risk** for the ambient surface; §5.2 is the mitigation. The
  descriptor-driven half cannot drift (regenerated from the same source as
  `bindings_gen.go`).
- **No new invariant** is introduced: this is a dev-aid artifact, not a system
  guarantee. (Per `.claude/rules/invariants.md`, a generated editor-tooling stub is
  not a registry invariant; the drift checks are PR-gate acceptance criteria, not
  `INV-*` entries.)
- **Not a runtime contract:** the stub is advisory for editors only. A plugin that
  ignores it behaves identically; a stale stub is a dev-ex bug, never a correctness
  bug.
