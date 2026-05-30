<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Colon-Style Subject Eradication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate every pub/sub stream name from legacy colon form (`location:<id>`, `character:<id>`) to JetStream-native dot form (`events.<gid>.location.<id>`) end to end, and delete `internal/eventbus/subjectxlate/`.

**Architecture:** Producers and clients emit a **domain-relative dot reference** (`location.<id>`, `character.<id>`, `global`); a single host-side qualifier (`eventbus.Qualify`) prepends `events.<gid>.` at exactly two boundaries (emit + read-entry), replacing the two `subjectxlate` call sites. Classifiers, the bus, and the durable audit see only fully-qualified dot subjects. The colon form survives **only** as an ABAC policy-DSL type-prefix (`character:`, `stream:`), never as a stream name.

**Tech Stack:** Go (`internal/`, `pkg/holo`, `cmd/holomush`), gRPC/protobuf (`api/proto`), SvelteKit/TypeScript (`web/src`), PostgreSQL (no migration — streams are not persisted as colon strings; audit stores the qualified subject).

**Spec:** [docs/superpowers/specs/2026-05-29-colon-style-subject-eradication-design.md](../specs/2026-05-29-colon-style-subject-eradication-design.md) — invariants INV-ROPS-1 … INV-ROPS-8.

**Bead:** `holomush-rops`. Subsumes `holomush-ec22.3`, `holomush-pkixe`, `holomush-ofpi`.

---

## Safety property that makes the ordering green

`subjectxlate.Legacy` only prepends `events.<gid>.` and replaces `:`→`.`. A
dot-relative reference has no colon, so `Legacy("location.<id>", gid)` and
`Legacy("location:<id>", gid)` produce the **identical** subject
`events.<gid>.location.<id>`. Therefore Tasks 1–3 (constructors, qualifier,
producers→dot, witness) can land while the shim is still present — the wire stays
correct. Tasks 4–6 flip the classifiers, read-entry, wire, and client; Task 7
deletes the shim. The full integration suite is green at the end of Task 7; unit
tests are green at every task.

---

## File structure

| File | Responsibility | Tasks |
| --- | --- | --- |
| `internal/eventbus/qualify.go` (new) | `Qualify(gid, relativeRef)` — the single gameID-injection helper | 1 |
| `internal/world/events.go` | relative stream builders (`:`→`.`); delete `StreamPrefix*` colon consts | 2 |
| `internal/core/event.go` | delete `StreamPrefixCharacter` colon const | 2 |
| `internal/core/engine.go` | producers emit dot-relative refs | 2 |
| `pkg/holo/emit.go` | Lua SDK emitter emits dot-relative refs | 3 |
| `internal/access/policy/attribute/stream.go` | dot-aware `location` parse + `has_location` witness | 4 |
| `internal/grpc/stream_access.go` | dot-only private/membership classifiers | 5 |
| `internal/grpc/scope_floor.go` | dot-only location/character scope-floor branches | 5 |
| `internal/grpc/query_stream_history.go` | read-entry qualifier; dot classifiers | 5 |
| `internal/grpc/server.go` | Subscribe read-entry qualifier; `dispatchDelivery` dot | 6 |
| `cmd/holomush/sub_grpc.go` | `busEventAppender`/`busHistoryReaderAdapter` use `Qualify` | 6 |
| `api/proto/holomush/core/v1/*.proto`, `web/src/lib/backfill/*` | wire + client carry dot refs | 6 |
| `internal/access/policy/seed.go` | location seeds use `has_location` witness | 5 |
| `internal/eventbus/subjectxlate/` | **deleted** | 7 |
| `internal/test/invariants/colon_eradication_test.go` (new) | INV-ROPS-3 repo-wide scan + bijection | 8 |
| `test/integration/streams/rops_test.go` (new) | INV-ROPS-4/7/8 round-trip, floor, seed auth | 8 |
| docs + ADR | iwzt, scenes-v2, event-conventions, architecture.md, s9nu amendment | 9 |

---

### Task 1: `eventbus.Qualify` — the single gameID-injection helper

**Files:**

- Create: `internal/eventbus/qualify.go`
- Test: `internal/eventbus/qualify_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/eventbus/qualify_test.go
package eventbus

import "testing"

func TestQualifyPrependsEventsAndGameID(t *testing.T) {
	got, err := Qualify("main", "location.01ABC")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "events.main.location.01ABC"; string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestQualifyRejectsEmptyGameID(t *testing.T) {
	if _, err := Qualify("", "location.01ABC"); err == nil {
		t.Fatal("expected error for empty game id")
	}
}

func TestQualifyPassesThroughAlreadyQualified(t *testing.T) {
	got, err := Qualify("main", "events.main.scene.01S.ic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "events.main.scene.01S.ic"; string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestQualify ./internal/eventbus/`
Expected: FAIL — `undefined: Qualify`

- [ ] **Step 3: Write minimal implementation**

```go
// internal/eventbus/qualify.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import "github.com/samber/oops"

// Qualify turns a domain-relative stream reference (e.g. "location.01ABC",
// "character.01XYZ", "global") into a fully-qualified JetStream subject by
// prepending "events.<gameID>.". References already starting with "events."
// are returned unchanged (idempotent). gameID MUST be non-empty.
//
// This is the single host-side gameID-injection point introduced by
// holomush-rops; it replaces subjectxlate.Legacy. Producers and clients, which
// lack the gameID, emit relative references; Qualify is applied at the emit and
// read-entry boundaries (spec §1 "where gameID enters").
func Qualify(gameID, relativeRef string) (Subject, error) {
	if relativeRef == "" {
		return "", oops.Errorf("empty stream reference")
	}
	if len(relativeRef) >= 7 && relativeRef[:7] == "events." {
		return NewSubject(relativeRef)
	}
	if gameID == "" {
		return "", oops.With("ref", relativeRef).Errorf("game id required to qualify stream reference")
	}
	return NewSubject("events." + gameID + "." + relativeRef)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestQualify ./internal/eventbus/`
