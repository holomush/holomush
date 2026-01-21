# Testing Libraries Adoption Design

This document defines the adoption of testify and ginkgo/gomega for HoloMUSH testing.

**Issue:** holomush-1hq.24
**Status:** Design complete
**Author:** Claude (with Sean)
**Date:** 2026-01-20

## Summary

Adopt testify for unit tests and ginkgo/gomega for integration tests. Migrate all
existing tests (41 files, ~22k lines) to the new patterns.

## Goals

- Reduce assertion verbosity (`if got != expected` → `assert.Equal`)
- Improve test structure for integration tests (BDD-style `Describe/Context/It`)
- Simplify mocking with testify/mock instead of hand-rolled function-field mocks

## Library Selection

| Test Type       | Library                 | Rationale                                            |
| --------------- | ----------------------- | ---------------------------------------------------- |
| Unit tests      | testify (assert + mock) | Fluent assertions, mock generation, minimal ceremony |
| Integration/E2E | ginkgo + gomega         | BDD structure, lifecycle hooks, async matchers       |

**Unchanged:**

- Table-driven test pattern (just with better assertions)
- testcontainers for database/container setup
- Build tags (`//go:build integration`) to separate test types

## Unit Tests with Testify

### Assertions

```go
// BEFORE
if got := tt.input.String(); got != tt.expected {
    t.Errorf("got %q, want %q", got, tt.expected)
}

// AFTER
assert.Equal(t, tt.expected, tt.input.String())
```

**Package choice:** Use `assert` (continues on failure) except for fatal setup failures
where `require` (stops immediately) is appropriate.

### Mocking

```go
// BEFORE: hand-rolled mock
type mockEventStore struct {
    appendFunc func(ctx context.Context, event core.Event) error
}
func (m *mockEventStore) Append(ctx context.Context, e core.Event) error {
    return m.appendFunc(ctx, e)
}

// AFTER: testify mock
type MockEventStore struct { mock.Mock }
func (m *MockEventStore) Append(ctx context.Context, e core.Event) error {
    args := m.Called(ctx, e)
    return args.Error(0)
}

// Usage
store := new(MockEventStore)
store.On("Append", mock.Anything, expectedEvent).Return(nil)
// ... test ...
store.AssertExpectations(t)
```

## Integration Tests with Ginkgo

### Structure

```go
// BEFORE
func TestPhase1_5Integration(t *testing.T) {
    env := setupTestEnv(t)
    defer env.teardown()

    t.Run("publishes event via gRPC", func(t *testing.T) { ... })
}

// AFTER
var _ = Describe("Phase 1.5 Integration", func() {
    var env *testEnv

    BeforeEach(func() {
        env = setupTestEnv()
    })

    AfterEach(func() {
        env.teardown()
    })

    Context("when publishing events via gRPC", func() {
        It("persists to PostgreSQL", func() {
            Expect(env.store.Append(ctx, event)).To(Succeed())
        })

        It("notifies subscribers", func() {
            Eventually(subscriber.Events).Should(Receive(Equal(event)))
        })
    })
})
```

### File Naming

- `*_suite_test.go` - ginkgo bootstrap file (one per package)
- `*_test.go` - spec files

### Benefits

- `Eventually`/`Consistently` for async assertions (no manual polling)
- Nested `Context` blocks for logical grouping
- Parallel specs with `--procs=N`
- Better failure output with diffs

## Migration Strategy

### Phases

| Phase                    | Scope                                                   | Effort |
| ------------------------ | ------------------------------------------------------- | ------ |
| 1. Setup                 | Add dependencies, configure ginkgo CLI, update Taskfile | Small  |
| 2. Unit test migration   | Convert 37 unit test files to testify assertions        | Medium |
| 3. Mock migration        | Replace hand-rolled mocks with testify/mock             | Medium |
| 4. Integration migration | Convert 4 integration test files to ginkgo suites       | Medium |
| 5. Cleanup               | Remove old mock structs, update documentation           | Small  |

### Migration Order

1. Leaf packages first (no internal dependencies)
2. Move inward to core packages
3. Integration tests last (biggest structural change)

### Validation

Each phase MUST:

- Pass `task test` after each file migration
- Maintain >80% coverage
- Have green CI before merging

## Dependencies

```go
// go.mod additions
github.com/stretchr/testify v1.9.0
github.com/onsi/ginkgo/v2 v2.22.0
github.com/onsi/gomega v1.36.0
```

## Taskfile Updates

Add `task test:ginkgo` for parallel ginkgo specs. Keep `task test` as unified entry
point that runs both standard and ginkgo tests.

## Documentation Updates

| Document                          | Update                                            |
| --------------------------------- | ------------------------------------------------- |
| `CLAUDE.md`                       | New testing patterns with testify/ginkgo examples |
| `docs/reference/testing-guide.md` | New doc: when to use each library                 |
| Suite files                       | Comments explaining ginkgo structure              |

## Risks

| Risk                          | Mitigation                                        |
| ----------------------------- | ------------------------------------------------- |
| Learning curve for ginkgo     | Start with simple specs, add complexity gradually |
| CI performance                | Ginkgo's `--procs` parallelism offsets overhead   |
| Mixed styles during migration | Complete one package fully before moving to next  |
