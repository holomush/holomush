<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Lua Binding Stub Generator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Emit a committed lua-language-server (LuaLS) `---@meta` definition file describing the Lua host-call surface (descriptor-driven `host.v1` capability namespaces + the ambient `holomush.*`/`holo.*` stdlib) purely for editor autocomplete/hover/go-to-def.

**Architecture:** Extend the existing descriptor-walking generator at `internal/plugin/luabridge/gen` so one `go run ./gen` emits both the runtime `bindings_gen.go` (unchanged) and a new `pkg/plugin/luastubs/holomush.lua` stub. The descriptor half reuses `collectServices`; the ambient half is driven by a structured Go decl table whose names are held equal to the live registrations by a parity test. A `pr-prep` regenerate-and-diff guards both outputs against drift.

**Tech Stack:** Go, `google.golang.org/protobuf/reflect/protoreflect` + `protoregistry`, `text/template`, `gopher-lua` (test-side enumeration), LuaLS annotations, go-task.

**Spec:** `docs/superpowers/specs/2026-06-16-lua-stub-generator-design.md`

---

## File structure

| Path | Responsibility | Create/Modify |
| --- | --- | --- |
| `internal/plugin/luabridge/gen/luatype.go` | proto `FieldDescriptor` → LuaLS type string (scalars, repeated, map, message ref, optional) | Create |
| `internal/plugin/luabridge/gen/luatype_test.go` | table-driven type-mapper tests | Create |
| `internal/plugin/luabridge/gen/ambient.go` | `ambientFn` type + the ambient decl table (single source of truth for `holomush.*`/`holomush.config`/`holo.fmt`/`holo.emit`) | Create |
| `internal/plugin/luabridge/gen/ambient_parity_test.go` | builds the live runtime surface via `hostfunc.Register` and asserts it equals the decl table | Create |
| `internal/plugin/luabridge/gen/luastub.go` | collect request/response messages + render the `---@meta` stub via `text/template` | Create |
| `internal/plugin/luabridge/gen/luastub_test.go` | structural sanity test of generated output | Create |
| `internal/plugin/luabridge/gen/main.go` | wire the stub emitter into `main()` after `bindings_gen.go` | Modify |
| `internal/plugin/luabridge/doc.go` | document the second generated artifact | Modify |
| `pkg/plugin/luastubs/holomush.lua` | the committed generated stub | Create (generated) |
| `Taskfile.yaml` | `generate:luabridge` `generates:` + the two drift-check blocks cover the `.lua` output | Modify |
| `site/src/content/docs/extending/how-to/lua-editor-setup.md` | document the `Lua.workspace.library` setting | Create |

---

## Phase 1: Generator building blocks

### Task 1: Proto → LuaLS type mapper

**Files:**

- Create: `internal/plugin/luabridge/gen/luatype.go`
- Test: `internal/plugin/luabridge/gen/luatype_test.go`

The generator is `package main`. This task adds a pure function mapping a single
`protoreflect.FieldDescriptor` to a LuaLS type string, per spec §3.

- [ ] **Step 1: Write the failing test**

Create `internal/plugin/luabridge/gen/luatype_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// fieldByPath looks up a field descriptor on a host.v1 message by message +
// field name, using the registered descriptors.
func fieldByPath(t *testing.T, msgFullName, field string) protoreflect.FieldDescriptor {
	t.Helper()
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(msgFullName))
	if err != nil {
		t.Fatalf("descriptor %s: %v", msgFullName, err)
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("%s is not a message", msgFullName)
	}
	fd := md.Fields().ByName(protoreflect.Name(field))
	if fd == nil {
		t.Fatalf("field %s not found on %s", field, msgFullName)
	}
	return fd
}

func TestLuaTypeMapsScalarStringField(t *testing.T) {
	// EmitEventRequest.stream is a proto string.
	fd := fieldByPath(t, "holomush.plugin.host.v1.EmitEventRequest", "stream")
	assert.Equal(t, "string", luaType(fd))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestLuaTypeMapsScalarStringField ./internal/plugin/luabridge/gen/`
Expected: FAIL — `undefined: luaType`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/plugin/luabridge/gen/luatype.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// luaType maps a proto field to a LuaLS type string (spec §3). Repeated and map
// fields are detected before scalar kind. Message fields render as the namespaced
// class name from luaClassName.
func luaType(fd protoreflect.FieldDescriptor) string {
	// Map must be checked before IsList: a proto map is a repeated synthetic
	// message, so IsMap() is the discriminator.
	if fd.IsMap() {
		k := scalarLuaType(fd.MapKey())
		v := luaTypeNoCollection(fd.MapValue())
		return fmt.Sprintf("table<%s, %s>", k, v)
	}
	if fd.IsList() {
		return luaTypeNoCollection(fd) + "[]"
	}
	return luaTypeNoCollection(fd)
}

// luaTypeNoCollection maps the element type, ignoring list/map wrapping.
func luaTypeNoCollection(fd protoreflect.FieldDescriptor) string {
	if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
		return luaClassName(fd.Message())
	}
	return scalarLuaType(fd)
}

