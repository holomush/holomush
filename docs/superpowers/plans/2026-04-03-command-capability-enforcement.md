# Command Capability Enforcement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace broken string-based command capabilities with structured `Capability{Action, Resource, Scope}` objects and implement two-layer authorization (command execution + capability pre-flight) at dispatch time.

**Architecture:** The `Capability` struct replaces `[]string` capabilities throughout the command system. The dispatcher checks Layer 1 (command execution via existing ABAC policies) then Layer 2 (capability pre-flight via new `CanPerformAction` engine method). Plugin YAML manifests switch from `capabilities: ["comms.say"]` to structured `capabilities: [{action: emit, resource: stream, scope: local}]`.

**Tech Stack:** Go, YAML (plugin manifests), ABAC policy engine

**Spec:** `docs/superpowers/specs/2026-04-03-command-capability-enforcement-design.md`

---

## File Map

| Action | File | Purpose |
| ------ | ---- | ------- |
| Modify | `internal/command/types.go` | Add `Capability` struct, scope constants, validation; change `CommandEntryConfig.Capabilities` from `[]string` to `[]Capability`; update `CommandEntry`, `GetCapabilities`, `NewCommandEntry`, `NewTestEntry` |
| Modify | `internal/command/access.go` | Remove `CheckCapability`; add `CheckCommandExecution` (Layer 1) and `CheckCapabilityPreFlight` (Layer 2) |
| Modify | `internal/command/errors.go` | Add `ErrInsufficientCapability` error constructor |
| Modify | `internal/command/dispatcher.go` | Replace capability loop with two-layer authorization |
| Modify | `internal/access/policy/types/types.go` | Add `CanPerformAction` to `AccessPolicyEngine` interface |
| Modify | `internal/access/policy/engine.go` | Implement `CanPerformAction` |
| Modify | `internal/command/handlers/register.go` | Update `shutdown` and `resetpassword` with structured capabilities |
| Modify | `internal/plugin/manifest.go` | Change `CommandSpec.Capabilities` from `[]string` to `[]Capability`; add validation |
| Modify | `internal/plugin/manager.go` | Pass structured capabilities to `NewCommandEntry` |
| Modify | `plugins/core-communication/plugin.yaml` | Structured capabilities |
| Modify | `plugins/core-building/plugin.yaml` | Structured capabilities |
| Modify | `plugins/core-aliases/plugin.yaml` | Structured capabilities |
| Modify | `plugins/core-objects/plugin.yaml` | Structured capabilities |
| Modify | `plugins/core-help/plugin.yaml` | Structured capabilities (empty stays empty) |
| Modify | `internal/command/types_test.go` | Update for `[]Capability` |
| Modify | `internal/command/access_test.go` | Rewrite for new functions |
| Modify | `internal/command/dispatcher_test.go` | Update capability format |
| Modify | `internal/command/registry_test.go` | Update capability format |
| Modify | `internal/command/errors_test.go` | Update for new error type |
| Modify | `internal/command/handlers/register_test.go` | Update capability assertions |
| Modify | `internal/command/plugin_dispatch_test.go` | Update for `[]Capability` |
| Modify | `internal/plugin/manifest_test.go` | Update for structured capabilities |
| Modify | `internal/plugin/hostfunc/commands_test.go` | Update all capability references |
| Modify | `internal/plugin/help_integration_test.go` | Update capability assertions |
| Modify | `internal/plugin/communication_integration_test.go` | Update capability assertions |
| Modify | `internal/plugin/service_proxy_impl_test.go` | Update test entries |
| Modify | `internal/access/policy/engine_test.go` | Add `CanPerformAction` tests |
| Modify | `test/integration/telnet/e2e_test.go` | Update any capability references |

---

## Task 1: Add `Capability` Struct and Scope Constants

**Files:**

- Modify: `internal/command/types.go`
- Modify: `internal/command/types_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/command/types_test.go`, add:

