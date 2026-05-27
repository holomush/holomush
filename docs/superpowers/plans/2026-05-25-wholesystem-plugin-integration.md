<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# WithInTreePlugins Harness Capability & Whole-System Integration Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an opt-in `integrationtest.WithInTreePlugins()` harness capability that boots the real in-tree plugin layer by reusing production's `setup.PluginSubsystem`, plus a whole-system integration suite asserting manifest-DAG / load-order / service-registration and cross-plugin-ABAC fidelity.

**Architecture:** `WithInTreePlugins` is a new `StartOption` on `internal/testsupport/integrationtest`. When set, `Start` assembles a unified plugins dir (mirroring the Dockerfile overlay), constructs a `setup.PluginSubsystemConfig` from thin provider adapters over objects the harness already builds, starts the subsystem (which calls `Manager.LoadAll`), and wraps the `CoreServer` dispatcher around the subsystem's populated command registry. The capability is engine-agnostic; the whole-system suite opts into the real seeded ABAC engine (delivered by holomush-f5t07) for its cross-plugin-ABAC assertions.

**Tech Stack:** Go, Ginkgo/Gomega (integration build tag), testify (unit assertions in the harness package), hashicorp/go-plugin (binary plugins), gopher-lua (Lua plugins), Postgres testcontainer, embedded NATS JetStream.

**Spec:** `docs/superpowers/specs/2026-05-25-wholesystem-plugin-integration-design.md`
**Design bead:** holomush-0f0f4 · **Blocked by:** holomush-f5t07 (real seeded ABAC) · **Companion:** holomush-6g45d

---

## Dependency note (read first)

**Phase 1 and Phase 3 are implementable now.** They do not require f5t07 — the
capability proves plugins *load* under the harness's default allow-all engine
(the `PolicyInstaller` installs manifest policies into the policystore
regardless of which engine enforces them).

**Phase 2, Task 9 (cross-plugin-ABAC permit/deny) is BLOCKED on holomush-f5t07.**
It needs a real seeded ABAC engine to make the installed plugin policies actually
enforce. Implement Tasks 1–8, 10, 11 first; Task 9 lands after f5t07 ships its
real-engine harness opt-in. Task 9 documents the exact integration point.

---

## File Structure

| File | Responsibility | Action |
| --- | --- | --- |
| `internal/testsupport/integrationtest/plugins.go` | New: `WithInTreePlugins` option, provider adapters, plugins-dir assembly, binary gate, subsystem lifecycle | Create |
| `internal/testsupport/integrationtest/plugins_test.go` | Unit tests for dir assembly + gate + adapters (no Docker) | Create |
| `internal/testsupport/integrationtest/harness.go` | Reorder `Start` to start the subsystem before the dispatcher when plugins requested; extend `Server` struct, `Stop`, add accessors | Modify |
| `internal/testsupport/integrationtest/invariants_test.go` | New: INV-WS-1 meta-test (source scan, untagged unit test) | Create |
| `internal/testsupport/integrationtest/default_plugins_test.go` | New: INV-WS-4 default-unchanged test (`//go:build integration`, needs Docker) | Create |
| `test/integration/wholesystem/wholesystem_suite_test.go` | New: Ginkgo suite bootstrap (`//go:build integration`) | Create |
| `test/integration/wholesystem/census_test.go` | Structural census specs (plugins loaded, services/commands registered) | Create |
| `test/integration/wholesystem/abac_test.go` | Cross-plugin-ABAC permit/deny specs (Task 9; blocked on f5t07) | Create |
| `Taskfile.yaml` | Set `HOLOMUSH_REQUIRE_PLUGINS=1` for the integration job | Modify |
| `site/docs/contributing/integration-tests.md` | Document the capability, the tier, the build prerequisite | Modify |
| `.claude/rules/testing.md` | Note the whole-system suite as the top Go-fidelity tier | Modify |

---

## Phase 1: The `WithInTreePlugins` Capability

### Task 1: Plugins-dir assembly helper

Assemble a unified plugins dir in a temp dir by overlaying source `plugins/`
then `build/plugins/`, mirroring `Dockerfile:20,25`. Real copies (no symlinks):
`Manager.Discover` skips symlinked dirs and `goplugin.Host` rejects symlinked
binaries (`test/integration/plugin/binary_plugin_test.go:146-157`).

**Files:**

- Create: `internal/testsupport/integrationtest/plugins.go`
- Test: `internal/testsupport/integrationtest/plugins_test.go`

