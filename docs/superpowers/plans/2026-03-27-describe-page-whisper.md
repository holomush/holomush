<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Describe, Page & Whisper Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `describe`, `page`, and `whisper` commands for v0.1 release.

**Architecture:** `describe` and `page` are core Go handlers; `whisper` is a Lua plugin in `plugins/communication/`. Shared infrastructure includes new session fields (`last_paged`, `last_whispered`), a `FindByCharacterName` session method, and `WorldService.UpdateCharacterDescription`. Page emits to character streams only; whisper emits to both character and location streams.

**Tech Stack:** Go, gopher-lua, PostgreSQL migrations, oops error handling

**Spec:** `docs/superpowers/specs/2026-03-27-describe-page-whisper-design.md`

---

## Chunk 1: Shared Infrastructure

### Task 1: Database migration — session columns

**Files:**

- Create: `internal/store/migrations/000019_session_messaging.up.sql`
- Create: `internal/store/migrations/000019_session_messaging.down.sql`

- [ ] **Step 1: Write up migration**

```sql
-- 000019_session_messaging.up.sql
ALTER TABLE sessions
  ADD COLUMN last_paged TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_whispered TEXT NOT NULL DEFAULT '';
```

```sql
-- 000019_session_messaging.down.sql
ALTER TABLE sessions
  DROP COLUMN IF EXISTS last_paged,
  DROP COLUMN IF EXISTS last_whispered;
```

- [ ] **Step 2: Verify migration number is next in sequence**

Run: `ls internal/store/migrations/ | tail -4`
Expected: 000018 is the latest; 000019 is correct.

- [ ] **Step 3: Commit**

```text
feat(store): add session messaging columns migration
```

---

### Task 2: session.Info fields + scan updates

**Files:**

- Modify: `internal/session/session.go` (Info struct, ~line 41)
- Modify: `internal/store/session_store.go` (sessionSelectColumns, scanSession)
- Modify: `internal/session/memstore.go` (if needed for field propagation)

- [ ] **Step 1: Add fields to session.Info**

In `internal/session/session.go`, add to the `Info` struct after `UpdatedAt`:

```go
LastPaged     string
LastWhispered string
```

- [ ] **Step 2: Update sessionSelectColumns in PostgresSessionStore**

In `internal/store/session_store.go`, find the `sessionSelectColumns` const
and add `last_paged, last_whispered` to the SELECT list.

- [ ] **Step 3: Update scanSession to read the new columns**

In `internal/store/session_store.go`, find the `scanSession` function and add
`&info.LastPaged, &info.LastWhispered` to the scan destination list. Order
MUST match the SELECT column order.

- [ ] **Step 4: Update Set to persist the new fields**

In `internal/store/session_store.go`, find the `Set` method and add
`last_paged` and `last_whispered` to the INSERT/UPDATE columns and values.

- [ ] **Step 5: Run tests**

Run: `task test`
Expected: All pass (new fields default to zero values).

- [ ] **Step 6: Commit**

```text
feat(session): add LastPaged and LastWhispered fields to Info
```

---

### Task 3: New session.Access methods

**Files:**

- Modify: `internal/session/session.go` (Access interface, ~line 96)
- Modify: `internal/store/session_store.go` (PostgresSessionStore implementations)
- Modify: `internal/session/memstore.go` (MemStore implementations)
- Modify: `internal/command/handlers/testutil/mock_session.go`
- Modify: `internal/command/handlers/testutil/services.go` (defaultAccess)
- Modify: `internal/command/dispatcher_test.go` (stubAccess)
- Modify: `internal/command/types_test.go` (mockAccess)
- Modify: `test/integration/command/ratelimit_integration_test.go` (stubSessionService)
- Modify: `internal/web/handler_test.go` (mockSessionStore)

- [ ] **Step 1: Add three methods to session.Access interface**

In `internal/session/session.go`, add to the `Access` interface:

