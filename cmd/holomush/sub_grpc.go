// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	cryptotls "crypto/tls"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"

	abacsetup "github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/auth"
	authpostgres "github.com/holomush/holomush/internal/auth/postgres"
	authsetup "github.com/holomush/holomush/internal/auth/setup"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/content"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/authguard"
	authguardaudit "github.com/holomush/holomush/internal/eventbus/authguard/audit"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/history"
	"github.com/holomush/holomush/internal/eventbus/history/source"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	holoFocus "github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/grpc/focus/scenepolicy"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/naming"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/cryptowiring"
	pluginsetup "github.com/holomush/holomush/internal/plugin/setup"
	"github.com/holomush/holomush/internal/presence"
	"github.com/holomush/holomush/internal/session"
	sessionsetup "github.com/holomush/holomush/internal/session/setup"
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/sysbroadcast"
	"github.com/holomush/holomush/internal/telnet"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	worldsetup "github.com/holomush/holomush/internal/world/setup"
	contentv1 "github.com/holomush/holomush/pkg/proto/holomush/content/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
	sceneaccessv1 "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
)

// grpcSubsystemConfig configures the gRPC subsystem.
type grpcSubsystemConfig struct {
	DB        *store.DatabaseSubsystem
	ABAC      *abacsetup.ABACSubsystem
	Auth      *authsetup.AuthSubsystem
	World     *worldsetup.WorldSubsystem
	Plugins   *pluginsetup.PluginSubsystem
	Sessions  *sessionsetup.SessionSubsystem
	Bootstrap *bootstrapsetup.BootstrapSubsystem
	// EventBus supplies the Publisher used by the shared plugin emitter.
	// Required post-F1 (cutover commit): plugin emits publish to JetStream,
	// not to the PostgreSQL events table.
	EventBus *eventbus.Subsystem

	GRPCAddr string
	// TLSConfig is a given value for callers that already hold a resolved
	// TLS config at construction (test literals). TLSProvider, resolved
	// once at the top of Start, is the production path — it wins when
	// non-nil (07-09 item 2/Task 1). Backed by tlscerts.TLSSubsystem's
	// panic-guarded TLSConfig() accessor.
	TLSConfig      *cryptotls.Config
	TLSProvider    func() *cryptotls.Config
	SessionTTL     time.Duration
	ReaperInterval time.Duration
	LeaseTTL       time.Duration
	BootGrace      time.Duration
	MaxHistory     int
	GameConfig     config.GameConfig
	StreamRegistry *holoGRPC.SessionStreamRegistry
	// VerbRegistry is the seeded verb registry for rendering enrichment.
	// Required by wrapPublisher (Task 19) which wraps the EventBus publisher.
	VerbRegistry *core.VerbRegistry

	// RekeyManager is the production dek.Manager for INV-CRYPTO-22 hot→cold-tier
	// FallbackResolver wiring (sub-epic E T44+). When non-nil, Start()
	// constructs a full AuthGuard + AuditEmitter pipeline and threads them
	// into newHistoryReader via WithHistoryAuthAndSourceResolver. When nil
	// (degraded deployment without KEK), the history reader falls back to
	// nil-auth passthrough (EVENTBUS_HISTORY_AUTH_GUARD_NIL).
	//
	// Populated by runCoreWithDeps from rekeyW.Manager after buildRekeyWiring.
	RekeyManager dek.Manager // carries full interface (satisfies SessionDEKManager)
	// AuthGuard overrides the default Guard constructed in Start(). When nil,
	// Start() constructs the default Guard from available deps if RekeyManager
	// is non-nil. Settable for test injection (nil is the prod default).
	AuthGuard eventbus.SessionAuthGuard
	// AuditEmitter overrides the default Emitter constructed in Start(). When
	// nil, Start() constructs the default Emitter from available deps if
	// RekeyManager is non-nil. Settable for test injection (nil is the prod
	// default).
	AuditEmitter eventbus.SessionAuditEmitter

	// KeySelector is the SHARED codec.KeySelector instance threaded into
	// both audit.PluginConsumerManager (via WithKeySelector) and
	// history.NewReader (via WithCodecSelector). Required by INV-CRYPTO-45 —
	// the test at test/integration/eventbus_e2e/dispatcher_selector_identity_test.go
	// asserts pointer-identity between the two. When nil, both paths
	// fall back to identity decoding.
	KeySelector codec.KeySelector

	// CryptoWiring is the memoized cryptoWiring builder (cryptowiring.go).
	// grpcSubsystem is the one consumer in package main, so it may hold
	// func() (*cryptoWiring, error) directly rather than a narrow
	// consumer-owned provider type (round 5, BLOCKER 4). Resolved at the
	// top of Start; its RekeyManager output feeds AuthGuard/AuditEmitter
	// construction, and — as a side effect of the FIRST resolution in the
	// running system — the invalidation.Coordinator is constructed and
	// started here too (07-09 item 4).
	CryptoWiring func() (*cryptoWiring, error)

	// CoordHolder is the late-bound holder the cryptoWiring builder
	// publishes the invalidation.Coordinator into. Stop uses it to drive
	// the Coordinator's shutdown (07-09 item 4 — replaces the ad-hoc
	// runCore-level defer with orchestrator-ordered Stop).
	CoordHolder *coordHolder
}

// grpcSubsystem is the terminal subsystem that wires the gRPC server.
// It depends on all other subsystems and creates the final serving stack.
type grpcSubsystem struct {
	cfg grpcSubsystemConfig

	grpcServer    *grpc.Server
	listener      net.Listener
	reaperCancel  context.CancelFunc
	guestReaper   *auth.GuestReaper
	sessionReaper *session.Reaper
}

// sceneMuteNotifyCacheTTL bounds how long a character's {globalNotifyEnabled,
// mutedSet} is memoized in the SceneMuteChecker before a refresh. Short enough
// that a mute/unmute or SetSceneNotifyPref takes effect within a window, long
// enough that the badge-downgrade branch makes at most one plugin round-trip per
// character per window (never a per-event RPC in the hot delivery loop).
const sceneMuteNotifyCacheTTL = 45 * time.Second

// sceneMuteLoaderTimeout bounds a single SceneMuteChecker refresh (dispatch +
// GetSceneNotifyPref + ListMutedScenes) so a hung core-scenes plugin cannot block
// badge delivery on the subscribe loop indefinitely. The checker fails OPEN on a
// returned error (delivers the content-free frame), but a HANG produces no error —
// only a deadline does. Well under the cache TTL.
const sceneMuteLoaderTimeout = 3 * time.Second

// newGRPCSubsystem returns a configured grpcSubsystem for the provided configuration.
// It does not allocate or start any runtime resources; Start must be called to initialize and run the subsystem.
func newGRPCSubsystem(cfg grpcSubsystemConfig) *grpcSubsystem {
	return &grpcSubsystem{cfg: cfg}
}