Expected: PASS

- [ ] **Step 5: Commit**

Commit using VCS-appropriate commands per `references/vcs-preamble.md`. Message:
`feat(eventbus): add Qualify gameID-injection helper (holomush-rops)`

---

### Task 2: Host producers emit dot-relative references

**Files:**

- Modify: `internal/world/events.go:22-40` (builders + delete colon consts)
- Modify: `internal/core/event.go:14-17` (delete `StreamPrefixCharacter`)
- Modify: `internal/core/engine.go:73,96` (inline `"location:"` → `"location."`)
- Modify: `internal/core/engine_end_session.go:62` (inline `"character:"` → `"character."`)
- Modify: `internal/grpc/focus/subscription_router.go:37,47` (inline `"location:"` → `"location."`)
- Modify: `internal/grpc/focus/scenepolicy/policy.go:29-30` (`StreamsFor` colon scene filters → dot relative)
- Test: `internal/world/events_test.go`, `internal/grpc/focus/scenepolicy/policy_test.go`

> **Note:** `auth_handlers.go:876` builds `"character:"+id` as an ABAC *subject*
> (not a stream); it is handled in **Task 5 Step 7** by routing through
> `access.CharacterSubject(...)`, not here. `subscription_router.go` deliberately
> avoids importing `internal/grpc`/`world` to dodge an import cycle (see its `:14`
> comment), so flip it with an **inline** `"location." + characterLocationID.String()`,
> not `world.LocationStream`. Also update its stale `:27` doc-comment that names
> `notifications:<character_id>` as a stream — restate it in dot form
> (`events.<gid>.notification.<id>`).

- [ ] **Step 1: Write the failing test**

```go
// internal/world/events_test.go — add
func TestLocationStreamUsesDotRelativeForm(t *testing.T) {
	id := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if got := LocationStream(id); got != "location."+id.String() {
		t.Fatalf("got %q, want dot-relative location.<id>", got)
	}
}

func TestCharacterStreamUsesDotRelativeForm(t *testing.T) {
	id := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if got := CharacterStream(id); got != "character."+id.String() {
		t.Fatalf("got %q, want dot-relative character.<id>", got)
	}
}

func TestBroadcastLocationStreamUsesDotWildcard(t *testing.T) {
	if got := BroadcastLocationStream(); got != "location.*" {
		t.Fatalf("got %q, want location.*", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run 'TestLocationStreamUsesDot|TestCharacterStreamUsesDot|TestBroadcastLocationStreamUsesDot' ./internal/world/`
Expected: FAIL — still returns `location:<id>`

- [ ] **Step 3: Flip the builders and delete the colon constants**

In `internal/world/events.go`, replace lines 22-40:

```go
// Stream domain tokens for domain-relative stream references. The host
// qualifier (eventbus.Qualify) prepends "events.<gameID>." to produce the
// fully-qualified subject; these builders return the relative reference only.
const (
	streamDomainLocation  = "location."
	streamDomainCharacter = "character."
)

// LocationStream returns the domain-relative stream reference for a location.
func LocationStream(id ulid.ULID) string {
	return streamDomainLocation + id.String()
}

// CharacterStream returns the domain-relative stream reference for a character.
func CharacterStream(id ulid.ULID) string {
	return streamDomainCharacter + id.String()
}

// BroadcastLocationStream returns the relative reference matching all locations.
func BroadcastLocationStream() string {
	return streamDomainLocation + "*"
}
```

In `internal/core/event.go`, delete lines 14-17 (the `StreamPrefixCharacter`
const and its doc comment).

In `internal/core/engine.go`, change the two inline literals at lines 73 and 96:

```go
		Stream:    "location." + char.LocationID.String(),
```

- [ ] **Step 4: Flip the inline colon producers + fix compile breaks**

First flip the inline literals the compiler will NOT catch (they use no deleted
constant):

- `internal/grpc/focus/subscription_router.go:37,47`:

```go
	return []string{"location." + characterLocationID.String()}
```

- `internal/core/engine_end_session.go:62` — the `NewEvent` stream argument:

```go
	event := NewEvent(
		"character."+char.ID.String(),
```

- `internal/grpc/focus/scenepolicy/policy.go:29-30` — `StreamsFor` returns the
  IC/OOC **subscription filters**. `s9nu` migrated the scene emit path and
  classifiers to dot but missed this filter producer (latent `ofpi`-class bug).
  Flip to the dot-relative form the host qualifier expects:

```go
func (p *ScenePolicy) StreamsFor(target session.FocusKey) []string {
	id := target.TargetID.String()
	return []string{
		"scene." + id + ".ic",
		"scene." + id + ".ooc",
	}
}
```

  Update `policy_test.go` expectations to the dot form.

Then `task build`. Expected: compile errors at every `core.StreamPrefixCharacter`
/ `world.StreamPrefix*` consumer (e.g. `internal/grpc/server.go:1246,1294`). For
each, replace the prefix-constant reference with the dot domain token. At
`server.go:1246`: `strings.HasPrefix(legacyStream, "character.")`; at `:1294`:
`strings.HasPrefix(ctrl.stream, "location.")`. Re-run `task build` until green.

