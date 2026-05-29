<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Recognized-Command Chip Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a recognized-command chip in the web composer (displaying the resolved canonical command name) for any command the current character can run, sourced from one core-owned command-query service that also reaches both plugin runtimes.

**Architecture:** Extract the ABAC-filtered command enumeration into a new core package `internal/command/commandquery`. Three thin adapters delegate to it — the Lua hostfunc (refactored to a shim, fixing a production registry-wiring bug), a new `PluginHostService.ListCommands`/`GetCommandHelp` pair for binary plugins, and a new `CoreService.ListAvailableCommands` proxied by `WebService.WebListCommands` for the web composer. The composer fetches the list once per session, caches it, and a single resolution helper replaces the hardcoded prefix matcher.

**Tech Stack:** Go (core, plugin host, gRPC), Protocol Buffers (buf), hashicorp/go-plugin, SvelteKit 5 + ConnectRPC + vitest.

**Design spec:** `docs/superpowers/specs/2026-05-29-recognized-command-chip-design.md` (holomush-2zjio).

---

## File Structure

| File | Responsibility | Action |
| --- | --- | --- |
| `internal/command/commandquery/query.go` | Core ABAC-filtered command enumeration + per-command help + alias map. One filter, shared by all adapters. | Create |
| `internal/command/commandquery/query_test.go` | Unit tests for the querier. | Create |
| `internal/plugin/hostfunc/commands.go` | Lua `list_commands`/`get_command_help` shims delegating to `commandquery`. | Modify |
| `internal/plugin/hostfunc/functions.go` | Add `WithCommandQuerier` option + `commandQuerier` field. | Modify |
| `internal/plugin/setup/subsystem.go` | Construct `commandquery.Querier` after the registry exists; inject into Lua host (fixes nil-registry bug) and expose for the binary host + CoreServer. | Modify |
| `api/proto/holomush/plugin/v1/plugin.proto` | `ListCommands`/`GetCommandHelp` RPCs + `CommandInfo`/messages on `PluginHostService`. | Modify |
| `internal/plugin/goplugin/host.go` | Add `commandQuerier` field to `Host`; wire it. | Modify |
| `internal/plugin/goplugin/host_service.go` | `ListCommands`/`GetCommandHelp` handlers. | Modify |
| `pkg/plugin/command_lister.go` | SDK facade `CommandLister` + `CommandListerAware` + client. | Create |
| `pkg/plugin/sdk.go` | Inject `CommandLister` in `Init`. | Modify |
| `api/proto/holomush/core/v1/core.proto` | `CoreService.ListAvailableCommands` + messages. | Modify |
| `api/proto/holomush/web/v1/web.proto` | `WebService.WebListCommands` + messages. | Modify |
| `internal/grpc/list_available_commands.go` | CoreServer handler (session-scoped, self-only). | Create |
| `internal/grpc/client.go` | In-process `ListAvailableCommands` client method. | Modify |
| `internal/web/handler.go` | `WebListCommands` gateway proxy + `CoreClient` interface method. | Modify |
| `web/src/lib/stores/commandListStore.ts` | Fetch + cache `{names, aliases, incomplete}` per session. | Create |
| `web/src/lib/components/terminal/composerChip.ts` | Resolution helper: text → chip kind + label. | Create |
| `web/src/lib/components/terminal/ModeChip.svelte` | Add `command` kind + render canonical name. | Modify |
| `web/src/lib/components/terminal/CommandInput.svelte` | Fetch list on session focus; use the helper; render the chip. | Modify |

---

## Phase 1 — Core command-query service + wiring fix + Lua shims

### Task 1: Core `commandquery` package

**Files:**

- Create: `internal/command/commandquery/query.go`
- Test: `internal/command/commandquery/query_test.go`

This package owns the single ABAC-filter (ported verbatim from `internal/plugin/hostfunc/commands.go:42-216`, including the 3-error circuit breaker) plus an alias map. It is a subpackage of `internal/command` so it may import both `internal/command` and `internal/access/...` with no import cycle (nothing low-level imports `commandquery`).

- [ ] **Step 1: Write the failing test**

```go
// internal/command/commandquery/query_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package commandquery_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/access/policy/policytest"
)

func newRegistry(t *testing.T) *command.Registry {
	t.Helper()
	reg := command.NewRegistry()
	// NewTestEntry skips the Handler/PluginName validation NewCommandEntry enforces
	// (types.go:670) — the querier only reads Name/Help/Usage/Source/capabilities.
	// no-capability command — always visible
	look := command.NewTestEntry(command.CommandEntryConfig{
		Name: "look", Help: "look around", Usage: "look", PluginName: "core", Source: "core",
	})
	require.NoError(t, reg.Register(look))
	// capability-gated command
	scene := command.NewTestEntry(command.CommandEntryConfig{
		Name: "scene", Help: "scene control", Usage: "scene <subcommand>", PluginName: "core-scenes", Source: "core-scenes",
		Capabilities: []command.Capability{{Action: "write", Resource: "scene", Scope: command.ScopeLocal}},
	})
	require.NoError(t, reg.Register(scene))
	return reg
}

func TestQuerierAvailableReturnsAllowedCommandsForGrantedSubject(t *testing.T) {
	reg := newRegistry(t)
	aliases := command.NewAliasCache()
	aliases.SetSystemAlias("l", "look")
	aliases.SetSystemAlias(`"`, "say") // say not registered here → must be filtered out of alias map

	q := commandquery.New(reg, policytest.AllowAllEngine(), aliases)
	res, err := q.Available(context.Background(), access.CharacterSubject("01HCHAR0000000000000000AAA"))
	require.NoError(t, err)
	assert.False(t, res.Incomplete)

	names := map[string]bool{}
	for _, c := range res.Commands {
		names[c.Name] = true
	}
	assert.True(t, names["look"])
	assert.True(t, names["scene"])
	// alias map carries only aliases whose target is in the visible set
	assert.Equal(t, "look", res.Aliases["l"])
	_, hasSay := res.Aliases[`"`]
	assert.False(t, hasSay, `alias for unregistered "say" must be omitted`)
}

func TestQuerierAvailableOmitsDeniedCapabilityCommands(t *testing.T) {
	reg := newRegistry(t)
	q := commandquery.New(reg, policytest.DenyAllEngine(), command.NewAliasCache())
	res, err := q.Available(context.Background(), access.CharacterSubject("01HCHAR0000000000000000BBB"))
	require.NoError(t, err)
	names := map[string]bool{}
	for _, c := range res.Commands {
		names[c.Name] = true
	}
	assert.True(t, names["look"], "no-capability command always visible")
	assert.False(t, names["scene"], "capability-gated command denied")
}

