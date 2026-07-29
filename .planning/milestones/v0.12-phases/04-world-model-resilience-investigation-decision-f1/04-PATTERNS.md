# Phase 4: World-Model Resilience Investigation & Decision (F1) - Pattern Map

**Mapped:** 2026-07-11
**Files analyzed:** 8 (new/modified)
**Analogs found:** 7 / 8

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/testsupport/integrationtest/options.go` (modify: `WithExternalNATS`, `WithSharedDatabase`) | test-infra option | config | `internal/testsupport/integrationtest/options.go:14-16` + `harness.go:198-278` | exact |
| `internal/testsupport/integrationtest/harness.go` (modify: startConfig fields, bus/DB seams) | test-infra harness | request-response | itself — `harness.go:291-394` (Start body) | exact |
| `test/integration/resilience/resilience_suite_test.go` (new: entry + gate) | test suite entry | event-driven | `test/integration/eventbus_external/external_boot_test.go:38-44` + `quarantinetest.go` | exact |
| `test/integration/resilience/m12_lastwritewins_test.go` (new) | integration test | CRUD (race) | `test/integration/crypto/cache_invalidation_test.go` (multi-replica Ordered suite) | role+flow match |
| `test/integration/resilience/m2_dualwrite_test.go` (new) | integration test | event-driven | `external_boot_test.go` (broker/stream assertions) + RESEARCH Pattern 2 (docker pause) | role match |
| `test/integration/resilience/chaos_helpers_test.go` (new) | test helper | control | `external_boot_test.go:55-102` (helpers: `startExternalNATS`, `newExternalSubsystem`, `boolPtr`, `expectOopsCode`) | exact |
| `docs/adr/holomush-i4784-<slug>.md` (new) | doc (ADR) | — | `docs/adr/holomush-zcyvb-merge-local-test-lint-build-offload-agents-into-one-local-ch.md` | exact (template) |
| Evidence/verdict doc (`docs/reviews/arch-review/2026-07-11/verification/...` or ADR §Evidence) | doc | — | `docs/reviews/arch-review/2026-07-11/verification/f1-eventsourcing-why.md` (sibling) | role match |

## Pattern Assignments

### `internal/testsupport/integrationtest/options.go` — new StartOptions

**Analog:** the file itself + `harness.go:198-225`

**StartOption pattern** (`options.go:1-16`, verbatim shape to copy):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

// WithExtraPluginDir stages an additional plugin directory (...) into the plugin
// load path so the real plugin subsystem loads it alongside the in-tree plugins.
func WithExtraPluginDir(dir string) StartOption {
	return func(c *startConfig) { c.extraPluginDirs = append(c.extraPluginDirs, dir) }
}
```

**startConfig field pattern** (`harness.go:200-216`):

```go
type StartOption func(*startConfig)

type startConfig struct {
	accessEngine              types.AccessPolicyEngine
	withPlugins               bool
	withRealABAC              bool
	...
	extraPluginDirs []string
}
```

New options follow this exactly: add fields (e.g. `externalNATSURL string`, `sharedConnStr string`) to `startConfig`, one exported `With*` func per seam, doc comment explaining the wiring it changes and what it REQUIRES/composes with (see `WithFocusDelivery` at `harness.go:239-254` for the "REQUIRES X, gated like Y — zero blast radius" doc style). Compose-guard panics live in `Start` after option resolution (`harness.go:377-385`):

```go
if cfg.withPluginCrypto && !cfg.withPlugins {
	panic("integrationtest: WithPluginCrypto() requires WithInTreePlugins()")
}
```

### `internal/testsupport/integrationtest/harness.go` — bus + DB seams

**Analog:** itself.

**Seam 1 — DB** (`harness.go:321-323`): today hard-coded fresh DB per call; the shared-DB option branches here:

```go
// Postgres: shared container, fresh per-test database.
shared := testutil.SharedPostgres(t)
connStr := testutil.FreshDatabase(t, shared)
```

**Seam 2 — bus** (`harness.go:367-368`): today hard-coded embedded bus; the external-NATS option replaces this with an external-mode `eventbus.NewSubsystem` (see chaos_helpers pattern below for the Config shape):