> Why both: the constant deletions are surfaced by the compiler, but
> `subscription_router.go`'s inline `"location:"+id` is not — only INV-ROPS-3
> (Task 8) would flag it. Flip it here so the eradication scan is green when it
> lands.

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- ./internal/world/ ./internal/core/`
Expected: PASS. (Wire stays correct: `subjectxlate.Legacy("location.<id>")` ==
`Legacy("location:<id>")` — the separator-agnostic property above.)

- [ ] **Step 6: Commit**

`refactor(world,core): emit dot-relative stream references (holomush-rops)`

---

### Task 3: Lua emit layer emits dot-relative references

Covers both Lua emit paths: the `pkg/holo` SDK constants AND the in-tree
`core-communication` plugin, which builds subjects **inline in Lua** (it does not
use the SDK constants). Both reach `event_emitter.go:207`, which qualifies with
the gameID — so both must emit the relative dot form, not colon.

**Files:**

- Modify: `pkg/holo/emit.go:18-22` (constants), `:54-88` (methods unchanged in shape)
- Modify: `plugins/core-communication/main.lua:76,121,156,178,276,397,398,443` (inline `subject =` colon → dot)
- Test: `pkg/holo/emit_test.go`, plus the `core-communication` integration/behavior coverage (say/pose/ooc/page/whisper/pemit/emit)

- [ ] **Step 1: Update the failing test**

In `pkg/holo/emit_test.go`, change the hardcoded colon expectations to dot. Find
each `"location:01ABC"` / `"character:01CHAR..."` expectation and rewrite the
stream assertion, e.g.:

```go
	// was: want stream "location:" + locationID
	if ev.Stream != "location."+locationID {
		t.Fatalf("got %q, want dot-relative location.<id>", ev.Stream)
	}
```

(Repeat for the `Character`, `Global`, and `*Sensitive` cases — `global` is
unchanged, it is already a bare token.)

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./pkg/holo/`
Expected: FAIL — emitter still produces `location:<id>`

- [ ] **Step 3: Flip the SDK constants**

In `pkg/holo/emit.go`, replace lines 18-22:

```go
// Domain-relative stream tokens. The host qualifier (eventbus.Qualify) prepends
// "events.<gameID>." — the SDK lacks the gameID, so it emits the relative ref.
const (
	streamPrefixCharacter = "character."
	streamPrefixLocation  = "location."
	streamPrefixGlobal    = "global"
)
```

(The `Location`/`Character`/`Global` method bodies are unchanged — they already
concatenate the prefix; only the prefix value changes. Update their doc-comments
that say `"location:<id>"` to `"location.<id>"`.)

- [ ] **Step 4: Flip the inline Lua subjects in `core-communication/main.lua`**

The plugin returns events with inline-built `subject =` strings. Flip the colon
to dot at all 8 sites (the `type = "core-communication:say"` colons are
event-type names — `plugin:verb` form — and stay):

```lua
        {subject = "location." .. ctx.location_id, type = "core-communication:say", payload = payload}
        {subject = "location." .. ctx.location_id, type = "core-communication:pose", payload = payload}
        {subject = "location." .. ctx.location_id, type = "core-communication:ooc", payload = payload}
        {subject = "location." .. loc, type = "emit", payload = payload}
        {subject = "character." .. target_session.character_id, type = "core-communication:page", payload = payload}
        {subject = "location." .. loc, type = "core-communication:whisper_notice", payload = notice_payload}
        {subject = "character." .. target.character_id, type = "core-communication:whisper", payload = whisper_payload}
        {subject = "character." .. target_session.character_id, type = "core-communication:pemit", payload = payload}
```

Why it matters: these bypass `pkg/holo` and reach `event_emitter.go:207`. After
Task 7 replaces `subjectxlate.Legacy` with `eventbus.Qualify`, a colon subject
(`"location:01ABC"`) qualifies to `"events.main.location:01ABC"`, which fails
`NewSubject` token validation (the colon is not in `[A-Za-z0-9_-]`) — breaking
every say/pose/ooc/page/whisper/pemit/emit command. The dot form qualifies cleanly.

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- ./pkg/holo/` and the `core-communication` plugin tests
(`task test -- ./plugins/core-communication/...` and the comms integration suite).
Expected: PASS.

- [ ] **Step 6: Commit**

`refactor(holo,core-communication): Lua emit layer emits dot-relative stream references (holomush-rops)`

---

### Task 4: `StreamProvider` — dot-aware location parse + `has_location` witness

**Files:**

- Modify: `internal/access/policy/attribute/stream.go:34-62`
- Test: `internal/access/policy/attribute/stream_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/access/policy/attribute/stream_test.go — add
func TestStreamProviderExtractsLocationFromQualifiedDotSubject(t *testing.T) {
	p := NewStreamProvider()
	attrs, err := p.ResolveResource(context.Background(), "stream:events.main.location.01LOC")
	require.NoError(t, err)
	require.Equal(t, "01LOC", attrs["location"])
	require.Equal(t, true, attrs["has_location"])
}

