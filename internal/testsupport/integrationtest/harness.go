// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// Package integrationtest provides a general-purpose integration-test
// harness that wraps a real in-process holomush stack — Postgres
// (testcontainers), embedded NATS JetStream, and the production CoreServer —
// so test files can express invariants against live gRPC handlers without
// mocking the access-control/event-delivery surface.
//
// Originally built for the holomush-iwzt history-scope privacy epic
// (formerly named "privacytest"); now also serves the holomush-5b2j presence
// snapshot tests, the holomush-e4qo location_state wire-format test, and
// future privacy/session/scene integration suites. Renamed to
// "integrationtest" to reflect this broader scope.
//
// Test packages that currently import this harness:
//
//   - test/integration/privacy/   (iwzt history-scope privacy invariants)
//   - test/integration/presence/  (5b2j presence snapshot semantics)
//
// Stack composition:
//
//   - Shared Postgres testcontainer with migrations applied + per-test DB
//   - Embedded NATS JetStream (in-memory, per-test isolation)
//   - Production CoreServer wired to the above via real options
//
// Default ABAC engine is allow-all (privacy tests focus on session/history
// gates, not role enforcement). Tests that need denial-path coverage pass
// WithPolicyEngine(policytest.DenyAllEngine()) — see iwzt.10 / iwzt.11 for
// usage. WithRealABAC opts into the real seeded ABAC engine (production's
// abacsetup.NewABACSubsystem path), making character_roles load-bearing:
// ConnectAuthedWithRoles grants role-based seed:* permits while a roleless
// ConnectAuthed receives only what seed:* grants a roleless character.
//
// Helper categories:
//
//   - Real-path drivers (e.g., EmitDirectEvent, ConnectGuest, ConnectAuthed):
//     exercise actual production code paths.
//   - Test-only escape hatches (e.g., MoveTo, DeleteCharacter, DeleteSession,
//     ExpireSession, SetLocationArrivedAt): direct SQL mutations used to
//     produce state shapes that production paths can't easily generate from
//     a test (e.g., expired sessions, future-dated LocationArrivedAt, guest
//     character cleanup that production logout doesn't perform). Each helper
//     documents what it bypasses and why.
//
// Usage:
//
//	ts := integrationtest.Start(t)
//	defer ts.Stop()
//	sess := ts.ConnectGuest(ctx)
//	sess.SendCommand(ctx, "look")
//	sess.Logout(ctx)
//
// Build tag: integration. This package is never imported by production code.
package integrationtest

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
	abacsetup "github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/auth"
	authpg "github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	authguardaudit "github.com/holomush/holomush/internal/eventbus/authguard/audit"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventbus/history"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/grpc/focus/scenepolicy"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/naming"
	"github.com/holomush/holomush/internal/pgnanos"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsetup "github.com/holomush/holomush/internal/plugin/setup"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telnet"
	"github.com/holomush/holomush/internal/world"
	worldpg "github.com/holomush/holomush/internal/world/postgres"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
	"github.com/holomush/holomush/test/testutil"
)

// Server is the privacy-test harness wrapping a real in-process holomush
// stack (Postgres + NATS JetStream + CoreServer) for integration testing of
// holomush-iwzt history-scope privacy invariants.
//
// Nine downstream integration tasks (iwzt.9 and later) depend on this
// package. Methods that rely on iwzt-introduced RPCs or fields not yet
// implemented will panic via t.Fatalf with a TODO message directing the
// implementer to the relevant bead.
type Server struct {
	t *testing.T

	// pool is the shared Postgres connection pool.
	pool *pgxpool.Pool

	// stores / repos
	playerSessionStore *store.PostgresPlayerSessionStore
	playerRepo         *authpg.PlayerRepository
	charRepo           auth.CharacterRepository
	sessionStore       session.Store
	locRepo            *worldpg.LocationRepository

	// services
	authService *auth.Service
	guestSvc    *auth.GuestService

	// bus (embedded NATS JetStream)
	bus *eventbustest.Embedded

	// coreServer is the in-process CoreServer (no network transport).
	coreServer *holoGRPC.CoreServer

	// pluginSub is the started plugin subsystem when WithInTreePlugins was
	// passed; nil otherwise. Stopped via t.Cleanup registered in startPlugins.
	pluginSub *pluginsetup.PluginSubsystem

	// pluginCrypto is the plugin-crypto substrate (ephemeral KEK + pool-backed
	// DEK manager + crypto-enabled publisher) wired when WithPluginCrypto was
	// passed; nil otherwise. The emit + wire-codec + DEK-count helpers in
	// crypto.go require it (requirePluginCrypto panics when absent). Retained on
	// the Server for Task 8's audit/read-back helpers.
	pluginCrypto *pluginCrypto

	// pluginConsumers is the per-plugin audit projection (link 3), wired when
	// WithPluginCrypto was passed; nil otherwise. Stopped via t.Cleanup in Start.
	pluginConsumers *audit.PluginConsumerManager

	// readbackDecryptor is the host read-back decryptor (link 4), wired when
	// WithPluginCrypto was passed; nil otherwise. ReadBackOwnRows drives it.
	readbackDecryptor *history.ReadbackDecryptor

	// readbackAuditCount counts read-back audit emissions on the
	// audit.<game_id>.plugin_decrypt.<plugin> subjects (read by ReadBackAuditCount).
	readbackAuditCount atomic.Int64

	// readbackAuditEm is the read-back audit emitter (link 4); its drain
	// goroutines are stopped via t.Cleanup in Start. nil unless WithPluginCrypto.
	readbackAuditEm *authguardaudit.Emitter

	// histCrypto bundles the shared crypto substrate (AuthGuard + session
	// bridges + audit emitter) used by BOTH the host history reader
	// (readerCryptoOptions, threaded into history.NewReader under
	// WithPluginCrypto) and the read-back decryptor (configureReadback). Built
	// once by buildHistoryCrypto so the two surfaces share one guard instance
	// and one audit emitter (DRY — no divergent guards). nil unless
	// WithPluginCrypto.
	histCrypto *historyCrypto

	// accessEngine is the ABAC policy engine the stack evaluates against: the
	// allow-all default, a WithPolicyEngine override, or — under WithRealABAC —
	// the real seeded engine (abacSub.Engine()). Exposed via AccessEngine() so
	// whole-system tests can evaluate plugin-installed manifest policies (e.g.
	// test-abac-widget's widget-read-normal / widget-forbid-restricted) directly
	// against the same engine the harness wired the plugin attribute resolvers
	// onto (holomush-0f0f4.9, INV-WS-2).
	accessEngine types.AccessPolicyEngine

	// guestStartLocationID is the location all guests are placed into.
	guestStartLocationID ulid.ULID
}

// StartOption tunes Start construction. Tests pass options to override
// harness defaults (e.g., the ABAC policy engine).
type StartOption func(*startConfig)

