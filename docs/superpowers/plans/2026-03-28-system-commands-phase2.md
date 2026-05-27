<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# System Commands Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement six new commands (ooc, pemit, home, teleport, examine, where) to make HoloMUSH a playable RP-focused MU\*.

**Architecture:** Each command is a Go handler in `internal/command/handlers/` following the existing `CommandHandler` pattern. Commands register via `RegisterAll()`, use ABAC for authorization, and emit events via `EventStore.Append()`. Two new event types (`ooc`, `pemit`) are added. The `WorldService` interface is extended with methods needed by teleport, examine, and property access (home property, examine properties). All property access flows through `WorldService` with ABAC checks — no direct repository access from handlers.

**Tech Stack:** Go, oops (structured errors), testify (assertions), mockery (mocks), slog (logging)

**Spec:** `docs/specs/2026-03-28-system-commands-phase2-design.md`

---

## Chunk 1: Infrastructure (Event Types, Interface Extensions, Seed Policies)

Before implementing any command handlers, the shared infrastructure needs to be in place:
new event types, WorldService interface extensions (including ABAC-gated property access),
ABAC seed policies for `character.teleport` and `comms.pemit`, and system aliases.

### Task 1: Add New Event Types

**Files:**

- Modify: `internal/core/event.go` (add EventTypeOOC, EventTypePemit constants)
- Modify: `internal/core/payloads.go` (add OOCPayload, PemitPayload structs)
- Test: `internal/core/event_test.go`

- [ ] **Step 1: Write the failing test**

Add test cases to the existing event type test that verify the new types:

```go
{"ooc event", EventTypeOOC, "ooc"},
{"pemit event", EventTypePemit, "pemit"},
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestEventType -v ./internal/core/...`
Expected: FAIL — `EventTypeOOC` and `EventTypePemit` undefined

- [ ] **Step 3: Add event type constants and payload structs**

In `internal/core/event.go`, add to the constants block:

```go
EventTypeOOC   EventType = "ooc"
EventTypePemit EventType = "pemit"
```

In `internal/core/payloads.go`, add:

```go
// OOCPayload carries an OOC communication event.
type OOCPayload struct {
    CharacterName string `json:"character_name"`
    Message       string `json:"message"`
    Style         string `json:"style"` // "say", "pose", "semipose"
}

// PemitPayload carries a private emit event.
type PemitPayload struct {
    SenderID   string `json:"sender_id"`
    SenderName string `json:"sender_name"`
    TargetID   string `json:"target_id"`
    Message    string `json:"message"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestEventType -v ./internal/core/...`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(core): add ooc and pemit event types
```

---

### Task 2: Extend WorldService Interface

The `teleport` command needs `FindLocationByName` and `examine` needs
`GetCharactersByLocation` and `ListPropertiesByParent`. These methods exist on
`world.Service` but are not exposed through the `command.WorldService` interface.

**Files:**

- Modify: `internal/command/types.go` (extend WorldService interface)

- [ ] **Step 1: Add methods to WorldService interface**

In `internal/command/types.go`, add to the `WorldService` interface:

```go
// FindLocationByName searches for a location by name after checking read authorization.
FindLocationByName(ctx context.Context, subjectID, name string) (*world.Location, error)

// GetCharactersByLocation returns characters at a location after checking authorization.
GetCharactersByLocation(ctx context.Context, subjectID string, locationID ulid.ULID, opts world.ListOptions) ([]*world.Character, error)

// GetObjectsByLocation returns objects at a location after checking authorization.
GetObjectsByLocation(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*world.Object, error)

// ListPropertiesByParent returns all properties for the given parent entity
// after checking read authorization on the parent. The ABAC seed policies for
// property visibility (public/private/restricted/system/admin) handle per-property
// filtering. This method returns ALL properties the caller is allowed to see.
ListPropertiesByParent(ctx context.Context, subjectID string, parentType string, parentID ulid.ULID) ([]*world.EntityProperty, error)
```

