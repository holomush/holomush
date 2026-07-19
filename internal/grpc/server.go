// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package grpc provides the gRPC server implementation for HoloMUSH Core.
package grpc

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/timestamppb"

	accessTypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/commandquery"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/presence"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// gRPC server resource limits. These bound the memory and stream
// concurrency a single client connection can consume.
//
// Size limits are a tradeoff: too small breaks legitimate use, too large
// lets a single request allocate arbitrary server memory. gRPC's built-in
// defaults are 4 MiB recv and ~math.MaxInt32 (~2 GiB) send, with unlimited
// concurrent streams per HTTP/2 connection — a single client could open
// unlimited Subscribe streams and exhaust server resources.
const (
	// MaxRecvMsgSize caps inbound unary/stream messages at 4 MiB. Matches
	// gRPC's built-in default but states the limit explicitly in-tree so
	// future gRPC default changes do not silently alter server behavior.
	MaxRecvMsgSize = 4 * 1024 * 1024

	// MaxSendMsgSize caps outbound messages at 16 MiB. Replay batches and
	// bootstrap history payloads can exceed the 4 MiB recv cap, so send is
	// allowed 4x recv while still bounded far below the unsafe ~2 GiB default.
	MaxSendMsgSize = 16 * 1024 * 1024

	// MaxConcurrentStreams caps concurrent HTTP/2 streams per connection at
	// 100. Subscribe, Presence, and command RPCs each consume a stream; a
	// well-behaved client needs ~3-5, so 100 leaves comfortable headroom
	// while preventing unbounded stream allocation from a single connection.
	MaxConcurrentStreams uint32 = 100
)

// SessionDefaults configures default values for new sessions.
type SessionDefaults struct {
	TTL        time.Duration
	MaxHistory int
	MaxReplay  int
}

// cleanupTimeout bounds best-effort teardown operations (connection removal,
// session reattach CAS) that must run on a fresh context so client disconnect
// does not abort cleanup.
const cleanupTimeout = 1 * time.Second

// tracer is the package-level OTel tracer for gRPC handler instrumentation.
// Sibling spans created here attach as children of the otelgrpc-installed
// server interceptor span, so each Subscribe RPC produces a full phase
// breakdown in Tempo / Grafana without a separate parent (holomush-87qu).
var tracer = otel.Tracer("holomush/internal/grpc")

// recordSpanError pairs RecordError with SetStatus(codes.Error, ...) so
// Tempo / Grafana queries that filter for `status.code=ERROR` surface
// the failing leg of a slow trace. RecordError alone leaves the span's
// status column as Unset; the exception event is attached but the span
// is not filterable. Mirrors plugins/core-scenes/observability.go's
// recordError pattern.
func recordSpanError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// replayCompleteFrame returns a SubscribeResponse containing the
// REPLAY_COMPLETE control signal and the server's Subscribe-attach
// moment as epoch-ms (holomush-iu8j). The client passes this value
// back as not_after_ms on subsequent WebQueryStreamHistory backfill
// calls so backfill returns only events that existed before the live
// stream attached — eliminating the connect-time replay/backfill race
// (fujt Fix B).
//
// attachMomentMs=0 is the legacy/no-op sentinel: clients reading 0
// MUST treat it as "no upper bound", preserving back-compat with
// servers that don't compute the attach moment.
func replayCompleteFrame(attachMomentMs int64) *corev1.SubscribeResponse {
	return &corev1.SubscribeResponse{
		Frame: &corev1.SubscribeResponse_Control{
			Control: &corev1.ControlFrame{
				Signal:         corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE,
				AttachMomentMs: attachMomentMs,
			},
		},
	}
}

// streamClosedFrame returns a SubscribeResponse containing the
// STREAM_CLOSED control signal with the given message.
func streamClosedFrame(msg string) *corev1.SubscribeResponse {
	return &corev1.SubscribeResponse{
		Frame: &corev1.SubscribeResponse_Control{
			Control: &corev1.ControlFrame{
				Signal:  corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED,
				Message: msg,
			},
		},
	}
}