// startConfig holds resolved Start options.
type startConfig struct {
	accessEngine      types.AccessPolicyEngine
	withPlugins       bool
	withRealABAC      bool
	withPluginCrypto  bool
	withFocusDelivery bool
	// pluginConfigOverrides is the per-plugin opaque config override
	// (plugin name → key → value) threaded into PluginSubsystemConfig.
	pluginConfigOverrides map[string]map[string]string
	// extraPluginDirs holds additional plugin directories (e.g. test-only Lua
	// fixtures) staged into the plugin load path alongside the in-tree plugins.
	extraPluginDirs []string
}

// WithPolicyEngine overrides the harness's default allow-all ABAC engine.
// Tests that need to exercise denial paths — e.g., the I-PRIV-1 hard-gate
// (iwzt.10) or the I-PRIV-5 wire-opacity meta-test (iwzt.11) — pass a
// stricter engine such as policytest.DenyAllEngine so staffOverride
// returns false and the hard-gate is exercised end-to-end.
func WithPolicyEngine(eng types.AccessPolicyEngine) StartOption {
	return func(c *startConfig) { c.accessEngine = eng }
}

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

// WithFocusDelivery wires a real focus.Coordinator + SessionStreamRegistry into
// the harness (mirroring production cmd/holomush/sub_grpc.go:428-470) so the
// REAL `scene join` command path reaches JoinFocus → AutoFocusOnJoin →
// per-connection subscription delivery. Without it, the plugin host-service
// JoinFocus RPC short-circuits with "focus coordinator not configured"
// (internal/plugin/goplugin/host_service.go:113) and no scene-stream
// subscription is ever added, so a post-join IC pose is never delivered to the
// joiner's live Subscribe stream.
//
// REQUIRES WithInTreePlugins (the coordinator is injected into the loaded
// plugin hosts via Manager.ConfigureFocusDeps). Gated exactly like
// WithPluginCrypto so non-focus suites keep the current WithSubscriber-only
// wiring — zero blast radius (holomush-y5inx.9).
func WithFocusDelivery() StartOption {
	return func(c *startConfig) { c.withFocusDelivery = true }
}

