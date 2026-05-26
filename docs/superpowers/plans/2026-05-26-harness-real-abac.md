# WithRealABAC() Harness Capability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an opt-in `WithRealABAC()` StartOption to the integration harness that boots the real seeded ABAC engine via production's `abacsetup.NewABACSubsystem`, so full-stack integration tests can catch g776-class default-deny regressions.

**Architecture:** Mirror PR #4275's `WithInTreePlugins()` pattern. A new `startConfig.withRealABAC` flag drives `Start()` to seed the `seed:*` policy set (`policy.Bootstrap`), boot `abacsetup.NewABACSubsystem`, use its `Engine()` as the harness access engine, and thread its concrete `AttributeResolver()`/`PluginProvider()`/`AuditLogger()` into the plugin layer (fixing the standalone-resolver bug). Default stays allow-all; opt-in only.

**Tech Stack:** Go, `//go:build integration`, testify (`require`), Postgres testcontainer (`testutil.SharedPostgres`), embedded NATS (`eventbustest`).

**Spec:** `docs/superpowers/specs/2026-05-26-harness-real-abac-design.md`

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `internal/testsupport/integrationtest/real_abac.go` | `WithRealABAC` wiring helpers: `poolProvider` adapter, `startRealABAC` (seed + subsystem boot), `pluginAttrSources` (resolver/provider/auditor selection) | Create |
| `internal/testsupport/integrationtest/harness.go` | `startConfig.withRealABAC` field, `WithRealABAC()` option, `Start()` real-engine branch + plugin threading | Modify |
| `internal/testsupport/integrationtest/plugins.go` | `pluginDeps` gains `resolver`/`pluginProvider`/`auditor`; `startPlugins` uses them instead of standalone instances | Modify |
| `internal/testsupport/integrationtest/real_abac_test.go` | All INV-RA-* tests (in-package; white-box helper tests + RPC-driven behavioral tests) | Create |
| `site/docs/contributing/integration-tests.md` | Document `WithRealABAC` (composition, seed step, live role semantics) | Modify |

**Note on test placement:** Tests live in-package (`package integrationtest`) because INV-RA-4 exercises unexported `pluginAttrSources`/`startRealABAC`. The harness package is `//go:build integration` and is already run under `task test:int` (see existing `harness_smoke_test.go`).

---

### Task 1: `WithRealABAC()` option + real-engine boot

**Files:**

- Create: `internal/testsupport/integrationtest/real_abac.go`
- Modify: `internal/testsupport/integrationtest/harness.go` (`startConfig` at `:132`, add option near `WithPolicyEngine` at `:142`, `Start()` after `pe := cfg.accessEngine` at `:218`)
- Test: `internal/testsupport/integrationtest/real_abac_test.go`

**Grounding (verified against current main):**

- Seed policies are runtime-installed via `policy.Bootstrap(ctx, partitions, policyStore, compiler, logger, opts)` (`internal/access/policy/bootstrap.go:30`); `BootstrapOptions{SkipSeedMigrations bool}` (`bootstrap.go:23`).
- `abacsetup.NewABACSubsystem(ABACSubsystemConfig{DB PoolProvider, Registry *lifecycle.ReadinessRegistry, AuditMode, CryptoOperators})` (`internal/access/setup/subsystem.go:31,51`); `PoolProvider = { Pool() *pgxpool.Pool }` (`subsystem.go`). `Start()` builds all repos from the pool internally (`subsystem.go:79-90`) and starts a poller goroutine (`subsystem.go:105`). `Engine()` (`:124`).
- `audit.NewPostgresPartitionCreator(pool)` (`internal/audit/partition_creator.go:22`); `policystore.NewPostgresStore(pool)`; `policy.NewCompiler(types.NewAttributeSchema())` (`compiler.go:31`, `types/types.go:368`).
- Under real ABAC, a regular character reading a **different** location's stream is denied (`staffOverride` returns false — no `read_unrestricted_history`; `internal/grpc/scope_floor.go:88`). Under the allow-all default that same read **succeeds** (staff bypass). A **same-location** read succeeds under either engine (session/colocation floor, ABAC-independent).
- Helpers: `Server.ConnectAuthed(ctx, name) *Session` (`harness.go:510`), `Server.NewLocation(ctx) ulid.ULID` (`harness.go:345`), `Session.MoveTo(ctx, locID)` (`session.go:203`), `Session.QueryStreamHistory(ctx, stream) ([]*corev1.EventFrame, error)` (`session.go:504`).

- [ ] **Step 1: Write the failing tests**