// WorldQuerier provides read-only access to world model data for building
// location_state payloads during event streaming. Satisfied by *world.Service.
type WorldQuerier interface {
	GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*world.Location, error)
	GetExitsByLocation(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*world.Exit, error)
}

// SessionStreamContributor collects plugin-contributed stream names for a session.
type SessionStreamContributor interface {
	QuerySessionStreams(ctx context.Context, req plugins.SessionStreamsRequest) []string
}

// GameIDProvider returns the current game id for subject translation.
// F5 removes the need for translation; until then Subscribe must know the
// game id to route the session's filters at `events.<gameID>.<...>`.
type GameIDProvider func() string

// CoreServer implements the gRPC Core service.
type CoreServer struct {
	corev1.UnimplementedCoreServiceServer

	presence        *presence.Emitter
	sessionStore    session.Store
	publisher       eventbus.Publisher
	worldQuerier    WorldQuerier
	sessionDefaults SessionDefaults
	disconnectHooks []func(session.Info)
	dispatcher      *command.Dispatcher
	cmdServices     *command.Services

	// Auth services for two-phase login and account management.
	authService       AuthServiceProvider
	resetService      ResetServiceProvider
	characterService  CharacterServiceProvider
	playerSessionRepo auth.PlayerSessionRepository
	playerRepo        auth.PlayerRepository
	charRepo          auth.CharacterRepository
	guestService      *auth.GuestService

	// Binding repository for current-binding lookup in Subscribe /
	// QueryStreamHistory (Current). Character-creation binding is owned by the
	// genesis service (05-15), not the CoreServer.
	bindings     BindingRepo
	cryptoActive bool // true when KEK (RekeyManager) is wired; gates binding lookup in Subscribe / QueryStreamHistory

	// Plugin stream contribution and mid-session stream control.
	streamContributor SessionStreamContributor
	streamRegistry    *SessionStreamRegistry

	// focusCoordinator manages session focus memberships and replay policy.
	focusCoordinator focus.Coordinator

	// identityRegistry resolves plugin/system ULIDs to display names for the
	// gRPC wire (actorIDString). Set via WithIdentityRegistry; nil means
	// non-character actors fall back to ULID-string form.
	identityRegistry plugins.IdentityRegistry

	// accessEngine evaluates ABAC policies for stream read authorization (Layer 2).
	// Nil if ABAC is not configured (public stream reads will be denied).
	accessEngine accessTypes.AccessPolicyEngine

	// characterNameResolver resolves character display names by ID for
	// ListFocusPresence and other current-state RPCs (5b2j).
	characterNameResolver characterNameResolver

	// commandQuerier is the ABAC-filtered command enumeration for ListAvailableCommands
	// (2zjio). Nil until WithCommandQuerier is called; nil fails closed with PERMISSION_DENIED.
	commandQuerier *commandquery.Querier

	// subscriber opens per-session durable consumers against the JetStream
	// event bus. Post-F3 Subscribe delegates its live loop to subscribe
	// streams; nil subscriber causes Subscribe to error early. Wired via
	// WithSubscriber from the grpc subsystem.
	subscriber eventbus.Subscriber

	// historyReader serves QueryStreamHistory from the JetStream/PostgreSQL
	// tier crossover (F4). Required post-F7; returns INTERNAL when nil.
	historyReader eventbus.HistoryReader

	// gameID returns the current game id used to qualify domain-relative dot
	// stream references (e.g. "character.01ABC") into fully-qualified JetStream
	// subjects (e.g. "events.main.character.01ABC") via eventbus.Qualify.
	// Colon-style references are rejected, not translated. Defaults to "main".
	gameID GameIDProvider

	// newSessionID is used for generating session IDs. Can be overridden for testing.
	newSessionID func() ulid.ULID
	verbRegistry *core.VerbRegistry

	// sceneMute optionally suppresses the SCENE_ACTIVITY badge downgrade for a
	// non-focused member whose GLOBAL notify preference is off or who has muted
	// the scene. Nil (unwired) or any returned error fails OPEN — the badge is
	// delivered, since mute/notify-pref are preferences, not access control, and
	// the downgraded frame is already content-free (INV-SCENE-62). Set via
	// WithSceneMuteChecker.
	sceneMute SceneMuteChecker

	// subscribeHandler owns the subscribe/stream-delivery cluster, commandHandler
	// the command-execution cluster, lifecycleHandler the session-lifecycle
	// cluster, and queryHandler the current-state query cluster (ARCH-01). All
	// four are built at the END of NewCoreServer, after the option loop has
	// populated every collaborator they read — constructing them earlier would
	// capture zero values.
	//
	// Build ORDER matters: lifecycleHandler owns runDisconnectHooks and
	// recomputeSessionLiveness and queryHandler owns buildCharacterIdentity;
	// commandHandler and subscribeHandler consume those as function values, so
	// the two owners are constructed first.
	subscribeHandler *SubscribeHandler
	commandHandler   *CommandHandler
	lifecycleHandler *LifecycleHandler
	queryHandler     *QueryHandler
}