func TestStreamProviderOmitsLocationForNonLocationStream(t *testing.T) {
	p := NewStreamProvider()
	attrs, err := p.ResolveResource(context.Background(), "stream:events.main.character.01CHR")
	require.NoError(t, err)
	_, present := attrs["location"]
	require.False(t, present, "location key MUST be absent (not empty-sentinel) for non-location streams")
	require.Equal(t, false, attrs["has_location"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestStreamProvider ./internal/access/policy/attribute/`
Expected: FAIL — current parse keys on `"location:"` prefix; dot name yields no `location`, and `has_location` is undefined.

- [ ] **Step 3: Make the parse dot-aware and emit the witness**

In `internal/access/policy/attribute/stream.go`, replace `ResolveResource`
(lines 34-51) and add `has_location` to `Schema()`:

```go
// ResolveResource resolves stream attributes for a resource. The resource ID is
// "stream:<name>" where <name> is a fully-qualified dot subject
// (e.g. "events.<gid>.location.<ULID>"). The location attribute is emitted ONLY
// for location subjects; the has_location witness is always present (true/false)
// per .claude/rules/abac-providers.md (omit value, never sentinel).
func (p *StreamProvider) ResolveResource(_ context.Context, resourceID string) (map[string]any, error) {
	id, ok := parseEntityID(resourceID, "stream")
	if !ok {
		return nil, nil
	}

	attrs := map[string]any{
		"type": "stream",
		"name": id,
	}

	// Location subjects are "events.<gid>.location.<ULID>": parts[2]=="location".
	parts := strings.Split(id, ".")
	if len(parts) == 4 && parts[0] == "events" && parts[2] == "location" && parts[3] != "" {
		attrs["location"] = parts[3]
		attrs["has_location"] = true
	} else {
		attrs["has_location"] = false
		// location key INTENTIONALLY ABSENT (ADR holomush-9gtl fail-safe).
	}

	return attrs, nil
}
```

Add to the `Schema()` `Attributes` map: `"has_location": types.AttrTypeBool,`.

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestStreamProvider ./internal/access/policy/attribute/`
Expected: PASS

- [ ] **Step 5: Commit**

`feat(access): StreamProvider dot-aware location parse + has_location witness (holomush-rops)`

---

### Task 5: Dot-only classifiers, read-entry qualifier, and location seeds

This is the privacy-critical core. All four `internal/grpc/` classifiers flip to
dot-only, `QueryStreamHistory` qualifies `req.Stream` at entry, the location
seeds switch to the `has_location` witness, and the straggler host ABAC-subject
sites route through the existing `internal/access` builders (so no inline colon
literal survives in host code — the INV-ROPS-3 boundary).

**Files:**

- Modify: `internal/grpc/stream_access.go:70-115`
- Modify: `internal/grpc/scope_floor.go:33-81,93`
- Modify: `internal/grpc/query_stream_history.go:45-110,170-250` (incl. `:220` ABAC subject)
- Modify: `internal/grpc/auth_handlers.go:876`, `internal/plugin/pluginauthz/evaluate.go:65`, `internal/eventbus/authguard/guard.go:130`, `internal/command/handlers/resetpassword.go:107`, `internal/store/role_resolver.go:30` (route ABAC subjects through `access.*` builders)
- Modify: `internal/access/policy/seed.go:59-70`
- Test: `internal/grpc/stream_access_test.go`, `internal/grpc/scope_floor_test.go`

- [ ] **Step 1: Write the failing classifier non-collision test (INV-ROPS-5)**

```go
// internal/grpc/stream_access_test.go — add
func TestStreamClassifiersNonCollision(t *testing.T) {
	cases := []struct {
		name              string
		stream            string
		wantPrivate       bool
		wantScene         bool
		wantLocation      bool
	}{
		{"qualified location is public-not-scene", "events.main.location.01LOC", false, false, true},
		{"qualified character is private-not-scene", "events.main.character.01CHR", true, false, false},
		{"qualified scene ic is private-and-scene", "events.main.scene.01SCN.ic", true, true, false},
		{"colon character is rejected (no longer private)", "character:01CHR", false, false, false},
		{"colon location is rejected (not location)", "location:01LOC", false, false, false},
		{"garbage is none", "nonsense", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantPrivate, isPrivateStream(tc.stream))
			require.Equal(t, tc.wantScene, isSceneStream(tc.stream))
			require.Equal(t, tc.wantLocation, isLocationStream(tc.stream))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestStreamClassifiersNonCollision ./internal/grpc/`
Expected: FAIL — colon character still classified private; qualified character not yet recognized.

- [ ] **Step 3: Flip the character classifiers to dot-only**

In `internal/grpc/stream_access.go`, add a dot character helper and update the
two character branches (replace the `HasPrefix("character:")` /
`TrimPrefix("character:")` logic at lines 70-115):

```go
// isCharacterStream reports whether a stream is a qualified personal character
// subject: events.<gameID>.character.<ULID> (exactly 4 segments).
func isCharacterStream(stream string) bool {
	parts := strings.Split(stream, ".")
	return len(parts) == 4 && parts[0] == "events" && parts[1] != "" &&
		parts[2] == "character" && parts[3] != ""
}

func extractCharacterID(stream string) (string, bool) {
	parts := strings.Split(stream, ".")
	if len(parts) == 4 && parts[0] == "events" && parts[1] != "" &&
		parts[2] == "character" && parts[3] != "" {
		return parts[3], true
	}
	return "", false
}

func isPrivateStream(stream string) bool {
	return isCharacterStream(stream) || isSceneStream(stream)
}
```

In `sessionHasMembership` (lines 85-115), replace the `character:` branch:

```go
	if charID, ok := extractCharacterID(stream); ok {
		if info.CharacterID == (ulid.ULID{}) {
			return false
		}
		return info.CharacterID.String() == charID
	}
```

- [ ] **Step 4: Flip the location/character scope-floor branches**

In `internal/grpc/scope_floor.go`, replace `isLocationStream` (lines 69-81) and
the character branch of `streamScopeFloor` (line 49):

```go
// isLocationStream reports whether a stream is a qualified location subject:
// events.<gameID>.location.<ULID> (exactly 4 segments).
func isLocationStream(stream string) bool {
	parts := strings.Split(stream, ".")
	return len(parts) == 4 && parts[0] == "events" && parts[1] != "" &&
		parts[2] == "location" && parts[3] != ""
}

func extractLocationID(stream string) string {
	parts := strings.Split(stream, ".")
	if len(parts) == 4 {
		return parts[3]
	}
	return ""
}
```

In `streamScopeFloor`, change `case strings.HasPrefix(stream, "character:"):`
to `case isCharacterStream(stream):`.

- [ ] **Step 5: Add the read-entry qualifier (INV-ROPS-2)**

In `internal/grpc/query_stream_history.go`, immediately after the `req.Stream ==
""` guard (line ~96) and after `info` is loaded, qualify the stream and reject
the unqualifiable, then run the existing classifier switch on the qualified value:

```go
	qualified, qErr := eventbus.Qualify(s.currentGameID(), req.Stream)
	if qErr != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid stream")
	}
	stream := string(qualified) // classify on the qualified subject below
```

Replace the `req.Stream` references in the auth switch (lines 173-231) with
`stream`. Delete the downstream `subjectxlate.Legacy(legacyStream, gameID)` at
`:425,494` — `stream` is already qualified; pass it directly to the bus fetch.

- [ ] **Step 6: Switch the location seeds to the has_location witness (INV-ROPS-8 fix)**

In `internal/access/policy/seed.go`, edit the two seeds at lines 60 and 66.
Replace `resource.stream.name like "location:*"` with the witness check; leave
the co-location clause untouched and bump `SeedVersion`:

```go
		{
			Name:        "seed:player-stream-emit",
			Description: "Characters can emit to co-located location streams",
			DSLText:     `permit(principal is character, action in ["emit"], resource is stream) when { resource.stream.has_location == true && resource.stream.location == principal.character.location };`,
			SeedVersion: 3,
		},
		{
			Name:        "seed:player-location-stream-read",
			Description: "Characters can read history of their current location stream",
			DSLText:     `permit(principal is character, action in ["read"], resource is stream) when { resource.stream.has_location == true && resource.stream.location == principal.character.location };`,
			SeedVersion: 2,
		},
```

- [ ] **Step 7: Route straggler host ABAC subjects through the `access.*` builders**

The colon ABAC-subject form is built by the **existing** `internal/access`
builders (`access.CharacterSubject`, `access.PluginSubject`, …), already the
convention across `world/`, `command/`, `hostfunc/`, `grpc/`. A few host sites
still inline the literal; route them through the builders so no inline colon
literal survives in host code (the INV-ROPS-3 boundary, and a cleanup
`prefix.go`'s doc-comment explicitly calls for). Each builder panics on empty
input, which is strictly safer than the bare concatenation:

- `internal/grpc/scope_floor.go:93` → `access.CharacterSubject(info.CharacterID.String())`
- `internal/grpc/query_stream_history.go:220` → `access.CharacterSubject(info.CharacterID.String())`
- `internal/grpc/auth_handlers.go:876` → `access.CharacterSubject(c.ID.String())`
- `internal/plugin/pluginauthz/evaluate.go:65` → `access.CharacterSubject(a.ID)`
- `internal/eventbus/authguard/guard.go:130` → `access.PluginSubject(req.Identity.PluginName)`
- `internal/command/handlers/resetpassword.go:107` → `access.CharacterSubject(<id>)` (replaces the `fmt.Sprintf("character:%s", …)`)
- `internal/store/role_resolver.go:30` → parse via `access.ParseEntityRef(subject)` (or `strings.TrimPrefix(subject, access.SubjectCharacter)`) instead of the bare `"character:"` literal

Add the `"github.com/holomush/holomush/internal/access"` import where missing.
After this, `task build` then `task test`.

> **Plugin residual (NOT host):** `plugins/core-scenes/commands.go` builds
> `"scene:"+id` as an ABAC resource and **cannot** import `internal/access`
> (plugin boundary). Those lines already call `evaluator.Evaluate(` or carry the
> `// ABAC resource ref` comment, which the Task 8 INV-ROPS-3 scan skips — they
> are left as-is. This is the only sanctioned inline colon residual.

- [ ] **Step 8: Run tests to verify they pass**

Run: `task test -- ./internal/grpc/ ./internal/access/... ./internal/plugin/...`
Expected: PASS (unit). Integration round-trip is verified in Task 8.

- [ ] **Step 9: Commit**

`refactor(grpc,access): dot-only stream classifiers + read-entry qualifier + witness seeds (holomush-rops)`

---

### Task 6: Wire contract, Subscribe path, dispatch, and SvelteKit client

**Files:**

- Modify: `internal/grpc/server.go` (Subscribe read-entry qualify; `dispatchDelivery:1177,1246,1252,1294`)
- Modify: `cmd/holomush/sub_grpc.go` (`busEventAppender.Append`, `busHistoryReaderAdapter.ReplayTail`)
- Modify: `api/proto/holomush/core/v1/*.proto` doc-comments for `stream`/`streams` fields (note dot form; no field-shape change)
- Modify: `web/src/lib/backfill/*` (construct/consume dot refs)
- Test: existing `internal/grpc` + `web` unit tests

- [ ] **Step 1: Qualify Subscribe filters at entry**

In `internal/grpc/server.go` `Subscribe`, replace the `computeInitialFilters` →
`toSubject` → `subjectxlate.Legacy` chain so each requested filter is qualified
via `eventbus.Qualify(s.currentGameID(), filter)`. Filters that fail to qualify
return `codes.InvalidArgument`.

- [ ] **Step 2: Make `dispatchDelivery` operate on the qualified subject**

In `internal/grpc/server.go` `dispatchDelivery` (~1168-1252): delete the
`legacyStream := subjectxlate.ToLegacy(...)` at `:1177`. Feed the already-dot
`event.Subject` directly to `streamScopeFloor` (`:1228`), to the character check
at `:1246` (now `isCharacterStream(string(event.Subject))`), and set the
delivered frame's stream field to the qualified subject (the client now expects
dot — Step 5).

- [ ] **Step 3: Update `sub_grpc.go` appender/replay**

In `cmd/holomush/sub_grpc.go`: `busEventAppender.Append` receives a dot-relative
`core.Event.Stream` from `engine.go` (Task 2); qualify it with
`eventbus.Qualify(gameID, ev.Stream)` instead of `subjectxlate.Legacy`. In
`busHistoryReaderAdapter.ReplayTail`, qualify the requested stream the same way
and stop calling `subjectxlate.ToLegacy` on results — return the qualified
subject as the frame stream.

- [ ] **Step 4: Update proto doc-comments**

In `api/proto/holomush/core/v1` the `string stream` (QueryStreamHistory),
`repeated string streams` (Subscribe filters), and delivered-frame `stream`
fields are unchanged in shape; update their leading doc-comments to state the
value is a domain-relative dot reference (`location.<id>`) that the server
qualifies. Run `task lint:proto`. Expected: PASS (COMMENTS gate satisfied).

- [ ] **Step 5: Flip the SvelteKit client to dot refs**

In `web/src/lib/backfill/*` change the stream references the client sends and
matches from colon to dot-relative (`location.<id>`, `character.<id>`). Update
the corresponding `*.test.ts` fixtures.

Run: `cd web && bun run test` (or the repo's web test task)
Expected: PASS

- [ ] **Step 6: Commit**

`refactor(grpc,web): dot stream refs on the wire + Subscribe/dispatch qualify (holomush-rops)`

---

### Task 7: Delete `subjectxlate` and its harness callers

**Files:**

- Delete: `internal/eventbus/subjectxlate/subjectxlate.go`, `subjectxlate_test.go`
- Modify: `internal/grpc/server.go:663`, `cmd/holomush/sub_grpc.go`, `internal/admin/readstream/`, `internal/testsupport/integrationtest/session.go:585`, `harness.go:1155,1197`, `crypto.go:160` (stale comment), `internal/testsupport/integrationtest/real_abac_test.go:30` and `harness_smoke_test.go:135` (both build `"location:"+id` for `EmitDirectEvent` — flip to `world.LocationStream`; break at `pr-prep:full` post-migration otherwise — `_test.go` so INV-ROPS-3 won't catch them)

- [ ] **Step 1: Replace the remaining callers**

Replace each remaining `subjectxlate.Legacy(x, gid)` with
`eventbus.Qualify(gid, x)` and each `subjectxlate.ToLegacy(sub, gid)` with a
direct use of the already-dot subject (drop the call). The test harness
`session.go:585` and `harness.go:1155,1197` use `Qualify`; update the
`crypto.go:160` doc-comment to drop the `subjectxlate` reference.

- [ ] **Step 2: Delete the package**

```bash
rm internal/eventbus/subjectxlate/subjectxlate.go internal/eventbus/subjectxlate/subjectxlate_test.go
```

- [ ] **Step 3: Verify the build and full unit suite**

Run: `task build && task test`
Expected: PASS — no remaining importers of `subjectxlate`.

- [ ] **Step 4: Commit**

`refactor(eventbus): delete subjectxlate shim — dot subjects are canonical (holomush-rops)`

---

### Task 8: Invariant tests — INV-ROPS-3 (eradication), INV-ROPS-4/6/7/8

**Files:**

- Create: `internal/test/invariants/colon_eradication_test.go` (INV-ROPS-3 + bijection)
- Delete: `internal/test/invariants/scene_subjects_test.go` (INV-P4-1, subsumed); remove its entry from `inv_p4_coverage_meta_test.go`
- Create: `test/integration/streams/rops_test.go` (INV-ROPS-4/7/8; `//go:build integration`)
- Test: the above

- [ ] **Step 1: Write the eradication meta-test (INV-ROPS-3)**

Model on the deleted `scene_subjects_test.go`, but **walk the tree** (do not use a
fixed file list) and **fail on a missing root** (no `t.Skipf`):

```go
// internal/test/invariants/colon_eradication_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invariants

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestINV_ROPS_3_NoColonStreamLiterals asserts no production Go source contains
// a colon-style entity-prefix literal as a pub/sub STREAM name. Supersedes
// INV-P4-1 with a repo-wide scan. Roots MUST exist (fail, not skip).
//
// The stream-vs-ABAC ambiguity is solved structurally: ABAC subjects/resources
// are built ONLY via internal/access builders (access.CharacterSubject,
// SceneResource, …), which live in the allowlisted internal/access package.
// Host code therefore has NO inline colon literal (Task 5 Step 7 routed the
// stragglers through the builders). The scan skips: (a) internal/access/;
// (b) comment lines; (c) lines that call an ABAC evaluation API or carry the
// "ABAC resource ref" marker — the only residual, in plugins/core-scenes/ which
// cannot import internal/access. Any other hit is unambiguously a stream-producer
// bug. (mirrors INV-P4-1's proven abacContextMarkers approach.)
func TestINV_ROPS_3_NoColonStreamLiterals(t *testing.T) {
	roots := []string{"../../../internal", "../../../pkg", "../../../plugins", "../../../cmd"}
	pattern := regexp.MustCompile(`"(location|character|notifications|scene|plugin):`)
	abacMarkers := []string{"Evaluate(", "CanPerformAction(", "NewAccessRequest(", ".Grant(", "ABAC resource ref"}
	for _, root := range roots {
		info, err := os.Stat(root)
		require.NoErrorf(t, err, "scan root missing: %s", root) // fail, never skip
		require.True(t, info.IsDir())
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if strings.Contains(path, "internal/access") {
					return filepath.SkipDir // sole sanctioned home of colon prefixes
				}
				return nil
			}
			// Scan Go AND Lua: core-communication/main.lua builds stream
			// subjects inline (bypasses pkg/holo), so .lua must be covered.
			isGo := strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
			isLua := strings.HasSuffix(path, ".lua")
			if !isGo && !isLua {
				return nil
			}
			data, rerr := os.ReadFile(path)
			require.NoError(t, rerr)
			for i, raw := range strings.Split(string(data), "\n") {
				if !pattern.MatchString(raw) {
					continue
				}
				line := strings.TrimSpace(raw)
				if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "*") {
					continue // doc/comment line, not a producer
				}
				skip := false
				for _, m := range abacMarkers {
					if strings.Contains(raw, m) {
						skip = true // ABAC subject/resource construction, not a stream
						break
					}
				}
				if skip {
					continue
				}
				t.Errorf("INV-ROPS-3: colon-style stream literal in %s:%d:\n  %s", path, i+1, line)
			}
			return nil
		})
		require.NoError(t, err)
	}
}
```

- [ ] **Step 2: Remove INV-P4-1 and its bijection entry**

```bash
rm internal/test/invariants/scene_subjects_test.go
```

In `internal/test/invariants/inv_p4_coverage_meta_test.go`, delete the
`TestINV_P4_1_NoColonStyleSceneSubjects` entry from the coverage map so
`TestINV_P4_Coverage_Meta` stays green.

- [ ] **Step 3: Write the integration invariants (INV-ROPS-4/7/8)**

Write these as **plain-Go `func Test…(t *testing.T)`** tests (build-tag
`//go:build integration`) using `integrationtest.Start(t)` + `testify` — the
repo convention for harness-based integration tests (no Ginkgo suite bootstrap;
unlike `test/integration/privacy/` which uses a Ginkgo suite, a new leaf dir does
not need one, and testify-in-integration is established convention).