```go
func TestCapability_Validate_Valid(t *testing.T) {
	tests := []struct {
		name string
		cap  Capability
	}{
		{"basic", Capability{Action: "read", Resource: "location"}},
		{"with local scope", Capability{Action: "write", Resource: "exit", Scope: ScopeLocal}},
		{"with global scope", Capability{Action: "admin", Resource: "server", Scope: ScopeGlobal}},
		{"self scope explicit", Capability{Action: "write", Resource: "character", Scope: ScopeSelf}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, tt.cap.Validate())
		})
	}
}

func TestCapability_Validate_Invalid(t *testing.T) {
	tests := []struct {
		name string
		cap  Capability
		want string
	}{
		{"empty action", Capability{Action: "", Resource: "location"}, "action"},
		{"empty resource", Capability{Action: "read", Resource: ""}, "resource"},
		{"unknown action", Capability{Action: "destroy", Resource: "location"}, "action"},
		{"unknown resource", Capability{Action: "read", Resource: "spaceship"}, "resource"},
		{"invalid scope", Capability{Action: "read", Resource: "location", Scope: "everywhere"}, "scope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cap.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestCapability_EffectiveScope(t *testing.T) {
	assert.Equal(t, ScopeSelf, Capability{Action: "read", Resource: "character"}.EffectiveScope())
	assert.Equal(t, ScopeLocal, Capability{Action: "read", Resource: "location", Scope: ScopeLocal}.EffectiveScope())
	assert.Equal(t, ScopeGlobal, Capability{Action: "emit", Resource: "stream", Scope: ScopeGlobal}.EffectiveScope())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestCapability -count=1 ./internal/command/...`
Expected: FAIL — `Capability` undefined.

- [ ] **Step 3: Implement `Capability` struct**

In `internal/command/types.go`, add before `CommandHandler`:

```go
// Scope constants define the spatial context for capability pre-flight checks.
const (
	ScopeSelf   = ""       // default — own character only
	ScopeLocal  = "local"  // current location + contents
	ScopeGlobal = "global" // server-wide
)

// validActions lists the known ABAC actions for capability validation.
var validActions = map[string]bool{
	"read": true, "write": true, "emit": true, "enter": true,
	"use": true, "delete": true, "execute": true, "admin": true,
}

// validResourceTypes lists the known ABAC resource types for capability validation.
var validResourceTypes = map[string]bool{
	"character": true, "location": true, "exit": true, "object": true,
	"stream": true, "property": true, "scene": true, "command": true,
	"server": true, "alias": true, "player": true,
}

// validScopes lists the known scope values.
var validScopes = map[string]bool{
	ScopeSelf: true, ScopeLocal: true, ScopeGlobal: true,
}

// Capability declares a resource type and action that a command will
// attempt. Used for pre-flight authorization at dispatch time.
type Capability struct {
	Action   string `yaml:"action" json:"action"`
	Resource string `yaml:"resource" json:"resource"`
	Scope    string `yaml:"scope,omitempty" json:"scope,omitempty"`
}

// Validate checks that the capability has valid action, resource, and scope.
func (c Capability) Validate() error {
	if c.Action == "" {
		return oops.Code("INVALID_CAPABILITY").Errorf("action is required")
	}
	if !validActions[c.Action] {
		return oops.Code("INVALID_CAPABILITY").
			With("action", c.Action).
			Errorf("unknown action %q", c.Action)
	}
	if c.Resource == "" {
		return oops.Code("INVALID_CAPABILITY").Errorf("resource is required")
	}
	if !validResourceTypes[c.Resource] {
		return oops.Code("INVALID_CAPABILITY").
			With("resource", c.Resource).
			Errorf("unknown resource type %q", c.Resource)
	}
	if !validScopes[c.Scope] {
		return oops.Code("INVALID_CAPABILITY").
			With("scope", c.Scope).
			Errorf("unknown scope %q", c.Scope)
	}
	return nil
}

// EffectiveScope returns the scope, defaulting to ScopeSelf if empty.
func (c Capability) EffectiveScope() string {
	if c.Scope == "" {
		return ScopeSelf
	}
	return c.Scope
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestCapability -count=1 ./internal/command/...`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(command): add Capability struct with action/resource/scope and validation
```

---

## Task 2: Migrate `CommandEntryConfig` and `CommandEntry` to `[]Capability`

**Files:**

- Modify: `internal/command/types.go`
- Modify: `internal/command/types_test.go`

- [ ] **Step 1: Change the field types**

In `internal/command/types.go`:

Change `CommandEntryConfig`:

```go
Capabilities []Capability // ALL required capabilities (AND logic)
```

Change `CommandEntry`:

```go
capabilities []Capability // ALL required capabilities (AND logic) - use GetCapabilities() for safe access
```

Change `GetCapabilities` return type and body:

```go
func (e *CommandEntry) GetCapabilities() []Capability {
	if e.capabilities == nil {
		return nil
	}
	result := make([]Capability, len(e.capabilities))
	copy(result, e.capabilities)
	return result
}
```

In `NewCommandEntry`, add validation:

```go
for i, cap := range cfg.Capabilities {
	if err := cap.Validate(); err != nil {
		return nil, oops.Code("INVALID_CAPABILITY").
			With("command", cfg.Name).
			With("index", i).
			Wrap(err)
	}
}
```

Update `NewTestEntry` to accept `[]Capability` (it mirrors `CommandEntryConfig`).

- [ ] **Step 2: Fix compilation errors in types_test.go**

Update all test cases that use `Capabilities: []string{...}` to use `Capabilities: []Capability{...}`. For example:

```go
// Old:
Capabilities: []string{"rp:speak"},
// New:
Capabilities: []Capability{{Action: "emit", Resource: "stream", Scope: ScopeLocal}},
```

Update `GetCapabilities` assertions accordingly.

- [ ] **Step 3: Run tests (expect many other packages to fail)**

Run: `task test -- -count=1 ./internal/command/...`
Expected: `types_test.go` passes, but other tests in this package will fail (dispatcher_test, access_test, etc. still use old format). That's expected — we'll fix them in subsequent tasks.

- [ ] **Step 4: Commit**

```text
refactor(command): migrate Capabilities from []string to []Capability