// CoreServerOption configures a CoreServer.
type CoreServerOption func(*CoreServer)

// WithSessionStore sets the session store for the server.
func WithSessionStore(store session.Store) CoreServerOption {
	return func(s *CoreServer) {
		s.sessionStore = store
	}
}

// WithSessionDefaults sets the default session parameters.
func WithSessionDefaults(defaults SessionDefaults) CoreServerOption {
	return func(s *CoreServer) {
		s.sessionDefaults = defaults
	}
}

// WithEventPublisher sets the event publisher for host-engine events (e.g.
// command_response/command_error) and the game-id qualification source used
// to turn emitCommandResponse's domain-relative stream ref into a fully
// qualified subject (eventbus.Qualify rejects a relative ref with an empty
// game id). gameID also feeds currentGameID/Subscribe subject qualification
// (D-02: one game-id source for the whole host) — it is the SAME field as
// WithGameID, not a duplicate; passing both is fine, the last one wins.
// Post-ARCH-04 this replaces the retired event-appender-backed seam.
func WithEventPublisher(pub eventbus.Publisher, gameID GameIDProvider) CoreServerOption {
	return func(s *CoreServer) {
		s.publisher = pub
		s.gameID = gameID
	}
}

// WithVerbRegistry sets the verb registry for event type validation.
func WithVerbRegistry(r *core.VerbRegistry) CoreServerOption {
	return func(s *CoreServer) {
		s.verbRegistry = r
	}
}

// WithWorldQuerier sets the world querier for building location_state payloads
// during event streaming (location-following).
func WithWorldQuerier(wq WorldQuerier) CoreServerOption {
	return func(s *CoreServer) {
		s.worldQuerier = wq
	}
}

// WithDisconnectHook registers a hook called after a session disconnects.
func WithDisconnectHook(hook func(session.Info)) CoreServerOption {
	return func(s *CoreServer) {
		s.disconnectHooks = append(s.disconnectHooks, hook)
	}
}

// WithStreamContributor sets the plugin stream contributor for the server.
func WithStreamContributor(c SessionStreamContributor) CoreServerOption {
	return func(s *CoreServer) { s.streamContributor = c }
}

// WithStreamRegistry sets the session stream registry for mid-session stream control.
func WithStreamRegistry(r *SessionStreamRegistry) CoreServerOption {
	return func(s *CoreServer) { s.streamRegistry = r }
}

// WithFocusCoordinator sets the focus coordinator for session focus management.
func WithFocusCoordinator(fc focus.Coordinator) CoreServerOption {
	return func(s *CoreServer) { s.focusCoordinator = fc }
}