```go
// FindByCharacterName returns the active session for a character by name.
// Case-insensitive match. Returns SESSION_NOT_FOUND code if no match.
FindByCharacterName(ctx context.Context, name string) (*Info, error)

// UpdateLastPaged sets the last-paged character name for a session.
UpdateLastPaged(ctx context.Context, sessionID string, name string) error

// UpdateLastWhispered sets the last-whispered character name for a session.
UpdateLastWhispered(ctx context.Context, sessionID string, name string) error
```

Also add these to the `Store` interface if it doesn't embed `Access`
(it does — check first).

- [ ] **Step 2: Implement FindByCharacterName on PostgresSessionStore**

In `internal/store/session_store.go`:

```go
func (s *PostgresSessionStore) FindByCharacterName(ctx context.Context, name string) (*Info, error) {
	query := `SELECT ` + sessionSelectColumns + ` FROM sessions
		WHERE LOWER(character_name) = LOWER($1) AND status = 'active'`
	row := s.pool.QueryRow(ctx, query, name)
	info, err := scanSession(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.Code("SESSION_NOT_FOUND").
				With("character_name", name).
				Errorf("no active session for character %q", name)
		}
		return nil, oops.With("operation", "find_by_character_name").Wrap(err)
	}
	return info, nil
}
```

- [ ] **Step 3: Implement UpdateLastPaged on PostgresSessionStore**

```go
func (s *PostgresSessionStore) UpdateLastPaged(ctx context.Context, sessionID string, name string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET last_paged = $1 WHERE id = $2`, name, sessionID)
	if err != nil {
		return oops.With("operation", "update_last_paged").With("session_id", sessionID).Wrap(err)
	}
	return nil
}
```

- [ ] **Step 4: Implement UpdateLastWhispered on PostgresSessionStore**

Same pattern as UpdateLastPaged with `last_whispered`.

- [ ] **Step 5: Implement all three on MemStore**

In `internal/session/memstore.go`:

`FindByCharacterName`: iterate `m.sessions`, case-insensitive compare
on `CharacterName`, return first active match or `SESSION_NOT_FOUND`.

`UpdateLastPaged`: find session by ID, set `LastPaged` field.

`UpdateLastWhispered`: find session by ID, set `LastWhispered` field.

- [ ] **Step 6: Add stub implementations to all test mocks**

Add to each mock/stub that implements `session.Access`:

- `internal/command/handlers/testutil/mock_session.go` — MockSessionAccess
- `internal/command/handlers/testutil/services.go` — defaultAccess
- `internal/command/dispatcher_test.go` — stubAccess
- `internal/command/types_test.go` — mockAccess
- `test/integration/command/ratelimit_integration_test.go` — stubSessionService
- `internal/web/handler_test.go` — mockSessionStore
Each stub returns nil/empty values (no-op for updates, SESSION_NOT_FOUND for find).

Note: `internal/grpc/server_test.go` uses `session.MemStore` directly
(not a custom stub), so it does not need manual updates — MemStore
implementations from Step 5 cover it.

- [ ] **Step 7: Write tests for FindByCharacterName**

In `internal/session/memstore_test.go` (or a new file if it doesn't exist):

- Test case-insensitive match
- Test no match returns SESSION_NOT_FOUND
- Test only returns active sessions (not detached/expired)

- [ ] **Step 8: Run tests**

Run: `task test`
Expected: All pass.

- [ ] **Step 9: Commit**

```text
feat(session): add FindByCharacterName, UpdateLastPaged, UpdateLastWhispered
```

---

### Task 4: New event types

**Files:**

- Modify: `internal/core/event.go` (EventType constants)
- Modify: `internal/core/event_test.go` (TestDocumentedEventTypes validTypes map)

- [ ] **Step 1: Add event type constants**

In `internal/core/event.go`, add to the event type constants:

```go
// Messaging event types.
EventTypePage    EventType = "page"
EventTypeWhisper EventType = "whisper"
```

- [ ] **Step 2: Add payload structs**

In `internal/core/event.go`:

```go
// PagePayload is the event payload for page messages.
type PagePayload struct {
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	Message    string `json:"message"`
	IsPose     bool   `json:"is_pose"`
}