Breaks downstream consumers — fixed in subsequent commits.
```

---

## Task 3: Add `CanPerformAction` to Engine Interface and Implement

**Files:**

- Modify: `internal/access/policy/types/types.go`
- Modify: `internal/access/policy/engine.go`
- Modify: `internal/access/policy/engine_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/access/policy/engine_test.go`, add:

```go
func TestEngine_CanPerformAction_AdminPermitted(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["write"], resource is location) when { "admin" in principal.character.roles };`,
	}
	engine := createTestEngineWithPolicies(t, dslTexts, testProviders())

	allowed, err := engine.CanPerformAction(context.Background(),
		"character:"+testCharID.String(), "write", "location", "")
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestEngine_CanPerformAction_NoMatchingPolicy(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["read"], resource is location);`,
	}
	engine := createTestEngineWithPolicies(t, dslTexts, testProviders())

	// "write" action has no matching policy
	allowed, err := engine.CanPerformAction(context.Background(),
		"character:"+testCharID.String(), "write", "location", "")
	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestEngine_CanPerformAction_ForbidOverridesPermit(t *testing.T) {
	dslTexts := []string{
		`permit(principal is character, action in ["write"], resource is location);`,
		`forbid(principal is character, action in ["write"], resource is location) when { "banned" in principal.character.roles };`,
	}
	engine := createTestEngineWithPolicies(t, dslTexts, testProviders())

	// Character with "banned" role should be denied
	allowed, err := engine.CanPerformAction(context.Background(),
		"character:"+bannedCharID.String(), "write", "location", "")
	require.NoError(t, err)
	assert.False(t, allowed)
}
```

Note: Use the existing test helpers (`createTestEngineWithPolicies`, `testProviders`) — check the test file to see what's available. If `bannedCharID` doesn't exist, create a test fixture with the "banned" role.

- [ ] **Step 2: Add `CanPerformAction` to interface**

In `internal/access/policy/types/types.go`, add to `AccessPolicyEngine`:

```go
type AccessPolicyEngine interface {
	Evaluate(ctx context.Context, request AccessRequest) (Decision, error)
	CanPerformAction(ctx context.Context, subject, action, resourceType, scope string) (bool, error)
}
```

- [ ] **Step 3: Implement `CanPerformAction`**

In `internal/access/policy/engine.go`, add:

```go
// CanPerformAction checks whether the subject could potentially perform
// the given action on the given resource type. This is a type-level
// pre-flight check — no specific resource instance is required.
//
// Returns true if any policy would potentially permit the action.
// Returns false if an unconditional forbid matches or no policies apply.
func (e *Engine) CanPerformAction(ctx context.Context, subject, action, resourceType, scope string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, oops.Wrapf(err, "context cancelled")
	}

	if e.degraded.Load() {
		return false, nil // fail-closed in degraded mode
	}

	// Validate subject format
	parts := strings.SplitN(subject, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false, oops.Code("INVALID_SUBJECT").
			Errorf("subject must be 'type:id' format, got %q", subject)
	}

	// Resolve subject attributes only (no resource to resolve)
	subjectBag := attribute.NewBag()
	if e.resolver != nil {
		// Build a synthetic request for subject resolution
		syntheticReq, _ := types.NewAccessRequest(subject, action, resourceType+":__preflight__")
		bags, resolveErr := e.resolver.Resolve(ctx, syntheticReq)
		if resolveErr != nil {
			// Subject resolution failed — fail-closed
			return false, nil
		}
		subjectBag = bags.Subject
	}

	// Get cached policies
	policies := e.cache.Policies()

	// Find applicable policies by action and resource type
	subjectType := parts[0]
	hasForbid := false
	hasPermit := false

	for _, p := range policies {
		target := p.Compiled.Target

		// Match principal type
		if target.PrincipalType != nil && *target.PrincipalType != subjectType {
			continue
		}

		// Match action
		if len(target.Actions) > 0 {
			found := false
			for _, a := range target.Actions {
				if a == action {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Match resource type
		if target.ResourceType != nil && *target.ResourceType != resourceType {
			continue
		}

		// Policy target matches. Now evaluate conditions using subject attributes.
		// If conditions reference resource attributes, we treat it as optimistic
		// (potentially matching) since we have no resource instance.
		condResult := evaluateSubjectOnlyConditions(p.Compiled, subjectBag)

		if p.Compiled.Effect == "forbid" && condResult != condFalse {
			hasForbid = true
		}
		if p.Compiled.Effect == "permit" && condResult != condFalse {
			hasPermit = true
		}
	}

	// Forbid takes precedence
	if hasForbid {
		return false, nil
	}
	return hasPermit, nil
}
```

The `evaluateSubjectOnlyConditions` helper evaluates condition blocks, returning:

- `condTrue` if all conditions evaluate to true using only subject attributes
- `condFalse` if any condition evaluates to false
- `condUnknown` if conditions reference resource attributes (optimistic)

This is a simplified version of the full condition evaluator that skips resource attribute lookups.

Note: The exact implementation depends on how the DSL evaluator works. Read `internal/access/policy/dsl/evaluator.go` to understand the condition evaluation model. You may need to create a helper that wraps `EvaluateConditions` with a bag that returns "unknown" for resource attributes.

- [ ] **Step 4: Update all implementations of `AccessPolicyEngine`**

Search for all types that implement this interface (mock implementations in tests, `policytest.AllowAllEngine`, etc.) and add `CanPerformAction` stubs.

- [ ] **Step 5: Run tests**

Run: `task test -- -count=1 ./internal/access/policy/...`
Expected: PASS

- [ ] **Step 6: Commit**

```text
feat(access): add CanPerformAction for type-level capability pre-flight

New method on AccessPolicyEngine evaluates whether a subject could
potentially perform an action on a resource type without requiring a
specific resource instance.
```

---

## Task 4: Rewrite Dispatcher Authorization

**Files:**

- Modify: `internal/command/access.go`
- Modify: `internal/command/dispatcher.go`
- Modify: `internal/command/errors.go`
- Modify: `internal/command/access_test.go`

- [ ] **Step 1: Add `ErrInsufficientCapability` error**

In `internal/command/errors.go`, add:

```go
// ErrInsufficientCapability creates an error for capability pre-flight failures.
func ErrInsufficientCapability(cmdName string, cap Capability) error {
	return oops.Code(CodePermissionDenied).
		With("command", cmdName).
		With("required_action", cap.Action).
		With("required_resource", cap.Resource).
		With("required_scope", cap.EffectiveScope()).
		Errorf("insufficient capability: command %s requires %s on %s", cmdName, cap.Action, cap.Resource)
}
```

- [ ] **Step 2: Replace `CheckCapability` with two-layer functions**

In `internal/command/access.go`, replace the entire `CheckCapability` function with:

```go
// CheckCommandExecution evaluates Layer 1: can the subject execute this command?
// Uses the standard ABAC engine with resource format "command:<name>".
func CheckCommandExecution(ctx context.Context, engine types.AccessPolicyEngine, subject, cmdName string) error {
	req, reqErr := types.NewAccessRequest(subject, "execute", "command:"+cmdName)
	if reqErr != nil {
		errutil.LogErrorContext(ctx, cmdName+" command access request failed",
			reqErr, "subject", subject, "command", cmdName)
		observability.RecordEngineFailure(cmdName + "_command_access")
		return oops.Code(CodeAccessEvaluationFailed).
			With("command", cmdName).
			Wrap(errors.Join(ErrCapabilityCheckFailed, reqErr))
	}

	decision, evalErr := engine.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, cmdName+" command access evaluation failed",
			evalErr, "subject", subject, "command", cmdName)
		observability.RecordEngineFailure(cmdName + "_command_access")
		return oops.Code(CodeAccessEvaluationFailed).
			With("command", cmdName).
			Wrap(errors.Join(ErrCapabilityCheckFailed, evalErr))
	}

	if !decision.IsAllowed() {
		slog.DebugContext(ctx, cmdName+" command execution denied",
			"subject", subject,
			"reason", decision.Reason(),
			"policy_id", decision.PolicyID())
		return ErrPermissionDenied(cmdName, "execute")
	}
	return nil
}