// scalarLuaType maps a proto scalar kind to a LuaLS primitive.
func scalarLuaType(fd protoreflect.FieldDescriptor) string {
	switch fd.Kind() {
	case protoreflect.StringKind, protoreflect.BytesKind:
		return "string"
	case protoreflect.BoolKind:
		return "boolean"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "number"
	case protoreflect.EnumKind:
		return "integer"
	default:
		// All int/uint/sint/fixed kinds.
		return "integer"
	}
}

// luaClassName returns the namespaced LuaLS @class name for a message descriptor,
// e.g. holomush.host.emit is the namespace; the class is suffixed with the message
// name. The token segment is derived from the message's parent package + message
// name; collisions are avoided by including the full message name.
func luaClassName(md protoreflect.MessageDescriptor) string {
	return "holomush.msg." + string(md.Name())
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestLuaTypeMapsScalarStringField ./internal/plugin/luabridge/gen/`
Expected: PASS.

- [ ] **Step 5: Add the remaining mapping cases**

Append to `luatype_test.go` (one subtest per spec §3 row), then run the whole test:

```go
func TestLuaTypeMapsRepeatedAndMessageAndOptional(t *testing.T) {
	// QueryStreamHistoryResponse.events is repeated Event (message) → Event class[].
	events := fieldByPath(t, "holomush.plugin.host.v1.QueryStreamHistoryResponse", "events")
	assert.Equal(t, "holomush.msg.Event[]", luaType(events))

	// EmitEventRequest.event_type is string.
	et := fieldByPath(t, "holomush.plugin.host.v1.EmitEventRequest", "event_type")
	assert.Equal(t, "string", luaType(et))
}
```

> Verify the exact field/message names against the real protos before asserting:
> `rg -n 'message (EmitEventRequest|QueryStreamHistoryResponse|Event)\b' api/proto/holomush/plugin/host/v1/*.proto`
> and adjust the message/field names in the test to match. The assertion VALUES
> (the LuaLS type strings) are fixed by §3; only the proto field references adapt.

Run: `task test -- -run TestLuaType ./internal/plugin/luabridge/gen/`
Expected: PASS.

- [ ] **Step 6: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`.

---

### Task 2: Ambient decl table + parity test

**Files:**

- Create: `internal/plugin/luabridge/gen/ambient.go`
- Test: `internal/plugin/luabridge/gen/ambient_parity_test.go`

Per spec §4.2 + §5.2: a structured decl table is the single source of truth for the
always-on ambient surface; a parity test holds it equal to the live registrations
built by the production entrypoint `hostfunc.Register`.

- [ ] **Step 1: Write the decl table (the data the parity test will validate)**

Create `internal/plugin/luabridge/gen/ambient.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

// ambientParam is one parameter of an ambient stdlib function.
type ambientParam struct {
	Name string
	Type string // LuaLS type string
}

// ambientFn declares one ambient host function for the stub. Module is the dotted
// path of the table the function lives on (spec §4.2). The (Module, Name) set MUST
// equal the live registration set — enforced by TestAmbientDeclTableMatchesRegistrations.
type ambientFn struct {
	Module  string // "holomush" | "holomush.config" | "holo.fmt" | "holo.emit"
	Name    string
	Doc     string
	Params  []ambientParam
	Returns []string // LuaLS return type strings
}

// ambientDecls is the single source of truth for ambient-surface annotations.
// The NAME set is verified against runtime registration (§5.2); the param/return
// types are hand-authored (no machine source exists). Authoritative ambient
// boundary: internal/plugin/hostfunc/functions.go:268-287 + RegisteredFunctionsForAudit().
// Signatures below are GROUNDED against the actual Lua arg-parsing in each
// implementation (see the source map in Step 1b). Do NOT alter a signature without
// re-reading the corresponding function — the parity test (Step 2) checks NAME
// equality only, so a wrong arity/order here ships green and misleads authors.
var ambientDecls = []ambientFn{
	// functions.go logFn:416-417 → (level, message).
	{Module: "holomush", Name: "log", Doc: "Write a structured log line.",
		Params: []ambientParam{{"level", "string"}, {"message", "string"}}},
	{Module: "holomush", Name: "new_request_id", Doc: "Return a fresh request-scoped ULID string.",
		Returns: []string{"string"}},
	// commands.go listCommandsFn:55-59 → (character_id) [ignored for authz]; returns (table, err?).
	{Module: "holomush", Name: "list_commands", Doc: "List commands visible to the subject. character_id is accepted for call-signature compatibility but IGNORED for authorization.",
		Params: []ambientParam{{"character_id", "string"}}, Returns: []string{"table", "string?"}},
	// commands.go getCommandHelpFn:145-150 → (command_name, character_id) [character_id ignored]; returns (table, err?).
	{Module: "holomush", Name: "get_command_help", Doc: "Return help info for a command. character_id is accepted for compatibility but IGNORED.",
		Params: []ambientParam{{"command_name", "string"}, {"character_id", "string"}}, Returns: []string{"table", "string?"}},
	// stdlib_streams.go addSessionStreamFn:42-44 → (session_id, stream).
	{Module: "holomush", Name: "add_session_stream", Doc: "Subscribe a session to a stream.",
		Params: []ambientParam{{"session_id", "string"}, {"stream", "string"}}},
	// stdlib_streams.go removeSessionStreamFn:70-72 → (session_id, stream).
	{Module: "holomush", Name: "remove_session_stream", Doc: "Unsubscribe a session from a stream.",
		Params: []ambientParam{{"session_id", "string"}, {"stream", "string"}}},
	// stdlib_focus.go queryStreamHistoryFn:342-345 → single table arg {stream, count?, not_before_ms?, cursor?}; returns (table, err?).
	{Module: "holomush", Name: "query_stream_history", Doc: "Read recent events from a stream. Pass a table: {stream=string, count?=integer, not_before_ms?=integer, cursor?=string}.",
		Params: []ambientParam{{"args", "table"}}, Returns: []string{"table", "string?"}},
	// stdlib_audit.go decryptOwnAuditRowsImpl:60 → (rows: array of row tables); returns (table, err?).
	{Module: "holomush", Name: "decrypt_own_audit_rows", Doc: "Decrypt audit rows the plugin owns. rows is an array of row tables.",
		Params: []ambientParam{{"rows", "table"}}, Returns: []string{"table", "string?"}},
	// functions.go:326-330 → (event_type); returns true.
	{Module: "holomush", Name: "register_emit_type", Doc: "Declare a plugin-owned event type (Load-time; INV-PLUGIN-32).",
		Params: []ambientParam{{"event_type", "string"}}, Returns: []string{"boolean"}},

	// config.go: every accessor is (key); require_* error if absent. Non-require return optional.
	{Module: "holomush.config", Name: "string", Params: []ambientParam{{"key", "string"}}, Returns: []string{"string?"}, Doc: "Read a string config value."},
	{Module: "holomush.config", Name: "require_string", Params: []ambientParam{{"key", "string"}}, Returns: []string{"string"}, Doc: "Read a required string config value (errors if absent)."},
	{Module: "holomush.config", Name: "int", Params: []ambientParam{{"key", "string"}}, Returns: []string{"integer?"}, Doc: "Read an integer config value."},
	{Module: "holomush.config", Name: "require_int", Params: []ambientParam{{"key", "string"}}, Returns: []string{"integer"}, Doc: "Read a required integer config value."},
	{Module: "holomush.config", Name: "bool", Params: []ambientParam{{"key", "string"}}, Returns: []string{"boolean?"}, Doc: "Read a boolean config value."},
	{Module: "holomush.config", Name: "require_bool", Params: []ambientParam{{"key", "string"}}, Returns: []string{"boolean"}, Doc: "Read a required boolean config value."},
	{Module: "holomush.config", Name: "duration", Params: []ambientParam{{"key", "string"}}, Returns: []string{"integer?"}, Doc: "Read a duration config value (nanoseconds)."},
	{Module: "holomush.config", Name: "require_duration", Params: []ambientParam{{"key", "string"}}, Returns: []string{"integer"}, Doc: "Read a required duration config value (nanoseconds)."},

	// stdlib.go fmt*: all (text) → string, EXCEPT color (color, text) and list/pairs/table (table arg), separator ().
	{Module: "holo.fmt", Name: "bold", Params: []ambientParam{{"text", "string"}}, Returns: []string{"string"}, Doc: "Wrap text in bold styling."},
	{Module: "holo.fmt", Name: "italic", Params: []ambientParam{{"text", "string"}}, Returns: []string{"string"}, Doc: "Wrap text in italic styling."},
	{Module: "holo.fmt", Name: "dim", Params: []ambientParam{{"text", "string"}}, Returns: []string{"string"}, Doc: "Wrap text in dim styling."},
	{Module: "holo.fmt", Name: "underline", Params: []ambientParam{{"text", "string"}}, Returns: []string{"string"}, Doc: "Wrap text in underline styling."},
	// stdlib.go fmtColor:84-85 → (color, text) — NOTE the order.
	{Module: "holo.fmt", Name: "color", Params: []ambientParam{{"color", "string"}, {"text", "string"}}, Returns: []string{"string"}, Doc: "Colorize text. Argument order is (color, text)."},
	{Module: "holo.fmt", Name: "list", Params: []ambientParam{{"items", "table"}}, Returns: []string{"string"}, Doc: "Format a list."},
	{Module: "holo.fmt", Name: "pairs", Params: []ambientParam{{"kv", "table"}}, Returns: []string{"string"}, Doc: "Format key/value pairs."},
	{Module: "holo.fmt", Name: "table", Params: []ambientParam{{"rows", "table"}}, Returns: []string{"string"}, Doc: "Format a table."},
	{Module: "holo.fmt", Name: "separator", Returns: []string{"string"}, Doc: "Return a horizontal separator."},
	{Module: "holo.fmt", Name: "header", Params: []ambientParam{{"text", "string"}}, Returns: []string{"string"}, Doc: "Format a header."},
	{Module: "holo.fmt", Name: "parse", Params: []ambientParam{{"markup", "string"}}, Returns: []string{"string"}, Doc: "Parse holo markup into rendered text."},

	// stdlib.go emitLocation:287-289 / emitCharacter:309-312 / emitGlobal:332-334 → (id..., event_type, payload table).
	{Module: "holo.emit", Name: "location", Params: []ambientParam{{"location_id", "string"}, {"event_type", "string"}, {"payload", "table"}}, Doc: "Emit an event to a location."},
	{Module: "holo.emit", Name: "character", Params: []ambientParam{{"character_id", "string"}, {"event_type", "string"}, {"payload", "table"}}, Doc: "Emit an event to a character."},
	{Module: "holo.emit", Name: "global", Params: []ambientParam{{"event_type", "string"}, {"payload", "table"}}, Doc: "Emit a global event."},
	{Module: "holo.emit", Name: "flush", Doc: "Flush buffered emit calls."},
}
```

- [ ] **Step 1b: Verify every signature against its implementation (REQUIRED — not optional)**

The parity test (Step 2) checks the function NAME set only; it cannot catch a wrong
arity, param order, or return shape. Those errors ship green and actively mislead
plugin authors — which inverts the entire value of an editor stub. So before
proceeding, open each implementation and confirm the decl-table `Params`/`Returns`
match the Lua arg-parsing exactly. Source map:

| Decl entry | Implementation | Confirm |
| --- | --- | --- |
| `holomush.log` | `functions.go` `logFn` (~:414-417) | `CheckString(1)`=level, `CheckString(2)`=message |
| `holomush.list_commands` | `commands.go` `listCommandsFn` (~:55-59) | `CheckString(1)`=character_id (ignored); returns (table, err) |
| `holomush.get_command_help` | `commands.go` `getCommandHelpFn` (~:145-150) | `CheckString(1)`=command_name, `CheckString(2)`=character_id |
| `holomush.add_session_stream` / `remove_session_stream` | `stdlib_streams.go` (~:42-44, :70-72) | `(session_id, stream)` |
| `holomush.query_stream_history` | `stdlib_focus.go` `queryStreamHistoryFn` (~:342-345) | one `CheckTable(1)` with `stream`/`count`/`not_before_ms`/`cursor` |
| `holomush.decrypt_own_audit_rows` | `stdlib_audit.go` `decryptOwnAuditRowsImpl` (~:60) | one rows-array table |
| `holomush.register_emit_type` | `functions.go` (~:326-330) | `(event_type)` → true |
| `holomush.config.*` | `config.go` (~:30-152) | every accessor is `(key)`; require_* error if absent |
| `holo.fmt.*` | `stdlib.go` `fmt*` (~:51-166) | all `(text)` EXCEPT `color`=`(color, text)` (:84-85), `list`/`pairs`/`table`=`(table)`, `separator`=`()` |
| `holo.emit.*` | `stdlib.go` `emit*` (~:286-334) | `location`/`character`=`(id, event_type, payload-table)`, `global`=`(event_type, payload-table)`, `flush`=`()` |

Any drift between the decl table and the implementation MUST be corrected in the decl
table now. (Line numbers are approximate — confirm by reading.)

- [ ] **Step 2: Write the failing parity test**

Create `internal/plugin/luabridge/gen/ambient_parity_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"sort"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// liveAmbientNames builds the production ambient surface in a real LState (via the
// SAME entrypoint production uses, hostfunc.Register — which installs holomush.* and
// calls RegisterStdlib for holo.fmt/holo.emit) and returns the set of
// "module.name" keys for every function-valued field. It MUST NOT call
// RegisterSessionFuncs (holo.session is capability-gated, out of scope — spec §1).
func liveAmbientNames(t *testing.T) map[string]bool {
	t.Helper()
	L := lua.NewState()
	defer L.Close()

	// nil KVStore is fine: Register installs no kv_* functions (kv is a retired
	// capability, host-brokered now — spec §2 authoritative boundary).
	f := hostfunc.New(nil)
	f.Register(L, "stub-gen")

	got := map[string]bool{}
	for _, global := range []string{"holomush", "holo"} {
		tbl, ok := L.GetGlobal(global).(*lua.LTable)
		require.True(t, ok, "global %q must be a table", global)
		collectFnNames(global, tbl, got)
	}
	return got
}

// collectFnNames recursively records module.name for every *LFunction field; it
// descends into subtables (e.g. holomush.config, holo.fmt) building the dotted path.
func collectFnNames(prefix string, tbl *lua.LTable, out map[string]bool) {
	tbl.ForEach(func(k, v lua.LValue) {
		name, ok := k.(lua.LString)
		if !ok {
			return
		}
		switch vv := v.(type) {
		case *lua.LFunction:
			out[prefix+"."+string(name)] = true
		case *lua.LTable:
			collectFnNames(prefix+"."+string(name), vv, out)
		}
	})
}

func declNames() map[string]bool {
	out := map[string]bool{}
	for _, d := range ambientDecls {
		out[d.Module+"."+d.Name] = true
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestAmbientDeclTableMatchesRegistrations is the §5.2 drift guard: the decl
// table's (Module, Name) set MUST equal the live ambient registration set. A
// registered ambient fn missing from the table, or a table entry with no live
// registration, fails here.
func TestAmbientDeclTableMatchesRegistrations(t *testing.T) {
	live := liveAmbientNames(t)
	decl := declNames()
	assert.Equal(t, keys(live), keys(decl),
		"ambient decl table must exactly match live holomush.*/holo.* function registrations")
}
```

- [ ] **Step 3: Run the parity test**

Run: `task test -- -run TestAmbientDeclTableMatchesRegistrations ./internal/plugin/luabridge/gen/`
Expected: PASS if the decl table (Step 1) is complete and correct. If it FAILS, the
assertion diff names the exact missing/extra `module.name` entries — reconcile the
decl table against the live set (add missing, remove extra) until green. This is the
intended TDD loop: the live runtime is the source of truth for the name set.

- [ ] **Step 4: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`.