Note: `ListPropertiesByParent` and `GetObjectsByLocation` may not yet exist on
`world.Service` with ABAC — if they do not, the task implementer MUST add them to
`world.Service` following the same `checkAccess` pattern used by `GetLocation`.
The actions should be `"list_properties"` and `"list_objects"` on the parent resource.

- [ ] **Step 2: Verify compilation**

Run: `task build`
Expected: Compile error — `world.Service` may not implement new methods yet.
If so, add stub implementations following the existing `GetLocation`/`checkAccess` pattern.

- [ ] **Step 3: Fix compilation errors**

Add any missing methods to `world.Service` using the same ABAC `checkAccess` pattern:

```go
func (s *Service) ListPropertiesByParent(ctx context.Context, subjectID string, parentType string, parentID ulid.ULID) ([]*EntityProperty, error) {
    // Construct resource based on parentType
    var resource string
    switch parentType {
    case "location":
        resource = access.LocationResource(parentID.String())
    case "character":
        resource = access.CharacterResource(parentID.String())
    default:
        resource = access.ObjectResource(parentID.String())
    }
    if err := s.checkAccess(ctx, subjectID, "list_properties", resource, prefixProperty); err != nil {
        return nil, err
    }
    return s.propertyRepo.ListByParent(ctx, parentType, parentID)
}
```

- [ ] **Step 4: Run build to verify**

Run: `task build`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(command): extend WorldService interface for phase 2 commands
```

---

### Task 3: Add ABAC Seed Policies and Default Grants

The ABAC seed policies gate execute permission on command names. Scope enforcement
(home-only, self-only) is handled in the command handlers, not in policies, because
the ABAC engine doesn't have access to runtime command arguments like target location.

**Files:**

- Modify: `internal/access/policy/seed.go` (add teleport + pemit policies)
- Modify: character creation code (add `character.teleport` to default grants)
- Test: `internal/access/policy/seed_test.go`

- [ ] **Step 1: Write the failing test**

Add test cases that verify the new seed policies exist and have correct DSL:

```go
{"seed:player-teleport", "permit", "all players can execute home and teleport"},
{"seed:pemit-storyteller", "permit", "storyteller/admin can execute pemit"},
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestSeedPolicies -v ./internal/access/policy/...`
Expected: FAIL — new seeds not found

- [ ] **Step 3: Add seed policies**

In `internal/access/policy/seed.go`, add to the seeds slice:

```go
// Teleport: all players can execute home and teleport commands.
// Scope enforcement (home-only for default role, self-only for builder)
// is handled in the command handlers, not here.
{
    Name: "seed:player-teleport",
    DSL:  `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name in ["teleport", "home"] };`,
},

// Pemit: storyteller and admin roles only
{
    Name: "seed:pemit-storyteller",
    DSL:  `permit(principal is character, action in ["execute"], resource is command) when { principal.character.role in ["storyteller", "admin"] && resource.command.name == "pemit" };`,
},
```

- [ ] **Step 4: Add character.teleport to default capability grants**

Find where default capabilities are assigned during character creation (search
for `world.*`, `comms.*`, `player.*` grants). Add `character.teleport` to the
same grant set. This introduces the `character.*` namespace — only
`character.teleport` is defined in this phase.

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestSeedPolicies -v ./internal/access/policy/...`
Expected: PASS

- [ ] **Step 6: Commit**

```text
feat(access): add seed policies for teleport and pemit capabilities
```

---

### Task 4: Register System Aliases (tel, ex)

System aliases are stored in the `system_aliases` DB table and loaded at startup
via `aliasRepo.GetSystemAliases()`. Add the new aliases using a SQL migration.

**Files:**

- Create: `internal/store/migrations/NNNNNN_add_teleport_examine_aliases.up.sql`
- Create: `internal/store/migrations/NNNNNN_add_teleport_examine_aliases.down.sql`

