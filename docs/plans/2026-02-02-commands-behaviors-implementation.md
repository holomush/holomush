# Commands & Behaviors Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the command system with Go core commands, Lua plugin commands, alias resolution, and capability-gated dispatch.

**Architecture:** Unified command registry with protocol-agnostic parsing, alias resolution with caching, capability checks via ABAC integration, and Lua plugin dispatch via existing host function infrastructure.

**Tech Stack:** Go 1.23+, PostgreSQL (aliases), gopher-lua (plugin commands), oops (errors), OTel (tracing/metrics)

**Related:**

- [Plugin Stdlib Design](2026-02-02-plugin-stdlib-design.md) — Standard library for Lua/Go plugins (formatting, typed contexts, event emission)

---

## Phase 0: ADRs (Foundation Decisions)

### Task 0.1: ADR - Unified Command Registry

**Files:**

- Create: `docs/adr/0006-unified-command-registry.md`

**Step 1: Write ADR**

```markdown
# ADR 0006: Unified Command Registry

**Date:** 2026-02-02
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH supports both Go core commands and Lua plugin commands. We need to decide
how to organize command registration and lookup.

## Options Considered

### Option A: Separate registries per implementation type

- Go commands in one registry, Lua in another
- Dispatcher checks both, Go takes precedence

### Option B: Unified registry with source tracking

- Single map from command name to handler
- Each entry tracks source ("core" or plugin name)
- Plugins can override core if explicitly configured

## Decision

**Option B: Unified registry.** Single registration point provides:

- Uniform error handling and help introspection
- Clear conflict detection with warnings
- Plugins can override builtins if admin allows
- Simpler dispatch path

## Consequences

- Command registration API is the same for Go and Lua
- Conflict resolution uses alphabetical plugin load order, last wins with warning
- Help system queries one registry for all commands
```

**Step 2: Commit**

```bash
git add docs/adr/0006-unified-command-registry.md
git commit -m "docs(adr): add ADR 0006 unified command registry"
```

Expected: ADR documents the unified registry decision

---

### Task 0.2: ADR - Command Security Model

**Files:**

- Create: `docs/adr/0007-command-security-model.md`

**Step 1: Write ADR**

```markdown
# ADR 0007: Command Security Model

**Date:** 2026-02-02
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

Commands need authorization checks. Options include role-based (admin/player) or
capability-based (fine-grained permissions like `world.look`, `admin.boot`).

## Decision

**Capability-based (fine-grained).** Each command declares required capabilities.
Dispatcher checks ALL capabilities before execution.

Benefits:

- Integrates with existing ABAC system (`internal/access`)
- Granular permissions without role explosion
- Uniform for Go and Lua commands
- Wildcard support (`world.*`) for convenience

## Consequences

- Every command MUST declare capabilities in registration
- Default character grants: `world.*`, `comms.*`, `player.*`
- Build and admin capabilities require explicit grants
- Capability check delegates to `access.Evaluator.Evaluate()`
```

**Step 2: Commit**

```bash
git add docs/adr/0007-command-security-model.md
git commit -m "docs(adr): add ADR 0007 command security model"
```

Expected: ADR documents capability-based security

---

### Task 0.3: ADR - Command Conflict Resolution

**Files:**

- Create: `docs/adr/0008-command-conflict-resolution.md`

**Step 1: Write ADR**

```markdown
# ADR 0008: Command Conflict Resolution

**Date:** 2026-02-02
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

Multiple plugins may register the same command name. We need deterministic
conflict resolution.

## Decision

**Alphabetical load order, last-loaded wins with warning.**

- Plugins load in alphabetical order by name
- Within a plugin, commands register in manifest order
- Last registration wins (overwrites previous)
- Server MUST log warning on conflict
- Plugins SHOULD NOT override core commands without explicit admin configuration

## Consequences

- Deterministic behavior across restarts
- Admins see warnings about conflicts in logs
- Can disable problematic plugins to resolve conflicts
- Future: may add explicit override configuration
```

**Step 2: Commit**

```bash
git add docs/adr/0008-command-conflict-resolution.md
git commit -m "docs(adr): add ADR 0008 command conflict resolution"
```

Expected: ADR documents conflict resolution strategy

---

## Phase 1: Command Registry & Parser (Core Infrastructure)

### Task 1.1: Command Types and Interfaces

**Files:**

- Create: `internal/command/types.go`
- Test: `internal/command/types_test.go`

**Step 1: Write the failing test**

```go
// internal/command/types_test.go
package command

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestCommandExecution_HasServices(t *testing.T) {
    exec := &CommandExecution{}
    assert.Nil(t, exec.Services, "Services should be nil when not set")
}

func TestServices_HasAllDependencies(t *testing.T) {
    svc := &Services{}
    assert.Nil(t, svc.World, "World service should be nil when not set")
    assert.Nil(t, svc.Session, "Session service should be nil when not set")
    assert.Nil(t, svc.Access, "Access service should be nil when not set")
    assert.Nil(t, svc.Events, "Events service should be nil when not set")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/command/... -run TestCommandExecution -v`
Expected: FAIL with "package internal/command is not in std"

**Step 3: Write minimal implementation**

```go
// internal/command/types.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package command provides the command registry, parser, and dispatch system.
package command

import (
    "context"
    "io"

    "github.com/oklog/ulid/v2"

    "github.com/holomush/holomush/internal/access"
    "github.com/holomush/holomush/internal/core"
    "github.com/holomush/holomush/internal/world"
)

// CommandHandler is the function signature for command handlers.
type CommandHandler func(ctx context.Context, exec *CommandExecution) error

// CommandEntry represents a registered command.
type CommandEntry struct {
    Name         string         // canonical name (e.g., "say")
    Handler      CommandHandler // Go handler or Lua dispatcher
    Capabilities []string       // ALL required capabilities (AND logic)
    Help         string         // short description (one line)
    Usage        string         // usage pattern (e.g., "say <message>")
    HelpText     string         // detailed markdown help
    Source       string         // "core" or plugin name
}

// CommandExecution provides context for command execution.
type CommandExecution struct {
    CharacterID   ulid.ULID
    LocationID    ulid.ULID
    CharacterName string
    PlayerID      ulid.ULID
    SessionID     ulid.ULID
    Args          string
    Output        io.Writer
    Services      *Services
}

// Services provides access to core services for command handlers.
type Services struct {
    World   world.Service
    Session core.SessionManager
    Access  access.AccessControl
    Events  core.EventStore
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/command/... -run TestCommandExecution -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/command/
git commit -m "feat(command): add command types and interfaces"
```

