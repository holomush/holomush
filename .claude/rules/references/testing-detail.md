<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Testing — deep reference

## Test Naming (ACE)

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

**Whole-system plugin tier** (top Go-fidelity tier within full-stack integration):
`integrationtest.Start(t, integrationtest.WithInTreePlugins())` loads ALL in-tree
plugins via `setup.PluginSubsystem` → `Manager.LoadAll` (INV-5, INV-WS-1). The
`test/integration/wholesystem/` census suite (`holomush-0f0f4`) asserts the full
plugin set loads and the `help` command is registered. Requires binary artifacts
(`task plugin:build-all`; automatic in `task test:int`). See
`site/src/content/docs/contributing/how-to/integration-tests.md#whole-system-plugin-tier-withintreeplugins`
for the full capability doc.

`eventbustest` provides the in-process embedded NATS server (`MemoryStorage`)
used at every non-unit tier. This matches production, which also runs embedded
NATS (`internal/eventbus/subsystem.go`); external/clustered NATS is unimplemented
(tracked in holomush-s5ts).

### Quarantine

Quarantine is for **genuinely intermittent failures without a reproducible cause**. If the root cause is *known* — resource contention, a race, undersized CI infra — **fix it; do not quarantine it.** Quarantining a known-cause failure hides a real infrastructure problem instead of resolving it.

### Diagnosing CI-only integration failures

`Integration Test` failures showing `connection refused` / `unexpected EOF` / testcontainer-start errors are usually a **transient Postgres testcontainer drop under CI load**, not a code defect. Before filing a bug or quarantining: check whether the **parent commit's** CI also failed the same way, then re-run the job. Treat it as a real failure only if it reproduces on a clean parent.

To un-quarantine: fix the flake, remove the marker and the `test/quarantine.yaml` row, then verify `task quarantine:audit` is clean.

## Plugin Tests (`internal/plugin`)

Lua plugins use gopher-lua which creates fresh VM state per event delivery. Binary plugins use hashicorp/go-plugin and communicate via gRPC.

| Principle | Description |
| --------- | ----------- |
| State isolation (Lua) | Each `DeliverEvent` creates a new Lua state |
| No shared state between tests | No need for special test helpers or shared fixtures |
| Fast startup (Lua) | ~50μs per Lua state |
| Process isolation (binary) | Binary plugins run as separate processes via go-plugin |

## Integration Tests (Ginkgo/Gomega BDD)

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