- [ ] **Step 1: Find the existing alias seed migration**

Search `internal/store/migrations/` for the migration that seeds `"` → say,
`:` → pose, `;` → pose. Use the same table/column names for the new migration.

- [ ] **Step 2: Create the migration**

Up migration:

```sql
INSERT INTO system_aliases (alias, command, created_by)
VALUES ('tel', 'teleport', 'system'),
       ('ex', 'examine', 'system')
ON CONFLICT (alias) DO NOTHING;
```

Down migration:

```sql
DELETE FROM system_aliases WHERE alias IN ('tel', 'ex') AND created_by = 'system';
```

- [ ] **Step 3: Verify migration applies**

Run: `task dev` and verify `tel` and `ex` resolve correctly.
Alternatively, run the migration directly and check the database.

- [ ] **Step 4: Commit**

```text
feat(alias): add tel and ex system aliases via migration
```

---

## Chunk 2: Communication Commands (ooc, pemit)

### Task 5: Implement `ooc` Command Handler

**Files:**

- Create: `internal/command/handlers/ooc.go`
- Create: `internal/command/handlers/ooc_test.go`
- Modify: `internal/command/handlers/register.go` (register ooc)

- [ ] **Step 1: Write the failing test**

```go
func TestOOCHandler(t *testing.T) {
    tests := []struct {
        name          string
        args          string
        wantEventType core.EventType
        wantStyle     string
        wantMessage   string
    }{
        {"say style", "This is great", core.EventTypeOOC, "say", "This is great"},
        {"pose style", ":laughs", core.EventTypeOOC, "pose", "laughs"},
        {"semipose style", ";'s phone rings", core.EventTypeOOC, "semipose", "'s phone rings"},
        {"empty message", "", core.EventTypeOOC, "", ""},  // should error
    }
    // ... table-driven test using mock EventStore
}
```

Follow the pattern from `say_test.go` — create a `CommandExecution` with mock
services, call `OOCHandler`, verify the appended event has correct type, stream,
and payload fields.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestOOCHandler -v ./internal/command/handlers/...`
Expected: FAIL — `OOCHandler` undefined

- [ ] **Step 3: Implement OOCHandler**

Create `internal/command/handlers/ooc.go`:

```go
func OOCHandler(ctx context.Context, exec *command.CommandExecution) error {
    msg := strings.TrimSpace(exec.Args)
    if msg == "" {
        return command.ErrInvalidArgs("ooc", "ooc <message>")
    }

    var style, text string
    switch {
    case strings.HasPrefix(msg, ":"):
        style = "pose"
        text = msg[1:]
    case strings.HasPrefix(msg, ";"):
        style = "semipose"
        text = msg[1:]
    default:
        style = "say"
        text = msg
    }

    payload, err := json.Marshal(core.OOCPayload{
        CharacterName: exec.CharacterName(),
        Message:       text,
        Style:         style,
    })
    if err != nil {
        return oops.With("operation", "marshal_ooc_payload").Wrap(err)
    }

    event := core.Event{
        ID:        core.NewULID(),
        Stream:    world.LocationStream(exec.LocationID()),
        Type:      core.EventTypeOOC,
        Timestamp: time.Now(),
        Actor:     core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
        Payload:   payload,
    }

    if err := exec.Services().Events().Append(ctx, event); err != nil {
        return oops.With("operation", "append_ooc_event").Wrap(err)
    }

    return nil
}
```

- [ ] **Step 4: Register command in register.go**

```go
mustRegister(command.CommandEntryConfig{
    Name:    "ooc",
    Handler: OOCHandler,
    Help:    "Say or pose something out of character",
    Usage:   "ooc <message>",
    HelpText: "## OOC\n\nSpeak out of character...",
    Source:  "core",
})
```

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestOOCHandler -v ./internal/command/handlers/...`
Expected: PASS