// WithIdentityRegistry wires the plugin manager's IdentityRegistry into the
// server so that actorIDString can resolve plugin/system ULIDs to display
// names on the gRPC wire (Subscribe and QueryStreamHistory paths).
func WithIdentityRegistry(reg plugins.IdentityRegistry) CoreServerOption {
	return func(s *CoreServer) { s.identityRegistry = reg }
}

// WithAccessEngine sets the ABAC policy engine for stream read authorization.
func WithAccessEngine(engine accessTypes.AccessPolicyEngine) CoreServerOption {
	return func(s *CoreServer) { s.accessEngine = engine }
}

// WithCharacterNameResolver sets the character name resolver for ListFocusPresence (5b2j).
func WithCharacterNameResolver(r characterNameResolver) CoreServerOption {
	return func(s *CoreServer) { s.characterNameResolver = r }
}

// WithCommandQuerier sets the ABAC-filtered command querier for ListAvailableCommands (2zjio).
func WithCommandQuerier(q *commandquery.Querier) CoreServerOption {
	return func(s *CoreServer) { s.commandQuerier = q }
}

// WithSubscriber wires the JetStream EventBus subscriber into the Subscribe
// live loop. Required post-F3 — Subscribe returns NOT_CONFIGURED otherwise.
func WithSubscriber(sub eventbus.Subscriber) CoreServerOption {
	return func(s *CoreServer) { s.subscriber = sub }
}

// WithHistoryReader wires the JetStream/PostgreSQL tier crossover reader into
// QueryStreamHistory. Required post-F7 — QueryStreamHistory returns
// INTERNAL otherwise.
func WithHistoryReader(r eventbus.HistoryReader) CoreServerOption {
	return func(s *CoreServer) { s.historyReader = r }
}

// WithGameID injects the game-id provider used for subject translation.
// Unset defaults to "main" (eventbus.Config default).
func WithGameID(p GameIDProvider) CoreServerOption {
	return func(s *CoreServer) { s.gameID = p }
}

// WithSceneMuteChecker wires the mute/notify-pref suppression checker consulted
// at the SCENE_ACTIVITY badge downgrade. Nil (the default) fails open — every
// badge is delivered.
func WithSceneMuteChecker(c SceneMuteChecker) CoreServerOption {
	return func(s *CoreServer) { s.sceneMute = c }
}

// NewCoreServer creates a new Core gRPC server.
func NewCoreServer(pres *presence.Emitter, sessionStore session.Store, dispatcher *command.Dispatcher, cmdServices *command.Services, opts ...CoreServerOption) *CoreServer {
	s := &CoreServer{
		presence:     pres,
		sessionStore: sessionStore,
		dispatcher:   dispatcher,
		cmdServices:  cmdServices,
		newSessionID: core.NewULID,
	}

	for _, opt := range opts {
		opt(s)
	}

	if dispatcher == nil || cmdServices == nil {
		panic("grpc.NewCoreServer: dispatcher and cmdServices must not be nil")
	}

	// Built AFTER the option loop so every collaborator below is populated.
	s.buildHandlers()

	return s
}

// buildHandlers constructs the four extracted cluster units in dependency
// order. It MUST be called after the CoreServerOption loop — the deps structs
// snapshot collaborator values, so building earlier captures zero values.
//
// Order is load-bearing. lifecycleHandler owns runDisconnectHooks and
// recomputeSessionLiveness; queryHandler owns buildCharacterIdentity. Both are
// consumed as method values by commandHandler and subscribeHandler, so the two
// owners are constructed before their consumers.
//
// Because the deps are a SNAPSHOT, a caller that mutates a CoreServer
// collaborator field after construction must call buildHandlers again for the
// change to reach the units. Production never does this; several in-package
// test fixtures do, and they call this method rather than re-deriving the order.
func (s *CoreServer) buildHandlers() {
	s.lifecycleHandler = s.newLifecycleHandler()
	s.queryHandler = s.newQueryHandler()
	s.commandHandler = s.newCommandHandler()
	s.subscribeHandler = s.newSubscribeHandler()
}