---

### Task 3: Message-class collection for the descriptor surface

**Files:**

- Modify: `internal/plugin/luabridge/gen/main.go` (extend `methodData` with `ResponseGoType`)
- Create: `internal/plugin/luabridge/gen/luastub.go` (message collection; emission added in Task 4)
- Test: `internal/plugin/luabridge/gen/luastub_test.go`

The existing `methodData` carries only `RequestGoType`. The stub needs the response
type and the full set of messages (transitively) reachable as a request/response, to
emit one `---@class` per message.

- [ ] **Step 1: Extend methodData with the response type**

In `internal/plugin/luabridge/gen/main.go`, modify the `methodData` struct (currently
`internal/plugin/luabridge/gen/main.go:104-107`) and `collectMethods` (`:175-197`):

```go
// methodData is the template input for one unary RPC.
type methodData struct {
	GoName        string // e.g. Get
	RequestGoType string // e.g. GetRequest
	ResponseGoType string // e.g. GetResponse
}
```

In `collectMethods`, set the response type alongside the request:

```go
		out = append(out, methodData{
			GoName:         string(md.Name()),
			RequestGoType:  reqGoType,
			ResponseGoType: goTypeName(md.Output().Name()),
		})
```

- [ ] **Step 2: Write the failing message-collection test**