The real harness API (verified in `internal/testsupport/integrationtest/`):
`Start(t, opts...) *Server`; `Server.NewLocation(ctx) ulid.ULID`,
`Server.GameID() string`, `Server.ConnectAuthed(ctx, charName) *Session`,
`WithRealABAC()` (loads the actual seeds — use this for INV-ROPS-8, NOT a
placeholder), `WithPolicyEngine(eng)`; `Session.EmitDirectEvent(ctx, stream,
evType, payload []byte) error`, `Session.QueryStreamHistory(ctx, stream)
([]*corev1.EventFrame, error)`, `Session.WaitForEvent(ctx, eventType)
*corev1.EventFrame`. There is no `Subscribe`/`Received`/`EmitLocationEvent`
shortcut — use emit-then-`WaitForEvent`/`QueryStreamHistory`. `EmitDirectEvent`
and `QueryStreamHistory` take the **relative** stream ref (`world.LocationStream(id)`);
the harness qualifies via the same `eventbus.Qualify` path the production server
uses (after Task 7 updates `session.go:585`).

```go
// test/integration/streams/rops_test.go
//go:build integration

package streams

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	"github.com/holomush/holomush/internal/world"
)

// INV-ROPS-4: an event emitted to a location's dot stream is delivered to a
// session present at that location — proving producer subject (world.LocationStream
// → Qualify) equals the subscriber filter the harness derives the same way.
func TestINV_ROPS_4_ProducerSubscriberSymmetry(t *testing.T) {
	ctx := context.Background()
	s := integrationtest.Start(t)
	locID := s.NewLocation(ctx)
	sess := s.ConnectAuthed(ctx, "alice")
	// Place alice at locID and focus the location stream (use the harness move/
	// focus helpers — confirm exact names in harness.go at implementation time).
	require.NoError(t, sess.EmitDirectEvent(ctx, world.LocationStream(locID), "say", []byte(`{"text":"hi"}`)))
	frame := sess.WaitForEvent(ctx, "say")
	require.NotNil(t, frame, "event emitted to dot location stream must be delivered")
}

// INV-ROPS-7: a late joiner cannot read pre-join history (scope floor applied),
// and the read is authorized by the I-17 membership gate, not ABAC fall-through.
func TestINV_ROPS_7_LateJoinerFloorAndMembershipGate(t *testing.T) {
	ctx := context.Background()
	s := integrationtest.Start(t)
	locID := s.NewLocation(ctx)
	emitter := s.ConnectAuthed(ctx, "early")
	require.NoError(t, emitter.EmitDirectEvent(ctx, world.LocationStream(locID), "say", []byte(`{"text":"pre"}`)))
	// late joiner connects AFTER the pre-join event; assert QueryStreamHistory
	// for the location stream omits the pre-join frame (LocationArrivedAt floor).
	late := s.ConnectAuthed(ctx, "late")
	frames, err := late.QueryStreamHistory(ctx, world.LocationStream(locID))
	require.NoError(t, err)
	// assert no frame predates late's arrival; and (per the pkixe tell) that the
	// authorization path was the I-17 membership gate, not ABAC default-deny —
	// assert via the harness's decision/log capture, not just the allow outcome.
	_ = frames
}

// INV-ROPS-8: with the real seeded engine, a co-located character can emit+read
// its dot location stream; a non-co-located character is denied.
func TestINV_ROPS_8_LocationSeedAuthorization(t *testing.T) {
	ctx := context.Background()
	s := integrationtest.Start(t, integrationtest.WithRealABAC())
	locID := s.NewLocation(ctx)
	resident := s.ConnectAuthed(ctx, "resident") // place at locID
	require.NoError(t, resident.EmitDirectEvent(ctx, world.LocationStream(locID), "say", []byte(`{"text":"hi"}`)))
	// outsider at a DIFFERENT location must be denied emit/read on locID's stream.
	otherLoc := s.NewLocation(ctx)
	outsider := s.ConnectAuthed(ctx, "outsider") // place at otherLoc
	err := outsider.EmitDirectEvent(ctx, world.LocationStream(locID), "say", []byte(`{"text":"no"}`))
	require.Error(t, err, "non-co-located emit must be denied by seed:player-stream-emit")
}
```