// CheckCapabilityPreFlight evaluates Layer 2: does the subject have
// the class of permissions this command needs?
func CheckCapabilityPreFlight(ctx context.Context, engine types.AccessPolicyEngine, subject, cmdName string, caps []Capability) error {
	for _, cap := range caps {
		allowed, err := engine.CanPerformAction(ctx, subject, cap.Action, cap.Resource, cap.EffectiveScope())
		if err != nil {
			errutil.LogErrorContext(ctx, cmdName+" capability pre-flight error",
				err, "subject", subject, "action", cap.Action, "resource", cap.Resource)
			return oops.Code(CodeAccessEvaluationFailed).
				With("command", cmdName).
				With("action", cap.Action).
				With("resource", cap.Resource).
				Wrap(err)
		}
		if !allowed {
			slog.DebugContext(ctx, cmdName+" capability pre-flight denied",
				"subject", subject,
				"action", cap.Action,
				"resource", cap.Resource,
				"scope", cap.EffectiveScope())
			return ErrInsufficientCapability(cmdName, cap)
		}
	}
	return nil
}
```

- [ ] **Step 3: Update dispatcher capability loop**

In `internal/command/dispatcher.go`, replace the capability checking block (lines ~191-207) with:

```go
	// Layer 1: Command execution check
	if execErr := CheckCommandExecution(ctx, d.engine, subject, parsed.Name); execErr != nil {
		oopsErr, ok := oops.AsOops(execErr)
		code, isStr := oopsErr.Code().(string)
		if ok && isStr && code == CodePermissionDenied {
			metrics.SetStatus(StatusPermissionDenied)
		} else {
			metrics.SetStatus(StatusEngineFailure)
			span.RecordError(execErr)
			span.SetStatus(codes.Error, execErr.Error())
		}
		return execErr
	}

	// Layer 2: Capability pre-flight
	if preflightErr := CheckCapabilityPreFlight(ctx, d.engine, subject, parsed.Name, entry.GetCapabilities()); preflightErr != nil {
		oopsErr, ok := oops.AsOops(preflightErr)
		code, isStr := oopsErr.Code().(string)
		if ok && isStr && code == CodePermissionDenied {
			metrics.SetStatus(StatusPermissionDenied)
		} else {
			metrics.SetStatus(StatusEngineFailure)
			span.RecordError(preflightErr)
			span.SetStatus(codes.Error, preflightErr.Error())
		}
		return preflightErr
	}
