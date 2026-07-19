// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package grpc provides the gRPC server implementation for HoloMUSH Core.
package grpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime/debug"
	"strings"
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
	"github.com/holomush/holomush/internal/eventvocab"
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

	// subscribeHandler owns the subscribe/stream-delivery cluster (ARCH-01).
	// Built at the END of NewCoreServer, after the option loop has populated
	// every collaborator it reads — constructing it earlier would capture
	// zero values.
	subscribeHandler *SubscribeHandler
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
	s.subscribeHandler = s.newSubscribeHandler()

	return s
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
		// types and never learns that CoreServer exists (D-02). Both helpers
		// are shared with clusters that have not been extracted yet.
		BuildIdentity:     s.buildCharacterIdentity,
		RecomputeLiveness: s.recomputeSessionLiveness,
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
// SECURITY (bd-jv7z): Before executing, the caller's player_session_token is
// validated against the target session via auth.ValidateSessionOwnership.
// Any failure — missing/invalid token, expired token, unknown session, or
// ownership mismatch — returns the enumeration-safe "session not found"
// response. This closes the IDOR surface where one player could submit a
// command against another player's session id.
func (s *CoreServer) HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(
		ctx, "handle command request",
		"request_id", requestID,
		"session_id", req.SessionId,
		"command", req.Command,
	)

	info, err := auth.ValidateSessionOwnership(
		ctx,
		s.playerSessionRepo,
		s.sessionStore,
		req.GetPlayerSessionToken(),
		req.GetSessionId(),
	)
	if err != nil {
		slog.DebugContext(
			ctx, "session ownership validation failed",
			"request_id", requestID,
			"session_id", req.SessionId,
			"error", err,
		)
		return &corev1.HandleCommandResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   "session not found",
		}, nil
	}

	// Record command in session history (best-effort)
	if appendErr := s.sessionStore.AppendCommand(ctx, req.SessionId, req.Command, info.MaxHistory); appendErr != nil {
		slog.WarnContext(
			ctx, "command history append failed",
			"session_id", req.SessionId,
			"error", appendErr,
		)
	}

	// Parse and execute command
	if err := s.executeCommand(ctx, info, req.Command, req.GetConnectionId()); err != nil {
		slog.WarnContext(
			ctx, "command execution failed",
			"request_id", requestID,
			"session_id", req.SessionId,
			"command", req.Command,
			"error", err,
		)
		return &corev1.HandleCommandResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &corev1.HandleCommandResponse{
		Meta:    responseMeta(requestID),
		Success: true,
	}, nil
}

// executeCommand parses and executes a command via the unified dispatcher.
// Output is delivered via command_response events emitted to the character's
// personal stream.
func (s *CoreServer) executeCommand(ctx context.Context, info *session.Info, input, connectionIDStr string) error {
	return s.executeViaDispatcher(ctx, info, input, connectionIDStr)
}

