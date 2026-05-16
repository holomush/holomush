# Use suiteT capture pattern instead of GinkgoT() for testing.TB

**Date:** 2026-05-16
**Status:** Accepted
**Decision:** holomush-1f1w
**Deciders:** Sean Brandt

## Context

Go 1.26's `testing.TB` interface contains an unexported `private()` method.
This is intentional Go design: it prevents any third-party type — including
`ginkgo.FullGinkgoTInterface` (the type returned by `GinkgoT()`) — from
satisfying `testing.TB`. The constraint is enforced at compile time, not
documented as a lint or runtime warning.

The original Canonical Pattern A in
`docs/superpowers/specs/2026-05-15-testify-ginkgo-migration-completion-design.md`
prescribed passing `GinkgoT()` to helpers taking `testing.TB`, and Task A1 of
the corresponding plan shipped a prerequisite errutil widening from
`*testing.T` to `testing.TB` (PR #3950) on that premise. That prescription
was discovered to be structurally unsatisfiable during execution of the
Ginkgo migration: any spec attempting to compile `helper(GinkgoT())` against
a `testing.TB`-typed parameter fails to build.

The codebase already contained ~30 Ginkgo specs that worked correctly before
the migration. Inspection of those specs surfaced an existing alternative
pattern that does NOT depend on `testing.TB` satisfaction.

## Decision

Every Ginkgo suite bootstrap in this codebase MUST:

1. Declare `var suiteT *testing.T` at package level.
2. Capture `suiteT = t` inside the `TestX` entry function BEFORE
   `RunSpecs(t, "...")`.
3. Spec bodies (`It(...)`, `BeforeEach(...)`, etc.) MUST pass `suiteT` (a
   real `*testing.T`) to any helper that takes `*testing.T` or `testing.TB`.

DO NOT use `GinkgoT()` to satisfy `*testing.T` or `testing.TB` parameters.
`GinkgoT()` remains valid for Ginkgo's own machinery (`AddReportEntry`,
`DeferCleanup` invocations through the Ginkgo interface) — it just cannot
adapt to standard-library testing types.

## Rationale

- **Compile correctness:** `*testing.T` satisfies both `*testing.T` and
  `testing.TB` parameter types with no interface gymnastics or shim layer.
- **Convention precedent:** ~30 pre-existing Ginkgo specs in the codebase
  already use this pattern; codifying it makes the convention explicit
  rather than implicit, and gives future migrations a documented anchor.
- **No helper changes required:** every helper that accepts `*testing.T`
  (e.g., `errutil.AssertErrorCode`, `testutil.SharedPostgres`,
  `testutil.FreshDatabase`) continues to work unchanged.
- **Documented Go behavior:** the `private()` method on `testing.TB` is a
  deliberate stdlib design choice (see Go issue tracker discussions on
  testing.TB extensibility). Working around it via wrappers would fight
  the language.

## Alternatives Considered

### `GinkgoT()` passed directly to `testing.TB` helpers (REJECTED)

**Strengths:** Idiomatic-looking Ginkgo pattern; documented in Ginkgo's own
FAQ and tutorials.

**Weaknesses:** Fails to compile under Go 1.26+. `ginkgo.FullGinkgoTInterface`
implements every exported method on `testing.TB` but cannot satisfy the
unexported `private()` method. Any widening shim (e.g., the errutil
`testing.T → testing.TB` widening from PR #3950) is harmless but useless for
this purpose — it does not enable `GinkgoT()` to satisfy `testing.TB`.

### `suiteT` capture pattern (ACCEPTED)

**Strengths:** Uses a real `*testing.T`; satisfies both `*testing.T` and
`testing.TB` parameters; pattern already in use across ~30 existing specs;
no interface gymnastics.

**Weaknesses:** `suiteT` is suite-scoped (it's the `*testing.T` from the
suite entry func, not from any individual spec). Callers MUST use Ginkgo's
`DeferCleanup` for spec-scoped cleanup and explicit `os.Setenv` + restore
for spec-scoped env mutation, NOT `suiteT.Cleanup` or `suiteT.Setenv`. This
constraint is documented alongside the pattern.

## Consequences

### Positive

- All Ginkgo specs compile and run correctly under Go 1.26+.
- No shim or wrapper type needed for `testing.TB` compatibility.
- Consistent with pre-existing specs that contributors already read as
  examples.
- Future Ginkgo migrations have a known-good pattern; no rediscovery cost.

### Negative

- `suiteT.Cleanup` and `suiteT.Setenv` are suite-scoped and silently leak
  across specs within a `Describe`. Contributors MUST know to use
  `DeferCleanup` for cleanup and per-spec `os.Setenv` + restore for env
  isolation. PR #4015 demonstrates the leak symptom and fix.
- The pattern is invisible to contributors who consult only Ginkgo's
  upstream documentation (which still prescribes `GinkgoT()`).

### Neutral

- The errutil widening shipped via PR #3950 is harmless and remains valid
  for any non-Ginkgo caller passing `*testing.B` or `*testing.F`. It just
  does not solve the Ginkgo case it was originally prescribed for.

## References

- Canonical worked example: PR #3951
  (`test/integration/plugin/plugin_role_permissions_test.go`,
  `test/integration/plugin/plugin_suite_test.go`)
- Spec-scoped cleanup demonstration: PR #4015 (`specTB` adapter,
  `freshBus()`, `freshPool()` no-arg helpers in
  `test/integration/eventbus_e2e/suite_test.go`)
- Superseded design doc:
  `docs/superpowers/specs/2026-05-15-testify-ginkgo-migration-completion-design.md`
- Closed epic: holomush-rccc
