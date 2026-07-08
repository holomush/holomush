# Testing Patterns

**Analysis Date:** 2026-07-08

## Test Framework

**Unit tests:** stdlib `testing` + `testify` (`assert`/`require`). No config file needed; run via `task test`.

**Integration tests:** Ginkgo/Gomega BDD, build-tagged `//go:build integration`, run via `task test:int` (needs Docker for testcontainers).

**Mocks:** `mockery` (config `.mockery.yaml`), `with-expecter: true`, boilerplate file `.mockery-boilerplate.txt`. Regenerate with `task mocks` (wraps `mockery`).

**Run commands:**

```bash
task test                                          # unit tests (parent + gorules analyzer module)
task test -- ./internal/command/                   # scope to a package
task test -- -run TestCapability ./internal/command/  # scope to a test
task test:verbose                                  # full verbose output
task test:cover                                    # coverage report
task test:int                                      # integration tests (Docker)
task test:int:focus -- -ginkgo.focus="..."          # focused Ginkgo run
task test:e2e                                       # Playwright E2E (Docker)
task test:bats                                      # bats shell tests (scripts/tests/)
task test:bench                                     # benchmarks
task test:fuzz                                       # fuzz tests
```

`task test` does **not** compile `//go:build integration` files — refactors of shared types/interfaces MUST also run `task test:int`, or breakage is silent.

## Test File Organization

**Location:** co-located — `foo.go` → `foo_test.go` in the same package (e.g. `internal/core/character.go` / `internal/core/character_test.go`).

**Integration specs:** `test/integration/<domain>/`, each domain with a `*_suite_test.go` bootstrap:

```go
//go:build integration

package integration

import (
    "testing"
    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

var suiteT *testing.T

func TestIntegration(t *testing.T) {
    suiteT = t
    RegisterFailHandler(Fail)
    RunSpecs(t, "Integration Suite")
}
```

Example files: `test/integration/integration_suite_test.go`, `test/integration/admin_policy_chain_test.go`, `test/integration/settings/settings_suite_test.go`.

**Whole-system tier:** `test/integration/wholesystem/` — census suite asserting all in-tree plugins load and register `help` (`holomush-0f0f4`).

## Test Naming (ACE — Action, Condition, Expectation)

Test names are sentences, not vague labels. From `internal/core/character_test.go`, `internal/core/builtins_operator_read_test.go`, `internal/core/actor_context_test.go`:

```go
func TestCharacterRefStringReturnsName(t *testing.T)
func TestWithActorRoundTripsTheStampedActor(t *testing.T)
func TestActorFromContextReturnsFalseWhenAbsent(t *testing.T)
func TestEventStoreAdapterDoesNotUseStringWorldServiceLabel(t *testing.T)
```

| Pattern | Example |
|---|---|
| Good | `TestConfigDirUsesXDGEnvVarWhenSet` |
| Bad | `TestConfigDir_EnvVar`, `"success"`, `"error case"` |

**Functions with subtests:** parent name identifies the unit under test; subtests carry the sentence, lowercase:

```go
func TestHashPassword(t *testing.T) {
    t.Run("produces valid argon2id hash", func(t *testing.T) { ... })
    t.Run("rejects empty password", func(t *testing.T) { ... })
}
```

Exception to PascalCase-no-underscore: `TestType_Method` shape is allowed when paired with subtests (e.g. `TestEngine_Evaluate`).

## Test Structure

**Table-driven (canonical shape):**

```go
func TestEventType_String(t *testing.T) {
    tests := []struct {
        name     string
        input    EventType
        expected string
    }{
        {"returns say for event type say", EventTypeSay, "say"},
        {"returns pose for event type pose", EventTypePose, "pose"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.expected, tt.input.String())
        })
    }
}
```

**Assertions:** `require.NoError(t, err)` for setup preconditions; `assert.*` for the actual check under test. Prefer `errutil.AssertErrorCode` / `assert.ErrorIs` over string matching for error assertions. `assert.Equal`, `assert.Contains`, `assert.Error` in general use.

**Rules (`.claude/rules/testing.md`):**

- Every exported function needs at least one positive and one negative test.
- No zero-assertion "don't panic" tests.
- One behavior per test/subtest — split on "and".

## Invariant Bindings

When a test genuinely asserts a registry invariant, annotate immediately above it:

```go
// Verifies: INV-CRYPTO-28
func TestDecryptPluginRowFailClosedWithoutAuditEmitter(t *testing.T) { ... }
```

This is what flips `docs/architecture/invariants.yaml` entry from `binding: pending` to `bound`. Never annotate a test that only touches the code without proving the guarantee (false-green precedent: `holomush-0sh1k`/INV-RB-3).

## Mocking

**Framework:** `mockery` (`.mockery.yaml`), `with-expecter: true` — generates `EXPECT()`-style fluent mocks.

**Config shape** (`.mockery.yaml`):

```yaml
boilerplate-file: .mockery-boilerplate.txt
with-expecter: true
resolve-type-alias: false
packages:
  github.com/holomush/holomush/internal/core:
    config:
      dir: "{{.InterfaceDir}}/mocks"
      outpkg: mocks
    interfaces:
      EventStore:
```

