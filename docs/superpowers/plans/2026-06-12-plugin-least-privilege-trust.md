<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Plugin Least-Privilege Enforcement & Plugin-Trust Security Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a plugin's capability grant fine-grained and policy-governed — a default-deny ABAC decision keyed on the host-vouched `plugin:<name>` subject, narrowed by `access:` (operation) and `scope:` (instance), and close the PR #4430 command-registry subject-spoofing finding symmetrically.

**Architecture:** One spine — a host-vouched `DispatchContext` stamped on the delivery `context.Context` — feeds four mechanisms. A host-owned `CapabilityDescriptor` table classifies every host.v1 method (action/resource/operation-class/scopes/extractor). One `hostcap` `grpc.UnaryServerInterceptor`, installed on both runtimes' host.v1 servers, runs static checks (declaration + `access:` class) then a per-call `pluginauthz` capability-access `Evaluate` (operator policy + `scope:` condition). The command-registry RPCs and their Lua twins switch from wire `character_id` to `DispatchContext.Subject`.

**Tech Stack:** Go, gRPC (interceptors + bufconn), gopher-lua, the existing `pluginauthz` ABAC core, the foundation resolver/`CapabilityVocabulary`, `invariants.yaml` + `cmd/inv-render`.

**Spec:** `docs/superpowers/specs/2026-06-12-plugin-least-privilege-trust-design.md`

**Conventions every task follows:**

- Tests before implementation (TDD). Run `task test -- ./internal/plugin/...` (or the named package) per change; `task test:int` after any `plugin.yaml`/`requires` fixture change.
- Errors via `oops` with a `Code(...)`; logs via `*Context` slog variants / `errutil.LogErrorContext`.
- Never bare `grep`; use `rg` / probe. Line-scoped `//nolint` only.
- Commit after each green task with a conventional-commit message ending in the `Co-Authored-By` byline.
- VCS is jj-colocated; commit per `references/vcs-preamble.md` (`jj commit -m "..."`).

---

## File structure

| File | Responsibility | Tasks |
| --- | --- | --- |
| `internal/plugin/dependency_type.go` | Add `Access` field to `Dependency` + `dependencyYAML` | 1 |
| `internal/plugin/manifest.go` | Validate `access:`/`scope:` capability-only (INV-PLUGIN-53); `RequiredCapabilities` already present | 2 |
| `schemas/plugin.schema.json` | Regenerated artifact (drift-gated) | 1 |
| `internal/plugin/hostcap/descriptor.go` | **New** — `CapabilityDescriptor` table (host-owned, per-method) | 3, 4 |
| `internal/plugin/pluginauthz/dispatch.go` | **New** — `DispatchContext` + `WithDispatch`/`DispatchForHost` | 5 |
| `internal/plugin/goplugin/host.go`, `internal/plugin/lua/host.go` | Stamp `DispatchContext` on the host-level delivery ctx (where the actor is on ctx) | 6 |
| `internal/plugin/pluginauthz/capability.go` | **New** — `EvaluateCapabilityAccess` sibling path (no OwnedTypes gate) | 7 |
| `internal/plugin/hostcap/interceptor.go` | **New** — the one capability interceptor (declaration + `access:` + policy + `scope:`) | 8, 9, 10 |
| `internal/plugin/goplugin/host_service.go`, `internal/plugin/lua/bufconn_endpoint.go` | Install the interceptor on both runtimes' host.v1 servers (the two `grpc.NewServer` + `hostcap.RegisterCapabilities` sites) | 9 |
| `internal/plugin/hostcap/descriptor_completeness_test.go` | **New** — scope-extractor completeness meta-test (INV-PLUGIN-52) | 11 |
| `internal/plugin/hostcap/servers.go` | `ListCommands`/`GetCommandHelp` use `DispatchContext.Subject` | 12 |
| `internal/plugin/hostfunc/commands.go` | Lua `list_commands`/`get_command_help` use `DispatchContext.Subject` | 13 |
| `docs/architecture/invariants.yaml` + `invariants.md` | Register + bind INV-PLUGIN-50…53 | 14 |

---

## Phase 1: Manifest plumbing — `access:` field + capability-only validation

### Task 1: Add the `Access` field to `Dependency`

**Files:**

- Modify: `internal/plugin/dependency_type.go:24-32` (struct) + the `dependencyYAML` mirror in the same file
- Test: `internal/plugin/dependency_type_test.go`

- [ ] **Step 1: Write the failing test** — round-trips `access:` through YAML.

```go
func TestDependencyAccessRoundTrips(t *testing.T) {
	var d Dependency
	err := yaml.Unmarshal([]byte("capability: kv\naccess: read\n"), &d)
	require.NoError(t, err)
	assert.Equal(t, KindCapability, d.Kind)
	assert.Equal(t, "kv", d.Name)
	assert.Equal(t, "read", d.Access)
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `task test -- -run TestDependencyAccessRoundTrips ./internal/plugin/`
Expected: FAIL — `d.Access undefined`.

- [ ] **Step 3: Add the field** to `Dependency` and its YAML mirror.

```go
type Dependency struct {
	Kind     DependencyKind
	Name     string
	Version  string // semver constraint; services only
	Optional bool
	// Scope and Access carry least-privilege parameters; semantics are sub-spec 4
	// (this work). Valid only on capability entries (INV-PLUGIN-53).
	Scope  string
	Access string // "" | "read" | "write"
}
```

Add `Access string \`yaml:"access,omitempty"\`` to the `dependencyYAML` struct and copy it in the `UnmarshalYAML` mapping alongside `Scope`.