// Start bootstraps a full in-process holomush stack and returns a Server.
// The caller MUST call Stop() (typically via defer) to release resources.
//
// The stack consists of:
//   - A shared Postgres testcontainer with migrations applied (per-test DB)
//   - An embedded NATS JetStream server (in-memory, per-test isolation)
//   - An in-process CoreServer wired to the above
//
// AllowAll ABAC engine is the default — privacy tests focus on session/
// history gates, not role enforcement. Pass WithPolicyEngine to override
// for tests that need denial-path coverage.
func Start(t *testing.T, opts ...StartOption) *Server {
	t.Helper()

	ctx := context.Background()

	// Postgres: shared container, fresh per-test database.
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)

	evStore, err := store.NewPostgresEventStore(ctx, connStr)
	require.NoError(t, err, "integrationtest.Start: open event store")
	t.Cleanup(evStore.Close)

	pool := evStore.Pool()

	// Stores and repos.
	playerSessionStore := store.NewPostgresPlayerSessionStore(pool)
	playerRepo := authpg.NewPlayerRepository(pool)
	hasher := auth.NewArgon2idHasher()

	authService, err := auth.NewAuthService(playerRepo, playerSessionStore, hasher)
	require.NoError(t, err, "integrationtest.Start: create auth service")

	worldCharRepo := worldpg.NewCharacterRepository(pool)
	charRepo := &authCharRepoAdapter{pool: pool, charRepo: worldCharRepo}
	sessionStoreInst := store.NewPostgresSessionStore(pool)
	locRepo := worldpg.NewLocationRepository(pool)

	// Guest start location: create a persistent location for guests.
	guestLocID := idgen.New()
	guestLoc := &world.Location{
		ID:           guestLocID,
		Name:         "Crossroads",
		Description:  "A well-travelled intersection.",
		Type:         world.LocationTypePersistent,
		ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
	}
	err = locRepo.Create(ctx, guestLoc)
	require.NoError(t, err, "integrationtest.Start: create guest start location")

	// GuestService wiring.
	guestNamer := naming.NewGemstoneElementTheme()
	guestBindingRepo := worldpg.NewBindingRepository(pool)
	guestTransactor := worldpg.NewTransactor(pool)
	guestSvc, err := auth.NewGuestService(
		telnet.NewGuestAuthenticator(guestNamer, guestLocID),
		playerRepo, charRepo, playerSessionStore,
		guestTransactor, guestBindingRepo,
	)
	require.NoError(t, err, "integrationtest.Start: create guest service")

	// Embedded NATS bus (in-memory, cleaned up via t.Cleanup).
	bus := eventbustest.New(t)

	// Resolve options. Default ABAC engine is allowAll (privacy tests focus
	// on session/history gates, not role enforcement). Tests that need
	// denial-path coverage override via WithPolicyEngine.
	cfg := &startConfig{accessEngine: &allowAllPolicyEngine{}}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.withPluginCrypto && !cfg.withPlugins {
		panic("integrationtest: WithPluginCrypto() requires WithInTreePlugins()")
	}
	if cfg.withFocusDelivery && !cfg.withPlugins {
		panic("integrationtest: WithFocusDelivery() requires WithInTreePlugins()")
	}
	pe := cfg.accessEngine

	// Real seeded ABAC engine (opt-in). Overrides the allow-all default and is
	// retained for the plugin layer's resolver/pluginProvider threading below.
	var abacSub *abacsetup.ABACSubsystem
	if cfg.withRealABAC {
		abacSub = startRealABAC(t, ctx, pool)
		pe = abacSub.Engine()
	}

	// VerbRegistry must exist before plugins load (they register verbs). It is
	// also required by the locationFollower's synthetic location_state emit path
	// so RenderingMetadata is stamped on the EventFrame (gateway drops
	// nil-Rendering events per INV-GW-5, holomush-4wdu). Production wires this in
	// cmd/holomush/sub_grpc.go.
	verbRegistry, err := core.BootstrapVerbRegistry("test")
	require.NoError(t, err, "integrationtest.Start: BootstrapVerbRegistry")

	// Command dispatcher. When WithInTreePlugins is set, the dispatcher is fed
	// the plugin subsystem's command registry so plugin commands are
	// dispatchable (mirrors cmd/holomush/sub_grpc.go); otherwise it gets an
	// empty registry (no commands registered).
	// Plugin-crypto substrate (opt-in via WithPluginCrypto, gated to require
	// WithInTreePlugins above). Constructed BEFORE startPlugins so its
	// crypto-enabled publisher can be threaded into the plugin event emitter
	// via ConfigureEventEmitter (link 1: sensitive plugin emits encrypt on the
	// wire with persisted DEKs).
	var pc *pluginCrypto
	if cfg.withPluginCrypto {
		pc = newPluginCrypto(t, bus, pool, verbRegistry)
	}

	// Focus-delivery: the SessionStreamRegistry MUST exist BEFORE startPlugins so
	// it can be wired into the CoreServer (the Subscribe handler registers each
	// connection's control channel on it).
	// nil under non-focus suites — zero blast radius (holomush-y5inx.9).
	var streamRegistry *holoGRPC.SessionStreamRegistry
	if cfg.withFocusDelivery {
		streamRegistry = holoGRPC.NewSessionStreamRegistry()
	}

	var pluginSub *pluginsetup.PluginSubsystem
	cmdRegistry := command.NewRegistry()
	if cfg.withPlugins {
		res, pp, aud := pluginAttrSources(abacSub)
		// Under WithRealABAC, route plugin manifest-policy installs through the
		// engine's own cache-wired installer so they go live on the real engine
		// (mirrors INV-RA-4's resolver/provider routing). nil → startPlugins uses
		// a fresh standalone installer for the allow-all default.
		var policyInst *plugins.PolicyInstaller
		if abacSub != nil {
			policyInst = abacSub.PolicyInstaller()
		}
		pluginSub = startPlugins(t, ctx, pluginDeps{
			pool:                  pool,
			connStr:               connStr,
			engine:                pe,
			sessionStore:          sessionStoreInst,
			verbReg:               verbRegistry,
			playerRepo:            playerRepo,
			hasher:                hasher,
			playerSess:            playerSessionStore,
			resolver:              res,
			pluginProvider:        pp,
			auditor:               aud,
			policyInstaller:       policyInst,
			cryptoPublisher:       cryptoPublisherOf(pc),
			gameID:                bus.Bus.GameID(),
			pluginConfigOverrides: cfg.pluginConfigOverrides,
			extraPluginDirs:       cfg.extraPluginDirs,
		})
		cmdRegistry = pluginSub.CommandRegistry()
	}

	// When plugins are loaded, route plugin-backed commands through the
	// PluginManager deliverer (mirrors cmd/holomush/sub_grpc.go:310). Without
	// this, SendCommand of any plugin command (e.g. "scene …") is rejected with
	// NO_PLUGIN_DELIVERER, so command-driven plugin E2Es cannot run.
	var dispatcherOpts []command.DispatcherOption
	if pluginSub != nil {
		dispatcherOpts = append(dispatcherOpts, command.WithPluginDeliverer(pluginSub.Manager()))
	}
	dispatcher, err := command.NewDispatcher(cmdRegistry, pe, dispatcherOpts...)
	require.NoError(t, err, "integrationtest.Start: create command dispatcher")
	// Session service wired so plugin commands that succeed can bump session
	// activity (dispatchToPlugin → exec.Services().Session().UpdateActivity).
	// session.Store satisfies session.Access (mirrors cmd/holomush/sub_grpc.go:295);
	// without it, command-driven plugin E2Es panic on the nil Session getter.
	cmdServices := command.NewTestServices(command.ServicesConfig{Engine: pe, Session: sessionStoreInst})

	// Core engine with a no-op event appender.
	engine := core.NewEngine(&noopEventAppender{})

	// HistoryReader: minimal wiring against the embedded bus's JetStream
	// and the test Postgres pool. Without WithPluginCrypto, all crypto/audit/
	// fence options are nil-defaulted — the production newHistoryReader in
	// cmd/holomush/sub_grpc.go layers those on, but for privacy-invariant
	// tests the bare JS+Postgres tier is sufficient (zero blast radius).
	//
	// Under WithPluginCrypto, build the shared AuthGuard + DEK manager + audit
	// emitter + codec selector and thread them into the reader (holomush-y5inx.8)
	// so a SENSITIVE plugin-owned scene event read back via QueryStreamHistory
	// decrypts for an authorized DEK participant. buildHistoryCrypto runs here
	// (after startPlugins, before the reader) so configureReadback below reuses
	// the SAME guard instance — no divergent guards.
	var histCrypto *historyCrypto
	historyReaderOpts := []history.Option{}
	if cfg.withPluginCrypto {
		histCrypto = buildHistoryCrypto(t, pc, pluginSub.Manager(), pe, bus.Bus.GameID())
		historyReaderOpts = histCrypto.readerCryptoOptions(pc)
	}
	historyReader := history.NewReader(bus.JS, pool, 30*24*time.Hour, time.Now, historyReaderOpts...)

	// Focus-delivery coordinator (opt-in via WithFocusDelivery; the
	// SessionStreamRegistry was created above, before startPlugins). Mirrors
	// production cmd/holomush/sub_grpc.go:428-470: a real focus.Coordinator wired
	// with the scene KindPolicy, game settings, player-preference reader, and the
	// plugin StreamContributor plus the ConnectionSender (both wired from one
	// SessionStreamRegistry via FocusStreamCoordinatorOptions, mirroring prod).
	// The scene `join` command reaches JoinFocus → AutoFocusOnJoin; the
	// coordinator itself then drives per-Connection subscription deltas
	// (driveFocusDeltas, INV-FS-1) → the connection's control channel, adding the
	// scene IC/OOC streams to the live Subscribe filter set. The coordinator is
	// injected into the loaded plugin hosts via Manager.ConfigureFocusDeps below.
	// Gated so non-focus suites keep the WithSubscriber-only wiring — zero blast
	// radius (holomush-y5inx.9).
	var focusCoord focus.Coordinator
	if cfg.withFocusDelivery {
		gameSettings := settings.NewGameSettings(&settings.SystemInfoAdapter{
			Store:       evStore,
			NotFoundErr: store.ErrSystemInfoNotFound,
		})
		coordOpts := []focus.CoordinatorOption{
			focus.WithSessionStore(sessionStoreInst),
			focus.WithKindPolicy(scenepolicy.New()),
			focus.WithGameSettings(gameSettings),
			focus.WithPlayerPreferences(focus.NewPlayerPrefsAdapter(playerRepo)),
			focus.WithStreamContributor(&focusStreamContributorAdapter{pm: pluginSub.Manager()}),
			focus.WithGameID(bus.Bus.GameID()),
		}
		coordOpts = append(coordOpts, holoGRPC.FocusStreamCoordinatorOptions(streamRegistry)...)
		var focusErr error
		focusCoord, focusErr = focus.NewCoordinator(coordOpts...)
		require.NoError(t, focusErr, "integrationtest.Start: build focus coordinator")
	}

	// Subscriber: the embedded bus subscriber powers Subscribe → WaitForEvent /
	// DrainEvents. Under WithFocusDelivery + WithPluginCrypto, the live Subscribe
	// loop must decode SENSITIVE scene IC events (delivered after a `scene join`).
	// A bare identity-codec subscriber hits the zero-key AEAD decode path and
	// errors ("bad key length"), tearing down the transport. Threading the same
	// AuthGuard + DEK manager + codec selector + decrypt-audit emitter that the
	// history reader uses (buildHistoryCrypto) gives the live path Decision-5
	// semantics: a non-DEK-participant receives a metadata-only frame (Type still
	// stamped) rather than an error, and a participant receives plaintext. Gated
	// to crypto so non-crypto suites keep the bare subscriber — zero blast radius
	// (holomush-y5inx.9).
	var subscriber eventbus.Subscriber
	if cfg.withFocusDelivery && histCrypto != nil {
		subscriber = bus.Bus.Subscriber(
			eventbus.WithSubscriberCodecSelector(pc.selector),
			eventbus.WithSubscriberAuthGuard(histCrypto.sessionGuard),
			eventbus.WithSubscriberDEKManager(pc.dekMgr),
			eventbus.WithSubscriberDecryptAuditEmitter(histCrypto.sessionAuditEm),
		)
	} else {
		subscriber = bus.Bus.Subscriber()
	}

	// CoreServer wired with all required subsystems.
	coreServerOpts := []holoGRPC.CoreServerOption{
		holoGRPC.WithAuthService(authService),
		holoGRPC.WithPlayerSessionRepo(playerSessionStore),
		holoGRPC.WithPlayerRepo(playerRepo),
		holoGRPC.WithCharacterRepo(charRepo),
		holoGRPC.WithCharacterNameResolver(holoGRPC.NewRepoCharacterNameResolver(worldCharRepo)),
		holoGRPC.WithSessionStore(sessionStoreInst),
		holoGRPC.WithGuestService(guestSvc),
		// Wire embedded bus subscriber so Subscribe calls succeed for
		// WaitForEvent / DrainEvents paths.
		holoGRPC.WithSubscriber(subscriber),
		// HistoryReader powers QueryStreamHistory end-to-end so privacy
		// integration tests can exercise the full RPC path.
		holoGRPC.WithHistoryReader(historyReader),
		// AccessEngine drives staffOverride() in QueryStreamHistory; with
		// it unwired, every override check returns false (the nil-engine
		// short-circuit), defeating I-PRIV-6 tests. The harness uses
		// allowAllPolicyEngine so override semantics are exercised
		// without the operational complexity of seeded ABAC policies.
		holoGRPC.WithAccessEngine(pe),
		holoGRPC.WithVerbRegistry(verbRegistry),
	}
	// Under WithPluginCrypto, enable the Phase 3b crypto identity path so
	// QueryStreamHistory builds a typed CHARACTER SessionIdentity (binding_id
	// resolved via the BindingRepo) and hands it to the hot-tier AuthGuard.
	// Without these the identity is the zero value and the guard cannot match a
	// DEK participant. Gated to crypto so non-crypto suites keep the current
	// (binding-lookup-skipped) behavior — zero blast radius (holomush-y5inx.8).
	if cfg.withPluginCrypto {
		coreServerOpts = append(
			coreServerOpts,
			holoGRPC.WithCryptoEnabled(true),
			holoGRPC.WithBindingRepository(worldpg.NewBindingRepository(pool)),
		)
	}
	// Under WithFocusDelivery, hand the CoreServer the stream registry and focus
	// coordinator. WithStreamRegistry makes Subscribe register each connection's
	// control channel (server.go:821/871); WithFocusCoordinator makes Subscribe
	// run RestoreFocus and lets AutoFocusOnJoin's filter updates reach the live
	// loop (holomush-y5inx.9).
	if cfg.withFocusDelivery {
		coreServerOpts = append(
			coreServerOpts,
			holoGRPC.WithStreamRegistry(streamRegistry),
			holoGRPC.WithFocusCoordinator(focusCoord),
		)
	}
	coreServer := holoGRPC.NewCoreServer(
		engine,
		sessionStoreInst,
		dispatcher,
		cmdServices,
		coreServerOpts...,
	)

	// Inject the focus coordinator + history reader into the loaded plugin hosts
	// (late-binding: plugins started before this wiring existed). Without this,
	// the plugin host-service JoinFocus RPC short-circuits with "focus
	// coordinator not configured" (host_service.go:113) and the real `scene join`
	// command never registers a scene-stream subscription (holomush-y5inx.9).
	if cfg.withFocusDelivery {
		pluginSub.Manager().ConfigureFocusDeps(focusCoord, &focusHistoryReaderAdapter{
			reader: historyReader,
			gameID: bus.Bus.GameID,
		})
	}

	srv := &Server{
		t:                    t,
		pool:                 pool,
		playerSessionStore:   playerSessionStore,
		playerRepo:           playerRepo,
		charRepo:             charRepo,
		sessionStore:         sessionStoreInst,
		locRepo:              locRepo,
		authService:          authService,
		guestSvc:             guestSvc,
		bus:                  bus,
		coreServer:           coreServer,
		pluginSub:            pluginSub,
		pluginCrypto:         pc,
		histCrypto:           histCrypto,
		accessEngine:         pe,
		guestStartLocationID: guestLocID,
	}

	// Plugin-crypto links 3+4 (Task 8): the audit projection (PluginConsumerManager
	// forwarding plugin-owned subjects to scene_log) and the read-back decryptor
	// (host-side DEK decrypt + INV-19 audit). Wired after startPlugins so the
	// Manager's audit clients are resolvable. INV-P7-9: the SAME pc.selector
	// instance feeds the consumer manager that the crypto-enabled publisher used
	// on the emit side. The read-back decryptor reuses the guard + audit emitter
	// built by buildHistoryCrypto above (also used by the host history reader).
	if cfg.withPluginCrypto {
		srv.readbackAuditEm = histCrypto.auditEm
		srv.seedScene(ctx, pc)
		srv.pluginConsumers = startPluginConsumers(t, ctx, bus, pluginSub.Manager(), pc.selector)
		t.Cleanup(func() { _ = srv.pluginConsumers.Stop(context.Background()) })
		srv.configureReadback(pc)
		t.Cleanup(func() { _ = srv.readbackAuditEm.Shutdown(context.Background()) })
	}

	return srv
}