Create `internal/plugin/luabridge/gen/luastub_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectStubMessagesIncludesRequestAndResponse(t *testing.T) {
	services, err := collectServices()
	require.NoError(t, err)

	msgs, err := collectStubMessages(services)
	require.NoError(t, err)

	names := map[string]bool{}
	for _, m := range msgs {
		names[m.ClassName] = true
	}
	// EmitService.EmitEvent has request EmitEventRequest + response EmitEventResponse.
	assert.True(t, names["holomush.msg.EmitEventRequest"], "request message class present")
	assert.True(t, names["holomush.msg.EmitEventResponse"], "response message class present")
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `task test -- -run TestCollectStubMessages ./internal/plugin/luabridge/gen/`
Expected: FAIL — `undefined: collectStubMessages`.

- [ ] **Step 4: Implement collectStubMessages**

Create `internal/plugin/luabridge/gen/luastub.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// stubField is one field of a message class in the stub.
type stubField struct {
	Name     string // proto field name (snake_case, as Lua sees it)
	Type     string // LuaLS type string
	Optional bool   // emitted as "name?"
	Doc      string // leading proto comment, if any
}

// stubMessage is one generated ---@class for a proto message.
type stubMessage struct {
	ClassName string
	Fields    []stubField
}

// collectStubMessages walks every request/response message of the collected
// services, transitively following message-typed fields, and returns one
// stubMessage per distinct message. Oneof variants are flattened to optional
// fields (spec §3).
func collectStubMessages(services []serviceData) ([]stubMessage, error) {
	seen := map[string]bool{}
	var out []stubMessage

	var visit func(md protoreflect.MessageDescriptor)
	visit = func(md protoreflect.MessageDescriptor) {
		cn := luaClassName(md)
		if seen[cn] {
			return
		}
		seen[cn] = true

		var fields []stubField
		fs := md.Fields()
		for i := 0; i < fs.Len(); i++ {
			fd := fs.Get(i)
			optional := fd.HasOptionalKeyword() || fd.ContainingOneof() != nil
			fields = append(fields, stubField{
				Name:     string(fd.Name()),
				Type:     luaType(fd),
				Optional: optional,
			})
			if (fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind) && !fd.IsMap() {
				visit(fd.Message())
			}
			if fd.IsMap() {
				mv := fd.MapValue()
				if mv.Kind() == protoreflect.MessageKind {
					visit(mv.Message())
				}
			}
		}
		out = append(out, stubMessage{ClassName: cn, Fields: fields})
	}

	for _, sd := range services {
		desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(sd.ServiceName))
		if err != nil {
			return nil, err
		}
		svc := desc.(protoreflect.ServiceDescriptor)
		methods := svc.Methods()
		for i := 0; i < methods.Len(); i++ {
			m := methods.Get(i)
			if m.IsStreamingClient() || m.IsStreamingServer() {
				continue
			}
			visit(m.Input())
			visit(m.Output())
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ClassName < out[j].ClassName })
	return out, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestCollectStubMessages ./internal/plugin/luabridge/gen/`
Expected: PASS.

- [ ] **Step 6: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`.