The `// place at …` / decision-capture bodies are wired against the harness's
character-placement and focus helpers at implementation time; the method calls
shown above are all real. The INV-ROPS-7 test MUST assert the read was authorized
**by the membership gate**, not ABAC fall-through (per the `pkixe` tell — the
empty `policy_id` + "by ABAC" log line).

- [ ] **Step 4: Write INV-ROPS-6 role-split unit test**

The role split is now a real package boundary: the **stream** builder
(`world.CharacterStream`, dot) vs the **ABAC subject** builder
(`access.CharacterSubject`, colon, in `internal/access/prefix.go`). Assert both
against the real builders:

```go
// internal/grpc/stream_access_test.go — add
func TestRoleSplitCharacterStreamDotABACSubjectColon(t *testing.T) {
	id := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	// Stream form: dot-relative (this migration).
	require.Equal(t, "character."+id.String(), world.CharacterStream(id))
	// ABAC subject form: colon — the existing access builder, unchanged.
	require.Equal(t, "character:"+id.String(), access.CharacterSubject(id.String()))
}
```

- [ ] **Step 5: Run the invariant tests**

Run: `task test -- ./internal/test/invariants/ ./internal/grpc/` then `task test:int -- ./test/integration/streams/`
Expected: PASS

- [ ] **Step 6: Commit**