// cryptoPublisherOf returns pc's crypto-enabled publisher, or nil when pc is
// nil (no WithPluginCrypto). A nil cryptoPublisher leaves the plugin event
// emitter unwired, preserving the WithInTreePlugins-only behavior the
// whole-system census suite relies on.
func cryptoPublisherOf(pc *pluginCrypto) eventbus.Publisher {
	if pc == nil {
		return nil
	}
	return pc.publisher
}

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

// CommandQuerier returns the shared, ABAC-filtered command querier built by the
// production PluginSubsystem.Start() path (subsystem.go) and late-bound into the
// Lua host via SetCommandQuerier. Panics if WithInTreePlugins was not passed.
// Used by the whole-system wiring regression to prove Start() yields a non-nil
// querier (design spec INV-1: single command-visibility filter).
func (s *Server) CommandQuerier() *commandquery.Querier {
	s.requirePlugins("CommandQuerier")
	return s.pluginSub.CommandQuerier()
}

// ServiceRegistry returns the plugin service registry. Panics if
// WithInTreePlugins was not passed.
func (s *Server) ServiceRegistry() *plugins.ServiceRegistry {
	s.requirePlugins("ServiceRegistry")
	return s.pluginSub.ServiceRegistry()
}

// SceneServiceClient returns a SceneService client backed by the loaded
// core-scenes plugin, resolved from the existing plugin ServiceRegistry.
// Test-only; requires WithInTreePlugins (panics otherwise via requirePlugins).
func (s *Server) SceneServiceClient() scenev1.SceneServiceClient {
	s.requirePlugins("SceneServiceClient")
	svc, err := s.ServiceRegistry().Resolve("holomush.scene.v1.SceneService")
	require.NoError(s.t, err, "integrationtest.Server.SceneServiceClient: resolve SceneService")
	require.NotNil(s.t, svc.Conn, "integrationtest.Server.SceneServiceClient: nil conn")
	return scenev1.NewSceneServiceClient(svc.Conn)
}