---

## Phase 2: Emission, wiring, and the committed artifact

### Task 4: Render the `---@meta` stub

**Files:**

- Modify: `internal/plugin/luabridge/gen/luastub.go` (add `renderLuaStub`)
- Test: `internal/plugin/luabridge/gen/luastub_test.go` (extend)

- [ ] **Step 1: Write the failing render test**

Append to `internal/plugin/luabridge/gen/luastub_test.go`:

```go
import "strings" // add to the import block

func TestRenderLuaStubProducesMetaAndNamespaces(t *testing.T) {
	services, err := collectServices()
	require.NoError(t, err)
	msgs, err := collectStubMessages(services)
	require.NoError(t, err)

	out, err := renderLuaStub(services, msgs, ambientDecls)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(out, "---@meta"), "stub MUST begin with ---@meta")
	assert.Contains(t, out, "---@class holomush.host.emit")
	assert.Contains(t, out, "function emit.EmitEvent(req)")
	assert.Contains(t, out, "---@class holomush.msg.EmitEventRequest")
	assert.Contains(t, out, "---@class holomush.config")
	assert.Contains(t, out, "function holo.fmt.bold(text)")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestRenderLuaStub ./internal/plugin/luabridge/gen/`
Expected: FAIL — `undefined: renderLuaStub`.

