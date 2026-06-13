<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin Capability-Declaration Enforcement (binary half) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a binary plugin fail to load (fail-closed) when its code can consume a non-exempt `host.v1` capability its manifest does not declare, bringing binary plugins to parity with Lua's declaration-driven wiring.

**Architecture:** The host passes the manifest's declared capabilities to the binary plugin via a new `ServiceConfig.declared_capabilities` proto field; the plugin SDK's `Init` validates — before injecting any host-capability client — that every non-exempt capability token granted by an implemented `*Aware` interface is declared, returning `CAPABILITY_NOT_DECLARED` (which fails `Init` → fails load) otherwise. Validation runs before injection, so injection is reached only for validated declarations (the spec's gate+validate, realized as one validation pass). The host calls `Init` unconditionally for binary plugins so the validation always runs. The existing wholesystem census becomes the integration guard.

**Tech Stack:** Go, Protocol Buffers (`buf`), `oops` errors, `manifest.RequiredCapabilities()`, testify.

**Spec:** `docs/superpowers/specs/2026-06-13-plugin-capability-declaration-enforcement-design.md`
**Design bead:** holomush-si3zs · **Invariants:** INV-PLUGIN-54 (binary, bound by this plan) / INV-PLUGIN-55 (Lua, pending → eykuh.4)

---

## File Structure

- `api/proto/holomush/plugin/v1/plugin.proto` — add `declared_capabilities` to `ServiceConfig` (Task 1).
- `pkg/plugin/*.pb.go` — regenerated (Task 1).
- `pkg/plugin/capability_declaration.go` (Create) — the `*Aware`→token requirement registry + `validateDeclaredCapabilities` (Task 2).
- `pkg/plugin/capability_declaration_test.go` (Create) — unit tests for validation; **binds INV-PLUGIN-54** (Task 2).
- `pkg/plugin/sdk.go` — wire validation into `pluginServerAdapter.Init` before injection (Task 3).
- `pkg/plugin/capability_declaration_registry_test.go` (Create) — registry-completeness meta-test (Task 4).
- `internal/plugin/goplugin/host.go` — populate `declared_capabilities`; make `Init` unconditional for binary plugins (Task 5).
- `internal/plugin/goplugin/host_test.go` — update/remove `manifestNeedsInit` tests (Task 5).
- `docs/architecture/invariants.yaml` + `docs/architecture/invariants.md` — register INV-PLUGIN-54/55 + regen (Task 6).
- `test/integration/wholesystem/census_test.go` — positive guard assertion (Task 7).

---

## Task 1: Add `declared_capabilities` to the `ServiceConfig` proto

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto:54-66`
- Regenerate: `pkg/plugin/v1/*.pb.go` (via `buf`)

- [ ] **Step 1: Add the field** to the `ServiceConfig` message, after `plugin_config = 3`:

```proto
  // Capability tokens the plugin declared in its manifest `requires:`
  // (manifest.RequiredCapabilities()). The plugin SDK validates, at Init, that
  // every non-exempt host capability its code can consume (via an implemented
  // *Aware interface) appears here, failing load otherwise (INV-PLUGIN-54).
  repeated string declared_capabilities = 4;
```

- [ ] **Step 2: Regenerate proto bindings**

Run: `task proto:generate` (or `buf generate` per the repo's proto task)
Expected: `pkg/plugin/.../service.pb.go` (or equivalent) gains `ServiceConfig.DeclaredCapabilities []string` + `GetDeclaredCapabilities()`.

- [ ] **Step 3: Verify the generated accessor exists**

Run: `rg -n 'func \(x \*ServiceConfig\) GetDeclaredCapabilities' pkg/plugin`
Expected: one match.

- [ ] **Step 4: Verify proto lint passes**

Run: `task lint:proto`
Expected: PASS (the new field has a Go-grounded doc comment; no name-echo).

- [ ] **Step 5: Commit**

`jj commit -m "feat(plugin): add ServiceConfig.declared_capabilities proto field (holomush-si3zs)"`

---

## Task 2: SDK validation registry + `validateDeclaredCapabilities`

**Files:**

- Create: `pkg/plugin/capability_declaration.go`
- Test: `pkg/plugin/capability_declaration_test.go`

- [ ] **Step 1: Write the failing test** (`pkg/plugin/capability_declaration_test.go`)

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// focusOnlyProvider implements FocusClientAware (grants focus + stream.history).
type focusOnlyProvider struct{ ServiceProvider }

func (focusOnlyProvider) SetFocusClient(FocusClient) {}

// Verifies: INV-PLUGIN-54
func TestValidateDeclaredCapabilitiesFailsClosedOnUndeclared(t *testing.T) {
	// FocusClientAware grants focus + stream.history; declaring only focus must fail.
	err := validateDeclaredCapabilities(focusOnlyProvider{}, []string{"focus"})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CAPABILITY_NOT_DECLARED")
	assert.Contains(t, err.Error(), "stream.history")
}

// Verifies: INV-PLUGIN-54
func TestValidateDeclaredCapabilitiesPassesWhenAllDeclared(t *testing.T) {
	err := validateDeclaredCapabilities(focusOnlyProvider{}, []string{"focus", "stream.history"})
	require.NoError(t, err)
}

// emitOnlyProvider implements EventSinkAware (emit is exempt — no declaration needed).
type emitOnlyProvider struct{ ServiceProvider }

func (emitOnlyProvider) SetEventSink(EventSink) {}

// Verifies: INV-PLUGIN-54
func TestValidateDeclaredCapabilitiesExemptNeedsNoDeclaration(t *testing.T) {
	err := validateDeclaredCapabilities(emitOnlyProvider{}, nil)
	require.NoError(t, err, "emit is self-gated (exempt); needs no declaration")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestValidateDeclaredCapabilities ./pkg/plugin/`
Expected: FAIL — `validateDeclaredCapabilities` undefined.

- [ ] **Step 3: Write the implementation** (`pkg/plugin/capability_declaration.go`)

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import "github.com/samber/oops"

// capabilityRequirement pairs a predicate reporting whether a provider opts into
// a host capability (by implementing its *Aware interface) with the non-exempt
// capability tokens that opt-in requires the manifest to declare. Exempt
// capabilities (emit, command-registry) carry an empty tokens slice: opting in
// is allowed with no declaration because they are self-gated (emit fence /
// host-vouched dispatch subject), matching hostcap.declarationExemptCapabilities.
type capabilityRequirement struct {
	awareName  string          // *Aware interface name, for error messages
	implements func(any) bool  // does the provider implement the interface?
	tokens     []string        // non-exempt capability tokens it grants
}

// hostCapabilityRequirements is the single source of truth mapping each
// host-capability *Aware interface to the capability tokens a provider
// implementing it MUST declare in its manifest requires: (INV-PLUGIN-54).
// FocusClientAware grants BOTH focus and stream.history (one interface backed by
// FocusServiceClient + StreamHistoryServiceClient; pkg/plugin/focus_client.go).
var hostCapabilityRequirements = []capabilityRequirement{
	{"EventSinkAware", func(p any) bool { _, ok := p.(EventSinkAware); return ok }, nil}, // emit: exempt
	{"FocusClientAware", func(p any) bool { _, ok := p.(FocusClientAware); return ok }, []string{"focus", "stream.history"}},
	{"HostEvaluatorAware", func(p any) bool { _, ok := p.(HostEvaluatorAware); return ok }, []string{"eval"}},
	{"SettingsClientAware", func(p any) bool { _, ok := p.(SettingsClientAware); return ok }, []string{"settings"}},
	{"SnapshotDecryptorAware", func(p any) bool { _, ok := p.(SnapshotDecryptorAware); return ok }, []string{"audit"}},
	{"CommandListerAware", func(p any) bool { _, ok := p.(CommandListerAware); return ok }, nil}, // command-registry: exempt
}

// validateDeclaredCapabilities returns a CAPABILITY_NOT_DECLARED error when the
// provider implements a host-capability *Aware interface for a non-exempt
// capability token absent from declared. Fail-closed: any undeclared token fails
// plugin Init (and thus load), the host-side load-time half of INV-PLUGIN-54.
func validateDeclaredCapabilities(provider any, declared []string) error {
	declaredSet := make(map[string]bool, len(declared))
	for _, c := range declared {
		declaredSet[c] = true
	}
	for _, req := range hostCapabilityRequirements {
		if !req.implements(provider) {
			continue
		}
		for _, tok := range req.tokens {
			if !declaredSet[tok] {
				return oops.Code("CAPABILITY_NOT_DECLARED").
					With("capability", tok).
					With("aware_interface", req.awareName).
					Errorf("plugin implements %s but did not declare capability %q in its manifest requires:", req.awareName, tok)
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestValidateDeclaredCapabilities ./pkg/plugin/`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

`jj commit -m "feat(plugin): SDK capability-declaration validation registry (INV-PLUGIN-54, holomush-si3zs)"`

---

## Task 3: Wire validation into `pluginServerAdapter.Init`

**Files:**

- Modify: `pkg/plugin/sdk.go:157-205` (the `Init` method, before the injection block)
- Test: `pkg/plugin/service_test.go` (extend with a validation-failure case)

- [ ] **Step 1: Write the failing test** (append to `pkg/plugin/service_test.go`)

```go
// focusUndeclaredProvider implements FocusClientAware but the InitRequest will
// declare no capabilities — Init must fail closed (INV-PLUGIN-54).
type focusUndeclaredProvider struct{ focusClient FocusClient }

func (p *focusUndeclaredProvider) SetFocusClient(c FocusClient) { p.focusClient = c }
func (p *focusUndeclaredProvider) Init(context.Context, *pluginv1.ServiceConfig) error { return nil }

// RegisterServices satisfies ServiceProvider (service.go:46) — the registrar is
// grpc.ServiceRegistrar, NOT *grpc.Server. Mirrors the existing dualAwareProvider.
func (p *focusUndeclaredProvider) RegisterServices(grpc.ServiceRegistrar) {}

// Verifies: INV-PLUGIN-54
func TestPluginServerAdapterInitFailsClosedOnUndeclaredCapability(t *testing.T) {
	adapter := &pluginServerAdapter{serviceProvider: &focusUndeclaredProvider{}}
	_, err := adapter.Init(context.Background(), &pluginv1.InitRequest{
		Config: &pluginv1.ServiceConfig{DeclaredCapabilities: nil}, // declares nothing
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CAPABILITY_NOT_DECLARED")
}
```

(Ensure `pkg/errutil` and `pluginv1` are imported in the test file; both are already used in `pkg/plugin`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestPluginServerAdapterInitFailsClosedOnUndeclaredCapability ./pkg/plugin/`
Expected: FAIL — Init returns nil error (validation not yet wired); injection of focus client also panics on a nil broker, so the test may fail on the wrong path — that is acceptable for the red step (it is not yet returning CAPABILITY_NOT_DECLARED).

- [ ] **Step 3: Add the validation call** in `pkg/plugin/sdk.go::Init`, immediately after `config` is resolved (after the block ending `config = req.GetConfig()` / the `var config` assignment around line 158-162) and **before** the `wantsSink/...` type-assertions:

```go
	// Fail closed at load: a provider that implements a host-capability *Aware
	// interface for a non-exempt capability it did not declare must not load
	// (INV-PLUGIN-54). Validation precedes injection, so a client is only ever
	// wired for a validated declaration — the spec's gate+validate in one pass.
	if a.serviceProvider != nil {
		if err := validateDeclaredCapabilities(a.serviceProvider, req.GetConfig().GetDeclaredCapabilities()); err != nil {
			return nil, oops.With("phase", "init").Wrap(err)
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestPluginServerAdapterInit ./pkg/plugin/`
Expected: PASS — the new failure test plus the existing injection tests (which declare the capabilities their providers use, or use exempt-only providers).

- [ ] **Step 5: Verify no existing pkg/plugin test regressed**

Run: `task test -- ./pkg/plugin/`
Expected: PASS. If an existing injection test (e.g. `TestPluginServerAdapterInitInjectsBothEventSinkAndFocusClient`) now fails because its provider implements `FocusClientAware` without declaring `focus`/`stream.history`, add `DeclaredCapabilities: []string{"focus", "stream.history"}` to that test's `InitRequest.Config` (EventSink is exempt). Fix each such test minimally.

- [ ] **Step 6: Commit**

`jj commit -m "feat(plugin): validate declared capabilities at SDK Init, fail closed (INV-PLUGIN-54, holomush-si3zs)"`

---

## Task 4: Registry-completeness meta-test

**Files:**

- Create: `pkg/plugin/capability_declaration_registry_test.go`

Prevents drift: every host-capability `*Aware` interface in `pkg/plugin` must appear in `hostCapabilityRequirements`, and every non-exempt token it lists must be a real capability token.

- [ ] **Step 1: Write the test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// canonicalNonExemptTokens mirrors the non-exempt host-capability tokens in
// internal/plugin/capability_vocab.go (CapabilityServiceNames) minus the exempt
// set (emit, command-registry). It is a literal here ON PURPOSE: pkg/plugin must
// NOT import internal/plugin (internal/plugin imports pkg/plugin — importing back
// would cycle). If a capability token is added to the canonical vocab, add it
// here too; this list is the in-package drift guard for the requirements map.
var canonicalNonExemptTokens = map[string]bool{
	"world.query": true, "world.mutation": true, "property": true,
	"session": true, "session.admin": true, "focus": true, "eval": true,
	"settings": true, "kv": true, "stream.history": true,
	"stream.subscription": true, "audit": true,
}

// TestHostCapabilityRequirementsTokensAreCanonical asserts every token listed in
// the requirements map is a known non-exempt capability token.
func TestHostCapabilityRequirementsTokensAreCanonical(t *testing.T) {
	for _, req := range hostCapabilityRequirements {
		for _, tok := range req.tokens {
			assert.Truef(t, canonicalNonExemptTokens[tok],
				"%s lists token %q which is not a known non-exempt capability token", req.awareName, tok)
		}
	}
}

// TestHostCapabilityRequirementsCoverKnownAwareNames is a guard list: the set of
// *Aware interface names in hostCapabilityRequirements must equal the known set.
// Adding a new SetXxx host-client injection in sdk.go without a registry row
// fails this test.
func TestHostCapabilityRequirementsCoverKnownAwareNames(t *testing.T) {
	got := map[string]bool{}
	for _, req := range hostCapabilityRequirements {
		got[req.awareName] = true
	}
	want := []string{
		"EventSinkAware", "FocusClientAware", "HostEvaluatorAware",
		"SettingsClientAware", "SnapshotDecryptorAware", "CommandListerAware",
	}
	for _, name := range want {
		assert.Truef(t, got[name], "hostCapabilityRequirements missing %s", name)
	}
	assert.Lenf(t, got, len(want), "hostCapabilityRequirements has an unexpected *Aware row; update want[] and the sdk.go injection block together")
}
```

- [ ] **Step 2: Run the test**

Run: `task test -- -run TestHostCapabilityRequirements ./pkg/plugin/`
Expected: PASS.

- [ ] **Step 3: Commit**

`jj commit -m "test(plugin): registry-completeness meta-test for capability requirements (holomush-si3zs)"`

---

## Task 5: Host populates `declared_capabilities` + unconditional binary Init

**Files:**

- Modify: `internal/plugin/goplugin/host.go:854-857` (InitRequest construction) and `:363` / `:850` (the `needsInit` gate)
- Modify: `internal/plugin/goplugin/host_test.go` (manifestNeedsInit tests)

- [ ] **Step 1: Write the failing test** (append to `internal/plugin/goplugin/host_test.go`)

Use the existing inline mock-factory harness from this file (the exact pattern at
`host_test.go:1175-1206`, `TestLoadCallsInitForPostgresStoragePlugin`):
`mockGRPCPluginClient` → `mockPluginClient{protocol: &mockClientProtocol{pluginClient: …}}`
→ `NewHostWithFactory(&mockClientFactory{client: …})` → `createTempExecutable` →
`host.Load(...)`, then assert on `grpcClient.initReq` / `grpcClient.initCalled`.

```go
// Verifies: INV-PLUGIN-54
func TestLoadPassesDeclaredCapabilitiesToInit(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	host := NewHostWithFactory(&mockClientFactory{client: mockClient})

	ctx := context.Background()
	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(tmpDir+"/test-plugin"))

	manifest := &plugins.Manifest{
		Name: "test-plugin", Version: "1.0.0", Type: plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{Executable: "test-plugin"},
		Requires: []plugins.Dependency{
			{Kind: plugins.DependencyCapability, Name: "focus"},
			{Kind: plugins.DependencyCapability, Name: "stream.history"},
		},
	}

	require.NoError(t, host.Load(ctx, manifest, tmpDir))
	require.NotNil(t, grpcClient.initReq, "Init must be called")
	require.NotNil(t, grpcClient.initReq.Config, "ServiceConfig must be set")
	assert.ElementsMatch(t, []string{"focus", "stream.history"},
		grpcClient.initReq.Config.GetDeclaredCapabilities())
}

// Verifies: INV-PLUGIN-54 — unconditional Init: a binary plugin with no
// requires/storage/config still gets Init, so the SDK capability validation
// always runs (closes the degenerate "declares nothing" escape).
func TestLoadCallsInitForBinaryPluginWithNoRequires(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}
	host := NewHostWithFactory(&mockClientFactory{client: mockClient})

	ctx := context.Background()
	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(tmpDir+"/bare-plugin"))

	manifest := &plugins.Manifest{
		Name: "bare-plugin", Version: "1.0.0", Type: plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{Executable: "bare-plugin"},
	}

	require.NoError(t, host.Load(ctx, manifest, tmpDir))
	assert.True(t, grpcClient.initCalled, "Init must be called even with no requires (INV-PLUGIN-54)")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run 'TestLoadPassesDeclaredCapabilitiesToInit|TestLoadCallsInitForBinaryPluginWithNoRequires' ./internal/plugin/goplugin/`
Expected: FAIL — `TestLoadPasses…` fails (`GetDeclaredCapabilities()` empty, host not populating yet); `TestLoadCallsInitForBinaryPluginWithNoRequires` fails (`manifestNeedsInit` gates Init out for a no-requires plugin → `initCalled` false). Both red drive Steps 3-4.

- [ ] **Step 3: Populate the field** — in `internal/plugin/goplugin/host.go`, the InitRequest construction (currently lines 855-857):

```go
		initReq := &pluginv1.InitRequest{
			Config: &pluginv1.ServiceConfig{
				RequiredServices:     requiredServices,
				DeclaredCapabilities: manifest.RequiredCapabilities(),
			},
		}
```

- [ ] **Step 4: Make Init unconditional for binary plugins** — replace `needsInit := manifestNeedsInit(manifest)` (around line 850) with:

```go
	// All binary plugins are Init'd so the SDK validates declared capabilities
	// even when the manifest has no requires/storage/config/emits — without this
	// a plugin implementing a capability *Aware interface but declaring nothing
	// would skip Init and escape the INV-PLUGIN-54 load-time check.
	needsInit := true
```

Then check for other callers of `manifestNeedsInit`:

Run: `rg -n 'manifestNeedsInit' internal/`

- If the only references are its definition + this call site + its tests: delete the `manifestNeedsInit` function (host.go:359-369) and its dedicated tests, leaving `needsInit := true`.
- If referenced elsewhere: keep the function but set `needsInit := true` at this call site and note why.

- [ ] **Step 5: Update `manifestNeedsInit` tests** — remove/replace any test asserting Init is skipped for trivial binary plugins (those now always Init). Run:

Run: `rg -n 'manifestNeedsInit|needsInit|Init.*not.*called|expected.*no.*init' internal/plugin/goplugin/host_test.go`
Fix each: delete the skip-Init assertion or invert it to expect Init is now always called.

- [ ] **Step 6: Run tests to verify they pass**

Run: `task test -- ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 7: Commit**

`jj commit -m "feat(plugin): host passes declared_capabilities + always Init binary plugins (INV-PLUGIN-54, holomush-si3zs)"`

---

## Task 6: Register INV-PLUGIN-54 (bound) + INV-PLUGIN-55 (pending)

**Files:**

- Modify: `docs/architecture/invariants.yaml`
- Regenerate: `docs/architecture/invariants.md` (via `go run ./cmd/inv-render`)

- [ ] **Step 1: Add both entries** to `docs/architecture/invariants.yaml` (match the existing INV-PLUGIN entry schema exactly — copy a neighbor like INV-PLUGIN-50 for field shape):

```yaml
  - id: INV-PLUGIN-54
    scope: PLUGIN
    origin_spec: docs/superpowers/specs/2026-06-13-plugin-capability-declaration-enforcement-design.md
    summary: >-
      A binary plugin's Init fails closed when its provider implements a
      host-capability *Aware interface for a non-exempt capability absent from
      the manifest; capability clients are injected only for declared
      capabilities. emit and command-registry are self-gated and exempt.
    binding: bound
    asserted_by:
      - pkg/plugin/capability_declaration_test.go
      - pkg/plugin/service_test.go
  - id: INV-PLUGIN-55
    scope: PLUGIN
    origin_spec: docs/superpowers/specs/2026-06-13-plugin-capability-declaration-enforcement-design.md
    summary: >-
      A Lua plugin is wired only the capabilities its manifest declares
      (declaration-gated host-cap bridge). Pending until holomush-eykuh.4
      migrates production Lua off the legacy hostfunc shim.
    binding: pending
```

(Do **not** add `asserted_by` to INV-PLUGIN-55 — the meta-test rejects a `pending` entry with provenance.)

- [ ] **Step 2: Regenerate the rendered table**

Run: `go run ./cmd/inv-render`
Expected: `docs/architecture/invariants.md` updates inside the generated regions with INV-PLUGIN-54/55 rows.

- [ ] **Step 3: Run the registry meta-tests**

Run: `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`
Expected: PASS — INV-PLUGIN-54 bound with genuine asserting tests; INV-PLUGIN-55 pending with no provenance.

- [ ] **Step 4: Commit**

`jj commit -m "docs(arch): register INV-PLUGIN-54 (bound) + INV-PLUGIN-55 (pending) (holomush-si3zs)"`

---

## Task 7: Wholesystem census — positive guard confirmation

**Files:**

- Modify: `test/integration/wholesystem/census_test.go`

The census already loads every in-tree plugin via the real `Manager.LoadAll`. With Task 3+5, a misdeclared in-tree binary plugin now fails load → the census fails. Confirm the current (correctly-declared) tree still loads, and document that the census is the integration guard for INV-PLUGIN-54.

- [ ] **Step 1: Add an assertion/comment** affirming the guard (the census already asserts all plugins load; add a focused comment + assertion that `core-scenes` is among the loaded set, since it is the plugin exercising the most capabilities):

```go
	// INV-PLUGIN-54: loading every in-tree plugin through the real path now also
	// validates that each binary plugin declared the host capabilities its code
	// consumes — a misdeclared plugin fails Init → fails load → fails this census.
	loaded := srv.PluginManager().ListPlugins()
	Expect(loaded).To(ContainElement("core-scenes"),
		"core-scenes (heaviest capability consumer) must load with its declared capabilities")
```

This is a Ginkgo/gomega suite (`srv.PluginManager().ListPlugins()` + `ContainElement`, matching the existing "loads every in-tree plugin" `It` block at `census_test.go:43-46`) — not testify.

- [ ] **Step 2: Run the census**

Run: `task test:int -- ./test/integration/wholesystem/`
Expected: PASS (the current tree declares all capabilities correctly, from PR #4434).

- [ ] **Step 3: Commit**

`jj commit -m "test(plugin): wholesystem census guards INV-PLUGIN-54 capability declaration (holomush-si3zs)"`

---

## Final verification

- [ ] **Full unit suite:** `task test` → PASS
- [ ] **Affected integration:** `task test:int -- ./test/integration/wholesystem/ ./test/integration/scenes/` → PASS
- [ ] **Lint:** `task lint` → PASS
- [ ] **pr-prep fast lane:** `task pr-prep` → status=pass
- [ ] **Confirm INV-PLUGIN-54 binding:** `task test -- -run 'TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` → PASS

## Notes on spec alignment

- **Gate+validate** (spec §3.1): realized as validation-before-injection (Task 3) — injection is reached only after validation succeeds, so a client is never wired for an undeclared capability. No per-injection conditional is needed; this is the gate, structurally.
- **Unconditional binary Init** (Task 5) is a grounding-discovered refinement of spec §3.2 (recorded on holomush-si3zs): without it a binary plugin declaring nothing would skip Init and escape the check.
- **Negative arm** (spec §3.6): covered by the SDK unit test (Task 2/3) rather than a new under-declaring fixture binary plugin (heavier; the unit test exercises the same validation path directly). The census (Task 7) is the positive integration guard.
- **Lua half** (INV-PLUGIN-55): out of scope; delivered by holomush-eykuh.4.
<!-- adr-capture: sha256=97f0ac7cadb0315b; session=cli; ts=2026-06-13T18:52:12Z; adrs=holomush-m4ac3,holomush-toh7a,holomush-1psri,holomush-wlyzs,holomush-nk46j -->