- [ ] **Step 6: Commit**

```text
feat(command): add ooc command for OOC communication
```

---

### Task 6: Implement `pemit` Command Handler

**Files:**

- Create: `internal/command/handlers/pemit.go`
- Create: `internal/command/handlers/pemit_test.go`
- Modify: `internal/command/handlers/register.go` (register pemit)

- [ ] **Step 1: Write the failing test**

```go
func TestPemitHandler(t *testing.T) {
    tests := []struct {
        name        string
        args        string
        wantErr     bool
        wantErrCode string
        wantStream  string
        wantMessage string
    }{
        {"valid pemit", "Sean=You hear a whisper", false, "", "character:<targetID>", "You hear a whisper"},
        {"missing equals", "just a message", true, command.CodeInvalidArgs, "", ""},
        {"empty message", "Sean=", true, command.CodeInvalidArgs, "", ""},
        {"unknown character", "Nobody=hello", true, "", "", ""},
        {"character not online", "Gandalf=hello", true, "", "", ""}, // exists in world but no active session
    }
}
```

Follow `page_test.go` pattern — mock Session().FindByCharacterName() for name resolution.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestPemitHandler -v ./internal/command/handlers/...`
Expected: FAIL

- [ ] **Step 3: Implement PemitHandler**

Create `internal/command/handlers/pemit.go`. Pattern: parse `name=message`,
resolve character via `Session().FindByCharacterName()`, emit `EventTypePemit`
to `world.CharacterStream(targetCharID)`, confirm to sender.

Key differences from `page`:

- Capability check: `comms.pemit` required
- No last-paged tracking
- No pose/semipose parsing
- Target sees raw message (no "From afar" prefix)
- Cross-location allowed (no same-room restriction)

- [ ] **Step 4: Register command in register.go**

```go
mustRegister(command.CommandEntryConfig{
    Name:         "pemit",
    Handler:      PemitHandler,
    Capabilities: []string{"comms.pemit"},
    Help:         "Send private narration to a character",
    Usage:        "pemit <character>=<message>",
    HelpText:     "## Pemit\n\nSend private narration...",
    Source:       "core",
})
```

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestPemitHandler -v ./internal/command/handlers/...`
Expected: PASS

- [ ] **Step 6: Commit**

```text
feat(command): add pemit command for private narration
```

---

## Chunk 3: Spatial Commands (home, teleport)

### Task 7: Implement `home` Command Handler

**Files:**

- Create: `internal/command/handlers/home.go`
- Create: `internal/command/handlers/home_test.go`
- Modify: `internal/command/handlers/register.go` (register home)

- [ ] **Step 1: Write the failing test**

```go
func TestHomeHandler(t *testing.T) {
    tests := []struct {
        name            string
        homePropertyVal *string // nil = no home property set
        startLocationID ulid.ULID
        wantErr         bool
        wantErrCode     string
        wantOutput      string // verify output message
    }{
        {"move to home", strPtr("01HOME..."), someOtherLoc, false, "", ""},
        {"already at home", strPtr("01HOME..."), homeLocID, false, "", "You are already home."},
        {"no home set uses default", nil, someOtherLoc, false, "", ""},
        {"home location deleted", strPtr("01GONE..."), someOtherLoc, true, "LOCATION_NOT_FOUND", ""},
    }
}
```

