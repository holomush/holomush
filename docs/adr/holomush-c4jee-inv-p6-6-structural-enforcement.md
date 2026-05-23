<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# INV-P6-6 (No ABAC Engine in Participant Publication RPCs) Enforced by AST Import Scan and Reflect Field Check

**Date:** 2026-05-23
**Status:** Accepted
**Decision:** holomush-c4jee
**Deciders:** HoloMUSH Contributors
**Related:** [`holomush-qd3r5`](holomush-qd3r5-two-pair-rpc-no-shared-gate-path.md), [`holomush-c8a9`](holomush-c8a9-scene-privacy-plugin-code-enforcement.md), [`holomush-nt2d`](holomush-nt2d-participant-gate-pattern-generalized.md)

## Context

INV-S9 (substrate-contract §4.1) requires that the hard privacy boundary for scenes be plugin-code-enforced, not ABAC-engine-enforced. Phase 6 inherits this invariant for the new publication RPCs and codifies it as INV-P6-5 (gate runs before content read) and INV-P6-6 (ABAC engine MUST NOT be called during participant-gated publication RPC handlers).

INV-P6-5 is verifiable via a call-stack tripwire test: a mock store with a content-read counter; the handler under test must deny non-participants before the counter increments. This pattern is established at `audit_test.go:229` (`TestQueryHistoryDeniesNonMemberWithoutHittingLogStore`) and is straightforward to extend to the new RPCs.

INV-P6-6 is harder to test. Two approaches were considered:

1. **Runtime injection via functional option.** Add `WithABACEngine(recordingEngine)` to `NewSceneServiceImpl`; tests inject a recording engine and assert `engine.EvaluateCalls() == 0` after the handler returns. Mirrors the pattern used in some other parts of the codebase for ABAC-dependent code.

2. **Structural enforcement.** Two static tests: a `go/parser` AST import scan asserts that `publish_service.go` imports no ABAC policy package; a `reflect` field-type enumeration asserts that `SceneServiceImpl` has no field whose type is (or contains) an ABAC engine interface. No production API change.

The functional-option approach changes production code to accommodate a test concern. Worse, it introduces a code path — `s.abacEngine` (or equivalent) — that a future contributor could mistakenly wire to a real engine, breaking the invariant. A subtle change to the test mock would not catch this; the test asserts on the recorder's behavior, not on the absence of the field.

The structural approach is purely additive: the tests read production code without modifying it. The production API surface stays narrow; there is no field on `SceneServiceImpl` capable of holding an ABAC engine, so the invariant is enforced by absence rather than by behavioral assertion.

## Decision

INV-P6-6 is verified by two tests in `plugins/core-scenes/service_publish_gate_test.go`:

### Test 1: AST import scan