- [ ] **Step 3: Implement renderLuaStub**

Append to `internal/plugin/luabridge/gen/luastub.go` (add `bytes`, `fmt`, `strings`,
`text/template` to its import block):

```go
// luaStubTmpl renders the full ---@meta definition file. Capability namespaces are
// emitted as bare globals (spec §3 naming note); messages as @class; ambient globals
// grouped by Module.
const luaStubTmpl = `---@meta holomush
-- Code generated by internal/plugin/luabridge/gen; DO NOT EDIT.
-- Regenerate with: go generate ./internal/plugin/luabridge/...
-- Editor setup: point lua-language-server's Lua.workspace.library at pkg/plugin/luastubs/

{{range .Messages}}
---@class {{.ClassName}}
{{- range .Fields}}
---@field {{.Name}}{{if .Optional}}?{{end}} {{.Type}}{{if .Doc}} {{.Doc}}{{end}}
{{- end}}
{{end}}
{{range .Services}}
---@class holomush.host.{{.Token}}
{{.Token}} = {}
{{- range .Methods}}
---@param req {{$.ClassPrefix}}{{.RequestGoType}}
---@return {{$.ClassPrefix}}{{.ResponseGoType}}
function {{.Token}}.{{.GoName}}(req) end
{{- end}}
{{end}}
{{range .AmbientModules}}
---@class {{.Module}}
{{.Module}} = {}
{{- range .Fns}}
---{{.Doc}}
{{- range .Params}}
---@param {{.Name}} {{.Type}}
{{- end}}
{{- range .Returns}}
---@return {{.}}
{{- end}}
function {{.Parent}}.{{.Name}}({{paramList .Params}}) end
{{- end}}
{{end}}`

// ambientModule groups ambientFns by their Module for templating.
type ambientModule struct {
	Module string
	Fns    []ambientFnTmpl
}

type ambientFnTmpl struct {
	ambientFn
	Parent string // the table identifier the function is set on (== Module)
}

// methodTmpl carries the Token onto each method so the template can name the global.
type serviceTmpl struct {
	serviceData
}

func renderLuaStub(services []serviceData, messages []stubMessage, ambient []ambientFn) (string, error) {
	// Group ambient fns by module, preserving the decl-table order.
	order := []string{}
	byMod := map[string][]ambientFnTmpl{}
	for _, d := range ambient {
		if _, ok := byMod[d.Module]; !ok {
			order = append(order, d.Module)
		}
		byMod[d.Module] = append(byMod[d.Module], ambientFnTmpl{ambientFn: d, Parent: d.Module})
	}
	mods := make([]ambientModule, 0, len(order))
	for _, m := range order {
		mods = append(mods, ambientModule{Module: m, Fns: byMod[m]})
	}

	data := struct {
		ClassPrefix    string
		Messages       []stubMessage
		Services       []serviceData
		AmbientModules []ambientModule
	}{
		ClassPrefix:    "holomush.msg.",
		Messages:       messages,
		Services:       services,
		AmbientModules: mods,
	}

	tmpl := template.Must(template.New("luastub").Funcs(template.FuncMap{
		"paramList": func(ps []ambientParam) string {
			names := make([]string, 0, len(ps))
			for _, p := range ps {
				names = append(names, p.Name)
			}
			return strings.Join(names, ", ")
		},
	}).Parse(luaStubTmpl))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering lua stub: %w", err)
	}
	return buf.String(), nil
}
```

> Note: the template references `.Token` on methods via the parent service range
> (`{{.Token}}.{{.GoName}}`), which works because Go templates re-scope `.` inside
> `range .Services` to each `serviceData` (which has `Token`); the inner
> `range .Methods` re-scopes again, so the method block uses `{{$.ClassPrefix}}` for
> the prefix and the service's `.Token` is captured by emitting the function line
> inside the service scope. If the template engine complains about `.Token` inside
> the method range, hoist the token via a `with` or restructure to a precomputed
> `[]serviceStub{Token, []methodStub{Global, Name, ReqClass, RespClass}}` — adjust
> until `TestRenderLuaStub` passes.

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestRenderLuaStub ./internal/plugin/luabridge/gen/`
Expected: PASS. Iterate on the template until the structural assertions hold.

- [ ] **Step 5: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`.

---