// executeViaDispatcher uses the unified command.Dispatcher for command
// execution. Handler output written to the CommandExecution's io.Writer is
// captured in a buffer and emitted as a command_response event afterward.
// connectionIDStr is the originating gateway connection ULID string (Phase 5);
// empty string is accepted for non-gateway callers (parsed as zero ULID).
func (s *CoreServer) executeViaDispatcher(ctx context.Context, info *session.Info, input, connectionIDStr string) error {
	char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}

	sessionID, parseErr := ulid.Parse(info.ID)
	if parseErr != nil {
		return oops.Code("INVALID_SESSION_ID").
			With("session_id", info.ID).
			Wrap(parseErr)
	}

	// Parse connectionIDStr to ULID. Empty is allowed (legacy non-gateway
	// callers omit connection_id), but a NON-EMPTY value that fails to
	// parse is an explicit error from the caller — failing silently with
	// a zero ULID would silently bypass per-connection command semantics.
	// (CodeRabbit PR #4191)
	var connectionID ulid.ULID
	if connectionIDStr != "" {
		parsed, connParseErr := ulid.Parse(connectionIDStr)
		if connParseErr != nil {
			return oops.Code("INVALID_CONNECTION_ID").
				With("session_id", info.ID).
				With("connection_id", connectionIDStr).
				Wrap(connParseErr)
		}
		connectionID = parsed
	}

	var buf bytes.Buffer
	exec, err := command.NewCommandExecution(command.CommandExecutionConfig{
		CharacterID:   info.CharacterID,
		PlayerID:      info.PlayerID,
		LocationID:    info.LocationID,
		CharacterName: info.CharacterName,
		SessionID:     sessionID,
		ConnectionID:  connectionID, // Phase 5 (holomush-5rh.14 T19 follow-up)
		Output:        &buf,
		Services:      s.cmdServices,
	})
	if err != nil {
		return oops.Code("EXECUTION_SETUP_FAILED").Wrap(err)
	}

	dispatchErr := s.dispatcher.Dispatch(ctx, input, exec)

	// Emit any buffered output as a command_response event.
	if buf.Len() > 0 {
		isError := exec.ResponseIsError() || (dispatchErr != nil && !errors.Is(dispatchErr, command.ErrSessionEnded))
		if emitErr := s.emitCommandResponse(ctx, char, strings.TrimRight(buf.String(), "\n"), isError); emitErr != nil {
			return oops.Wrap(emitErr)
		}
	}

	// Quit/self-boot detection: handler signals intent, server does teardown.
	if errors.Is(dispatchErr, command.ErrSessionEnded) {
		if dcErr := s.presence.EmitLeave(ctx, char, "quit"); dcErr != nil {
			slog.WarnContext(ctx, "leave event failed", "error", dcErr)
		}
		if endErr := s.presence.EmitSessionEnded(ctx, char, info.ID,
			core.SessionEndedCauseQuit, "Goodbye!"); endErr != nil {
			// If we can't append session_ended, subscribers will not receive
			// STREAM_CLOSED. Retain the session row so the reaper can retry
			// (or at least so the row is not orphaned from its audit event).
			slog.WarnContext(
				ctx, "session_ended event failed — retaining session row for reap",
				"session_id", info.ID,
				"error", endErr,
			)
			s.runDisconnectHooks(ctx, *info)
			return nil
		}
		if delErr := s.sessionStore.Delete(ctx, info.ID); delErr != nil {
			slog.WarnContext(ctx, "session delete failed", "error", delErr)
		}
		s.runDisconnectHooks(ctx, *info)
		return nil
	}

	if dispatchErr != nil {
		// User-facing errors are delivered as command_response events, not
		// RPC-level failures. Emit the player message and return nil so
		// HandleCommand returns Success=true.
		if isUserFacingError(dispatchErr) {
			if buf.Len() == 0 {
				if emitErr := s.emitCommandResponse(ctx, char, command.PlayerMessage(dispatchErr), true); emitErr != nil {
					return oops.Wrap(emitErr)
				}
			}
			return nil
		}
		// Infrastructure errors propagate as RPC failures (Success=false).
		return oops.Wrap(dispatchErr)
	}

	// Process booted sessions: emit leave events and run disconnect hooks
	// for targets that were forcibly removed by admin boot.
	bootedSessions := exec.BootedSessions()
	for i := range bootedSessions {
		booted := &bootedSessions[i]

		// If CharacterRef is empty (e.g. plugin-originated boot that only
		// provided a session ID), look up the session to populate it.
		if booted.CharacterRef.ID.IsZero() && booted.SessionInfo.ID != "" {
			info, lookupErr := s.sessionStore.Get(ctx, booted.SessionInfo.ID)
			if lookupErr != nil {
				slog.WarnContext(ctx, "failed to look up booted session",
					"session_id", booted.SessionInfo.ID, "error", lookupErr)
				continue
			}
			booted.CharacterRef = core.CharacterRef{
				ID:         info.CharacterID,
				Name:       info.CharacterName,
				LocationID: info.LocationID,
			}
			booted.SessionInfo = *info
		}

		if dcErr := s.presence.EmitLeave(ctx, booted.CharacterRef, "booted"); dcErr != nil {
			slog.WarnContext(ctx, "boot leave event failed",
				"target_id", booted.CharacterRef.ID.String(),
				"error", dcErr)
		}
		if endErr := s.presence.EmitSessionEnded(ctx, booted.CharacterRef, booted.SessionInfo.ID,
			core.SessionEndedCauseKicked,
			"You have been disconnected by an administrator."); endErr != nil {
			// If we can't append session_ended, subscribers will not receive
			// STREAM_CLOSED. Retain the session row so the reaper can retry
			// (or at least so the row is not orphaned from its audit event).
			slog.WarnContext(ctx, "boot session_ended event failed — retaining session row for reap",
				"session_id", booted.SessionInfo.ID,
				"target_id", booted.CharacterRef.ID.String(),
				"error", endErr)
			s.runDisconnectHooks(ctx, booted.SessionInfo)
			continue
		}
		if delErr := s.sessionStore.Delete(ctx, booted.SessionInfo.ID); delErr != nil {
			slog.WarnContext(ctx, "boot session delete failed",
				"session_id", booted.SessionInfo.ID,
				"target_id", booted.CharacterRef.ID.String(),
				"error", delErr)
		}
		s.runDisconnectHooks(ctx, booted.SessionInfo)
	}

	return nil
}