Mock `World().ListPropertiesByParent()` to return (or not return) a home property.
Mock `World().MoveCharacter()` for the actual move.
Mock `World().GetLocation()` to return the destination for display.
Verify the output buffer for "already at home" case.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestHomeHandler -v ./internal/command/handlers/...`
Expected: FAIL

- [ ] **Step 3: Implement HomeHandler**

Create `internal/command/handlers/home.go`:

1. Query character's home property via `World().ListPropertiesByParent()`
   - Filter for property with name `"home"` — its value is the ULID string
   of the home location
2. If no home property, fall back to the default starting location
3. If already at home, output `You are already home.` and return
4. Call `World().MoveCharacter()` (reuses existing leave/arrive event emission)
5. Fetch and display new location via `World().GetLocation()`

**Default starting location:** The `guest_start_location` ULID is parsed in
`cmd/holomush/core.go`. The implementer MUST wire this value into `Services`
(add a `StartingLocationID` field + getter) so the handler can access it as
a fallback when no home property is set.

**Scope note:** The `home` command always targets the character's own home
location. Since the seed policy permits all characters to execute `home`,
and the target is always a fixed location (not arbitrary), no handler-level
scope enforcement is needed. The `home` command is inherently home-only.

- [ ] **Step 4: Register command in register.go**

```go
mustRegister(command.CommandEntryConfig{
    Name:         "home",
    Handler:      HomeHandler,
    Capabilities: []string{"character.teleport"},
    Help:         "Return to your home location",
    Usage:        "home",
    HelpText:     "## Home\n\nReturn to your home location...",
    Source:       "core",
})
```

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestHomeHandler -v ./internal/command/handlers/...`
Expected: PASS

- [ ] **Step 6: Commit**

```text
feat(command): add home command for returning to home location
```

---

### Task 8: Implement `teleport` Command Handler

**Files:**

- Create: `internal/command/handlers/teleport.go`
- Create: `internal/command/handlers/teleport_test.go`
- Modify: `internal/command/handlers/register.go` (register teleport)

- [ ] **Step 1: Write the failing tests**

This command has the most complex ABAC requirements. Tests MUST cover:

**Basic functionality:**

```go
{"teleport to location", "The Library", false, ""},
{"location not found", "Nowhere", true, "LOCATION_NOT_FOUND"},
{"already at location", "Current Room", false, ""}, // shows "already here"
{"teleport other (admin)", "Sean=The Library", false, ""},
{"teleport other output", "Sean=The Library", false, ""}, // verify target gets "You have been teleported to The Library by <admin>."
```

**Name disambiguation:**

```go
{"exact match wins over prefix", "The Library", false, ""}, // "The Library" exact matches over "The Library Annex"
{"ambiguous prefix match", "The", true, ""}, // "Multiple locations match" error with list
```

**ABAC scope tiers:**

```go
{"default role - home only", "The Library", true, "PERMISSION_DENIED"},
{"builder - any location self", "The Library", false, ""},
{"builder - cannot teleport others", "Sean=The Library", true, "PERMISSION_DENIED"},
{"admin - any location any target", "Sean=The Library", false, ""},
```

**Departure/destination ABAC permutations** (per spec). Test at each scope tier:

```go
// Builder scope:
{"builder: allowed departure + allowed destination", ...},
{"builder: allowed departure + denied destination", ...},
{"builder: denied departure + allowed destination", ...},
{"builder: denied departure + denied destination", ...},
// Admin scope (all should succeed):
{"admin: allowed departure + allowed destination", ...},
{"admin: allowed departure + denied destination", ...},
// ... etc
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestTeleportHandler -v ./internal/command/handlers/...`
Expected: FAIL

- [ ] **Step 3: Implement TeleportHandler**

Create `internal/command/handlers/teleport.go`:

1. Parse args: `<location>` or `<character>=<location>`
2. Resolve location by name via `World().FindLocationByName()`
   - If exact match found, use it
   - If not found, attempt prefix match (see disambiguation below)
3. If teleporting others, resolve target character via `Session().FindByCharacterName()`
4. Handler-level scope enforcement (ABAC engine only gates command name):
   - Read character's role. The existing ABAC patterns use
     `access.CharacterSubject(id)` and the engine evaluates role-based policies.
     To determine the role for scope enforcement, query the character's role
     property via `World().ListPropertiesByParent()` (look for a `role` property
     on the character), OR use the pattern from `boot.go` which checks role
     by attempting a capability check (e.g., `CheckCapability(ctx, engine,
     subject, "admin.teleport.others", "teleport")`) and treating denial as
     "not admin". The implementer should check how `boot.go` determines self
     vs other and follow the same pattern.
   - Default role: verify target is character's home (read home property)
   - Builder role: verify teleporting self only
   - Admin: unrestricted