```go
// Embedded NATS bus (in-memory, cleaned up via t.Cleanup).
bus := eventbustest.New(t)
```

**Seam 3 — M2 emitter wiring precedent** (`harness.go:607-610`): the harness already wraps the bus publisher for the CoreServer; wire the same shape (or `world.NewEventStoreAdapter`) into `world.Service` for M2 observability:

```go
holoGRPC.WithEventStore(&busEventAppenderAdapter{
	publisher: eventbus.NewRenderingPublisher(bus.Bus.Publisher(), verbRegistry),
	gameID:    bus.Bus.GameID,
}),
```

**Note:** guest-location seeding + KEK `t.Setenv` run per `Start` call (`harness.go:296-354`) — the shared-DB path must make replica-2 boot idempotent (RESEARCH Pitfall 3).

### `test/integration/resilience/resilience_suite_test.go` — suite entry + D-05 gate

**Analog:** `test/integration/eventbus_external/external_boot_test.go:1-44`

**File header + package doc + entry pattern** (lines 1-44):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package resilience_test ... (doc comment enumerating what the suite proves)
package resilience_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
)

// TestWorldModelResilience is the Ginkgo entry point. The name is stable so
// `task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` selects it.
func TestWorldModelResilience(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "World-Model Resilience Suite")
}
```

**D-05 gate** (`quarantinetest.go:20-23` — use `Enabled()`, NOT `Skip()`; message must NOT contain `quarantined:` per the bijection regex at `test/meta/quarantine_registry_test.go:31`):

```go
if !quarantinetest.Enabled() {
	t.Skipf("resilience harness is nightly/opt-in: set %s=1 to run (#4791)", quarantinetest.EnvVar)
}
```

### `test/integration/resilience/m12_lastwritewins_test.go` — multi-replica race spec

**Analog:** `test/integration/crypto/cache_invalidation_test.go`

**Suite topology — one container per suite, `Ordered` + `BeforeAll`/`AfterAll`** (lines 69-82):

```go
var _ = Describe("Invalidation Coordinator (multi-node external NATS)", Ordered, func() {
	var env *natstest.NATSEnv

	BeforeAll(func() {
		var err error
		env, err = natstest.StartNATS(context.Background())
		Expect(err).NotTo(HaveOccurred(), "natstest.StartNATS")
	})

	AfterAll(func() {
		if env != nil {
			Expect(env.Terminate(context.Background())).To(Succeed(), "natstest env.Terminate")
		}
	})
```

**Per-replica independent conn + DeferCleanup lifecycle** (lines 28-67): each replica dials its OWN conn (`h.Members[i].Conn` — "never a shared connection"); Start/Stop bounded with context timeouts; `DeferCleanup` for teardown; `// Verifies: INV-N` annotation style if any invariant is bound (Phase 4 likely binds none — invariants belong to Phase 5).

**Concurrency shape:** RESEARCH Pattern 3 (start-gun channel + `sync.WaitGroup`, N bounded rounds, verdict = "lost in k/N"); read-back via the shared pool using the harness escape-hatch idiom (`harness.go:901-908`):

```go
tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
require.NoError(s.t, err, "integrationtest.Server.DeleteSession")
```

(i.e. direct SQL against `s.pool` with `require` + descriptive message — for M12 use `SELECT name, description FROM locations WHERE id = $1` style read-backs, never session/subscriber frames.)

### `test/integration/resilience/m2_dualwrite_test.go` — broker flap

**Analog:** `external_boot_test.go` for stream/broker assertions; RESEARCH Pattern 2 for pause.

**Independent-conn stream inspection** (`external_boot_test.go:85-93`) — observe broker state, not a subsystem's cached view:

```go
conn, err := nats.Connect(url)
Expect(err).NotTo(HaveOccurred())
defer conn.Close()
js, err := jetstream.New(conn)
Expect(err).NotTo(HaveOccurred())
_, err = js.Stream(ctx, eventbus.StreamName)
```

**Flap** (RESEARCH Pattern 2, verified against pinned testcontainers-go v0.43.0):

```go
cli, err := testcontainers.NewDockerClientWithOpts(ctx)
require.NoError(t, err)
require.NoError(t, cli.ContainerPause(ctx, env.Container.GetContainerID()))
// … MoveCharacter whose notification should be lost …
require.NoError(t, cli.ContainerUnpause(ctx, env.Container.GetContainerID()))
```

**Error-code assertion pattern** (`external_boot_test.go:96-102`):

```go
oopsErr, ok := oops.AsOops(err)
Expect(ok).To(BeTrue(), "expected an oops error, got %T", err)
Expect(oopsErr.Code()).To(Equal(code))
```

### `test/integration/resilience/chaos_helpers_test.go`

**Analog:** `external_boot_test.go:53-80` helper style — small package-private funcs with `GinkgoHelper()`/`DeferCleanup`:

```go
func startExternalNATS(ctx context.Context) *natstest.NATSEnv {
	env, err := natstest.StartNATS(ctx)
	Expect(err).NotTo(HaveOccurred(), "StartNATS should return a running container")
	DeferCleanup(func() { _ = env.Terminate(context.Background()) })
	return env
}

func newExternalSubsystem(url string, provision *bool, maxAge time.Duration) *eventbus.Subsystem {
	cfg := eventbus.Config{
		Mode:         eventbus.ModeExternal,
		URL:          url,
		StreamMaxAge: maxAge,
		DupeWindow:   dupeWindow,
		Provision:    provision,
	}.Defaults()
	return eventbus.NewSubsystem(cfg)
}

func boolPtr(b bool) *bool { return &b } // replica-2 boots Provision:false (verify-only)
```

### `docs/adr/holomush-i4784-<slug>.md`

**Analog:** `docs/adr/holomush-zcyvb-merge-local-test-lint-build-offload-agents-into-one-local-ch.md` (2026-07-06, most recent template)

**Header + structure to copy** (omit the `<!-- adr-render: source=bd:... -->` line — bd is retired, hand-authored is now correct per RESEARCH):

```markdown
<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->

# <Title>

**Date:** 2026-07-XX
**Status:** Accepted
**Decision:** holomush-i4784
**Deciders:** Sean Brandt

## Context
## Decision
## Rationale
## Alternatives Considered
## Consequences
```

Plus a README.md index-table row (Title | Date | Status | decision token). Run `task lint` (ADR-shape lint exists) before finalizing.

## Shared Patterns

### SPDX header + build tag
**Source:** every file above. All new `.go` files: `// SPDX-License-Identifier: Apache-2.0` + `// Copyright 2026 HoloMUSH Contributors`, then `//go:build integration` (all harness/suite code — keeps depguard-protected testsupport imports out of the unit tier).

### Ginkgo dot-import nolint
**Source:** `external_boot_test.go:30-31`, `cache_invalidation_test.go:18-19` — always:

```go
. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
. "github.com/onsi/gomega"    //nolint:revive // gomega convention
```

### Bounded lifecycle + DeferCleanup
**Source:** `cache_invalidation_test.go:53-65`. Every Start/Stop of a subsystem or coordinator gets a `context.WithTimeout`; teardown via `DeferCleanup`; best-effort `_ = x.Stop(ctx)` where deliberate chaos may have wedged it.

### Assert on outcomes, not connection state
**Source:** RESEARCH Pitfall 5 + `.claude/rules/search-tools.md`. Judge by returned errors, DB rows read back via the shared pool, and stream contents over an independent conn — never grep output or key on nats connection-state callbacks.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| Docker pause/unpause chaos helper | test helper | control | No in-tree caller of `ContainerPause/Unpause`; use RESEARCH Pattern 2 (verified via `go doc` against pinned v0.43.0) |

## Metadata

**Analog search scope:** `internal/testsupport/{integrationtest,natstest,quarantinetest}`, `test/integration/{eventbus_external,crypto}`, `docs/adr/`
**Files scanned:** 7 read directly (RESEARCH.md pre-verified ~30 more)
**Pattern extraction date:** 2026-07-11