// wrapPublisher wraps the raw EventBus publisher with RenderingPublisher
// so all emit-site callers (pluginManager and CoreServer.emitCommandResponse)
// get rendering-metadata enrichment for free. Returns an error if the verb
// registry is not configured.
func (s *grpcSubsystem) wrapPublisher(raw eventbus.Publisher) (eventbus.Publisher, error) {
	if s.cfg.VerbRegistry == nil {
		return nil, oops.Code("GRPC_VERB_REGISTRY_MISSING").
			Errorf("gRPC subsystem requires VerbRegistry for emit-time rendering enrichment")
	}
	return eventbus.NewRenderingPublisher(raw, s.cfg.VerbRegistry), nil
}

// ID returns SubsystemGRPC.
func (s *grpcSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemGRPC }

// DependsOn returns the subsystems that must start before gRPC.
// Bootstrap transitively depends on ABAC, World, Plugins, Database.
// EventBus is required so the Publisher is ready when ConfigureEventEmitter
// runs (F1 cutover: plugin emits publish to JetStream).
//
// TLS is required because Start resolves TLSProvider (07-09 item 2). Cluster
// is required because the invalidation.Coordinator (built as a side effect
// of the first CryptoWiring resolution) needs Registry: clusterSub (07-09
// item 4). CryptoChainVerifier is the T-07-51 re-scope (07-09 item 8): gRPC
// binds its TCP listener only after the INV-CRYPTO-102 chain walk has run.
// Database/Auth/ABAC are THE RULE's cryptoWiring consumer superset — this
// subsystem holds a CryptoWiring provider, and whichever consumer resolves
// it first builds it, so every consumer must declare the full set.
// AuditProjection is required (07-10, T-07-50): without this edge gRPC could
// begin serving before the host audit projection is up, so events served in
// that window would not be durably audited (a repudiation gap).
func (s *grpcSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{
		lifecycle.SubsystemBootstrap,
		lifecycle.SubsystemSessions,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemEventBus,
		lifecycle.SubsystemTLS,
		lifecycle.SubsystemCluster,
		lifecycle.SubsystemCryptoChainVerifier,
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemABAC,
		lifecycle.SubsystemAuditProjection,
	}
}

// cryptoActiveFor reports whether sensitive-event crypto is active for this
// subsystem config: true iff a KEK (RekeyManager) is wired. This is the single
// activation gate (replaces the vestigial cryptoEnabled flag).
func cryptoActiveFor(cfg grpcSubsystemConfig) bool { return cfg.RekeyManager != nil }

// publisherOptionsFor returns the PublishOptions for the live publisher.
// When a DEK manager (RekeyManager) is configured, the publisher is DEK-aware
// so Sensitive=true events take the encrypt branch (publisher.go:208); when
// nil, the publisher stays plaintext-only and the subsystem still starts
// (degraded-but-safe for KEK-less deployments).
func publisherOptionsFor(cfg grpcSubsystemConfig) []eventbus.PublishOption {
	if cfg.RekeyManager == nil {
		return nil
	}
	return []eventbus.PublishOption{eventbus.WithDEKManager(cfg.RekeyManager)}
}

// subscriberOptionsFor returns the SubscribeOptions for the live subscriber.
// When the AuthGuard is present (KEK configured), the subscriber decodes and
// authorizes sensitive events (subscriber.go:505 decodeAndAuthorize),
// delivering plaintext to DEK participants and metadata-only to others. When
// the guard is nil, the bare subscriber preserves the pre-flag-flip
// passthrough (subscriber.go:514).
func subscriberOptionsFor(
	guard eventbus.SessionAuthGuard,
	dekMgr eventbus.SessionDEKManager,
	auditEm eventbus.SessionAuditEmitter,
) []eventbus.SubscribeOption {
	// Require both guard and dekMgr: a guard without a DEK manager is a
	// half-configured decrypt path. This mirrors newHistoryReader's
	// all-or-nothing contract (see this file's newHistoryReader doc); the
	// inner fail-closed at subscriber.go:645 (EVENTBUS_DEK_MANAGER_NIL) is the
	// deeper guard if the subscriber is ever built directly without this seam.
	if guard == nil || dekMgr == nil {
		return nil
	}
	opts := []eventbus.SubscribeOption{
		eventbus.WithSubscriberAuthGuard(guard),
		eventbus.WithSubscriberDEKManager(dekMgr),
	}
	if auditEm != nil {
		opts = append(opts, eventbus.WithSubscriberDecryptAuditEmitter(auditEm))
	}
	return opts
}

