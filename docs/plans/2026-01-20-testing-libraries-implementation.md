# Testing Libraries Adoption Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate 41 test files (~1296 assertion sites, 24 mocks) to testify and ginkgo.

**Architecture:** Use testify assert/require for unit test assertions, mockery for mock generation, and ginkgo/gomega for integration tests with BDD structure.

**Tech Stack:** github.com/stretchr/testify, github.com/onsi/ginkgo/v2, github.com/onsi/gomega, github.com/vektra/mockery/v2

**Scope:**

- 41 test files with ~1296 assertion sites
- 24 hand-rolled mock structs to replace
- 3 integration test files to convert to ginkgo

---

## Phase 1: Setup

### Task 1: Add testing library dependencies

**Files:**

- Modify: `go.mod`
- Modify: `Taskfile.yaml`

**Step 1: Add dependencies**

```bash
go get github.com/stretchr/testify@latest
go get github.com/onsi/ginkgo/v2@latest
go get github.com/onsi/gomega@latest
go install github.com/vektra/mockery/v2@latest
go install github.com/onsi/ginkgo/v2/ginkgo@latest
```

**Step 2: Verify installation**

```bash
go mod tidy
ginkgo version
mockery --version
```

**Step 3: Add Taskfile targets**

Add to `Taskfile.yaml`:

```yaml
test:ginkgo:
  desc: Run ginkgo integration tests with parallelism
  cmds:
    - ginkgo -r -p --randomize-all ./test/integration/...

mocks:generate:
  desc: Generate mocks with mockery
  cmds:
    - mockery
```

**Step 4: Commit**

```bash
git add go.mod go.sum Taskfile.yaml
git commit -m "deps: add testify, ginkgo, gomega, and mockery"
```

---

### Task 2: Configure mockery

**Files:**

- Create: `.mockery.yaml`

**Step 1: Create mockery config**

Create `.mockery.yaml`:

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

with-expecter: true
packages:
  github.com/holomush/holomush/internal/core:
    interfaces:
      EventStore:
      SessionStore:
      Broadcaster:
  github.com/holomush/holomush/internal/grpc:
    interfaces:
      Authenticator:
```

**Step 2: Generate initial mocks**

```bash
mockery
```

**Step 3: Verify generated files**

```bash
ls internal/core/mocks/
ls internal/grpc/mocks/
```

**Step 4: Commit**

```bash
git add .mockery.yaml internal/*/mocks/
git commit -m "build: configure mockery for mock generation"
```

---

## Phase 2: Unit Test Migration (by package)

### Task 3: Migrate internal/core unit tests

**Files:**

- Modify: `internal/core/event_test.go` (2 sites)
- Modify: `internal/core/session_test.go` (14 sites)
- Modify: `internal/core/engine_test.go` (25 sites)
- Modify: `internal/core/broadcaster_test.go` (3 sites)
- Modify: `internal/core/command_test.go` (2 sites)
- Modify: `internal/core/ulid_test.go` (2 sites)
- Modify: `internal/core/store_test.go` (17 sites)

**Step 1: Add testify import to event_test.go**

```go
import (
    "testing"

    "github.com/stretchr/testify/assert"
)
```

**Step 2: Replace assertions**

Pattern:

```go
// BEFORE
if got := tt.input.String(); got != tt.expected {
    t.Errorf("got %q, want %q", got, tt.expected)
}

// AFTER
assert.Equal(t, tt.expected, tt.input.String())
```

**Step 3: Run tests**

```bash
go test ./internal/core/... -v
```

**Step 4: Commit**

```bash
git add internal/core/*_test.go
git commit -m "test(core): migrate to testify assertions"
```

---

### Task 4: Migrate internal/store unit tests

**Files:**

- Modify: `internal/store/postgres_test.go` (37 sites)

**Step 1: Add testify import**

**Step 2: Replace assertions using pattern from Task 3**

**Step 3: Run tests**

```bash
go test ./internal/store/... -v -short
```

**Step 4: Commit**

```bash
git add internal/store/postgres_test.go
git commit -m "test(store): migrate to testify assertions"
```

---

### Task 5: Migrate internal/grpc unit tests

**Files:**

- Modify: `internal/grpc/server_test.go` (64 sites)
- Modify: `internal/grpc/client_test.go` (48 sites)

**Step 1: Add testify import to both files**

**Step 2: Replace assertions**

**Step 3: Run tests**

```bash
go test ./internal/grpc/... -v
```

**Step 4: Commit**

```bash
git add internal/grpc/*_test.go
git commit -m "test(grpc): migrate to testify assertions"
```

---

### Task 6: Migrate internal/plugin unit tests

**Files:**

- Modify: `internal/plugin/manifest_test.go` (21 sites)
- Modify: `internal/plugin/schema_test.go` (10 sites)
- Modify: `internal/plugin/manager_test.go` (43 sites)
- Modify: `internal/plugin/subscriber_test.go` (12 sites)
- Modify: `internal/plugin/capability/enforcer_test.go` (47 sites)
- Modify: `internal/plugin/lua/host_test.go` (41 sites)
- Modify: `internal/plugin/lua/state_test.go` (22 sites)
- Modify: `internal/plugin/lua/state_internal_test.go` (1 site)
- Modify: `internal/plugin/hostfunc/functions_test.go` (75 sites)
- Modify: `internal/plugin/goplugin/host_test.go` (56 sites)

**Step 1: Add testify imports**

**Step 2: Replace assertions in each file**

**Step 3: Run tests**

```bash
go test ./internal/plugin/... -v
```

**Step 4: Commit**

```bash
git add internal/plugin/
git commit -m "test(plugin): migrate to testify assertions"
```

---

### Task 7: Migrate internal/control unit tests

**Files:**

- Modify: `internal/control/grpc_server_test.go` (147 sites)

**Step 1: Add testify import**

**Step 2: Replace assertions**

**Step 3: Run tests**

```bash
go test ./internal/control/... -v
```

**Step 4: Commit**

```bash
git add internal/control/
git commit -m "test(control): migrate to testify assertions"
```

---

### Task 8: Migrate cmd unit tests

**Files:**

- Modify: `cmd/holomush/core_test.go` (50 sites)
- Modify: `cmd/holomush/status_test.go` (49 sites)
- Modify: `cmd/holomush/gateway_test.go` (41 sites)
- Modify: `cmd/holomush/deps_test.go` (15 sites)
- Modify: `cmd/holomush/main_test.go` (11 sites)

**Step 1: Add testify imports**

**Step 2: Replace assertions**

**Step 3: Run tests**

```bash
go test ./cmd/holomush/... -v
```

**Step 4: Commit**

```bash
git add cmd/holomush/*_test.go
git commit -m "test(cmd): migrate to testify assertions"
```

---

### Task 9: Migrate remaining unit tests

**Files:**

- Modify: `internal/tls/certs_test.go` (141 sites)
- Modify: `internal/telnet/server_test.go` (3 sites)
- Modify: `internal/telnet/handler_test.go` (13 sites)
- Modify: `internal/observability/server_test.go` (47 sites)
- Modify: `internal/xdg/xdg_test.go` (28 sites)
- Modify: `internal/logging/handler_test.go` (11 sites)
- Modify: `pkg/plugin/sdk_test.go` (3 sites)
- Modify: `pkg/plugin/event_test.go` (1 site)
- Modify: `pkg/plugin/adapter_test.go` (25 sites)
- Modify: `pkg/proto/holomush/plugin/v1/plugin_test.go` (1 site)
- Modify: `pkg/proto/holomush/plugin/v1/hostfunc_test.go` (4 sites)
- Modify: `pkg/proto/holomush/plugin/v1/grpc_test.go` (1 site)

**Step 1-4: Follow same pattern**

**Step 5: Commit**

```bash
git add internal/ pkg/
git commit -m "test: migrate remaining packages to testify"
```

---

## Phase 3: Mock Migration

### Task 10: Replace hand-rolled mocks with mockery-generated mocks

**Files to modify (remove old mocks, use generated):**

- `internal/grpc/server_test.go` - mockEventStore, mockAuthenticator, mockSubscribeStream, mockSessionStore
- `internal/grpc/client_test.go` - mockCoreServer, mockCoreServerWithSubscribe
- `cmd/holomush/deps_test.go` - mockEventStore, mockControlServer, mockObservabilityServer
- `cmd/holomush/gateway_test.go` - mockNetConn
- `internal/plugin/manager_test.go` - mockHost
- `internal/plugin/goplugin/host_test.go` - mockClientProtocol, mockPluginClient, etc.
- `internal/plugin/hostfunc/functions_test.go` - mockKVStore
- `internal/plugin/integration_test.go` - mockEmitter
- `pkg/proto/holomush/plugin/v1/grpc_test.go` - mockPluginServer, mockHostFunctionsServer

**Step 1: Update .mockery.yaml with all interfaces**

**Step 2: Generate mocks**

```bash
mockery
```

**Step 3: Update test files to use generated mocks**

Pattern:

```go
// BEFORE
store := &mockEventStore{
    appendFunc: func(ctx context.Context, e core.Event) error {
        return nil
    },
}

// AFTER
store := mocks.NewMockEventStore(t)
store.EXPECT().Append(mock.Anything, mock.Anything).Return(nil)
```

**Step 4: Remove old mock definitions**

**Step 5: Run all tests**

```bash
task test
```

**Step 6: Commit**

```bash
git add .
git commit -m "test: replace hand-rolled mocks with mockery-generated mocks"
```

---

## Phase 4: Integration Test Migration

### Task 11: Migrate test/integration to ginkgo

**Files:**

- Create: `test/integration/integration_suite_test.go`
- Modify: `test/integration/phase1_5_test.go` (92 sites)

**Step 1: Bootstrap ginkgo suite**

```bash
cd test/integration
ginkgo bootstrap
```

This creates `integration_suite_test.go`.

**Step 2: Convert test structure**

```go
// BEFORE
func TestPhase1_5Integration(t *testing.T) {
    env := setupTestEnv(t)
    defer env.teardown()

    t.Run("publishes event", func(t *testing.T) {
        // test code
    })
}

// AFTER
var _ = Describe("Phase 1.5 Integration", func() {
    var env *testEnv

    BeforeEach(func() {
        var err error
        env, err = setupTestEnv()
        Expect(err).NotTo(HaveOccurred())
    })

    AfterEach(func() {
        env.teardown()
    })

    Context("when publishing events", func() {
        It("persists to database", func() {
            // test with Expect()
        })
    })
})
```

**Step 3: Replace assertions with gomega**

```go
// BEFORE
if err != nil {
    t.Fatalf("failed: %v", err)
}

// AFTER
Expect(err).NotTo(HaveOccurred())

// BEFORE
if got != expected {
    t.Errorf("got %v, want %v", got, expected)
}

// AFTER
Expect(got).To(Equal(expected))
```

**Step 4: Use Eventually for async**

```go
// BEFORE
time.Sleep(100 * time.Millisecond)
if len(received) != 1 {
    t.Errorf("expected 1 event")
}

// AFTER
Eventually(func() int {
    return len(received)
}).Should(Equal(1))
```

**Step 5: Run ginkgo tests**

```bash
ginkgo -v ./test/integration/...
```

**Step 6: Commit**

```bash
git add test/integration/
git commit -m "test(integration): migrate to ginkgo/gomega"
```

---

### Task 12: Migrate internal/store/postgres_integration_test.go

**Files:**

- Create: `internal/store/store_suite_test.go`
- Modify: `internal/store/postgres_integration_test.go` (45 sites)

Follow same pattern as Task 11.

---

### Task 13: Migrate internal/plugin/integration_test.go

**Files:**

- Create: `internal/plugin/plugin_suite_test.go`
- Modify: `internal/plugin/integration_test.go` (26 sites)

Follow same pattern as Task 11.

---

## Phase 5: Documentation

### Task 14: Update CLAUDE.md testing patterns

**Files:**

- Modify: `CLAUDE.md`

Add to Testing section:

```markdown
### Assertions

Use testify for unit test assertions:

\`\`\`go
// Equality
assert.Equal(t, expected, got)

// Error checking
require.NoError(t, err)
assert.Error(t, err)

// Contains
assert.Contains(t, slice, element)
\`\`\`

### Mocking

Generate mocks with mockery:

\`\`\`bash
mockery # Uses .mockery.yaml config
\`\`\`

Use generated mocks:

\`\`\`go
store := mocks.NewMockEventStore(t)
store.EXPECT().Append(mock.Anything, mock.Anything).Return(nil)
\`\`\`

### Integration Tests

Use ginkgo/gomega for integration tests:

\`\`\`go
var _ = Describe("Feature", func() {
It("does something", func() {
Expect(result).To(Equal(expected))
})
})
\`\`\`

For async operations:

\`\`\`go
Eventually(func() int {
return len(results)
}).Should(Equal(expected))
\`\`\`
```

---

### Task 15: Create docs/reference/testing-guide.md

**Files:**

- Create: `docs/reference/testing-guide.md`

Create comprehensive testing guide covering:

- When to use testify vs ginkgo
- Assertion patterns
- Mock generation
- Running tests
- Coverage requirements

---

### Task 16: Final validation

**Step 1: Run all tests**

```bash
task test
task test:ginkgo
```

**Step 2: Check coverage**

```bash
task test:coverage
```

Expected: >80% coverage maintained.

**Step 3: Run linter**

```bash
task lint
```

**Step 4: Commit**

```bash
git add .
git commit -m "test: complete testing libraries migration"
```

---

## Summary

| Phase       | Tasks  | Files  | Assertion Sites |
| ----------- | ------ | ------ | --------------- |
| Setup       | 2      | 3      | -               |
| Unit Tests  | 7      | 38     | ~1200           |
| Mocks       | 1      | 9      | 24 mocks        |
| Integration | 3      | 3      | ~163            |
| Docs        | 3      | 2      | -               |
| **Total**   | **16** | **55** | **~1387**       |
