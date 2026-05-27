<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin ABAC Trust Boundary & Attribute Resolution — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow binary plugins to install character-level ABAC policies scoped to their own resource types, provide attribute resolution via gRPC, and migrate seed command policies to their owning plugins.

**Architecture:** Expand the policy installer trust boundary using a `resource_types` manifest field that scopes which resource types a plugin can target. Binary plugins implement an `AttributeResolver` gRPC service for schema discovery and resource attribute resolution. Core registers proxy `AttributeProvider` instances per resource type during plugin load. Seed command policies migrate to plugin manifests.

**Tech Stack:** Go, protobuf/buf, hashicorp/go-plugin, gRPC, testify, Ginkgo/Gomega (E2E)

**Spec:** `docs/superpowers/specs/2026-04-06-plugin-abac-trust-boundary-design.md`

---

## File Structure

### New files

| File | Responsibility |
|---|---|
| `api/proto/holomush/plugin/v1/attribute.proto` | `AttributeResolver` gRPC service definition |
| `internal/plugin/attribute_proxy.go` | Proxy `AttributeProvider` that routes to plugin gRPC |
| `internal/plugin/attribute_proxy_test.go` | Unit tests for proxy provider |
| `internal/plugin/policy_validator.go` | Trust boundary validation logic (extracted from policy_installer) |
| `internal/plugin/policy_validator_test.go` | Unit tests for trust boundary rules |
| `internal/plugin/manifest_warnings.go` | Manifest validation warnings (missing execute policy, etc.) |
| `internal/plugin/manifest_warnings_test.go` | Unit tests for manifest warnings |
| `plugins/test-abac-widget/main.go` | Test binary plugin |
| `plugins/test-abac-widget/plugin.yaml` | Test plugin manifest |
| `test/integration/plugin/abac_widget_test.go` | E2E tests for plugin ABAC pipeline |

### Modified files

| File | Changes |
|---|---|
| `internal/plugin/manifest.go` | Add `ResourceTypes`, `Trust` fields; validation rules |
| `internal/plugin/manifest_test.go` | Tests for new fields |
| `internal/plugin/policy_installer.go` | Replace line 58 check with `policy_validator.go` call |
| `internal/plugin/policy_installer_test.go` | Updated tests for expanded trust |
| `internal/plugin/manager.go` | Schema discovery + proxy provider registration in `loadPlugin` |
| `internal/plugin/goplugin/host.go` | Expose `AttributeResolver` client from loaded plugins |
| `internal/plugin/goplugin/plugin.go` | Register `AttributeResolver` in plugin map |
| `internal/access/policy/seed.go` | Trim command policies per spec §6.2 |
| `internal/access/policy/seed_test.go` | Update seed count assertions |
| `internal/access/policy/seed_smoke_test.go` | Update/remove smoke tests for migrated policies |
| `pkg/plugin/sdk.go` | Support `AttributeResolver` registration |
| `plugins/core-communication/plugin.yaml` | Add `policies:` section |
| `plugins/core-objects/plugin.yaml` | Add `policies:` section |
| `plugins/core-help/plugin.yaml` | Add `policies:` section |
| `plugins/core-aliases/plugin.yaml` | Add `policies:` section |
| `plugins/core-building/plugin.yaml` | Add `policies:` section |
| `plugins/core-scenes/plugin.yaml` | Add `policies:` section |

---

### Task 1: Manifest — `resource_types` and `trust` fields

**Files:**