// isUserFacingError returns true for errors that should be delivered to the
// player via a command_response event rather than as an RPC-level failure.
// Delegates to command.PlayerMessage to stay in sync with the command package's
// error classification — if PlayerMessage returns a specific message (not the
// generic fallback), the error is user-facing.
func isUserFacingError(err error) bool {
	msg := command.PlayerMessage(err)
	return msg != "Something went wrong. Try again."
}

// emitCommandResponse emits a command_response or command_error event to the
// character's personal stream. Returns an error if the event could not be emitted.
func (s *CoreServer) emitCommandResponse(ctx context.Context, char core.CharacterRef, text string, isError bool) error {
	payload, err := json.Marshal(eventvocab.CommandResponsePayload{
		Text: text,
	})
	if err != nil {
		slog.ErrorContext(
			ctx, "failed to marshal command_response payload",
			"character_id", char.ID.String(),
			"error", err,
		)
		return oops.Code("COMMAND_RESPONSE_MARSHAL_FAILED").Wrap(err)
	}

	eventType := eventvocab.EventTypeCommandResponse
	if isError {
		eventType = eventvocab.EventTypeCommandError
	}

	if s.publisher == nil {
		slog.DebugContext(ctx, "emitCommandResponse: publisher not configured, event not emitted")
		return nil
	}

	sub, err := qualifyStreamSubject(s.currentGameID(), world.CharacterStream(char.ID))
	if err != nil {
		return oops.Code("COMMAND_RESPONSE_EMIT_FAILED").With("character_id", char.ID.String()).Wrap(err)
	}
	typ, err := eventbus.NewType(string(eventType))
	if err != nil {
		return oops.Code("COMMAND_RESPONSE_EMIT_FAILED").With("character_id", char.ID.String()).Wrap(err)
	}
	event := eventbus.NewEvent(sub, typ, eventbus.Actor{
		Kind: eventbus.ActorKindSystem,
		ID:   core.SystemActorULID,
	}, payload)

	if err := s.publisher.Publish(ctx, event); err != nil {
		slog.WarnContext(
			ctx, "failed to publish command_response event",
			"character_id", char.ID.String(),
			"error", err,
		)
		return oops.Code("COMMAND_RESPONSE_EMIT_FAILED").Wrap(err)
	}
	return nil
}