// newQueryHandler snapshots the collaborators the current-state query cluster
// needs into a QueryHandler. It MUST be called after the CoreServerOption loop
// — calling it earlier captures zero values.
//
// Shared collaborators (focusCoordinator, streamContributor, identityRegistry)
// are passed as the SAME values that reach SubscribeHandler — no clone, no
// wrapper, no re-derivation. Each is a read-only interface, never shared
// mutable state, so there is nothing to coordinate. accessEngine is
// additionally still read by the auth cluster, which stays on the facade.
func (s *CoreServer) newQueryHandler() *QueryHandler {
	return NewQueryHandler(QueryDeps{
		SessionStore:          s.sessionStore,
		PlayerSessionRepo:     s.playerSessionRepo,
		AccessEngine:          s.accessEngine,
		HistoryReader:         s.historyReader,
		IdentityRegistry:      s.identityRegistry,
		CharacterNameResolver: s.characterNameResolver,
		CommandQuerier:        s.commandQuerier,
		FocusCoordinator:      s.focusCoordinator,
		StreamContributor:     s.streamContributor,
		Bindings:              s.bindings,
		CryptoActive:          s.cryptoActive,
		GameID:                s.currentGameID,
	})
}

// newLifecycleHandler snapshots the collaborators the session-lifecycle cluster
// needs into a LifecycleHandler. It MUST be called after the CoreServerOption
// loop — WithDisconnectHook appends to disconnectHooks, so building earlier
// would capture an empty (or partial) hook list.
func (s *CoreServer) newLifecycleHandler() *LifecycleHandler {
	return NewLifecycleHandler(LifecycleDeps{
		Presence:          s.presence,
		SessionStore:      s.sessionStore,
		PlayerSessionRepo: s.playerSessionRepo,
		DisconnectHooks:   s.disconnectHooks,
	})
}

// newCommandHandler snapshots the collaborators the command-execution cluster
// needs into a CommandHandler. It MUST be called after the CoreServerOption
// loop, and after newLifecycleHandler — RunDisconnectHooks is the lifecycle
// handler's own method value.
//
// presence.Emitter is passed as the SAME value into both handlers. It is an
// emitter — a collaborator, not shared mutable state owned by either unit — so
// there is nothing to coordinate and no instance to clone.
func (s *CoreServer) newCommandHandler() *CommandHandler {
	return NewCommandHandler(CommandDeps{
		Dispatcher:        s.dispatcher,
		CmdServices:       s.cmdServices,
		Presence:          s.presence,
		Publisher:         s.publisher,
		SessionStore:      s.sessionStore,
		PlayerSessionRepo: s.playerSessionRepo,
		GameID:            s.currentGameID,
		// Method value, not a parent pointer: the handler sees a plain function
		// type and never learns that CoreServer or LifecycleHandler exists (D-02).
		RunDisconnectHooks: s.lifecycleHandler.runDisconnectHooks,
	})
}

// newSubscribeHandler snapshots the collaborators the subscribe/stream-delivery
// cluster needs into a SubscribeHandler. It MUST be called after the
// CoreServerOption loop — calling it earlier captures zero values.
//
// Shared collaborators (focusCoordinator, streamContributor, identityRegistry,
// worldQuerier) are passed by value to both the facade and the unit. Each is a
// read-only interface or emitter, never shared mutable state, so there is
// nothing to coordinate.
func (s *CoreServer) newSubscribeHandler() *SubscribeHandler {
	return NewSubscribeHandler(SubscribeDeps{
		SessionStore:      s.sessionStore,
		PlayerSessionRepo: s.playerSessionRepo,
		Subscriber:        s.subscriber,
		FocusCoordinator:  s.focusCoordinator,
		StreamContributor: s.streamContributor,
		StreamRegistry:    s.streamRegistry,
		WorldQuerier:      s.worldQuerier,
		VerbRegistry:      s.verbRegistry,
		IdentityRegistry:  s.identityRegistry,
		SceneMute:         s.sceneMute,
		GameID:            s.currentGameID,
		// Method values, not a parent pointer: the handler sees plain function
		// types and never learns that CoreServer exists (D-02). BuildIdentity
		// now belongs to QueryHandler (it is shared with QueryStreamHistory);
		// RecomputeLiveness belongs to LifecycleHandler.
		BuildIdentity:     s.queryHandler.buildCharacterIdentity,
		RecomputeLiveness: s.lifecycleHandler.recomputeSessionLiveness,
	})
}