// WhisperPayload is the event payload for whisper messages on a character stream.
type WhisperPayload struct {
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	Message    string `json:"message"`
	IsPose     bool   `json:"is_pose"`
}

// WhisperNoticePayload is the event payload for whisper notices on a location stream.
type WhisperNoticePayload struct {
	SenderName string `json:"sender_name"`
	TargetName string `json:"target_name"`
	Notice     string `json:"notice"`
}
```

- [ ] **Step 3: Update TestDocumentedEventTypes**

In `internal/core/event_test.go`, add to the `validTypes` map:

```go
string(EventTypePage):    true,
string(EventTypeWhisper): true,
```

- [ ] **Step 4: Update plugin authoring docs if referenced by test**

Check `site/docs/developers/plugin-authoring.md` for event type listings.
Add `page` and `whisper` to any documented lists.

- [ ] **Step 5: Run tests**

Run: `task test`
Expected: All pass including TestDocumentedEventTypes.

- [ ] **Step 6: Commit**

```text
feat(core): add page and whisper event types with payload structs
```

---

### Task 5: WorldService.UpdateCharacterDescription

**Files:**

- Modify: `internal/command/types.go` (WorldService interface, ~line 24)
- Modify: `internal/world/service.go` (implementation)
- Create: `internal/world/service_describe_test.go` (or add to existing test file)
- Modify: Test mocks for WorldService (find with `grep -rn "WorldService" --include="*_test.go"`)

- [ ] **Step 1: Add method to WorldService interface**

In `internal/command/types.go`, add to the `WorldService` interface:

```go
// UpdateCharacterDescription sets a character's description after checking authorization.
UpdateCharacterDescription(ctx context.Context, subjectID string, characterID ulid.ULID, description string) error
```

- [ ] **Step 2: Implement on world.Service**

In `internal/world/service.go`:

```go
func (s *Service) UpdateCharacterDescription(ctx context.Context, subjectID string, characterID ulid.ULID, description string) error {
	if err := s.checkAccess(ctx, subjectID, "write", access.CharacterResource(characterID.String()), prefixCharacter); err != nil {
		return err
	}

	char, err := s.charRepo.Get(ctx, characterID)
	if err != nil {
		return oops.Code("CHARACTER_GET_FAILED").
			With("character_id", characterID.String()).Wrap(err)
	}

	char.Description = description
	if err := s.charRepo.Update(ctx, char); err != nil {
		return oops.Code("CHARACTER_UPDATE_FAILED").
			With("character_id", characterID.String()).Wrap(err)
	}
	return nil
}
```

`checkAccess` takes `(ctx, subject, action, resource string, prefix entityPrefix)`.
Follow the pattern of `GetCharacter` which uses `"read"` action + `access.CharacterResource()`

- `prefixCharacter`. For mutation, use `"write"` action.

- [ ] **Step 3: Write test for UpdateCharacterDescription**

Test: successful update, access denied, character not found.

- [ ] **Step 4: Add stub to WorldService mocks in test files**

Find all mock/stub implementations of `WorldService` and add a no-op
`UpdateCharacterDescription` method.

- [ ] **Step 5: Run tests**

Run: `task test`
Expected: All pass.

- [ ] **Step 6: Commit**

```text
feat(world): add UpdateCharacterDescription to WorldService
```

---

## Chunk 2: Describe Command

### Task 6: describe handler + tests

**Files:**

- Create: `internal/command/handlers/describe.go`
- Create: `internal/command/handlers/describe_test.go`
- Modify: `internal/command/handlers/register.go` (RegisterAll)

- [ ] **Step 1: Write failing tests for describe handler**

Create `internal/command/handlers/describe_test.go`:

```go
func TestDescribeHandler_Me(t *testing.T) {
	// describe me A tall figure with dark hair
	// → calls WorldService.UpdateCharacterDescription with the text
}