Expected: Command types defined with proper dependencies

---

### Task 1.2: Command Registry Implementation

**Files:**

- Create: `internal/command/registry.go`
- Test: `internal/command/registry_test.go`

**Step 1: Write the failing test**

```go
// internal/command/registry_test.go
package command

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
    reg := NewRegistry()

    handler := func(ctx context.Context, exec *CommandExecution) error {
        return nil
    }

    entry := CommandEntry{
        Name:         "look",
        Handler:      handler,
        Capabilities: []string{"world.look"},
        Help:         "Look at your surroundings",
        Usage:        "look [target]",
        Source:       "core",
    }

    err := reg.Register(entry)
    require.NoError(t, err)

    got, ok := reg.Get("look")
    assert.True(t, ok)
    assert.Equal(t, "look", got.Name)
    assert.Equal(t, []string{"world.look"}, got.Capabilities)
}

func TestRegistry_GetNotFound(t *testing.T) {
    reg := NewRegistry()
    _, ok := reg.Get("nonexistent")
    assert.False(t, ok)
}

func TestRegistry_All(t *testing.T) {
    reg := NewRegistry()

    reg.Register(CommandEntry{Name: "look", Source: "core"})
    reg.Register(CommandEntry{Name: "say", Source: "comms"})

    all := reg.All()
    assert.Len(t, all, 2)
}

func TestRegistry_ConflictWarning(t *testing.T) {
    reg := NewRegistry()

    reg.Register(CommandEntry{Name: "look", Source: "core"})
    err := reg.Register(CommandEntry{Name: "look", Source: "plugin-a"})

    // Should succeed but we can check it overwrote
    require.NoError(t, err)
    got, _ := reg.Get("look")
    assert.Equal(t, "plugin-a", got.Source)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/command/... -run TestRegistry -v`
Expected: FAIL with "undefined: NewRegistry"

**Step 3: Write minimal implementation**

```go
// internal/command/registry.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
    "log/slog"
    "sync"
)

// Registry manages command registration and lookup.
type Registry struct {
    commands map[string]CommandEntry
    mu       sync.RWMutex
}

// NewRegistry creates a new command registry.
func NewRegistry() *Registry {
    return &Registry{
        commands: make(map[string]CommandEntry),
    }
}

// Register adds a command to the registry.
// If a command with the same name exists, it is overwritten and a warning is logged.
func (r *Registry) Register(entry CommandEntry) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if existing, ok := r.commands[entry.Name]; ok {
        slog.Warn("command conflict: overwriting existing command",
            "command", entry.Name,
            "previous_source", existing.Source,
            "new_source", entry.Source)
    }

    r.commands[entry.Name] = entry
    return nil
}

// Get retrieves a command by name.
func (r *Registry) Get(name string) (CommandEntry, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    entry, ok := r.commands[name]
    return entry, ok
}

// All returns all registered commands.
func (r *Registry) All() []CommandEntry {
    r.mu.RLock()
    defer r.mu.RUnlock()

    entries := make([]CommandEntry, 0, len(r.commands))
    for _, e := range r.commands {
        entries = append(entries, e)
    }
    return entries
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/command/... -run TestRegistry -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/command/registry.go internal/command/registry_test.go
git commit -m "feat(command): implement command registry with conflict detection"
```

Expected: Registry with register/get/all operations

---

### Task 1.3: Command Name Validation

**Files:**

- Create: `internal/command/validation.go`
- Test: `internal/command/validation_test.go`

**Step 1: Write the failing test**

```go
// internal/command/validation_test.go
package command

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestValidateCommandName(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"simple lowercase", "look", false},
        {"with at prefix", "@create", true},
        {"with plus prefix", "+who", true},
        {"with underscore", "my_cmd", false},
        {"with question mark", "say?", false},
        {"max length 20", "abcdefghijklmnopqrst", false},
        {"too long 21", "abcdefghijklmnopqrstu", true},
        {"starts with digit", "123go", true},
        {"starts with star", "*star", true},
        {"empty", "", true},
        {"only spaces", "   ", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateCommandName(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}

func TestValidateAliasName(t *testing.T) {
    // Aliases follow same rules as commands
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"single letter", "l", false},
        {"lowercase", "look", false},
        {"starts with digit", "1look", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateAliasName(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/command/... -run TestValidate -v`
Expected: FAIL with "undefined: ValidateCommandName"

**Step 3: Write minimal implementation**

```go
// internal/command/validation.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
    "regexp"
    "strings"

    "github.com/samber/oops"
)

const (
    // MaxNameLength is the maximum length for command and alias names.
    MaxNameLength = 20
)

// namePattern validates command/alias names: must start with letter,
// followed by letters, digits, or special chars: _!?@#$%^+-
var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_!?@#$%^+\-]{0,19}$`)

// ValidateCommandName validates a command name.
func ValidateCommandName(name string) error {
    return validateName(name, "command")
}

// ValidateAliasName validates an alias name.
func ValidateAliasName(name string) error {
    return validateName(name, "alias")
}