// currentGameID returns the configured game id, falling back to "main".
func (s *CoreServer) currentGameID() string {
	if s.gameID != nil {
		if g := s.gameID(); g != "" {
			return g
		}
	}
	return "main"
}

// HandleCommand processes a game command.
//
// The execution pipeline lives in CommandHandler (command_handler.go); this
// method exists only to keep CoreServer's corev1.CoreServiceServer method set
// fixed (D-03). All security commentary — notably the
// auth.ValidateSessionOwnership preamble (SECURITY bd-jv7z) — travelled with
// the body; see CommandHandler.HandleCommand.
func (s *CoreServer) HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
	return s.commandHandler.HandleCommand(ctx, req)
}

// actorIDString stringifies the bus-side actor for the gRPC wire.
// Resolution order:
//  1. Zero ULID → "" (preserves existing wire contract; gateway/web
//     clients don't see synthetic "00000000..." values).
//  2. NameByID lookup via IdentityRegistry → returns the registered
//     name for plugin and system sentinel ULIDs (uniform display).
//  3. Fallback: ULID-string form (characters / players whose ULIDs
//     are not in the IdentityRegistry — they live in the user store).
func actorIDString(a eventbus.Actor, reg plugins.IdentityRegistry) string {
	if a.ID.Compare(ulid.ULID{}) == 0 {
		return ""
	}
	if reg != nil {
		if name, ok := reg.NameByID(a.ID); ok {
			return name
		}
	}
	return a.ID.String()
}

// subscribeSessionNotFound builds the enumeration-safe SESSION_NOT_FOUND error
// for the Subscribe handler AND stamps it with a wire-classifiable gRPC status
// code. A bare oops error has no GRPCStatus method, so grpc-go would surface it
// to the client as codes.Unknown — indistinguishable from a transient
// core-down. Routing through authFailureToStatus maps the SESSION_NOT_FOUND
// oops code to codes.Unauthenticated on the wire, which the gateway client
// (translateSubscribeErr) decodes back to the SESSION_NOT_FOUND oops code so the
// telnet/web reconnect loops treat a reaped session as terminal (return to
// re-auth) rather than retrying for the full reconnect ceiling (rsoe6.11.1).
func subscribeSessionNotFound(sessionID string) error {
	err := oops.Code("SESSION_NOT_FOUND").With("session_id", sessionID).Errorf("session not found")
	if statusErr := authFailureToStatus(err); statusErr != nil {
		return statusErr
	}
	return err
}

// Subscribe opens a stream of events for the session.
//
// The stream logic lives in SubscribeHandler (subscribe_handler.go); this
// method exists only to keep CoreServer's corev1.CoreServiceServer method set
// fixed (D-03). All security commentary — notably the
// auth.ValidateSessionOwnership preamble (SECURITY bd-jv7z) — travelled with
// the body; see SubscribeHandler.Subscribe.
func (s *CoreServer) Subscribe(req *corev1.SubscribeRequest, stream grpc.ServerStreamingServer[corev1.SubscribeResponse]) error {
	return s.subscribeHandler.Subscribe(req, stream)
}

func filterSetToSlice(set map[eventbus.Subject]struct{}) []eventbus.Subject {
	out := make([]eventbus.Subject, 0, len(set))
	for sub := range set {
		out = append(out, sub)
	}
	return out
}

