<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Communication Content Contract — Slice 1 (Broadcast Core) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish the `holomush.comm.v1.CommunicationContent` payload contract and migrate the broadcast conversational verbs (`say`/`pose`/`ooc`/`emit`) in both plugins to emit it, unblocking `holomush-g1qcw`.

**Architecture:** A new proto content body + `protovalidate` rules; a single-source Go grammar/builder in `pkg/plugin/comm` reached by Go plugins directly and by Lua plugins through a `holo.comm.*` hostfunc (so both runtimes converge on one implementation); a `ContentValidationPublisher` chain link that is *built and unit-tested but NOT wired live* this slice; and backward-compatible consumer updates so migrated and un-migrated verbs both render during the transition.

**Tech Stack:** Go, buf + protovalidate, gopher-lua hostfuncs, the eventbus publisher chain, the invariant registry (`cmd/inv-render`).

**Spec:** `docs/superpowers/specs/2026-07-03-communication-content-contract-design.md` (`holomush-kk1ot`, design-reviewer READY).

---

## Two planning findings folded into this plan

1. **Gate is built, not live, in Slice 1.** The gate keys on `category: communication`, which *also* covers the still-unmigrated `page`/`whisper`/`whisper_notice`. Wiring it live now would reject them. So Task 3 builds + unit-tests `ContentValidationPublisher` but does **not** wire it into `sub_grpc.go`; live enforcement (and INV-COMM-1 binding) is Slice 2. (Spec §8 updated.)
2. **Consumers need a backward-compatible field-name update (Task 6).** `CommunicationContent` renames `character_name`→`actor_display_name` and `style`→`ooc_style`; `translate.go:84-98` and the telnet gateway read the old names. Task 6 makes both consumers read new→old (fallback) so migrating any single verb never breaks rendering, and un-migrated verbs keep working.

**Out of scope (Slice 1):** `page`/`whisper`/`pemit` migration, `pemit`/`whisper_notice` recategorization, live gate wiring, `sender_display_name` (all Slice 2). Realizing `no_space` in the **web scene renderer** (`logEntryToLine`/`LogEntry` plumbing) is a rendering-convergence follow-up — Slice 1 fixes the **data** contract (scene events now *carry* `no_space`), which is what g1qcw needs.

## File structure

| Path | Responsibility | Task |
| --- | --- | --- |
| `api/proto/holomush/comm/v1/comm.proto` (create) | `CommunicationContent` message + validate rules | 1 |
| `pkg/proto/holomush/comm/v1/*.pb.go` (generated) | Go type | 1 |
| `pkg/plugin/comm/grammar.go` + `builder.go` (create) | single-source sigil grammar + JSON builder | 2 |
| `internal/eventbus/content_publisher.go` (create) | `ContentValidationPublisher` (built, not wired) | 3 |
| `internal/plugin/hostfunc/stdlib_comm.go` (create) | `holo.comm.*` Lua binding → `pkg/plugin/comm` | 4 |
| `internal/web/translate.go` (modify) | new→old field fallback | 6 |
| `internal/telnet/gateway_handler.go` (modify) | new→old field fallback | 6 |
| `plugins/core-scenes/commands.go:1305-1323` (modify) | `handleEmit` → builder, preserve `actor_id` | 7 |
| `plugins/core-communication/main.lua` (modify) | `say`/`pose`/`ooc`/`emit` → `holo.comm.*` | 9 |
| `docs/architecture/invariants.yaml` (modify) | COMM boundary + INV-COMM-1/2 | 10 |

---

### Task 1: Proto — `holomush.comm.v1.CommunicationContent`

**Files:**

- Create: `api/proto/holomush/comm/v1/comm.proto`
- Generated: `pkg/proto/holomush/comm/v1/comm.pb.go`

- [ ] **Step 1: Write the proto** (every element needs a Go-grounded doc comment per `.claude/rules/proto-doc-comments.md`; buf `COMMENTS` is enforced):