func validateName(name, kind string) error {
    trimmed := strings.TrimSpace(name)
    if trimmed == "" {
        return oops.Code("INVALID_NAME").
            With("kind", kind).
            Errorf("%s name cannot be empty", kind)
    }

    if len(trimmed) > MaxNameLength {
        return oops.Code("INVALID_NAME").
            With("kind", kind).
            With("length", len(trimmed)).
            With("max", MaxNameLength).
            Errorf("%s name exceeds maximum length of %d", kind, MaxNameLength)
    }

    if !namePattern.MatchString(trimmed) {
        return oops.Code("INVALID_NAME").
            With("kind", kind).
            With("name", trimmed).
            Errorf("%s name must start with a letter and contain only letters, digits, or _!?@#$%%^+-", kind)
    }

    return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/command/... -run TestValidate -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/command/validation.go internal/command/validation_test.go
git commit -m "feat(command): add command and alias name validation"
```

Expected: Validation with regex pattern matching spec

---

### Task 1.4: Command Parser

**Files:**

- Create: `internal/command/parser.go`
- Test: `internal/command/parser_test.go`

**Step 1: Write the failing test**

```go
// internal/command/parser_test.go
package command

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
    tests := []struct {
        name        string
        input       string
        wantCmd     string
        wantArgs    string
        wantErr     bool
    }{
        {
            name:     "simple command",
            input:    "look",
            wantCmd:  "look",
            wantArgs: "",
        },
        {
            name:     "command with args",
            input:    "say hello world",
            wantCmd:  "say",
            wantArgs: "hello world",
        },
        {
            name:     "command with leading whitespace",
            input:    "   look",
            wantCmd:  "look",
            wantArgs: "",
        },
        {
            name:     "command with trailing whitespace",
            input:    "look   ",
            wantCmd:  "look",
            wantArgs: "",
        },
        {
            name:     "preserves internal arg whitespace",
            input:    "say   hello    world",
            wantCmd:  "say",
            wantArgs: "hello    world",
        },
        {
            name:    "empty input",
            input:   "",
            wantErr: true,
        },
        {
            name:    "whitespace only",
            input:   "   ",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            parsed, err := Parse(tt.input)
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.wantCmd, parsed.Name)
            assert.Equal(t, tt.wantArgs, parsed.Args)
            assert.Equal(t, tt.input, parsed.Raw)
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/command/... -run TestParse -v`
Expected: FAIL with "undefined: Parse"

**Step 3: Write minimal implementation**

```go
// internal/command/parser.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
    "strings"

    "github.com/samber/oops"
)

// ParsedCommand represents a parsed command input.
type ParsedCommand struct {
    Name string // command name
    Args string // unparsed argument string
    Raw  string // original input
}

// Parse splits raw input into command name and arguments.
// The command name is the first whitespace-delimited token.
// Arguments preserve internal whitespace.
func Parse(input string) (*ParsedCommand, error) {
    trimmed := strings.TrimSpace(input)
    if trimmed == "" {
        return nil, oops.Code("EMPTY_INPUT").Errorf("no command provided")
    }

    // Split on first whitespace
    parts := strings.SplitN(trimmed, " ", 2)
    name := parts[0]

    var args string
    if len(parts) > 1 {
        // Trim leading whitespace from args but preserve internal whitespace
        args = strings.TrimLeft(parts[1], " \t")
    }

    return &ParsedCommand{
        Name: name,
        Args: args,
        Raw:  input,
    }, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/command/... -run TestParse -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/command/parser.go internal/command/parser_test.go
git commit -m "feat(command): implement command parser"
```

Expected: Parser splits input into command + args

---

### Task 1.5: Error Types

**Files:**

- Create: `internal/command/errors.go`
- Test: `internal/command/errors_test.go`

**Step 1: Write the failing test**

```go
// internal/command/errors_test.go
package command

import (
    "testing"

    "github.com/samber/oops"
    "github.com/stretchr/testify/assert"
)

func TestErrUnknownCommand(t *testing.T) {
    err := ErrUnknownCommand("foo")
    assert.Error(t, err)

    oopsErr, ok := oops.AsOops(err)
    assert.True(t, ok)
    assert.Equal(t, "UNKNOWN_COMMAND", oopsErr.Code())
    assert.Equal(t, "foo", oopsErr.Context()["command"])
}

func TestErrPermissionDenied(t *testing.T) {
    err := ErrPermissionDenied("boot", "admin.boot")
    oopsErr, _ := oops.AsOops(err)
    assert.Equal(t, "PERMISSION_DENIED", oopsErr.Code())
    assert.Equal(t, "boot", oopsErr.Context()["command"])
    assert.Equal(t, "admin.boot", oopsErr.Context()["capability"])
}

func TestPlayerMessage(t *testing.T) {
    tests := []struct {
        name     string
        err      error
        expected string
    }{
        {
            name:     "world error with message",
            err:      WorldError("There's no exit to the north.", nil),
            expected: "There's no exit to the north.",
        },
        {
            name:     "unknown command",
            err:      ErrUnknownCommand("foo"),
            expected: "Unknown command. Try 'help'.",
        },
        {
            name:     "permission denied",
            err:      ErrPermissionDenied("boot", "admin.boot"),
            expected: "You don't have permission to do that.",
        },
        {
            name:     "generic error",
            err:      oops.Errorf("something broke"),
            expected: "Something went wrong. Try again.",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            msg := PlayerMessage(tt.err)
            assert.Equal(t, tt.expected, msg)
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/command/... -run TestErr -v`
Expected: FAIL with "undefined: ErrUnknownCommand"

**Step 3: Write minimal implementation**

```go
// internal/command/errors.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
    "github.com/samber/oops"
)

// Error codes for command dispatch failures.
const (
    CodeUnknownCommand   = "UNKNOWN_COMMAND"
    CodePermissionDenied = "PERMISSION_DENIED"
    CodeInvalidArgs      = "INVALID_ARGS"
    CodeWorldError       = "WORLD_ERROR"
    CodeRateLimited      = "RATE_LIMITED"
)

// ErrUnknownCommand creates an error for an unknown command.
func ErrUnknownCommand(cmd string) error {
    return oops.Code(CodeUnknownCommand).
        With("command", cmd).
        Errorf("unknown command: %s", cmd)
}

// ErrPermissionDenied creates an error for permission denial.
func ErrPermissionDenied(cmd, capability string) error {
    return oops.Code(CodePermissionDenied).
        With("command", cmd).
        With("capability", capability).
        Errorf("permission denied for command %s", cmd)
}

// ErrInvalidArgs creates an error for invalid arguments.
func ErrInvalidArgs(cmd, usage string) error {
    return oops.Code(CodeInvalidArgs).
        With("command", cmd).
        With("usage", usage).
        Errorf("invalid arguments")
}

// WorldError creates an error for world state issues with a player-facing message.
func WorldError(message string, cause error) error {
    builder := oops.Code(CodeWorldError).With("message", message)
    if cause != nil {
        return builder.Wrap(cause)
    }
    return builder.Errorf("%s", message)
}

// ErrRateLimited creates an error for rate limiting.
func ErrRateLimited(cooldownMs int64) error {
    return oops.Code(CodeRateLimited).
        With("cooldown_ms", cooldownMs).
        Errorf("Too many commands. Please slow down.")
}

// PlayerMessage extracts a player-facing message from an error.
func PlayerMessage(err error) string {
    oopsErr, ok := oops.AsOops(err)
    if !ok {
        return "Something went wrong. Try again."
    }

    switch oopsErr.Code() {
    case CodeUnknownCommand:
        return "Unknown command. Try 'help'."
    case CodePermissionDenied:
        return "You don't have permission to do that."
    case CodeInvalidArgs:
        if usage, ok := oopsErr.Context()["usage"].(string); ok && usage != "" {
            return "Usage: " + usage
        }
        return "Invalid arguments."
    case CodeWorldError:
        if msg, ok := oopsErr.Context()["message"].(string); ok {
            return msg
        }
        return "Something went wrong. Try again."
    case CodeRateLimited:
        return "Too many commands. Please slow down."
    default:
        return "Something went wrong. Try again."
    }
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/command/... -run TestErr -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/command/errors.go internal/command/errors_test.go
git commit -m "feat(command): add error types with player-facing messages"
```

Expected: Error constructors and PlayerMessage extractor

---

### Task 1.6: Command Dispatcher

**Files:**

- Create: `internal/command/dispatcher.go`
- Test: `internal/command/dispatcher_test.go`

**Step 1: Write the failing test**

```go
// internal/command/dispatcher_test.go
package command

import (
    "bytes"
    "context"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/access/accesstest"
)

func TestDispatcher_Dispatch(t *testing.T) {
    reg := NewRegistry()
    mockAccess := accesstest.NewMockAccessControl()

    // Register a test command
    var capturedArgs string
    reg.Register(CommandEntry{
        Name:         "echo",
        Capabilities: []string{"test.echo"},
        Handler: func(ctx context.Context, exec *CommandExecution) error {
            capturedArgs = exec.Args
            exec.Output.Write([]byte("echoed: " + exec.Args))
            return nil
        },
        Source: "test",
    })

    // Grant capability
    charID := ulid.Make()
    mockAccess.Grant(charID.String(), "test.echo")

    dispatcher := NewDispatcher(reg, mockAccess)

    var output bytes.Buffer
    exec := &CommandExecution{
        CharacterID: charID,
        Output:      &output,
    }

    err := dispatcher.Dispatch(context.Background(), "echo hello world", exec)
    require.NoError(t, err)
    assert.Equal(t, "hello world", capturedArgs)
    assert.Equal(t, "echoed: hello world", output.String())
}

func TestDispatcher_UnknownCommand(t *testing.T) {
    reg := NewRegistry()
    mockAccess := accesstest.NewMockAccessControl()
    dispatcher := NewDispatcher(reg, mockAccess)

    var output bytes.Buffer
    exec := &CommandExecution{Output: &output}

    err := dispatcher.Dispatch(context.Background(), "nonexistent", exec)
    require.Error(t, err)
    assert.Contains(t, PlayerMessage(err), "Unknown command")
}

func TestDispatcher_PermissionDenied(t *testing.T) {
    reg := NewRegistry()
    mockAccess := accesstest.NewMockAccessControl()

    reg.Register(CommandEntry{
        Name:         "admin",
        Capabilities: []string{"admin.manage"},
        Handler:      func(ctx context.Context, exec *CommandExecution) error { return nil },
        Source:       "core",
    })

    // Don't grant capability
    dispatcher := NewDispatcher(reg, mockAccess)

    var output bytes.Buffer
    exec := &CommandExecution{
        CharacterID: ulid.Make(),
        Output:      &output,
    }

    err := dispatcher.Dispatch(context.Background(), "admin", exec)
    require.Error(t, err)
    assert.Contains(t, PlayerMessage(err), "permission")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/command/... -run TestDispatcher -v`
Expected: FAIL with "undefined: NewDispatcher"

**Step 3: Write minimal implementation**

```go
// internal/command/dispatcher.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
    "context"
    "log/slog"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/trace"

    "github.com/holomush/holomush/internal/access"
)

var tracer = otel.Tracer("holomush/command")

// Dispatcher handles command parsing, capability checks, and execution.
type Dispatcher struct {
    registry *Registry
    access   access.AccessControl
}

// NewDispatcher creates a new command dispatcher.
func NewDispatcher(registry *Registry, ac access.AccessControl) *Dispatcher {
    return &Dispatcher{
        registry: registry,
        access:   ac,
    }
}

// Dispatch parses and executes a command.
func (d *Dispatcher) Dispatch(ctx context.Context, input string, exec *CommandExecution) error {
    // Parse input
    parsed, err := Parse(input)
    if err != nil {
        return err
    }

    // Start trace span
    ctx, span := tracer.Start(ctx, "command.execute",
        trace.WithAttributes(
            attribute.String("command.name", parsed.Name),
            attribute.String("character.id", exec.CharacterID.String()),
        ),
    )
    defer span.End()

    // Look up command
    entry, ok := d.registry.Get(parsed.Name)
    if !ok {
        return ErrUnknownCommand(parsed.Name)
    }

    span.SetAttributes(attribute.String("command.source", entry.Source))

    // Check capabilities
    for _, cap := range entry.Capabilities {
        allowed, err := d.access.Check(ctx, access.Request{
            Subject:    exec.CharacterID.String(),
            Permission: cap,
        })
        if err != nil {
            slog.Error("capability check failed",
                "command", parsed.Name,
                "capability", cap,
                "error", err)
            return ErrPermissionDenied(parsed.Name, cap)
        }
        if !allowed {
            return ErrPermissionDenied(parsed.Name, cap)
        }
    }

    // Execute
    exec.Args = parsed.Args
    return entry.Handler(ctx, exec)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/command/... -run TestDispatcher -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/command/dispatcher.go internal/command/dispatcher_test.go
git commit -m "feat(command): implement dispatcher with capability checks"
```

Expected: Dispatcher with parse, capability check, execute flow

---

## Phase 2: Alias System

### Task 2.1: Alias Database Migration

**Files:**

- Create: `internal/store/migrations/000006_aliases.up.sql`
- Create: `internal/store/migrations/000006_aliases.down.sql`

**Step 1: Write migration**

```sql
-- internal/store/migrations/000006_aliases.up.sql
-- System-wide aliases (admin-managed)
CREATE TABLE system_aliases (
    alias       TEXT PRIMARY KEY,
    command     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT REFERENCES players(id)
);

-- Player-specific aliases
CREATE TABLE player_aliases (
    player_id   TEXT REFERENCES players(id) ON DELETE CASCADE,
    alias       TEXT NOT NULL,
    command     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (player_id, alias)
);

-- Index for efficient player alias lookup
CREATE INDEX idx_player_aliases_player ON player_aliases(player_id);
```

```sql
-- internal/store/migrations/000006_aliases.down.sql
DROP INDEX IF EXISTS idx_player_aliases_player;
DROP TABLE IF EXISTS player_aliases;
DROP TABLE IF EXISTS system_aliases;
```

**Step 2: Verify migration syntax**

Run: `task lint:sql` (if available) or manual review
Expected: Valid SQL syntax

**Step 3: Commit**

```bash
git add internal/store/migrations/000006_aliases.*
git commit -m "feat(store): add alias tables migration"
```

Expected: Migration creates alias tables

---

### Task 2.2: Alias Repository Interface

**Files:**

- Create: `internal/command/alias.go`
- Test: `internal/command/alias_test.go`

**Step 1: Write the failing test**

```go
// internal/command/alias_test.go
package command

import (
    "context"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
)

func TestAliasCache_SystemAliases(t *testing.T) {
    cache := NewAliasCache()

    cache.SetSystemAlias("l", "look")
    cache.SetSystemAlias("n", "move north")

    cmd, ok := cache.ResolveSystemAlias("l")
    assert.True(t, ok)
    assert.Equal(t, "look", cmd)

    _, ok = cache.ResolveSystemAlias("nonexistent")
    assert.False(t, ok)
}

func TestAliasCache_PlayerAliases(t *testing.T) {
    cache := NewAliasCache()
    playerID := ulid.Make()

    cache.SetPlayerAlias(playerID, "ll", "look long")

    cmd, ok := cache.ResolvePlayerAlias(playerID, "ll")
    assert.True(t, ok)
    assert.Equal(t, "look long", cmd)

    // Different player shouldn't see the alias
    otherPlayer := ulid.Make()
    _, ok = cache.ResolvePlayerAlias(otherPlayer, "ll")
    assert.False(t, ok)
}

func TestAliasResolver_ResolutionOrder(t *testing.T) {
    cache := NewAliasCache()
    reg := NewRegistry()

    // Register a real command
    reg.Register(CommandEntry{Name: "look", Source: "core"})

    // Set up aliases
    playerID := ulid.Make()
    cache.SetSystemAlias("l", "look")
    cache.SetPlayerAlias(playerID, "l", "look at me") // Player overrides system

    resolver := NewAliasResolver(cache, reg)

    // Player alias takes precedence over system
    resolved := resolver.Resolve(context.Background(), "l", playerID)
    assert.Equal(t, "look at me", resolved)

    // Command name is not expanded
    resolved = resolver.Resolve(context.Background(), "look", playerID)
    assert.Equal(t, "look", resolved)

    // Unknown stays as-is
    resolved = resolver.Resolve(context.Background(), "unknown", playerID)
    assert.Equal(t, "unknown", resolved)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/command/... -run TestAlias -v`
Expected: FAIL with "undefined: NewAliasCache"

**Step 3: Write minimal implementation**

```go
// internal/command/alias.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
    "context"
    "sync"

    "github.com/oklog/ulid/v2"
)

// AliasRepository defines persistence operations for aliases.
type AliasRepository interface {
    // System aliases
    GetSystemAliases(ctx context.Context) (map[string]string, error)
    SetSystemAlias(ctx context.Context, alias, command, createdBy string) error
    DeleteSystemAlias(ctx context.Context, alias string) error

    // Player aliases
    GetPlayerAliases(ctx context.Context, playerID ulid.ULID) (map[string]string, error)
    SetPlayerAlias(ctx context.Context, playerID ulid.ULID, alias, command string) error
    DeletePlayerAlias(ctx context.Context, playerID ulid.ULID, alias string) error
}

// AliasCache provides in-memory alias resolution.
type AliasCache struct {
    systemAliases map[string]string
    playerAliases map[ulid.ULID]map[string]string
    mu            sync.RWMutex
}

// NewAliasCache creates a new alias cache.
func NewAliasCache() *AliasCache {
    return &AliasCache{
        systemAliases: make(map[string]string),
        playerAliases: make(map[ulid.ULID]map[string]string),
    }
}

// SetSystemAlias adds or updates a system alias.
func (c *AliasCache) SetSystemAlias(alias, command string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.systemAliases[alias] = command
}

// DeleteSystemAlias removes a system alias.
func (c *AliasCache) DeleteSystemAlias(alias string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    delete(c.systemAliases, alias)
}

// ResolveSystemAlias looks up a system alias.
func (c *AliasCache) ResolveSystemAlias(alias string) (string, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    cmd, ok := c.systemAliases[alias]
    return cmd, ok
}

// SetPlayerAlias adds or updates a player alias.
func (c *AliasCache) SetPlayerAlias(playerID ulid.ULID, alias, command string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.playerAliases[playerID] == nil {
        c.playerAliases[playerID] = make(map[string]string)
    }
    c.playerAliases[playerID][alias] = command
}

// DeletePlayerAlias removes a player alias.
func (c *AliasCache) DeletePlayerAlias(playerID ulid.ULID, alias string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if aliases, ok := c.playerAliases[playerID]; ok {
        delete(aliases, alias)
    }
}

// ResolvePlayerAlias looks up a player alias.
func (c *AliasCache) ResolvePlayerAlias(playerID ulid.ULID, alias string) (string, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if aliases, ok := c.playerAliases[playerID]; ok {
        cmd, found := aliases[alias]
        return cmd, found
    }
    return "", false
}

// ClearPlayer removes all aliases for a player (on session end).
func (c *AliasCache) ClearPlayer(playerID ulid.ULID) {
    c.mu.Lock()
    defer c.mu.Unlock()
    delete(c.playerAliases, playerID)
}

// LoadSystemAliases bulk loads system aliases.
func (c *AliasCache) LoadSystemAliases(aliases map[string]string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.systemAliases = aliases
}

// LoadPlayerAliases bulk loads player aliases.
func (c *AliasCache) LoadPlayerAliases(playerID ulid.ULID, aliases map[string]string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.playerAliases[playerID] = aliases
}

// AliasResolver handles alias expansion with proper precedence.
type AliasResolver struct {
    cache    *AliasCache
    registry *Registry
}

// NewAliasResolver creates a new alias resolver.
func NewAliasResolver(cache *AliasCache, registry *Registry) *AliasResolver {
    return &AliasResolver{
        cache:    cache,
        registry: registry,
    }
}

// MaxExpansionDepth prevents circular alias chains.
const MaxExpansionDepth = 10

// Resolve expands an alias following precedence: command > player alias > system alias.
func (r *AliasResolver) Resolve(ctx context.Context, input string, playerID ulid.ULID) string {
    return r.resolveWithDepth(input, playerID, 0)
}

func (r *AliasResolver) resolveWithDepth(input string, playerID ulid.ULID, depth int) string {
    if depth >= MaxExpansionDepth {
        return input // Prevent infinite loops
    }

    // Parse to get command name
    parsed, err := Parse(input)
    if err != nil {
        return input
    }

    // 1. Exact command match - don't expand
    if _, ok := r.registry.Get(parsed.Name); ok {
        return input
    }

    // 2. Player alias
    if cmd, ok := r.cache.ResolvePlayerAlias(playerID, parsed.Name); ok {
        expanded := cmd
        if parsed.Args != "" {
            expanded = cmd + " " + parsed.Args
        }
        return r.resolveWithDepth(expanded, playerID, depth+1)
    }

    // 3. System alias
    if cmd, ok := r.cache.ResolveSystemAlias(parsed.Name); ok {
        expanded := cmd
        if parsed.Args != "" {
            expanded = cmd + " " + parsed.Args
        }
        return r.resolveWithDepth(expanded, playerID, depth+1)
    }

    // 4. No alias found
    return input
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/command/... -run TestAlias -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/command/alias.go internal/command/alias_test.go
git commit -m "feat(command): add alias cache and resolver"
```

Expected: Alias system with cache and resolution

---

### Task 2.3: Alias PostgreSQL Repository

**Files:**

- Create: `internal/store/alias.go`
- Test: `internal/store/alias_test.go`

**Step 1: Write the failing test**

```go
// internal/store/alias_test.go
package store

import (
    "context"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestAliasRepository_SystemAliases(t *testing.T) {
    // This would be an integration test with testcontainers
    t.Skip("Requires database - run with integration tag")
}

func TestAliasRepository_PlayerAliases(t *testing.T) {
    t.Skip("Requires database - run with integration tag")
}
```

**Step 2: Write implementation**

```go
// internal/store/alias.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
    "context"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"
)

// AliasRepository implements command.AliasRepository with PostgreSQL.
type AliasRepository struct {
    pool *pgxpool.Pool
}

// NewAliasRepository creates a new alias repository.
func NewAliasRepository(pool *pgxpool.Pool) *AliasRepository {
    return &AliasRepository{pool: pool}
}

// GetSystemAliases retrieves all system aliases.
func (r *AliasRepository) GetSystemAliases(ctx context.Context) (map[string]string, error) {
    rows, err := r.pool.Query(ctx, `SELECT alias, command FROM system_aliases`)
    if err != nil {
        return nil, oops.In("alias").Code("DB_ERROR").Wrap(err)
    }
    defer rows.Close()

    aliases := make(map[string]string)
    for rows.Next() {
        var alias, command string
        if err := rows.Scan(&alias, &command); err != nil {
            return nil, oops.In("alias").Code("DB_ERROR").Wrap(err)
        }
        aliases[alias] = command
    }
    return aliases, rows.Err()
}

// SetSystemAlias creates or updates a system alias.
func (r *AliasRepository) SetSystemAlias(ctx context.Context, alias, command, createdBy string) error {
    _, err := r.pool.Exec(ctx, `
        INSERT INTO system_aliases (alias, command, created_by)
        VALUES ($1, $2, $3)
        ON CONFLICT (alias) DO UPDATE SET command = $2
    `, alias, command, createdBy)
    if err != nil {
        return oops.In("alias").Code("DB_ERROR").With("alias", alias).Wrap(err)
    }
    return nil
}

// DeleteSystemAlias removes a system alias.
func (r *AliasRepository) DeleteSystemAlias(ctx context.Context, alias string) error {
    _, err := r.pool.Exec(ctx, `DELETE FROM system_aliases WHERE alias = $1`, alias)
    if err != nil {
        return oops.In("alias").Code("DB_ERROR").With("alias", alias).Wrap(err)
    }
    return nil
}

// GetPlayerAliases retrieves all aliases for a player.
func (r *AliasRepository) GetPlayerAliases(ctx context.Context, playerID ulid.ULID) (map[string]string, error) {
    rows, err := r.pool.Query(ctx, `
        SELECT alias, command FROM player_aliases WHERE player_id = $1
    `, playerID.String())
    if err != nil {
        return nil, oops.In("alias").Code("DB_ERROR").With("player_id", playerID).Wrap(err)
    }
    defer rows.Close()

    aliases := make(map[string]string)
    for rows.Next() {
        var alias, command string
        if err := rows.Scan(&alias, &command); err != nil {
            return nil, oops.In("alias").Code("DB_ERROR").Wrap(err)
        }
        aliases[alias] = command
    }
    return aliases, rows.Err()
}

// SetPlayerAlias creates or updates a player alias.
func (r *AliasRepository) SetPlayerAlias(ctx context.Context, playerID ulid.ULID, alias, command string) error {
    _, err := r.pool.Exec(ctx, `
        INSERT INTO player_aliases (player_id, alias, command)
        VALUES ($1, $2, $3)
        ON CONFLICT (player_id, alias) DO UPDATE SET command = $3
    `, playerID.String(), alias, command)
    if err != nil {
        return oops.In("alias").Code("DB_ERROR").With("player_id", playerID).With("alias", alias).Wrap(err)
    }
    return nil
}

// DeletePlayerAlias removes a player alias.
func (r *AliasRepository) DeletePlayerAlias(ctx context.Context, playerID ulid.ULID, alias string) error {
    _, err := r.pool.Exec(ctx, `
        DELETE FROM player_aliases WHERE player_id = $1 AND alias = $2
    `, playerID.String(), alias)
    if err != nil {
        return oops.In("alias").Code("DB_ERROR").With("player_id", playerID).With("alias", alias).Wrap(err)
    }
    return nil
}

// Ensure AliasRepository implements the interface (compile-time check would go in command package).
var _ interface {
    GetSystemAliases(ctx context.Context) (map[string]string, error)
} = (*AliasRepository)(nil)
```

**Step 3: Commit**

```bash
git add internal/store/alias.go internal/store/alias_test.go
git commit -m "feat(store): add alias PostgreSQL repository"
```

Expected: PostgreSQL implementation for alias storage

---

### Task 2.4: Integrate Alias Resolution into Dispatcher

**Files:**

- Modify: `internal/command/dispatcher.go`
- Modify: `internal/command/dispatcher_test.go`

**Step 1: Write the failing test**

```go
// Add to dispatcher_test.go
func TestDispatcher_AliasResolution(t *testing.T) {
    reg := NewRegistry()
    mockAccess := accesstest.NewMockAccessControl()
    cache := NewAliasCache()

    // Register look command
    reg.Register(CommandEntry{
        Name:         "look",
        Capabilities: []string{"world.look"},
        Handler: func(ctx context.Context, exec *CommandExecution) error {
            exec.Output.Write([]byte("You look around."))
            return nil
        },
        Source: "core",
    })

    // Set up alias
    cache.SetSystemAlias("l", "look")

    charID := ulid.Make()
    playerID := ulid.Make()
    mockAccess.Grant(charID.String(), "world.look")

    dispatcher := NewDispatcher(reg, mockAccess)
    dispatcher.SetAliasResolver(NewAliasResolver(cache, reg))

    var output bytes.Buffer
    exec := &CommandExecution{
        CharacterID: charID,
        PlayerID:    playerID,
        Output:      &output,
    }

    // Use alias
    err := dispatcher.Dispatch(context.Background(), "l", exec)
    require.NoError(t, err)
    assert.Equal(t, "You look around.", output.String())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/command/... -run TestDispatcher_Alias -v`
Expected: FAIL with "dispatcher.SetAliasResolver undefined"

**Step 3: Update implementation**

```go
// Update dispatcher.go - add alias resolver field and method

type Dispatcher struct {
    registry      *Registry
    access        access.AccessControl
    aliasResolver *AliasResolver
}

// SetAliasResolver sets the alias resolver for command expansion.
func (d *Dispatcher) SetAliasResolver(resolver *AliasResolver) {
    d.aliasResolver = resolver
}

// Update Dispatch to use alias resolution
func (d *Dispatcher) Dispatch(ctx context.Context, input string, exec *CommandExecution) error {
    // Resolve aliases if resolver is set
    resolvedInput := input
    if d.aliasResolver != nil {
        resolvedInput = d.aliasResolver.Resolve(ctx, input, exec.PlayerID)
    }

    // Parse resolved input
    parsed, err := Parse(resolvedInput)
    // ... rest of dispatch logic
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/command/... -run TestDispatcher -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/command/dispatcher.go internal/command/dispatcher_test.go
git commit -m "feat(command): integrate alias resolution into dispatcher"
```

Expected: Dispatcher resolves aliases before dispatch

---

## Phase 3: Manifest Command Extension

### Task 3.1: Extend Plugin Manifest for Commands

**Files:**

- Modify: `internal/plugin/manifest.go`
- Modify: `internal/plugin/manifest_test.go`

**Step 1: Write the failing test**

```go
// Add to manifest_test.go
func TestManifest_Commands(t *testing.T) {
    yaml := `
name: communication
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
commands:
  - name: say
    capabilities:
      - comms.say
    help: "Send a message to the room"
    usage: "say <message>"
    helpText: |
      ## Say
      Speaks a message aloud.
`
    m, err := ParseManifest([]byte(yaml))
    require.NoError(t, err)
    require.Len(t, m.Commands, 1)

    cmd := m.Commands[0]
    assert.Equal(t, "say", cmd.Name)
    assert.Equal(t, []string{"comms.say"}, cmd.Capabilities)
    assert.Equal(t, "Send a message to the room", cmd.Help)
    assert.Equal(t, "say <message>", cmd.Usage)
    assert.Contains(t, cmd.HelpText, "Speaks a message")
}

func TestManifest_CommandValidation(t *testing.T) {
    // Both helpText and helpFile is invalid
    yaml := `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
commands:
  - name: say
    help: "test"
    usage: "say"
    helpText: "inline help"
    helpFile: "help/say.md"
`
    _, err := ParseManifest([]byte(yaml))
    require.Error(t, err)
    assert.Contains(t, err.Error(), "both helpText and helpFile")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/plugin/... -run TestManifest_Command -v`
Expected: FAIL (Commands field not defined)

**Step 3: Update manifest.go**

```go
// Add to manifest.go

// CommandSpec defines a command declared in a plugin manifest.
type CommandSpec struct {
    Name         string   `yaml:"name" json:"name" jsonschema:"required,minLength=1,maxLength=20"`
    Capabilities []string `yaml:"capabilities" json:"capabilities"`
    Help         string   `yaml:"help" json:"help" jsonschema:"required"`
    Usage        string   `yaml:"usage" json:"usage" jsonschema:"required"`
    HelpText     string   `yaml:"helpText,omitempty" json:"helpText,omitempty"`
    HelpFile     string   `yaml:"helpFile,omitempty" json:"helpFile,omitempty"`
}

// Update Manifest struct to include Commands
type Manifest struct {
    // ... existing fields ...
    Commands []CommandSpec `yaml:"commands,omitempty" json:"commands,omitempty"`
}

// Add validation in Validate()
func (m *Manifest) Validate() error {
    // ... existing validation ...

    // Validate commands
    for i, cmd := range m.Commands {
        if cmd.Name == "" {
            return oops.In("manifest").With("index", i).New("command name is required")
        }
        if cmd.HelpText != "" && cmd.HelpFile != "" {
            return oops.In("manifest").With("command", cmd.Name).New("cannot specify both helpText and helpFile")
        }
    }
    return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/plugin/... -run TestManifest_Command -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/plugin/manifest.go internal/plugin/manifest_test.go
git commit -m "feat(plugin): add commands to manifest schema"
```

Expected: Manifest supports command declarations

---

## Phase 4: Core Go Commands

### Task 4.1: Look Command Handler

**Files:**

- Create: `internal/command/handlers/look.go`
- Test: `internal/command/handlers/look_test.go`

**Step 1: Write the failing test**

```go
// internal/command/handlers/look_test.go
package handlers

import (
    "bytes"
    "context"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/command"
    "github.com/holomush/holomush/internal/world"
    worldmocks "github.com/holomush/holomush/internal/world/mocks"
)

func TestLookHandler_CurrentRoom(t *testing.T) {
    mockWorld := worldmocks.NewMockService(t)
    locationID := ulid.Make()

    mockWorld.EXPECT().GetLocation(mock.Anything, locationID).Return(&world.Location{
        ID:          locationID,
        Name:        "The Library",
        Description: "A dusty room filled with ancient tomes.",
    }, nil)

    var output bytes.Buffer
    exec := &command.CommandExecution{
        CharacterID: ulid.Make(),
        LocationID:  locationID,
        Output:      &output,
        Services: &command.Services{
            World: mockWorld,
        },
    }

    err := Look(context.Background(), exec)
    require.NoError(t, err)
    assert.Contains(t, output.String(), "The Library")
    assert.Contains(t, output.String(), "dusty room")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/command/handlers/... -run TestLookHandler -v`
Expected: FAIL with "undefined: Look"

**Step 3: Write implementation**

```go
// internal/command/handlers/look.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package handlers implements core Go command handlers.
package handlers

import (
    "context"
    "fmt"

    "github.com/holomush/holomush/internal/command"
)

// Look implements the look command - examine current location or target.
func Look(ctx context.Context, exec *command.CommandExecution) error {
    // TODO: Parse args for target
    // For now, just look at current room

    room, err := exec.Services.World.GetLocation(ctx, exec.LocationID)
    if err != nil {
        return command.WorldError("You can't see anything here.", err)
    }

    fmt.Fprintf(exec.Output, "%s\n%s\n", room.Name, room.Description)
    return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/command/handlers/... -run TestLookHandler -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/command/handlers/
git commit -m "feat(command): implement look command handler"
```

Expected: Look command shows room name and description

---

### Task 4.2: Quit Command Handler

**Files:**

- Create: `internal/command/handlers/quit.go`
- Test: `internal/command/handlers/quit_test.go`

**Step 1: Write the failing test**

```go
// internal/command/handlers/quit_test.go
package handlers

import (
    "bytes"
    "context"
    "testing"

    "github.com/oklog/ulid/v2"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/command"
    "github.com/holomush/holomush/internal/core/mocks"
)

func TestQuitHandler(t *testing.T) {
    mockSession := mocks.NewMockSessionManager(t)
    sessionID := ulid.Make()

    mockSession.EXPECT().EndSession(mock.Anything, sessionID).Return(nil)

    var output bytes.Buffer
    exec := &command.CommandExecution{
        SessionID: sessionID,
        Output:    &output,
        Services: &command.Services{
            Session: mockSession,
        },
    }

    err := Quit(context.Background(), exec)
    require.NoError(t, err)
    assert.Contains(t, output.String(), "Goodbye")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/command/handlers/... -run TestQuitHandler -v`
Expected: FAIL with "undefined: Quit"

**Step 3: Write implementation**

```go
// internal/command/handlers/quit.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
    "context"
    "fmt"

    "github.com/holomush/holomush/internal/command"
)

// Quit implements the quit command - ends the player's session.
func Quit(ctx context.Context, exec *command.CommandExecution) error {
    fmt.Fprintln(exec.Output, "Goodbye! See you next time.")

    if err := exec.Services.Session.EndSession(ctx, exec.SessionID); err != nil {
        return command.WorldError("Failed to end session cleanly.", err)
    }

    return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/command/handlers/... -run TestQuitHandler -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/command/handlers/quit.go internal/command/handlers/quit_test.go
git commit -m "feat(command): implement quit command handler"
```

Expected: Quit command ends session gracefully

---

### Task 4.3: Who Command Handler

**Files:**

- Create: `internal/command/handlers/who.go`
- Test: `internal/command/handlers/who_test.go`

Implementation follows same TDD pattern - lists connected players.

---

### Task 4.4: Move Command Handler

**Files:**

- Create: `internal/command/handlers/move.go`
- Test: `internal/command/handlers/move_test.go`

Implementation follows same TDD pattern - moves through exits.

---

## Phase 5: Lua Plugin Commands

### Task 5.1: Add Command Host Functions

**Files:**

- Modify: `api/proto/plugin/v1/hostfunc.proto`
- Modify: `internal/plugin/hostfunc/functions.go`

Add `ListCommands` and `GetCommandHelp` RPCs for the help plugin.

---

### Task 5.2: Communication Plugin (say, pose, emit)

**Files:**

- Create: `plugins/communication/plugin.yaml`
- Create: `plugins/communication/main.lua`

Implements say, pose, emit as Lua commands to prove the plugin model.

---

## Phase 6: Help System

### Task 6.1: Help Plugin

**Files:**

- Create: `plugins/help/plugin.yaml`
- Create: `plugins/help/main.lua`

Implements help command in Lua, querying registry via host functions.

---

## Post-Implementation Checklist

- [ ] All tasks have >90% test coverage
- [ ] `task lint` passes
- [ ] `task test` passes
- [ ] Integration tests added for command dispatch
- [ ] ADRs document key decisions
- [ ] CLAUDE.md updated if patterns changed
- [ ] Beads closed for completed phases

---

## Phase Dependencies

```text
Phase 0 (ADRs) ─────────────────────────────────┐
                                                 │
Phase 1 (Registry/Parser) ──────────────────────┼─→ blocks wr9.3
                                                 │
Phase 2 (Alias System) ─────────────────────────┼─→ blocks wr9.4
                                                 │
Phase 3 (Manifest Extension) ───────────────────┤
                                                 │
Phase 4 (Go Commands) ──────────────────────────┼─→ blocks wr9.5, wr9.8
                                                 │
Phase 5 (Lua Commands) ─────────────────────────┼─→ blocks wr9.6, wr9.7
                                                 │
Phase 6 (Help System) ──────────────────────────┴─→ blocks wr9.9
```