func TestDescribeHandler_Here(t *testing.T) {
	// describe here A dusty chamber
	// → calls applyProperty for "location" entity type with "description"
}

func TestDescribeHandler_TargetEquals(t *testing.T) {
	// describe sword=A gleaming blade
	// → resolves target, calls applyProperty for "object"
}

func TestDescribeHandler_NoText(t *testing.T) {
	// describe me
	// → returns ErrInvalidArgs
}

func TestDescribeHandler_EmptyArgs(t *testing.T) {
	// describe (no args)
	// → returns ErrInvalidArgs
}

func TestDescribeHandler_HerePermissionDenied(t *testing.T) {
	// describe here A dusty chamber (without objects.set capability)
	// → returns access denied error
}
```

Use `testutil.ServicesBuilder` to wire up mock WorldService. Follow the
pattern in `say_test.go` and `objects_test.go` for CommandExecution setup.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestDescribeHandler -count=1 ./internal/command/handlers/`
Expected: FAIL — DescribeHandler not defined.

- [ ] **Step 3: Implement DescribeHandler**

Create `internal/command/handlers/describe.go`:

```go
// DescribeHandler sets the description of a character, object, or location.
// Syntax:
//   describe me <text>       — set own character description
//   describe here <text>     — set current location description
//   describe <target>=<text> — set named target description
func DescribeHandler(ctx context.Context, exec *command.CommandExecution) error {
	args := strings.TrimSpace(exec.Args)
	if args == "" {
		return command.ErrInvalidArgs("describe", "describe me <text>")
	}

	// Parse: "me <text>", "here <text>", or "<target>=<text>"
	target, text, err := parseDescribeArgs(args)
	if err != nil {
		return err
	}
	if text == "" {
		return command.ErrInvalidArgs("describe", "describe me <text>")
	}

	if target == "me" {
		return describeSelf(ctx, exec, text)
	}

	return describeTarget(ctx, exec, target, text)
}
```

`parseDescribeArgs`: check for `me` prefix, `here` prefix, otherwise
split on `=`. Return (target, text, error).

`describeSelf`: check `player.describe` capability via ABAC engine,
then call `exec.Services().World().UpdateCharacterDescription(ctx, subjectID, charID, text)`.

`describeTarget`: resolve target via `resolveTarget`, look up `description`
property, call `applyProperty`. Same flow as `set` command but hardcoded
to `description` property.

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestDescribeHandler -count=1 ./internal/command/handlers/`
Expected: All pass.

- [ ] **Step 5: Register in RegisterAll**

In `internal/command/handlers/register.go`, add:

```go
mustRegister(command.CommandEntryConfig{
	Name:    "describe",
	Handler: DescribeHandler,
	Help:    "Set a description",
	Usage:   "describe me <text>",
	HelpText: `## Describe

Set the description of your character, the current location, or an object.

### Usage

- ` + "`describe me <text>`" + ` - Set your character's description
- ` + "`describe here <text>`" + ` - Set the location's description
- ` + "`describe <target>=<text>`" + ` - Set a named target's description

### Examples

- ` + "`describe me A tall figure with dark hair and bright eyes.`" + `
- ` + "`describe here A dusty chamber lit by flickering torches.`" + `
- ` + "`describe sword=A gleaming blade etched with runes.`" + ``,
	Source: "core",
})
```

No capabilities at registration level — checked inside handler.

- [ ] **Step 6: Run full tests + lint**

Run: `task test && task lint:go`
Expected: All pass, 0 lint issues.

- [ ] **Step 7: Commit**

```text
feat(command): add describe handler for character/location/object descriptions
```

---

## Chunk 3: Page Command

### Task 7: page handler + tests

**Files:**

- Create: `internal/command/handlers/page.go`
- Create: `internal/command/handlers/page_test.go`
- Modify: `internal/command/handlers/register.go` (RegisterAll)

- [ ] **Step 1: Write failing tests for page handler**

Create `internal/command/handlers/page_test.go`:

```go
func TestPageHandler_Basic(t *testing.T) {
	// page alex=Hey there
	// → emits page event to Alex's character stream
	// → emits command_response to sender's character stream
	// → updates session.LastPaged to "Alex"
}