```

- [ ] **Step 4: Update access_test.go**

Rewrite tests to test `CheckCommandExecution` and `CheckCapabilityPreFlight` instead of `CheckCapability`.

- [ ] **Step 5: Run tests**

Run: `task test -- -count=1 ./internal/command/...`
Expected: access_test.go passes. Other test files may still fail (using old `[]string` format).

- [ ] **Step 6: Commit**

```text
feat(command): implement two-layer authorization in dispatcher

Layer 1: command execution check via engine.Evaluate("command:<name>")
Layer 2: capability pre-flight via engine.CanPerformAction for each
declared capability.
```

---

## Task 5: Update Core Command Registrations

**Files:**

- Modify: `internal/command/handlers/register.go`
- Modify: `internal/command/handlers/register_test.go`

- [ ] **Step 1: Update shutdown and resetpassword**

In `register.go`:

```go
// shutdown
mustRegister(command.CommandEntryConfig{
	Name:    "shutdown",
	Handler: ShutdownHandler,
	Capabilities: []command.Capability{
		{Action: "admin", Resource: "server", Scope: command.ScopeGlobal},
	},
	// ... rest unchanged
})

// resetpassword
mustRegister(command.CommandEntryConfig{
	Name:    "resetpassword",
	Handler: NewResetPasswordHandler(deps),
	Capabilities: []command.Capability{
		{Action: "write", Resource: "player", Scope: command.ScopeGlobal},
	},
	// ... rest unchanged
})
```

Also update the help text references from `` `admin:shutdown` `` to describe the new capability format.

- [ ] **Step 2: Update test assertions**

In `register_test.go`, update capability assertions to match new `[]Capability` type.

- [ ] **Step 3: Run tests**

Run: `task test -- -count=1 ./internal/command/handlers/...`
Expected: PASS

- [ ] **Step 4: Commit**

```text
feat(command): update core command registrations with structured capabilities
```

---

## Task 6: Update Plugin YAML Manifests

**Files:**

- Modify: `plugins/core-communication/plugin.yaml`
- Modify: `plugins/core-building/plugin.yaml`
- Modify: `plugins/core-aliases/plugin.yaml`
- Modify: `plugins/core-objects/plugin.yaml`
- Modify: `plugins/core-help/plugin.yaml` (verify — may need no change)

- [ ] **Step 1: Update core-communication**

```yaml
# say: no capabilities needed (execution policy handles it)
# pose: no capabilities needed
# page:
  capabilities:
    - action: emit
      resource: stream
      scope: local