- [ ] **Step 1: Write the failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package integrationtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssemblePluginsDirOverlaysSourceAndBuild(t *testing.T) {
	root := t.TempDir()
	// Fake source tree: two plugin dirs with manifests.
	srcPlugins := filepath.Join(root, "plugins")
	require.NoError(t, os.MkdirAll(filepath.Join(srcPlugins, "core-help"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(srcPlugins, "core-scenes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcPlugins, "core-help", "plugin.yaml"), []byte("name: core-help\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcPlugins, "core-scenes", "plugin.yaml"), []byte("name: core-scenes\n"), 0o644))
	// Fake build tree: core-scenes binary overlay.
	buildPlugins := filepath.Join(root, "build", "plugins")
	require.NoError(t, os.MkdirAll(filepath.Join(buildPlugins, "core-scenes", "linux-amd64"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(buildPlugins, "core-scenes", "linux-amd64", "core-scenes"), []byte("ELF"), 0o755))

	dst := t.TempDir()
	err := assemblePluginsDir(dst, srcPlugins, buildPlugins)
	require.NoError(t, err)

	// Both source manifests present.
	require.FileExists(t, filepath.Join(dst, "core-help", "plugin.yaml"))
	require.FileExists(t, filepath.Join(dst, "core-scenes", "plugin.yaml"))
	// Binary overlay present in the same plugin dir.
	require.FileExists(t, filepath.Join(dst, "core-scenes", "linux-amd64", "core-scenes"))
	// No symlinks (Discover skips them).
	info, err := os.Lstat(filepath.Join(dst, "core-scenes"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Zero(t, info.Mode()&os.ModeSymlink, "plugin dir must be a real dir, not a symlink")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestAssemblePluginsDirOverlaysSourceAndBuild ./internal/testsupport/integrationtest/`
Expected: FAIL — `undefined: assemblePluginsDir`.

- [ ] **Step 3: Write minimal implementation**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package-level helpers for the WithInTreePlugins capability live in this file;
// see harness.go for the Server lifecycle they plug into.
package integrationtest

import (
	"io"
	"os"
	"path/filepath"

	"github.com/samber/oops"
)

// assemblePluginsDir builds a unified plugins directory under dst by copying
// the source plugins tree (Lua/setting manifests + Lua source) then overlaying
// the compiled binary artifacts, mirroring the production image overlay
// (Dockerfile COPY plugins/ then COPY build/plugins/). All copies are real
// files: Manager.Discover skips symlinked dirs and goplugin.Host rejects
// symlinked binaries.
func assemblePluginsDir(dst, srcPlugins, buildPlugins string) error {
	if err := copyTree(srcPlugins, dst); err != nil {
		return oops.Code("PLUGINS_DIR_COPY_SOURCE").Wrap(err)
	}
	// build/plugins may be absent when binaries are not built; the binary gate
	// (Task 2) handles that case. Overlay only if present.
	if _, err := os.Stat(buildPlugins); err == nil {
		if err := copyTree(buildPlugins, dst); err != nil {
			return oops.Code("PLUGINS_DIR_OVERLAY_BUILD").Wrap(err)
		}
	}
	return nil
}

// copyTree recursively copies src into dst, creating dst dirs as needed.
// Existing dst files are overwritten (overlay semantics).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // test-only paths under t.TempDir / repo tree
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode) //nolint:gosec // test-only
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestAssemblePluginsDirOverlaysSourceAndBuild ./internal/testsupport/integrationtest/`
Expected: PASS.

- [ ] **Step 5: Run the linter**

Run: `task lint:go`
Expected: no new findings. (If `gosec` flags the `os.Open`/`os.OpenFile`, the
line-scoped `//nolint:gosec` directives above are the repo-precedent fix — do
not widen `.golangci.yaml`.)

- [ ] **Step 6: Commit**

```bash
jj commit -m "test(integrationtest): plugins-dir overlay assembly helper (holomush-0f0f4)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Binary-plugin availability gate

Resolve the repo's `build/plugins` dir (mirroring `binary_plugin_test.go:46-63`)
and decide skip-vs-fail. Lua/setting plugins never gate; only the binary
artifacts do. Absent + `HOLOMUSH_REQUIRE_PLUGINS` set ⇒ hard fail; absent
otherwise ⇒ skip (INV-WS-3).

**Files:**

- Modify: `internal/testsupport/integrationtest/plugins.go`
- Test: `internal/testsupport/integrationtest/plugins_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBinaryArtifactsPresentDetectsCoreScenes(t *testing.T) {
	root := t.TempDir()
	build := filepath.Join(root, "build", "plugins")
	// Absent → false.
	require.False(t, binaryArtifactsPresent(build))
	// Present (core-scenes for the current platform) → true.
	platform := goPlatformDir()
	require.NoError(t, os.MkdirAll(filepath.Join(build, "core-scenes", platform), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(build, "core-scenes", platform, "core-scenes"), []byte("ELF"), 0o755))
	require.True(t, binaryArtifactsPresent(build))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestBinaryArtifactsPresentDetectsCoreScenes ./internal/testsupport/integrationtest/`
Expected: FAIL — `undefined: binaryArtifactsPresent`.

- [ ] **Step 3: Write minimal implementation**

```go
// Append to internal/testsupport/integrationtest/plugins.go.

import (
	"runtime"
	// ... existing imports
)

// requirePluginsEnv, when truthy, turns a missing binary-plugin artifact into a
// hard failure instead of a skip (INV-WS-3). The CI integration job sets it.
const requirePluginsEnv = "HOLOMUSH_REQUIRE_PLUGINS"

// goPlatformDir is the per-platform subdir name build-plugins.sh emits
// (e.g. "darwin-arm64", "linux-amd64").
func goPlatformDir() string { return runtime.GOOS + "-" + runtime.GOARCH }

// repoBuildPluginsDir resolves the build/plugins directory the same way
// test/integration/plugin/binary_plugin_test.go does: PLUGIN_BINARY_DIR if set,
// else <repoRoot>/build/plugins resolved from this source file's location.
func repoBuildPluginsDir() string {
	if dir := os.Getenv("PLUGIN_BINARY_DIR"); dir != "" {
		return dir
	}
	_, thisFile, _, _ := runtime.Caller(0)
	// internal/testsupport/integrationtest/plugins.go → repo root is 3 dirs up.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(repoRoot, "build", "plugins")
}

// repoPluginsSrcDir resolves the source plugins/ tree from this file's location.
func repoPluginsSrcDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(repoRoot, "plugins")
}

// binaryArtifactsPresent reports whether the core-scenes binary for the current
// platform exists under buildDir. core-scenes is the canonical production binary
// plugin; if it built, the rest did too (single build-plugins.sh pass).
func binaryArtifactsPresent(buildDir string) bool {
	exe := filepath.Join(buildDir, "core-scenes", goPlatformDir(), "core-scenes")
	info, err := os.Stat(exe)
	return err == nil && !info.IsDir()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestBinaryArtifactsPresentDetectsCoreScenes ./internal/testsupport/integrationtest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "test(integrationtest): binary-plugin availability gate detection (holomush-0f0f4)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Provider adapters

Thin adapters satisfying the six `PluginSubsystemConfig` provider interfaces
(`subsystem.go:40-69`) over objects the harness already constructs, plus the
`AdminDeps` and `WorldService` construction. Pure value wiring; unit-testable
without Docker.

**Files:**

- Modify: `internal/testsupport/integrationtest/plugins.go`
- Test: `internal/testsupport/integrationtest/plugins_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPluginProvidersSatisfyInterfaces(t *testing.T) {
	// Compile-time interface satisfaction is the real assertion; this test
	// exists so the file fails to build if an adapter drifts from its iface.
	var (
		_ setup.EngineProvider          = engineProvider{}
		_ setup.SessionProvider         = sessionProvider{}
		_ setup.WorldServiceProvider    = worldProvider{}
		_ setup.AdminDepsProvider       = adminDepsProvider{}
		_ setup.PolicyInstallerProvider = policyInstallerProvider{}
		_ setup.PluginProviderSetter    = pluginProviderSetter{}
	)
	require.NotNil(t, &engineProvider{})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestPluginProvidersSatisfyInterfaces ./internal/testsupport/integrationtest/`
Expected: FAIL — undefined adapter types.

- [ ] **Step 3: Write minimal implementation**

```go
// Append to internal/testsupport/integrationtest/plugins.go.

import (
	pluginsetup "github.com/holomush/holomush/internal/plugin/setup"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	policytypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	// ... existing imports
)

type engineProvider struct{ eng policytypes.AccessPolicyEngine }

func (p engineProvider) Engine() policytypes.AccessPolicyEngine { return p.eng }

type sessionProvider struct{ store session.Access }

func (p sessionProvider) SessionStore() session.Access { return p.store }

type worldProvider struct{ svc *world.Service }

func (p worldProvider) Service() *world.Service { return p.svc }

type adminDepsProvider struct{ deps handlers.AdminDeps }

func (p adminDepsProvider) AdminDeps() handlers.AdminDeps { return p.deps }

type policyInstallerProvider struct{ inst *plugins.PolicyInstaller }

func (p policyInstallerProvider) PolicyInstaller() *plugins.PolicyInstaller { return p.inst }

type pluginProviderSetter struct{ pp *attribute.PluginProvider }

func (p pluginProviderSetter) PluginProvider() *attribute.PluginProvider { return p.pp }
```

Note: alias the import as `pluginsetup` but the test references `setup.*`. Use a
single consistent alias — change the test's `setup.` references to `pluginsetup.`
(or import as `setup`). This plan uses `pluginsetup` everywhere; update Step 1's
test to `pluginsetup.EngineProvider` etc. before running.

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestPluginProvidersSatisfyInterfaces ./internal/testsupport/integrationtest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
jj commit -m "test(integrationtest): plugin-subsystem provider adapters (holomush-0f0f4)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `WithInTreePlugins` option + subsystem startup wiring

Add the `StartOption`; build the `PluginSubsystem` from the adapters; start it
*before* the dispatcher; feed `subsystem.CommandRegistry()` to the dispatcher so
plugin commands are dispatchable (mirrors `sub_grpc.go:189,306`). Gate on binary
artifacts. This task touches the integration path and so its behavioral test is a
Ginkgo spec under the integration tag; a lighter unit test covers the option
plumbing.

**Files:**

- Modify: `internal/testsupport/integrationtest/plugins.go` (the `startPlugins` helper + `WithInTreePlugins`)
- Modify: `internal/testsupport/integrationtest/harness.go` (`startConfig`, `Start` reorder, `Server` fields)

- [ ] **Step 1: Extend `startConfig` and add the option**

In `harness.go`, extend `startConfig` (currently `harness.go:126-128`):

```go
type startConfig struct {
	accessEngine types.AccessPolicyEngine
	withPlugins  bool
}
```

Add to `plugins.go`:

```go
// WithInTreePlugins boots the real in-tree plugin layer inside the harness
// CoreServer by reusing production's setup.PluginSubsystem (which calls
// Manager.LoadAll). It is engine-agnostic — compose it with WithPolicyEngine
// for cross-plugin-ABAC coverage. If the binary-plugin artifacts are missing,
// the harness skips the test (run `task plugin:build-all`), unless
// HOLOMUSH_REQUIRE_PLUGINS is set, in which case it fails (INV-WS-3).
func WithInTreePlugins() StartOption {
	return func(c *startConfig) { c.withPlugins = true }
}
```

- [ ] **Step 2: Add the `startPlugins` helper**

In `plugins.go`:

```go
import (
	"context"
	"github.com/holomush/holomush/internal/access/policy/store"
	authpostgres "github.com/holomush/holomush/internal/auth/postgres"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/jackc/pgx/v5/pgxpool"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	// ... existing imports
)

// pluginDeps is the minimal set of already-built harness objects startPlugins
// needs. Start passes these in so this helper stays decoupled from the Server.
type pluginDeps struct {
	pool         *pgxpool.Pool
	connStr      string
	engine       policytypes.AccessPolicyEngine
	sessionStore session.Access
	verbReg      *core.VerbRegistry
	playerRepo   auth.PlayerRepository
	hasher       auth.PasswordHasher
	playerSess   auth.PlayerSessionRepository // *store.PostgresPlayerSessionStore satisfies this (player_session_store.go:19)
}

// startPlugins constructs and starts a PluginSubsystem mirroring production
// (cmd/holomush/sub_grpc.go + internal/plugin/setup). It returns the started
// subsystem; callers register Stop via t.Cleanup and read CommandRegistry()
// for the dispatcher. It calls t.Skip/t.Fatal on a missing binary gate.
func startPlugins(t *testing.T, ctx context.Context, d pluginDeps) *pluginsetup.PluginSubsystem {
	t.Helper()

	buildDir := repoBuildPluginsDir()
	if !binaryArtifactsPresent(buildDir) {
		msg := "in-tree binary plugins not built; run `task plugin:build-all`"
		if isTruthy(os.Getenv(requirePluginsEnv)) {
			t.Fatalf("%s (HOLOMUSH_REQUIRE_PLUGINS set)", msg)
		}
		t.Skip(msg)
	}

	// Assemble the unified plugins dir under a per-test temp DataDir.
	dataDir := t.TempDir()
	pluginsDst := filepath.Join(dataDir, "plugins")
	require.NoError(t,
		assemblePluginsDir(pluginsDst, repoPluginsSrcDir(), buildDir),
		"startPlugins: assemble plugins dir")

	// WorldService — mirror internal/world/setup/subsystem.go:64-76.
	// EventEmitter is intentionally omitted (production world/setup omits it too);
	// world.NewService logs a benign slog.Warn (service.go:64-66). The census
	// suite reads load state + registry only, so emit paths aren't exercised.
	worldSvc := world.NewService(world.ServiceConfig{
		LocationRepo:  worldpostgres.NewLocationRepository(d.pool),
		ExitRepo:      worldpostgres.NewExitRepository(d.pool),
		ObjectRepo:    worldpostgres.NewObjectRepository(d.pool),
		SceneRepo:     worldpostgres.NewSceneRepository(d.pool),
		CharacterRepo: worldpostgres.NewCharacterRepository(d.pool),
		PropertyRepo:  worldpostgres.NewPropertyRepository(d.pool),
		Engine:        d.engine,
		Transactor:    worldpostgres.NewTransactor(d.pool),
	})

	// AdminDeps — all 5 fields required (handlers.RegisterAdmin panics on nil,
	// register.go:12-24; subsystem.go:322 calls it unconditionally).
	adminDeps := handlers.AdminDeps{
		PlayerRepo:     d.playerRepo,
		Hasher:         d.hasher,
		PlayerSessions: d.playerSess,
		ResetRepo:      authpostgres.NewPasswordResetRepository(d.pool),
		CharLister:     bootstrapsetup.NewCharRepoAdapter(d.pool, worldpostgres.NewCharacterRepository(d.pool)),
	}

	// PolicyInstaller over a real policystore so manifest policies install.
	policyInst := plugins.NewPolicyInstaller(store.NewPostgresStore(d.pool))

	cfg := pluginsetup.PluginSubsystemConfig{
		DataDir:            dataDir,
		DatabaseConnStr:    d.connStr,
		ABAC:               engineProvider{eng: d.engine},
		PolicyInst:         policyInstallerProvider{inst: policyInst},
		PluginProv:         pluginProviderSetter{pp: attribute.NewPluginProvider(nil)},
		World:              worldProvider{svc: worldSvc},
		Sessions:           sessionProvider{store: d.sessionStore},
		AdminDeps:          adminDepsProvider{deps: adminDeps},
		Registry:           lifecycle.NewReadinessRegistry(),
		VerbRegistry:       d.verbReg,
		LuaTimeout:         5 * time.Second,
		LuaRegistryMaxSize: 1024 * 1024,
	}

	ps := pluginsetup.NewPluginSubsystem(cfg)
	require.NoError(t, ps.Start(ctx), "startPlugins: PluginSubsystem.Start (LoadAll)")
	t.Cleanup(func() { _ = ps.Stop(context.Background()) })
	return ps
}

// isTruthy treats "1", "true", "yes" (case-insensitive) as true.
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	}
	return false
}
```

- [ ] **Step 3: Reorder `Start` to wire the subsystem registry into the dispatcher**

In `harness.go`, the current order is: build empty `dispatcher` (`:214`),
build `engine`/`historyReader`/`verbRegistry`, build `coreServer` (`:236`).
Change to: resolve options, build `verbRegistry`, then — **when
`cfg.withPlugins`** — call `startPlugins` and use its `CommandRegistry()` for the
dispatcher; otherwise keep the empty registry. Replace the dispatcher
construction block (`harness.go:213-216`) with:

```go
	// VerbRegistry must exist before plugins load (they register verbs).
	verbRegistry, err := core.BootstrapVerbRegistry("test")
	require.NoError(t, err, "integrationtest.Start: BootstrapVerbRegistry")

	var pluginSub *pluginsetup.PluginSubsystem
	cmdRegistry := command.NewRegistry()
	if cfg.withPlugins {
		pluginSub = startPlugins(t, ctx, pluginDeps{
			pool:         pool,
			connStr:      connStr,
			engine:       pe,
			sessionStore: sessionStoreInst,
			verbReg:      verbRegistry,
			playerRepo:   playerRepo,
			hasher:       hasher,
			playerSess:   playerSessionStore,
		})
		cmdRegistry = pluginSub.CommandRegistry()
	}

	dispatcher, err := command.NewDispatcher(cmdRegistry, pe)
	require.NoError(t, err, "integrationtest.Start: create command dispatcher")
	cmdServices := command.NewTestServices(command.ServicesConfig{Engine: pe})
```

Then delete the now-duplicated `verbRegistry` construction at the original
`harness.go:232` and the old dispatcher block at `:213-216`. Add `pluginSub` to
the returned `Server` literal (`:263-276`):

```go
	return &Server{
		// ... existing fields ...
		pluginSub: pluginSub,
	}
```

- [ ] **Step 4: Add the field to `Server`**

In the `Server` struct (`harness.go:94-119`) add:

```go
	// pluginSub is the started plugin subsystem when WithInTreePlugins was
	// passed; nil otherwise. Stopped via t.Cleanup registered in startPlugins.
	pluginSub *pluginsetup.PluginSubsystem
```

- [ ] **Step 5: Verify it compiles**

Run: `task test -- -run TestPluginProvidersSatisfyInterfaces ./internal/testsupport/integrationtest/`
Expected: PASS (package compiles; the integration behavior is exercised in Task 8).

- [ ] **Step 6: Lint**

Run: `task lint:go`
Expected: no new findings.

- [ ] **Step 7: Commit**

```bash
jj commit -m "feat(integrationtest): WithInTreePlugins option + subsystem startup wiring (holomush-0f0f4)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `Server.Stop` teardown + plugin accessors

`Server.Stop` is currently a no-op (`harness.go:281-283`); subsystem teardown is
already registered via `t.Cleanup` in `startPlugins`, but `Stop` should also stop
it for callers that `defer Stop()` explicitly and want deterministic ordering.
Add read accessors for the whole-system suite.

**Files:**

- Modify: `internal/testsupport/integrationtest/harness.go`
- Test: covered by Task 8's suite + the Task 7 default test (no separate unit test — accessors panic-guard mirror the subsystem's own contract).

- [ ] **Step 1: Implement Stop + accessors**

Replace `Stop` (`harness.go:281-283`) and add accessors:

```go
// Stop tears down the in-process stack. Idempotent. Postgres and NATS cleanup
// are handled by t.Cleanup handlers registered in Start; the plugin subsystem
// (if started) is stopped here and is also t.Cleanup-registered as a safety net.
func (s *Server) Stop() {
	if s.pluginSub != nil {
		_ = s.pluginSub.Stop(context.Background())
	}
}

// PluginManager returns the loaded plugin Manager. Panics if WithInTreePlugins
// was not passed to Start.
func (s *Server) PluginManager() *plugins.Manager {
	s.requirePlugins("PluginManager")
	return s.pluginSub.Manager()
}

// CommandRegistry returns the plugin-populated command registry (builtins +
// admin + plugin commands). Panics if WithInTreePlugins was not passed.
func (s *Server) CommandRegistry() *command.Registry {
	s.requirePlugins("CommandRegistry")
	return s.pluginSub.CommandRegistry()
}

// ServiceRegistry returns the plugin service registry. Panics if
// WithInTreePlugins was not passed.
func (s *Server) ServiceRegistry() *plugins.ServiceRegistry {
	s.requirePlugins("ServiceRegistry")
	return s.pluginSub.ServiceRegistry()
}

func (s *Server) requirePlugins(method string) {
	if s.pluginSub == nil {
		panic("integrationtest: " + method + "() requires Start(t, WithInTreePlugins())")
	}
}
```

Add imports to `harness.go` if not present: `plugins "github.com/holomush/holomush/internal/plugin"`, `pluginsetup "github.com/holomush/holomush/internal/plugin/setup"`, `"github.com/holomush/holomush/internal/command"`.

- [ ] **Step 2: Verify it compiles**

Run: `task test -- -run TestPluginProvidersSatisfyInterfaces ./internal/testsupport/integrationtest/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
jj commit -m "feat(integrationtest): plugin subsystem teardown + accessors (holomush-0f0f4)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: INV-WS-1 meta-test (reuse, not replicate)

Guard that the capability routes through `setup.PluginSubsystem` and never calls
`plugins.NewManager` directly in the harness package.

**Files:**

- Create: `internal/testsupport/integrationtest/invariants_test.go`

- [ ] **Step 1: Write the test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package integrationtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWithInTreePluginsReusesSubsystem enforces INV-WS-1: the capability MUST
// reuse setup.PluginSubsystem and MUST NOT construct plugins.NewManager directly
// in this package. Guards against a future refactor that silently forks the
// production wiring.
func TestWithInTreePluginsReusesSubsystem(t *testing.T) {
	src, err := os.ReadFile("plugins.go")
	require.NoError(t, err)
	body := string(src)
	require.Contains(t, body, "pluginsetup.NewPluginSubsystem(",
		"INV-WS-1: capability must construct the PluginSubsystem")
	require.NotContains(t, body, "plugins.NewManager(",
		"INV-WS-1: capability must not construct plugins.NewManager directly — reuse PluginSubsystem")
	_ = filepath.Separator // keep filepath imported if unused elsewhere
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `task test -- -run TestWithInTreePluginsReusesSubsystem ./internal/testsupport/integrationtest/`
Expected: PASS (Task 4 used `pluginsetup.NewPluginSubsystem` and no `plugins.NewManager`).

- [ ] **Step 3: Commit**

```bash
jj commit -m "test(integrationtest): INV-WS-1 reuse-PluginSubsystem meta-test (holomush-0f0f4)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: INV-WS-4 default-unchanged guarantee

Prove that omitting `WithInTreePlugins` leaves the harness plugin-free
(`pluginSub == nil`), so targeted suites are unaffected.

**Files:**

- Create: `internal/testsupport/integrationtest/default_plugins_test.go`

> INV-WS-4 lives in its own `//go:build integration` file (not `invariants_test.go`,
> which is an untagged unit test) because it starts the real harness and so
> requires Docker — matching the rest of the suite's runtime needs.

- [ ] **Step 1: Write the test (integration-tagged)**

Create `internal/testsupport/integrationtest/default_plugins_test.go`:

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package integrationtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStartWithoutPluginsLeavesHarnessPluginFree enforces INV-WS-4: omitting
// WithInTreePlugins yields a plugin-free harness; the plugin accessors panic.
func TestStartWithoutPluginsLeavesHarnessPluginFree(t *testing.T) {
	srv := Start(t)
	defer srv.Stop()
	require.Nil(t, srv.pluginSub, "default Start must not start the plugin subsystem")
	require.Panics(t, func() { _ = srv.PluginManager() },
		"PluginManager must panic when plugins were not requested")
}
```

- [ ] **Step 2: Run it**

Run: `task test:int -- -run TestStartWithoutPluginsLeavesHarnessPluginFree ./internal/testsupport/integrationtest/`
Expected: PASS (Docker required).

- [ ] **Step 3: Commit**

```bash
jj commit -m "test(integrationtest): INV-WS-4 default-plugin-free guarantee (holomush-0f0f4)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Phase 2: The Whole-System Suite

### Task 8: Suite bootstrap + structural census

New Ginkgo package loading all in-tree plugins; assert the census. Works under
the harness default allow-all engine (no f5t07 needed).

**Files:**

- Create: `test/integration/wholesystem/wholesystem_suite_test.go`
- Create: `test/integration/wholesystem/census_test.go`

- [ ] **Step 1: Suite bootstrap**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package wholesystem_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWholeSystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Whole-System Plugin Integration Suite")
}
```

- [ ] **Step 2: Write the census specs**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package wholesystem_test

import (
	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// expectedPlugins is the in-tree set discover-all loads (spec §1: 10 plugins).
var expectedPlugins = []string{
	"core-aliases", "core-building", "core-communication", "core-help",
	"core-objects", "core-scenes", "echo-bot", "setting-crossroads",
	"setting-skeleton", "test-abac-widget",
}

var _ = Describe("whole-system plugin load (INV-5)", Ordered, func() {
	var srv *integrationtest.Server

	BeforeAll(func() {
		srv = integrationtest.Start(GinkgoT(), integrationtest.WithInTreePlugins())
	})
	AfterAll(func() {
		if srv != nil {
			srv.Stop()
		}
	})

	It("loads every in-tree plugin via Manager.LoadAll", func() {
		loaded := srv.PluginManager().ListPlugins() // []string, manager.go:1241
		for _, name := range expectedPlugins {
			Expect(loaded).To(ContainElement(name), "plugin %q must load", name)
		}
	})

	It("registers plugin commands in the dispatcher registry", func() {
		reg := srv.CommandRegistry()
		// core-help provides `help`. Registry.Get(name) (CommandEntry, bool)
		// at registry.go:62.
		_, ok := reg.Get("help")
		Expect(ok).To(BeTrue(), "core-help command must be registered")
	})
})
```

> **Implementer note:** `Manager.ListPlugins()` (`internal/plugin/manager.go:1241`)
> returns loaded plugin names; confirm it returns *loaded* (not merely
> *discovered*) names — if discovery and load can diverge, assert against the
> loaded set. `command.Registry.Get` is verified at `registry.go:62`.

- [ ] **Step 3: Build the binaries, then run**

Run: `task plugin:build-all`
Run: `task test:int -- ./test/integration/wholesystem/...`
Expected: PASS (or SKIP only if binaries truly absent — they were just built).

- [ ] **Step 4: Commit**

```bash
jj commit -m "test(wholesystem): suite bootstrap + structural plugin-load census (holomush-0f0f4)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Cross-plugin-ABAC permit/deny (BLOCKED on holomush-f5t07)

> **BLOCKED:** This task needs a real seeded ABAC engine. Implement it only after
> holomush-f5t07 ships its real-engine harness opt-in. The exact option name is
> whatever f5t07 lands (the spec hedges `WithRealABAC`); wire to that. Until
> then, leave this file uncreated — Tasks 1–8, 10, 11 deliver the rest.

INV-WS-2: assert ≥1 permit and ≥1 forbid against the real engine, exercising a
plugin-installed manifest policy. `test-abac-widget` ships
`widget-read-normal` (permit) and `widget-forbid-restricted` (forbid)
(`plugins/test-abac-widget/plugin.yaml:31-36`).

**Files:**

- Create: `test/integration/wholesystem/abac_test.go`

- [ ] **Step 1: Write the permit/deny specs**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package wholesystem_test

import (
	"context"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	// policytest "<f5t07's seeded-engine helper package>" — wire to f5t07.
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cross-plugin ABAC (INV-WS-2)", Ordered, func() {
	var srv *integrationtest.Server

	BeforeAll(func() {
		// Replace WithRealABAC() with the exact opt-in f5t07 ships.
		srv = integrationtest.Start(GinkgoT(),
			integrationtest.WithInTreePlugins(),
			integrationtest.WithRealABAC(),
		)
	})
	AfterAll(func() {
		if srv != nil {
			srv.Stop()
		}
	})

	It("permits widget read on a normal widget (plugin-installed permit policy)", func() {
		// Construct the AccessRequest a `widget read` produces and evaluate it
		// against srv's real engine. The widget resolver (test-abac-widget)
		// resolves widget.type=normal → widget-read-normal permits.
		// Use srv's engine accessor (added by f5t07) + the request shape from
		// abac_widget_test.go:314-352 as the reference.
		_ = context.Background()
		Skip("implement once f5t07 exposes the real engine handle on Server")
	})

	It("forbids widget read on a restricted widget (plugin-installed forbid policy)", func() {
		Skip("implement once f5t07 exposes the real engine handle on Server")
	})
})
```

> The `Skip` placeholders are intentional **only** because this task is gated on
> f5t07; do not merge them as permanent. When f5t07 lands, replace each `Skip`
> with a real evaluation mirroring `test/integration/plugin/abac_widget_test.go:314-352`
> (instance-level permit/forbid) and remove this note.

- [ ] **Step 2: Run after f5t07 lands**

Run: `task test:int -- ./test/integration/wholesystem/...`
Expected: PASS with ≥1 permit and ≥1 forbid asserted.

- [ ] **Step 3: Commit**

```bash
jj commit -m "test(wholesystem): cross-plugin-ABAC permit/deny against real engine (holomush-0f0f4)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: CI guard — `HOLOMUSH_REQUIRE_PLUGINS`

Make CI fail (not skip) if the binary artifacts are missing when the suite runs,
closing the false-green hole (INV-WS-3).

**Files:**

- Modify: `Taskfile.yaml` (the `test:int` target, `Taskfile.yaml:152-163`)

- [ ] **Step 1: Set the env var on the integration target**

In the `test:int` task, add `HOLOMUSH_REQUIRE_PLUGINS: "1"` to the task's `env:`
block (or export it before the `gotestsum` invocation). Since `test:int` already
runs `plugin:build-all` first, the artifacts will be present and the guard never
trips in the happy path — it only fires if the build step is removed or fails
silently. Exact edit:

```yaml
  test:int:
    desc: Run integration tests (needs Docker)
    env:
      HOLOMUSH_REQUIRE_PLUGINS: "1"
    cmds:
      - task: plugin:build-all
      - gotestsum {{.GOTESTSUM_FLAGS}} -- -tags=integration {{.CLI_ARGS | default "./..."}}
```

> **Implementer:** read the real `test:int` block first (`Taskfile.yaml:152-163`)
> and preserve its existing `cmds`/flags verbatim — only add the `env:` key. Do
> not rewrite the gotestsum invocation from this plan's approximation.

- [ ] **Step 2: Verify the guard fires when artifacts are absent**

```bash
rm -rf build/plugins
HOLOMUSH_REQUIRE_PLUGINS=1 task test:int -- -run TestWholeSystem ./test/integration/wholesystem/ 2>&1 | head -40
```

Expected: the suite **fails** (not skips) with the "HOLOMUSH_REQUIRE_PLUGINS set"
message. Then `task plugin:build-all` to restore.

- [ ] **Step 3: Commit**

```bash
jj commit -m "ci(test): require built plugins in the integration job (INV-WS-3, holomush-0f0f4)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Phase 3: Documentation (PR-blocking)

### Task 11: Contributor docs + harness doc-comment

**Files:**

- Modify: `site/docs/contributing/integration-tests.md`
- Modify: `.claude/rules/testing.md`
- Modify: `internal/testsupport/integrationtest/plugins.go` (package/option doc-comments — done inline in Tasks 1–5; verify completeness)

- [ ] **Step 1: Document the capability + tier in `integration-tests.md`**

Add a section "Whole-system plugin tier" covering: `WithInTreePlugins()` opt-in,
that it reuses `PluginSubsystem` (production path), the `task plugin:build-all`
prerequisite, skip-vs-`HOLOMUSH_REQUIRE_PLUGINS`-fail behavior, and that targeted
suites stay plugin-free. Include a Mermaid `sequenceDiagram` mirroring spec §5
(per `feedback_mermaid_diagrams` — render diagrams as Mermaid, not ASCII).

- [ ] **Step 2: Note the tier in `.claude/rules/testing.md`**

Add the whole-system suite to the tier taxonomy as the top Go-fidelity tier
(loads plugins via `Manager.LoadAll`); cross-reference INV-5 / INV-WS-1.

- [ ] **Step 3: Verify docs format**

Run: `task fmt`
Run: `task lint:docs-symmetry` (CLAUDE.md↔AGENTS.md symlink integrity unaffected, but run to be safe)
Expected: clean.

- [ ] **Step 4: Commit**

```bash
jj commit -m "docs(testing): document whole-system plugin tier + WithInTreePlugins (holomush-0f0f4)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Final Verification

- [ ] `task lint` — clean
- [ ] `task plugin:build-all && task test:int -- ./test/integration/wholesystem/...` — green (Task 9 specs skip until f5t07)
- [ ] `task test -- ./internal/testsupport/integrationtest/...` — unit tests green
- [ ] INV-WS-1 meta-test passes; no `plugins.NewManager(` in the `integrationtest` package
- [ ] Targeted suites (privacy, presence) still green — `task test:int -- ./test/integration/privacy/... ./test/integration/presence/...`
- [ ] `task pr-prep` — green before push

## Spec coverage map

| Spec requirement | Task |
| --- | --- |
| §4.1 reuse PluginSubsystem | 3, 4 |
| §4.2 plugins-dir overlay assembly | 1 |
| §4.3 discover-all | 4, 8 |
| §4.4 binary gate (skip / CI-fail) | 2, 4, 10 |
| §4.5 teardown + accessors | 5 |
| §5 structural census | 8 |
| §5 cross-plugin-ABAC permit/deny | 9 (blocked on f5t07) |
| §5 CI guard | 10 |
| §7 INV-5 | 4, 8 |
| §7 INV-WS-1 | 6 |
| §7 INV-WS-2 | 9 |
| §7 INV-WS-3 | 2, 10 |
| §7 INV-WS-4 | 7 |
| §8 docs | 11 |

<!-- adr-capture: sha256=217bf79c8d7d1356, session=brainstorm-holomush-0f0f4, ts=2026-05-25, adrs= -->