func TestQuerierAvailableMarksIncompleteOnEngineErrors(t *testing.T) {
	reg := newRegistry(t)
	q := commandquery.New(reg, policytest.NewErrorEngine(assert.AnError), command.NewAliasCache())
	res, err := q.Available(context.Background(), access.CharacterSubject("01HCHAR0000000000000000CCC"))
	require.NoError(t, err)
	assert.True(t, res.Incomplete, "engine errors must set Incomplete")
	// no-capability command still present despite circuit breaker
	found := false
	for _, c := range res.Commands {
		if c.Name == "look" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestQuerierHelpReturnsDetailForGranted(t *testing.T) {
	reg := newRegistry(t)
	q := commandquery.New(reg, policytest.AllowAllEngine(), command.NewAliasCache())
	d, err := q.Help(context.Background(), access.CharacterSubject("01HCHAR0000000000000000DDD"), "scene")
	require.NoError(t, err)
	assert.Equal(t, "scene", d.Name)
	assert.Len(t, d.Capabilities, 1)
}

func TestQuerierHelpDeniesUngranted(t *testing.T) {
	reg := newRegistry(t)
	q := commandquery.New(reg, policytest.DenyAllEngine(), command.NewAliasCache())
	_, err := q.Help(context.Background(), access.CharacterSubject("01HCHAR0000000000000000EEE"), "scene")
	require.Error(t, err)
}
```

> NOTE: this test calls `aliases.SetSystemAlias(...)`. `AliasCache` today has no exported single-alias setter (it has `ListSystemAliases`). Add a tiny setter in this task (Step 3a) or seed via the existing seeding path. The minimal setter is shown in Step 3a.

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- ./internal/command/commandquery/`
Expected: FAIL — package `commandquery` does not exist.

- [ ] **Step 3a: Add the `SetSystemAlias` test seam to `AliasCache`**

In `internal/command/alias.go`, after `ListSystemAliases` (around `alias.go:290`):

```go
// SetSystemAlias registers or overwrites a single system alias (alias → command).
// Used by command-query construction and tests; production seeding still flows
// through the manifest alias seeder.
func (c *AliasCache) SetSystemAlias(alias, cmd string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.systemAliases[alias] = cmd
}
```

- [ ] **Step 3b: Write the package**

```go
// internal/command/commandquery/query.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package commandquery owns the single ABAC-filtered enumeration of registered
// commands for a subject. The Lua hostfunc bridge, the binary PluginHostService
// handler, and the CoreService RPC all delegate here — there is exactly one
// command-visibility filter (design spec INV-1).
package commandquery

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/pkg/errutil"
)

// maxEngineErrors trips the circuit breaker after repeated engine failures so a
// degraded engine does not incur O(commands*capabilities) calls. Ported from the
// former hostfunc implementation.
const maxEngineErrors = 3

// Registry is the read-only registry view the querier needs.
type Registry interface {
	All() []command.CommandEntry
	Get(name string) (command.CommandEntry, bool)
}

// AliasLister exposes the system/manifest alias map (tiers 1+2).
type AliasLister interface {
	ListSystemAliases() map[string]string
}

// Summary is the per-command metadata used by enumeration.
type Summary struct {
	Name   string
	Help   string
	Usage  string
	Source string
}

// Detail is the full per-command help payload.
type Detail struct {
	Name         string
	Help         string
	Usage        string
	HelpText     string
	Source       string
	Capabilities []command.Capability
}

// Result is the ABAC-filtered enumeration plus the alias map for visible commands.
type Result struct {
	Commands   []Summary
	Aliases    map[string]string // alias → canonical command name, restricted to visible commands
	Incomplete bool              // true when engine errors hid some commands
}

// Querier is the single command-visibility filter.
type Querier struct {
	registry Registry
	engine   types.AccessPolicyEngine
	aliases  AliasLister
}

// New constructs a Querier. All three dependencies are required for full results;
// a nil engine yields Incomplete results limited to no-capability commands.
func New(registry Registry, engine types.AccessPolicyEngine, aliases AliasLister) *Querier {
	return &Querier{registry: registry, engine: engine, aliases: aliases}
}

// Available returns the commands the subject may execute, the alias map for those
// commands, and an Incomplete flag. subject MUST be a formatted subject string
// (e.g. access.CharacterSubject(id)).
func (q *Querier) Available(ctx context.Context, subject string) (Result, error) {
	all := q.registry.All()
	visible := make(map[string]struct{}, len(all))
	out := make([]Summary, 0, len(all))

	var hadEngineError bool
	var engineErrorCount int
	circuitTripped := false

	for i := range all {
		if len(all[i].GetCapabilities()) == 0 {
			out = append(out, summaryOf(all[i]))
			visible[all[i].Name] = struct{}{}
			continue
		}
		if q.engine == nil {
			hadEngineError = true
			continue
		}
		if circuitTripped {
			continue
		}
		allowed, hadError := q.canExecute(ctx, subject, all[i])
		if hadError {
			hadEngineError = true
			engineErrorCount++
			if engineErrorCount >= maxEngineErrors {
				circuitTripped = true
			}
		}
		if allowed {
			out = append(out, summaryOf(all[i]))
			visible[all[i].Name] = struct{}{}
		}
	}

	aliasMap := map[string]string{}
	if q.aliases != nil {
		for alias, cmd := range q.aliases.ListSystemAliases() {
			if _, ok := visible[cmd]; ok {
				aliasMap[alias] = cmd
			}
		}
	}

	return Result{Commands: out, Aliases: aliasMap, Incomplete: hadEngineError}, nil
}

// Help returns the full help detail for one command after an access check.
func (q *Querier) Help(ctx context.Context, subject, name string) (Detail, error) {
	cmd, found := q.registry.Get(name)
	if !found {
		return Detail{}, oops.Code("NOT_FOUND").With("command", name).Errorf("command not found")
	}
	if len(cmd.GetCapabilities()) > 0 {
		if q.engine == nil {
			return Detail{}, oops.Code("UNAVAILABLE").Errorf("access engine not available")
		}
		allowed, hadError := q.canExecute(ctx, subject, cmd)
		if hadError {
			return Detail{}, oops.Code("UNAVAILABLE").With("command", name).Errorf("access check failed")
		}
		if !allowed {
			return Detail{}, oops.Code("PERMISSION_DENIED").With("command", name).Errorf("access denied")
		}
	}
	return Detail{
		Name: cmd.Name, Help: cmd.Help, Usage: cmd.Usage,
		HelpText: cmd.HelpText, Source: cmd.Source,
		Capabilities: cmd.GetCapabilities(),
	}, nil
}

func summaryOf(e command.CommandEntry) Summary {
	return Summary{Name: e.Name, Help: e.Help, Usage: e.Usage, Source: e.Source}
}

// canExecute ports the two-layer ABAC check from the former hostfunc impl
// (internal/plugin/hostfunc/commands.go:175-216).
func (q *Querier) canExecute(ctx context.Context, subject string, cmd command.CommandEntry) (allowed, hadError bool) {
	req, reqErr := types.NewAccessRequest(subject, "execute", "command:"+cmd.Name, nil)
	if reqErr != nil {
		errutil.LogErrorContext(ctx, "command access request failed", reqErr, "subject", subject, "command", cmd.Name)
		observability.RecordEngineFailure("command_capability_engine_error")
		return false, true
	}
	decision, evalErr := q.engine.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, "command access evaluation failed", evalErr, "subject", subject, "command", cmd.Name)
		observability.RecordEngineFailure("command_capability_engine_error")
		return false, true
	}
	if !decision.IsAllowed() {
		if decision.IsInfraFailure() {
			return false, true
		}
		return false, false
	}
	for _, capability := range cmd.GetCapabilities() {
		ok, err := q.engine.CanPerformAction(ctx, subject, capability.Action, capability.Resource, capability.EffectiveScope())
		if err != nil {
			errutil.LogErrorContext(ctx, "capability pre-flight failed", err, "subject", subject, "action", capability.Action, "resource", capability.Resource)
			observability.RecordEngineFailure("command_capability_engine_error")
			return false, true
		}
		if !ok {
			return false, hadError
		}
	}
	return true, hadError
}
```

> Error idiom (verified): `errutil` lives at `github.com/holomush/holomush/pkg/errutil` (logging helpers only — no coded-error constructors). Coded errors use `oops.Code("NOT_FOUND"/"UNAVAILABLE"/"PERMISSION_DENIED").Errorf(...)`, matching `internal/grpc/list_focus_presence.go:103`. The hostfunc returned plain Lua strings; the core package returns coded `oops` errors so the gRPC layer maps them and the Lua shim (Task 2) translates them back to the legacy strings.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `task test -- ./internal/command/commandquery/`
Expected: PASS (all 5 tests + `SetSystemAlias`).

- [ ] **Step 5: Lint + commit**

Run: `task lint:go`
Commit: `feat(command): core command-query service with shared ABAC filter (holomush-2zjio)`

---

### Task 2: Lua hostfunc shims + production wiring fix

**Files:**

- Modify: `internal/plugin/hostfunc/functions.go` (add `commandQuerier` field + `WithCommandQuerier`)
- Modify: `internal/plugin/hostfunc/commands.go` (delegate to the querier)
- Modify: `internal/plugin/setup/subsystem.go` (construct querier after registry; inject)
- Test: `internal/plugin/setup/subsystem_wiring_test.go` (Create — regression for the nil-registry bug)

This fixes the confirmed production bug: `subsystem.go:193` builds `hostfunc.New(...)` before `s.cmdRegistry` exists (line 384), so the Lua `list_commands` registry is nil in prod. We inject the fully-built `commandquery.Querier` instead.

- [ ] **Step 1: Write the failing regression test**

```go
// internal/plugin/setup/subsystem_wiring_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package setup_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	// ... existing setup-test imports (mirror an existing subsystem_test.go in this package)
)

// TestPluginSubsystemWiresCommandQuerierIntoLuaHost asserts the production wiring
// path provides a non-nil command querier to the Lua host — guarding the
// nil-registry regression (holomush-2zjio plan grounding).
func TestPluginSubsystemWiresCommandQuerierIntoLuaHost(t *testing.T) {
	sub := startTestSubsystem(t) // mirror existing helper in this package's *_test.go
	defer sub.Stop(context.Background())

	// list_commands for any character must NOT return "command registry not available".
	out, errStr := sub.InvokeLuaListCommands(t, "01HCHAR0000000000000000WIR")
	require.Empty(t, errStr, "registry must be wired in production path")
	require.NotNil(t, out)
}
```

> If no `startTestSubsystem`/`InvokeLuaListCommands` helper exists, the equivalent assertion is to load the `help` plugin via `integrationtest.Start(t, integrationtest.WithInTreePlugins())` and run the `help` command, asserting the output lists `look`/`help` rather than the "command registry not available" string. Use whichever harness the package already provides — confirm with `mcp__probe__search_code "PluginSubsystem test"`.

- [ ] **Step 2: Run it to verify it fails**

Run: `task test:int -- ./internal/plugin/setup/`
Expected: FAIL — `errStr` is "command registry not available" (the current prod bug).

- [ ] **Step 3a: Add the querier field + option to `Functions`**

In `internal/plugin/hostfunc/functions.go`, add to the `Functions` struct (after `engine`):

```go
	commandQuerier *commandquery.Querier
```

Add the option near `WithEngine` in `commands.go`:

```go
// WithCommandQuerier injects the shared command-query service. When set, the
// list_commands / get_command_help host functions delegate to it.
func WithCommandQuerier(q *commandquery.Querier) Option {
	return func(f *Functions) {
		f.commandQuerier = q
	}
}
```

- [ ] **Step 3b: Refactor `listCommandsFn` / `getCommandHelpFn` to delegate**

Replace the body of `listCommandsFn` (`commands.go:42-173`) so it builds the subject and calls the querier, preserving the exact Lua-table shape (`{commands:[{name,help,usage,source}], incomplete}` + error string). The `aliases` field is intentionally NOT added to the Lua table (design INV-2: parity is reachability; alias map is a web payload).

```go
func (f *Functions) listCommandsFn(_ string) lua.LGFunction {
	return func(L *lua.LState) int {
		charIDStr := L.CheckString(1)
		if charIDStr == "" {
			L.RaiseError("character ID cannot be empty")
			return 0
		}
		charID, err := ulid.Parse(charIDStr)
		if err != nil {
			L.RaiseError("invalid character ID: %s", charIDStr)
			return 0
		}
		if f.commandQuerier == nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("command registry not available"))
			return 2
		}
		ctx := L.Context()
		if ctx == nil {
			slog.Warn("lua VM context is nil in list_commands, using background context")
			ctx = context.Background()
		}
		res, qErr := f.commandQuerier.Available(ctx, access.CharacterSubject(charID.String()))
		if qErr != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("command listing failed"))
			return 2
		}
		commandsTbl := L.NewTable()
		for i := range res.Commands {
			cmdTbl := L.NewTable()
			L.SetField(cmdTbl, "name", lua.LString(res.Commands[i].Name))
			L.SetField(cmdTbl, "help", lua.LString(res.Commands[i].Help))
			L.SetField(cmdTbl, "usage", lua.LString(res.Commands[i].Usage))
			L.SetField(cmdTbl, "source", lua.LString(res.Commands[i].Source))
			L.SetTable(commandsTbl, lua.LNumber(i+1), cmdTbl)
		}
		resultTbl := L.NewTable()
		L.SetField(resultTbl, "commands", commandsTbl)
		L.SetField(resultTbl, "incomplete", lua.LBool(res.Incomplete))
		L.Push(resultTbl)
		if res.Incomplete {
			L.Push(lua.LString("some commands may be hidden due to a system error; try again or contact an admin if the problem persists"))
		} else {
			L.Push(lua.LNil)
		}
		return 2
	}
}
```

Refactor `getCommandHelpFn` (`commands.go:218-308`) analogously: call `f.commandQuerier.Help(ctx, subject, name)`; map `Detail` to the existing Lua table (`name/help/usage/help_text/source/capabilities`); translate the typed errors (not-found / denied / unavailable) to the existing Lua error strings. Delete the now-unused `canExecuteCommand` from `commands.go` (it lives in `commandquery`); keep `WithCommandRegistry`/`WithEngine` only if other host functions still need them — otherwise remove and update `commands_test.go` to construct via `WithCommandQuerier`.

- [ ] **Step 3c: Wire the querier in `subsystem.go`**

After `s.cmdRegistry` is populated (`subsystem.go:391`, post `RegisterPluginCommands`) and the alias cache is built, construct the querier and inject it into the Lua host. Because `hostFuncs` is built at line 193 (before the registry), add a setter on the Lua host or rebuild the relevant option. The minimal change: store the querier on `PluginSubsystem` and pass it through a `Functions` setter.

Add to `Functions` (`functions.go`) a setter mirroring the late-bound KV pattern:

```go
// SetCommandQuerier late-binds the command querier after the registry is built.
func (f *Functions) SetCommandQuerier(q *commandquery.Querier) { f.commandQuerier = q }
```

In `subsystem.go`, after line 391:

```go
	s.commandQuerier = commandquery.New(s.cmdRegistry, s.cfg.ABAC.Engine(), s.aliasCache)
	hostFuncs.SetCommandQuerier(s.commandQuerier)
```

Add `commandQuerier *commandquery.Querier` to the `PluginSubsystem` struct and a `CommandQuerier()` accessor (mirroring `CommandRegistry()` at `subsystem.go:430`) — the binary host and CoreServer wiring (Tasks 4, 8) consume it.

> Confirm `hostFuncs` is reachable at line 391 (it's the local from line 193). Confirm `s.aliasCache` is populated by then (`AliasCache()` panics if nil — alias seeding happens during plugin load, before/at command registration; verify ordering with `mcp__probe__extract_code subsystem.go` and move the construction after whichever of {registry, aliasCache} is last).

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- ./internal/plugin/hostfunc/` then `task test:int -- ./internal/plugin/setup/`
Expected: PASS — hostfunc unit tests (updated to `WithCommandQuerier`) and the new wiring regression both green. Also run the holomush-mexs guard: `task test:int -- ./internal/plugin/` (help integration).

- [ ] **Step 5: Lint + commit**

Run: `task lint:go`
Commit: `fix(plugin): wire command querier into Lua host; delegate list_commands/get_command_help (holomush-2zjio)`

---

## Phase 2 — Binary-plugin parity (`PluginHostService`)

### Task 3: Proto — `ListCommands`/`GetCommandHelp` on `PluginHostService`

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto`

Every new element needs a Go-grounded doc comment (`.claude/rules/proto-doc-comments.md`). No `CommandInfo` message exists yet — author it.

- [ ] **Step 1: Add RPCs to the `PluginHostService` block** (after the `Evaluate` RPC, `plugin.proto:190`)

```proto
  // ListCommands enumerates the commands the named character may execute,
  // ABAC-filtered by the host. SERVED: pluginHostServiceServer.ListCommands,
  // delegating to commandquery.Querier.Available. The subject is the request's
  // character_id (parity with the Lua holomush.list_commands(character_id) host
  // function — not the dispatch-token actor, since this is read-only metadata,
  // not an actor-gated mutation). incomplete is true when engine errors hid
  // some commands.
  rpc ListCommands(PluginHostServiceListCommandsRequest) returns (PluginHostServiceListCommandsResponse);

  // GetCommandHelp returns full help detail for one command after an access
  // check for character_id. SERVED: pluginHostServiceServer.GetCommandHelp,
  // delegating to commandquery.Querier.Help. Mirrors the Lua
  // holomush.get_command_help(name, character_id) host function.
  rpc GetCommandHelp(PluginHostServiceGetCommandHelpRequest) returns (PluginHostServiceGetCommandHelpResponse);
```

- [ ] **Step 2: Add the messages** (near the Evaluate messages, after `plugin.proto:782`)

```proto
// PluginHostServiceCommandInfo is per-command metadata returned by ListCommands.
message PluginHostServiceCommandInfo {
  // name is the canonical command name (e.g. "scene").
  string name = 1;
  // help is the one-line description from the command registry.
  string help = 2;
  // usage is the usage pattern (e.g. "scene <subcommand>").
  string usage = 3;
  // source is "core" or the owning plugin name.
  string source = 4;
}

// PluginHostServiceListCommandsRequest names the character whose executable
// command set to enumerate.
message PluginHostServiceListCommandsRequest {
  // character_id is the ULID of the character whose capabilities filter the list.
  string character_id = 1 [(buf.validate.field).string.min_len = 26];
}

// PluginHostServiceListCommandsResponse returns the filtered command set.
message PluginHostServiceListCommandsResponse {
  // commands is the ABAC-filtered set the character may execute.
  repeated PluginHostServiceCommandInfo commands = 1;
  // incomplete is true when engine errors hid some commands from the result.
  bool incomplete = 2;
}

// PluginHostServiceGetCommandHelpRequest names a command and the character whose
// access is checked before returning detail.
message PluginHostServiceGetCommandHelpRequest {
  // name is the canonical command name to describe.
  string name = 1 [(buf.validate.field).string.min_len = 1];
  // character_id is the ULID of the character whose access is checked.
  string character_id = 2 [(buf.validate.field).string.min_len = 26];
}

// PluginHostServiceGetCommandHelpResponse returns full help detail.
message PluginHostServiceGetCommandHelpResponse {
  // name is the canonical command name.
  string name = 1;
  // help is the one-line description.
  string help = 2;
  // usage is the usage pattern.
  string usage = 3;
  // help_text is the detailed markdown help body.
  string help_text = 4;
  // source is "core" or the owning plugin name.
  string source = 5;
}
```

- [ ] **Step 3: Regenerate + lint proto**

Run: `task lint:proto` (fails first if comments missing), then the buf generate target (confirm with `task --list | rg buf` or `rg generate Taskfile.yml`; commonly `task gen:proto` or `buf generate`).
Expected: generated Go in `pkg/proto/holomush/plugin/v1/` includes the new RPCs + messages; `task lint:proto` PASS.

- [ ] **Step 4: Verify build**

Run: `task build`
Expected: PASS.

- [ ] **Step 5: Commit**

Commit: `feat(proto): ListCommands/GetCommandHelp on PluginHostService (holomush-2zjio)`

---

### Task 4: goplugin `Host` field + handlers

**Files:**

- Modify: `internal/plugin/goplugin/host.go` (add `commandQuerier` field; wire from subsystem)
- Modify: `internal/plugin/goplugin/host_service.go` (handlers)
- Test: `internal/plugin/goplugin/host_service_test.go` (add handler unit tests if a unit harness exists; else covered by Task 6 integration)

- [ ] **Step 1: Write the failing handler test (table)**

```go
// internal/plugin/goplugin/host_service_command_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

func TestPluginHostServiceListCommandsFiltersByCharacter(t *testing.T) {
	reg := command.NewRegistry()
	look := command.NewTestEntry(command.CommandEntryConfig{Name: "look", PluginName: "core", Source: "core"})
	require.NoError(t, reg.Register(look))
	q := commandquery.New(reg, policytest.AllowAllEngine(), command.NewAliasCache())

	srv := &pluginHostServiceServer{
		host:       &Host{commandQuerier: q},
		pluginName: "test-plugin",
	}
	resp, err := srv.ListCommands(context.Background(), &pluginv1.PluginHostServiceListCommandsRequest{
		CharacterId: "01HCHAR0000000000000000ZZZ",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetCommands(), 1)
	assert.Equal(t, "look", resp.GetCommands()[0].GetName())
}

func TestPluginHostServiceListCommandsFailsClosedWithoutQuerier(t *testing.T) {
	srv := &pluginHostServiceServer{host: &Host{}, pluginName: "test-plugin"}
	_, err := srv.ListCommands(context.Background(), &pluginv1.PluginHostServiceListCommandsRequest{
		CharacterId: "01HCHAR0000000000000000ZZZ",
	})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `task test -- ./internal/plugin/goplugin/`
Expected: FAIL — `Host` has no `commandQuerier`; `ListCommands` undefined.

- [ ] **Step 3a: Add the field to `Host`** (`host.go:194-224`, after `engine`)

```go
	commandQuerier *commandquery.Querier
```

Add a `WithCommandQuerier` host option (mirror the existing `With*` host options pattern in `host.go`) and have the subsystem pass `s.CommandQuerier()` when constructing the binary `Host`. (Find the `Host` construction site with `mcp__probe__search_code "NewHost"` / where `goplugin.Host` is built in `subsystem.go` and add the option there.)

- [ ] **Step 3b: Implement the handlers** (`host_service.go`, after `Evaluate`)

```go
// ListCommands implements PluginHostService.ListCommands for binary plugins.
// Unlike Evaluate/EmitEvent, this is read-only metadata keyed by the request's
// character_id (parity with the Lua list_commands host function), so it does NOT
// require a dispatch token. Fail-closed on nil host / nil querier.
func (s *pluginHostServiceServer) ListCommands(ctx context.Context, req *pluginv1.PluginHostServiceListCommandsRequest) (*pluginv1.PluginHostServiceListCommandsResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	s.host.mu.RLock()
	q := s.host.commandQuerier
	s.host.mu.RUnlock()
	if q == nil {
		return nil, oops.Code("COMMAND_QUERIER_UNCONFIGURED").With("plugin", s.pluginName).Errorf("command querier is not configured")
	}
	charID, err := ulid.Parse(req.GetCharacterId())
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").With("plugin", s.pluginName).Errorf("invalid character_id")
	}
	res, err := q.Available(ctx, access.CharacterSubject(charID.String()))
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}
	out := make([]*pluginv1.PluginHostServiceCommandInfo, 0, len(res.Commands))
	for i := range res.Commands {
		out = append(out, &pluginv1.PluginHostServiceCommandInfo{
			Name: res.Commands[i].Name, Help: res.Commands[i].Help,
			Usage: res.Commands[i].Usage, Source: res.Commands[i].Source,
		})
	}
	return &pluginv1.PluginHostServiceListCommandsResponse{Commands: out, Incomplete: res.Incomplete}, nil
}

// GetCommandHelp implements PluginHostService.GetCommandHelp for binary plugins.
func (s *pluginHostServiceServer) GetCommandHelp(ctx context.Context, req *pluginv1.PluginHostServiceGetCommandHelpRequest) (*pluginv1.PluginHostServiceGetCommandHelpResponse, error) {
	if s.host == nil {
		return nil, oops.With("plugin", s.pluginName).New("plugin host service is not configured")
	}
	s.host.mu.RLock()
	q := s.host.commandQuerier
	s.host.mu.RUnlock()
	if q == nil {
		return nil, oops.Code("COMMAND_QUERIER_UNCONFIGURED").With("plugin", s.pluginName).Errorf("command querier is not configured")
	}
	charID, err := ulid.Parse(req.GetCharacterId())
	if err != nil {
		return nil, oops.Code("INVALID_ARGUMENT").With("plugin", s.pluginName).Errorf("invalid character_id")
	}
	d, err := q.Help(ctx, access.CharacterSubject(charID.String()), req.GetName())
	if err != nil {
		return nil, oops.With("plugin", s.pluginName).Wrap(err)
	}
	return &pluginv1.PluginHostServiceGetCommandHelpResponse{
		Name: d.Name, Help: d.Help, Usage: d.Usage, HelpText: d.HelpText, Source: d.Source,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

Run: `task lint:go`
Commit: `feat(plugin): binary host RPC handlers for ListCommands/GetCommandHelp (holomush-2zjio)`

---

### Task 5: SDK facade `CommandLister`

**Files:**

- Create: `pkg/plugin/command_lister.go`
- Modify: `pkg/plugin/sdk.go` (inject in `Init`)
- Test: `pkg/plugin/command_lister_test.go`

Mirror `pkg/plugin/evaluate_client.go` exactly. No token ferrying (ListCommands isn't token-gated).

- [ ] **Step 1: Write the failing test**

```go
// pkg/plugin/command_lister_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostCommandListerNilClientFailsClosed(t *testing.T) {
	c := &hostCommandClient{client: nil}
	_, err := c.ListCommands(context.Background(), "01HCHAR0000000000000000AAA")
	require.Error(t, err)
}
```

(A full success test needs a stub `PluginHostServiceClient`; mirror the stub style from an existing `pkg/plugin/*_test.go`. Confirm with `mcp__probe__search_code "PluginHostServiceClient stub"`.)

- [ ] **Step 2: Run it to verify it fails**

Run: `task test -- ./pkg/plugin/`
Expected: FAIL — `hostCommandClient` undefined.

- [ ] **Step 3: Write the facade**

```go
// pkg/plugin/command_lister.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	"github.com/samber/oops"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// CommandSummary is one command's metadata as seen by a binary plugin.
type CommandSummary struct {
	Name, Help, Usage, Source string
}

// CommandList is the result of CommandLister.ListCommands.
type CommandList struct {
	Commands   []CommandSummary
	Incomplete bool
}

// CommandLister is the SDK facade binary plugins use to enumerate the commands a
// character may execute (parity with the Lua holomush.list_commands host function).
type CommandLister interface {
	ListCommands(ctx context.Context, characterID string) (CommandList, error)
}

// CommandListerAware is the optional interface a service provider implements to
// receive a CommandLister during Init, parallel to HostEvaluatorAware.
type CommandListerAware interface {
	SetCommandLister(CommandLister)
}

type hostCommandClient struct {
	client pluginv1.PluginHostServiceClient
}

// ListCommands implements CommandLister. A nil client fails closed.
func (c *hostCommandClient) ListCommands(ctx context.Context, characterID string) (CommandList, error) {
	if c.client == nil {
		return CommandList{}, oops.New("host command lister client is not configured")
	}
	resp, err := c.client.ListCommands(ctx, &pluginv1.PluginHostServiceListCommandsRequest{CharacterId: characterID})
	if err != nil {
		return CommandList{}, oops.With("character_id", characterID).Wrap(err)
	}
	out := make([]CommandSummary, 0, len(resp.GetCommands()))
	for _, ci := range resp.GetCommands() {
		out = append(out, CommandSummary{Name: ci.GetName(), Help: ci.GetHelp(), Usage: ci.GetUsage(), Source: ci.GetSource()})
	}
	return CommandList{Commands: out, Incomplete: resp.GetIncomplete()}, nil
}
```

- [ ] **Step 4: Inject in `Init`** (`sdk.go:144-192`)

Add to the want-detection block: `_, wantsCommandLister := a.serviceProvider.(CommandListerAware)`; include it in the `if wants... {` dial condition; and after the evaluator injection:

```go
	if clAware, ok := a.serviceProvider.(CommandListerAware); ok {
		clAware.SetCommandLister(&hostCommandClient{client: hostClient})
	}
```

- [ ] **Step 5: Run tests, lint, commit**

Run: `task test -- ./pkg/plugin/` then `task lint:go`
Expected: PASS.
Commit: `feat(plugin-sdk): CommandLister facade for binary plugins (holomush-2zjio)`

---

### Task 6: Binary↔Lua parity integration test

**Files:**

- Test: `test/integration/plugin/command_introspection_parity_test.go` (Create)
- Modify: a testdata binary plugin to call `ListCommands` (mirror `testdata/forgery_plugin`)

- [ ] **Step 1: Write the parity spec** (mirror `actor_authentication_test.go:231` structure)

```go
//go:build integration

package plugin_test

// Describe("Command-introspection runtime parity (holomush-2zjio)", ...)
//   It("binary plugin ListCommands returns the same set as the Lua host function for the same character")
//   It("both runtimes omit a capability-gated command the character is denied")
```

The `It` loads both a Lua plugin and a binary plugin via the host fixture, calls each runtime's path for the same character against the same registry, and asserts equal command-name sets (INV-2).

- [ ] **Step 2-4: Run/iterate**

Run: `task test:int -- ./test/integration/plugin/`
Expected: PASS — sets equal across runtimes.

- [ ] **Step 5: Commit**

Commit: `test(plugin): binary↔Lua command-introspection parity (holomush-2zjio)`

---

## Phase 3 — Web RPC

### Task 7: Proto — `CoreService.ListAvailableCommands` + `WebService.WebListCommands`

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto`
- Modify: `api/proto/holomush/web/v1/web.proto`

- [ ] **Step 1: Core RPC + messages** (RPC after `ListFocusPresence`, `core.proto:162`; messages near `:449`)

```proto
  // ListAvailableCommands returns the commands the session's own character may
  // execute, with the system/manifest alias map for those commands. SERVED:
  // CoreServer.ListAvailableCommands, delegating to commandquery.Querier.Available.
  // Self-scoped: the subject is the session's character (ownership-validated),
  // never an arbitrary character_id. Pure read.
  rpc ListAvailableCommands(ListAvailableCommandsRequest) returns (ListAvailableCommandsResponse);
```

```proto
// AvailableCommand is one command's metadata in a ListAvailableCommands result.
message AvailableCommand {
  // name is the canonical command name.
  string name = 1;
  // help is the one-line description.
  string help = 2;
  // usage is the usage pattern.
  string usage = 3;
  // source is "core" or the owning plugin name.
  string source = 4;
}

// ListAvailableCommandsRequest asks for the session character's executable set.
message ListAvailableCommandsRequest {
  // meta carries request correlation data.
  RequestMeta meta = 1;
  // player_session_token proves the caller owns session_id; failures collapse to SESSION_NOT_FOUND.
  string player_session_token = 2;
  // session_id names the session whose character's command set is enumerated.
  string session_id = 3;
}

// ListAvailableCommandsResponse returns the filtered set + alias map.
message ListAvailableCommandsResponse {
  // meta echoes request correlation data.
  ResponseMeta meta = 1;
  // commands is the ABAC-filtered set the session character may execute.
  repeated AvailableCommand commands = 2;
  // aliases maps alias → canonical command name (system/manifest aliases for visible commands).
  map<string, string> aliases = 3;
  // incomplete is true when engine errors hid some commands.
  bool incomplete = 4;
}
```

- [ ] **Step 2: Web RPC + messages** (RPC after `WebListFocusPresence`, `web.proto:228`; messages near `:833`)

```proto
  // WebListCommands returns the recognized-command set + alias map for the
  // session's character, for the composer's command chip. Proxies to
  // CoreService.ListAvailableCommands; player_session_token is read from the
  // cookie by gateway middleware.
  rpc WebListCommands(WebListCommandsRequest) returns (WebListCommandsResponse);
```

```proto
// WebAvailableCommand is one command's metadata for the web composer.
message WebAvailableCommand {
  // name is the canonical command name shown on the chip.
  string name = 1;
  // help is the one-line description (for future tooltip use).
  string help = 2;
  // usage is the usage pattern.
  string usage = 3;
  // source is "core" or the owning plugin name.
  string source = 4;
}

// WebListCommandsRequest names the session whose character's commands to list.
message WebListCommandsRequest {
  // session_id identifies the requesting session; core resolves the character and enforces ownership.
  string session_id = 1;
}

// WebListCommandsResponse returns the command set + alias map for the composer.
message WebListCommandsResponse {
  // commands is the set the session character may execute.
  repeated WebAvailableCommand commands = 1;
  // aliases maps alias → canonical command name for visible commands.
  map<string, string> aliases = 2;
  // incomplete is true when engine errors hid some commands.
  bool incomplete = 3;
}
```

- [ ] **Step 3: Regenerate Go + TS**

Run: the proto-gen target (Go into `pkg/proto/...`, TS into `web/src/lib/connect/...`), then `task lint:proto`.
Expected: `webListCommands` appears on the generated `WebService` descriptor in `web/src/lib/connect/holomush/web/v1/web_pb.ts`.

- [ ] **Step 4: Build**

Run: `task build`
Expected: PASS.

- [ ] **Step 5: Commit**

Commit: `feat(proto): ListAvailableCommands (core) + WebListCommands (web) (holomush-2zjio)`

---

### Task 8: CoreServer handler `ListAvailableCommands`

**Files:**

- Create: `internal/grpc/list_available_commands.go`
- Modify: `internal/grpc/server.go` (option to inject `*commandquery.Querier`, mirror `WithAccessEngine`)
- Test: `internal/grpc/list_available_commands_test.go`

Mirror `internal/grpc/list_focus_presence.go` for ownership + self-scoping.

- [ ] **Step 1: Write the failing test** (table; uses an in-memory querier + a test session store — mirror `list_focus_presence_test.go` setup)

```go
// TestListAvailableCommandsReturnsSessionCharacterCommands — ownership ok, returns the
//   character's allowed set + alias map.
// TestListAvailableCommandsCollapsesUnknownSessionToNotFound — bad token → SESSION_NOT_FOUND.
// TestListAvailableCommandsFailsClosedWithoutQuerier — nil querier → PERMISSION_DENIED.
```

- [ ] **Step 2: Run to verify fail**

Run: `task test -- ./internal/grpc/ -run ListAvailableCommands`
Expected: FAIL — handler undefined.

- [ ] **Step 3: Write the handler**

```go
// internal/grpc/list_available_commands.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/auth"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// ListAvailableCommands returns the commands the session's own character may
// execute. Self-scoped (the subject is the session character, never an arbitrary
// id) per design INV-5. Ownership failures collapse to SESSION_NOT_FOUND, mirroring
// ListFocusPresence.
func (s *CoreServer) ListAvailableCommands(ctx context.Context, req *corev1.ListAvailableCommandsRequest) (*corev1.ListAvailableCommandsResponse, error) {
	if req == nil {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("request is required")
	}
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}
	if req.SessionId == "" {
		return nil, oops.Code("INVALID_ARGUMENT").Errorf("session_id is required")
	}
	if _, err := auth.ValidateSessionOwnership(ctx, s.playerSessionRepo, s.sessionStore, req.GetPlayerSessionToken(), req.GetSessionId()); err != nil {
		return nil, oops.Code("SESSION_NOT_FOUND").With("session_id", req.SessionId).Errorf("session not found")
	}
	info, err := s.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		return nil, oops.Code("SESSION_NOT_FOUND").With("session_id", req.SessionId).Errorf("session not found")
	}
	if info.IsExpired() {
		return nil, oops.Code("SESSION_EXPIRED").With("session_id", req.SessionId).Errorf("session expired")
	}
	if s.commandQuerier == nil {
		slog.ErrorContext(ctx, "list available commands: querier not configured", "request_id", requestID, "session_id", req.SessionId)
		return nil, oops.Code("PERMISSION_DENIED").With("reason", "command_querier_not_configured").Errorf("permission denied")
	}
	res, err := s.commandQuerier.Available(ctx, access.CharacterSubject(info.CharacterID.String()))
	if err != nil {
		return nil, oops.Code("INTERNAL").Wrap(err)
	}
	out := make([]*corev1.AvailableCommand, 0, len(res.Commands))
	for i := range res.Commands {
		out = append(out, &corev1.AvailableCommand{
			Name: res.Commands[i].Name, Help: res.Commands[i].Help,
			Usage: res.Commands[i].Usage, Source: res.Commands[i].Source,
		})
	}
	return &corev1.ListAvailableCommandsResponse{
		Meta: responseMeta(requestID), Commands: out, Aliases: res.Aliases, Incomplete: res.Incomplete,
	}, nil
}
```

Add `commandQuerier *commandquery.Querier` to `CoreServer` + `WithCommandQuerier` option in `server.go` (mirror `WithAccessEngine`), wired at `cmd/holomush/sub_grpc.go` from `PluginSubsystem.CommandQuerier()`.

- [ ] **Step 4: Run/pass**

Run: `task test -- ./internal/grpc/ -run ListAvailableCommands`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

Commit: `feat(grpc): ListAvailableCommands CoreServer handler (holomush-2zjio)`

---

### Task 9: Gateway proxy `WebListCommands`

**Files:**

- Modify: `internal/web/handler.go` (handler + `CoreClient` interface method)
- Modify: `internal/grpc/client.go` (in-process `ListAvailableCommands`)
- Modify: `internal/web/handler_test.go` (mock method)
- Test: `internal/web/handler_test.go` (proxy test)

- [ ] **Step 1: Write the failing proxy test** (mirror the `WebListFocusPresence` handler test; assert it calls `client.ListAvailableCommands` and maps fields)

- [ ] **Step 2: Run/fail**

Run: `task test -- ./internal/web/ -run WebListCommands`
Expected: FAIL.

- [ ] **Step 3: Add interface method, in-process client method, handler**

`internal/web/handler.go` `CoreClient` interface (near `:57`):

```go
	// Command introspection
	ListAvailableCommands(ctx context.Context, req *corev1.ListAvailableCommandsRequest) (*corev1.ListAvailableCommandsResponse, error)
```

`internal/grpc/client.go` (mirror the `ListFocusPresence` wrapper at `:293`):

```go
func (c *InProcessClient) ListAvailableCommands(ctx context.Context, req *corev1.ListAvailableCommandsRequest) (*corev1.ListAvailableCommandsResponse, error) {
	return c.server.ListAvailableCommands(ctx, req) //nolint:wrapcheck // in-process passthrough
}
```

`internal/web/handler.go` handler (mirror `WebListFocusPresence` at `:494`):

```go
func (h *Handler) WebListCommands(ctx context.Context, req *connect.Request[webv1.WebListCommandsRequest]) (*connect.Response[webv1.WebListCommandsResponse], error) {
	token := req.Header().Get(headerInjectSessionToken)
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	coreResp, err := h.client.ListAvailableCommands(rpcCtx, &corev1.ListAvailableCommandsRequest{
		SessionId: req.Msg.GetSessionId(), PlayerSessionToken: token,
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through so clients can distinguish SESSION_NOT_FOUND / PERMISSION_DENIED.
	}
	out := make([]*webv1.WebAvailableCommand, 0, len(coreResp.GetCommands()))
	for _, c := range coreResp.GetCommands() {
		if c == nil {
			continue
		}
		out = append(out, &webv1.WebAvailableCommand{Name: c.GetName(), Help: c.GetHelp(), Usage: c.GetUsage(), Source: c.GetSource()})
	}
	return connect.NewResponse(&webv1.WebListCommandsResponse{
		Commands: out, Aliases: coreResp.GetAliases(), Incomplete: coreResp.GetIncomplete(),
	}), nil
}
```

Add the `ListAvailableCommands` method to `mockCoreClient` in `internal/web/handler_test.go` (pattern at `:212`).

- [ ] **Step 4: Run/pass**

Run: `task test -- ./internal/web/ -run WebListCommands`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

Commit: `feat(web): WebListCommands gateway proxy (holomush-2zjio)`

---

## Phase 4 — Composer chip

### Task 10: `commandListStore.ts` — fetch + cache

**Files:**

- Create: `web/src/lib/stores/commandListStore.ts`
- Test: `web/src/lib/stores/commandListStore.test.ts`

- [ ] **Step 1: Write the failing test** (mirror `commandHistoryStore.test.ts` style)

```ts
// web/src/lib/stores/commandListStore.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { commandList, seedCommandList, resetCommandList } from './commandListStore';

describe('commandListStore', () => {
  beforeEach(() => resetCommandList());

  it('starts empty', () => {
    const s = get(commandList);
    expect(s.names.size).toBe(0);
    expect(Object.keys(s.aliases)).toHaveLength(0);
  });

  it('seedCommandList stores names as a set and keeps aliases', () => {
    seedCommandList({
      commands: [{ name: 'look' }, { name: 'scene' }],
      aliases: { l: 'look', '"': 'say' },
      incomplete: false,
    });
    const s = get(commandList);
    expect(s.names.has('scene')).toBe(true);
    expect(s.aliases['l']).toBe('look');
    expect(s.incomplete).toBe(false);
  });
});
```

- [ ] **Step 2: Run/fail**

Run: `cd web && pnpm vitest run src/lib/stores/commandListStore.test.ts`
Expected: FAIL — module missing.

- [ ] **Step 3: Write the store** (writable + async fetch, mirroring contentStore + commandHistoryStore)

```ts
// web/src/lib/stores/commandListStore.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable } from 'svelte/store';
import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';

export interface CommandListState {
  names: Set<string>;            // canonical command names the character may run
  aliases: Record<string, string>; // alias → canonical name
  incomplete: boolean;
}

const empty: CommandListState = { names: new Set(), aliases: {}, incomplete: false };
export const commandList = writable<CommandListState>(empty);

const client = createClient(WebService, transport);

export function resetCommandList(): void {
  commandList.set({ names: new Set(), aliases: {}, incomplete: false });
}

export function seedCommandList(resp: {
  commands: { name: string }[];
  aliases: Record<string, string>;
  incomplete: boolean;
}): void {
  commandList.set({
    names: new Set(resp.commands.map((c) => c.name)),
    aliases: { ...resp.aliases },
    incomplete: resp.incomplete,
  });
}

// fetchCommandList loads the recognized-command set for a session. Mirrors the
// stale-session guard used by CommandInput's history fetch. Errors degrade to an
// empty list (chip simply won't render — INV-6 graceful degradation).
export async function fetchCommandList(sessionId: string): Promise<void> {
  if (!sessionId) {
    resetCommandList();
    return;
  }
  try {
    const resp = await client.webListCommands({ sessionId });
    seedCommandList({
      commands: (resp.commands ?? []).map((c) => ({ name: c.name })),
      aliases: resp.aliases ?? {},
      incomplete: resp.incomplete ?? false,
    });
  } catch (e) {
    console.warn('[commands] list load failed', e);
    resetCommandList();
  }
}
```

- [ ] **Step 4: Run/pass**

Run: `cd web && pnpm vitest run src/lib/stores/commandListStore.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

Commit: `feat(web): commandListStore — fetch + cache recognized commands (holomush-2zjio)`

---

### Task 11: Resolution helper `composerChip.ts`

**Files:**

- Create: `web/src/lib/components/terminal/composerChip.ts`
- Test: `web/src/lib/components/terminal/composerChip.test.ts`

This is the single resolution helper replacing the hardcoded `modeChip` matcher (design INV-4) and the tier-3 alias seam.

- [ ] **Step 1: Write the failing test**

```ts
// web/src/lib/components/terminal/composerChip.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';
import { resolveComposerChip } from './composerChip';

const state = {
  names: new Set(['look', 'scene', 'say', 'pose', 'ooc']),
  aliases: { l: 'look', '"': 'say', ':': 'pose', ';': 'pose' },
  incomplete: false,
};

describe('resolveComposerChip', () => {
  it('returns a speech chip for say sigil', () => {
    expect(resolveComposerChip('"hi there', state)).toEqual({ kind: 'say', label: 'say' });
  });
  it('returns a speech chip for the say verb', () => {
    expect(resolveComposerChip('say hi', state)).toEqual({ kind: 'say', label: 'say' });
  });
  it('returns a pose chip for the : sigil', () => {
    expect(resolveComposerChip(':waves', state)).toEqual({ kind: 'pose', label: 'pose' });
  });
  it('returns a command chip with canonical name for a recognized command', () => {
    expect(resolveComposerChip('scene list', state)).toEqual({ kind: 'command', label: 'scene' });
  });
  it('resolves an alias to its canonical name on a command chip', () => {
    expect(resolveComposerChip('l', state)).toEqual({ kind: 'command', label: 'look' });
  });
  it('returns null for an unrecognized first token', () => {
    expect(resolveComposerChip('sceen list', state)).toBeNull();
  });
  it('returns null for empty input', () => {
    expect(resolveComposerChip('   ', state)).toBeNull();
  });
});
```

- [ ] **Step 2: Run/fail**

Run: `cd web && pnpm vitest run src/lib/components/terminal/composerChip.test.ts`
Expected: FAIL — module missing.

- [ ] **Step 3: Write the helper**

```ts
// web/src/lib/components/terminal/composerChip.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import type { CommandListState } from '$lib/stores/commandListStore';

export type ChipKind = 'say' | 'pose' | 'ooc' | 'command';
export interface ComposerChip {
  kind: ChipKind;
  label: string; // lowercase; ModeChip uppercases via CSS
}

const SPEECH = new Set(['say', 'pose', 'ooc']);
// Single-char sigils that attach to text without a space (tier-2 prefix aliases).
const SIGILS = new Set(['"', ':', ';']);

// resolveComposerChip maps composer text to a chip via the server-sourced command
// set + alias map. Replaces the former hardcoded prefix matcher (design INV-4).
// Tier-3 (player aliases) seam: a future server ResolveInput RPC would slot in here.
export function resolveComposerChip(text: string, state: CommandListState): ComposerChip | null {
  const v = text.trimStart();
  if (v === '') return null;

  // Sigil-prefix aliases attach directly to text (":waves", '"hi'). Resolve the
  // leading sigil char before falling back to whitespace tokenization.
  const first = v[0];
  let token: string;
  if (SIGILS.has(first)) {
    token = first;
  } else {
    token = v.split(/\s+/, 1)[0];
  }

  const canonical = state.names.has(token) ? token : state.aliases[token];
  if (!canonical) return null;

  if (SPEECH.has(canonical)) {
    return { kind: canonical as ChipKind, label: canonical };
  }
  return { kind: 'command', label: canonical };
}
```

- [ ] **Step 4: Run/pass**

Run: `cd web && pnpm vitest run src/lib/components/terminal/composerChip.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

Commit: `feat(web): composer chip resolution helper (holomush-2zjio)`

---

### Task 12: `ModeChip` command kind + `CommandInput` integration

**Files:**

- Modify: `web/src/lib/components/terminal/ModeChip.svelte`
- Modify: `web/src/lib/components/terminal/CommandInput.svelte`

- [ ] **Step 1: Extend `ModeChip.svelte`** to accept the `command` kind and a label

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  type Kind = 'say' | 'pose' | 'ooc' | 'command';
  interface Props { kind: Kind; label: string; }
  let { kind, label }: Props = $props();
</script>

<span class="mode-chip"
  class:mode-say={kind === 'say'}
  class:mode-pose={kind === 'pose'}
  class:mode-ooc={kind === 'ooc'}
  class:mode-command={kind === 'command'}>
  {label}
</span>

<style>
  .mode-chip {
    display: inline-flex; align-items: center;
    padding: 0 6px; border-radius: 999px;
    font-size: 10px; font-weight: bold; letter-spacing: 0.5px;
    text-transform: uppercase; flex-shrink: 0;
    line-height: 16px; height: 16px;
  }
  .mode-say { background: color-mix(in srgb, var(--mush-say-speaker) 20%, transparent); color: var(--mush-say-speaker); }
  .mode-pose { background: color-mix(in srgb, var(--mush-pose-actor) 20%, transparent); color: var(--mush-pose-actor); }
  .mode-ooc { background: color-mix(in srgb, var(--mush-ooc) 20%, transparent); color: var(--mush-ooc); }
  /* Recognized-command chip — distinct from speech tokens. Uses an existing
     neutral UI token (NOT a --mush-* speech color, NOT amber per branding INV-1). */
  .mode-command { background: color-mix(in srgb, var(--color-muted-foreground) 18%, transparent); color: var(--color-muted-foreground); }
</style>
```

> Confirm `--color-muted-foreground` exists in `web/src/app.css` `@theme`; if not, pick an existing neutral `--color-*` token. Palette choice is deliberately conservative (design Non-goals: no palette work).

- [ ] **Step 2: Update `CommandInput.svelte`**

Replace the `modeChip` `$derived` (`:39-46`) and the `<ModeChip>` render (`:171`):

```svelte
  import { resolveComposerChip } from './composerChip';
  import { commandList, fetchCommandList } from '$lib/stores/commandListStore';

  let chip = $derived(resolveComposerChip(text, $commandList));
```

In the session `$effect` (`:53-77`), after the history fetch, add (mirroring the stale-session guard):

```svelte
    fetchCommandList(sessionId); // refetch on session/character change
```

Template (`:171`):

```svelte
  {#if chip}<ModeChip kind={chip.kind} label={chip.label} />{/if}
```

Remove the now-unused `'say' | 'pose' | 'ooc'` typing of the old derived.

- [ ] **Step 3: Type-check + build**

Run: `cd web && pnpm check && pnpm build`
Expected: PASS — no type errors; the old hardcoded matcher is gone (INV-4).

- [ ] **Step 4: Manual smoke (dev)**

Run: `task dev`; in the terminal type `scene`, `l`, `"hi`, `:waves`, `ooc x`, and `sceen`.
Expected: `scene`→SCENE chip, `l`→LOOK chip, `"`/`:`/`ooc` → speech chips, `sceen` → no chip.

- [ ] **Step 5: Commit**

Commit: `feat(web): recognized-command chip in composer (holomush-2zjio)`

---

### Task 13: Component test — chip behavior across kinds

**Files:**

- Test: `web/src/lib/components/terminal/composerChip.integration.test.ts` (Create)

- [ ] **Step 1: Write the test** — seed `commandList` with a realistic set incl. a denied command absent from the set; assert speech sigils still chip, `scene` chips as command, an alias resolves, a command NOT in the set (simulating ABAC denial / unknown) yields null, and `incomplete:true` still chips the present commands (INV-5, INV-6, INV-7).

```ts
import { describe, it, expect } from 'vitest';
import { resolveComposerChip } from './composerChip';

describe('composer chip across kinds (INV-5/6/7)', () => {
  const denied = { names: new Set(['look', 'say']), aliases: { '"': 'say' }, incomplete: true };
  it('omits the chip for a command absent from the ABAC-filtered set (INV-5)', () => {
    expect(resolveComposerChip('scene list', denied)).toBeNull();
  });
  it('still chips present commands when incomplete (INV-6)', () => {
    expect(resolveComposerChip('look', denied)).toEqual({ kind: 'command', label: 'look' });
  });
  it('preserves speech chips distinct from command chips (INV-7)', () => {
    expect(resolveComposerChip('"hi', denied)).toEqual({ kind: 'say', label: 'say' });
  });
});
```

- [ ] **Step 2-4: Run/pass**

Run: `cd web && pnpm vitest run src/lib/components/terminal/composerChip.integration.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

Commit: `test(web): composer chip invariant coverage (holomush-2zjio)`

---

## Final verification

Run before opening the PR:

- `task test -- ./internal/command/... ./internal/grpc/... ./internal/plugin/...`
- `task test:int -- ./internal/plugin/... ./test/integration/plugin/...` (parity + wiring regression)
- `cd web && pnpm check && pnpm build && pnpm vitest run`
- `task pr-prep`

## Follow-ups (file as separate beads at landing)

- **Tier-3 player aliases** — server-side per-input resolution behind `resolveComposerChip` (deferred).
- **`Query*` trio binary parity** — `QueryLocation`/`QueryCharacter`/`QueryLocationCharacters` on `PluginHostService`.
- **holomush-4hhh0** — remove the dead `HostFunctionsService` proto (P1, independent).

<!-- adr-capture: sha256=b4483fe9bbada2bd; session=brainstorm-2zjio; ts=2026-05-30T00:19:36Z; adrs=holomush-nxwl5,holomush-kn3o1 -->