5. Call `World().MoveCharacter()` for the actual move
6. If teleporting others, notify target: `You have been teleported to <location> by <admin>.`
7. Display new location to the actor

**Disambiguation:** If `FindLocationByName` returns `ErrNotFound`, attempt
prefix matching. The implementer MUST check whether `FindLocationByName`
already does prefix matching or whether a new `FindLocationsByPrefix` method
is needed on the repository and service layers. If prefix match returns
multiple results, format them as: `Multiple locations match "<name>": <list>`

- [ ] **Step 4: Register command in register.go**

```go
mustRegister(command.CommandEntryConfig{
    Name:         "teleport",
    Handler:      TeleportHandler,
    Capabilities: []string{"character.teleport"},
    Help:         "Teleport to a location",
    Usage:        "teleport <location>",
    HelpText:     "## Teleport\n\nMove instantly to a named location...",
    Source:       "core",
})
```

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestTeleportHandler -v ./internal/command/handlers/...`
Expected: PASS

- [ ] **Step 6: Commit**

```text
feat(command): add teleport command with ABAC-scoped access
```

---

## Chunk 4: Information Commands (examine, where)

### Task 9: Implement `examine` Command Handler

**Files:**

- Create: `internal/command/handlers/examine.go`
- Create: `internal/command/handlers/examine_test.go`
- Modify: `internal/command/handlers/register.go` (register examine)

- [ ] **Step 1: Write the failing tests**

```go
func TestExamineHandler(t *testing.T) {
    tests := []struct {
        name       string
        args       string
        role       string // "player", "builder", "admin"
        wantFields []string // fields that should appear in output
        wantHidden []string // fields that should NOT appear in output
    }{
        {"player sees name+desc", "", "player", []string{"Name:", "Description:"}, []string{"Owner:", "ULID:"}},
        {"builder sees owner+type", "", "builder", []string{"Owner:", "Type:"}, []string{"ULID:"}},
        {"admin sees everything", "", "admin", []string{"ULID:", "Owner:", "Type:"}, nil},
        {"examine character", "Gandalf", "player", []string{"Name:", "Description:"}, nil},
        {"target not found", "Nobody", "player", nil, nil}, // should error
        {"ambiguous match", "G", "player", nil, nil}, // should error with "Multiple matches"
    }
}
```

Test ABAC-filtered output at each tier by mocking the ABAC engine to return
different roles and verifying which fields appear in the output buffer.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestExamineHandler -v ./internal/command/handlers/...`
Expected: FAIL

- [ ] **Step 3: Implement ExamineHandler**

Create `internal/command/handlers/examine.go`:

1. Parse target: no args or `here` → current location; otherwise name match
2. Resolve target by name against location contents:
   - Characters: `World().GetCharactersByLocation()` then case-insensitive name match
   - Exits: `World().GetExitsByLocation()` then name match via `exit.MatchesName()`
   - Objects: `World().GetObjectsByLocation()` then case-insensitive name match
   - If multiple entities match the name, return `Multiple matches for "<name>": <list>`
3. Query properties via `World().ListPropertiesByParent()` (ABAC-gated)
4. Determine viewer's access tier by checking role/ownership via ABAC
5. Filter output based on tier:
   - Player: Name, Description, `public` visibility properties
   - Owner: Above + `private` properties, exit destinations
   - Builder: Above + Owner (PlayerID), Type, CreatedAt, `restricted` properties
   - Admin: Above + ULID, all properties including `system` and `admin`
6. Format and write structured text output

