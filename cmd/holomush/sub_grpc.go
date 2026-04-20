// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	cryptotls "crypto/tls"
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

	abacsetup "github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/auth"
	authsetup "github.com/holomush/holomush/internal/auth/setup"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/content"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/history"
	"github.com/holomush/holomush/internal/eventbus/subjectxlate"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	holoFocus "github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/grpc/focus/scenepolicy"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/naming"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsetup "github.com/holomush/holomush/internal/plugin/setup"
	"github.com/holomush/holomush/internal/session"
	sessionsetup "github.com/holomush/holomush/internal/session/setup"
	"github.com/holomush/holomush/internal/settings"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/telnet"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	worldsetup "github.com/holomush/holomush/internal/world/setup"
	contentv1 "github.com/holomush/holomush/pkg/proto/holomush/content/v1"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
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

	GRPCAddr       string
	TLSConfig      *cryptotls.Config
	SessionTTL     time.Duration
	ReaperInterval time.Duration
	MaxHistory     int
	GameConfig     config.GameConfig
	StreamRegistry *holoGRPC.SessionStreamRegistry
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
	eventWriter   *core.EventWriter
}

// newGRPCSubsystem returns a configured grpcSubsystem for the provided configuration.
// It does not allocate or start any runtime resources; Start must be called to initialize and run the subsystem.
func newGRPCSubsystem(cfg grpcSubsystemConfig) *grpcSubsystem {
	return &grpcSubsystem{cfg: cfg}
}

// ID returns SubsystemGRPC.
func (s *grpcSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemGRPC }

// DependsOn returns the subsystems that must start before gRPC.
// Bootstrap transitively depends on ABAC, World, Plugins, Database.
// EventBus is required so the Publisher is ready when ConfigureEventEmitter
// runs (F1 cutover: plugin emits publish to JetStream).
func (s *grpcSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{
		lifecycle.SubsystemBootstrap,
		lifecycle.SubsystemSessions,
		lifecycle.SubsystemAuth,
		lifecycle.SubsystemEventBus,
	}
}