# whisper:
  capabilities:
    - action: emit
      resource: stream
      scope: local
# pemit:
  capabilities:
    - action: emit
      resource: stream
      scope: global
# emit:
  capabilities:
    - action: emit
      resource: stream
      scope: local
# wall:
  capabilities:
    - action: emit
      resource: stream
      scope: global
```

- [ ] **Step 2: Update core-building**

```yaml
# dig:
  capabilities:
    - action: write
      resource: location
      scope: local
    - action: write
      resource: exit
      scope: local
# link:
  capabilities:
    - action: write
      resource: exit
      scope: local
```

- [ ] **Step 3: Update core-aliases**

```yaml
# alias/unalias/aliases (player):
  capabilities:
    - action: write  # or read for aliases
      resource: alias
# sysalias/sysunsalias/sysaliases (admin):
  capabilities:
    - action: write  # or read for sysaliases
      resource: alias
      scope: global
```

- [ ] **Step 4: Update core-objects**

```yaml
# create:
  capabilities:
    - action: write
      resource: object
      scope: local
# set:
  capabilities:
    - action: write
      resource: property
      scope: local
```

- [ ] **Step 5: Update manifest loader**

In `internal/plugin/manifest.go`, change `CommandSpec.Capabilities` from `[]string` to `[]Capability`:

```go
type CommandSpec struct {
	Name         string       `yaml:"name" json:"name" jsonschema:"required,minLength=1"`
	Capabilities []Capability `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	// ... rest unchanged
}
```

Import `command` package for the `Capability` type — or define a parallel `Capability` type in the manifest package if there's a circular dependency risk. Check the import graph.

Add validation in `CommandSpec.Validate()`:

```go
for i, cap := range c.Capabilities {
	if err := cap.Validate(); err != nil {
		return oops.In("command").With("name", c.Name).With("capability_index", i).Wrap(err)
	}
}
```

- [ ] **Step 6: Update manager.go**

In `internal/plugin/manager.go`, the line that passes capabilities:

```go
Capabilities: cmdSpec.Capabilities, // already []Capability now
```

- [ ] **Step 7: Run tests**

Run: `task test -- -count=1 ./internal/plugin/...`
Expected: manifest tests fail (they use old string format). Fix in Task 7.

- [ ] **Step 8: Commit**

```text
feat(plugin): update all plugin manifests to structured capabilities

All five core plugins now use Capability{Action, Resource, Scope}
format instead of dotted/colon string capabilities.
```

---

## Task 7: Update All Test Files

**Files:**

- Modify: `internal/command/dispatcher_test.go`
- Modify: `internal/command/registry_test.go`
- Modify: `internal/command/errors_test.go`
- Modify: `internal/command/plugin_dispatch_test.go`
- Modify: `internal/plugin/manifest_test.go`
- Modify: `internal/plugin/hostfunc/commands_test.go`
- Modify: `internal/plugin/help_integration_test.go`
- Modify: `internal/plugin/communication_integration_test.go`
- Modify: `internal/plugin/service_proxy_impl_test.go`
- Modify: `test/integration/telnet/e2e_test.go`

- [ ] **Step 1: Update dispatcher_test.go**

Replace all `capabilities: []string{"admin:manage"}` with structured capabilities. Replace all `Grant(subject, "execute", "admin:manage")` calls with appropriate ABAC grants for the new check flow.

The dispatcher now does two checks:

1. `CheckCommandExecution` → needs `command:<name>` to be permitted
2. `CheckCapabilityPreFlight` → needs `CanPerformAction` to return true

Test helpers that set up `StaticAccessControl` grants need to grant `execute` on `command:<name>` for Layer 1. Layer 2 uses `CanPerformAction` which checks policies.

- [ ] **Step 2: Update hostfunc/commands_test.go**

Replace all `Capabilities: []string{"comms.say"}` with `Capabilities: []command.Capability{{Action: "emit", Resource: "stream", Scope: command.ScopeLocal}}`.

Replace all `Grant(subject, "execute", "comms.say")` with grants appropriate for the new model.

- [ ] **Step 3: Update remaining test files**

Apply the same pattern to all other test files listed above.

- [ ] **Step 4: Run full test suite**

Run: `task test`
Expected: ALL tests pass.

- [ ] **Step 5: Commit**

```text
test: update all test files for structured capabilities

Migrates all test capability references from string format to
Capability{Action, Resource, Scope} objects.
```

---

## Task 8: Cleanup and Documentation

**Files:**

- Modify: `docs/adr/0007-command-security-model.md`
- Modify: `site/docs/reference/access-control.md`

- [ ] **Step 1: Add supersession notice to ADR 0007**

Add to the top of `docs/adr/0007-command-security-model.md`:

```markdown
> **Superseded:** This ADR's capability-namespace model was fully replaced
> by structured capabilities (see spec: `docs/superpowers/specs/2026-04-03-command-capability-enforcement-design.md`).
> Commands now declare capabilities as `{action, resource, scope}` objects
> validated against the ABAC policy engine at dispatch time.
```

- [ ] **Step 2: Update access-control reference**

Update `site/docs/reference/access-control.md` to document the new capability format and two-layer authorization model.

- [ ] **Step 3: Run lint**

Run: `task lint`
Expected: PASS

- [ ] **Step 4: Run full test suite**

Run: `task test`
Expected: PASS

- [ ] **Step 5: Commit**

```text
docs: update ADR 0007 supersession notice and access-control reference
```

---

## Task 9: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `task test`
Expected: PASS, zero failures.

- [ ] **Step 2: Run lint**

Run: `task lint`
Expected: PASS

- [ ] **Step 3: Run build**

Run: `task build`
Expected: Compiles.

- [ ] **Step 4: Run pr-prep**

Run: `task pr-prep`
Expected: All CI checks pass.

- [ ] **Step 5: Close beads**

```bash
bd close holomush-jh4l --reason "Fixed: command capabilities replaced with structured Capability{Action, Resource, Scope} objects and two-layer ABAC enforcement"
```