- [ ] **Step 4: Run it, verify it passes**

Run: `task test -- -run TestDependencyAccessRoundTrips ./internal/plugin/`
Expected: PASS.

- [ ] **Step 5: Regenerate the JSON schema + commit**

Run: `go generate ./internal/plugin/...` then `task lint` (drift gate). Confirm `schemas/plugin.schema.json` updated.
Commit: `feat(plugin): add access field to manifest Dependency (holomush-eykuh.3)`.

### Task 2: Validate `access:`/`scope:` are capability-only and well-formed (INV-PLUGIN-53)

**Files:**

- Modify: `internal/plugin/manifest.go` — extend `Validate()` (line 419) with a per-`Requires`-entry check
- Test: `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write the failing tests** — service entry with `scope:`/`access:` errors; bad `access:` enum errors; capability entry with `access: read` validates.

```go
func TestManifestRejectsLeastPrivilegeParamsOnServiceEntry(t *testing.T) {
	for _, tc := range []struct{ name, yamlFrag, code string }{
		{"scope on service", "requires:\n  - service: holomush.scene.v1.SceneService\n    scope: own-location\n", "LEAST_PRIVILEGE_PARAM_ON_SERVICE"},
		{"access on service", "requires:\n  - service: holomush.scene.v1.SceneService\n    access: read\n", "LEAST_PRIVILEGE_PARAM_ON_SERVICE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := validBaseManifest(t, tc.yamlFrag) // helper: name/version/type + frag
			errutil.AssertErrorCode(t, m.Validate(), tc.code)
		})
	}
}

func TestManifestRejectsUnknownAccessValue(t *testing.T) {
	m := validBaseManifest(t, "requires:\n  - capability: kv\n    access: delete\n")
	errutil.AssertErrorCode(t, m.Validate(), "INVALID_ACCESS_VALUE")
}

func TestManifestAcceptsAccessReadOnCapability(t *testing.T) {
	m := validBaseManifest(t, "requires:\n  - capability: kv\n    access: read\n")
	require.NoError(t, m.Validate())
}
```

- [ ] **Step 2: Run them, verify they fail**

Run: `task test -- -run 'TestManifestRejectsLeastPrivilege|TestManifestRejectsUnknownAccess|TestManifestAcceptsAccessRead' ./internal/plugin/`
Expected: FAIL — no such validation yet.

- [ ] **Step 3: Add validation** inside `Validate()`, iterating `m.Requires`.

```go
var validAccess = map[string]bool{"": true, "read": true, "write": true}