- Modify: `internal/plugin/manifest.go`
- Modify: `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write failing tests for resource_types field**

Add to `manifest_test.go`:

```go
func TestManifestResourceTypesValidForBinaryPlugin(t *testing.T) {
	data := []byte(`
name: test-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: test-plugin
resource_types: [widget]
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	assert.Equal(t, []string{"widget"}, m.ResourceTypes)
}

func TestManifestResourceTypesRejectedForLuaPlugin(t *testing.T) {
	data := []byte(`
name: test-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
resource_types: [widget]
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resource_types")
}

func TestManifestResourceTypesRejectsProtectedType(t *testing.T) {
	data := []byte(`
name: test-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: test-plugin
resource_types: [location]
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protected")
}

func TestManifestTrustFieldParsed(t *testing.T) {
	data := []byte(`
name: test-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: test-plugin
trust:
  all_principals: true
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	require.NotNil(t, m.Trust)
	assert.True(t, m.Trust.AllPrincipals)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestManifestResourceTypes ./internal/plugin/`
Expected: FAIL — fields don't exist

- [ ] **Step 3: Add fields to Manifest struct and Validate()**

In `internal/plugin/manifest.go`, add to the `Manifest` struct:

```go
ResourceTypes []string       `yaml:"resource_types,omitempty" json:"resource_types,omitempty"`
Trust         *TrustConfig   `yaml:"trust,omitempty" json:"trust,omitempty"`
```

Add the `TrustConfig` type:

```go
// TrustConfig declares trust escalation for the plugin.
// When AllPrincipals is true AND the server config allowlists this plugin,
// the plugin can install policies targeting any principal/resource type.
type TrustConfig struct {
	AllPrincipals bool `yaml:"all_principals" json:"all_principals"`
}
```

Add the protected types set:

```go
// ProtectedResourceTypes are core resource types that plugins MUST NOT
// declare in resource_types. Plugins cannot install character-level
// policies targeting these types without trust escalation.
var ProtectedResourceTypes = map[string]bool{
	"character": true, "location": true, "exit": true, "object": true,
	"stream": true, "property": true, "scene": true, "command": true,
	"system": true, "server": true, "player": true,
}
```

Add validation in `Validate()` after the storage validation block:

```go
// Validate resource_types: binary-only, valid names, no protected types.
if len(m.ResourceTypes) > 0 {
	if m.Type != TypeBinary {
		return oops.In("manifest").With("name", m.Name).
			New("resource_types can only be declared by binary plugins")
	}
	seen := make(map[string]bool)
	for _, rt := range m.ResourceTypes {
		if !namePattern.MatchString(rt) {
			return oops.In("manifest").With("name", m.Name).With("resource_type", rt).
				New("resource type name must match plugin naming pattern")
		}
		if ProtectedResourceTypes[rt] {
			return oops.In("manifest").With("name", m.Name).With("resource_type", rt).
				New("resource type is protected and cannot be declared by plugins")
		}
		if seen[rt] {
			return oops.In("manifest").With("name", m.Name).With("resource_type", rt).
				New("duplicate resource type")
		}
		seen[rt] = true
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run TestManifest ./internal/plugin/`
Expected: PASS

- [ ] **Step 5: Commit**

```text
jj --no-pager commit -m "feat(plugin): add resource_types and trust manifest fields

Plugins can declare resource types they own. Only binary plugins may
use resource_types. Protected core types are rejected. Trust config
parsed but not yet enforced."
```

---

### Task 2: Policy validator — trust boundary rules

**Files:**

- Create: `internal/plugin/policy_validator.go`
- Create: `internal/plugin/policy_validator_test.go`

- [ ] **Step 1: Write failing tests for policy validation rules**

Create `internal/plugin/policy_validator_test.go`:

```go
package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestValidatePolicyAllowsPrincipalPlugin(t *testing.T) {
	ctx := plugins.PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := plugins.ValidatePluginPolicy(ctx, plugins.ManifestPolicy{
		Name: "allow-kv",
		DSL:  `permit(principal is plugin, action in ["kv:read"], resource is kv);`,
	})
	require.NoError(t, err)
}

func TestValidatePolicyAllowsCharacterOnDeclaredResourceType(t *testing.T) {
	ctx := plugins.PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := plugins.ValidatePluginPolicy(ctx, plugins.ManifestPolicy{
		Name: "widget-read",
		DSL:  `permit(principal is character, action in ["read"], resource is widget);`,
	})
	require.NoError(t, err)
}

func TestValidatePolicyAllowsCharacterOnCommandForOwnCommand(t *testing.T) {
	ctx := plugins.PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := plugins.ValidatePluginPolicy(ctx, plugins.ManifestPolicy{
		Name: "widget-execute",
		DSL:  `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name == "widget" };`,
	})
	require.NoError(t, err)
}

func TestValidatePolicyRejectsCharacterOnProtectedType(t *testing.T) {
	ctx := plugins.PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := plugins.ValidatePluginPolicy(ctx, plugins.ManifestPolicy{
		Name: "bad-location",
		DSL:  `permit(principal is character, action in ["read"], resource is location);`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protected")
}

func TestValidatePolicyRejectsCharacterOnUndeclaredType(t *testing.T) {
	ctx := plugins.PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := plugins.ValidatePluginPolicy(ctx, plugins.ManifestPolicy{
		Name: "bad-gadget",
		DSL:  `permit(principal is character, action in ["read"], resource is gadget);`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in plugin's resource_types")
}

func TestValidatePolicyRejectsCommandForForeignCommand(t *testing.T) {
	ctx := plugins.PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := plugins.ValidatePluginPolicy(ctx, plugins.ManifestPolicy{
		Name: "bad-execute",
		DSL:  `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name == "look" };`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foreign command")
}

func TestValidatePolicyRejectsPrincipalSystem(t *testing.T) {
	ctx := plugins.PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := plugins.ValidatePluginPolicy(ctx, plugins.ManifestPolicy{
		Name: "system-abuse",
		DSL:  `permit(principal is system, action in ["write"], resource is widget);`,
	})
	require.Error(t, err)
}

func TestValidatePolicyAllowsElevatedTrustWithAllPrincipals(t *testing.T) {
	ctx := plugins.PolicyValidationContext{
		PluginName:       "exotic-plugin",
		ResourceTypes:    []string{},
		CommandNames:     []string{},
		TrustEscalated:   true,
	}
	err := plugins.ValidatePluginPolicy(ctx, plugins.ManifestPolicy{
		Name: "broad-policy",
		DSL:  `permit(principal is character, action in ["read"], resource is location);`,
	})
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestValidatePolicy ./internal/plugin/`
Expected: FAIL — `ValidatePluginPolicy` doesn't exist

- [ ] **Step 3: Implement policy_validator.go**

Create `internal/plugin/policy_validator.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/dsl"
)

// PolicyValidationContext provides the context needed to validate a plugin policy.
type PolicyValidationContext struct {
	PluginName     string
	ResourceTypes  []string
	CommandNames   []string
	TrustEscalated bool
}

// ValidatePluginPolicy checks whether a single manifest policy is allowed
// given the plugin's declared resource types and trust level.
func ValidatePluginPolicy(ctx PolicyValidationContext, mp ManifestPolicy) error {
	parsed, err := dsl.Parse(mp.DSL)
	if err != nil {
		return oops.With("plugin", ctx.PluginName).With("policy", mp.Name).
			Wrapf(err, "compiling plugin policy DSL")
	}

	// Existing scope check: plugins can only reference their own principal name
	if ok, foreignName := dsl.ValidatePrincipalScope(parsed, ctx.PluginName); !ok {
		return oops.With("plugin", ctx.PluginName).With("policy", mp.Name).
			With("foreign_principal", foreignName).
			Errorf("plugin policy references foreign principal %q", foreignName)
	}

	// If no principal type specified, allow (wildcard principal)
	if parsed.Target == nil || parsed.Target.Principal == nil || parsed.Target.Principal.Type == "" {
		return nil
	}

	principalType := parsed.Target.Principal.Type

	// Rule: principal is plugin → always allowed
	if principalType == "plugin" {
		return nil
	}

	// Rule: principal is system → always rejected
	if principalType == "system" {
		return oops.With("plugin", ctx.PluginName).With("policy", mp.Name).
			Errorf("plugins must not declare principal type %q", principalType)
	}

	// Rule: principal is character → check trust boundary
	if principalType == "character" {
		return validateCharacterPolicy(ctx, mp, parsed)
	}

	// Unknown principal type — reject by default
	return oops.With("plugin", ctx.PluginName).With("policy", mp.Name).
		Errorf("unsupported principal type %q", principalType)
}

func validateCharacterPolicy(ctx PolicyValidationContext, mp ManifestPolicy, parsed *dsl.Policy) error {
	// Trust-escalated plugins bypass all resource checks
	if ctx.TrustEscalated {
		slog.Warn("plugin installing character-level policy with elevated trust",
			"plugin", ctx.PluginName, "policy", mp.Name)
		return nil
	}

	// Extract resource type from the policy target
	resourceType := ""
	if parsed.Target != nil && parsed.Target.Resource != nil && parsed.Target.Resource.Type != "" {
		resourceType = parsed.Target.Resource.Type
	}

	// No resource type in policy → reject (too broad)
	if resourceType == "" {
		return oops.With("plugin", ctx.PluginName).With("policy", mp.Name).
			Errorf("character-level policies must specify a resource type")
	}

	// Special case: command resource type — check command name scoping
	if resourceType == "command" {
		return validateCommandPolicy(ctx, mp, parsed)
	}

	// Check if resource type is protected
	if ProtectedResourceTypes[resourceType] {
		return oops.With("plugin", ctx.PluginName).With("policy", mp.Name).
			With("resource_type", resourceType).
			Errorf("resource type %q is protected; plugins cannot target it", resourceType)
	}

	// Check if resource type is in plugin's declared resource_types
	allowed := false
	for _, rt := range ctx.ResourceTypes {
		if rt == resourceType {
			allowed = true
			break
		}
	}
	if !allowed {
		return oops.With("plugin", ctx.PluginName).With("policy", mp.Name).
			With("resource_type", resourceType).
			Errorf("resource type %q is not in plugin's resource_types", resourceType)
	}

	slog.Info("plugin installing character-level policy",
		"plugin", ctx.PluginName, "policy", mp.Name, "resource_type", resourceType)
	return nil
}

func validateCommandPolicy(ctx PolicyValidationContext, mp ManifestPolicy, parsed *dsl.Policy) error {
	// Extract command names referenced in conditions
	referencedNames := dsl.ExtractCommandNames(parsed.Conditions)

	// Every referenced command name must be in the plugin's command list
	cmdSet := make(map[string]bool, len(ctx.CommandNames))
	for _, name := range ctx.CommandNames {
		cmdSet[name] = true
	}

	for _, name := range referencedNames {
		if !cmdSet[name] {
			return oops.With("plugin", ctx.PluginName).With("policy", mp.Name).
				With("command", name).
				Errorf("policy references foreign command %q not declared by this plugin", name)
		}
	}

	slog.Info("plugin installing command execute policy",
		"plugin", ctx.PluginName, "policy", mp.Name)
	return nil
}
```

- [ ] **Step 4: Implement ExtractCommandNames in DSL package**

Create or add to `internal/access/policy/dsl/refs.go`:

```go
// ExtractCommandNames extracts all string literals compared against
// resource.command.name in conditions. Used by the policy validator to
// verify plugins only grant execute permits for their own commands.
func ExtractCommandNames(block *ConditionBlock) []string {
	if block == nil {
		return nil
	}
	var names []string
	for _, disj := range block.Disjunctions {
		for _, cond := range disj.Conditions {
			names = append(names, extractCommandNamesFromCondition(cond)...)
		}
	}
	return names
}

func extractCommandNamesFromCondition(c *Condition) []string {
	if c == nil {
		return nil
	}
	if c.Negation != nil {
		return extractCommandNamesFromCondition(c.Negation)
	}
	if c.Parenthesized != nil {
		return ExtractCommandNames(c.Parenthesized)
	}
	if c.IfThenElse != nil {
		var names []string
		names = append(names, extractCommandNamesFromCondition(c.IfThenElse.If)...)
		names = append(names, extractCommandNamesFromCondition(c.IfThenElse.Then)...)
		names = append(names, extractCommandNamesFromCondition(c.IfThenElse.Else)...)
		return names
	}
	if c.Comparison != nil {
		return extractCommandNamesFromComparison(c.Comparison)
	}
	if c.InList != nil {
		return extractCommandNamesFromInList(c.InList)
	}
	return nil
}

func extractCommandNamesFromComparison(cmp *Comparison) []string {
	if isCommandNameRef(cmp.Left) && cmp.Right != nil && cmp.Right.Literal != nil && cmp.Right.Literal.Str != nil {
		return []string{*cmp.Right.Literal.Str}
	}
	if isCommandNameRef(cmp.Right) && cmp.Left != nil && cmp.Left.Literal != nil && cmp.Left.Literal.Str != nil {
		return []string{*cmp.Left.Literal.Str}
	}
	return nil
}

func extractCommandNamesFromInList(il *InListCondition) []string {
	if !isCommandNameRef(il.Left) {
		return nil
	}
	var names []string
	for _, item := range il.Items {
		if item.Str != nil {
			names = append(names, *item.Str)
		}
	}
	return names
}

func isCommandNameRef(e *Expr) bool {
	if e == nil || e.AttrRef == nil {
		return false
	}
	return e.AttrRef.Root == "resource" &&
		len(e.AttrRef.Path) == 2 &&
		e.AttrRef.Path[0] == "command" &&
		e.AttrRef.Path[1] == "name"
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test -- -run TestValidatePolicy ./internal/plugin/`
Expected: PASS

- [ ] **Step 6: Commit**

```text
jj --no-pager commit -m "feat(plugin): policy validation rules for trust boundary

Implements the §2.1 validation table from the spec. Plugins can
install character-level policies scoped to their declared resource
types or their own commands. Protected core types are rejected.
Trust escalation bypasses checks."
```

---

### Task 3: Wire validator into policy installer

**Files:**

- Modify: `internal/plugin/policy_installer.go`
- Modify: `internal/plugin/policy_installer_test.go`

- [ ] **Step 1: Write failing tests for expanded trust in installer**

Add to `policy_installer_test.go`:

```go
func TestPolicyInstallerAcceptsCharacterPolicyOnDeclaredResourceType(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "widget-read",
			DSL:  `permit(principal is character, action in ["read"], resource is widget);`,
		},
	}

	manifest := &plugins.Manifest{
		Name:          "my-plugin",
		ResourceTypes: []string{"widget"},
		Commands:      []plugins.CommandSpec{{Name: "widget"}},
	}

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.NoError(t, err)
	assert.Len(t, fs.created, 1)
}

func TestPolicyInstallerRejectsCharacterPolicyOnProtectedType(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	policies := []plugins.ManifestPolicy{
		{
			Name: "bad-policy",
			DSL:  `permit(principal is character, action in ["read"], resource is location);`,
		},
	}

	manifest := &plugins.Manifest{
		Name:          "my-plugin",
		ResourceTypes: []string{"widget"},
	}

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), manifest, policies)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protected")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestPolicyInstaller ./internal/plugin/`
Expected: FAIL — `InstallPluginPoliciesWithManifest` doesn't exist

- [ ] **Step 3: Add manifest-aware method to PolicyInstaller**

In `policy_installer.go`, replace `compilePolicies` with a manifest-aware version and add `InstallPluginPoliciesWithManifest`:

```go
// InstallPluginPoliciesWithManifest validates and installs policies using
// the full manifest context for trust boundary validation.
func (pi *PolicyInstaller) InstallPluginPoliciesWithManifest(ctx context.Context, manifest *Manifest, policies []ManifestPolicy) error {
	compiled, err := compilePoliciesWithManifest(manifest, policies)
	if err != nil {
		return err
	}
	if err := pi.store.ReplaceBySource(ctx, "plugin", "plugin:"+manifest.Name+":", compiled); err != nil {
		return oops.With("plugin", manifest.Name).Wrapf(err, "installing plugin policies")
	}
	return nil
}
```

Update `compilePoliciesWithManifest` to use the new validator instead of the old hardcoded check. Keep backward compatibility: `InstallPluginPolicies` (the old method) continues working for `principal is plugin` policies by constructing a minimal validation context.

- [ ] **Step 4: Run all policy installer tests**

Run: `task test -- -run TestPolicyInstaller ./internal/plugin/`
Expected: PASS (all existing + new tests)

- [ ] **Step 5: Commit**

```text
jj --no-pager commit -m "feat(plugin): wire trust boundary validator into policy installer

InstallPluginPoliciesWithManifest uses the full manifest to validate
policies against the trust boundary. Existing InstallPluginPolicies
preserved for backward compatibility."
```

---

### Task 4: Proto — AttributeResolver service

**Files:**

- Create: `api/proto/holomush/plugin/v1/attribute.proto`

- [ ] **Step 1: Create the proto file**

Create `api/proto/holomush/plugin/v1/attribute.proto` with the service definition from spec §3.1.

- [ ] **Step 2: Generate Go code**

Run: `task proto`
Expected: Generated files in `pkg/proto/holomush/plugin/v1/`

- [ ] **Step 3: Verify generated code compiles**

Run: `task build`
Expected: PASS

- [ ] **Step 4: Commit**

```text
jj --no-pager commit -m "feat(proto): add AttributeResolver gRPC service for plugin attribute resolution

GetSchema returns resource type schemas at load time.
ResolveResource returns attributes for specific resource instances
during ABAC policy evaluation."
```

---

### Task 5: Proxy AttributeProvider

**Files:**

- Create: `internal/plugin/attribute_proxy.go`
- Create: `internal/plugin/attribute_proxy_test.go`

- [ ] **Step 1: Write failing tests**

Test that `PluginAttributeProvider` implements `AttributeProvider`, routes `ResolveResource` to a mock gRPC client, returns `nil, nil` for `ResolveSubject`, and converts proto responses to the expected `map[string]any` format.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestPluginAttributeProvider ./internal/plugin/`
Expected: FAIL

- [ ] **Step 3: Implement PluginAttributeProvider**

The provider wraps a `pluginv1.AttributeResolverClient` (from generated proto code) and a `types.NamespaceSchema` (from `GetSchema` response). It converts proto `AttributeValue` to Go types matching the `AttributeProvider` contract (strings, float64 for numbers, bool, `[]string` for lists).

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run TestPluginAttributeProvider ./internal/plugin/`
Expected: PASS

- [ ] **Step 5: Commit**

```text
jj --no-pager commit -m "feat(plugin): proxy AttributeProvider for plugin resource types

Routes ResolveResource calls over gRPC to the plugin's
AttributeResolver service. Converts proto responses to the
map[string]any format expected by the ABAC resolver."
```

---

### Task 6: Plugin SDK — AttributeResolver support

**Files:**

- Modify: `pkg/plugin/sdk.go`
- Modify: `pkg/plugin/service.go` (if needed)

- [ ] **Step 1: Add AttributeResolverProvider interface**

```go
// AttributeResolverProvider is implemented by binary plugins that provide
// attribute resolution for resource types they own.
type AttributeResolverProvider interface {
	RegisterAttributeResolver(registrar grpc.ServiceRegistrar)
}
```

- [ ] **Step 2: Wire into ServeWithServices**

In the `grpcPlugin.GRPCServer` method, check if the handler also implements `AttributeResolverProvider` and call `RegisterAttributeResolver(s)` to register the service.

- [ ] **Step 3: Run existing tests**

Run: `task test -- ./pkg/plugin/...`
Expected: PASS (no regressions)

- [ ] **Step 4: Commit**

```text
jj --no-pager commit -m "feat(sdk): support AttributeResolver registration in plugin SDK

Binary plugins implementing AttributeResolverProvider get their
resolver service registered on the go-plugin gRPC transport."
```

---

### Task 7: Wire schema discovery + proxy registration into loadPlugin

**Files:**

- Modify: `internal/plugin/manager.go`
- Modify: `internal/plugin/goplugin/host.go`
- Modify: `internal/plugin/goplugin/plugin.go`

- [ ] **Step 1: Expose AttributeResolverClient from goplugin Host**

In `goplugin/host.go`, add a method to get the `AttributeResolverClient` for a loaded plugin (using the stored `grpc.ClientConnInterface`).

- [ ] **Step 2: Add AttributeProviderRegistrar interface to Manager**

The manager needs a way to register proxy providers with the ABAC resolver. Add a `WithAttributeResolver` option that accepts the resolver.

- [ ] **Step 3: Implement schema discovery in loadPlugin**

After `host.Load()` succeeds but before `InstallPluginPolicies`, if the manifest has `resource_types`:

1. Get the `AttributeResolverClient` from the host
2. Call `GetSchema(ctx, &pluginv1.GetSchemaRequest{})`
3. Validate returned types match `resource_types`
4. Convert proto schema to `types.NamespaceSchema`
5. Create `PluginAttributeProvider` per resource type
6. Register providers with the ABAC resolver

- [ ] **Step 4: Update loadPlugin to use manifest-aware policy installation**

Replace the `InstallPluginPolicies` call with `InstallPluginPoliciesWithManifest`, passing the full manifest for trust boundary validation.

- [ ] **Step 5: Run unit tests**

Run: `task test -- ./internal/plugin/...`
Expected: PASS

- [ ] **Step 6: Commit**

```text
jj --no-pager commit -m "feat(plugin): schema discovery and proxy provider registration during load

loadPlugin calls GetSchema on binary plugins with resource_types,
validates schema matches declarations, registers proxy
AttributeProviders, and uses manifest-aware policy installation."
```

---

### Task 8: Test binary plugin — test-abac-widget

**Files:**

- Create: `plugins/test-abac-widget/main.go`
- Create: `plugins/test-abac-widget/plugin.yaml`

- [ ] **Step 1: Create plugin.yaml**

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

name: test-abac-widget
version: 1.0.0
type: binary
resource_types: [widget]
binary-plugin:
  executable: test-abac-widget
commands:
  - name: widget
    capabilities:
      - action: read
        resource: widget
        scope: self
    help: "Test widget command for ABAC validation"
    usage: "widget <id>"
policies:
  - name: widget-execute
    dsl: >-
      permit(principal is character, action in ["execute"],
      resource is command) when { resource.command.name == "widget" }
  - name: widget-read-normal
    dsl: >-
      permit(principal is character, action in ["read"],
      resource is widget) when { resource.widget.type == "normal" }
  - name: widget-forbid-restricted
    dsl: >-
      forbid(principal is character, action in ["read"],
      resource is widget) when { resource.widget.type == "restricted" }
```

- [ ] **Step 2: Create main.go**

Implement a minimal binary plugin that:

- Implements `Handler`, `CommandHandler`, `AttributeResolverProvider`
- `HandleCommand` returns a simple success response
- `GetSchema` returns `widget` type with `type` (string) and `owner` (string) attributes
- `ResolveResource` returns hardcoded attributes: ID `widget-normal` → `type: "normal"`, ID `widget-restricted` → `type: "restricted"`, anything else → `type: "normal"`, `owner: "test-owner"`
- Uses `pluginsdk.ServeWithServices`

- [ ] **Step 3: Build the plugin**

Run: `task plugin:build -- test-abac-widget`
Expected: Binary in `build/plugins/test-abac-widget/`

- [ ] **Step 4: Commit**

```text
jj --no-pager commit -m "feat(plugin): add test-abac-widget binary plugin

Minimal binary plugin for E2E testing of the plugin ABAC pipeline.
Declares resource_types: [widget], contributes character-level
policies, and implements AttributeResolver for widget attributes."
```

---

### Task 9: E2E tests for plugin ABAC pipeline

**Files:**

- Create: `test/integration/plugin/abac_widget_test.go`

- [ ] **Step 1: Write E2E test file**

Create `test/integration/plugin/abac_widget_test.go` using Ginkgo/Gomega following the pattern in `binary_plugin_test.go`. Test cases per spec §5.3:

1. Plugin loads with `resource_types: [widget]`
2. Character-level policies are installed in the policy store
3. Command dispatch passes Layer 1 (execute permit from plugin)
4. Command dispatch passes Layer 2 pre-flight
5. Full ABAC evaluation: normal widget → permit
6. Full ABAC evaluation: restricted widget → forbid
7. Policy targeting `resource is location` → rejected at load time
8. Trust escalation without server allowlist → rejected

- [ ] **Step 2: Build and run E2E tests**

Run: `task test:int`
Expected: All E2E tests pass (including existing plugin tests)

- [ ] **Step 3: Commit**

```text
jj --no-pager commit -m "test(plugin): E2E tests for plugin ABAC trust boundary and attribute resolution

Validates full pipeline: plugin loads, schema discovered, character-level
policies installed, command dispatches through ABAC, and plugin-resolved
attributes used in policy evaluation."
```

---

### Task 10: Seed policy migration — core plugin policies

**Files:**

- Modify: `plugins/core-communication/plugin.yaml`
- Modify: `plugins/core-objects/plugin.yaml`
- Modify: `plugins/core-help/plugin.yaml`
- Modify: `plugins/core-aliases/plugin.yaml`
- Modify: `plugins/core-building/plugin.yaml`
- Modify: `plugins/core-scenes/plugin.yaml`

- [ ] **Step 1: Add policies to core-communication**

Add `policies:` section to `plugins/core-communication/plugin.yaml`:

```yaml
policies:
  - name: execute-communication
    dsl: >-
      permit(principal is character, action in ["execute"],
      resource is command) when { resource.command.name in
      ["say", "pose", "page", "whisper", "emit", "ooc", "wall"] }
  - name: execute-pemit
    dsl: >-
      permit(principal is character, action in ["execute"],
      resource is command) when {
      principal.character.roles.containsAny(["storyteller", "admin"])
      && resource.command.name == "pemit" }
```

- [ ] **Step 2: Add policies to core-objects**

```yaml
policies:
  - name: execute-objects-player
    dsl: >-
      permit(principal is character, action in ["execute"],
      resource is command) when { resource.command.name in
      ["examine", "set"] }
  - name: execute-objects-builder
    dsl: >-
      permit(principal is character, action in ["execute"],
      resource is command) when {
      "builder" in principal.character.roles
      && resource.command.name in ["create", "describe"] }
```

- [ ] **Step 3: Add policies to core-help**

```yaml
policies:
  - name: execute-help
    dsl: >-
      permit(principal is character, action in ["execute"],
      resource is command) when { resource.command.name == "help" }
```

- [ ] **Step 4: Add policies to core-aliases**

```yaml
policies:
  - name: execute-aliases-player
    dsl: >-
      permit(principal is character, action in ["execute"],
      resource is command) when { resource.command.name in
      ["alias", "unalias", "aliases"] }
  - name: execute-aliases-admin
    dsl: >-
      permit(principal is character, action in ["execute"],
      resource is command) when {
      "admin" in principal.character.roles
      && resource.command.name in ["sysalias", "sysunsalias", "sysaliases"] }
```

- [ ] **Step 5: Add policies to core-building**

```yaml
policies:
  - name: execute-building
    dsl: >-
      permit(principal is character, action in ["execute"],
      resource is command) when {
      "builder" in principal.character.roles
      && resource.command.name in ["dig", "link"] }
```

- [ ] **Step 6: Add policies to core-scenes**

```yaml
policies:
  - name: execute-scenes
    dsl: >-
      permit(principal is character, action in ["execute"],
      resource is command) when { resource.command.name in
      ["scene", "scenes"] }
```

- [ ] **Step 7: Run manifest validation**

Run: `task test -- -run TestManifest ./internal/plugin/`
Expected: PASS — all manifests parse correctly

- [ ] **Step 8: Commit**

```text
jj --no-pager commit -m "feat(plugin): migrate command execute policies to core plugin manifests

Each core Lua plugin now declares its own execute policies instead
of relying on core seed policies. Policies use the same DSL as seed
but are installed at plugin load time."
```

---

### Task 11: Trim seed policies

**Files:**

- Modify: `internal/access/policy/seed.go`
- Modify: `internal/access/policy/seed_test.go`
- Modify: `internal/access/policy/seed_smoke_test.go`

- [ ] **Step 1: Trim seed:player-basic-commands to v5**

In `seed.go`, update `seed:player-basic-commands`:

```go
{
    Name:        "seed:player-basic-commands",
    Description: "Characters can execute core compiled-in commands and unimplemented commands",
    DSLText:     `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name in ["quit", "look", "go", "who"] };`,
    SeedVersion: 5,
},
```

- [ ] **Step 2: Remove seed:builder-commands**

Delete the `seed:builder-commands` entry entirely.

- [ ] **Step 3: Remove seed:pemit-storyteller**

Delete the `seed:pemit-storyteller` entry entirely.

- [ ] **Step 4: Update seed count in doc comment**

Update the `SeedPolicies` doc comment to reflect the new count (24 policies: 23 permit, 1 forbid).

- [ ] **Step 5: Update seed tests**

Update `seed_test.go` to assert the new count and verify the trimmed policy content. Update `seed_smoke_test.go` to remove tests for migrated policies and update any count-based assertions.

- [ ] **Step 6: Run all seed tests**

Run: `task test -- ./internal/access/policy/...`
Expected: PASS

- [ ] **Step 7: Commit**

```text
jj --no-pager commit -m "refactor(seed): trim command policies migrated to plugins

seed:player-basic-commands trimmed to quit/look/go/who (v5).
seed:builder-commands and seed:pemit-storyteller removed — these
commands are now in plugin manifests. Seed drops from 27 to 24."
```

---

### Task 12: Manifest warnings

**Files:**

- Create: `internal/plugin/manifest_warnings.go`
- Create: `internal/plugin/manifest_warnings_test.go`

- [ ] **Step 1: Write failing tests**

Test that `CheckManifestWarnings` logs warnings for:

- Command with no matching execute policy
- Command with capabilities on resource type with no matching policy
- Policy references `resource.<type>.<attr>` where `<attr>` not in discovered schema (§3.4)

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestCheckManifestWarnings ./internal/plugin/`
Expected: FAIL

- [ ] **Step 3: Implement CheckManifestWarnings**

Function that inspects the manifest's commands and policies, logging INFO-level warnings for missing coverage. Called from `loadPlugin` after policy installation.

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- -run TestCheckManifestWarnings ./internal/plugin/`
Expected: PASS

- [ ] **Step 5: Commit**

```text
jj --no-pager commit -m "feat(plugin): manifest validation warnings for missing policy coverage

Warns at load time when a command has no execute policy or declares
capabilities with no matching resource-type policy."
```

---

### Task 13: Full integration — run pr-prep

- [ ] **Step 1: Run task pr-prep**

Run: `task pr-prep`
Expected: All checks pass — lint, fmt, unit tests, integration tests, E2E tests

- [ ] **Step 2: Fix any failures**

Address lint warnings, test failures, or compilation issues.

- [ ] **Step 3: Final commit if needed**

Commit any fixes from the pr-prep run.