Interfaces mocked span `internal/core` (`EventStore`), `internal/grpc` (`AuthServiceProvider`, `ResetServiceProvider`, `CharacterServiceProvider`), `internal/plugin` (`Host`, `EventEmitter`, `ServiceProxy`), `internal/plugin/hostfunc` (`KVStore`), `internal/world` (repositories → `internal/world/worldtest`, package `worldtest`), `internal/access/policy/types` (`AccessPolicyEngine` → `internal/access/policy/policytest`, package `policytest`), `internal/auth`, `internal/session`, `internal/totp`, and generated proto clients (e.g. `SceneServiceClient` → `internal/grpc/scenemocks`).

Note the two naming conventions: most interfaces land in a sibling `mocks` package (`outpkg: mocks`); a few domain packages get bespoke test-double package names (`worldtest`, `policytest`, `scenemocks`) for ergonomic call-site imports.

**Usage pattern:**

```go
pub := mocks.NewMockPublisher(t)
pub.EXPECT().Publish(mock.Anything, mock.Anything).Return(nil)
```

**Regenerate:** `task mocks` (wraps `mockery`).

## ABAC Test Engines (`policytest`)

```go
mockAccess := policytest.NewGrantEngine()
mockAccess.GrantCommandExecution(subject, "say", "look")  // Layer 1 grants
mockAccess.Grant(subject, "emit", "stream")               // Layer 2 / capability grants
```

Other engines: `AllowAllEngine()`, `DenyAllEngine()`, `NewErrorEngine(err)`, `NewInfraFailureEngine(t, reason, policyID)`.

## Fixtures and Factories

Test fixtures are built inline via struct literals using `core.NewULID()` / `idgen.New()` for IDs (never hardcoded strings), e.g. `CharacterRef{ID: NewULID(), Name: "Emerald_Zephyr", LocationID: NewULID()}` (`internal/core/character_test.go`). No separate global fixture-file convention observed; each package builds its own minimal fixtures near the test.

## Coverage

**Requirement:** MUST maintain >80% coverage per package (90%+ target for `internal/core`, `internal/world`). Verify with `task test:cover`.

## Test Types

**Unit tests:** package-local, no external dependencies, mock everything via mockery-generated doubles.

**Integration tests (Ginkgo/Gomega, `test/integration/`):** BDD `Describe`/`It` blocks map to user stories; async assertions use `Eventually`:

```go
Eventually(func() int {
    return len(results)
}).Should(Equal(expected))
```

Five sub-tiers: bus-integration (embedded NATS via `eventbustest`), audit-integration (+ Postgres testcontainer), full-stack integration (+ production `CoreServer`), whole-system plugin tier (`integrationtest.Start(t, integrationtest.WithInTreePlugins())` loads ALL in-tree plugins — requires `task plugin:build-all` binaries, automatic under `task test:int`).

**Integration harness:** `internal/testsupport/integrationtest/` — canonical in-process stack (Postgres + embedded NATS JetStream + production `CoreServer`), files: `harness.go`, `options.go`, `session.go`, `crypto.go`, `plugins.go`, `real_abac.go`, `subscribe_stream.go`, plus matching `*_test.go` (`harness_smoke_test.go`, `invariants_test.go`, `default_plugins_test.go`). Session-store tests need Docker even under `task test` — MUST use `sessiontest.NewStore(t)` (deliberate SharedPostgres exception).

**E2E:** Playwright, full Docker stack, driven through a real browser client — run via `task test:e2e` (`task test:e2e:cover` for coverage-instrumented binaries). "E2E" is reserved for this Playwright suite; the Ginkgo suite is always called "integration" regardless of stack depth.

Production code MUST NOT import `eventbustest` or `internal/core/coretest` — enforced by `depguard` in `.golangci.yaml`.

## Quarantine

Known-flaky integration/E2E specs self-skip in gating CI, running only nightly or with `HOLOMUSH_RUN_QUARANTINED=1`. Registry: `test/quarantine.yaml` — every entry needs `{id, kind (go|ginkgo|playwright), bead, since, reason}` and an open bead. Example (`test/quarantine.yaml`):

```yaml
entries:
  - id: TestProjectionResumesAfterRestart
    kind: go
    bead: holomush-q55b
    since: 2026-05-25
    reason: consumer-info eventual-consistency race on restart (5zpf closed as dup)
```

Three marker idioms: Go (`quarantinetest.Skip(t, "holomush-xxxx")`), Ginkgo (`if !quarantinetest.Enabled() { Skip("quarantined: holomush-xxxx") }`), Playwright (`{ tag: ['@quarantine', '@holomush-xxxx'] }`). Bijection enforced by `test/meta/quarantine_registry_test.go`; `task quarantine:audit` flags rows whose bead is closed. Quarantine is for flakiness with a known reproducible cause left unfixed only when the fix is deferred — if the root cause is *known* (contention, race, undersized CI), fix it, do not quarantine. Production code MUST NOT import `quarantinetest` (depguard-enforced).

## Async / CI-flake diagnosis

`Integration Test` failures with `connection refused` / `unexpected EOF` / testcontainer-start errors are usually a transient Postgres testcontainer drop under CI load, not a code defect — check whether the parent commit's CI also failed the same way before filing a bug or quarantining.

---

*Testing analysis: 2026-07-08*