// Start wires all dependencies and starts the gRPC server.
// Start is idempotent: if the gRPC server is already running, it returns nil.
// codecov:ignore — tested by integration and E2E tests
func (s *grpcSubsystem) Start(ctx context.Context) error {
	if s.grpcServer != nil {
		return nil // already started
	}

	// Resolve the TLS config provider (07-09 item 2) — wins over the given
	// TLSConfig field when non-nil. Backed by tlscerts.TLSSubsystem's
	// panic-guarded accessor; DependsOn(SubsystemTLS) guarantees Start has
	// already run.
	if s.cfg.TLSProvider != nil {
		s.cfg.TLSConfig = s.cfg.TLSProvider()
	}

	// Resolve the cryptoWiring builder (07-09 items 4 + 9). This is the
	// FIRST call to CryptoWiring in the running system (grpcSub depends,
	// transitively or directly, on every other wiring consumer's
	// dependencies), so it is also where the invalidation.Coordinator gets
	// constructed + started, as a side effect inside the builder.
	if s.cfg.CryptoWiring != nil {
		w, wiringErr := s.cfg.CryptoWiring()
		if wiringErr != nil {
			return wiringErr
		}
		// Thread the production dek.Manager so Start() below can construct
		// the AuthGuard + AuditEmitter for INV-CRYPTO-22 FallbackResolver.
		// When nil (KEK unavailable), the history reader keeps nil-auth
		// passthrough.
		s.cfg.RekeyManager = w.rekeyManager
	}

	// Gather dependencies from subsystems.
	// F7: EventWriter and PG events table are gone. CoreServer.emitCommandResponse
	// publishes directly to JetStream via WithEventPublisher. rawEventStore is
	// kept only for system-info / game-ID (GetSystemInfo / InitGameID) used by
	// GameSettings.
	rawEventStore := s.cfg.DB.EventStore()
	pool := s.cfg.DB.Pool()
	policyEngine := s.cfg.ABAC.Engine()
	worldService := s.cfg.World.Service()
	authService := s.cfg.Auth.AuthService()
	resetService := s.cfg.Auth.ResetService()
	authPlayerRepo := s.cfg.Auth.PlayerRepo()
	authPlayerSessionRepo := s.cfg.Auth.PlayerSessionStore()
	sessionStore := s.cfg.Sessions.Store()

	// Wire session store as the movement hook so character moves propagate
	// LocationID + LocationArrivedAt to active sessions before the move event
	// is emitted (ADR holomush-kmac; INV-PRIVACY-1 Tier 1).
	worldService.SetMovementHook(&sessionStoreMovementHook{sessions: sessionStore})

	startLocationID := s.cfg.Bootstrap.StartLocationID()
	pluginManager := s.cfg.Plugins.Manager()
	cmdRegistry := s.cfg.Plugins.CommandRegistry()
	aliasRepo := s.cfg.Plugins.AliasRepo()
	aliasCache := s.cfg.Plugins.AliasCache()
	// F1 cutover: plugin emits now publish to JetStream via the EventBus
	// Publisher. EventBus subsystem is a DependsOn above, so the publisher
	// is guaranteed non-nil by the time Start runs.
	if s.cfg.EventBus == nil {
		return oops.Code("GRPC_EVENTBUS_MISSING").
			Errorf("gRPC subsystem requires EventBus subsystem for plugin emit routing")
	}
	rawPublisher := s.cfg.EventBus.Publisher(publisherOptionsFor(s.cfg)...)
	if rawPublisher == nil {
		return oops.Code("GRPC_EVENTBUS_NOT_STARTED").
			Errorf("EventBus publisher is nil; subsystem not started")
	}

	publisher, err := s.wrapPublisher(rawPublisher)
	if err != nil {
		return err
	}

	pluginManager.ConfigureEventEmitter(
		publisher,
		plugins.WithGameID(s.cfg.EventBus.GameID),
	)

	// bus is the one game-id source shared by every closure below — the
	// presence emitter, the SessionAdmin broadcast backing, and (Task 3)
	// command.Services' SystemBroadcaster all qualify subjects off the same
	// EventBus.GameID(), rather than three independent sources (D-02).
	bus := s.cfg.EventBus

	// Wire the plugin SessionAdmin broadcast backing now that the publisher
	// exists — the brokered SessionAdminService.Broadcast emits a system event to
	// the reserved subject over this publisher (holomush-eykuh.4.2, decision
	// holomush-t019a). Late binding: the publisher is built here, after the plugin
	// subsystem started, so this cannot be a host construction-time option.
	s.cfg.Plugins.ConfigureSystemBroadcaster(publisher, func() string { return bus.GameID() })

	// 1. Create the presence emitter (arrive/leave/session_ended) over the
	// SAME wrapped publisher CoreServer.emitCommandResponse uses (never
	// rawPublisher — the audit projection fails closed without the
	// App-Rendering header the wrapper stamps), plus the bus's own GameID
	// as the qualification source (FINDING-5).
	presenceEmitter := presence.NewEmitter(publisher, s.cfg.EventBus.GameID)

	// Wire game-session fanout into the auth service so evictions emit
	// session_ended events for child game sessions before FK cascade removes them.
	authService.ConfigureGameSessionFanout(presenceEmitter, sessionStore)

	// 2. Create gRPC server with TLS credentials.
	// Install GRPCServiceProxy as UnknownServiceHandler so plugin-provided
	// gRPC services are automatically forwarded through the service registry.
	serviceRegistry := s.cfg.Plugins.ServiceRegistry()
	grpcProxy := plugins.NewGRPCServiceProxy(serviceRegistry)

	creds := credentials.NewTLS(s.cfg.TLSConfig)
	s.grpcServer = grpc.NewServer(
		grpc.Creds(creds),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		// Resource limits — see internal/grpc package constants for rationale.
		// Bounds memory per request and caps concurrent streams per connection
		// so a single client cannot open unlimited Subscribe streams.
		grpc.MaxRecvMsgSize(holoGRPC.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(holoGRPC.MaxSendMsgSize),
		grpc.MaxConcurrentStreams(holoGRPC.MaxConcurrentStreams),
		grpcProxy.Handler(),
	)

	// 3. Create guest authenticator (using start location from bootstrap).
	guestAuth := telnet.NewGuestAuthenticator(naming.NewGemstoneElementTheme(), startLocationID)

	// 4. Create auth adapters for CharacterService and gRPC options.
	charRepo := worldpostgres.NewCharacterRepository(pool)
	locRepo := worldpostgres.NewLocationRepository(pool)
	authCharRepo := bootstrapsetup.NewCharRepoAdapter(pool, charRepo)
	authLocRepo := bootstrapsetup.NewLocRepoAdapter(&startLocationID, locRepo)

	// 5. Create binding repository and transactor (shared with the genesis service).
	bindingRepo := worldpostgres.NewBindingRepository(pool)
	transactor := worldpostgres.NewTransactor(pool)

	// 5a. Build the atomic character-genesis service (character + optional binding
	// + genesis envelope in one world transaction, 05-15). Its concrete char
	// writer, transactor, binding repo, and outbox store share the same pool so
	// all three enroll in the same world txKey. It is the ONLY production path
	// that inserts a character.
	genesis, genErr := auth.NewCharacterGenesisService(
		charRepo,
		transactor,
		bindingRepo,
		worldpostgres.NewOutboxStore(pool),
		worldpostgres.NewReapingGuard(pool),
	)
	if genErr != nil {
		return oops.Code("CHARACTER_GENESIS_SERVICE_FAILED").Wrap(genErr)
	}

	characterService, charErr := auth.NewCharacterService(authCharRepo, authLocRepo, genesis)
	if charErr != nil {
		return oops.Code("CHARACTER_SERVICE_FAILED").Wrap(charErr)
	}

	// 5a'. Build the atomic character-reaping service (05-16 / round-5 D-06): the
	// tombstone-emitting DELETION counterpart to the genesis service. For each of a
	// guest's characters it runs {property cascade + guarded world Delete +
	// character_deleted envelope} in one re-entrant world tx, then deletes the
	// player. It reaps through the version-scanning CharacterRepository.ListByPlayer
	// (R6-1) + guarded Delete, the world property repo (R6-3 cascade parity), the
	// same outbox store + transactor as genesis, and the concrete auth player repo
	// (MarkReaping + DeleteGuestPlayer, own pool). Both guest-deletion paths — the
	// reaper and failed-guest cleanup — route through it.
	reapPlayerRepo := authpostgres.NewPlayerRepository(pool)
	reapingService, reapErr := auth.NewCharacterReapingService(
		charRepo, // version-scanning ListByPlayer (R6-1)
		charRepo, // guarded tombstone-delta Delete
		worldpostgres.NewPropertyRepository(pool),
		bindingRepo, // hard-deletes guest bindings in-tx (RESTRICT FK, guest teardown)
		transactor,
		worldpostgres.NewOutboxStore(pool),
		reapPlayerRepo, // DeleteGuestPlayer (own pool, ordered after tombstones)
		reapPlayerRepo, // MarkReaping (R6-2 anti-TOCTOU)
	)
	if reapErr != nil {
		return oops.Code("CHARACTER_REAPING_SERVICE_FAILED").Wrap(reapErr)
	}

	// 5b. Create guest service for gRPC-based guest login (web client). Guest
	// creation commits the player first (own pool), then routes character +
	// binding + envelope through the genesis service. Failed-guest cleanup routes
	// through the reaping service (tombstone-emitting).
	guestService, guestSvcErr := auth.NewGuestService(
		guestAuth,
		authPlayerRepo,
		authCharRepo,
		authPlayerSessionRepo,
		genesis,
		reapingService,
	)
	if guestSvcErr != nil {
		return oops.Code("GUEST_SERVICE_FAILED").Wrap(guestSvcErr)
	}

	// 6. Remove disabled commands from registry.
	for _, name := range s.cfg.GameConfig.DisabledCommands {
		if unregErr := cmdRegistry.Unregister(name); unregErr != nil {
			slog.WarnContext(ctx, "disabled command not found in registry", "command", name)
		} else {
			slog.WarnContext(ctx, "disabled built-in command", "command", name)
		}
	}

	// 7. Create command services and dispatcher.
	cmdServices, cmdSvcErr := command.NewServices(command.ServicesConfig{
		World:              worldService,
		Session:            sessionStore,
		Engine:             policyEngine,
		Broadcaster:        sysbroadcast.NewBroadcaster(publisher, func() string { return bus.GameID() }),
		AliasCache:         aliasCache,
		AliasRepo:          aliasRepo,
		Registry:           cmdRegistry,
		StartingLocationID: startLocationID,
	})
	if cmdSvcErr != nil {
		return oops.Code("COMMAND_SERVICES_FAILED").Wrap(cmdSvcErr)
	}

	focusRedirects, frErr := pluginManager.BuildFocusRedirects(cmdRegistry)
	if frErr != nil {
		return oops.Code("FOCUS_REDIRECTS_INVALID").Wrap(frErr)
	}
	cmdDispatcher, cmdDispErr := command.NewDispatcher(
		cmdRegistry, policyEngine,
		command.WithAliasCache(aliasCache),
		command.WithPluginDeliverer(pluginManager),
		command.WithFocusReader(command.NewStoreFocusReader(sessionStore)),
		command.WithFocusRedirects(focusRedirects),
	)
	if cmdDispErr != nil {
		return oops.Code("COMMAND_DISPATCHER_FAILED").Wrap(cmdDispErr)
	}

	// 8. Create CoreServer and register with gRPC.
	// F4: wire the JetStream/PostgreSQL tier crossover history reader into
	// QueryStreamHistory. Both the JetStream context and the PG pool are
	// in scope here, making Option B the natural construction site. The
	// reader is built inline so we don't need to add a pool parameter to
	// eventbus.Subsystem (which doesn't own the DB pool).
	//
	// F5: also wire the subject-ownership map + plugin history router so
	// QueryHistory calls for plugin-owned subjects route to the plugin's
	// PluginAuditService.QueryHistory RPC instead of the host tiers.
	js := s.cfg.EventBus.JS()
	owners := cryptowiring.OwnerMapFromManager(managerSource{mgr: pluginManager})
	router := audit.NewPluginHistoryRouter(pluginAuditClientProvider{mgr: pluginManager})

	// INV-CRYPTO-22 wiring: if RekeyManager is set (production shape with KEK wired),
	// construct AuthGuard + AuditEmitter for the FallbackResolver path.
	// AuthGuard and AuditEmitter may be pre-set for test injection; in prod they
	// are always nil here and built below.
	historyAuthGuard := s.cfg.AuthGuard
	historyDEKMgr := s.cfg.RekeyManager
	historyAuditEm := s.cfg.AuditEmitter
	if s.cfg.RekeyManager != nil && historyAuthGuard == nil && historyAuditEm == nil {
		auditEm, auditErr := authguardaudit.NewQueuedEmitter(publisher,
			authguardaudit.WithGameID(s.cfg.EventBus.GameID()))
		if auditErr != nil {
			slog.WarnContext(ctx, "history auth guard: audit emitter construction failed — INV-CRYPTO-22 fallback disabled",
				"error", auditErr)
		} else {
			sessionBridgeEm, bridgeErr := authguardaudit.NewSessionBridgeEmitter(auditEm)
			if bridgeErr != nil {
				slog.WarnContext(ctx, "history auth guard: session bridge emitter construction failed — INV-CRYPTO-22 fallback disabled",
					"error", bridgeErr)
			} else {
				participantLookup := authguard.NewDEKParticipantLookup(s.cfg.RekeyManager)
				manifestLookup := authguard.NewPluginManifestLookup(pluginManager)
				guard, guardErr := authguard.New(participantLookup, manifestLookup, policyEngine, auditEm)
				if guardErr != nil {
					slog.WarnContext(ctx, "history auth guard: guard construction failed — INV-CRYPTO-22 fallback disabled",
						"error", guardErr)
				} else {
					historyAuthGuard = authguard.NewSessionBridgeGuard(guard)
					historyAuditEm = sessionBridgeEm
				}
			}
		}
	}

	// F3 / Phase 3d flag flip (holomush-5rh.8.29): wire the JetStream event bus
	// subscriber into the Subscribe RPC. The bus owns per-session durable
	// consumers; the gRPC handler pumps deliveries through Send → Ack. The
	// subscriber is built AFTER the AuthGuard/DEK/audit triad is resolved so
	// decode-on-fan-out can decrypt sensitive events for DEK participants. When
	// KEK is absent (historyAuthGuard==nil) the bare subscriber preserves the
	// pre-flag-flip passthrough. EventBus subsystem is a DependsOn above, so
	// Subscriber is guaranteed non-nil by the time Start runs.
	subscriber := s.cfg.EventBus.Subscriber(
		subscriberOptionsFor(historyAuthGuard, historyDEKMgr, historyAuditEm)...,
	)
	if subscriber == nil {
		return oops.Code("GRPC_EVENTBUS_SUBSCRIBER_NIL").
			Errorf("EventBus subscriber is nil; subsystem not started")
	}

	// Phase 7 INV-CRYPTO-42 + INV-CRYPTO-50: assemble the PluginDowngradeFence
	// inputs from already-loaded deps. pluginManager is the same
	// pluginSub.Manager() the audit closure used to build the
	// PluginConsumerManager — its manifests are populated by now
	// (DependsOn enforces plugin Start before gRPC Start).
	alwaysSensitive := cryptowiring.AlwaysSensitiveSet(managerSource{mgr: pluginManager})
	cryptoKeysLookupForFence := cryptowiring.CryptoKeysLookup(pool)
	// Pass the RAW publisher + registry; newViolationEmitter wraps
	// internally so the violation event gets exactly one App-Rendering
	// stamp. The subsystem's primary `publisher` is already a
	// RenderingPublisher and would re-wrap to EMIT_RESERVED_HEADER —
	// the constructor's signature makes that misuse impossible.
	violationEmitterForFence := newViolationEmitter(
		s.cfg.EventBus.Publisher(),
		s.cfg.VerbRegistry,
		s.cfg.EventBus.GameID(),
	)

	historyReader := newHistoryReader(js, pool, s.cfg.EventBus.Config(), owners, router,
		historyAuthGuard, historyDEKMgr, historyAuditEm,
		s.cfg.KeySelector, alwaysSensitive, cryptoKeysLookupForFence, violationEmitterForFence)

	coreServerOpts := []holoGRPC.CoreServerOption{
		// The wrapped publisher (RenderingPublisher) is used here — NEVER
		// rawPublisher — so command_response/command_error events carry the
		// App-Rendering header the audit projection requires (round 7, MEDIUM).
		holoGRPC.WithEventPublisher(publisher, s.cfg.EventBus.GameID),
		holoGRPC.WithWorldQuerier(worldService),
		holoGRPC.WithAuthService(authService),
		holoGRPC.WithResetService(resetService),
		holoGRPC.WithCharacterService(characterService),
		holoGRPC.WithPlayerSessionRepo(authPlayerSessionRepo),
		holoGRPC.WithPlayerRepo(authPlayerRepo),
		holoGRPC.WithCharacterRepo(authCharRepo),
		holoGRPC.WithSessionDefaults(holoGRPC.SessionDefaults{
			TTL:        s.cfg.SessionTTL,
			MaxHistory: s.cfg.MaxHistory,
		}),
		holoGRPC.WithDisconnectHook(func(info session.Info) {
			if info.IsGuest {
				guestAuth.ReleaseGuest(info.CharacterName)
			}
		}),
		holoGRPC.WithGuestService(guestService),
		holoGRPC.WithBindingRepository(bindingRepo),
		holoGRPC.WithCryptoActive(cryptoActiveFor(s.cfg)),
		holoGRPC.WithStreamContributor(pluginManager),
		holoGRPC.WithAccessEngine(policyEngine),
		holoGRPC.WithCommandQuerier(s.cfg.Plugins.CommandQuerier()),
		holoGRPC.WithSubscriber(subscriber),
		holoGRPC.WithHistoryReader(historyReader),
		// gameID is also set by WithEventPublisher above (same source,
		// s.cfg.EventBus.GameID); kept explicit here too since WithGameID
		// predates WithEventPublisher and other Subscribe-path readers
		// look for it by name.
		holoGRPC.WithGameID(s.cfg.EventBus.GameID),
		holoGRPC.WithIdentityRegistry(pluginManager),
		// VerbRegistry feeds the synthetic-location_state emit path's
		// RenderingMetadata stamp. Without it, the locationFollower's
		// fallback EventFrame has nil Rendering and the gateway drops
		// every synthetic event per INV-EVENTBUS-6 (holomush-4wdu).
		holoGRPC.WithVerbRegistry(s.cfg.VerbRegistry),
	}
	if s.cfg.StreamRegistry != nil {
		coreServerOpts = append(coreServerOpts, holoGRPC.WithStreamRegistry(s.cfg.StreamRegistry))
	}

	// 8a. Create focus.Coordinator.
	gameSettings := settings.NewGameSettings(&settings.SystemInfoAdapter{
		Store:       rawEventStore,
		NotFoundErr: store.ErrSystemInfoNotFound,
	})
	// Character-scope settings: repo-backed over characters.preferences
	// (iokti.5). Owner-partitioned, persisted via read-modify-write.
	// The preferences WRITE routes through the world boundary
	// (world.Service.UpdateCharacterPreferences — version-guarded + enveloped;
	// round-4 C5 / D-05); the READ stays a direct pool read.
	characterSettings := settings.NewRepoCharacterSettingsStore(
		store.NewCharacterSettingsRepository(pool, worldService),
	)
	focusCoordOpts := []holoFocus.CoordinatorOption{
		holoFocus.WithSessionStore(sessionStore),
		holoFocus.WithKindPolicy(scenepolicy.New()),
		holoFocus.WithGameSettings(gameSettings),
		holoFocus.WithCharacterSettings(characterSettings),
		holoFocus.WithPlayerPreferences(holoFocus.NewPlayerPrefsAdapter(authPlayerRepo)),
		holoFocus.WithStreamContributor(&focusStreamContributorAdapter{pm: pluginManager}),
	}
	focusCoordOpts = append(focusCoordOpts, holoFocus.WithGameID(s.cfg.EventBus.GameID()))
	if s.cfg.StreamRegistry != nil {
		focusCoordOpts = append(focusCoordOpts, holoGRPC.FocusStreamCoordinatorOptions(s.cfg.StreamRegistry)...)
	}
	focusCoord, focusErr := holoFocus.NewCoordinator(focusCoordOpts...)
	if focusErr != nil {
		return oops.Code("FOCUS_COORDINATOR_FAILED").Wrap(focusErr)
	}
	// 5b2j: inject the character-name resolver used by ListFocusPresence to
	// batch-resolve names for the presence snapshot.
	coreServerOpts = append(coreServerOpts,
		holoGRPC.WithFocusCoordinator(focusCoord),
		holoGRPC.WithCharacterNameResolver(holoGRPC.NewRepoCharacterNameResolver(charRepo)))

	// 8b. Inject focus coordinator + history reader into plugin hosts (late-binding).
	// The plugin subsystem started before gRPC, so these deps were not available
	// at host construction time. Binary plugins use them for JoinFocus/LeaveFocus/
	// PresentFocus/QueryStreamHistory RPCs; Lua plugins use them for holomush.*
	// hostfuncs. F7: pass busHistoryReaderAdapter (JetStream tier) instead of
	// the legacy EventStore.
	pluginHistoryReader := &busHistoryReaderAdapter{
		reader: historyReader,
		gameID: s.cfg.EventBus.GameID,
	}
	pluginManager.ConfigureFocusDeps(focusCoord, pluginHistoryReader)

	// 8b2: Inject the owner-partitioned settings stores into plugin hosts
	// (late-binding, holomush-iokti.7). Binary plugins use them for the
	// GetSetting/SetSetting host RPCs; the player store is repo-backed over
	// players.preferences (owner partition persisted via read-modify-write),
	// reusing the same player repo as the focus coordinator's player-prefs
	// adapter. gameSettings / characterSettings were built above for the focus
	// coordinator and are shared here.
	playerSettings := settings.NewRepoPlayerSettingsStore(authPlayerRepo)
	pluginManager.ConfigureSettingsDeps(playerSettings, characterSettings, gameSettings)

	// Wire the read-back decryptor for the DecryptOwnAuditRows host RPC
	// (holomush-m7pxs INV-CRYPTO-27/31/37). It reuses the SAME OwnerMap (g1
	// ownership gate) and crypto deps (fence set, DEK-existence lookup,
	// AuthGuard, DEKManager, audit emitter) assembled above for the history
	// reader, so the snapshot read-back path is authorization-symmetric with
	// the routed read path.
	pluginManager.ConfigureReadbackDecryptor(history.NewReadbackDecryptor(
		owners,
		alwaysSensitive,
		cryptoKeysLookupForFence,
		historyAuthGuard,
		historyDEKMgr,
		historyAuditEm,
	))

	// Wire the SceneMuteChecker consulted at the SCENE_ACTIVITY badge downgrade
	// (D-04): a non-focused member's badge is suppressed when their GLOBAL notify
	// preference is off (Finding 2) or the scene is muted. The checker's loader
	// dispatches to the plugin SceneService with the SAME host-vouched
	// actor+ownerPlayerID the SceneAccessService facade uses (BeginServiceDispatch,
	// Finding 3), reading BOTH GetSceneNotifyPref and ListMutedScenes on the
	// vouched ctx — NOT the SceneAccessService facade, which exposes neither RPC.
	// The plugin guards each request's character_id against the vouched actor
	// metadata (mirroring the facade). When the plugin is absent the checker stays
	// unset and CoreServer fails OPEN (delivers every badge).
	const sceneServiceName = "holomush.scene.v1.SceneService"
	if sceneSvc, resolveErr := serviceRegistry.Resolve(sceneServiceName); resolveErr == nil {
		sceneMuteClient := scenev1.NewSceneServiceClient(sceneSvc.Conn)
		loader := func(loaderCtx context.Context, characterID, playerID string) (bool, []string, error) {
			// Bound the plugin round-trip so a hung core-scenes plugin cannot block
			// badge delivery indefinitely (fail-open covers errors, not hangs).
			bctx, cancel := context.WithTimeout(loaderCtx, sceneMuteLoaderTimeout)
			defer cancel()
			actor := core.Actor{Kind: core.ActorCharacter, ID: characterID}
			dctx, release, dispErr := pluginManager.BeginServiceDispatch(bctx, "core-scenes", actor, playerID)
			if dispErr != nil {
				return false, nil, oops.Wrap(dispErr)
			}
			defer release()
			pref, prefErr := sceneMuteClient.GetSceneNotifyPref(dctx, &scenev1.GetSceneNotifyPrefRequest{CharacterId: characterID})
			if prefErr != nil {
				return false, nil, oops.Wrap(prefErr)
			}
			muted, mutedErr := sceneMuteClient.ListMutedScenes(dctx, &scenev1.ListMutedScenesRequest{CharacterId: characterID})
			if mutedErr != nil {
				return false, nil, oops.Wrap(mutedErr)
			}
			return pref.GetEnabled(), muted.GetSceneIds(), nil
		}
		coreServerOpts = append(coreServerOpts,
			holoGRPC.WithSceneMuteChecker(holoGRPC.NewSceneMuteChecker(loader, sceneMuteNotifyCacheTTL, time.Now)))
	} else {
		slog.WarnContext(ctx, "sceneMuteChecker unavailable: plugin absent — badge suppression disabled (fail-open)",
			"service", sceneServiceName)
	}

	coreServer := holoGRPC.NewCoreServer(presenceEmitter, sessionStore, cmdDispatcher, cmdServices, coreServerOpts...)
	corev1.RegisterCoreServiceServer(s.grpcServer, coreServer)

	// 9. Create ContentService, register with gRPC.
	contentStore := content.NewPostgresStore(pool)
	contentv1.RegisterContentServiceServer(s.grpcServer, holoGRPC.NewContentServiceServer(contentStore))

	// 9a. Create SceneAccessService facade, register with gRPC.
	// The facade wraps the plugin SceneService with host-side player auth,
	// server-verified identity resolution (INV-SCENE-63), and guest rejection
	// (INV-SCENE-64). The SceneService client is resolved from the plugin
	// service registry; if the plugin is absent the facade falls back to its
	// embedded UnimplementedSceneAccessServiceServer (Unimplemented for all RPCs).
	// sceneServiceName is declared above where the SceneMuteChecker resolves the
	// same plugin SceneService.
	var sceneAccessSrv sceneaccessv1.SceneAccessServiceServer
	if sceneSvc, resolveErr := serviceRegistry.Resolve(sceneServiceName); resolveErr == nil {
		saSrv := holoGRPC.NewSceneAccessServer(
			authPlayerSessionRepo,
			authPlayerRepo,
			authCharRepo,
			sessionStore,
			focusCoord,
			scenev1.NewSceneServiceClient(sceneSvc.Conn),
			pluginManager,
		)
		// Seed DEK participants on SetSceneFocus when the KEK/DEK stack is
		// present so the AuthGuard permits decryption of sensitive scene
		// events for the focusing session (holomush-5rh.8.29). Gated on
		// RekeyManager != nil to mirror the publisher/subscriber gating.
		if s.cfg.RekeyManager != nil {
			saSrv.WithSceneDEKAdder(s.cfg.RekeyManager)
		}
		saSrv.WithCharacterNameResolver(holoGRPC.NewRepoCharacterNameResolver(charRepo))
		sceneAccessSrv = saSrv
		slog.InfoContext(ctx, "sceneAccessService facade registered")
	} else {
		slog.WarnContext(ctx, "sceneAccessService unavailable: plugin absent", "service", sceneServiceName)
		sceneAccessSrv = &sceneaccessv1.UnimplementedSceneAccessServiceServer{}
	}
	sceneaccessv1.RegisterSceneAccessServiceServer(s.grpcServer, sceneAccessSrv)

	// 10. Create and start session reaper.
	reaperCtx, reaperCancel := context.WithCancel(context.Background())
	s.reaperCancel = reaperCancel

	s.sessionReaper = session.NewReaper(sessionStore, session.ReaperConfig{
		Interval:  s.cfg.ReaperInterval,
		LeaseTTL:  s.cfg.LeaseTTL,
		BootGrace: s.cfg.BootGrace,
		OnExpired: func(info *session.Info) {
			char := core.CharacterRef{
				ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID,
			}
			if dcErr := presenceEmitter.EmitLeave(reaperCtx, char, "session expired"); dcErr != nil {
				slog.WarnContext(
					reaperCtx,
					"reaper: leave event failed",
					"session_id", info.ID,
					"error", dcErr,
				)
			}
			if endErr := presenceEmitter.EmitSessionEnded(reaperCtx, char, info.ID,
				core.SessionEndedCauseReaped,
				"Session expired due to inactivity."); endErr != nil {
				slog.WarnContext(reaperCtx, "reaper: session_ended event failed",
					"session_id", info.ID, "error", endErr)
			}
			if info.IsGuest {
				guestAuth.ReleaseGuest(info.CharacterName)
			}
		},
		OnSessionDetached: func(info *session.Info) {
			slog.InfoContext(reaperCtx, "lease sweep detached session", "session_id", info.ID)
			// Leave is deferred to OnExpired at TTL (matches the cooperative detach path).
		},
		OnGridPhaseOut: func(info *session.Info) {
			char := core.CharacterRef{
				ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID,
			}
			if dcErr := presenceEmitter.EmitLeave(reaperCtx, char, "phased out"); dcErr != nil {
				slog.WarnContext(reaperCtx, "lease sweep grid phase-out leave failed",
					"session_id", info.ID, "error", dcErr)
			}
		},
	})
	go s.sessionReaper.Run(reaperCtx)

	// 11. Create and start guest reaper. Its cleaner is the tombstone-emitting
	// reaping service (05-16 / D-06) — each reaped guest's characters are deleted
	// through the world CharacterWriter.Delete + a character_deleted tombstone
	// before the player is deleted, so guest expiration cannot produce
	// genesis-without-tombstone feed history. The lister stays the auth player repo.
	s.guestReaper = auth.NewGuestReaper(auth.GuestReaperConfig{
		Interval: 1 * time.Minute,
		IdleTTL:  10 * time.Minute,
	}, authPlayerRepo, reapingService)
	go s.guestReaper.Run(reaperCtx)

	// 12. Bind TCP listener.
	s.listener, err = net.Listen("tcp", s.cfg.GRPCAddr)
	if err != nil {
		reaperCancel()
		return oops.Code("LISTEN_FAILED").With("operation", "listen").With("addr", s.cfg.GRPCAddr).Wrap(err)
	}

	// 13. Start grpcServer.Serve() in goroutine.
	slog.InfoContext(ctx, "gRPC server listening", "addr", s.cfg.GRPCAddr)
	go func() {
		if serveErr := s.grpcServer.Serve(s.listener); serveErr != nil {
			slog.ErrorContext(ctx, "gRPC server stopped", "error", serveErr)
		}
	}()

	return nil
}

// Stop gracefully shuts down the gRPC server, cancels reapers, and closes the listener.
// GracefulStop is bound to the context — if the context expires, a hard Stop() is forced.
// codecov:ignore — tested by integration and E2E tests
func (s *grpcSubsystem) Stop(ctx context.Context) error {
	if s.grpcServer != nil {
		done := make(chan struct{})
		go func() {
			s.grpcServer.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
			// graceful shutdown completed
		case <-ctx.Done():
			slog.WarnContext(ctx, "gRPC graceful shutdown timed out, forcing stop")
			s.grpcServer.Stop()
		}
	}
	if s.reaperCancel != nil {
		s.reaperCancel()
	}
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			slog.DebugContext(ctx, "error closing gRPC listener", "error", err)
		}
	}
	// grpcSubsystem owns the invalidation.Coordinator's lifecycle (07-09
	// item 4) — replaces the former runCore-level ad-hoc defer with
	// orchestrator-ordered Stop. CoordHolder.coord is nil when the wiring
	// was never resolved (CryptoWiring nil, e.g. some unit-test configs) or
	// when the Coordinator degraded-path construction/start failed.
	if s.cfg.CoordHolder != nil && s.cfg.CoordHolder.coord != nil {
		if stopErr := s.cfg.CoordHolder.coord.Stop(ctx); stopErr != nil {
			slog.WarnContext(ctx, "invalidation.Coordinator stop error", "error", stopErr)
		}
	}
	return nil
}