```proto
// SPDX header is added by `task fmt` (license-eye); do not hand-write it.
syntax = "proto3"; // matches all 25 existing api/proto protos; NOT edition 2023

package holomush.comm.v1;

import "buf/validate/validate.proto";

option go_package = "github.com/holomush/holomush/pkg/proto/holomush/comm/v1;commv1";

// CommunicationContent is the canonical instance-level payload body for every
// real-time conversational-content event (say/pose/ooc/emit; page/whisper/pemit
// arrive in Slice 2). It is the per-emit body the type-level rendering hints
// stamped by RenderingPublisher (kind via Format, label) render over. Producers
// build it via pkg/plugin/comm; the ContentValidationPublisher validates it.
message CommunicationContent {
  // actor_id is the stable character ULID of the author. Scene replay/export/
  // publish-snapshot decoders read it self-contained from the payload to
  // populate PublishedSceneEntry.Speaker (plugins/core-scenes/commands.go
  // decodeReplayEntries; publish_snapshot.go decodeSnapshotEntry). Empty only
  // for the actorless `emit` verb; never validated as required (emit).
  string actor_id = 1;

  // actor_display_name is the resolved author name for rendering. Empty when
  // name resolution is deferred (scenes today) or for actorless `emit`; the
  // renderer then falls back to actor_id.
  string actor_display_name = 2;

  // text is the raw, unrendered content ("waves", "Hello there."). Required
  // non-empty. The renderer produces the surface form; producers MUST NOT
  // pre-render (e.g. no "Alaric waves" here).
  string text = 3 [(buf.validate.field).string.min_len = 1];

  // no_space renders the actor and text with no separating space (the ";"
  // semipose form -> "Alaric's eyes narrow"). Default false.
  bool no_space = 4;

  // ooc_style selects the OOC surface form for ooc events: "" (default, treated
  // as "say") / "say" / "pose" / "semipose". Empty for non-ooc kinds.
  string ooc_style = 5 [(buf.validate.field).string.in = ["", "say", "pose", "semipose"]];
}
```

- [ ] **Step 2: Generate + verify.** Run: `task proto && task web:generate`. Expected: `pkg/proto/holomush/comm/v1/comm.pb.go` created; `task lint:proto` green.
- [ ] **Step 3: Commit** the proto + generated files together (per CLAUDE.md "Generated code" — same change or CI fails stale-diff).

---

### Task 2: Go grammar + builder — `pkg/plugin/comm`

**Files:**

- Create: `pkg/plugin/comm/grammar.go`, `pkg/plugin/comm/builder.go`
- Test: `pkg/plugin/comm/comm_test.go`

- [ ] **Step 1: Write the failing test** (`comm_test.go`):

```go
func TestParsePoseSetsNoSpaceForSemipose(t *testing.T) {
	got := comm.ParsePose(";", "waves") // invokedAs ";", raw "waves"
	require.Equal(t, "waves", got.Text)
	require.True(t, got.NoSpace)
}

func TestParsePoseStripsLeadingSigilFromRaw(t *testing.T) {
	got := comm.ParsePose("", ":waves") // no alias; sigil embedded in raw
	require.Equal(t, "waves", got.Text)
	require.False(t, got.NoSpace)
}

func TestParseOOCStyleFromPrefix(t *testing.T) {
	require.Equal(t, "pose", comm.ParseOOC(":laughs").Style)
	require.Equal(t, "semipose", comm.ParseOOC(";'s data is gone").Style)
	require.Equal(t, "say", comm.ParseOOC("brb").Style)
}

func TestBuildPoseProducesValidCommunicationContent(t *testing.T) {
	payload := comm.Pose(comm.Author{ID: "01H...", Name: "Alaric"}, ";", "waves") // returns string
	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(payload), &got))
	require.Equal(t, "01H...", got.GetActorId())
	require.Equal(t, "waves", got.GetText())
	require.True(t, got.GetNoSpace())
}
```

- [ ] **Step 2: Run to verify it fails.** Run: `task test -- ./pkg/plugin/comm/`. Expected: FAIL (package/functions undefined).
- [ ] **Step 3: Implement** `grammar.go` (the single-source sigil/style rules, mirroring `plugins/core-communication/main.lua` handle_pose/handle_ooc so both runtimes share ONE implementation):

```go
package comm

import "strings"

type Author struct{ ID, Name string }
type PoseParse struct{ Text string; NoSpace bool }
type OOCParse struct{ Text, Style string }

// ParsePose applies the ";"/":" pose grammar. invokedAs is the alias that fired
// (";" -> semipose/no-space, ":" -> pose); when empty, a leading sigil in raw is
// honored instead (mirrors main.lua handle_pose).
func ParsePose(invokedAs, raw string) PoseParse {
	a := strings.TrimSpace(raw)
	switch invokedAs {
	case ";":
		return PoseParse{Text: a, NoSpace: true}
	case ":":
		return PoseParse{Text: a, NoSpace: false}
	}
	if strings.HasPrefix(a, ";") {
		return PoseParse{Text: strings.TrimSpace(a[1:]), NoSpace: true}
	}
	if strings.HasPrefix(a, ":") {
		return PoseParse{Text: strings.TrimSpace(a[1:]), NoSpace: false}
	}
	return PoseParse{Text: a}
}

// ParseOOC classifies the OOC style from a leading sigil (mirrors handle_ooc).
func ParseOOC(raw string) OOCParse {
	m := strings.TrimSpace(raw)
	if strings.HasPrefix(m, ":") {
		return OOCParse{Text: strings.TrimSpace(m[1:]), Style: "pose"}
	}
	if strings.HasPrefix(m, ";") {
		return OOCParse{Text: strings.TrimSpace(m[1:]), Style: "semipose"}
	}
	return OOCParse{Text: m, Style: "say"}
}
```