// Start wires all dependencies and starts the gRPC server.
// Start is idempotent: if the gRPC server is already running, it returns nil.
// codecov:ignore — tested by integration and E2E tests
func (s *grpcSubsystem) Start(_ context.Context) error {
	if s.grpcServer != nil {
		return nil // already started
	}

	// Gather dependencies from subsystems.
	rawEventStore := s.cfg.DB.EventStore()
	s.eventWriter = core.NewEventWriter(rawEventStore)
	// F3 transitional dual-write: every host engine Append is also
	// published to JetStream so the F3 Subscribe handler (which reads
	// from the bus) still sees say/pose/move/session_ended/etc. F6 drops
	// the PG events table and the Append leg disappears.
	s.eventWriter.SetBusFanout(newCoreEventBusFanout(s.cfg.EventBus))
	// Use the EventWriter as the EventStore for all production code paths.
	// EventWriter serializes Append calls (I-14 enforcement) and delegates
	// reads to the underlying store.
	eventStore := s.eventWriter
	// Close the EventWriter on any early-return error path below to prevent
	// leaking the writer goroutine.
	writerStarted := true
	defer func() {
		if writerStarted {
			s.eventWriter.Close()
			s.eventWriter = nil
		}
	}()
	pool := s.cfg.DB.Pool()
	policyEngine := s.cfg.ABAC.Engine()
	worldService := s.cfg.World.Service()
	authService := s.cfg.Auth.AuthService()
	resetService := s.cfg.Auth.ResetService()
	authPlayerRepo := s.cfg.Auth.PlayerRepo()
	authPlayerSessionRepo := s.cfg.Auth.PlayerSessionStore()
	sessionStore := s.cfg.Sessions.Store()
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
	publisher := s.cfg.EventBus.Publisher()
	if publisher == nil {
		return oops.Code("GRPC_EVENTBUS_NOT_STARTED").
			Errorf("EventBus publisher is nil; subsystem not started")
	}
	pluginManager.ConfigureEventEmitter(
		publisher,
		plugins.WithGameID(s.cfg.EventBus.GameID),
	)

	// 1. Create core engine from event store.
	engine := core.NewEngine(eventStore, core.WithProductionGuardrail())

	// Wire game-session fanout into the auth service so evictions emit
	// session_ended events for child game sessions before FK cascade removes them.
	authService.ConfigureGameSessionFanout(engine, sessionStore)

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

	characterService, charErr := auth.NewCharacterService(authCharRepo, authLocRepo)
	if charErr != nil {
		return oops.Code("CHARACTER_SERVICE_FAILED").Wrap(charErr)
	}

	// 5. Create guest service for gRPC-based guest login (web client).
	guestService, guestSvcErr := auth.NewGuestService(
		guestAuth,
		authPlayerRepo,
		authCharRepo,
		authPlayerSessionRepo,
	)
	if guestSvcErr != nil {
		return oops.Code("GUEST_SERVICE_FAILED").Wrap(guestSvcErr)
	}

	// 6. Remove disabled commands from registry.
	for _, name := range s.cfg.GameConfig.DisabledCommands {
		if unregErr := cmdRegistry.Unregister(name); unregErr != nil {
			slog.Warn("disabled command not found in registry", "command", name)
		} else {
			slog.Warn("disabled built-in command", "command", name)
		}
	}

	// 7. Create command services and dispatcher.
	cmdServices, cmdSvcErr := command.NewServices(command.ServicesConfig{
		World:              worldService,
		Session:            sessionStore,
		Engine:             policyEngine,
		Events:             eventStore,
		AliasCache:         aliasCache,
		AliasRepo:          aliasRepo,
		Registry:           cmdRegistry,
		StartingLocationID: startLocationID,
	})
	if cmdSvcErr != nil {
		return oops.Code("COMMAND_SERVICES_FAILED").Wrap(cmdSvcErr)
	}

	cmdDispatcher, cmdDispErr := command.NewDispatcher(cmdRegistry, policyEngine,
		command.WithAliasCache(aliasCache),
		command.WithPluginDeliverer(pluginManager),
	)
	if cmdDispErr != nil {
		return oops.Code("COMMAND_DISPATCHER_FAILED").Wrap(cmdDispErr)
	}

	// 8. Create CoreServer and register with gRPC.
	// F3: wire the JetStream event bus subscriber into the Subscribe RPC.
	// The bus owns per-session durable consumers; the gRPC handler pumps
	// deliveries through Send → Ack. EventBus subsystem is a DependsOn
	// above, so Subscriber is guaranteed non-nil by the time Start runs.
	subscriber := s.cfg.EventBus.Subscriber()
	if subscriber == nil {
		return oops.Code("GRPC_EVENTBUS_SUBSCRIBER_NIL").
			Errorf("EventBus subscriber is nil; subsystem not started")
	}

	// F4: wire the JetStream/PostgreSQL tier crossover history reader into
	// QueryStreamHistory. Both the JetStream context and the PG pool are
	// in scope here, making Option B the natural construction site. The
	// reader is built inline so we don't need to add a pool parameter to
	// eventbus.Subsystem (which doesn't own the DB pool).
	js := s.cfg.EventBus.JS()
	historyReader := newHistoryReader(js, pool, s.cfg.EventBus.Config())

	coreServerOpts := []holoGRPC.CoreServerOption{
		holoGRPC.WithEventStore(eventStore),
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
		holoGRPC.WithStreamContributor(pluginManager),
		holoGRPC.WithAccessEngine(policyEngine),
		holoGRPC.WithSubscriber(subscriber),
		holoGRPC.WithHistoryReader(historyReader),
		holoGRPC.WithGameID(s.cfg.EventBus.GameID),
	}
	if s.cfg.StreamRegistry != nil {
		coreServerOpts = append(coreServerOpts, holoGRPC.WithStreamRegistry(s.cfg.StreamRegistry))
	}

	// 8a. Create focus.Coordinator.
	gameSettings := settings.NewGameSettings(&settings.SystemInfoAdapter{
		Store:       rawEventStore,
		NotFoundErr: store.ErrSystemInfoNotFound,
	})
	focusCoordOpts := []holoFocus.CoordinatorOption{
		holoFocus.WithSessionStore(sessionStore),
		holoFocus.WithEventStore(eventStore),
		holoFocus.WithKindPolicy(scenepolicy.New()),
		holoFocus.WithGameSettings(gameSettings),
		holoFocus.WithPlayerPreferences(holoFocus.NewPlayerPrefsAdapter(authPlayerRepo)),
		holoFocus.WithStreamContributor(&focusStreamContributorAdapter{pm: pluginManager}),
	}
	if s.cfg.StreamRegistry != nil {
		focusCoordOpts = append(focusCoordOpts,
			holoFocus.WithStreamSender(holoGRPC.NewStreamSenderAdapter(s.cfg.StreamRegistry)),
		)
	}
	focusCoord, focusErr := holoFocus.NewCoordinator(focusCoordOpts...)
	if focusErr != nil {
		return oops.Code("FOCUS_COORDINATOR_FAILED").Wrap(focusErr)
	}
	coreServerOpts = append(coreServerOpts, holoGRPC.WithFocusCoordinator(focusCoord))

	// 8b. Inject focus coordinator + event store into plugin hosts (late-binding).
	// The plugin subsystem started before gRPC, so these deps were not available
	// at host construction time. Binary plugins use them for JoinFocus/LeaveFocus/
	// PresentFocus/QueryStreamHistory RPCs; Lua plugins use them for holomush.*
	// hostfuncs.
	pluginManager.ConfigureFocusDeps(focusCoord, eventStore)

	coreServer := holoGRPC.NewCoreServer(engine, sessionStore, cmdDispatcher, cmdServices, coreServerOpts...)
	corev1.RegisterCoreServiceServer(s.grpcServer, coreServer)

	// 9. Create ContentService, register with gRPC.
	contentStore := content.NewPostgresStore(pool)
	contentv1.RegisterContentServiceServer(s.grpcServer, holoGRPC.NewContentServiceServer(contentStore))

	// 10. Create and start session reaper.
	reaperCtx, reaperCancel := context.WithCancel(context.Background())
	s.reaperCancel = reaperCancel

	s.sessionReaper = session.NewReaper(sessionStore, session.ReaperConfig{
		Interval: s.cfg.ReaperInterval,
		OnExpired: func(info *session.Info) {
			char := core.CharacterRef{
				ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID,
			}
			if dcErr := engine.HandleDisconnect(reaperCtx, char, "session expired"); dcErr != nil {
				slog.Warn("reaper: leave event failed",
					"session_id", info.ID,
					"error", dcErr,
				)
			}
			if endErr := engine.EndSession(reaperCtx, char, info.ID,
				core.SessionEndedCauseReaped,
				"Session expired due to inactivity."); endErr != nil {
				slog.Warn("reaper: session_ended event failed",
					"session_id", info.ID, "error", endErr)
			}
			if info.IsGuest {
				guestAuth.ReleaseGuest(info.CharacterName)
			}
		},
	})
	go s.sessionReaper.Run(reaperCtx)

	// 11. Create and start guest reaper.
	s.guestReaper = auth.NewGuestReaper(auth.GuestReaperConfig{
		Interval: 1 * time.Minute,
		IdleTTL:  10 * time.Minute,
	}, authPlayerRepo, authPlayerRepo)
	go s.guestReaper.Run(reaperCtx)

	// 12. Bind TCP listener.
	var err error
	s.listener, err = net.Listen("tcp", s.cfg.GRPCAddr)
	if err != nil {
		reaperCancel()
		return oops.Code("LISTEN_FAILED").With("operation", "listen").With("addr", s.cfg.GRPCAddr).Wrap(err)
	}

	// 13. Start grpcServer.Serve() in goroutine.
	slog.Info("gRPC server listening", "addr", s.cfg.GRPCAddr)
	go func() {
		if serveErr := s.grpcServer.Serve(s.listener); serveErr != nil {
			slog.Error("gRPC server stopped", "error", serveErr)
		}
	}()

	writerStarted = false // Success — Stop() now owns the writer lifecycle.
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
			slog.Warn("gRPC graceful shutdown timed out, forcing stop")
			s.grpcServer.Stop()
		}
	}
	if s.reaperCancel != nil {
		s.reaperCancel()
	}
	if s.eventWriter != nil {
		s.eventWriter.Close()
	}
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			slog.Debug("error closing gRPC listener", "error", err)
		}
	}
	return nil
}