### Task 5: Wire the emitter into `main()` and produce the committed artifact

**Files:**

- Modify: `internal/plugin/luabridge/gen/main.go` (`main()`, around `:109-132`)
- Modify: `internal/plugin/luabridge/doc.go`
- Create (generated): `pkg/plugin/luastubs/holomush.lua`

- [ ] **Step 1: Emit the stub from main()**

In `internal/plugin/luabridge/gen/main.go`, after the existing block that writes
`bindings_gen.go` (ends `internal/plugin/luabridge/gen/main.go:131` with the
`fmt.Printf("wrote %s ...")`), add:

```go
	// Second output: the LuaLS editor stub (holomush-eykuh.9). Off the runtime
	// path — purely for editor autocomplete/hover.
	messages, err := collectStubMessages(services)
	if err != nil {
		log.Fatalf("collecting stub messages: %v", err)
	}
	stub, err := renderLuaStub(services, messages, ambientDecls)
	if err != nil {
		log.Fatalf("rendering lua stub: %v", err)
	}
	stubPath := filepath.Join(root, "pkg", "plugin", "luastubs", "holomush.lua")
	if mkErr := os.MkdirAll(filepath.Dir(stubPath), 0o755); mkErr != nil {
		log.Fatalf("creating luastubs dir: %v", mkErr)
	}
	if wErr := os.WriteFile(stubPath, []byte(stub), 0o600); wErr != nil {
		log.Fatalf("writing %s: %v", stubPath, wErr)
	}
	fmt.Printf("wrote %s\n", stubPath)
```

- [ ] **Step 2: Update the package doc**

In `internal/plugin/luabridge/doc.go`, extend the generated-artifacts description to
mention the second output (`pkg/plugin/luastubs/holomush.lua`, the LuaLS editor stub).
Keep it factual; one or two sentences.

- [ ] **Step 3: Generate the committed artifact**

Run: `go generate ./internal/plugin/luabridge/...`
Expected: prints `wrote .../bindings_gen.go (N services)` and `wrote .../pkg/plugin/luastubs/holomush.lua`. Confirm the file exists:

Run: `test -f pkg/plugin/luastubs/holomush.lua && head -20 pkg/plugin/luastubs/holomush.lua`
Expected: file exists; begins with `---@meta holomush` and the generated header.

- [ ] **Step 4: Verify nothing else regressed**

Run: `task test -- ./internal/plugin/luabridge/...`
Expected: PASS (bindings_gen.go output is unchanged; only the new file added).

- [ ] **Step 5: Commit** (include the generated `pkg/plugin/luastubs/holomush.lua`)

Commit using VCS-appropriate commands per `references/vcs-preamble.md`.

---

### Task 6: Structural sanity test for the generated stub

**Files:**

- Test: `internal/plugin/luabridge/gen/luastub_test.go` (extend)

Per spec §5.4: assert the rendered stub is structurally valid — begins with
`---@meta`, every `@class` referenced by a `@field`/`@param`/`@return` is declared,
no field has an empty type.

- [ ] **Step 1: Write the structural test**

Append to `internal/plugin/luabridge/gen/luastub_test.go`:

```go
import "regexp" // add to the import block

func TestRenderedStubIsStructurallyValid(t *testing.T) {
	services, err := collectServices()
	require.NoError(t, err)
	msgs, err := collectStubMessages(services)
	require.NoError(t, err)
	out, err := renderLuaStub(services, msgs, ambientDecls)
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(out, "---@meta"))

	// Collect declared classes.
	declRe := regexp.MustCompile(`(?m)^---@class (\S+)`)
	declared := map[string]bool{}
	for _, m := range declRe.FindAllStringSubmatch(out, -1) {
		declared[m[1]] = true
	}

	// Every referenced holomush.msg.* class MUST be declared.
	refRe := regexp.MustCompile(`holomush\.msg\.\w+`)
	for _, ref := range refRe.FindAllString(out, -1) {
		assert.Truef(t, declared[ref], "referenced class %q is not declared", ref)
	}

	// No @field / @param with an empty type (would be "---@field name" with no type token).
	assert.NotRegexp(t, regexp.MustCompile(`(?m)^---@(field|param)\s+\S+\s*$`), out,
		"every @field/@param MUST carry a type")
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `task test -- -run TestRenderedStubIsStructurallyValid ./internal/plugin/luabridge/gen/`
Expected: PASS. If a referenced class is undeclared, `collectStubMessages` missed a
transitive message — extend the walk (Task 3) until green.

- [ ] **Step 3: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`.

---

## Phase 3: Drift guard and documentation

### Task 7: Extend the pr-prep drift gate to cover the stub

**Files:**

- Modify: `Taskfile.yaml` (`generate:luabridge` `generates:` list `:382-395`; both
  drift-check blocks `:837-846` and `:968-975`)

Per spec §5.1: `go run ./gen` leaving EITHER `bindings_gen.go` OR
`pkg/plugin/luastubs/holomush.lua` modified MUST fail the check.

- [ ] **Step 1: Add the stub to the generate task's outputs**

In `Taskfile.yaml`, the `generate:luabridge` task's `generates:` block (currently
lists `internal/plugin/luabridge/bindings_gen.go` at `:395`) — add the stub path:

```yaml
    generates:
      - internal/plugin/luabridge/bindings_gen.go
      - pkg/plugin/luastubs/holomush.lua
```

- [ ] **Step 2: Extend both drift-check blocks**

There are two copies of the "Verifying Lua host-cap bindings are current" block
(`Taskfile.yaml:837-846` in the fast lane and `:968-975` in `pr-prep:run`). Each
computes a sha of `BINDINGS=internal/plugin/luabridge/bindings_gen.go` before/after
`task generate:luabridge` and fails on diff. In **each** block, also guard the stub —
change the shell body to hash both files. Concretely, replace the single-file check
with:

```bash
          BINDINGS=internal/plugin/luabridge/bindings_gen.go
          STUB=pkg/plugin/luastubs/holomush.lua
          BEFORE=$(sha256sum "$BINDINGS" "$STUB" | sha256sum)
          go generate ./internal/plugin/luabridge/...
          AFTER=$(sha256sum "$BINDINGS" "$STUB" | sha256sum)
          if [ "$BEFORE" != "$AFTER" ]; then
            echo "ERROR: Lua bindings or editor stub out of sync. Run 'task generate:luabridge' and commit."
            exit 1
          fi
```

> Read the exact current shell of each block first (`sed -n '837,846p' Taskfile.yaml`
> and `sed -n '968,975p' Taskfile.yaml`) and preserve its invocation style (it may
> call `task generate:luabridge` rather than `go generate`; keep whichever it uses).
> Apply the same two-file hashing to both copies.

- [ ] **Step 3: Verify the gate passes clean**

Run: `task generate:luabridge && git diff --quiet pkg/plugin/luastubs/holomush.lua internal/plugin/luabridge/bindings_gen.go && echo CLEAN`
Expected: `CLEAN` (regeneration is idempotent on a committed-up-to-date tree).

- [ ] **Step 4: Verify the gate CATCHES drift**

Run: `printf '\n-- drift\n' >> pkg/plugin/luastubs/holomush.lua && task generate:luabridge && git diff --quiet pkg/plugin/luastubs/holomush.lua && echo CLEAN || echo "DRIFT CAUGHT"`
Expected: `DRIFT CAUGHT` (regeneration overwrites the manual edit; the diff is dirty until committed). Then restore: `git checkout pkg/plugin/luastubs/holomush.lua` (or re-run generate).

- [ ] **Step 5: Run the bats lane to confirm Taskfile YAML is valid**

Run: `task lint:yaml`
Expected: PASS (no YAML syntax error introduced).

- [ ] **Step 6: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`.

---

### Task 8: Contributor documentation

**Files:**

- Create: `site/src/content/docs/extending/how-to/lua-editor-setup.md`

- [ ] **Step 1: Write the doc**

Create the doc in `site/src/content/docs/extending/how-to/` (the `extending/` dir is
organized into `explanation/`/`how-to/`/`reference/`/`tutorials/` subdirs — an editor-
setup page is a how-to). Match an existing `how-to/` sibling's **file extension**
(`.md` vs `.mdx`) and Starlight frontmatter shape (`title`/`description` keys); adjust
the filename extension accordingly. Content MUST cover:

- What the stub is (a generated LuaLS `---@meta` definition file for the Lua host-call
  surface; editor-only, not loaded at runtime).
- The setting: point lua-language-server at the stub directory, e.g.

  ```jsonc
  // .luarc.json (or editor settings)
  { "workspace.library": ["pkg/plugin/luastubs"] }
  ```

- That it is generated (`task generate:luabridge`) and committed; authors do not edit
  it by hand.
- Scope note: covers `host.v1` capability namespaces (`emit`, `kv`, `focus`, …) and
  the ambient `holomush.*` / `holo.*` stdlib; does NOT cover provider (plugin→plugin)
  services or the capability-gated `holo.session.*` (out of scope per the design).

- [ ] **Step 2: Verify docs lint**

Run: `task lint:markdown`
Expected: PASS.

- [ ] **Step 3: Verify the docs IA / symmetry check**

Run: `task lint:docs-symmetry`
Expected: PASS. If the new page must be registered in a nav/sidebar config, add it
where sibling `extending/` pages are listed and re-run.

- [ ] **Step 4: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`.

---

## Verification (whole-feature)

- [ ] `task test -- ./internal/plugin/luabridge/...` — all generator unit + parity + structural tests pass.
- [ ] `task generate:luabridge` then `git diff --quiet` on both generated files — idempotent (no drift).
- [ ] `task lint` — YAML, markdown, docs-symmetry green.
- [ ] `task pr-prep` — fast lane green (includes the extended bindings/stub drift block).
- [ ] Manual spot-check: open `pkg/plugin/luastubs/holomush.lua`; confirm it begins with `---@meta`, declares `emit`/`kv`/… namespaces, the `holomush.msg.*` classes, and the `holomush`/`holomush.config`/`holo.fmt`/`holo.emit` ambient tables; confirm `holo.session.*` and `kv_*` are ABSENT.
<!-- adr-capture: sha256=03e645456223de5c; session=cli; ts=2026-06-16T15:09:50Z; adrs=holomush-vhz3h,holomush-nc5v1 -->