func TestPageHandler_LastPaged(t *testing.T) {
	// Set session.LastPaged = "Alex"
	// page How's it going?
	// → pages Alex with "How's it going?"
}

func TestPageHandler_NoLastPaged(t *testing.T) {
	// page How's it going? (no last-paged set)
	// → returns error "Page who?"
}

func TestPageHandler_Pose(t *testing.T) {
	// page alex=:waves
	// → page event has is_pose=true
	// → sender sees "Long distance to Alex: Sean waves."
	// → target sees "From afar, Sean waves."
}

func TestPageHandler_PoseSemicolon(t *testing.T) {
	// page alex=;'s jaw drops
	// → sender sees "Long distance to Alex: Sean's jaw drops."
}

func TestPageHandler_TargetNotFound(t *testing.T) {
	// page nobody=Hey
	// → error "No one named "nobody" is connected."
}

func TestPageHandler_EmptyMessage(t *testing.T) {
	// page alex=
	// → returns ErrInvalidArgs
}

func TestPageHandler_NoArgs(t *testing.T) {
	// page (no args)
	// → returns ErrInvalidArgs
}
```

Tests need:

- MockSessionAccess with `FindByCharacterName` returning a target session
- MockEventStore to capture appended events
- Verify page event payload on target character stream
- Verify command_response output on sender's io.Writer

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestPageHandler -count=1 ./internal/command/handlers/`
Expected: FAIL — PageHandler not defined.

- [ ] **Step 3: Implement PageHandler**

Create `internal/command/handlers/page.go`:

```go
func PageHandler(ctx context.Context, exec *command.CommandExecution) error {
	args := strings.TrimSpace(exec.Args)
	if args == "" {
		return command.ErrInvalidArgs("page", "page <name>=<message>")
	}

	target, message := parsePageArgs(args, exec)
	if target == "" {
		// No = and no last-paged
		return command.ErrInvalidArgs("page", "Page who? Use: page <name>=<message>")
	}
	if message == "" {
		return command.ErrInvalidArgs("page", "page <name>=<message>")
	}

	// Find target session
	targetSession, err := exec.Services().Session().FindByCharacterName(ctx, target)
	if err != nil {
		writeOutputf(ctx, exec, "page", "No one named %q is connected.\n", target)
		return nil // User-facing error, not infrastructure failure
	}

	// Detect pose mode
	isPose, displayMessage := formatPageMessage(exec.CharacterName(), message)

	// Emit page event to target's character stream
	// ... (marshal PagePayload, append to character:targetCharID stream)

	// Emit command_response to sender
	// ... (format sender confirmation)

	// Update last-paged
	_ = exec.Services().Session().UpdateLastPaged(ctx, exec.SessionID().String(), targetSession.CharacterName)

	return nil
}
```

`parsePageArgs`: if `=` present, split. Otherwise use `exec` session's
`LastPaged` as target, entire args as message. Need to read session info
from the session store to get `LastPaged` — the handler needs the session
ID. `exec.SessionID()` provides it; fetch the session to read `LastPaged`.