// runDisconnectHooks runs all registered disconnect hooks with panic recovery.
func (s *CoreServer) runDisconnectHooks(ctx context.Context, info session.Info) {
	for _, hook := range s.disconnectHooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.ErrorContext(
						ctx, "disconnect hook panicked",
						"panic", r,
						"stack", string(debug.Stack()),
					)
				}
			}()
			hook(info)
		}()
	}
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
// When all terminal/telnet connections close but comms_hub remains, the
// character phases out (leave event) but the session stays active.
// When ALL connections close, non-guest sessions detach with TTL;
// guest sessions are deleted immediately.
//
// SECURITY (bd-jv7z): Before acting, the caller's player_session_token is
// validated against the target session via auth.ValidateSessionOwnership.
// Any failure — missing/invalid token, expired token, unknown session, or
// ownership mismatch — returns the enumeration-safe "session not found"
// response (success=false) with no state change. This closes the IDOR
// surface where one player could forcibly disconnect another player's
// session with just the session_id.
func (s *CoreServer) Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(
		ctx, "disconnect request",
		"request_id", requestID,
		"session_id", req.SessionId,
		"connection_id", req.ConnectionId,
	)

	// Validate session ownership before any state-changing work.
	// Enumeration-safe: every failure mode collapses to the same
	// "session not found" response.
	if _, err := auth.ValidateSessionOwnership(
		ctx,
		s.playerSessionRepo,
		s.sessionStore,
		req.GetPlayerSessionToken(),
		req.GetSessionId(),
	); err != nil {
		slog.DebugContext(
			ctx, "disconnect session ownership validation failed",
			"request_id", requestID,
			"session_id", req.SessionId,
			"error", err,
		)
		return &corev1.DisconnectResponse{
			Meta:    responseMeta(requestID),
			Success: false,
		}, nil
	}

	// Remove the named connection and read the session's remaining connection
	// counts. With a connection_id these run in ONE transaction under a
	// sessions-row lock (RemoveConnectionAndCount): concurrent disconnects on
	// the same session serialize, so exactly one observes totalCount==0 and
	// runs cleanup — closing the double-cleanup race (holomush-cizj). Done
	// BEFORE the Get below so a transient Get error still removes the
	// connection, matching the prior remove-then-lookup ordering.
	var totalCount, gridConns int
	var counted bool
	// mayCleanup gates the lifecycle transition below. The session-level
	// (no connection_id) path removes nothing and has no removal signal, so it
	// keeps the prior unconditional behavior.
	mayCleanup := true
	if connID, parseErr := ulid.Parse(req.ConnectionId); req.ConnectionId != "" && parseErr == nil {
		counts, removed, cErr := s.sessionStore.RemoveConnectionAndCount(ctx, req.SessionId, connID)
		if cErr != nil {
			slog.WarnContext(
				ctx, "failed to remove connection and count — skipping lifecycle transition",
				"request_id", requestID,
				"session_id", req.SessionId,
				"connection_id", req.ConnectionId,
				"error", cErr,
			)
			return &corev1.DisconnectResponse{Meta: responseMeta(requestID), Success: true}, nil
		}
		totalCount, gridConns = counts.Total, counts.Grid
		counted = true
		// Only the disconnect that actually removed the connection owns the
		// lifecycle cleanup; a duplicate disconnect for an already-removed
		// connection_id (removed=false) must not re-emit leave/session_ended
		// (holomush-cizj duplicate-disconnect guard).
		mayCleanup = removed
	}

	// Look up session — needed for the lifecycle branch below. If it is
	// already gone, Disconnect is idempotent success.
	info, err := s.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		//nolint:nilerr // intentional: session already gone, return success (idempotent)
		return &corev1.DisconnectResponse{
			Meta:    responseMeta(requestID),
			Success: true,
		}, nil
	}

	// No-connection_id path (e.g. a session-level disconnect) removes nothing,
	// so these separate counts carry no remove/count race.
	if !counted {
		totalCount, err = s.sessionStore.CountConnections(ctx, req.SessionId)
		if err != nil {
			slog.WarnContext(
				ctx, "failed to count connections — skipping lifecycle transition",
				"request_id", requestID,
				"session_id", req.SessionId,
				"error", err,
			)
			return &corev1.DisconnectResponse{Meta: responseMeta(requestID), Success: true}, nil
		}
		termCount, tErr := s.sessionStore.CountConnectionsByType(ctx, req.SessionId, "terminal")
		if tErr != nil {
			slog.WarnContext(ctx, "failed to count terminal connections", "request_id", requestID, "error", tErr)
		}
		telCount, tlErr := s.sessionStore.CountConnectionsByType(ctx, req.SessionId, "telnet")
		if tlErr != nil {
			slog.WarnContext(ctx, "failed to count telnet connections", "request_id", requestID, "error", tlErr)
		}
		gridConns = termCount + telCount
	}

	if mayCleanup && totalCount == 0 {
		// No connections at all
		if info.IsGuest {
			// Guests can't reconnect — delete immediately
			char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
			if err := s.presence.EmitLeave(ctx, char, "quit"); err != nil {
				slog.WarnContext(
					ctx, "leave event failed",
					"request_id", requestID,
					"error", err,
				)
			}

			if endErr := s.presence.EmitSessionEnded(ctx, char, info.ID,
				core.SessionEndedCauseGuestEnd, "Session ended."); endErr != nil {
				// If we can't append session_ended, subscribers will not receive
				// STREAM_CLOSED. Retain the session row so the reaper can retry
				// (or at least so the row is not orphaned from its audit event).
				slog.WarnContext(
					ctx, "guest session_ended event failed — retaining session row for reap",
					"request_id", requestID,
					"session_id", info.ID,
					"error", endErr,
				)
				s.runDisconnectHooks(ctx, *info)
				return &corev1.DisconnectResponse{
					Meta:    responseMeta(requestID),
					Success: true,
				}, nil
			}

			if err := s.sessionStore.Delete(ctx, req.SessionId); err != nil {
				slog.WarnContext(
					ctx, "failed to delete guest session",
					"request_id", requestID,
					"error", err,
				)
			}

			// Run disconnect hooks for guests
			s.runDisconnectHooks(ctx, *info)
		} else {
			// Non-guest: detach session with TTL instead of deleting.
			// Do NOT emit leave event — player may reconnect.
			// Reaper handles leave events when TTL expires.
			if err := s.recomputeSessionLiveness(ctx, req.SessionId); err != nil {
				slog.WarnContext(
					ctx, "failed to recompute session liveness on disconnect",
					"request_id", requestID,
					"session_id", req.SessionId,
					"error", err,
				)
			}
		}
	} else if mayCleanup && gridConns == 0 && info.GridPresent {
		// Only comms_hub connections remain — phase out from grid.
		// Emit the leave event (engine concern), then update grid presence via helper.
		char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
		if err := s.presence.EmitLeave(ctx, char, "phased out"); err != nil {
			slog.WarnContext(
				ctx, "phase-out leave event failed",
				"request_id", requestID,
				"session_id", req.SessionId,
				"error", err,
			)
		}
		if err := s.recomputeSessionLiveness(ctx, req.SessionId); err != nil {
			slog.WarnContext(
				ctx, "failed to recompute session liveness on phase-out",
				"request_id", requestID,
				"session_id", req.SessionId,
				"error", err,
			)
		}
	}

	slog.InfoContext(
		ctx, "session disconnected",
		"request_id", requestID,
		"session_id", req.SessionId,
		"character_id", info.CharacterID.String(),
	)

	return &corev1.DisconnectResponse{
		Meta:    responseMeta(requestID),
		Success: true,
	}, nil
}

