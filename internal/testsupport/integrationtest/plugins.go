// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// assemblePluginsDir and related helpers implement the WithInTreePlugins
// capability; see harness.go for the Server lifecycle they plug into.
package integrationtest

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	policytypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/auth"
	authpostgres "github.com/holomush/holomush/internal/auth/postgres"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/lifecycle"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	pluginsetup "github.com/holomush/holomush/internal/plugin/setup"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
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
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error { //nolint:wrapcheck // test helper: walk errors are filesystem errors from t.TempDir paths
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err //nolint:wrapcheck // test helper: Rel only errors on unrooted paths, not possible here
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755) //nolint:gosec // test-only: 0o755 matches plugin dir permissions in the real tree
		}
		return copyFile(path, target, info.Mode())
	})
}

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

type engineProvider struct {
	eng      policytypes.AccessPolicyEngine
	resolver *attribute.Resolver
	auditor  pluginauthz.Auditor
}

func (p engineProvider) Engine() policytypes.AccessPolicyEngine { return p.eng }
func (p engineProvider) AttributeResolver() *attribute.Resolver { return p.resolver }
func (p engineProvider) AuditLogger() pluginauthz.Auditor       { return p.auditor }

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

// WithInTreePlugins boots the real in-tree plugin layer inside the harness
// CoreServer by reusing production's setup.PluginSubsystem (which calls
// Manager.LoadAll). It is engine-agnostic — compose it with WithPolicyEngine
// for cross-plugin-ABAC coverage. If the binary-plugin artifacts are missing,
// the harness skips the test (run `task plugin:build-all`), unless
// HOLOMUSH_REQUIRE_PLUGINS is set, in which case it fails (INV-WS-3).
//
// Event EMISSION is deliberately NOT wired: startPlugins does not call
// Manager.ConfigureEventEmitter, and the WorldService is built without an
// EventEmitter. The whole-system census suite (holomush-0f0f4.8) reads plugin
// load state and the command registry only, so emit paths are out of scope —
// a test that drives a plugin command/handler which emits events will fail with
// "plugin event emitter is not configured". Wiring the emitter is deferred until
// a suite needs it.
func WithInTreePlugins() StartOption {
	return func(c *startConfig) { c.withPlugins = true }
}

// WithPluginConfigOverrides sets per-plugin config overrides (plugin name →
// key → value) the harness threads into PluginSubsystemConfig.PluginConfigOverrides
// — the same opaque channel production uses (yzt86). Reusable by any plugin's
// harness tests. Empty/absent → manifest defaults.
func WithPluginConfigOverrides(overrides map[string]map[string]string) StartOption {
	return func(c *startConfig) { c.pluginConfigOverrides = overrides }
}