// Disconnect removes a connection and handles session lifecycle.
//
// The lifecycle logic lives in LifecycleHandler (lifecycle_handler.go); this
// method exists only to keep CoreServer's corev1.CoreServiceServer method set
// fixed (D-03). All security commentary — notably the
// auth.ValidateSessionOwnership preamble (SECURITY bd-jv7z) — travelled with
// the body; see LifecycleHandler.Disconnect.
func (s *CoreServer) Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	return s.lifecycleHandler.Disconnect(ctx, req)
}

// The six current-state query RPCs below live in QueryHandler
// (query_handler.go and the per-RPC files beside it); these methods exist only
// to keep CoreServer's corev1.CoreServiceServer method set fixed (D-03). All
// security commentary — the auth.ValidateSessionOwnership preambles (SECURITY
// bd-jv7z), the fail-closed ABAC defaults, and the enumeration-safe error
// collapses — travelled with the bodies; see the QueryHandler methods.

// GetCommandHistory retrieves command history for a session.
func (s *CoreServer) GetCommandHistory(ctx context.Context, req *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	return s.queryHandler.GetCommandHistory(ctx, req)
}

// QueryStreamHistory reads a page of stream history for a session.
func (s *CoreServer) QueryStreamHistory(ctx context.Context, req *corev1.QueryStreamHistoryRequest) (*corev1.QueryStreamHistoryResponse, error) {
	return s.queryHandler.QueryStreamHistory(ctx, req)
}

// ListFocusPresence returns the Active sessions at the caller's location.
func (s *CoreServer) ListFocusPresence(ctx context.Context, req *corev1.ListFocusPresenceRequest) (*corev1.ListFocusPresenceResponse, error) {
	return s.queryHandler.ListFocusPresence(ctx, req)
}

// ListSessionStreams returns the stream names the session is subscribed to.
func (s *CoreServer) ListSessionStreams(ctx context.Context, req *corev1.ListSessionStreamsRequest) (*corev1.ListSessionStreamsResponse, error) {
	return s.queryHandler.ListSessionStreams(ctx, req)
}

// ListAvailableCommands returns the commands the session's character may execute.
func (s *CoreServer) ListAvailableCommands(ctx context.Context, req *corev1.ListAvailableCommandsRequest) (*corev1.ListAvailableCommandsResponse, error) {
	return s.queryHandler.ListAvailableCommands(ctx, req)
}

// RefreshConnection bumps a connection's liveness lease.
func (s *CoreServer) RefreshConnection(ctx context.Context, req *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error) {
	return s.queryHandler.RefreshConnection(ctx, req)
}

// responseMeta creates a ResponseMeta with the request ID echoed.
func responseMeta(requestID string) *corev1.ResponseMeta {
	return &corev1.ResponseMeta{
		RequestId: requestID,
		Timestamp: timestamppb.Now(),
	}
}

// NewGRPCServer creates a new gRPC server with mTLS credentials.
// Applies explicit message size and concurrent-stream limits (see
// MaxRecvMsgSize, MaxSendMsgSize, MaxConcurrentStreams).
func NewGRPCServer(tlsConfig *tls.Config) *grpc.Server {
	creds := credentials.NewTLS(tlsConfig)
	return grpc.NewServer(
		grpc.Creds(creds),
		grpc.MaxRecvMsgSize(MaxRecvMsgSize),
		grpc.MaxSendMsgSize(MaxSendMsgSize),
		grpc.MaxConcurrentStreams(MaxConcurrentStreams),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
}

// NewGRPCServerInsecure creates a new gRPC server without TLS (for testing).
// Includes a permissive keepalive enforcement policy to prevent "too_many_pings"
// rejections during long-running integration tests. Applies the same message
// size and concurrent-stream limits as the TLS-enabled server so tests exercise
// the same resource bounds as production.
func NewGRPCServerInsecure() *grpc.Server {
	return grpc.NewServer(
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.MaxRecvMsgSize(MaxRecvMsgSize),
		grpc.MaxSendMsgSize(MaxSendMsgSize),
		grpc.MaxConcurrentStreams(MaxConcurrentStreams),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
}