// sessionStoreMovementHook implements world.MovementHook by delegating to
// session.Store.UpdateLocationOnMove. Wired at gRPC subsystem startup so
// character moves propagate LocationID + LocationArrivedAt to active sessions
// before the move event is emitted (ADR holomush-kmac; INV-PRIVACY-1 Tier 1).
type sessionStoreMovementHook struct {
	sessions session.Store
}

func (h *sessionStoreMovementHook) OnCharacterMoved(ctx context.Context, characterID, newLocationID ulid.ULID, arrivedAt time.Time) error {
	return h.sessions.UpdateLocationOnMove(ctx, characterID, newLocationID, arrivedAt)
}

var _ world.MovementHook = (*sessionStoreMovementHook)(nil)

// busHistoryReaderAdapter bridges eventbus.HistoryReader (QueryHistory) to
// plugins.HistoryReader (ReplayTail). Used by the plugin subsystem for
// QueryStreamHistory RPCs and Lua holomush.query_stream_history hostfuncs.
type busHistoryReaderAdapter struct {
	reader eventbus.HistoryReader
	gameID func() string
}

var _ plugins.HistoryReader = (*busHistoryReaderAdapter)(nil)

// ReplayTail satisfies plugins.HistoryReader. It fetches count most-recent
// events on stream (optionally filtered by notBefore and an exclusive
// (beforeSeq, beforeID) cursor), returning them in ascending ULID order
// (oldest→newest).
//
// beforeSeq == 0 means "no cursor — read the tail" (D-07/ARCH-04): there is
// no ID-only pagination fallback on either tier (see
// internal/eventbus/bus.go's HistoryQuery doc). beforeID alone is a tripwire
// that validates a nonzero beforeSeq still names the same event in storage;
// it is set whenever provided (including as a stale legacy artifact of a
// zero-seq cursor) but only takes effect once beforeSeq is also nonzero.
func (a *busHistoryReaderAdapter) ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeSeq uint64, beforeID ulid.ULID) ([]eventbus.Event, error) {
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
	if beforeSeq != 0 {
		q.BeforeSeq = beforeSeq
	}
	if !beforeID.IsZero() {
		q.BeforeID = beforeID
	}
	hs, err := a.reader.QueryHistory(ctx, q)
	if err != nil {
		return nil, oops.With("stream", stream).Wrap(err)
	}
	defer hs.Close() //nolint:errcheck // best-effort iterator close

	// Backward direction yields newest-first; collect then reverse.
	collected := make([]eventbus.Event, 0, count)
	for {
		e, nextErr := hs.Next(ctx)
		if nextErr != nil {
			if nextErr == io.EOF { //nolint:errorlint // io.EOF is sentinel
				break
			}
			return nil, oops.With("stream", stream).Wrap(nextErr)
		}
		collected = append(collected, e)
		if len(collected) >= count {
			break
		}
	}

	// Reverse to ascending (oldest→newest). The frame's Subject is already
	// the fully-qualified dot subject QueryHistory produced (holomush-rops);
	// callers reading it as a plugin-facing "stream" value re-derive the
	// relative form themselves (see eventbusEventToProto / the Lua table
	// build in hostfunc), so no per-event reconstruction happens here.
	result := make([]eventbus.Event, len(collected))
	for i := range collected {
		j := len(collected) - 1 - i
		result[j] = collected[i]
	}
	return result, nil
}