**Property filtering:** Use `EntityProperty.Visibility` field to filter.
For `restricted` properties, check `VisibleTo`/`ExcludedFrom` lists.
This matches the existing ABAC seed policies for property access.

- [ ] **Step 4: Register command in register.go**

```go
mustRegister(command.CommandEntryConfig{
    Name:    "examine",
    Handler: ExamineHandler,
    Help:    "Inspect an object, location, or character",
    Usage:   "examine [target]",
    HelpText: "## Examine\n\nInspect details about...",
    Source:  "core",
})
```

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestExamineHandler -v ./internal/command/handlers/...`
Expected: PASS

- [ ] **Step 6: Commit**

```text
feat(command): add examine command with ABAC-filtered output
```

---

### Task 10: Implement `where` Command Handler

**Files:**

- Create: `internal/command/handlers/where.go`
- Create: `internal/command/handlers/where_test.go`
- Modify: `internal/command/handlers/register.go` (register where)

- [ ] **Step 1: Write the failing tests**

```go
func TestWhereHandler(t *testing.T) {
    tests := []struct {
        name             string
        activeSessions   []*session.Info
        visibleCharIDs   []ulid.ULID // which characters pass ABAC check
        wantCharCount    int
        wantOutputContains []string
    }{
        {"shows visible characters", twoSessions, bothVisible, 2, []string{"Gandalf", "Grand Hall"}},
        {"filters ABAC-invisible", twoSessions, onlyFirst, 1, []string{"Gandalf"}},
        {"no sessions", nil, nil, 0, []string{"0 characters online"}},
    }
}
```

Follow the `who_test.go` pattern — mock `Session().ListActive()` and
`World().GetCharacter()` with ABAC filtering. Additionally mock
`World().GetLocation()` for resolving location names.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestWhereHandler -v ./internal/command/handlers/...`
Expected: FAIL

- [ ] **Step 3: Implement WhereHandler**

Create `internal/command/handlers/where.go`:

1. Call `Session().ListActive()` to get all active sessions
2. For each session, call `World().GetCharacter()` with ABAC subject
   (same circuit breaker pattern as `who.go` — skip on permission denied,
   count engine failures, break after 3)
3. For visible characters, resolve location name via `World().GetLocation()`
   - If location lookup fails (permission denied), show `[Private]`
4. Sort results by location name, then character name
5. Format as table and output

**Reuse from who.go:** The ABAC filtering pattern (circuit breaker, error
categorization) is identical. Consider extracting a shared helper if the
duplication is significant, but don't over-abstract — two uses is fine.

- [ ] **Step 4: Register command in register.go**

```go
mustRegister(command.CommandEntryConfig{
    Name:    "where",
    Handler: WhereHandler,
    Help:    "Show where connected characters are",
    Usage:   "where",
    HelpText: "## Where\n\nList all connected characters and their locations...",
    Source:  "core",
})
```

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestWhereHandler -v ./internal/command/handlers/...`
Expected: PASS

- [ ] **Step 6: Commit**

```text
feat(command): add where command for player location discovery
```

---

## Chunk 5: Web Client Rendering and Final Verification

### Task 11: Add Web Client Event Rendering for OOC and Pemit

This is a spike task — the implementer must first explore how existing events
are rendered, then follow the same pattern.

**Files:** (determine exact paths in Step 1)

- Web client event rendering (SvelteKit components)
- Server-side event translation (gRPC → web proto)

- [ ] **Step 1: Identify rendering code path**

Search for how `say` and `page` events are rendered in the web client:

1. Find the gRPC→web event translation in `internal/web/` (look for `sendProtoEvent`
   or `EventTypeCommandResponse` handling)
2. Find the SvelteKit component that renders terminal events (search for
   `command_response` or `say` in `site/src/`)
3. Find how event types map to CSS classes or rendering logic

Document the exact file paths before proceeding.

- [ ] **Step 2: Add OOC event translation and rendering**

Server side: Add `ooc` case to the event translation switch (same location as
`say`/`pose` handling). Map `OOCPayload` fields to the web proto format.

Client side: Add `ooc` case to the rendering component. OOC events SHOULD render with:

- `[OOC]` prefix
- CSS class `ooc-message` with muted/dimmed color
- Same say/pose/semipose formatting as IC equivalents

- [ ] **Step 3: Add pemit event translation and rendering**

Server side: Add `pemit` case to event translation. Map `PemitPayload` fields.

Client side: Add `pemit` case to rendering. Pemit events SHOULD render as:

- Raw message text (no prefix)
- CSS class `pemit-message` with italic styling

- [ ] **Step 4: Verify with E2E test**

Add a Playwright E2E test that:

1. Connects a character
2. Sends `ooc hello`
3. Verifies terminal output contains `[OOC]`

Run: `task test:e2e`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(web): render ooc and pemit events in terminal
```