// AccessEngine returns the ABAC policy engine the stack evaluates against.
// Under WithRealABAC it is the real seeded engine (abacSub.Engine()); composed
// with WithInTreePlugins, plugin-declared attribute resolvers are registered on
// that engine's resolver, so callers can evaluate plugin-installed manifest
// policies against it directly (holomush-0f0f4.9, INV-WS-2). Without
// WithRealABAC it is the allow-all default (or a WithPolicyEngine override).
func (s *Server) AccessEngine() types.AccessPolicyEngine {
	return s.accessEngine
}

func (s *Server) requirePlugins(method string) {
	if s.pluginSub == nil {
		panic("integrationtest: " + method + "() requires Start(t, WithInTreePlugins())")
	}
}

// NewLocation creates a fresh persistent location in the world and returns
// its ULID. Bypasses ABAC (direct repo write for test convenience).
func (s *Server) NewLocation(ctx context.Context) ulid.ULID {
	s.t.Helper()
	locID := idgen.New()
	loc := &world.Location{
		ID:           locID,
		Name:         "TestLoc_" + locID.String()[:8],
		Description:  "A test location.",
		Type:         world.LocationTypePersistent,
		ReplayPolicy: world.DefaultReplayPolicy(world.LocationTypePersistent),
	}
	err := s.locRepo.Create(ctx, loc)
	require.NoError(s.t, err, "integrationtest.Server.NewLocation: create location")
	return loc.ID
}

// NewSceneWithoutMember creates a scene with no members and returns its ULID.
//
// Scenes are referenced by ULID alone for I-17 / scope-floor purposes — the
// session's FocusMemberships JSONB carries the per-session membership state,
// so no backing row is required to make a scene "exist" from the test's
// perspective. Production scenes are created by the core-scenes plugin via
// CreateScene RPC; that path is out of scope for privacy-floor tests, which
// only need a well-formed scene ULID to construct dot-style subjects and
// FocusMembership entries.
//
// Callers add a session as a scene member via Session.JoinScene.
func (s *Server) NewSceneWithoutMember(_ context.Context) ulid.ULID {
	s.t.Helper()
	return idgen.New()
}

// GameID returns the embedded NATS JetStream game identifier, used by tests
// that need to construct dot-style stream subjects of the form
// `events.<gameID>.scene.<sceneID>.{ic,ooc}` (per INV-P4-1 / ADR holomush-s9nu).
func (s *Server) GameID() string {
	return s.bus.Bus.GameID()
}

// DeleteSession directly deletes a session row from Postgres. Used by
// iwzt.11 wire-opacity tests to exercise the missing-session denial
// branch of I-PRIV-5: a client holding a session_id that no longer
// resolves in sessionStore.Get MUST receive STREAM_ACCESS_DENIED on the
// wire (denial_reason=session_not_found is slog-only).
//
// FK side-effect: cascades to session_connections (ON DELETE CASCADE
// per migration 000001_baseline.up.sql). Any future FK added to
// sessions without ON DELETE CASCADE would need explicit pre-cleanup.
func (s *Server) DeleteSession(ctx context.Context, sessionID string) {
	s.t.Helper()
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
	require.NoError(s.t, err, "integrationtest.Server.DeleteSession")
	require.Equalf(s.t, int64(1), tag.RowsAffected(),
		"integrationtest.Server.DeleteSession: expected 1 row affected, got %d (sessionID=%s)",
		tag.RowsAffected(), sessionID)
}

// ExpireSession directly marks a session row as expired in Postgres.
// Used by iwzt tests to force session-expiry scenarios.
func (s *Server) ExpireSession(ctx context.Context, sessionID string) {
	s.t.Helper()
	now := time.Now().UTC()
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET status = $1, expires_at = $2, updated_at = $2 WHERE id = $3`,
		string(session.StatusExpired), now.UnixNano(), sessionID)
	require.NoError(s.t, err, "integrationtest.Server.ExpireSession")
	require.Equalf(s.t, int64(1), tag.RowsAffected(),
		"integrationtest.Server.ExpireSession: expected 1 row affected, got %d (sessionID=%s)", tag.RowsAffected(), sessionID)
}

// SetLocationArrivedAt directly mutates a session's location_arrived_at column
// in Postgres. Used by 5b2j tests to exercise floor-bypass semantics
// (I-PRES-2): the snapshot RPC reads sessionStore directly and is exempt from
// the I-PRIV-1 temporal floor, so manipulating this column should NOT affect
// ListFocusPresence's behavior.
func (s *Server) SetLocationArrivedAt(ctx context.Context, sessionID string, t time.Time) {
	s.t.Helper()
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET location_arrived_at = $1, updated_at = $1 WHERE id = $2`,
		t.UTC().UnixNano(), sessionID)
	require.NoError(s.t, err, "integrationtest.Server.SetLocationArrivedAt")
	require.Equalf(s.t, int64(1), tag.RowsAffected(),
		"integrationtest.Server.SetLocationArrivedAt: expected 1 row affected, got %d (sessionID=%s)", tag.RowsAffected(), sessionID)
}

// DeleteCharacter removes a character row + its FK-dependent rows from
// Postgres in dependency-safe order. Used by iwzt.21 (I-PRIV-2 guest
// name-reuse) to simulate guest-character cleanup that production logout
// does NOT currently perform — without this, the unique-name constraint on
// `characters.LOWER(name)` blocks any subsequent guest from drawing the
// same display name, defeating the name-reuse scenario.
//
// Production guest service relies on ExistsByName to retry-on-collision;
// this helper is test-only and MUST NOT be invoked from production paths.
func (s *Server) DeleteCharacter(ctx context.Context, charID ulid.ULID) {
	s.t.Helper()
	charIDStr := charID.String()

	// FK-safe order: dependent rows first (sessions, bindings, roles, owned
	// objects), then the character row. sessions for this character must be
	// gone before the character can be deleted; the test contract is that
	// Logout has already removed them, but DELETE is idempotent so we cover
	// that case too. objects.owner_id REFERENCES characters(id) defaults to
	// ON DELETE RESTRICT (per migrations/000001_baseline.up.sql), so any
	// character-owned objects would block the character DELETE without an
	// explicit pre-clean.
	for _, child := range []struct{ table, col string }{
		{"sessions", "character_id"},
		{"player_character_bindings", "character_id"},
		{"character_roles", "character_id"},
		{"objects", "owner_id"},
	} {
		_, err := s.pool.Exec(ctx, "DELETE FROM "+child.table+" WHERE "+child.col+" = $1", charIDStr)
		require.NoError(s.t, err, "integrationtest.Server.DeleteCharacter: clean %s", child.table)
	}

	tag, err := s.pool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charIDStr)
	require.NoError(s.t, err, "integrationtest.Server.DeleteCharacter: delete characters")
	require.Equalf(s.t, int64(1), tag.RowsAffected(),
		"integrationtest.Server.DeleteCharacter: expected 1 row affected, got %d (charID=%s)",
		tag.RowsAffected(), charIDStr)
}

