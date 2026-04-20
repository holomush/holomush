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

	abacsetup "github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/auth"
	authsetup "github.com/holomush/holomush/internal/auth/setup"
	bootstrapsetup "github.com/holomush/holomush/internal/bootstrap/setup"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/content"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/history"
	"github.com/holomush/holomush/internal/eventbus/subjectxlate"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	holoFocus "github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/grpc/focus/scenepolicy"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/naming"
	plugins "github.com/holomush/holomush/internal/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
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
	// F7: EventWriter and PG events table are gone. Append goes directly to
	// JetStream via busEventAppender. rawEventStore is kept only for
	// system-info / game-ID (GetSystemInfo / InitGameID) used by GameSettings.
	rawEventStore := s.cfg.DB.EventStore()
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

	// F7: build the JetStream-backed EventAppender. All host-engine Append
	// calls publish directly to JetStream; the PG events table is gone.
	eventStore := &busEventAppender{
		publisher: publisher,
		bus:       s.cfg.EventBus,
	}

	// 1. Create core engine from event store.
	engine := core.NewEngine(eventStore)

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
	//
	// F5: also wire the subject-ownership map + plugin history router so
	// QueryHistory calls for plugin-owned subjects route to the plugin's
	// PluginAuditService.QueryHistory RPC instead of the host tiers.
	js := s.cfg.EventBus.JS()
	owners := historyOwnersFromPlugins(pluginManager)
	router := audit.NewPluginHistoryRouter(pluginAuditClientProvider{mgr: pluginManager})
	historyReader := newHistoryReader(js, pool, s.cfg.EventBus.Config(), owners, router)

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
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			slog.Debug("error closing gRPC listener", "error", err)
		}
	}
	return nil
}

// busEventAppender implements core.EventAppender by publishing directly to
// JetStream. F7 removes the PG events table and the EventWriter; all host-
// engine Append calls go straight to the bus.
type busEventAppender struct {
	publisher eventbus.Publisher
	bus       *eventbus.Subsystem
}

var _ core.EventAppender = (*busEventAppender)(nil)

// Append translates a core.Event to an eventbus.Event and publishes it to
// JetStream. Legacy colon-delimited streams (e.g. "location:01ABC") are
// mapped to `events.<gameID>.<...>` via subjectxlate.Legacy.
func (b *busEventAppender) Append(ctx context.Context, event core.Event) error {
	gameID := b.bus.GameID()
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
	return oops.Wrap(b.publisher.Publish(ctx, busEvent))
}

// coreToBusActor bridges the legacy core.Actor (ID is a string, sometimes
// a ULID and sometimes a plugin name) to the JetStream-side Actor (ID is
// a ULID; zero for anonymous/system). Non-ULID values (e.g. plugin names)
// are preserved on Actor.LegacyID so plugin-authored host events retain
// their actor identity across JetStream/audit/history.
func coreToBusActor(a core.Actor) eventbus.Actor {
	out := eventbus.Actor{Kind: coreActorKindToBus(a.Kind)}
	if a.ID != "" {
		if parsed, parseErr := ulid.Parse(a.ID); parseErr == nil {
			out.ID = parsed
		} else {
			out.LegacyID = a.ID
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

// busHistoryReaderAdapter bridges eventbus.HistoryReader (QueryHistory) to
// plugins.HistoryReader (ReplayTail). Used by the plugin subsystem for
// QueryStreamHistory RPCs and Lua holomush.query_stream_history hostfuncs.
type busHistoryReaderAdapter struct {
	reader  eventbus.HistoryReader
	gameID  func() string
}

var _ plugins.HistoryReader = (*busHistoryReaderAdapter)(nil)

// ReplayTail satisfies plugins.HistoryReader. It fetches count most-recent
// events on stream (optionally filtered by notBefore and exclusive beforeID),
// returning them in ascending ULID order (oldest→newest).
func (a *busHistoryReaderAdapter) ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]core.Event, error) {
	if count <= 0 {
		return nil, nil
	}
	gameID := a.gameID()
	if gameID == "" {
		gameID = "main"
	}
	natsSubject, err := subjectxlate.Legacy(stream, gameID)
	if err != nil {
		return nil, oops.With("stream", stream).Wrap(err)
	}
	sub, err := eventbus.NewSubject(natsSubject)
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
		q.Before = beforeID
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

	// Reverse to ascending (oldest→newest) and translate to core.Event.
	result := make([]core.Event, len(collected))
	for i := range collected {
		j := len(collected) - 1 - i
		streamName := subjectxlate.ToLegacy(string(collected[i].Subject), gameID)
		result[j] = busEventToCoreEvent(collected[i], streamName)
	}
	return result, nil
}

// busEventToCoreEvent translates an eventbus.Event to a core.Event for plugin
// consumption. The Actor ID is a ULID string when present; otherwise the
// LegacyID (e.g. a plugin name) is preserved so plugin-authored events keep
// their actor identity across the round-trip.
func busEventToCoreEvent(e eventbus.Event, stream string) core.Event {
	actorID := ""
	switch {
	case e.Actor.ID != (ulid.ULID{}):
		actorID = e.Actor.ID.String()
	case e.Actor.LegacyID != "":
		actorID = e.Actor.LegacyID
	}
	return core.Event{ //nolint:gocritic // translation path: preserves existing ID+Timestamp from JetStream; core.NewEvent() would clobber them
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
func newHistoryReader(
	js jetstream.JetStream,
	pool *pgxpool.Pool,
	cfg eventbus.Config,
	owners *audit.OwnerMap,
	router history.PluginHistoryRouter,
) eventbus.HistoryReader {
	opts := []history.Option{}
	if owners != nil {
		opts = append(opts, history.WithOwners(owners))
	}
	if router != nil {
		opts = append(opts, history.WithPluginRouter(router))
	}
	return history.NewReader(js, pool, cfg.StreamMaxAge, time.Now, opts...)
}

// historyOwnersFromPlugins aggregates plugin-declared audit subjects into an
// audit.OwnerMap. A nil return means no plugins declared ownership, which
// the reader treats as "host owns everything" — the Phase A default.
// Returns nil on construction failure and logs the reason; a broken
// manifest MUST NOT block gRPC start because plugin-owned subjects will
// still fall through to host storage (which is the safe degradation).
func historyOwnersFromPlugins(mgr *plugins.Manager) *audit.OwnerMap {
	if mgr == nil {
		return nil
	}
	decls := mgr.AuditSubjects()
	if len(decls) == 0 {
		return nil
	}
	owners := make([]audit.SubjectOwner, 0, len(decls))
	for _, d := range decls {
		owners = append(owners, audit.SubjectOwner{
			PluginName: d.PluginName,
			Pattern:    d.Subject,
		})
	}
	m, err := audit.NewOwnerMap(owners)
	if err != nil {
		slog.Error("history OwnerMap construction failed; plugin-owned subjects will route via host fallback",
			"error", err)
		return nil
	}
	return m
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