// recomputeSessionLiveness inspects the current connection counts for
// sessionID and applies the canonical liveness transition:
//
//   - 0 total connections → detach (StatusDetached, grid_present=false,
//     expires_at = now + reattach TTL). TTL is taken from info.TTLSeconds;
//     defaults to 1800 s (30 min) when ≤0. Does NOT emit a leave event —
//     the caller is responsible for any engine-level side effects.
//   - >0 total connections → ensure status=active; set grid_present=true
//     when at least one terminal or telnet connection exists, false otherwise.
//
// This helper is the single place that owns the connection-count →
// session-liveness mapping. Disconnect, the lease sweep (Task 5), and
// AddConnection all call it so the rule stays DRY.
func (s *CoreServer) recomputeSessionLiveness(ctx context.Context, sessionID string) error {
	info, err := s.sessionStore.Get(ctx, sessionID)
	if err != nil {
		return oops.With("session_id", sessionID).Wrap(err)
	}

	totalCount, err := s.sessionStore.CountConnections(ctx, sessionID)
	if err != nil {
		return oops.With("session_id", sessionID).Wrap(err)
	}

	if totalCount == 0 {
		// Detach: set status + expires_at; clear grid presence.
		now := time.Now()
		ttlSeconds := info.TTLSeconds
		if ttlSeconds <= 0 {
			ttlSeconds = 1800 // default 30 minutes
		}
		expiresAt := now.Add(time.Duration(ttlSeconds) * time.Second)
		err = s.sessionStore.UpdateStatus(ctx, sessionID,
			session.StatusDetached, &now, &expiresAt)
		if err != nil {
			return oops.With("session_id", sessionID).Wrap(err)
		}
		if info.GridPresent {
			if err = s.sessionStore.UpdateGridPresent(ctx, sessionID, false); err != nil {
				return oops.With("session_id", sessionID).Wrap(err)
			}
		}
		return nil
	}

	// >0 connections: ensure status=active + correct grid presence.
	// This matters when the lease sweep (Task 5) or a future AddConnection
	// caller invokes recomputeSessionLiveness on a session that is still
	// StatusDetached because a prior recompute was interrupted or missed.
	if info.Status != session.StatusActive {
		if err = s.sessionStore.UpdateStatus(ctx, sessionID,
			session.StatusActive, nil, nil); err != nil {
			return oops.With("session_id", sessionID).Wrap(err)
		}
	}

	termCount, err := s.sessionStore.CountConnectionsByType(ctx, sessionID, "terminal")
	if err != nil {
		return oops.With("session_id", sessionID).Wrap(err)
	}
	telCount, err := s.sessionStore.CountConnectionsByType(ctx, sessionID, "telnet")
	if err != nil {
		return oops.With("session_id", sessionID).Wrap(err)
	}
	gridConns := termCount + telCount
	wantGrid := gridConns > 0

	if info.GridPresent != wantGrid {
		err = s.sessionStore.UpdateGridPresent(ctx, sessionID, wantGrid)
		if err != nil {
			return oops.With("session_id", sessionID).Wrap(err)
		}
	}
	return nil
}