`formatPageMessage`: check `:` or `;` prefix → pose mode. Return
(isPose bool, formattedMessage string).

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestPageHandler -count=1 ./internal/command/handlers/`
Expected: All pass.

- [ ] **Step 5: Register in RegisterAll**

```go
mustRegister(command.CommandEntryConfig{
	Name:         "page",
	Handler:      PageHandler,
	Capabilities: []string{"comms.page"},
	Help:         "Send a private message",
	Usage:        "page <name>=<message>",
	HelpText: `## Page

Send a private out-of-character message to another player.

### Usage

- ` + "`page <name>=<message>`" + ` - Page a player
- ` + "`page <message>`" + ` - Page the last person you paged
- ` + "`page <name>=:<action>`" + ` - Pose-page

### Examples

- ` + "`page Alex=Hey, want to join the scene?`" + `
- ` + "`page How about now?`" + ` - Pages Alex again
- ` + "`page Alex=:waves hello.`" + ` - Alex sees: From afar, Sean waves hello.`,
	Source: "core",
})
```

- [ ] **Step 6: Run full tests + lint**

Run: `task test && task lint:go`
Expected: All pass, 0 lint issues.

- [ ] **Step 7: Commit**

```text
feat(command): add page handler for OOC private messaging
```

---

## Chunk 4: Whisper Command (Lua Plugin)

### Task 8: Extend RegisterStdlib for session dependency

**Files:**

- Modify: `internal/plugin/hostfunc/stdlib.go` (RegisterStdlib signature, add session namespace)
- Modify: All callers of RegisterStdlib (search: `RegisterStdlib(`)
- Create: `internal/plugin/hostfunc/stdlib_session_test.go`

- [ ] **Step 1: Find all callers of RegisterStdlib**

Run: `rg 'RegisterStdlib\(' --type go -n`

Update the function signature and all call sites.

- [ ] **Step 2: Add session.Access parameter to RegisterStdlib**

```go
func RegisterStdlib(ls *lua.LState, sessAccess session.Access) {
```

If `sessAccess` is non-nil, register the `holo.session` namespace.

- [ ] **Step 3: Implement holo.session.find_by_name**

In `internal/plugin/hostfunc/stdlib.go`, add a `registerSession` function:

```go
func registerSession(ls *lua.LState, holoTable *lua.LTable, sessAccess session.Access) {
	sessionTable := ls.NewTable()

	sessionTable.RawSetString("find_by_name", ls.NewFunction(func(ls *lua.LState) int {
		name := ls.CheckString(1)
		ctx := context.Background() // or extract from Lua state
		info, err := sessAccess.FindByCharacterName(ctx, name)
		if err != nil {
			ls.Push(lua.LNil)
			return 1
		}
		result := ls.NewTable()
		result.RawSetString("character_id", lua.LString(info.CharacterID.String()))
		result.RawSetString("character_name", lua.LString(info.CharacterName))
		result.RawSetString("location_id", lua.LString(info.LocationID.String()))
		ls.Push(result)
		return 1
	}))

	holoTable.RawSetString("session", sessionTable)
}
```

- [ ] **Step 4: Implement holo.session.set_last_whispered**

Add to the session table in `registerSession`:

```go
sessionTable.RawSetString("set_last_whispered", ls.NewFunction(func(ls *lua.LState) int {
	sessionID := ls.CheckString(1)
	name := ls.CheckString(2)
	ctx := context.Background()
	_ = sessAccess.UpdateLastWhispered(ctx, sessionID, name)
	return 0
}))
```

Note: The session ID must be available in the Lua command context. Check
how the command context (`ctx.session_id`) is set in the Lua host. The
`on_command(ctx)` function receives a context table — ensure `session_id`
is populated there.

- [ ] **Step 5: Write tests**

Test `find_by_name` returns correct table for existing session, nil for missing.
Test `set_last_whispered` calls UpdateLastWhispered on the mock.

- [ ] **Step 6: Run tests + lint**

Run: `task test && task lint:go`

- [ ] **Step 7: Commit**

```text
feat(plugin): add holo.session namespace with find_by_name and set_last_whispered
```

---

### Task 9: Whisper Lua handler + plugin registration

**Files:**

- Modify: `plugins/communication/plugin.yaml` (add whisper command)
- Modify: `plugins/communication/main.lua` (add whisper handler)

- [ ] **Step 1: Add whisper command to plugin.yaml**

In `plugins/communication/plugin.yaml`, add to the `commands` list:

```yaml
  - name: whisper
    capabilities:
      - comms.whisper
    help: "Whisper to someone in the room"
    usage: "whisper <character>=<message>"
    helpText: |
      ## Whisper

      Whisper a private in-character message to someone in your location.
      Others see that you whispered, but not what you said.

      ### Usage

      - `whisper <name>=<message>` - Whisper to someone
      - `whisper <message>` - Whisper to the last person you whispered to
      - `whisper <name>=:<action>` - Pose-whisper

      ### Examples

      - `whisper Alex=Meet me at the tavern`
      - `whisper Alex=:nods knowingly.`
```

Also add `session.query` to the policies section so the plugin can call
`holo.session.find_by_name`.

- [ ] **Step 2: Implement whisper handler in main.lua**

Add to `plugins/communication/main.lua`:

```lua
function on_command(ctx)
    if ctx.name == "say" then
        return handle_say(ctx)
    elseif ctx.name == "pose" then
        return handle_pose(ctx)
    elseif ctx.name == "emit" then
        return handle_emit(ctx)
    elseif ctx.name == "whisper" then
        return handle_whisper(ctx)
    end
    return nil
end

function handle_whisper(ctx)
    local target_name, message = parse_whisper_args(ctx.args, ctx)
    if target_name == nil then
        holo.emit.character(ctx.character_id, "command_response", {
            text = "Whisper to whom? Use: whisper <name>=<message>",
            is_error = true
        })
        return holo.emit.flush()
    end
    if message == "" or message == nil then
        holo.emit.character(ctx.character_id, "command_response", {
            text = "Whisper what?",
            is_error = true
        })
        return holo.emit.flush()
    end

    -- Find target session
    local target = holo.session.find_by_name(target_name)
    if target == nil then
        holo.emit.character(ctx.character_id, "command_response", {
            text = 'No one named "' .. target_name .. '" is connected.',
            is_error = true
        })
        return holo.emit.flush()
    end

    -- Same-location check
    if target.location_id ~= ctx.location_id then
        holo.emit.character(ctx.character_id, "command_response", {
            text = 'You don\'t see anyone named "' .. target_name .. '" here.',
            is_error = true
        })
        return holo.emit.flush()
    end

    -- Detect pose mode
    local is_pose = false
    local display_message = message
    local sender_display = message
    if message:sub(1, 1) == ":" then
        is_pose = true
        display_message = "From nearby, " .. ctx.character_name .. " " .. message:sub(2)
        sender_display = message:sub(2)
    elseif message:sub(1, 1) == ";" then
        is_pose = true
        display_message = "From nearby, " .. ctx.character_name .. message:sub(2)
        sender_display = message:sub(2)
    end

    -- Emit to location stream (notice only)
    holo.emit.location(ctx.location_id, "whisper", {
        sender_name = ctx.character_name,
        target_name = target.character_name,
        notice = ctx.character_name .. " whispers to " .. target.character_name .. "."
    })

    -- Emit to target's character stream (full content)
    if is_pose then
        holo.emit.character(target.character_id, "whisper", {
            sender_id = ctx.character_id,
            sender_name = ctx.character_name,
            message = display_message,
            is_pose = true
        })
    else
        holo.emit.character(target.character_id, "whisper", {
            sender_id = ctx.character_id,
            sender_name = ctx.character_name,
            message = ctx.character_name .. ' whispers, "' .. message .. '"',
            is_pose = false
        })
    end

    -- Emit confirmation to sender
    if is_pose then
        holo.emit.character(ctx.character_id, "command_response", {
            text = "You whisper-pose to " .. target.character_name .. ": " .. sender_display,
            is_error = false
        })
    else
        holo.emit.character(ctx.character_id, "command_response", {
            text = "You whisper to " .. target.character_name .. ": " .. message,
            is_error = false
        })
    end

    -- Update last-whispered
    holo.session.set_last_whispered(ctx.session_id, target.character_name)

    return holo.emit.flush()
end

function parse_whisper_args(args, ctx)
    if args == nil or args == "" then
        return nil, nil
    end
    local eq_pos = args:find("=")
    if eq_pos then
        local name = args:sub(1, eq_pos - 1)
        local msg = args:sub(eq_pos + 1)
        return name, msg
    end
    -- No = sign: use last-whispered target
    -- Need to get last_whispered from context
    -- This requires ctx.last_whispered to be populated
    if ctx.last_whispered and ctx.last_whispered ~= "" then
        return ctx.last_whispered, args
    end
    return nil, nil
end
```

- [ ] **Step 3: Inject last_whispered into Lua command context**

In `internal/plugin/hostfunc/commands.go`, find where the command context
table is built (the table passed to `on_command(ctx)`). Add a
`last_whispered` field from the session info. This requires the command
context builder to have access to session info — check how `session_id`
and other session fields are currently populated, and add `last_whispered`
following the same pattern. If session info is not currently available
in the context builder, extend the builder to accept it.

- [ ] **Step 4: Run communication plugin integration test**

Run: `task test -- -run TestCommunication -count=1 ./internal/plugin/`
Expected: Existing tests still pass, whisper command appears in manifest.

- [ ] **Step 4: Commit**

```text
feat(plugin): add whisper command to communication plugin
```

---

## Chunk 5: System Aliases + E2E

### Task 10: Register aliases

**Files:**

- Modify: `internal/command/handlers/register.go`

- [ ] **Step 1: Add alias registrations**

In `RegisterAll`, add `desc` as a second registration for `DescribeHandler`,
and `p` as a second registration for `PageHandler`:

```go
mustRegister(command.CommandEntryConfig{
	Name:    "desc",
	Handler: DescribeHandler,
	Help:    "Set a description (alias for describe)",
	Usage:   "desc me <text>",
	Source:  "core",
})

mustRegister(command.CommandEntryConfig{
	Name:         "p",
	Handler:      PageHandler,
	Capabilities: []string{"comms.page"},
	Help:         "Send a private message (alias for page)",
	Usage:        "p <name>=<message>",
	Source:       "core",
})
```

`w` for whisper: add a second command entry in
`plugins/communication/plugin.yaml` with `name: w` pointing to the same
handler as `whisper`. This is the same pattern used for `:` → pose.

- [ ] **Step 2: Run tests + lint**

Run: `task test && task lint:go`

- [ ] **Step 3: Commit**

```text
feat(command): register desc and p as core command aliases
```

---

### Task 11: E2E tests

**Files:**

- Create: `test/integration/telnet/messaging_test.go` (Ginkgo suite, follows existing pattern in `test/integration/telnet/`)

- [ ] **Step 1: Add describe E2E test**

Test that `describe me A tall figure` sets the description, and `look me`
(or `look` for location) shows it.

- [ ] **Step 2: Add page E2E test**

Two concurrent telnet sessions. Session A pages Session B. Verify Session B
receives the page event.

- [ ] **Step 3: Add whisper E2E test**

Two concurrent telnet sessions in the same location. Session A whispers to
Session B. Verify:

- Session B receives the whisper content
- Both sessions see the location whisper notice

- [ ] **Step 4: Run E2E tests**

Run: `task test:e2e`
Expected: All pass including new tests.

- [ ] **Step 5: Commit**

```text
test(e2e): add describe, page, and whisper end-to-end tests
```

---

## Post-Implementation Checklist

- [ ] `task test` — all unit tests pass
- [ ] `task lint` — clean
- [ ] `task test:int` — integration tests pass
- [ ] `task test:e2e` — E2E tests pass
- [ ] Close beads: `bd close holomush-a3a7.1 holomush-a3a7.2`
- [ ] Create PR