for _, d := range m.Requires {
	if d.Kind == KindService && (d.Scope != "" || d.Access != "") {
		return oops.Code("LEAST_PRIVILEGE_PARAM_ON_SERVICE").
			With("plugin", m.Name).With("service", d.Name).
			Errorf("access:/scope: are valid only on capability entries (INV-PLUGIN-53)")
	}
	if !validAccess[d.Access] {
		return oops.Code("INVALID_ACCESS_VALUE").
			With("plugin", m.Name).With("access", d.Access).
			Errorf("access must be one of: read, write")
	}
}
```

(`scope:` token-vs-vocabulary validation is Task 4, once the descriptor exists.)

- [ ] **Step 4: Run them, verify they pass**

Run: `task test -- -run 'TestManifestRejectsLeastPrivilege|TestManifestRejectsUnknownAccess|TestManifestAcceptsAccessRead' ./internal/plugin/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): validate access/scope capability-only (INV-PLUGIN-53, holomush-eykuh.3)`.

---

## Phase 2: The `CapabilityDescriptor` table

### Task 3: Define the descriptor types + the table for read-only capabilities

**Files:**

- Create: `internal/plugin/hostcap/descriptor.go`
- Test: `internal/plugin/hostcap/descriptor_test.go`

- [ ] **Step 1: Write the failing test** — the table classifies a known method.

```go
func TestDescriptorClassifiesEvalMethods(t *testing.T) {
	d, ok := Descriptors["eval"]
	require.True(t, ok, "eval capability has a descriptor")
	m, ok := d.Methods["Evaluate"]
	require.True(t, ok)
	assert.Equal(t, ClassRead, m.Class)
	assert.Empty(t, m.Scopes, "eval is not scope-eligible")
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `task test -- -run TestDescriptorClassifiesEvalMethods ./internal/plugin/hostcap/`
Expected: FAIL — `Descriptors undefined`.

- [ ] **Step 3: Define the types + the read-only slice of the table.**

```go
// OperationClass is the read/write class of a host.v1 method (M2).
type OperationClass int

const (
	ClassRead OperationClass = iota
	ClassWrite
)

// ScopedResourceFn extracts the ABAC resource id a request touches, for the
// scope condition (M3). Returns "" when no resource is in play.
type ScopedResourceFn func(req any) (resourceID string, ok bool)

// MethodDescriptor is the host-owned per-method classification.
type MethodDescriptor struct {
	Action   string           // ABAC action, e.g. "write"
	Resource string           // ABAC resource type, e.g. "location"
	Class    OperationClass   // read | write (M2)
	Scopes   []string         // supported scope tokens (M3); empty => not scope-eligible
	Extract  ScopedResourceFn // required iff len(Scopes) > 0 (M3, INV-PLUGIN-52)
}

// CapabilityDescriptor is the host-owned table for one capability token.
type CapabilityDescriptor struct {
	Token   string
	Methods map[string]MethodDescriptor
}

// Descriptors is the single host-owned source for M1/M2/M3 per-method metadata,
// keyed by capability token. It is the per-method companion to the sub-spec-2
// token->service registry.
var Descriptors = map[string]CapabilityDescriptor{
	"eval": {Token: "eval", Methods: map[string]MethodDescriptor{
		"Evaluate": {Action: "evaluate", Resource: "policy", Class: ClassRead},
	}},
	"settings": {Token: "settings", Methods: map[string]MethodDescriptor{
		"GetSetting": {Action: "read", Resource: "setting", Class: ClassRead},
		"SetSetting": {Action: "write", Resource: "setting", Class: ClassWrite},
	}},
	"kv": {Token: "kv", Methods: map[string]MethodDescriptor{
		"KVGet":    {Action: "read", Resource: "kv", Class: ClassRead},
		"KVSet":    {Action: "write", Resource: "kv", Class: ClassWrite},
		"KVDelete": {Action: "write", Resource: "kv", Class: ClassWrite},
	}},
	"command-registry": {Token: "command-registry", Methods: map[string]MethodDescriptor{
		"ListCommands":   {Action: "list", Resource: "command", Class: ClassRead},
		"GetCommandHelp": {Action: "read", Resource: "command", Class: ClassRead},
	}},
}
```

> The `Action`/`Resource` values must match the ABAC vocabulary the host
> capabilities already use; confirm against `internal/access` action/resource
> constants and the existing `host.Evaluate` usage before finalizing each row.
> Add the remaining capability rows (focus, emit, audit, stream-history,
> stream-subscription, session, property, world, log) in Task 4 alongside the
> scope-eligible entries.

- [ ] **Step 4: Run it, verify it passes**

Run: `task test -- -run TestDescriptorClassifiesEvalMethods ./internal/plugin/hostcap/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): host-owned CapabilityDescriptor table (holomush-eykuh.3)`.

### Task 4: Add scope-eligible rows + per-capability scope vocabulary; wire `scope:` manifest validation

**Files:**

- Modify: `internal/plugin/hostcap/descriptor.go` (add world/session/property rows with `Scopes`+`Extract`)
- Modify: `internal/plugin/manifest.go` (`Validate()` — `scope:` token must be in the capability's descriptor)
- Modify: `internal/plugin/hostcap/descriptor_test.go`, `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write the failing tests** — a scope-eligible method has an extractor; an unknown scope token on a capability is a manifest error.

```go
func TestWorldMutationIsScopeEligibleWithExtractor(t *testing.T) {
	m := Descriptors["world.mutation"].Methods["CreateLocation"] // adjust to a real mutation method
	assert.Equal(t, ClassWrite, m.Class)
	assert.Contains(t, m.Scopes, "own-location")
	require.NotNil(t, m.Extract, "scope-eligible method must carry an extractor")
}

func TestManifestRejectsUnknownScopeToken(t *testing.T) {
	m := validBaseManifest(t, "requires:\n  - capability: world.mutation\n    scope: own-galaxy\n")
	errutil.AssertErrorCode(t, m.Validate(), "UNKNOWN_SCOPE_TOKEN")
}
```

- [ ] **Step 2: Run them, verify they fail**

Run: `task test -- -run 'TestWorldMutationIsScopeEligible|TestManifestRejectsUnknownScopeToken' ./internal/plugin/...`
Expected: FAIL.

- [ ] **Step 3a: Add the scope-eligible descriptor rows** (real method/field names from `api/proto/holomush/plugin/host/v1/*.proto` — confirm via probe).

```go
"world.mutation": {Token: "world.mutation", Methods: map[string]MethodDescriptor{
	"CreateLocation": {
		Action: "write", Resource: "location", Class: ClassWrite,
		Scopes:  []string{"own-location"},
		Extract: func(req any) (string, bool) {
			r, ok := req.(*hostv1.CreateLocationRequest)
			if !ok { return "", false }
			return r.GetLocationId(), r.GetLocationId() != ""
		},
	},
	// ... other world.mutation methods, each with its typed Extract
}},
```

- [ ] **Step 3b: Add a descriptor lookup helper** for manifest validation (the manifest package must not import `hostcap` if that creates a cycle — expose the scope vocabulary as a small map the manifest validator can consult, e.g. a `ScopeTokens(token string) map[string]bool` function in a leaf package, or pass the vocabulary into `Validate`). Confirm import direction first via `go list -deps`; if `hostcap`→`plugin` would cycle, site the scope-token map in the existing `capability_vocab.go` and have `hostcap` register into it at init.

- [ ] **Step 3c: Add `scope:` token validation** in `Validate()`:

```go
if d.Kind == KindCapability && d.Scope != "" {
	if !capabilityScopeTokens(d.Name)[d.Scope] {
		return oops.Code("UNKNOWN_SCOPE_TOKEN").
			With("plugin", m.Name).With("capability", d.Name).With("scope", d.Scope).
			Errorf("capability %q does not support scope %q", d.Name, d.Scope)
	}
}
```

- [ ] **Step 4: Run them, verify they pass**

Run: `task test -- -run 'TestWorldMutationIsScopeEligible|TestManifestRejectsUnknownScopeToken' ./internal/plugin/...`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): scope-eligible descriptor rows + scope-token validation (holomush-eykuh.3)`.

---

## Phase 3: P0 — the dispatch-context primitive

### Task 5: `DispatchContext` type + context-key accessors

**Files:**

- Create: `internal/plugin/pluginauthz/dispatch.go`
- Test: `internal/plugin/pluginauthz/dispatch_test.go`

- [ ] **Step 1: Write the failing test** — round-trip + absence is fail-closed (no value).

```go
func TestDispatchContextRoundTrips(t *testing.T) {
	dc := DispatchContext{Subject: "character:01ABC", Attributes: map[string]string{"location": "01LOC"}}
	ctx := WithDispatch(context.Background(), dc)
	got, ok := DispatchForHost(ctx)
	require.True(t, ok)
	assert.Equal(t, dc.Subject, got.Subject)
	assert.Equal(t, "01LOC", got.Attributes["location"])
}

func TestDispatchAbsentByDefault(t *testing.T) {
	_, ok := DispatchForHost(context.Background())
	assert.False(t, ok, "absent dispatch context must be detectable for fail-closed")
}
```

- [ ] **Step 2: Run them, verify they fail**

Run: `task test -- -run 'TestDispatchContext|TestDispatchAbsent' ./internal/plugin/pluginauthz/`
Expected: FAIL.

- [ ] **Step 3: Implement.** (the key is unexported; `WithDispatch` is host-only and `DispatchForHost` is the host-side reader, so a plugin can neither set nor observe it.)

```go
type DispatchContext struct {
	Subject    string            // host-vouched ABAC subject (access.CharacterSubject)
	Attributes map[string]string // host-resolved acting-character attributes (location, ...)
}

type dispatchKey struct{}

// WithDispatch stamps the host-vouched dispatch context. Host-only; called from
// DeliverCommand/DeliverEvent before any plugin code runs (INV-PLUGIN-51).
func WithDispatch(ctx context.Context, dc DispatchContext) context.Context {
	return context.WithValue(ctx, dispatchKey{}, dc)
}

// DispatchForHost reads the dispatch context. Exported for the host-side
// readers in other packages (hostcap interceptor, command-registry servers,
// Lua hostfuncs). Plugins are not Go callers of this package and cannot set
// the key (only WithDispatch does), so exporting the reader is safe.
func DispatchForHost(ctx context.Context) (DispatchContext, bool) {
	dc, ok := ctx.Value(dispatchKey{}).(DispatchContext)
	return dc, ok
}
```

`DispatchForHost` is the single exported host-side reader; all later tasks (interceptor, command-registry servers, Lua hostfuncs) call it.

- [ ] **Step 4: Run them, verify they pass**

Run: `task test -- -run 'TestDispatchContext|TestDispatchAbsent' ./internal/plugin/pluginauthz/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): DispatchContext primitive in pluginauthz (P0, holomush-eykuh.3)`.

### Task 6: Stamp `DispatchContext` on the delivery context

**Files:**

- Modify: `internal/plugin/goplugin/host.go` — in `DeliverEvent` (941) and `DeliverCommand` (1020), at the point each already calls `core.ActorFromContext(ctx)` (976 / 1047), stamp the dispatch context onto `ctx` before plugin code runs
- Modify: `internal/plugin/lua/host.go` — `DeliverEvent` (390) / `DeliverCommand` (484): same stamp at the Lua host's actor-read point, so the in-VM hostfuncs inherit it on `ls.Context()`
- Test: `internal/plugin/goplugin/host_test.go`, `internal/plugin/lua/host_test.go`

> NOTE: `manager.go`'s `DeliverCommand`/`DeliverEvent` (313/458) are thin
> delegation wrappers that do **not** read the actor; the stamp belongs at the
> two host-level delivery points above, where `core.ActorFromContext` is already
> consulted. A shared helper `pluginauthz.WithDispatch` keeps both identical.

- [ ] **Step 1: Write the failing test** — delivering with an acting character makes the dispatch subject visible to a capability call.

```go
func TestDeliverStampsDispatchSubjectFromActor(t *testing.T) {
	charID := ulid.Make()
	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID.String()})
	// Drive a delivery that reaches a capability and capture the ctx seen there;
	// assert pluginauthz.DispatchForHost(ctx).Subject == access.CharacterSubject(charID.String()).
	// DispatchForHost is the exported host-side reader from Task 5.
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `task test -- -run TestDeliverStampsDispatchSubject ./internal/plugin/`
Expected: FAIL — dispatch not stamped.

- [ ] **Step 3: Stamp in the manager.** Build the `DispatchContext` from the acting actor (subject eager; attributes resolved via the character provider) and thread it.

```go
// inside DeliverCommand / DeliverEvent, once the acting core.Actor is known:
if actor, ok := core.ActorFromContext(ctx); ok && actor.Kind == core.ActorCharacter && actor.ID != "" {
	subject := access.CharacterSubject(actor.ID)
	attrs, err := m.dispatchAttrs(ctx, subject) // resolves via CharacterProvider.ResolveSubject; "location" etc.
	if err != nil {
		errutil.LogErrorContext(ctx, "resolve dispatch attributes failed", err, "subject", subject)
		attrs = nil // fail-closed at scope time, not here
	}
	ctx = pluginauthz.WithDispatch(ctx, pluginauthz.DispatchContext{Subject: subject, Attributes: attrs})
}
```

Add `dispatchAttrs` as a thin adapter over the character attribute provider already wired into the manager (string-map projection of `ResolveSubject`’s `map[string]any`, keeping only string-valued keys like `location`).

- [ ] **Step 4: Run it, verify it passes**

Run: `task test -- -run TestDeliverStampsDispatchSubject ./internal/plugin/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): stamp DispatchContext on delivery ctx (P0, holomush-eykuh.3)`.

---

## Phase 4: M1 — the capability-access entitlement

### Task 7: `EvaluateCapabilityAccess` sibling path (no OwnedTypes gate)

**Files:**

- Create: `internal/plugin/pluginauthz/capability.go`
- Test: `internal/plugin/pluginauthz/capability_test.go`

- [ ] **Step 1: Write the failing tests** — declared+permitted allows; declared+policy-deny denies; the OwnedTypes gate is NOT applied (a host resource type the plugin does not own still evaluates).

```go
func TestEvaluateCapabilityAccessAllowsDeclaredPermitted(t *testing.T) {
	dec, err := EvaluateCapabilityAccess(context.Background(), CapabilityInput{
		Engine: policytest.AllowAllEngine(), PluginName: "core-objects",
		Subject: access.PluginSubject("core-objects"),
		Action:  "read", Resource: "kv:foo", Declared: true,
	})
	require.NoError(t, err)
	assert.True(t, dec.Allowed)
}

func TestEvaluateCapabilityAccessDeniedByPolicyDespiteDeclaration(t *testing.T) {
	dec, _ := EvaluateCapabilityAccess(context.Background(), CapabilityInput{
		Engine: policytest.DenyAllEngine(), PluginName: "core-objects",
		Subject: access.PluginSubject("core-objects"),
		Action:  "read", Resource: "kv:foo", Declared: true,
	})
	assert.False(t, dec.Allowed) // INV-PLUGIN-50: declaration necessary, not sufficient
}

func TestEvaluateCapabilityAccessUndeclaredFailsClosed(t *testing.T) {
	_, err := EvaluateCapabilityAccess(context.Background(), CapabilityInput{
		Engine: policytest.AllowAllEngine(), PluginName: "core-objects",
		Subject: access.PluginSubject("core-objects"),
		Action:  "read", Resource: "kv:foo", Declared: false,
	})
	errutil.AssertErrorCode(t, err, "CAPABILITY_NOT_DECLARED")
}
```

- [ ] **Step 2: Run them, verify they fail**

Run: `task test -- -run TestEvaluateCapabilityAccess ./internal/plugin/pluginauthz/`
Expected: FAIL.

- [ ] **Step 3: Implement** the sibling path — shares subject/engine/audit with `Evaluate`, substitutes the declaration entitlement for the OwnedTypes gate.

```go
// CapabilityInput is the capability-access decision input. Subject is
// host-derived (plugin:<name>); Declared is the resolver-proven reach.
type CapabilityInput struct {
	Engine     types.AccessPolicyEngine
	Auditor    Auditor
	PluginName string
	Subject    string
	Action     string
	Resource   string
	Declared   bool
	Context    map[string]any // dispatch attributes for scope conditions (M3)
}

// EvaluateCapabilityAccess authorizes a plugin's consumption of a host
// capability. Entitlement is manifest-declaration (Declared), NOT OwnedTypes —
// host-capability resources are not plugin-owned (INV-PLUGIN-50). Shares the
// engine call + single audit event with Evaluate (INV-PLUGIN-26).
func EvaluateCapabilityAccess(ctx context.Context, in CapabilityInput) (Decision, error) {
	if in.Engine == nil {
		return Decision{}, oops.Code("EVALUATE_NO_ENGINE").With("plugin", in.PluginName).Errorf("nil engine")
	}
	if in.Subject == "" {
		return Decision{}, oops.Code("EVALUATE_NO_SUBJECT").With("plugin", in.PluginName).Errorf("no subject")
	}
	if !in.Declared {
		return Decision{}, oops.Code("CAPABILITY_NOT_DECLARED").
			With("plugin", in.PluginName).With("resource", in.Resource).
			Errorf("capability not declared by plugin")
	}
	req, err := types.NewAccessRequest(in.Subject, in.Action, in.Resource, in.Context)
	if err != nil {
		return Decision{}, oops.With("plugin", in.PluginName).Wrap(err)
	}
	dec, err := in.Engine.Evaluate(ctx, req)
	if err != nil {
		errutil.LogErrorContext(ctx, "capability-access engine error", err, "plugin", in.PluginName)
		return Decision{}, oops.With("plugin", in.PluginName).Wrap(err)
	}
	// ... map dec -> Decision{Allowed, MatchedPolicy}; emit one audit event via
	// in.Auditor (mirror Evaluate's audit block, Name "plugin.capability_access").
	return toDecision(dec), nil
}
```

- [ ] **Step 4: Run them, verify they pass**

Run: `task test -- -run TestEvaluateCapabilityAccess ./internal/plugin/pluginauthz/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): capability-access entitlement path (M1, INV-PLUGIN-50, holomush-eykuh.3)`.

---

## Phase 5–6: M2/M3 — the one capability interceptor

### Task 8: The interceptor — declaration + `access:` static checks

**Files:**

- Create: `internal/plugin/hostcap/interceptor.go`
- Test: `internal/plugin/hostcap/interceptor_test.go`

- [ ] **Step 1: Write the failing tests** — `access: read` denies a write method; absent declared `access:` permits both classes; an undeclared capability is denied.

```go
func TestInterceptorAccessReadDeniesWriteMethod(t *testing.T) {
	ic := NewCapabilityInterceptor(InterceptorDeps{
		Engine: policytest.AllowAllEngine(),
		DeclaredAccess: func(plugin, capToken string) (string, bool) { return "read", true },
	})
	_, err := ic(ctxWithDispatch(t), &hostv1.KVSetRequest{}, &grpc.UnaryServerInfo{
		FullMethod: "/holomush.plugin.host.v1.KVService/KVSet",
	}, okHandler)
	errutil.AssertErrorCode(t, err, "ACCESS_CLASS_DENIED")
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `task test -- -run TestInterceptorAccessReadDeniesWriteMethod ./internal/plugin/hostcap/`
Expected: FAIL.

- [ ] **Step 3: Implement the static half** — parse `FullMethod` → capability token + method, look up the descriptor, deny if declared `access:` does not cover the method `Class`. (Map the gRPC service name → capability token via the sub-spec-2 token↔service registry.)

```go
func NewCapabilityInterceptor(d InterceptorDeps) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		capToken, method, ok := d.classify(info.FullMethod) // service->token + method name
		if !ok { return h(ctx, req) }                       // not a gated host.v1 method
		md, ok := Descriptors[capToken].Methods[method]
		if !ok {
			return nil, oops.Code("UNCLASSIFIED_CAPABILITY_METHOD").With("method", info.FullMethod).
				Errorf("no descriptor entry") // fail-closed
		}
		declAccess, declared := d.DeclaredAccess(d.pluginName, capToken)
		if !declared {
			return nil, oops.Code("CAPABILITY_NOT_DECLARED").With("capability", capToken).Errorf("undeclared")
		}
		if declAccess == "read" && md.Class == ClassWrite {
			return nil, oops.Code("ACCESS_CLASS_DENIED").With("capability", capToken).With("method", method).
				Errorf("declared access: read does not cover write method")
		}
		// policy + scope half added in Task 10:
		return h(ctx, req)
	}
}
```

- [ ] **Step 4: Run it, verify it passes**

Run: `task test -- -run TestInterceptorAccessReadDeniesWriteMethod ./internal/plugin/hostcap/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): capability interceptor static checks (M2, holomush-eykuh.3)`.

### Task 9: Install the interceptor on both runtimes' servers

**Files:**

- Modify: `internal/plugin/goplugin/host_service.go:28-31` — `newPluginHostServiceServer` already calls `grpc.NewServer(opts...)` then `hostcap.RegisterCapabilities`; thread the interceptor into `opts` (this is the binary host.v1 server, **not** `broker_proxy.go`, which is the separate plugin→plugin proxy)
- Modify: `internal/plugin/lua/bufconn_endpoint.go` — the Lua per-plugin `grpc.NewServer` (`newPluginEndpoint`); thread the interceptor as a `ServerOption`
- Test: `internal/plugin/hostcap/interceptor_test.go` + a cross-runtime parity test

- [ ] **Step 1: Write the failing test** — both servers reject an undeclared capability identically (parity).

```go
func TestInterceptorInstalledOnBothRuntimes(t *testing.T) {
	// Stand up the binary broker server and the Lua bufconn server, each with a
	// plugin declaring NO capabilities; assert a KVGet call is denied
	// CAPABILITY_NOT_DECLARED on BOTH. (INV-PLUGIN-45/49 parity.)
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `task test:int -- -run TestInterceptorInstalledOnBothRuntimes ./internal/plugin/...`
Expected: FAIL — interceptor not installed.

- [ ] **Step 3: Thread the interceptor** as a `grpc.ChainUnaryInterceptor(ic)` `ServerOption` at both `grpc.NewServer` sites. Construct one `ic` per plugin (closes over `pluginName` + the plugin's resolved declared-capability/access set from the foundation resolver).

- [ ] **Step 4: Run it, verify it passes**

Run: `task test:int -- -run TestInterceptorInstalledOnBothRuntimes ./internal/plugin/...`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): install capability interceptor on both runtimes (INV-PLUGIN-45/49, holomush-eykuh.3)`.

### Task 10: The interceptor — policy + `scope:` (M1 policy + M3)

**Files:**

- Modify: `internal/plugin/hostcap/interceptor.go`
- Test: `internal/plugin/hostcap/interceptor_test.go`

- [ ] **Step 1: Write the failing tests** — operator deny blocks a declared cap; `scope: own-location` permits matching location, denies mismatch; absent dispatch fails a scoped call closed.

```go
func TestInterceptorScopeOwnLocationPermitsMatch(t *testing.T) { /* dispatch.location == resource location => allow */ }
func TestInterceptorScopeOwnLocationDeniesMismatch(t *testing.T) { /* differ => SCOPE_DENIED */ }
func TestInterceptorScopedCallFailsClosedWithoutDispatch(t *testing.T) { /* no DispatchContext => deny */ }
```

- [ ] **Step 2: Run them, verify they fail**

Run: `task test -- -run 'TestInterceptorScope' ./internal/plugin/hostcap/`
Expected: FAIL.

- [ ] **Step 3: Add the policy+scope half** after the static checks in the interceptor.

```go
dc, haveDispatch := pluginauthz.DispatchForHost(ctx) // host-side accessor
var scopeAttrs map[string]any
if len(md.Scopes) > 0 {
	if !haveDispatch {
		return nil, oops.Code("SCOPE_NO_DISPATCH").With("method", method).Errorf("scoped call without dispatch context")
	}
	if md.Extract == nil {
		return nil, oops.Code("SCOPE_NO_EXTRACTOR").With("method", method).Errorf("scope-eligible method missing extractor") // fail-closed, INV-PLUGIN-52
	}
	scopeAttrs = map[string]any{"dispatch.location": dc.Attributes["location"]}
}
resourceID := capToken
if md.Extract != nil {
	if id, ok := md.Extract(req); ok { resourceID = md.Resource + ":" + id }
}
dec, err := pluginauthz.EvaluateCapabilityAccess(ctx, pluginauthz.CapabilityInput{
	Engine: d.Engine, Auditor: d.Auditor, PluginName: d.pluginName,
	Subject: access.PluginSubject(d.pluginName), Action: md.Action, Resource: resourceID,
	Declared: true, Context: scopeAttrs,
})
if err != nil { return nil, err }
if !dec.Allowed {
	return nil, oops.Code("SCOPE_DENIED").With("capability", capToken).With("method", method).Errorf("denied by policy/scope")
}
return h(ctx, req)
```

The scope condition (`resource.location == dispatch.location`) is expressed as the operator/host policy for the capability's resource type, consuming the `dispatch.location` context attribute. Ship the default host policy that enforces `own-location` for `world.mutation` as part of this task (under the host's policy seeds), so the compiled condition exists without operator authoring.

- [ ] **Step 4: Run them, verify they pass**

Run: `task test -- -run 'TestInterceptorScope' ./internal/plugin/hostcap/`
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(plugin): interceptor policy + scope enforcement (M1/M3, INV-PLUGIN-50, holomush-eykuh.3)`.

### Task 11: Scope-extractor completeness meta-test (INV-PLUGIN-52)

**Files:**

- Create: `internal/plugin/hostcap/descriptor_completeness_test.go`

- [ ] **Step 1: Write the test** — every scope-eligible method carries an extractor; build fails otherwise.

```go
// Verifies: INV-PLUGIN-52
func TestEveryScopeEligibleMethodHasExtractor(t *testing.T) {
	for token, d := range Descriptors {
		for name, m := range d.Methods {
			if len(m.Scopes) > 0 {
				require.NotNilf(t, m.Extract, "capability %q method %q is scope-eligible but has no extractor (fail-open hazard)", token, name)
			}
		}
	}
}
```

- [ ] **Step 2: Run it, verify it passes** (the table is already correct).

Run: `task test -- -run TestEveryScopeEligibleMethodHasExtractor ./internal/plugin/hostcap/`
Expected: PASS.

- [ ] **Step 3: Add a runtime fail-closed test** — an interceptor with a deliberately extractor-less scoped descriptor denies (asserts the runtime guard, not just the meta-test).

- [ ] **Step 4: Run it, verify it passes**

- [ ] **Step 5: Commit** — `test(plugin): scope-extractor completeness + runtime fail-closed (INV-PLUGIN-52, holomush-eykuh.3)`.

---

## Phase 7: M4 — the command-registry trust fix

### Task 12: Binary `ListCommands`/`GetCommandHelp` use `DispatchContext.Subject`

**Files:**

- Modify: `internal/plugin/hostcap/servers.go:959-1013`
- Test: `internal/plugin/goplugin/host_service_command_test.go` (+ a spoof test)

- [ ] **Step 1: Write the failing tests** — a spoofed `character_id` is ignored; the host-vouched dispatch subject is used; absent dispatch fails closed.

```go
// Verifies: INV-PLUGIN-51
func TestListCommandsIgnoresWireCharacterIDUsesDispatch(t *testing.T) {
	dispatchChar := ulid.Make()
	ctx := pluginauthz.WithDispatch(context.Background(), pluginauthz.DispatchContext{
		Subject: access.CharacterSubject(dispatchChar.String()),
	})
	srv := &commandRegistryServer{ /* base with querier */ }
	// Pass a DIFFERENT character_id on the wire; assert the querier was called
	// with the dispatch subject, NOT the wire id.
	_, err := srv.ListCommands(ctx, &hostv1.ListCommandsRequest{CharacterId: ulid.Make().String()})
	require.NoError(t, err)
	// assert recorded subject == access.CharacterSubject(dispatchChar.String())
}

func TestListCommandsFailsClosedWithoutDispatch(t *testing.T) {
	srv := &commandRegistryServer{ /* ... */ }
	_, err := srv.ListCommands(context.Background(), &hostv1.ListCommandsRequest{CharacterId: ulid.Make().String()})
	errutil.AssertErrorCode(t, err, "NO_DISPATCH_SUBJECT")
}
```

- [ ] **Step 2: Run them, verify they fail**

Run: `task test -- -run 'TestListCommandsIgnoresWire|TestListCommandsFailsClosed' ./internal/plugin/goplugin/`
Expected: FAIL — still reads wire id.

- [ ] **Step 3: Switch the subject source** in both methods.

```go
dc, ok := pluginauthz.DispatchForHost(ctx)
if !ok || dc.Subject == "" {
	return nil, oops.Code("NO_DISPATCH_SUBJECT").With("plugin", s.pluginName).
		Errorf("command-registry call without a host-vouched dispatch subject")
}
res, err := q.Available(ctx, dc.Subject) // was access.CharacterSubject(req.GetCharacterId())
```

Apply the same to `GetCommandHelp` (`q.Help(ctx, dc.Subject, req.GetName())`). Leave the proto field present but no longer read for authorization (final remove-vs-keep is a follow-up proto-compat bead).

- [ ] **Step 4: Run them, verify they pass**

Run: `task test -- -run 'TestListCommandsIgnoresWire|TestListCommandsFailsClosed' ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 5: Commit** — `fix(plugin): command-registry uses host-vouched dispatch subject (M4, INV-PLUGIN-51, holomush-eykuh.3)`.

### Task 13: Lua `list_commands`/`get_command_help` use `DispatchContext.Subject` (symmetry)

**Files:**

- Modify: `internal/plugin/hostfunc/commands.go`
- Test: `internal/plugin/hostfunc/commands_test.go` + `internal/plugin/lua/command_parity_test.go`

- [ ] **Step 1: Write the failing tests** — the Lua hostfuncs ignore the supplied character arg and use the dispatch subject; parity test asserts binary + Lua resolve the identical subject.

```go
// Verifies: INV-PLUGIN-51
func TestLuaListCommandsIgnoresArgUsesDispatch(t *testing.T) {
	// hostfunc bound on a ctx carrying DispatchContext.Subject = character X;
	// Lua calls holomush.list_commands("<character Y>"); assert querier saw X.
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `task test -- -run TestLuaListCommandsIgnoresArg ./internal/plugin/hostfunc/`
Expected: FAIL.

- [ ] **Step 3: Switch the Lua shims** to read `pluginauthz.DispatchForHost(ls.Context())` for the subject instead of the Lua-supplied `character_id`; fail closed when absent. Keep the Lua signature (drop the arg from the authz path; argument becomes ignored or removed per the same proto-compat follow-up).

- [ ] **Step 4: Run it + the parity test, verify they pass**

Run: `task test -- -run 'TestLuaListCommandsIgnoresArg|TestCommandRegistryParity' ./internal/plugin/...`
Expected: PASS.

- [ ] **Step 5: Commit** — `fix(plugin): Lua command-registry symmetry with dispatch subject (M4, holomush-eykuh.3)`.

---

## Phase 8: Invariant registration

### Task 14: Register + bind INV-PLUGIN-50…53

**Files:**

- Modify: `docs/architecture/invariants.yaml`
- Regenerate: `docs/architecture/invariants.md` via `go run ./cmd/inv-render`
- Test: `test/meta/invariant_registry_test.go` (drift + binding gates)

> NOTE: INV-PLUGIN-50…53 are **already registered as `binding: pending`** in
> `invariants.yaml` (added at spec finalization, in the docs PR, because the
> spec under `docs/superpowers/specs/` references them and the orphan meta-test
> requires registry presence). This task only **flips them to `bound`** and adds
> `asserted_by` once the asserting tests exist. The registry entry shape is
> `scope: INV-PLUGIN` + `origin_spec` + `summary` + `binding` (no `boundary`
> field — match the existing INV-PLUGIN-49 entry).

- [ ] **Step 1: Flip each entry to `bound`** in `invariants.yaml` and add its `asserted_by`. Map: INV-PLUGIN-50 → `internal/plugin/pluginauthz/capability_test.go` (Task 7); INV-PLUGIN-51 → `internal/plugin/goplugin/host_service_command_test.go` + `internal/plugin/hostfunc/commands_test.go` (Tasks 12/13); INV-PLUGIN-52 → `internal/plugin/hostcap/descriptor_completeness_test.go` (Task 11); INV-PLUGIN-53 → `internal/plugin/manifest_test.go` (Tasks 2/4).

```yaml
  # change `binding: pending` -> `binding: bound` and append asserted_by, e.g.:
  - id: INV-PLUGIN-50
    scope: INV-PLUGIN
    origin_spec: "docs/superpowers/specs/2026-06-12-plugin-least-privilege-trust-design.md"
    summary: "A plugin's consumption of a host capability ... necessary but not sufficient ..."
    binding: bound
    asserted_by:
      - "internal/plugin/pluginauthz/capability_test.go"
```

- [ ] **Step 2: Regenerate + verify**

Run: `go run ./cmd/inv-render` then `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`
Expected: PASS (no drift; all four bound and genuinely asserted).

- [ ] **Step 3: Add `// Verifies: INV-PLUGIN-5N`** annotations immediately above each asserting test (Task 7/11/12/13/2 tests) if not already added when written.

- [ ] **Step 4: Run the full meta suite**

Run: `task test -- ./test/meta/`
Expected: PASS.

- [ ] **Step 5: Commit** — `docs(arch): register INV-PLUGIN-50..53 (holomush-eykuh.3)`.

---

## Post-implementation checklist

- [ ] `task pr-prep` green (fast lane), and `task pr-prep:full` since this touches plugin int surface (Ginkgo + bufconn).
- [ ] `task test:int` green (delivery + both-runtime interceptor parity).
- [ ] `schemas/plugin.schema.json` regenerated and committed (drift gate).
- [ ] `docs/architecture/invariants.md` regenerated from yaml (never hand-edited).
- [ ] `.claude/rules/plugin-manifest.md` updated to document `access:`/`scope:` (capability-only) — small docs edit.
- [ ] Site `extending/` doc: a short "least-privilege: access/scope" section (`plugin info` shows the declared contract).
- [ ] Sub-spec 5 (`holomush-eykuh.4`) handoff note: production-manifest migration + Lua-injection gating consume these mechanics; nothing here migrates production plugins.
- [ ] Follow-up bead: final disposition of the wire `character_id` field on the command-registry RPCs (keep-but-ignore vs remove).