// newCoreEventBusFanout returns a core.BusFanoutFunc that publishes each
// host-engine core.Event to the JetStream EventBus with the matching
// subject. Legacy colon-delimited streams (e.g. "location:01ABC") are
// translated to `events.<gameID>.<...>` so subscribers filter on the
// bus-native subject shape.
//
// F3 transitional only — F6 drops PG and the core engine writes to the
// bus directly.
func newCoreEventBusFanout(bus *eventbus.Subsystem) core.BusFanoutFunc {
	if bus == nil {
		return nil
	}
	publisher := bus.Publisher()
	if publisher == nil {
		return nil
	}
	return func(ctx context.Context, event core.Event) error {
		gameID := bus.GameID()
		if gameID == "" {
			gameID = "main"
		}
		natsSubject, err := subjectxlate.Legacy(event.Stream, gameID)
		if err != nil {
			return oops.With("stream", event.Stream).Wrap(err)
		}
		sub, err := eventbus.NewSubject(natsSubject)
		if err != nil {
			return oops.With("stream", event.Stream).Wrap(err)
		}
		typ, err := eventbus.NewType(string(event.Type))
		if err != nil {
			return oops.With("type", string(event.Type)).Wrap(err)
		}
		busEvent := eventbus.Event{
			ID:        event.ID,
			Subject:   sub,
			Type:      typ,
			Timestamp: event.Timestamp,
			Actor:     coreToBusActor(event.Actor),
			Payload:   event.Payload,
		}
		return oops.Wrap(publisher.Publish(ctx, busEvent))
	}
}

