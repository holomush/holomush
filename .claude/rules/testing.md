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

## EventBus Test Harness

`internal/eventbus/eventbustest.New(t)` provides an in-process embedded NATS server with `MemoryStorage` for unit and bus-integration tests.

| Requirement | Description |
| ----------- | ----------- |
| **MUST NOT** use in E2E tests | E2E tests use the full server stack with a real PG testcontainer |
| **Compile-time enforcement** | The `//go:build !integration` tag on the harness file enforces this |

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