- [ ] **Step 4: Implement** `builder.go` (marshals via `protojson` with `UseProtoNames: true` so the wire JSON is snake_case, matching `renderingJSONOpts` at `rendering_publisher.go:21-25` and what Task 3's gate expects):

```go
package comm

import (
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	commv1 "github.com/holomush/holomush/pkg/proto/holomush/comm/v1"
)

var marshal = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}

// build marshals to snake_case JSON and returns a string — EmitIntent.Payload
// (pkg/plugin/event.go:120) and Event.Payload (event.go:80) are string, and Lua
// LString is a string, so returning string avoids a conversion at every caller.
func build(c *commv1.CommunicationContent) string {
	b, err := marshal.Marshal(c) // marshal of an in-hand proto cannot fail on valid fields
	if err != nil {
		panic("comm.build: " + err.Error())
	}
	return string(b)
}

func Say(a Author, text string) string {
	return build(&commv1.CommunicationContent{ActorId: a.ID, ActorDisplayName: a.Name, Text: strings.TrimSpace(text)})
}

func Pose(a Author, invokedAs, raw string) string {
	p := ParsePose(invokedAs, raw)
	return build(&commv1.CommunicationContent{ActorId: a.ID, ActorDisplayName: a.Name, Text: p.Text, NoSpace: p.NoSpace})
}

func OOC(a Author, raw string) string {
	p := ParseOOC(raw)
	return build(&commv1.CommunicationContent{ActorId: a.ID, ActorDisplayName: a.Name, Text: p.Text, OocStyle: p.Style})
}

func Emit(text string) string {
	return build(&commv1.CommunicationContent{Text: strings.TrimSpace(text)})
}
```

- [ ] **Step 5: Run to verify pass.** Run: `task test -- ./pkg/plugin/comm/`. Expected: PASS.
- [ ] **Step 6: Commit.**

---

### Task 3: `ContentValidationPublisher` (built, NOT wired live)

**Files:**

- Create: `internal/eventbus/content_publisher.go`
- Test: `internal/eventbus/content_publisher_test.go`

- [ ] **Step 1: Write the failing test:**

```go
func TestContentValidationRejectsNonConformingCommunicationPayload(t *testing.T) {
	inner := &recordingPublisher{}
	p := eventbus.NewContentValidationPublisher(inner)
	ev := eventbus.Event{
		Type:      "core-communication:say",
		Rendering: &eventbus.RenderingMetadata{Category: "communication"},
		Payload:   []byte(`{"sender_name":"X","message":"hi"}`), // no text -> invalid
	}
	err := p.Publish(context.Background(), ev)
	require.Error(t, err)
	require.Equal(t, "EMIT_CONTENT_INVALID", oops.AsOops(err).Code())
	require.Zero(t, inner.count) // did NOT forward
}

func TestContentValidationPassesConformingPayload(t *testing.T) {
	inner := &recordingPublisher{}
	p := eventbus.NewContentValidationPublisher(inner)
	ev := eventbus.Event{
		Type:      "core-communication:say",
		Rendering: &eventbus.RenderingMetadata{Category: "communication"},
		Payload:   []byte(`{"actor_id":"01H","actor_display_name":"Alaric","text":"hi"}`),
	}
	require.NoError(t, p.Publish(context.Background(), ev))
	require.Equal(t, 1, inner.count)
}

func TestContentValidationIgnoresNonCommunicationCategory(t *testing.T) {
	inner := &recordingPublisher{}
	p := eventbus.NewContentValidationPublisher(inner)
	ev := eventbus.Event{
		Type:      "plugin_integrity_violation",
		Rendering: &eventbus.RenderingMetadata{Category: "system"},
		Payload:   []byte(`{"anything":true}`),
	}
	require.NoError(t, p.Publish(context.Background(), ev)) // pass-through, no validation
	require.Equal(t, 1, inner.count)
}
```

- [ ] **Step 2: Run to verify fail.** Run: `task test -- ./internal/eventbus/ -run TestContentValidation`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement** `content_publisher.go`:

```go
package eventbus

import (
	"context"
	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/encoding/protojson"
	commv1 "github.com/holomush/holomush/pkg/proto/holomush/comm/v1"
	"github.com/samber/oops"
)

// ContentValidationPublisher validates the payload of category:communication
// events against holomush.comm.v1.CommunicationContent at emit. It decodes the
// UNTRUSTED plugin JSON (snake_case, unknown-tolerant) then protovalidates —
// strictly more than RenderingPublisher.validateRendering, which validates a
// host-constructed proto. Built this slice; NOT wired into the live chain until
// the whole category:communication family conforms (spec §8).
type ContentValidationPublisher struct {
	inner     Publisher
	validator protovalidate.Validator
	unmarshal protojson.UnmarshalOptions
}

func NewContentValidationPublisher(inner Publisher) *ContentValidationPublisher {
	if inner == nil {
		panic("eventbus.NewContentValidationPublisher: inner publisher is nil")
	}
	v, err := protovalidate.New()
	if err != nil {
		panic("eventbus.NewContentValidationPublisher: " + err.Error())
	}
	return &ContentValidationPublisher{
		inner:     inner,
		validator: v,
		unmarshal: protojson.UnmarshalOptions{DiscardUnknown: true},
	}
}

func (p *ContentValidationPublisher) Publish(ctx context.Context, event Event) error {
	if event.Rendering == nil || event.Rendering.Category != "communication" {
		return p.inner.Publish(ctx, event)
	}
	var msg commv1.CommunicationContent
	if err := p.unmarshal.Unmarshal(event.Payload, &msg); err != nil {
		return oops.Code("EMIT_CONTENT_INVALID").With("event_type", string(event.Type)).Wrap(err)
	}
	if err := p.validator.Validate(&msg); err != nil {
		return oops.Code("EMIT_CONTENT_INVALID").With("event_type", string(event.Type)).Wrap(err)
	}
	return p.inner.Publish(ctx, event)
}
```

- [ ] **Step 4: Run to verify pass.** Run: `task test -- ./internal/eventbus/ -run TestContentValidation`. Expected: PASS.
- [ ] **Step 5: Commit.** (Note in the commit body: NOT wired into `sub_grpc.go` — Slice 2 wires it live.)

---

### Task 4: Lua binding — `holo.comm.*` (ambient stdlib, NOT the capability path)

**Files:**

- Create: `internal/plugin/hostfunc/stdlib_comm.go` (a `registerComm` helper)
- Modify: `internal/plugin/hostfunc/stdlib.go:18-29` (`RegisterStdlib` — add the `registerComm` call next to `registerFmt`/`registerEmit`)
- Test: `internal/plugin/hostfunc/stdlib_comm_test.go`

> **Registration site (grounded — this was a plan-review Critical fix).** `holo.comm.*`
> is a pure-computation helper like `holo.fmt.*`, so it registers through the ambient
> `RegisterStdlib` path (`stdlib.go:18`, invoked from `functions.go:304`), NOT through
> `RegisterSessionFuncs` — that is a **retired, test-only** capability shim
> (`export_test.go:21-36` doc: "MUST NOT be used in production code"; session moved to
> the host-brokered `luabridge.RegisterHostCaps` path). Wiring `holo.comm` anywhere but
> `RegisterStdlib` makes it unreachable in production Lua, and Task 9 would fail at
> runtime.

- [ ] **Step 1: Write the failing test** (drives the *production* registration via `RegisterStdlib`):

```go
func TestHoloCommPoseReturnsConformingPayload(t *testing.T) {
	ls := lua.NewState(); defer ls.Close()
	hostfunc.RegisterStdlib(ls) // sets global `holo`, now incl. holo.comm
	require.NoError(t, ls.DoString(`payload = holo.comm.pose("01H", "Alaric", ";", "waves")`))
	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(ls.GetGlobal("payload").String()), &got))
	require.Equal(t, "waves", got.GetText())
	require.True(t, got.GetNoSpace())
}
```

- [ ] **Step 2: Run to verify fail.** Run: `task test -- ./internal/plugin/hostfunc/ -run TestHoloComm`. Expected: FAIL (`holo.comm` is nil).
- [ ] **Step 3: Implement** `stdlib_comm.go` — a `registerComm` helper (lowercase, mirroring `registerFmt`/`registerEmit`) whose closures call `pkg/plugin/comm` (the single Go source; builders return `string`, so no `[]byte` conversion at the Lua boundary):

```go
package hostfunc

import (
	lua "github.com/yuin/gopher-lua"
	"github.com/holomush/holomush/pkg/plugin/comm"
)

// registerComm sets up the holo.comm.* namespace: pose/say/ooc/emit each return
// the CommunicationContent JSON built by pkg/plugin/comm (the single source shared
// with binary plugins). Called from RegisterStdlib alongside registerFmt/registerEmit.
func registerComm(ls *lua.LState, holoTable *lua.LTable) {
	mod := ls.NewTable()
	ls.SetField(mod, "pose", ls.NewFunction(func(l *lua.LState) int {
		a := comm.Author{ID: l.CheckString(1), Name: l.CheckString(2)}
		l.Push(lua.LString(comm.Pose(a, l.CheckString(3), l.CheckString(4))))
		return 1
	}))
	ls.SetField(mod, "say", ls.NewFunction(func(l *lua.LState) int {
		l.Push(lua.LString(comm.Say(comm.Author{ID: l.CheckString(1), Name: l.CheckString(2)}, l.CheckString(3))))
		return 1
	}))
	ls.SetField(mod, "ooc", ls.NewFunction(func(l *lua.LState) int {
		l.Push(lua.LString(comm.OOC(comm.Author{ID: l.CheckString(1), Name: l.CheckString(2)}, l.CheckString(3))))
		return 1
	}))
	ls.SetField(mod, "emit", ls.NewFunction(func(l *lua.LState) int {
		l.Push(lua.LString(comm.Emit(l.CheckString(1))))
		return 1
	}))
	ls.SetField(holoTable, "comm", mod)
}
```

- [ ] **Step 4: Wire into `RegisterStdlib`** (`stdlib.go:23-28`), right after `registerEmit`:

```go
	// Register holo.emit namespace
	registerEmit(ls, holoTable)

	// Register holo.comm namespace (say/pose/ooc/emit content builders)
	registerComm(ls, holoTable)

	ls.SetGlobal("holo", holoTable)
```

- [ ] **Step 5: Run to verify pass.** Run: `task test -- ./internal/plugin/hostfunc/ -run TestHoloComm`. Expected: PASS.
- [ ] **Step 6: Commit.**

---

### Task 5: Go↔Lua builder parity (INV-COMM-2)

**Files:**

- Test: `internal/plugin/hostfunc/comm_parity_test.go`

- [ ] **Step 1: Write the test** — the Lua hostfunc and the Go builder must decode to an equal proto for the same inputs (`// Verifies: INV-COMM-2`):

```go
// Verifies: INV-COMM-2
func TestGoAndLuaCommBuildersAgree(t *testing.T) {
	cases := []struct{ id, name, invoked, raw string }{
		{"01H", "Alaric", ";", "waves"},
		{"01H", "Alaric", ":", "smiles"},
		{"01H", "Bob", "", "plain pose"},
	}
	ls := lua.NewState(); defer ls.Close()
	hostfunc.RegisterStdlib(ls) // production holo.comm
	for _, c := range cases {
		goJSON := comm.Pose(comm.Author{ID: c.id, Name: c.name}, c.invoked, c.raw)
		require.NoError(t, ls.DoString(`out = holo.comm.pose("`+c.id+`","`+c.name+`","`+c.invoked+`","`+c.raw+`")`))
		var g, l commv1.CommunicationContent
		require.NoError(t, protojson.Unmarshal([]byte(goJSON), &g))
		require.NoError(t, protojson.Unmarshal([]byte(ls.GetGlobal("out").String()), &l))
		require.True(t, proto.Equal(&g, &l), "case %+v: %v != %v", c, &g, &l)
	}
}
```

- [ ] **Step 2: Run.** Run: `task test -- ./internal/plugin/hostfunc/ -run TestGoAndLuaCommBuildersAgree`. Expected: PASS (both call the same Go builder, so parity is structural — the test pins it).
- [ ] **Step 3: Commit.**

---

### Task 6: Consumer backward-compat (translate.go + telnet gateway)

**Files:**

- Modify: `internal/web/translate.go:18-32` (genericPayload) + `:84-98` (extraction)
- Modify: `internal/telnet/gateway_handler.go` (the `case "communication":` payload mapping near `:1131`)
- Test: `internal/web/translate_test.go`

- [ ] **Step 1: Write the failing test** — a `CommunicationContent`-shaped payload renders the same actor/text/style as the legacy shape:

```go
func TestTranslateReadsCommunicationContentFieldNames(t *testing.T) {
	h := newTestHandler(t)
	ev := commFrame("core-communication:pose", "action",
		`{"actor_id":"01H","actor_display_name":"Alaric","text":"waves","no_space":true}`)
	got := h.translateEvent(ev)
	require.Equal(t, "Alaric", got.GetActor()) // from actor_display_name, not character_name
	require.Equal(t, "waves", got.GetText())
	require.Equal(t, true, got.GetMetadata().AsMap()["no_space"])
}

func TestTranslateStillReadsLegacyFieldNames(t *testing.T) {
	h := newTestHandler(t)
	ev := commFrame("core-communication:pose", "action",
		`{"character_name":"Bob","action":"nods","no_space":false}`)
	got := h.translateEvent(ev)
	require.Equal(t, "Bob", got.GetActor()) // legacy character_name still works
	require.Equal(t, "nods", got.GetText())
}
```

- [ ] **Step 2: Run to verify fail.** Run: `task test -- ./internal/web/ -run TestTranslate`. Expected: FAIL (`actor_display_name` unread).
- [ ] **Step 3: Implement.** Add fields to `genericPayload` and prefer new→old in extraction:

```go
// in genericPayload struct, add:
	ActorDisplayName string `json:"actor_display_name"`
	OocStyle         string `json:"ooc_style"`

// in translateEvent, replace the actor extraction (translate.go:84-88):
	actor := p.ActorDisplayName
	if actor == "" { actor = p.CharacterName }
	if actor == "" { actor = p.SenderName }

// and the style metadata (translate.go:115-117, the `if p.Style != ""` block):
	if s := p.OocStyle; s != "" {
		meta["style"] = s
	} else if p.Style != "" {
		meta["style"] = p.Style
	}
```

(`text` already extracts via `p.Text`; `no_space` via `p.NoSpace` — both survive unchanged.)

- [ ] **Step 4: Fix the telnet gateway** — `formatCommunication` (`internal/telnet/gateway_handler.go:1149-1176`) reads `actor` from `character_name`/`sender_name` and content from `message`/`action`/`notice` but **never** `text`, so migrated verbs would render blank on telnet (a gap `translate.go` does NOT have — it already falls back to `p.Text`). Add `actor_display_name` and `text` to every lookup:

```go
	actor := stringFromPayload(payload, "actor_display_name", "character_name", "sender_name")
	// case "speech":
	text := stringFromPayload(payload, "text", "message")
	// case "action":
	text := stringFromPayload(payload, "text", "action", "notice", "message")
	// default:
	text := stringFromPayload(payload, "text", "message", "action", "notice")
```

Add a telnet test asserting a `CommunicationContent`-shaped `pose` (`{"actor_id":"01H","actor_display_name":"Alaric","text":"waves","no_space":true}`) renders `"Alaricwaves"` (no-space) and a `say` renders `Alaric says, "hi"`.

- [ ] **Step 5: Run to verify pass.** Run: `task test -- ./internal/web/ ./internal/telnet/`. Expected: PASS.
- [ ] **Step 6: Commit.**

---

### Task 7: Migrate core-scenes `handleEmit` → builder (preserve `actor_id`)

**Files:**

- Modify: `plugins/core-scenes/commands.go:1305-1323`
- Test: `plugins/core-scenes/commands_test.go`

- [ ] **Step 1: Write the failing test** — a scene pose invoked with `;` now carries `no_space` while still carrying `actor_id`:

```go
func TestHandleEmitBuildsCommunicationContentWithNoSpace(t *testing.T) {
	// ... arrange scene focus + participant grant (existing handleEmit test scaffold) ...
	resp, err := p.handleEmit(ctx, reqWithInvokedAs(";"), "waves", "core-scenes:scene_pose", false)
	require.NoError(t, err)
	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(capturedIntent.Payload), &got)) // EmitIntent.Payload is string
	require.Equal(t, req.CharacterID, got.GetActorId()) // preserved for Speaker
	require.Equal(t, "waves", got.GetText())
	require.True(t, got.GetNoSpace())
}
```

- [ ] **Step 2: Run to verify fail.** Run: `task test -- ./plugins/core-scenes/ -run TestHandleEmit`. Expected: FAIL (payload still flat map).
- [ ] **Step 3: Implement.** Replace the `payloadMap` build (`commands.go:1310-1323`) with the builder. `handleEmit` receives `eventType` (`core-scenes:scene_pose`/`_say`/`_ooc`/`_emit`) — map it to the right builder call, threading `req.InvokedAs` for pose:

```go
author := comm.Author{ID: req.CharacterID, Name: req.CharacterName}
var payload string // comm builders return string; EmitIntent.Payload is string
switch eventType {
case "core-scenes:scene_pose":
	payload = comm.Pose(author, req.InvokedAs, text)
case "core-scenes:scene_say":
	payload = comm.Say(author, text)
case "core-scenes:scene_ooc":
	payload = comm.OOC(author, text)
case "core-scenes:scene_emit":
	payload = comm.Emit(text)
}
// (scene_id is dropped from the body — redundant with the subject; no consumer
// reads it from the payload. pose/say/ooc carry actor_id for the replay/snapshot
// Speaker; scene_emit is intentionally ACTORLESS — comm.Emit sets no actor_id —
// which is correct: publish_render renders EntryKindEmit as content only, with NO
// speaker (publish_render.go:32,61), so an empty Speaker on emit entries is
// expected and harmless. Task 8's characterization test uses a POSE, so it still
// asserts actor_id round-trips.)
```

Confirm `pluginsdk.CommandRequest` exposes `InvokedAs`; if not, thread it (it exists on `CommandExecution.InvokedAs` and is already plumbed to plugins for the `;`/`:` distinction — verify via `mcp__probe__extract_code CommandRequest`).

- [ ] **Step 4: Run to verify pass.** Run: `task test -- ./plugins/core-scenes/ -run TestHandleEmit`. Expected: PASS.
- [ ] **Step 5: Commit.**

---

### Task 8: Scene Speaker-preservation characterization test

**Files:**

- Test: `plugins/core-scenes/commands_test.go` (or `publish_snapshot_test.go`)

- [ ] **Step 1: Write the test** — the round-trip from the new payload through the decoders still yields the actor id as `Speaker`:

```go
func TestReplayDecodePreservesSpeakerFromCommunicationContent(t *testing.T) {
	payload := comm.Pose(comm.Author{ID: "01HSPEAKER", Name: "Alaric"}, ":", "waves")
	entries, err := decodeReplayEntries([]pluginsdk.Event{{Type: "core-scenes:scene_pose", Payload: payload}})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "01HSPEAKER", entries[0].Speaker) // actor_id round-trips
	require.Equal(t, "waves", entries[0].Content)
}
```

- [ ] **Step 2: Run.** Run: `task test -- ./plugins/core-scenes/ -run TestReplayDecodePreservesSpeaker`. Expected: PASS (`decodeReplayEntries` reads `actor_id`, which the builder emits). If it FAILS, Task 7 dropped `actor_id` — fix Task 7.
- [ ] **Step 3: Commit.**

---

### Task 9: Migrate core-communication `say`/`pose`/`ooc`/`emit` → `holo.comm.*`

**Files:**

- Modify: `plugins/core-communication/main.lua` (handle_say :77-89, handle_pose :95-134, handle_ooc :140-169, handle_emit :175-190)
- Create: `plugins/core-communication/main_lua_test.go` (no handler harness exists — the package has only `events_test.go`, which tests EventType constants)

> **No handler harness exists yet (plan-review Important fix).** `deliverCommand`/`ctxWith`
> do NOT exist anywhere in the repo. Build a minimal one mirroring
> `plugins/core-help/help_lua_test.go`'s `runHelp` (`help_lua_test.go:39-54`):
> `lua.NewState()` → `hostfunc.RegisterStdlib(ls)` (provides `holo.comm`) → stub the
> `holomush` global table (`log`, etc., as `runHelp` stubs its host tables) →
> `L.DoFile("main.lua")` → `L.CallByParam` on the `on_command` global entry
> (`main.lua:563`, the host's real dispatch entry — it routes `ctx.command` to the
> right `handle_*`), NOT the file-local `handle_pose` (a `local function`, unreachable).

- [ ] **Step 1: Build the harness + write the failing test** (`main_lua_test.go`):

```go
func runCommand(t *testing.T, ctx map[string]string) string {
	L := lua.NewState(); defer L.Close()
	hostfunc.RegisterStdlib(L)          // provides holo.comm
	stubHolomush(L)                     // stub `holomush` global (log etc.) — mirror runHelp's host stubs
	require.NoError(t, L.DoFile("main.lua"))
	ctxTbl := L.NewTable()
	for k, v := range ctx { L.SetField(ctxTbl, k, lua.LString(v)) }
	require.NoError(t, L.CallByParam(lua.P{Fn: L.GetGlobal("on_command"), NRet: 1, Protect: true}, ctxTbl))
	return firstEventPayload(t, L.Get(-1)) // dig events[1].payload out of the returned table
}

func TestPoseHandlerEmitsCommunicationContent(t *testing.T) {
	payload := runCommand(t, map[string]string{
		"command": "pose", "character_id": "01H", "character_name": "Alaric",
		"args": "waves", "invoked_as": ";", "location_id": "01LOC",
	})
	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(payload), &got))
	require.Equal(t, "waves", got.GetText())
	require.True(t, got.GetNoSpace())
}
```

- [ ] **Step 2: Run to verify fail.** Run: `task test -- ./plugins/core-communication/`. Expected: FAIL (payload still `{character_name,action}`).
- [ ] **Step 3: Implement.** Replace each handler's hand-built JSON with a `holo.comm.*` call, keeping the subject/type unchanged:

```lua
local function handle_pose(ctx)
    local payload = holo.comm.pose(ctx.character_id or "", ctx.character_name, ctx.invoked_as or "", ctx.args or "")
    if payload == "" then return error_response("What do you want to pose?") end
    return ok_events({{subject = "location." .. ctx.location_id, type = "core-communication:pose", payload = payload}})
end
```

Apply the analogous swap to `handle_say` (`holo.comm.say`), `handle_ooc` (`holo.comm.ooc`), `handle_emit` (`holo.comm.emit`, actorless). Preserve the existing empty-input guards.

- [ ] **Step 4: Run to verify pass.** Run: `task test -- ./plugins/core-communication/`. Expected: PASS.
- [ ] **Step 5: Commit.**

---

### Task 10: Invariant registry — COMM boundary + INV-COMM-1/2

**Files:**

- Modify: `docs/architecture/invariants.yaml`
- Generated: `docs/architecture/invariants.md` (via `go run ./cmd/inv-render`)

> **Two plan-review Critical corrections baked in below:** (a) the new scope's `name`
> carries the `INV-` prefix and invariants reference it as `scope: INV-COMM` (matching
> `invariants.yaml:12` `name: INV-CRYPTO` / `scope: INV-CRYPTO`); (b) `boundary` is a
> field on the **scope**, NOT on the invariant entry (`internal/invregistry/registry.go`
> `Entry` has no `Boundary` field — it lives only on `Scope`). Getting either wrong makes
> `go run ./cmd/inv-render` **error** (`render.go` fails loud on an undeclared scope).

- [ ] **Step 1a: Add the `INV-COMM` scope** to the top-level `scopes:` list (mirror the shape at `invariants.yaml:12-25`; copy an existing entry's exact field set to avoid missing a required key):

```yaml
  - name: INV-COMM
    description: "Canonical communication-content payload contract (CommunicationContent), its emit-time validation, and dual-runtime builders."
    boundary: "The conversational-content payload body and its enforcement. Does NOT include: rendering metadata (→ INV-EVENTBUS), event payload encryption (→ INV-CRYPTO), focus-routing (holomush-g1qcw)."
    status: migrated
    origin_specs:
      - "docs/superpowers/specs/2026-07-03-communication-content-contract-design.md"
    owned_paths:
      - "api/proto/holomush/comm/**"
      - "pkg/plugin/comm/**"
      - "internal/eventbus/content_publisher.go"
      - "internal/plugin/hostfunc/stdlib_comm.go"
```

- [ ] **Step 1b: Add the two invariant entries** (`scope: INV-COMM` with the prefix; NO `boundary` field on the entry):

```yaml
  - id: INV-COMM-1
    scope: INV-COMM
    origin_spec: docs/superpowers/specs/2026-07-03-communication-content-contract-design.md
    summary: >-
      Every category:communication event carries a wire payload that validates as
      holomush.comm.v1.CommunicationContent.
    binding: pending
  - id: INV-COMM-2
    scope: INV-COMM
    origin_spec: docs/superpowers/specs/2026-07-03-communication-content-contract-design.md
    summary: >-
      The Go and Lua communication-content builders produce payloads that decode to
      an equal CommunicationContent proto for the same inputs.
    binding: bound
    asserted_by:
      - internal/plugin/hostfunc/comm_parity_test.go
```

- [ ] **Step 2: Regenerate + verify.** Run: `go run ./cmd/inv-render` then `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`. Expected: PASS (scope declared so `inv-render` does not error; INV-COMM-2's `// Verifies:` annotation from Task 5 is found; INV-COMM-1 tolerated as pending).
- [ ] **Step 3: Commit** `invariants.yaml` + regenerated `invariants.md` together.

---

### Task 11: Integration verification

**Files:**

- Test: `test/integration/comm/content_contract_test.go` (create; `//go:build integration`)

- [ ] **Step 1: Write a Ginkgo spec** driving a real broadcast through the stack: a `pose` with `;` from core-communication, and a scene `pose` via core-scenes, both land as `CommunicationContent` on the wire and translate to a `GameEvent` with `no_space` in metadata and the actor name populated.
- [ ] **Step 2: Run.** Run: `task test:int -- ./test/integration/comm/`. Expected: PASS.
- [ ] **Step 3: Run the full fast lane.** Run: `task pr-prep`. Expected: green (schema/license/lint/fmt/unit/build). Fix any `task fmt` mutations (SPDX headers on new `.go`, generated proto) and commit them.
- [ ] **Step 4: Commit.**

---

## Rule 7 grounding (verified while writing)

- `api/proto/holomush/comm/v1/` does not exist (Task 1 creates it); buf.validate syntax mirrors `api/proto/holomush/scene/v1/scene.proto:322` (`string.min_len`).
- Hostfunc registration pattern: `internal/plugin/hostfunc/stdlib.go:18-29` (`RegisterStdlib` → `registerFmt`/`registerEmit`/`registerComm`), invoked from `functions.go:304`. NOT `RegisterSessionFuncs` — that is the retired, test-only capability shim (`export_test.go:21-36`).
- Scene emit build site: `plugins/core-scenes/commands.go:1310-1323` (`payloadMap`).
- Decoders reading `actor_id`: `decodeReplayEntries` (commands.go), `decodeSnapshotEntry` (`publish_snapshot.go:368-375`), rendered `publish_render.go:28-31`.
- Consumer mapping: `internal/web/translate.go:74-160` (`genericPayload` + actor/text/style extraction); telnet `internal/telnet/gateway_handler.go:1131`.
- Gate machinery precedent: `internal/eventbus/rendering_publisher.go` (protovalidate at emit), encryption inlined `internal/eventbus/publisher.go:208-238`, wiring `cmd/holomush/sub_grpc.go:140-146`.
<!-- adr-capture: sha256=d43b22847068ba92; session=f01a4c84; ts=2026-07-04T01:09:14Z; adrs=holomush-2hhq2,holomush-byqph -->