---

### Task 12: Run Full Test Suite and Lint

- [ ] **Step 1: Run all tests**

Run: `task test`
Expected: PASS

- [ ] **Step 2: Run linter**

Run: `task lint`
Expected: PASS (fix any issues)

- [ ] **Step 3: Run integration tests**

Run: `task test:int`
Expected: PASS

- [ ] **Step 4: Run E2E tests**

Run: `task test:e2e`
Expected: PASS

- [ ] **Step 5: Run coverage check**

Run: `task test:cover`
Expected: >80% coverage on new handler files

- [ ] **Step 6: Final commit if any fixes**

```text
fix: address lint and test issues from phase 2 commands
```

---

## File Summary

| Action | Path | Task |
| ------ | ---- | ---- |
| Modify | `internal/core/event.go` | 1 |
| Modify | `internal/core/payloads.go` | 1 |
| Modify | `internal/command/types.go` | 2 |
| Modify | `internal/world/service.go` | 2 |
| Modify | `internal/access/policy/seed.go` | 3 |
| Modify | Character creation code | 3 |
| Create | `internal/store/migrations/NNNNNN_*.sql` | 4 |
| Create | `internal/command/handlers/ooc.go` | 5 |
| Create | `internal/command/handlers/ooc_test.go` | 5 |
| Create | `internal/command/handlers/pemit.go` | 6 |
| Create | `internal/command/handlers/pemit_test.go` | 6 |
| Create | `internal/command/handlers/home.go` | 7 |
| Create | `internal/command/handlers/home_test.go` | 7 |
| Modify | `cmd/holomush/core.go` | 7 (StartingLocationID) |
| Create | `internal/command/handlers/teleport.go` | 8 |
| Create | `internal/command/handlers/teleport_test.go` | 8 |
| Create | `internal/command/handlers/examine.go` | 9 |
| Create | `internal/command/handlers/examine_test.go` | 9 |
| Create | `internal/command/handlers/where.go` | 10 |
| Create | `internal/command/handlers/where_test.go` | 10 |
| Modify | `internal/command/handlers/register.go` | 5, 6, 7, 8, 9, 10 |
| Modify | Web client rendering + event translation | 11 |

## Dependency Order

```text
Task 1 (event types) ─┐
Task 2 (WorldService) ─┼─→ Tasks 5-6 (ooc, pemit) ─→ Task 11 (web client)
Task 3 (seed policies) ┤                                      │
Task 4 (aliases) ───────┘   Tasks 7-8 (home, teleport) ───────→ Task 12 (final)
                            Tasks 9-10 (examine, where) ──────┘
```

Tasks 1-4 (infrastructure) MUST complete before command handlers.
Tasks 5-10 (commands) are independent of each other and MAY run in parallel,
except that Tasks 7-8 (home/teleport) depend on Task 3 (seed policies) for
the `character.teleport` capability.
Task 11 (web client) depends on ooc and pemit event types existing.
Task 12 (verification) runs last.