```go
func TestPublicationServiceFileImportsNoABACPolicyPackage(t *testing.T) {
    src, err := os.ReadFile("publish_service.go")
    require.NoError(t, err)
    fset := token.NewFileSet()
    f, err := parser.ParseFile(fset, "publish_service.go", src, parser.ImportsOnly)
    require.NoError(t, err)
    for _, imp := range f.Imports {
        path := strings.Trim(imp.Path.Value, `"`)
        require.False(t, strings.HasPrefix(path, "github.com/holomush/holomush/internal/access/policy"),
            "INV-P6-6 violation: publish_service.go imports ABAC policy package %s", path)
    }
}
```

### Test 2: Reflect field-type check

```go
func TestPublicationServiceTypeHasNoABACEngineField(t *testing.T) {
    typ := reflect.TypeOf(SceneServiceImpl{})
    for i := 0; i < typ.NumField(); i++ {
        f := typ.Field(i)
        require.NotContains(t, strings.ToLower(f.Type.String()), "policy.engine",
            "INV-P6-6 violation: field %s carries ABAC engine type %s", f.Name, f.Type.String())
    }
}
```

Neither test requires a constructor seam, a functional option, or any production-code change to enable verification. The production constructor signature `NewSceneServiceImpl(store sceneStorer)` is preserved (matches the existing one-parameter shape at `service.go:88`).

## Rationale

**Absence-of-capability is structurally stronger than absence-of-call.** A behavioral test (recording engine + zero-calls assertion) verifies that at the moment the test runs, the handler did not call the engine. A structural test (no import, no field) verifies that at the moment the test runs, the handler **cannot** call the engine — there is no code path that could reach it. The latter survives refactors that introduce a new call site; the former does not.

**Constructor seams introduce production risk.** A functional option `WithABACEngine(engine policy.Engine)` exists in production code even when only tests use it. A future contributor reading `service.go` sees the option, infers "this service can optionally consult ABAC," and adds a real engine. The test that catches this depends on someone re-running it — but the failure mode is "engine consulted, leaked content" before anyone notices the test regression. The structural approach has no such code path.

**The tests are purely additive.** `service_publish_gate_test.go` reads production code via `os.ReadFile` and `reflect.TypeOf`. Production code knows nothing about the tests. Removing the tests is the only way to weaken the invariant — and removal is a visible PR edit that a reviewer can catch.

**Parallels the existing INV-S9 test pattern.** `audit_test.go:229` (`TestQueryHistoryDeniesNonMemberWithoutHittingLogStore`) uses a tripwire mock store to assert call-stack ordering. This ADR's tests extend the pattern from runtime call-stack assertion to compile-time structural assertion. Both are CI-time regression locks; the structural variant runs faster (no test substrate) and catches a strictly larger class of violations.

## Alternatives Considered

**Option A: AST import scan + reflect field-type check (chosen).**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Invariant verified by structural absence (no code path can violate); production API unchanged; tests are purely additive; fast (no runtime substrate); regression-locks at CI time |
| Weaknesses | Doesn't cover dynamically-obtained engines (e.g., via `ctx.Value`, global registry, or reflection) — but such patterns would be unusual in this codebase and would themselves require explicit code addition |

**Option B: `WithABACEngine(engine)` functional option + recording engine in tests.**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Familiar functional-options pattern; tests have explicit injection point; runtime-call-count is directly assertable |
| Weaknesses | Changes production API surface to accommodate test concern; introduces a code path (`s.abacEngine`) that exists even when unused; a future contributor could mistakenly wire a real engine through the option; test failure surface is "ran but called the engine" which requires the test to run on every PR (versus structural check that fails at build time) |

**Option C: Build-tag-gated test seam.**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Test-only seam invisible to production builds |
| Weaknesses | Build-tag complexity for one invariant; debugging seam-related test failures is harder than debugging straightforward structural assertions; introduces a precedent that test-only build tags are acceptable, which complicates the build-tag policy |

## Consequences

- **Regression locked at CI time.** Any future PR that adds an `internal/access/policy` import to `publish_service.go` or adds an `policy.Engine`-typed field to `SceneServiceImpl` triggers an immediate test failure. The intent of the violator must be made explicit by removing the test — a visible, reviewable act.
- **No production API surface change.** The constructor signature is untouched. Phase 6 does not add a `policy.Engine` parameter or option to any constructor.
- **Tests are pure code-level checks.** They run as plain `go test`, no integration harness, no runtime substrate. Fast and reproducible.
- **Limitation: dynamic engines not covered.** If a future implementation obtained an ABAC engine through a different mechanism (e.g., loaded from `ctx.Value(...)`, or via a global registry), the structural tests would not catch it. Contributors must understand the scope: structural absence-of-field is the invariant being verified, not runtime call-site absence.
- **Combines with [`holomush-qd3r5`](holomush-qd3r5-two-pair-rpc-no-shared-gate-path.md).** The no-shared-code-path decision (qd3r5) plus structural enforcement (this ADR) together make INV-S9 regression resistance a multi-layer property: even if one layer is compromised by a future refactor, the other catches it.

## References

- [Scenes Phase 6 design spec](../superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md) §9, §9.4, INV-P6-5, INV-P6-6
- [Scenes Phase 6 implementation plan](../superpowers/plans/2026-05-23-scenes-phase-6-logs-vote-privacy.md) Task B5 (INV-S9 tripwire + structural tests)
- [Substrate-contract spec](../superpowers/specs/2026-05-16-social-spaces-substrate-contract.md) INV-S9
- Existing INV-S9 test pattern: `plugins/core-scenes/audit_test.go:229` (`TestQueryHistoryDeniesNonMemberWithoutHittingLogStore`)
- Design bead `holomush-5rh.20` (plan-review rounds 1-2 fix log — WithABACEngine option rejected in favor of structural approach)