// coreToBusActor bridges the legacy core.Actor (ID is a string, sometimes
// a ULID and sometimes a plugin name) to the JetStream-side Actor (ID is
// a ULID; zero for anonymous/system).
func coreToBusActor(a core.Actor) eventbus.Actor {
	out := eventbus.Actor{Kind: coreActorKindToBus(a.Kind)}
	if a.ID != "" {
		if parsed, parseErr := ulid.Parse(a.ID); parseErr == nil {
			out.ID = parsed
		}
	}
	return out
}

func coreActorKindToBus(k core.ActorKind) eventbus.ActorKind {
	switch k {
	case core.ActorCharacter:
		return eventbus.ActorKindCharacter
	case core.ActorSystem:
		return eventbus.ActorKindSystem
	case core.ActorPlugin:
		return eventbus.ActorKindPlugin
	default:
		return eventbus.ActorKindUnknown
	}
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
// reader. js and pool may be nil if the subsystem has not started yet, but
// in production both are non-nil by the time Start() reaches this point.
// The wall clock is passed as time.Now — forbidigo bans time.Now only inside
// internal/eventbus/** and test/integration/eventbus_e2e/**; cmd/ is not in
// the banned path.
func newHistoryReader(js jetstream.JetStream, pool *pgxpool.Pool, cfg eventbus.Config) eventbus.HistoryReader {
	return history.NewReader(js, pool, cfg.StreamMaxAge, time.Now)
}