Create `internal/testsupport/integrationtest/real_abac_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package integrationtest

import (
	"context"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"
)

// movedAwayPlayer connects charName, records its start location, then moves it
// to a fresh location. Querying the START location afterward is the ABAC-gated
// (different-location) path: permitted only for staff via read_unrestricted_history.
func movedAwayPlayer(t *testing.T, ctx context.Context, ts *Server, charName string, roles []string) (sess *Session, priorStream string) {
	t.Helper()
	if roles == nil {
		sess = ts.ConnectAuthed(ctx, charName)
	} else {
		sess = ts.ConnectAuthedWithRoles(ctx, charName, roles)
	}
	priorStream = "location:" + sess.LocationID.String()
	sess.MoveTo(ctx, ts.NewLocation(ctx))
	return sess, priorStream
}

// INV-RA-1: under WithRealABAC, a regular (non-staff) character is DENIED a
// read against a location it has left. Allow-all would permit this via the
// staffOverride bypass — so a passing assertion here proves the engine is the
// real seeded engine, not allowAllPolicyEngine.
func TestRealABAC_RegularPlayerDeniedNonColocatedRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ts := Start(t, WithRealABAC())
	mover, priorStream := movedAwayPlayer(t, ctx, ts, "Mover", nil)

	_, err := mover.QueryStreamHistory(ctx, priorStream)
	require.Error(t, err, "INV-RA-1: real engine MUST deny a regular player's non-colocated read")
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "denial must surface as an oops error")
	require.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code(),
		"INV-RA-1: denial MUST collapse to STREAM_ACCESS_DENIED")
}

// INV-RA-2: without WithRealABAC, the harness retains the allow-all default —
// the same non-colocated read SUCCEEDS (staff bypass). Guards against an
// accidental flip of the default.
func TestRealABAC_DefaultEngineStillAllowAll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ts := Start(t) // no WithRealABAC
	mover, priorStream := movedAwayPlayer(t, ctx, ts, "Mover", nil)

	_, err := mover.QueryStreamHistory(ctx, priorStream)
	require.NoError(t, err,
		"INV-RA-2: allow-all default MUST permit the non-colocated read via staff bypass")
}

// INV-RA-3 + INV-RA-5: under WithRealABAC, an admin-role character is PERMITTED
// the non-colocated read via seed:admin-full-access (read_unrestricted_history).
// This proves (a) the seed:* set is installed and a seeded permit works, and
// (b) the provider populating principal.character.roles is registered — an
// unregistered provider (the g776/xxel fingerprint) would silently default-deny
// and flip this to STREAM_ACCESS_DENIED, which allow-all would have masked.
func TestRealABAC_AdminPermittedNonColocatedRead_g776Sentinel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ts := Start(t, WithRealABAC())
	boss, priorStream := movedAwayPlayer(t, ctx, ts, "Boss", []string{"admin"})

	_, err := boss.QueryStreamHistory(ctx, priorStream)
	require.NoError(t, err,
		"INV-RA-3/RA-5: seed:admin-full-access MUST permit an admin's non-colocated read; "+
			"a failure here means seeds weren't installed or the roles provider is unregistered")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test:int -- -run TestRealABAC ./internal/testsupport/integrationtest/`
Expected: FAIL — `undefined: WithRealABAC`.

- [ ] **Step 3: Create the wiring helper**

Create `internal/testsupport/integrationtest/real_abac.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package integrationtest

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	policytypes "github.com/holomush/holomush/internal/access/policy/types"
	abacsetup "github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/lifecycle"
)

// poolProvider adapts a *pgxpool.Pool to abacsetup.PoolProvider so the harness
// can hand the test pool to the production ABAC subsystem.
type poolProvider struct{ pool *pgxpool.Pool }

func (p poolProvider) Pool() *pgxpool.Pool { return p.pool }

// startRealABAC seeds the production seed:* policy set and boots the real ABAC
// subsystem (production's abacsetup.NewABACSubsystem path, the same constructor
// cmd/holomush/core.go:380 uses). It returns the started subsystem; callers read
// Engine()/AttributeResolver()/PluginProvider()/AuditLogger() and the poller is
// stopped via t.Cleanup.
func startRealABAC(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *abacsetup.ABACSubsystem {
	t.Helper()

	// Seed first: the subsystem's Start → BuildABACStack → cache.Reload reads
	// the policy store at construction. An unseeded store has zero policies and
	// default-denies everything.
	require.NoError(t,
		policy.Bootstrap(
			ctx,
			audit.NewPostgresPartitionCreator(pool),
			policystore.NewPostgresStore(pool),
			policy.NewCompiler(policytypes.NewAttributeSchema()),
			slog.Default(),
			policy.BootstrapOptions{},
		),
		"startRealABAC: seed policies",
	)

	abacSub := abacsetup.NewABACSubsystem(abacsetup.ABACSubsystemConfig{
		DB:       poolProvider{pool: pool},
		Registry: lifecycle.NewReadinessRegistry(),
	})
	require.NoError(t, abacSub.Start(ctx), "startRealABAC: ABAC subsystem start")
	t.Cleanup(func() { _ = abacSub.Stop(context.Background()) })
	return abacSub
}
```