`test(rops): INV-ROPS-3/4/6/7/8 + retire INV-P4-1 (holomush-rops)`

---

### Task 9: Docs, ADR amendment, stale comments

**Files:**

- Modify: `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` §3 (iwzt stream-type table → dot)
- Modify: `docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md` §3.1 (stream-naming table; notifications row)
- Modify: `.claude/rules/event-conventions.md` (dot-only subject naming)
- Modify: `site/src/content/docs/contributing/explanation/architecture.md` (stale pre-cutover model, `ec22.18`)
- Modify: `docs/adr/holomush-s9nu-scene-subject-atomic-migration.md` (amend Consequences: INV-P4-1 superseded by INV-ROPS-3)
- Modify: `internal/grpc/query_stream_history.go:576-578` (stale doc-comment)

- [ ] **Step 1: Update the spec/rules/docs tables**

Rewrite each stream-naming table cell from colon to dot form. In
`event-conventions.md`, state that domain-relative dot references are the only
form producers emit and the host qualifies with `events.<gid>.`.

- [ ] **Step 2: Amend ADR `holomush-s9nu`**

Add an addendum to the Consequences section: "INV-P4-1's 3-file scene scan is
superseded by INV-ROPS-3 (repo-wide colon-stream-literal gate, `holomush-rops`).
The dot-style scene migration this ADR recorded is now the universal stream
form." Update the bd decision record per the `dev-flow:evolve-adr` flow.