// ConnectGuest creates a guest player+character and opens a game session.
// The returned Session is ready for SendCommand / DrainEvents / Logout calls.
func (s *Server) ConnectGuest(ctx context.Context) *Session {
	s.t.Helper()

	resp, err := s.coreServer.CreateGuest(ctx, &corev1.CreateGuestRequest{})
	require.NoError(s.t, err, "integrationtest.ConnectGuest: CreateGuest RPC")
	require.True(s.t, resp.GetSuccess(), "integrationtest.ConnectGuest: CreateGuest failed: %s", resp.GetErrorMessage())

	rawToken := resp.GetPlayerSessionToken()
	charID, parseErr := ulid.Parse(resp.GetDefaultCharacterId())
	require.NoError(s.t, parseErr, "integrationtest.ConnectGuest: parse character ID")

	selResp, err := s.coreServer.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: rawToken,
		CharacterId:        charID.String(),
	})
	require.NoError(s.t, err, "integrationtest.ConnectGuest: SelectCharacter RPC")
	require.True(s.t, selResp.GetSuccess(),
		"integrationtest.ConnectGuest: SelectCharacter failed: %s", selResp.GetErrorMessage())

	// Hydrate session timestamps from the persisted row, NOT from time.Now() —
	// see the parallel block in ConnectAuthedWithRoles for the rationale.
	persisted, getErr := s.sessionStore.Get(ctx, selResp.GetSessionId())
	require.NoError(s.t, getErr, "integrationtest.ConnectGuest: read persisted session")

	sess := &Session{
		server:             s,
		SessionID:          selResp.GetSessionId(),
		PlayerID:           persisted.PlayerID, // populated from persisted row so guest-reaper tests can backdate the player
		CharacterID:        charID,
		CharacterName:      selResp.GetCharacterName(),
		LocationID:         s.guestStartLocationID,
		OriginalLocationID: s.guestStartLocationID,
		LocationArrivedAt:  persisted.LocationArrivedAt,
		SessionCreatedAt:   persisted.CreatedAt,
		playerSessionToken: rawToken,
	}
	sess.attach(ctx)
	return sess
}

// ConnectAuthed creates a named player+character and opens a game session.
// The character is placed at the server's guest start location.
func (s *Server) ConnectAuthed(ctx context.Context, charName string) *Session {
	return s.ConnectAuthedWithRoles(ctx, charName, nil)
}

