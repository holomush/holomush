<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

---
paths:
  - "**/*_test.go"
  - "test/**/*.go"
  - "**/testutil/**/*.go"
  - "**/policytest/**/*.go"
  - "**/eventbustest/**/*.go"
  - "**/mocks/**/*.go"
---

# Go Testing Rules

This file auto-loads when editing test files. The high-level testing posture is in `CLAUDE.md`; the operational detail lives here.

## Coverage

| Requirement | Description |
| ----------- | ----------- |
| **MUST** maintain >80% coverage | Per-package coverage must exceed 80% |
| **MUST** run `task test:cover` | Verify coverage before completing work |
| **SHOULD** target 90%+ coverage | For core packages (`internal/core`, `internal/world`) |

## Integration Tests and Refactoring

`task test` does NOT compile `//go:build integration` files. When refactoring shared types, interfaces, or packages, always run `task test:int` to catch breakage that unit tests miss.

## Test Files

- Tests live next to implementation: `foo.go` → `foo_test.go`
- Integration tests in `*_integration_test.go`
- Use build tags for integration tests: `//go:build integration`

## Test Naming (ACE)

Test names MUST be sentences that communicate behavior. Follow the ACE framework: **Action** (what), **Condition** (when/given), **Expectation** (then/result).

Reference: [Test Names Should Be Sentences](https://bitfieldconsulting.com/posts/test-names)

**Functions without subtests** — the function name itself is the sentence:

| Pattern | Example |
| ------- | ------- |
| Good | `TestConfigDirUsesXDGEnvVarWhenSet` |
| Good | `TestEnsureDirFailsWhenParentIsAFile` |
| Bad | `TestConfigDir_EnvVar` |
| Bad | `TestEnsureDir_Error` |

**Functions with subtests** — parent name identifies the unit under test, subtest names carry the sentence:

```go
func TestHashPassword(t *testing.T) {
    t.Run("produces valid argon2id hash", func(t *testing.T) { ... })
    t.Run("rejects empty password", func(t *testing.T) { ... })
}
```

| Requirement | Description |
| ----------- | ----------- |
| **MUST** follow ACE | Every test name communicates action, condition, and expectation |
| **MUST** use PascalCase | Top-level function names: `TestConfigDirFallsBackToHomeDotConfig` |
| **SHOULD NOT** use underscores | Exception: `TestType_Method` with subtests (e.g., `TestEngine_Evaluate`) |
| **MUST** use lowercase subtests | Subtest strings: `"returns ErrNotFound for missing character"` |
| **MUST NOT** use vague names | No `"success"`, `"error case"`, `"test 1"` |

## Table-Driven Tests

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

## Assertions

Use testify for unit test assertions:

```go
// Equality
assert.Equal(t, expected, got)

// Error checking
require.NoError(t, err)
assert.Error(t, err)

// Contains
assert.Contains(t, slice, element)
```

## Test Quality

| Requirement | Description |
| ----------- | ----------- |
| **MUST** test both paths | Every exported function needs at least one positive and one negative test |
| **MUST** assert behavior | No zero-assertion "don't panic" tests |
| **MUST** focus each test | One behavior per test/subtest — if it needs "and," split it |
| **SHOULD** use error codes | Prefer `errutil.AssertErrorCode` or `assert.ErrorIs` over string matching |
| **MUST** use `require` for preconditions | `require.NoError` for setup, `assert.*` for the check under test |

## Invariant Bindings (`// Verifies:`)

When a test genuinely asserts a named system invariant from the registry
(`docs/architecture/invariants.yaml`), annotate it so the registry can bind it:

```go
// Verifies: INV-CRYPTO-28
func TestDecryptPluginRowFailClosedWithoutAuditEmitter(t *testing.T) { ... }
```

| Requirement | Rule |
| ----------- | ---- |
| **MUST** annotate invariant-asserting tests | A test that proves an `INV-<SCOPE>-N` carries `// Verifies: INV-<SCOPE>-N` immediately above the test (or the assertion block). This is what flips the registry entry from `binding: pending` to `bound`. |
| **MUST NOT** annotate a test that does not prove it | A `// Verifies:` on a test that only touches the code is a false-green (the INV-RB-3 bug, `holomush-0sh1k`). No genuine assertion ⇒ leave it pending and file a coverage bug. |
| **SHOULD** read the rule | Full workflow (define / respect / bind / regenerate) is in `.claude/rules/invariants.md`. |

## Mockery

Generate mocks with mockery (config in `.mockery.yaml`):

```bash
mockery
```

Use generated mocks:

```go
pub := mocks.NewMockPublisher(t)
pub.EXPECT().Publish(mock.Anything, mock.Anything).Return(nil)
```

## Test Engine Helpers (ABAC)

Use `policytest.GrantEngine` for authorization in tests:

```go
mockAccess := policytest.NewGrantEngine()
mockAccess.GrantCommandExecution(subject, "say", "look") // Layer 1 grants
mockAccess.Grant(subject, "emit", "stream")              // Layer 2 / capability grants
```

Other test engines: `AllowAllEngine()`, `DenyAllEngine()`, `NewErrorEngine(err)`, `NewInfraFailureEngine(t, reason, policyID)`.

## Test Tiers

| Tier | Dependencies | Runner | Build tag |
| --- | --- | --- | --- |
| unit | none | `task test` | (none) |
| bus-integration | embedded NATS (`eventbustest`) | `task test:int` | `//go:build integration` |
| audit-integration | embedded NATS + Postgres testcontainer | `task test:int` | `//go:build integration` |
| full-stack integration | embedded NATS + Postgres + `CoreServer` (+ optional in-tree plugins via `WithInTreePlugins()`) | `task test:int` | `//go:build integration` |
| **E2E** | full Docker stack driven through a real client (browser) | `task test:e2e` | (Playwright) |

"E2E" means the Playwright browser suite — a test that crosses the real user
boundary. The Ginkgo `test:int` suite is **integration** (it calls Go/gRPC APIs
in-process), regardless of how much of the stack it stands up. Use "E2E" only
for the Playwright suite; Go Ginkgo suites are "integration".

**Whole-system plugin tier** (top Go-fidelity tier within full-stack integration):
`integrationtest.Start(t, integrationtest.WithInTreePlugins())` loads ALL in-tree
plugins via `setup.PluginSubsystem` → `Manager.LoadAll` (INV-5, INV-WS-1). The
`test/integration/wholesystem/` census suite (`holomush-0f0f4`) asserts the full
plugin set loads and the `help` command is registered. Requires binary artifacts
(`task plugin:build-all`; automatic in `task test:int`). See
`site/docs/contributing/integration-tests.md#whole-system-plugin-tier-withintreeplugins`
for the full capability doc.

`eventbustest` provides the in-process embedded NATS server (`MemoryStorage`)
used at every non-unit tier. This matches production, which also runs embedded
NATS (`internal/eventbus/subsystem.go`); external/clustered NATS is unimplemented
(tracked in holomush-s5ts). Production code MUST NOT import `eventbustest` or
`internal/core/coretest` — enforced by the depguard rule in `.golangci.yaml`.

### Quarantine

Known-flaky integration and E2E specs are quarantined so they self-skip in gating CI and in `task pr-prep:full`, running only nightly or locally with `HOLOMUSH_RUN_QUARANTINED=1`. Quarantine is for **flakiness with an open bead** — never for a real failure.

Quarantine is for **genuinely intermittent failures without a reproducible cause**. If the root cause is *known* — resource contention, a race, undersized CI infra — **fix it; do not quarantine it.** Quarantining a known-cause failure hides a real infrastructure problem instead of resolving it.

### Diagnosing CI-only integration failures

`Integration Test` failures showing `connection refused` / `unexpected EOF` / testcontainer-start errors are usually a **transient Postgres testcontainer drop under CI load**, not a code defect. Before filing a bug or quarantining: check whether the **parent commit's** CI also failed the same way, then re-run the job. Treat it as a real failure only if it reproduces on a clean parent.

**Three marker idioms:**

| Stack | Marker |
| --- | --- |
| Go unit/integration | `quarantinetest.Skip(t, "holomush-xxxx")` (helper at `internal/testsupport/quarantinetest`) |
| Ginkgo | `if !quarantinetest.Enabled() { Skip("quarantined: holomush-xxxx") }` |
| Playwright | `{ tag: ['@quarantine', '@holomush-xxxx'] }` |

Every marker MUST have a corresponding row in `test/quarantine.yaml` citing an open bead (enforced by the bijection meta-test INV-2 at `test/meta/quarantine_registry_test.go`). `task quarantine:audit` flags rows whose cited bead is closed.

Production code MUST NOT import `quarantinetest` — enforced by depguard.

To un-quarantine: fix the flake, remove the marker and the `test/quarantine.yaml` row, then verify `task quarantine:audit` is clean.

See [site/docs/contributing/quarantine.md](../../site/docs/contributing/quarantine.md) for the full contributor guide.

## Plugin Tests (`internal/plugin`)

Lua plugins use gopher-lua which creates fresh VM state per event delivery. Binary plugins use hashicorp/go-plugin and communicate via gRPC.

| Principle | Description |
| --------- | ----------- |
| State isolation (Lua) | Each `DeliverEvent` creates a new Lua state |
| No shared state between tests | No need for special test helpers or shared fixtures |
| Fast startup (Lua) | ~50μs per Lua state |
| Process isolation (binary) | Binary plugins run as separate processes via go-plugin |

## Integration Tests (Ginkgo/Gomega BDD)

| Requirement | Description |
| ----------- | ----------- |
| **MUST** use Ginkgo/Gomega | All integration tests use BDD-style specs |
| **MUST** write feature specs | User stories become `Describe`/`It` blocks |
| **MUST** use `//go:build integration` | Tag all integration test files |
| **SHOULD** use testcontainers | For database integration tests |

**Structure:** Feature specs live in `test/integration/<domain>/`:

```go
//go:build integration

var _ = Describe("Feature Name", func() {
    Describe("User story or capability", func() {
        It("expected behavior in plain English", func() {
            // Given/When/Then pattern
            Expect(result).To(Equal(expected))
        })
    })
})
```

**Async operations:**

```go
Eventually(func() int {
    return len(results)
}).Should(Equal(expected))
```

**Run integration tests:**

```bash
task test:int
# or, equivalent:
go test -race -v -tags=integration ./test/integration/...
```