- [ ] **Step 3: Fix the stale Go doc-comments**

Rewrite the comments that describe colon-delimited streams / `subjectxlate`
translation (now gone) to the dot form:

- `internal/grpc/query_stream_history.go:576-578` ("colon-delimited ... the web
  client expects").
- `internal/grpc/server.go:211` and `:655` (e.g. `"character:01ABC"` translation
  examples — `:655` mentions "F5 migrates", which this work completes).
- `internal/plugin/pluginauthz/evaluate.go:247` (the `"character:01ABC" →
  "character"` parse example — clarify it is an ABAC subject, not a stream).

(These comment lines are skipped by INV-ROPS-3's comment-skip rule, so they are
hygiene, not a CI blocker — but leaving them stale misleads future readers.)

- [ ] **Step 4: Verify docs lint**

Run: `task lint:markdown` and `task lint:proto`
Expected: PASS

- [ ] **Step 5: Commit**

`docs(rops): dot-style stream docs + s9nu ADR amendment (holomush-rops)`

---

## Post-implementation checklist

- [ ] `task pr-prep` green (fast lane).
- [ ] `task pr-prep:full` green (this diff touches int + E2E surface — Subscribe/QueryStreamHistory + scene/privacy integration suites).
- [ ] `rg -n '"(location|character|notifications|scene|plugin):' internal pkg plugins cmd | rg -v 'internal/access|Evaluate\(|CanPerformAction\(|NewAccessRequest\(|\.Grant\(|ABAC resource ref|^\s*//'` returns nothing (INV-ROPS-3 holds manually — host code routes ABAC subjects through `access.*` builders; the only colon residual is plugin `Evaluate(` lines).
- [ ] `holomush-pkixe` verified fixed by this work (scene members read their own history via the I-17 gate); close it with evidence.
- [ ] `holomush-ofpi` verified fixed (Subscribe scope-floor sees qualified subjects); close it.
- [ ] `holomush-ec22.3` closed as superseded (subjectxlate deleted).
- [ ] `code-reviewer` + `abac-reviewer` run before push (touches `internal/access/` + I-17 gate).

<!-- adr-capture: sha256=56edd66c42d34847; session=brainstorm-rops; ts=2026-05-29T00:00:00Z; adrs=holomush-21ahd,holomush-wia9e -->