// focusStreamContributorAdapter bridges plugins.Manager.QuerySessionStreams
// to focus.StreamContributor by converting the request type.
type focusStreamContributorAdapter struct {
	pm *plugins.Manager
}

// QuerySessionStreams implements focus.StreamContributor.
func (a *focusStreamContributorAdapter) QuerySessionStreams(ctx context.Context, req holoFocus.StreamContributorRequest) []string {
	return a.pm.QuerySessionStreams(ctx, plugins.SessionStreamsRequest{
		CharacterID: req.CharacterID,
		PlayerID:    req.PlayerID,
		SessionID:   req.SessionID,
	})
}

// newHistoryReader constructs the F4 JetStream/PostgreSQL tier crossover
// reader with F5 plugin-owned subject routing. js and pool may be nil if
// the subsystem has not started yet, but in production both are non-nil
// by the time Start() reaches this point. The wall clock is passed as
// time.Now — forbidigo bans time.Now only inside internal/eventbus/**
// and test/integration/eventbus_e2e/**; cmd/ is not in the banned path.
//
// owners MAY be nil; when non-nil, plugin-owned subjects are routed to
// the given PluginHistoryRouter rather than queried locally. router MAY
// be nil only when owners is also nil — the Reader surfaces
// EVENTBUS_PLUGIN_HISTORY_NOT_WIRED when a plugin-owned subject is
// queried without a router.
//
// guard, dekMgr, and auditEm are optional crypto dependencies. All three
// MUST be non-nil for any to take effect — partial wiring is a
// misconfiguration and is rejected silently (the Reader falls back to
// nil-auth passthrough, same as today). The all-or-nothing contract
// prevents half-configured states where, e.g., the AuthGuard can permit
// a decrypt but the DEKManager is nil and panics in decodeAuthorizeAndDispatch.
//
// When all three are non-nil, WithHistoryAuthAndSourceResolver wires the
// INV-CRYPTO-22 hot→cold FallbackResolver on the hot tier and a SimpleResolver
// on the cold tier (no further fallback past the cold tier itself). pool
// may be nil — the FallbackResolver's ColdTierLookup is only invoked on
// actual reads when a hot-tier DEK miss occurs.
//
// When all three are nil (degraded deployment), behavior is unchanged —
// sensitive events surface EVENTBUS_HISTORY_AUTH_GUARD_NIL.
func newHistoryReader(
	js jetstream.JetStream,
	pool *pgxpool.Pool,
	cfg eventbus.Config,
	owners *audit.OwnerMap,
	router history.PluginHistoryRouter,
	guard eventbus.SessionAuthGuard, // nil = passthrough (current behavior)
	dekMgr eventbus.SessionDEKManager, // nil = passthrough (current behavior)
	auditEm eventbus.SessionAuditEmitter, // nil = passthrough (current behavior)
	keySelector codec.KeySelector, // nil = identity decoding (Phase 7 INV-CRYPTO-45)
	alwaysSensitive map[string]struct{}, // empty = INV-CRYPTO-42 manifest-set check off
	cryptoKeysLookup history.CryptoKeysLookup, // nil = INV-CRYPTO-50 DEK-existence check off
	violationEmitter history.ViolationEmitter, // nil = no plugin_integrity_violation publish
) eventbus.HistoryReader {
	opts := []history.Option{}
	if owners != nil {
		opts = append(opts, history.WithOwners(owners))
	}
	if router != nil {
		opts = append(opts, history.WithPluginRouter(router))
	}
	if keySelector != nil {
		// INV-CRYPTO-45: the SAME selector instance must be threaded into the
		// PluginConsumerManager (cmd/holomush/core.go:488 audit closure)
		// for cross-tier pointer identity. Production wiring constructs
		// the selector once and passes it via grpcSubsystemConfig.KeySelector.
		opts = append(opts, history.WithCodecSelector(keySelector))
	}
	// Phase 7 INV-CRYPTO-42 + INV-CRYPTO-50: install the read-side fence around
	// plugin-routed history. Both lookup and emitter may be nil in
	// degraded deployments; the fence's internal nil-handling preserves
	// the per-row refusal semantics.
	//
	// T8 (INV-CRYPTO-32): wire the fence's read-back crypto (guard/dek/audit) so a
	// clean plugin-owned row can be DECRYPTED for an authorized routed
	// participant. These are the SAME guard/dekMgr/auditEm forwarded below to
	// the tier auth; when crypto is disabled (Crypto.Enabled=false) all three
	// are nil and the fence falls back to ciphertext-passthrough on clean rows.
	if len(alwaysSensitive) > 0 || cryptoKeysLookup != nil || violationEmitter != nil {
		opts = append(opts, history.WithPluginDowngradeFenceReadback(
			alwaysSensitive,
			cryptoKeysLookup,
			violationEmitter,
			guard,
			dekMgr,
			auditEm,
		))
	}
	if guard != nil && dekMgr != nil && auditEm != nil {
		// INV-CRYPTO-22: wire FallbackResolver on hot tier (hot→cold fallback when DEK
		// destroyed after Rekey) and SimpleResolver on cold tier (cold IS the
		// fallback target; a further fallback resolver there would recurse).
		//
		// source.NewFallbackResolver and source.NewSimpleResolver require
		// dek.Manager (the full interface). The production path always supplies
		// a concrete *dek.manager; the type-assert fails only in tests that use
		// a narrow stub, in which case we fall back to WithHistoryAuth (no
		// FallbackResolver). This matches the all-or-nothing contract in the
		// doc comment above.
		if fullMgr, ok := dekMgr.(dek.Manager); ok {
			sourceMetrics := source.NewMetrics(prometheus.DefaultRegisterer)
			coldLookup := history.NewColdTierLookup(pool)
			hotResolver := source.NewFallbackResolver(fullMgr, coldLookup, sourceMetrics, slog.Default())
			coldResolver := source.NewSimpleResolver(fullMgr)
			opts = append(opts, history.WithHistoryAuthAndSourceResolver(guard, dekMgr, auditEm, hotResolver, coldResolver))
		} else {
			// Narrow stub (test path or degraded deployment) — wire guard+dekMgr+auditEm
			// without the FallbackResolver. Reads succeed but INV-CRYPTO-22 fallback is inactive.
			opts = append(opts, history.WithHistoryAuth(guard, dekMgr, auditEm))
		}
	}
	return history.NewReader(js, pool, cfg.StreamMaxAge, time.Now, opts...)
}

// pluginAuditClientProvider adapts the plugin manager to the
// audit.PluginHistoryClientProvider interface. Kept as an unexported type
// in the cmd package so the plugin package does not need to depend on
// the audit package to satisfy the contract.
type pluginAuditClientProvider struct {
	mgr *plugins.Manager
}

// PluginAuditClient resolves the PluginAuditService client by name via the
// plugin manager. Returns (nil, false) when the plugin is not loaded or
// its host does not expose a PluginAuditService.
func (p pluginAuditClientProvider) PluginAuditClient(name string) (pluginv1.PluginAuditServiceClient, bool) {
	if p.mgr == nil {
		return nil, false
	}
	return p.mgr.PluginAuditClient(name)
}