- [ ] **Step 4: Wire the option into harness.go**

In `internal/testsupport/integrationtest/harness.go`, add the field to `startConfig` (currently `:132`):

```go
type startConfig struct {
	accessEngine types.AccessPolicyEngine
	withPlugins  bool
	withRealABAC bool
}
```

Add the option next to `WithPolicyEngine` (`:142`):

```go
// WithRealABAC boots the real seeded ABAC engine inside the harness via
// production's abacsetup.NewABACSubsystem (which calls setup.BuildABACStack),
// seeding the seed:* policy set first. Opt-in; the default stays allow-all.
// Compose with WithInTreePlugins for cross-plugin ABAC coverage.
//
// Under WithRealABAC, character_roles become load-bearing: ConnectAuthedWithRoles
// grants role-based permits, while a roleless ConnectAuthed receives only what
// seed:* grants a roleless character.
func WithRealABAC() StartOption {
	return func(c *startConfig) { c.withRealABAC = true }
}
```

In `Start()`, immediately after `pe := cfg.accessEngine` (`:218`), add:

```go
	// Real seeded ABAC engine (opt-in). Overrides the allow-all default and is
	// retained for the plugin layer's resolver/pluginProvider threading below.
	var abacSub *abacsetup.ABACSubsystem
	if cfg.withRealABAC {
		abacSub = startRealABAC(t, ctx, pool)
		pe = abacSub.Engine()
	}
```

Add the import to harness.go's import block:

```go
	abacsetup "github.com/holomush/holomush/internal/access/setup"
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test:int -- -run TestRealABAC ./internal/testsupport/integrationtest/`
Expected: PASS (all three). Requires Docker (Postgres testcontainer).

- [ ] **Step 6: Lint**

Run: `task lint:go`
Expected: no findings. (Use line-scoped `//nolint:<rule> // reason` only if a specific finding is unavoidable; never widen `.golangci.yaml`.)

- [ ] **Step 7: Commit**

```text
jj commit -m "feat(integrationtest): WithRealABAC() boots real seeded ABAC engine (holomush-f5t07)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Thread real resolver/pluginProvider/auditor into the plugin layer

**Files:**

- Modify: `internal/testsupport/integrationtest/real_abac.go` (add `pluginAttrSources`)
- Modify: `internal/testsupport/integrationtest/plugins.go` (`pluginDeps` at `:167`, standalone-resolver block at `:235-236,:243`, `cfg.ABAC` at `:242`)
- Modify: `internal/testsupport/integrationtest/harness.go` (plugin call site at `:234`)
- Test: `internal/testsupport/integrationtest/real_abac_test.go`

**Grounding (verified):**

- `startPlugins` builds a standalone `attribute.NewResolver(attribute.NewSchemaRegistry())` (`plugins.go:235-236`) and `attribute.NewPluginProvider(nil)` (`plugins.go:243`), passing `engineProvider{eng: d.engine, resolver: resolver}` as `cfg.ABAC` (`plugins.go:242`). `engineProvider` has fields `eng`, `resolver`, `auditor` (`plugins.go`).
- `abacsetup.ABACSubsystem` exposes `AttributeResolver() *attribute.Resolver` (`subsystem.go:182`), `PluginProvider() *attribute.PluginProvider` (`:149`), `AuditLogger() pluginauthz.Auditor` (`:196`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/testsupport/integrationtest/real_abac_test.go`:

```go
// INV-RA-4: when a real ABAC subsystem is present, pluginAttrSources MUST route
// the plugin layer to the subsystem's OWN resolver and plugin provider (pointer
// identity) — not freshly-allocated standalone instances — so plugin-declared
// attribute providers register on the resolver the engine evaluates against.
func TestRealABAC_PluginAttrSourcesUsesEngineInstances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	evStore, err := store.NewPostgresEventStore(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(evStore.Close)

	abacSub := startRealABAC(t, ctx, evStore.Pool())
	res, pp, aud := pluginAttrSources(abacSub)

	require.Same(t, abacSub.AttributeResolver(), res,
		"INV-RA-4: plugin resolver MUST be the engine's resolver instance")
	require.Same(t, abacSub.PluginProvider(), pp,
		"INV-RA-4: plugin provider MUST be the engine's plugin-provider instance")
	require.Equal(t, abacSub.AuditLogger(), aud,
		"INV-RA-4: plugin auditor MUST be the engine's auditor")

	// nil subsystem → fresh standalone instances (the allow-all default path).
	resStd, ppStd, audStd := pluginAttrSources(nil)
	require.NotNil(t, resStd, "standalone resolver must be non-nil")
	require.NotNil(t, ppStd, "standalone plugin provider must be non-nil")
	require.Nil(t, audStd, "standalone auditor is nil")
	require.NotSame(t, abacSub.AttributeResolver(), resStd,
		"standalone resolver must differ from the engine's")
}
```

Add the imports used above to the test file's import block:

```go
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test:int -- -run TestRealABAC_PluginAttrSourcesUsesEngineInstances ./internal/testsupport/integrationtest/`
Expected: FAIL — `undefined: pluginAttrSources`.

- [ ] **Step 3: Add `pluginAttrSources`**

Append to `internal/testsupport/integrationtest/real_abac.go` (add `attribute`, `pluginauthz` imports):

```go
// pluginAttrSources returns the attribute resolver, plugin provider, and auditor
// the plugin subsystem should register against. With a real ABAC subsystem, these
// are the subsystem's OWN instances so plugin-declared providers (e.g. core-scenes'
// "scene" namespace) register on the resolver the engine evaluates against
// (INV-RA-4). With no real engine (allow-all default), fresh standalone instances
// are correct — allow-all ignores attributes, so the #4275 behavior is preserved.
func pluginAttrSources(abacSub *abacsetup.ABACSubsystem) (*attribute.Resolver, *attribute.PluginProvider, pluginauthz.Auditor) {
	if abacSub != nil {
		return abacSub.AttributeResolver(), abacSub.PluginProvider(), abacSub.AuditLogger()
	}
	return attribute.NewResolver(attribute.NewSchemaRegistry()), attribute.NewPluginProvider(nil), nil
}
```

Imports to add to `real_abac.go`:

```go
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
```

- [ ] **Step 4: Extend `pluginDeps` and use the threaded instances in `startPlugins`**

In `internal/testsupport/integrationtest/plugins.go`, extend `pluginDeps` (`:167`):

```go
type pluginDeps struct {
	pool           *pgxpool.Pool
	connStr        string
	engine         policytypes.AccessPolicyEngine
	sessionStore   session.Access
	verbReg        *core.VerbRegistry
	playerRepo     auth.PlayerRepository
	hasher         auth.PasswordHasher
	playerSess     auth.PlayerSessionRepository
	resolver       *attribute.Resolver
	pluginProvider *attribute.PluginProvider
	auditor        pluginauthz.Auditor
}
```

Replace the standalone-resolver construction (`plugins.go:235-236`) and the `cfg.ABAC`/`PluginProv` fields (`:242-243`). Delete:

```go
	schemaReg := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(schemaReg)
```

and change the `PluginSubsystemConfig` to use the caller-supplied instances:

```go
		ABAC:               engineProvider{eng: d.engine, resolver: d.resolver, auditor: d.auditor},
		PolicyInst:         policyInstallerProvider{inst: policyInst},
		PluginProv:         pluginProviderSetter{pp: d.pluginProvider},
```

(The `pluginauthz` import already exists in `plugins.go`.)

- [ ] **Step 5: Pass the threaded instances from `Start()`**

In `internal/testsupport/integrationtest/harness.go`, replace the `if cfg.withPlugins { ... }` block (`:234`):

```go
	if cfg.withPlugins {
		res, pp, aud := pluginAttrSources(abacSub)
		pluginSub = startPlugins(t, ctx, pluginDeps{
			pool:           pool,
			connStr:        connStr,
			engine:         pe,
			sessionStore:   sessionStoreInst,
			verbReg:        verbRegistry,
			playerRepo:     playerRepo,
			hasher:         hasher,
			playerSess:     playerSessionStore,
			resolver:       res,
			pluginProvider: pp,
			auditor:        aud,
		})
		cmdRegistry = pluginSub.CommandRegistry()
	}
```

- [ ] **Step 6: Write the composition + option-order tests (INV-RA-6)**

Append to `internal/testsupport/integrationtest/real_abac_test.go`:

```go
// INV-RA-6: option order MUST NOT affect the resulting stack. Both orderings
// must produce the same real-ABAC deny for a regular non-colocated read.
// Plugin-gated: skipped when binary plugins are unbuilt (HOLOMUSH_REQUIRE_PLUGINS
// forces failure instead — see plugins.go).
func TestRealABAC_OptionOrderIndependent(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []StartOption
	}{
		{"plugins-then-abac", []StartOption{WithInTreePlugins(), WithRealABAC()}},
		{"abac-then-plugins", []StartOption{WithRealABAC(), WithInTreePlugins()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			ts := Start(t, tc.opts...) // skips here if binary plugins unbuilt
			mover, priorStream := movedAwayPlayer(t, ctx, ts, "Mover", nil)

			_, err := mover.QueryStreamHistory(ctx, priorStream)
			require.Error(t, err, "INV-RA-6: composed real engine MUST deny regardless of option order")
			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok)
			require.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code())
		})
	}
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `task plugin:build-all` (so the composition test runs instead of skipping), then
`task test:int -- -run TestRealABAC ./internal/testsupport/integrationtest/`
Expected: PASS. `TestRealABAC_OptionOrderIndependent` runs (not skipped) once binaries are built.

- [ ] **Step 8: Lint + commit**

Run: `task lint:go` (expect clean), then:

```text
jj commit -m "feat(integrationtest): thread real resolver/provider/auditor into plugin layer (holomush-f5t07)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Documentation (PR-blocking)

**Files:**

- Modify: `site/docs/contributing/integration-tests.md`
- Modify: `internal/testsupport/integrationtest/harness.go` (package doc-comment)

- [ ] **Step 1: Add a `WithRealABAC` section to the contributor guide**

In `site/docs/contributing/integration-tests.md`, add this section after the `WithInTreePlugins` documentation:

````markdown
## Real ABAC (`WithRealABAC`)

By default the harness wires an allow-all ABAC engine — most integration tests
assert session/history floors, which are ABAC-independent. To exercise the
**real seeded ABAC engine** (production's `abacsetup.NewABACSubsystem` path),
opt in:

```go
ts := integrationtest.Start(t, integrationtest.WithRealABAC())
```

This seeds the production `seed:*` policy set (`policy.Bootstrap`) and boots the
real engine. Compose with `WithInTreePlugins()` for cross-plugin ABAC coverage —
the plugin subsystem registers its attribute providers on the engine's own
resolver:

```go
ts := integrationtest.Start(t, integrationtest.WithInTreePlugins(), integrationtest.WithRealABAC())
```

**Live role semantics.** Under `WithRealABAC`, `character_roles` are evaluated by
the engine. `ConnectAuthedWithRoles(ctx, name, []string{"admin"})` grants
role-based permits (e.g. `seed:admin-full-access`); a roleless `ConnectAuthed`
receives only what `seed:*` grants a roleless character. Tests that pass under
allow-all may see denials under `WithRealABAC` until they seed the roles their
actions require.
````

- [ ] **Step 2: Update the harness package doc-comment**

In `internal/testsupport/integrationtest/harness.go`, add a sentence to the package doc-comment noting the `WithRealABAC` opt-in and the live role semantics (mirroring the `WithInTreePlugins` note already present).

- [ ] **Step 3: Verify docs build / lint**

Run: `task fmt:check`
Expected: PASS (markdown formatting clean).

- [ ] **Step 4: Commit**

```text
jj commit -m "docs(integrationtest): document WithRealABAC opt-in and role semantics (holomush-f5t07)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Acceptance verification (after all tasks)

- [ ] `task test:int -- -run TestRealABAC ./internal/testsupport/integrationtest/` — all INV-RA-* green (with `task plugin:build-all` run first so the composition test executes).
- [ ] INV-RA-1…6 each have a passing test (RA-3 and RA-5 share the admin sentinel).
- [ ] `task pr-prep` green.
- [ ] Spec acceptance criteria satisfied: full-stack tests can opt into the real seeded engine; the admin sentinel demonstrates a g776-class default-deny that allow-all masks; docs updated.

## Out of scope (per spec §3)

- Flipping the harness default to production-shape (follow-up bead).
- `holomush-0f0f4.9`'s `wholesystem/abac_test.go` (consumes this substrate; adds the scene-namespace cross-plugin permit/forbid that fully exercises INV-RA-4 end-to-end).
- Plugin event emission.

<!-- adr-capture: sha256=c5dcbafac498bfe6; session=brainstorm-holomush-f5t07; ts=2026-05-26T19:04:01Z; adrs=holomush-jvrq3 -->