// GetCommandHistory retrieves command history for a session.
//
// SECURITY (bd-jv7z): Before returning history, the caller's
// player_session_token is validated against the target session via
// auth.ValidateSessionOwnership. Any failure — missing/invalid token,
// expired token, unknown session, or ownership mismatch — returns the
// enumeration-safe "session not found" response (success=false) with
// an empty command list. This closes the IDOR surface where one player
// could read another player's typed command history with just the
// session_id.
func (s *CoreServer) GetCommandHistory(ctx context.Context, req *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	// Validate session ownership before any store read.
	// Enumeration-safe: every failure mode collapses to the same
	// "session not found" response with no commands.
	if _, err := auth.ValidateSessionOwnership(
		ctx,
		s.playerSessionRepo,
		s.sessionStore,
		req.GetPlayerSessionToken(),
		req.GetSessionId(),
	); err != nil {
		slog.DebugContext(
			ctx, "get_command_history session ownership validation failed",
			"request_id", requestID,
			"session_id", req.GetSessionId(),
			"error", err,
		)
		return &corev1.GetCommandHistoryResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   "session not found",
		}, nil
	}

	sessionID := req.GetSessionId()
	history, err := s.sessionStore.GetCommandHistory(ctx, sessionID)
	if err != nil {
		return nil, oops.Code("COMMAND_HISTORY_FAILED").With("session_id", sessionID).Wrap(err)
	}

	return &corev1.GetCommandHistoryResponse{
		Meta:     responseMeta(requestID),
		Success:  true,
		Commands: history,
	}, nil
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