// pluginDeps is the minimal set of already-built harness objects startPlugins
// needs. Start passes these in so this helper stays decoupled from the Server.
type pluginDeps struct {
	pool           *pgxpool.Pool
	connStr        string
	engine         policytypes.AccessPolicyEngine
	sessionStore   session.Access
	verbReg        *core.VerbRegistry
	playerRepo     auth.PlayerRepository
	hasher         auth.PasswordHasher
	playerSess     auth.PlayerSessionRepository // *store.PostgresPlayerSessionStore satisfies this (player_session_store.go:30)
	resolver       *attribute.Resolver
	pluginProvider *attribute.PluginProvider
	auditor        pluginauthz.Auditor
	// policyInstaller routes manifest-policy installs through the engine's OWN
	// cache-wired PolicyInstaller when WithRealABAC is active, so each install
	// fires the store→cache invalidation (setup.go SetOnMutate) and plugin
	// policies become live on the engine the harness evaluates against. nil under
	// the allow-all default → startPlugins builds a fresh standalone installer
	// (there is no engine cache to invalidate). (holomush-0f0f4.9, INV-WS-2)
	policyInstaller *plugins.PolicyInstaller
	// cryptoPublisher is the crypto-enabled publisher (DEK manager + identity
	// codec selector, rendering-wrapped) wired by WithPluginCrypto. When
	// non-nil, startPlugins calls Manager.ConfigureEventEmitter so plugin emits
	// publish through it (link 1: sensitive emits encrypt on the wire). nil
	// leaves the emitter unwired, matching the WithInTreePlugins-only behavior
	// the whole-system census suite relies on.
	cryptoPublisher eventbus.Publisher
	// gameID is the embedded bus game id, threaded into the emitter as a
	// GameIDProvider closure so plugin emits translate legacy colon-style
	// subjects to events.<game_id>.<ns>.<id> consistently.
	gameID string
	// pluginConfigOverrides is the per-plugin opaque config override
	// (plugin name → key → value) threaded into PluginSubsystemConfig.
	pluginConfigOverrides map[string]map[string]string
	// extraPluginDirs holds additional plugin directories (e.g. test-only Lua
	// fixtures) staged into the plugin load path after the in-tree plugins.
	extraPluginDirs []string
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

	for _, extra := range d.extraPluginDirs {
		abs, err := filepath.Abs(extra)
		require.NoError(t, err, "startPlugins: resolve extra plugin dir")
		base := filepath.Base(abs)
		dstSub := filepath.Join(pluginsDst, base)
		// Fail loudly on a basename collision rather than silently overwriting:
		// the staging dest is keyed only by filepath.Base, so two extra dirs (or
		// an extra dir clashing with an in-tree plugin) sharing a final component
		// would otherwise clobber each other.
		_, statErr := os.Stat(dstSub)
		require.Truef(t, os.IsNotExist(statErr),
			"startPlugins: extra plugin dir basename %q collides with an already-staged plugin at %s", base, dstSub)
		require.NoError(t, copyTree(abs, dstSub), "startPlugins: stage extra plugin dir")
	}

	// WorldService — mirror internal/world/setup/subsystem.go. EventEmitter is
	// intentionally omitted (production world/setup omits it too); world.NewService
	// logs a benign slog.Warn. The census suite reads load state + registry only,
	// so emit paths aren't exercised.
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

	// AdminDeps — all 5 caller-supplied fields required (handlers.RegisterAdmin
	// panics on nil; subsystem.go calls it unconditionally). PluginLister is set
	// by the subsystem itself.
	adminDeps := handlers.AdminDeps{
		PlayerRepo:     d.playerRepo,
		Hasher:         d.hasher,
		PlayerSessions: d.playerSess,
		ResetRepo:      authpostgres.NewPasswordResetRepository(d.pool),
		CharLister:     bootstrapsetup.NewCharRepoAdapter(d.pool, worldpostgres.NewCharacterRepository(d.pool)),
	}

	// PolicyInstaller: under WithRealABAC, reuse the engine's OWN installer (over
	// the cache-wired store) so each manifest-policy install trips the engine's
	// store→cache invalidation (setup.go SetOnMutate → cache.Invalidate, which
	// reloads inline unless a reload is already in flight; the engine's poller
	// backstops). Installs run sequentially during LoadAll, before Start returns,
	// so plugin policies are live by the time tests evaluate. Otherwise (allow-all
	// default) a fresh installer over the pool suffices — no engine cache to
	// invalidate.
	policyInst := d.policyInstaller
	if policyInst == nil {
		policyInst = plugins.NewPolicyInstaller(policystore.NewPostgresStore(d.pool))
	}

	// Resolver / plugin provider are caller-supplied (pluginAttrSources): with a
	// real ABAC subsystem they are the engine's OWN instances so plugin-declared
	// providers (e.g. core-scenes' "scene" namespace) register on the resolver the
	// engine evaluates against (INV-RA-4); with allow-all they are fresh standalone
	// instances. Both are non-nil — PluginSubsystem.Start calls
	// resolver.RegisterProvider per plugin that declares resource_types and panics
	// on a nil resolver (subsystem.go:319).
	cfg := pluginsetup.PluginSubsystemConfig{
		DataDir:               dataDir,
		DatabaseConnStr:       d.connStr,
		ABAC:                  engineProvider{eng: d.engine, resolver: d.resolver, auditor: d.auditor},
		PolicyInst:            policyInstallerProvider{inst: policyInst},
		PluginProv:            pluginProviderSetter{pp: d.pluginProvider},
		World:                 worldProvider{svc: worldSvc},
		Sessions:              sessionProvider{store: d.sessionStore},
		AdminDeps:             adminDepsProvider{deps: adminDeps},
		Registry:              lifecycle.NewReadinessRegistry(),
		VerbRegistry:          d.verbReg,
		LuaTimeout:            5 * time.Second,
		LuaRegistryMaxSize:    1024 * 1024,
		PluginConfigOverrides: d.pluginConfigOverrides,
	}

	ps := pluginsetup.NewPluginSubsystem(cfg)
	t.Cleanup(func() { _ = ps.Stop(context.Background()) })
	require.NoError(t, ps.Start(ctx), "startPlugins: PluginSubsystem.Start (LoadAll)")

	// Wire the plugin event emitter to the crypto-enabled publisher when
	// WithPluginCrypto supplied one. WithGameID takes a GameIDProvider
	// (func() string), NOT a string (event_emitter.go:63-64) — wrap the
	// captured gameID in a closure. WithCryptoEnabled(true) turns on the Phase
	// 3a sensitivity fence so sensitivity:always emits (e.g. scene_pose) that
	// claim Sensitive=true publish encrypted.
	if d.cryptoPublisher != nil {
		gameID := d.gameID
		ps.Manager().ConfigureEventEmitter(
			d.cryptoPublisher,
			plugins.WithGameID(func() string { return gameID }),
			plugins.WithCryptoEnabled(true),
		)
	}
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

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err //nolint:wrapcheck // test helper: OS errors pass through directly
	}
	defer in.Close() //nolint:errcheck // read-only; close error inconsequential
	dstDir := filepath.Dir(dst)
	if mkErr := os.MkdirAll(dstDir, 0o755); mkErr != nil { //nolint:gosec // test-only: 0o755 matches plugin dir permissions
		return mkErr //nolint:wrapcheck // test helper: OS errors pass through directly
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err //nolint:wrapcheck // test helper: OS errors pass through directly
	}
	defer out.Close() //nolint:errcheck // final close below is the authoritative error check
	if _, cpErr := io.Copy(out, in); cpErr != nil {
		return cpErr //nolint:wrapcheck // test helper: IO errors pass through directly
	}
	return out.Close() //nolint:wrapcheck // test helper: final flush/close error is surfaced directly
}