// ConnectAuthedWithRoles creates a named player+character with the given
// roles and opens a game session. If roles is non-nil, each role is inserted
// into character_roles directly via Postgres (bypassing ABAC for harness
// convenience).
func (s *Server) ConnectAuthedWithRoles(ctx context.Context, charName string, roles []string) *Session {
	s.t.Helper()

	// Unique username per character name to avoid collisions across tests.
	username := charName + "_" + idgen.New().String()[:8]
	password := "TestPassword1!"

	// Register the player account.
	player, playerSession, rawToken, err := s.authService.CreatePlayer(ctx, username, password, "")
	require.NoError(s.t, err, "integrationtest.ConnectAuthedWithRoles: CreatePlayer")

	// Persist the player session so SelectCharacter can resolve the token.
	require.NoError(s.t, s.playerSessionStore.Create(ctx, playerSession),
		"integrationtest.ConnectAuthedWithRoles: persist player session")

	// Create the character directly (bypasses characterService wiring).
	startLocID := s.guestStartLocationID
	char, err := world.NewCharacter(player.ID, charName)
	require.NoError(s.t, err, "integrationtest.ConnectAuthedWithRoles: NewCharacter")
	char.LocationID = &startLocID
	// authCharRepoAdapter.Create delegates to worldpg.CharacterRepository.Create.
	require.NoError(s.t, s.charRepo.Create(ctx, char),
		"integrationtest.ConnectAuthedWithRoles: persist character")

	// Under WithPluginCrypto the CoreServer runs with WithCryptoEnabled(true), so
	// Subscribe / QueryStreamHistory perform a binding lookup
	// (BindingRepository.Current) to build the typed CHARACTER identity. Create
	// the binding row here — production characters always have one; the harness's
	// direct charRepo.Create bypasses that path (holomush-y5inx.8).
	if s.pluginCrypto != nil {
		_, bindErr := worldpg.NewBindingRepository(s.pool).Create(ctx,
			player.ID.String(), char.ID.String(), "integrationtest.ConnectAuthedWithRoles")
		require.NoError(s.t, bindErr, "integrationtest.ConnectAuthedWithRoles: create binding")
	}

	// Stamp roles into character_roles.
	for _, role := range roles {
		_, roleErr := s.pool.Exec(ctx,
			`INSERT INTO character_roles (character_id, role) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			char.ID.String(), role)
		require.NoError(s.t, roleErr, "integrationtest.ConnectAuthedWithRoles: insert role %q", role)
	}

	// Open a game session by selecting the character.
	selResp, err := s.coreServer.SelectCharacter(ctx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: rawToken,
		CharacterId:        char.ID.String(),
	})
	require.NoError(s.t, err, "integrationtest.ConnectAuthedWithRoles: SelectCharacter RPC")
	require.True(s.t, selResp.GetSuccess(),
		"integrationtest.ConnectAuthedWithRoles: SelectCharacter failed: %s", selResp.GetErrorMessage())

	// Hydrate session timestamps from the persisted session row, NOT from
	// time.Now() — the server-side LocationArrivedAt drives the I-PRIV-1 /
	// I-PRIV-6 floor in QueryStreamHistory, so tests that assert against
	// it MUST see the canonical value (per CodeRabbit thread on PR #4048).
	persisted, getErr := s.sessionStore.Get(ctx, selResp.GetSessionId())
	require.NoError(s.t, getErr, "integrationtest.ConnectAuthedWithRoles: read persisted session")

	sess := &Session{
		server:             s,
		SessionID:          selResp.GetSessionId(),
		PlayerID:           player.ID,
		CharacterID:        char.ID,
		CharacterName:      selResp.GetCharacterName(),
		LocationID:         s.guestStartLocationID,
		OriginalLocationID: s.guestStartLocationID,
		LocationArrivedAt:  persisted.LocationArrivedAt,
		SessionCreatedAt:   persisted.CreatedAt,
		playerSessionToken: rawToken,
	}
	sess.attach(ctx)
	return sess
}

// AuthedPlayer creates a named player + character + persisted player session
// and returns a handle for opening game sessions independently of the
// player/character bootstrap. Unlike ConnectAuthed (which combines player
// creation with a single SelectCharacter call), AuthedPlayer defers
// SelectCharacter to OpenWebSession so tests can exercise
// detach/reattach scenarios where a second OpenWebSession call reattaches
// to an existing session row (per spec §5 row 2 + I-PRIV-3).
//
// The returned handle carries the player_session bearer token for use
// across subsequent OpenWebSession calls.
func (s *Server) AuthedPlayer(ctx context.Context, charName string) *AuthedPlayer {
	s.t.Helper()

	// Unique username per character name to avoid collisions across tests.
	username := charName + "_" + idgen.New().String()[:8]
	password := "TestPassword1!"

	player, playerSession, rawToken, err := s.authService.CreatePlayer(ctx, username, password, "")
	require.NoError(s.t, err, "integrationtest.Server.AuthedPlayer: CreatePlayer")
	require.NoError(s.t, s.playerSessionStore.Create(ctx, playerSession),
		"integrationtest.Server.AuthedPlayer: persist player session")

	startLocID := s.guestStartLocationID
	char, err := world.NewCharacter(player.ID, charName)
	require.NoError(s.t, err, "integrationtest.Server.AuthedPlayer: NewCharacter")
	char.LocationID = &startLocID
	require.NoError(s.t, s.charRepo.Create(ctx, char),
		"integrationtest.Server.AuthedPlayer: persist character")

	return &AuthedPlayer{
		PlayerID:    player.ID,
		CharacterID: char.ID,
		LocationID:  startLocID,
		server:      s,
		rawToken:    rawToken,
	}
}

// DetachSession transitions a session row to StatusDetached with the same
// (detached_at, expires_at) writes that production Disconnect performs at
// internal/grpc/server.go:1376-1389. Mirrors a non-guest transport drop:
// the session row is held open for the TTL window so a later reattach (via
// SelectCharacter or Subscribe.ReattachCAS) can resume the same session.
//
// Used by iwzt.17 (I-PRIV-3 / transport-continuity) to simulate the
// transport-drop side of detach/reattach without tearing down a live
// Subscribe stream (iwzt.16's separate concern). LocationArrivedAt is
// NOT touched here — verifying the floor's preservation across this
// transition is the test's central assertion.
//
// Bypasses the production session-ownership guard
// (auth.ValidateSessionOwnership at internal/grpc/server.go:1253-1259) that
// Disconnect performs before reaching this UpdateStatus call. The guard is
// IDOR-class (token vs. session matching), not ABAC, and is out of scope
// for the privacy-floor tests this helper supports. Production callers
// MUST go through Disconnect, never this helper.
func (s *Server) DetachSession(ctx context.Context, sessionID string) {
	s.t.Helper()
	info, err := s.sessionStore.Get(ctx, sessionID)
	require.NoError(s.t, err, "integrationtest.Server.DetachSession: read session")

	now := time.Now().UTC()
	ttlSeconds := info.TTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = 1800
	}
	expiresAt := now.Add(time.Duration(ttlSeconds) * time.Second)
	require.NoError(s.t,
		s.sessionStore.UpdateStatus(ctx, sessionID, session.StatusDetached, &now, &expiresAt),
		"integrationtest.Server.DetachSession: update status to detached")
}

// ReattachSession transitions a Detached session row back to Active via
// the production ReattachCAS path (internal/store/session_store.go:421-429
// — the same path internal/grpc/server.go:778 takes when Subscribe arrives
// on a Detached session). Asserts the CAS succeeded (returns true) so a
// silent loss-of-race fails the test rather than producing a misleading
// QueryStreamHistory result against a stale status.
//
// LocationArrivedAt is preserved by ReattachCAS (the UPDATE writes only
// status/detached_at/expires_at/updated_at) — this is what I-PRIV-3 codifies
// and what iwzt.17 verifies end-to-end.
//
// Bypasses the production session-ownership guard
// (auth.ValidateSessionOwnership at internal/grpc/server.go:718-731) that
// Subscribe performs before reaching this ReattachCAS call. Same rationale
// as DetachSession above — IDOR-class, not ABAC. Production callers MUST
// go through Subscribe, never this helper.
func (s *Server) ReattachSession(ctx context.Context, sessionID string) {
	s.t.Helper()
	ok, err := s.sessionStore.ReattachCAS(ctx, sessionID)
	require.NoError(s.t, err, "integrationtest.Server.ReattachSession: ReattachCAS")
	require.Truef(s.t, ok,
		"integrationtest.Server.ReattachSession: CAS lost — session %s was not in Detached status", sessionID)
}

// Pool returns the shared Postgres connection pool. Exposed for tests that
// construct store instances (e.g. authpg.PlayerRepository) to drive
// reaper-level scenarios end-to-end (holomush-rsoe6, Task 13).
func (s *Server) Pool() *pgxpool.Pool {
	return s.pool
}

// SessionStore returns the session.Store backed by the shared Postgres pool.
// Exposed for reaper tests that need to drive the session reaper against the
// same store the harness uses (holomush-rsoe6, Task 13).
func (s *Server) SessionStore() session.Store {
	return s.sessionStore
}

// BackdateGuestPlayer sets a guest player's updated_at to the given time.
// Used by lease-reaper tests to make the player appear idle to
// ListIdleGuests (predicate: updated_at < idleSince). Direct SQL; test-only.
func (s *Server) BackdateGuestPlayer(ctx context.Context, playerID ulid.ULID, backdateTo time.Time) {
	s.t.Helper()
	tag, err := s.pool.Exec(ctx,
		`UPDATE players SET updated_at = $1 WHERE id = $2 AND is_guest = true`,
		backdateTo.UTC().UnixNano(), playerID.String())
	require.NoError(s.t, err, "integrationtest.Server.BackdateGuestPlayer")
	require.Equalf(s.t, int64(1), tag.RowsAffected(),
		"integrationtest.Server.BackdateGuestPlayer: expected 1 row affected, got %d (playerID=%s)",
		tag.RowsAffected(), playerID.String())
}

// --- internal helpers ---

// noopEventAppender satisfies core.EventAppender for tests that don't
// exercise event storage. Mirrors the pattern in test/integration/auth/.
type noopEventAppender struct{}

func (*noopEventAppender) Append(_ context.Context, _ core.Event) error { return nil }

var _ core.EventAppender = (*noopEventAppender)(nil)

// authCharRepoAdapter wraps *worldpg.CharacterRepository to satisfy
// auth.CharacterRepository. Mirrors test/integration/auth/auth_suite_test.go.
type authCharRepoAdapter struct {
	pool     *pgxpool.Pool
	charRepo *worldpg.CharacterRepository
}

func (a *authCharRepoAdapter) Create(ctx context.Context, char *world.Character) error {
	return a.charRepo.Create(ctx, char)
}

func (a *authCharRepoAdapter) ExistsByName(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := a.pool.QueryRow(
		ctx,
		"SELECT EXISTS(SELECT 1 FROM characters WHERE LOWER(name) = LOWER($1))", name,
	).Scan(&exists)
	if err != nil {
		return false, oops.Code("CHARACTER_EXISTS_CHECK_FAILED").With("name", name).Wrap(err)
	}
	return exists, nil
}

func (a *authCharRepoAdapter) CountByPlayer(ctx context.Context, playerID ulid.ULID) (int, error) {
	var count int
	err := a.pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM characters WHERE player_id = $1", playerID.String(),
	).Scan(&count)
	if err != nil {
		return 0, oops.Code("CHARACTER_COUNT_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	return count, nil
}

func (a *authCharRepoAdapter) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error) {
	rows, err := a.pool.Query(
		ctx,
		`SELECT id, player_id, name, description, location_id, created_at
		 FROM characters WHERE player_id = $1 ORDER BY name`, playerID.String(),
	)
	if err != nil {
		return nil, oops.Code("CHARACTER_LIST_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	defer rows.Close()

	var chars []*world.Character
	for rows.Next() {
		var c world.Character
		var idStr, pidStr string
		var locStr *string
		var createdAt pgnanos.Time
		if scanErr := rows.Scan(&idStr, &pidStr, &c.Name, &c.Description, &locStr, &createdAt); scanErr != nil {
			return nil, oops.Code("CHARACTER_SCAN_FAILED").Wrap(scanErr)
		}
		c.CreatedAt = createdAt.Time()
		var parseErr error
		c.ID, parseErr = ulid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.Code("CHARACTER_ULID_DECODE_FAILED").With("field", "id").Wrap(parseErr)
		}
		c.PlayerID, parseErr = ulid.Parse(pidStr)
		if parseErr != nil {
			return nil, oops.Code("CHARACTER_ULID_DECODE_FAILED").With("field", "player_id").Wrap(parseErr)
		}
		if locStr != nil {
			lid, locParseErr := ulid.Parse(*locStr)
			if locParseErr != nil {
				return nil, oops.Code("CHARACTER_ULID_DECODE_FAILED").With("field", "location_id").Wrap(locParseErr)
			}
			c.LocationID = &lid
		}
		chars = append(chars, &c)
	}
	if rows.Err() != nil {
		return nil, oops.Code("CHARACTER_ROWS_FAILED").Wrap(rows.Err())
	}
	return chars, nil
}

var _ auth.CharacterRepository = (*authCharRepoAdapter)(nil)

// allowAllPolicyEngine is a minimal AccessPolicyEngine that grants every
// request. Used in the privacy-test harness so tests focus on session/history
// privacy gates rather than ABAC policy enforcement.
type allowAllPolicyEngine struct{}

func (*allowAllPolicyEngine) Evaluate(_ context.Context, _ types.AccessRequest) (types.Decision, error) {
	return types.NewDecision(types.EffectAllow, "harness-allow-all", ""), nil
}

func (*allowAllPolicyEngine) CanPerformAction(_ context.Context, _, _, _, _ string) (bool, error) {
	return true, nil
}

var _ types.AccessPolicyEngine = (*allowAllPolicyEngine)(nil)

// focusStreamContributorAdapter bridges plugins.Manager.QuerySessionStreams to
// focus.StreamContributor by converting the request type. Mirrors the
// production adapter at cmd/holomush/sub_grpc.go:770. Wired only under
// WithFocusDelivery so RestoreFocus can include ambient plugin streams.
type focusStreamContributorAdapter struct {
	pm *plugins.Manager
}

// QuerySessionStreams implements focus.StreamContributor.
func (a *focusStreamContributorAdapter) QuerySessionStreams(ctx context.Context, req focus.StreamContributorRequest) []string {
	return a.pm.QuerySessionStreams(ctx, plugins.SessionStreamsRequest{
		CharacterID: req.CharacterID,
		PlayerID:    req.PlayerID,
		SessionID:   req.SessionID,
	})
}

var _ focus.StreamContributor = (*focusStreamContributorAdapter)(nil)

// focusHistoryReaderAdapter bridges eventbus.HistoryReader (QueryHistory) to
// plugins.HistoryReader (ReplayTail), so the focus coordinator's
// QueryStreamHistory replay path resolves under WithFocusDelivery. Mirrors the
// production busHistoryReaderAdapter at cmd/holomush/sub_grpc.go:670.
type focusHistoryReaderAdapter struct {
	reader eventbus.HistoryReader
	gameID func() string
}

var _ plugins.HistoryReader = (*focusHistoryReaderAdapter)(nil)

// ReplayTail satisfies plugins.HistoryReader. Fetches up to count most-recent
// events on stream (optionally filtered by notBefore and exclusive beforeID),
// returning them in ascending ULID order (oldest→newest).
func (a *focusHistoryReaderAdapter) ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]core.Event, error) {
	if count <= 0 {
		return nil, nil
	}
	gameID := a.gameID()
	if gameID == "" {
		gameID = "main"
	}
	sub, err := eventbus.Qualify(gameID, stream)
	if err != nil {
		return nil, oops.With("stream", stream).Wrap(err)
	}
	q := eventbus.HistoryQuery{
		Subject:   sub,
		Direction: eventbus.DirectionBackward,
		PageSize:  count,
		NotBefore: notBefore,
	}
	if !beforeID.IsZero() {
		q.BeforeID = beforeID
	}
	hs, err := a.reader.QueryHistory(ctx, q)
	if err != nil {
		return nil, oops.With("stream", stream).Wrap(err)
	}
	defer hs.Close() //nolint:errcheck // best-effort iterator close

	collected := make([]eventbus.Event, 0, count)
	for {
		e, nextErr := hs.Next(ctx)
		if nextErr != nil {
			if errors.Is(nextErr, io.EOF) {
				break
			}
			return nil, oops.With("stream", stream).Wrap(nextErr)
		}
		collected = append(collected, e)
		if len(collected) >= count {
			break
		}
	}
	// Backward direction yields newest-first; reverse to ascending order
	// (oldest→newest) and translate eventbus.Event → core.Event.
	result := make([]core.Event, len(collected))
	for i := range collected {
		j := len(collected) - 1 - i
		streamName := string(collected[i].Subject)
		result[j] = busEventToCoreEvent(collected[i], streamName)
	}
	return result, nil
}

// busEventToCoreEvent translates an eventbus.Event to a core.Event for plugin
// consumption. Mirrors cmd/holomush/sub_grpc.go:739.
func busEventToCoreEvent(e eventbus.Event, stream string) core.Event {
	actorID := ""
	if e.Actor.ID != (ulid.ULID{}) {
		actorID = e.Actor.ID.String()
	}
	return core.Event{
		ID:        e.ID,
		Stream:    stream,
		Type:      core.EventType(e.Type),
		Timestamp: e.Timestamp,
		Actor: core.Actor{
			Kind: busActorKindToCore(e.Actor.Kind),
			ID:   actorID,
		},
		Payload: e.Payload,
	}
}

func busActorKindToCore(k eventbus.ActorKind) core.ActorKind {
	switch k {
	case eventbus.ActorKindCharacter:
		return core.ActorCharacter
	case eventbus.ActorKindSystem:
		return core.ActorSystem
	case eventbus.ActorKindPlugin:
		return core.ActorPlugin
	default:
		return core.ActorSystem
	}
}
