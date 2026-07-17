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
	"io"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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
	eventStore      core.EventAppender
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

// WithEventStore sets the event appender for host-engine events (e.g.
// command_response). Post-F7 this is wired to the JetStream bus.
func WithEventStore(store core.EventAppender) CoreServerOption {
	return func(s *CoreServer) {
		s.eventStore = store
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

	return s
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
	event := core.NewEvent(world.CharacterStream(char.ID), eventType, core.Actor{
		Kind: core.ActorSystem,
		ID:   core.ActorSystemID,
	}, payload)

	if s.eventStore == nil {
		slog.DebugContext(ctx, "emitCommandResponse: eventStore not configured, event not emitted")
		return nil
	}

	if err := s.eventStore.Append(ctx, event); err != nil {
		slog.WarnContext(
			ctx, "failed to append command_response event",
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

// toProtoSubscribeResponse maps an eventbus.Event and its MetadataOnly flag
// to a gRPC SubscribeResponse frame. metadataOnly comes from
// Delivery.MetadataOnly() and is stamped onto EventFrame.metadata_only
// (Phase 3b grounding doc Decision 4). ev.NoPlaintextReason is stamped into
// EventFrame.no_plaintext_reason (holomush-ojw1.6). The stream field carries
// the fully-qualified JetStream subject (e.g. "events.main.character.01ABC")
// as-is: producers and clients exchange domain-relative dot references and the
// server qualifies on the way in (holomush-rops). The web client dedups history
// by event ID and renders live frames without matching by stream, so the
// relative-send / qualified-receive asymmetry is intentional.
func (s *CoreServer) toProtoSubscribeResponse(ev eventbus.Event, metadataOnly bool) *corev1.SubscribeResponse {
	return &corev1.SubscribeResponse{
		Frame: &corev1.SubscribeResponse_Event{
			Event: &corev1.EventFrame{
				Id:                ev.ID.String(),
				Stream:            string(ev.Subject),
				Type:              string(ev.Type),
				Timestamp:         timestamppb.New(ev.Timestamp),
				ActorType:         ev.Actor.Kind.String(),
				ActorId:           actorIDString(ev.Actor, s.identityRegistry),
				Payload:           ev.Payload,
				Cursor:            encodeEventCursor(ev),
				Rendering:         eventbus.RenderingToProto(ev.Rendering),
				MetadataOnly:      metadataOnly,
				NoPlaintextReason: corev1.NoPlaintextReason(ev.NoPlaintextReason),
			},
		},
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

// computeInitialFilters qualifies a focus restore plan's stream list into
// bus-side subjects. Each domain-relative dot reference is qualified to an
// events.<game>. subject (via eventbus.Qualify); subjects that already look
// JetStream-native pass through unchanged. References that fail to qualify
// (e.g. colon-style or malformed) are logged and dropped so a single bad
// stream can't brick the Subscribe handshake.
func (s *CoreServer) computeInitialFilters(ctx context.Context, plan focus.RestorePlan) []eventbus.Subject {
	gameID := s.currentGameID()
	out := make([]eventbus.Subject, 0, len(plan.Streams))
	for _, sm := range plan.Streams {
		sub, err := s.toSubject(gameID, sm.Stream)
		if err != nil {
			slog.WarnContext(ctx, "skipping stream with invalid subject translation",
				"stream", sm.Stream, "error", err)
			continue
		}
		out = append(out, sub)
	}
	return out
}

// toSubject qualifies a domain-relative stream reference (e.g. "location.01ABC")
// against the game id via eventbus.Qualify, which validates the result against
// eventbus.NewSubject. A colon-style reference no longer qualifies and is
// rejected here (holomush-rops).
func (s *CoreServer) toSubject(gameID, streamName string) (eventbus.Subject, error) {
	sub, err := eventbus.Qualify(gameID, streamName)
	if err != nil {
		return "", oops.With("stream", streamName).Wrap(err)
	}
	return sub, nil
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
// SECURITY (bd-jv7z): Before opening the stream, the caller's
// player_session_token is validated against the target session via
// auth.ValidateSessionOwnership. Any failure — missing/invalid token,
// expired token, unknown session, or ownership mismatch — collapses to
// the enumeration-safe SESSION_NOT_FOUND error. This closes the IDOR
// surface where one player could subscribe to another player's event
// stream. Ongoing revocation (session deletion, cap eviction)
// propagates via the existing control-frame mechanism — validation runs
// once at stream open.
//
// Post-F3 live-loop architecture: the handler opens a durable JetStream
// session consumer via eventbus.Subscriber.OpenSession and pumps every
// delivery through toProtoSubscribeResponse → grpcStream.Send → Ack.
// Cursor resume is JS-native (durable consumer acked-seq). Mid-session
// filter changes use SessionStream.SetFilters which JS applies atomically
// while preserving the cursor.
func (s *CoreServer) Subscribe(req *corev1.SubscribeRequest, stream grpc.ServerStreamingServer[corev1.SubscribeResponse]) error {
	ctx := stream.Context()
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	// Phase-level spans on the Subscribe handler (holomush-87qu). The
	// otelgrpc interceptor's RPC span is the parent; each phase below
	// is a sibling child span scoped to a single piece of pre-replay
	// work. The dominator of the 8-10s "syncing" window observed in
	// holomush-87qu is unknown by code reading alone — these spans let
	// Tempo / Grafana show which phase carries the time.
	subscribeSpan := trace.SpanFromContext(ctx)
	subscribeSpan.SetAttributes(attribute.String("subscribe.session_id", req.SessionId))
	if requestID != "" {
		// Skip emitting the attribute on Meta-less requests so a Tempo
		// search by `subscribe.request_id=""` doesn't match every empty
		// caller.
		subscribeSpan.SetAttributes(attribute.String("subscribe.request_id", requestID))
	}

	slog.DebugContext(
		ctx, "subscribe request",
		"request_id", requestID,
		"session_id", req.SessionId,
	)

	if s.subscriber == nil {
		return oops.Code("NOT_CONFIGURED").Errorf("event bus subscriber not configured")
	}

	// Validate session ownership before any other work. Enumeration-safe:
	// every failure mode collapses to the same SESSION_NOT_FOUND error.
	validateCtx, validateSpan := tracer.Start(ctx, "subscribe.validate_ownership")
	if _, err := auth.ValidateSessionOwnership(
		validateCtx,
		s.playerSessionRepo,
		s.sessionStore,
		req.GetPlayerSessionToken(),
		req.GetSessionId(),
	); err != nil {
		recordSpanError(validateSpan, err)
		validateSpan.End()
		slog.DebugContext(
			ctx, "subscribe session ownership validation failed",
			"request_id", requestID,
			"session_id", req.SessionId,
			"error", err,
		)
		return subscribeSessionNotFound(req.SessionId)
	}
	validateSpan.End()

	getCtx, getSpan := tracer.Start(ctx, "subscribe.session_get")
	info, err := s.sessionStore.Get(getCtx, req.SessionId)
	if err != nil {
		recordSpanError(getSpan, err)
		getSpan.End()
		return subscribeSessionNotFound(req.SessionId)
	}
	getSpan.End()

	// Control channel + stream-registry registration MUST happen before
	// RestoreFocus / OpenSession (CodeRabbit PR #4191): a focus-change RPC
	// firing in that ~130-line window would persist successfully but its
	// SendToConnection would fail with CONNECTION_NOT_REGISTERED, leaving
	// the new subscriber on stale filters. The ctrlCh buffer (16) absorbs
	// any inbound updates until runSubscribeLoop drains them after
	// OpenSession completes.
	ctrlCh := make(chan sessionStreamUpdate, 16)
	if s.streamRegistry != nil {
		s.streamRegistry.Register(info.ID, ctrlCh)
		defer s.streamRegistry.Deregister(info.ID, ctrlCh)
	}

	// Connection registration (bd-j2xj). Only fires when the caller supplies
	// a connection_id + client_type. Gate removal uses a fresh context so
	// stream context cancellation does not abort cleanup.
	if req.GetConnectionId() != "" {
		connID, parseErr := ulid.Parse(req.GetConnectionId())
		if parseErr != nil || req.GetClientType() == "" {
			return oops.Code("SUBSCRIBE_INVALID_CONNECTION").
				With("session_id", req.GetSessionId()).
				Errorf("subscribe: connection_id must be a valid ULID and client_type must be set")
		}
		conn := &session.Connection{
			ID:          connID,
			SessionID:   req.GetSessionId(),
			ClientType:  req.GetClientType(),
			Streams:     []string{},
			ConnectedAt: time.Now(),
		}
		addCtx, addSpan := tracer.Start(ctx, "subscribe.add_connection",
			trace.WithAttributes(attribute.String("connection.id", connID.String())))
		if addErr := s.sessionStore.AddConnection(addCtx, conn); addErr != nil {
			recordSpanError(addSpan, addErr)
			addSpan.End()
			return oops.Code("SUBSCRIBE_ADD_CONNECTION_FAILED").
				With("session_id", req.GetSessionId()).
				With("connection_id", req.GetConnectionId()).
				Wrap(addErr)
		}
		addSpan.End()
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
			defer cancel()
			if rmErr := s.sessionStore.RemoveConnection(cleanupCtx, connID); rmErr != nil {
				slog.WarnContext(
					ctx, "subscribe: failed to remove connection on stream close",
					"connection_id", connID.String(),
					"session_id", req.GetSessionId(),
					"error", rmErr,
				)
			}
		}()
		// Per-Connection routing (INV-SCENE-24): register in the per-Connection
		// map immediately after AddConnection so T14-T18 coordinators can
		// route to exactly one connection. The session-wide Register above
		// has already armed ctrlCh.
		if s.streamRegistry != nil {
			s.streamRegistry.RegisterConnection(info.ID, connID, ctrlCh)
			defer s.streamRegistry.DeregisterConnection(info.ID, connID, ctrlCh)
		}

		// RestoreConnectionFocus (D-08): a reconnecting telnet member whose
		// session PresentingFocus was a scene has its fresh per-connection
		// FocusKey repopulated so it receives live scene content rather than a
		// badge downgrade. Gated on PresentingFocus != nil (Assumption A2 /
		// Pitfall 5): an unconditional restore would clobber a web tab's
		// per-tab focus, and PresentingFocus is the telnet single-pane
		// reconnect signal. The primitive itself validates FocusMemberships
		// (INV-SCENE-18) and grid-falls-back when membership was revoked mid-
		// disconnect — which also blocks a swapped-in character (D-09) from
		// inheriting a prior character's scene focus. Best-effort: a restore
		// failure is logged but MUST NOT fail the Subscribe.
		if s.focusCoordinator != nil && info.PresentingFocus != nil {
			rcfCtx, rcfSpan := tracer.Start(ctx, "subscribe.restore_connection_focus",
				trace.WithAttributes(attribute.String("connection.id", connID.String())))
			if rcfErr := s.focusCoordinator.RestoreConnectionFocus(rcfCtx, req.GetSessionId(), connID); rcfErr != nil {
				recordSpanError(rcfSpan, rcfErr)
				slog.WarnContext(
					ctx, "subscribe: restore connection focus failed (non-fatal)",
					"session_id", req.GetSessionId(),
					"connection_id", connID.String(),
					"error", rcfErr,
				)
			}
			rcfSpan.End()
		}
	}

	// Reattach if detached.
	if info.Status == session.StatusDetached {
		reattachCtx, reattachSpan := tracer.Start(ctx, "subscribe.reattach_cas")
		ok, casErr := s.sessionStore.ReattachCAS(reattachCtx, req.SessionId)
		if casErr != nil {
			recordSpanError(reattachSpan, casErr)
			reattachSpan.End()
			return oops.Code("SESSION_REATTACH_FAILED").With("session_id", req.SessionId).Wrap(casErr)
		}
		reattachSpan.SetAttributes(attribute.Bool("reattach.won_cas", ok))
		reattachSpan.End()
		if !ok {
			return oops.Code("SESSION_REATTACH_LOST").
				With("session_id", req.SessionId).
				Errorf("session reattach CAS lost — another handler won the race")
		}
		// Transport reattach: the session row was held open across a
		// transport disconnect. The session is the SAME continuing
		// session — its LocationArrivedAt MUST NOT be reset. Per spec §2
		// (post-2026-05-17 amendment): only session-create and
		// character-move advance the floor. See SelectCharacter reattach
		// branch for the matching commentary.
	}

	// Recompute grid_present now that a connection exists (and after
	// ReattachCAS has already transitioned a formerly-detached session to
	// active). This restores grid_present=true when a reattaching session
	// was left at grid_present=false after the prior Disconnect (I-LIVE-3).
	// Placed here — after ReattachCAS — so that recomputeSessionLiveness
	// reads status=active and only needs to fix grid_present, rather than
	// racing with ReattachCAS over the detached→active transition.
	if req.GetConnectionId() != "" {
		if liveErr := s.recomputeSessionLiveness(ctx, req.GetSessionId()); liveErr != nil {
			slog.WarnContext(ctx, "subscribe: failed to recompute session liveness after add_connection",
				"session_id", req.GetSessionId(), "error", liveErr)
		}
	}

	// RestoreFocus — produces the full stream list and replay modes.
	var plan focus.RestorePlan
	if s.focusCoordinator != nil {
		restoreCtx, restoreSpan := tracer.Start(ctx, "subscribe.restore_focus")
		p, planErr := s.focusCoordinator.RestoreFocus(restoreCtx, req.SessionId)
		if planErr != nil {
			recordSpanError(restoreSpan, planErr)
			slog.WarnContext(ctx, "restoreFocus failed, falling back to empty plan",
				"session_id", req.SessionId, "error", planErr)
		}
		restoreSpan.SetAttributes(attribute.Int("restore.stream_count", len(p.Streams)))
		restoreSpan.End()
		plan = p
	}

	// Ensure ambient streams are present even without a coordinator.
	if len(plan.Streams) == 0 {
		plan.Streams = append(
			plan.Streams,
			focus.StreamWithMode{Stream: world.CharacterStream(info.CharacterID), Mode: focus.ReplayModeFromCursor},
		)
		if !info.LocationID.IsZero() {
			plan.Streams = append(
				plan.Streams,
				focus.StreamWithMode{Stream: world.LocationStream(info.LocationID), Mode: focus.ReplayModeFromCursor},
			)
		}
		if s.streamContributor != nil {
			playerID := ""
			if !info.PlayerID.IsZero() {
				playerID = info.PlayerID.String()
			}
			pluginStreams := s.streamContributor.QuerySessionStreams(ctx, plugins.SessionStreamsRequest{
				CharacterID: info.CharacterID.String(),
				PlayerID:    playerID,
				SessionID:   info.ID,
			})
			for _, ps := range pluginStreams {
				plan.Streams = append(
					plan.Streams,
					focus.StreamWithMode{Stream: ps, Mode: focus.ReplayModeFromCursor},
				)
			}
		}
	}

	// Active filter set — mutated by location-following and mid-session
	// ctrl updates. Both paths funnel into SessionStream.SetFilters.
	filters := s.computeInitialFilters(ctx, plan)

	// Build the typed authenticated identity for this session's subscriber.
	// Decision 2 (Phase 3b grounding doc): the gRPC handler is the
	// authentication boundary; identity is derived solely from the
	// server-side session record — never from client-supplied fields.
	sessionIdentity, identityErr := s.buildCharacterIdentity(ctx, info.PlayerID.String(), info.CharacterID.String())
	if identityErr != nil {
		return oops.Code("SUBSCRIBE_BINDING_LOOKUP_FAILED").Wrap(identityErr)
	}

	// Compute minFloor across all subscribed subjects per holomush-iwzt §6.2 Tier 1.
	// See maxStreamScopeFloor in scope_floor.go for MAX semantics + the legacy-vs-NATS
	// subject format gap that currently keeps minFloor at zero for production filters.
	filterStrings := make([]string, len(filters))
	for i, subj := range filters {
		filterStrings[i] = string(subj)
	}
	minFloor := maxStreamScopeFloor(info, filterStrings)

	// Open the bus session. JS preserves the durable consumer's cursor
	// across reconnect, so events not acked last time get redelivered
	// automatically; there is no explicit replay phase. This span covers
	// the JetStream LookupConsumer + CreateOrUpdateConsumer round trips
	// — known to be a non-trivial chunk of connect latency under load
	// (see holomush-l015 for the warmup-race surface on the audit-side
	// counterpart).
	openCtx, openSpan := tracer.Start(ctx, "subscribe.bus_open_session",
		trace.WithAttributes(attribute.Int("filters.count", len(filters))))
	busStream, subErr := s.subscriber.OpenSession(openCtx, req.SessionId, sessionIdentity, filters, minFloor)
	if subErr != nil {
		recordSpanError(openSpan, subErr)
		openSpan.End()
		return oops.Code("SUBSCRIBE_FAILED").With("session_id", req.SessionId).Wrap(subErr)
	}
	openSpan.End()

	// Capture the Subscribe-attach moment IMMEDIATELY after OpenSession
	// returns (holomush-iu8j). This is the latest server timestamp that
	// bounds what backfill MIGHT need to deliver: events with a
	// timestamp later than attachMoment are guaranteed to go through
	// the live Subscribe stream (the durable consumer is now wired and
	// receiving), so backfill never has to cover them. The client
	// passes this value back on WebQueryStreamHistory as not_after_ms
	// to eliminate the connect-time replay/backfill race (fujt Fix B).
	//
	// Server-side time source: avoids the clock-skew risk a
	// client-computed value would introduce. Both publish-side
	// timestamps and this attach moment come from the same process
	// clock.
	attachMomentMs := time.Now().UTC().UnixMilli()
	defer func() {
		if closeErr := busStream.Close(); closeErr != nil {
			slog.WarnContext(ctx, "bus stream close failed", "session_id", req.SessionId, "error", closeErr)
		}
	}()

	// filterSet tracks the current subject set so mid-session mutations
	// can diff additions/removals without the caller having to enumerate.
	filterSet := make(map[eventbus.Subject]struct{}, len(filters))
	for _, f := range filters {
		filterSet[f] = struct{}{}
	}

	// locationFollower translates character move events into atomic filter
	// swaps against the bus session.
	locStreamName := ""
	if !info.LocationID.IsZero() {
		locStreamName = world.LocationStream(info.LocationID)
	}
	lf := &locationFollower{
		characterID:   info.CharacterID,
		currentLocID:  info.LocationID,
		worldQuerier:  s.worldQuerier,
		sessionStore:  s.sessionStore,
		locStreamName: locStreamName,
		updateFilters: s.makeFilterUpdater(busStream, filterSet),
		verbRegistry:  s.verbRegistry,
	}
	syntheticCtx, syntheticSpan := tracer.Start(ctx, "subscribe.send_synthetic")
	if sendErr := lf.sendSynthetic(syntheticCtx, stream); sendErr != nil {
		recordSpanError(syntheticSpan, sendErr)
		syntheticSpan.End()
		return oops.Code("SEND_FAILED").With("session_id", req.SessionId).Wrap(sendErr)
	}
	syntheticSpan.End()

	// (ctrlCh + Register/RegisterConnection were hoisted above to close
	// the focus-snapshot race window per CodeRabbit PR #4191 review.)

	// REPLAY_COMPLETE is emitted immediately — the bus handles replay
	// transparently by redelivering from the durable's acked-seq. Clients
	// that gate UI on this signal still see it at the expected point.
	// This is the latency-budget boundary for holomush-87qu: time from
	// Subscribe RPC entry to this Send is what the user perceives as the
	// 'syncing' window on the server side.
	if err := stream.Send(replayCompleteFrame(attachMomentMs)); err != nil {
		return oops.With("session_id", req.SessionId).Wrap(err)
	}
	subscribeSpan.AddEvent("subscribe.replay_complete_sent")

	// Resolve the connection's current FocusKey for badge downgrade.
	// connID is only non-zero when connection_id was supplied in the request.
	var connID *ulid.ULID
	if req.GetConnectionId() != "" {
		if parsed, parseErr := ulid.Parse(req.GetConnectionId()); parseErr == nil {
			connID = &parsed
		}
	}

	return s.runSubscribeLoop(ctx, info, busStream, filterSet, stream, lf, ctrlCh, connID)
}

// runSubscribeLoop is the post-REPLAY_COMPLETE live pump. It multiplexes
// bus deliveries against control-channel updates and ctx cancellation.
//
// Returns cleanly (nil) for:
//   - ctx cancellation (context.Canceled): client disconnected
//   - errStreamTerminated: matching session_ended observed inline
//
// All other errors (Send failures, bus errors) surface wrapped.
func (s *CoreServer) runSubscribeLoop(
	ctx context.Context,
	info *session.Info,
	busStream eventbus.SessionStream,
	filterSet map[eventbus.Subject]struct{},
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
	lf *locationFollower,
	ctrlCh chan sessionStreamUpdate,
	connID *ulid.ULID,
) error {
	type busResult struct {
		delivery eventbus.Delivery
		err      error
	}

	// Pumper goroutine — Next is blocking. Uses the handler's ctx so
	// ctx cancellation unblocks the pump cleanly.
	deliveries := make(chan busResult, 1)
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()
	go func() {
		defer close(deliveries)
		for {
			d, err := busStream.Next(pumpCtx)
			if err != nil {
				// ctx cancelled (handler returning) OR iterator closed
				// (busStream.Close was called on the handler's defer).
				select {
				case deliveries <- busResult{err: err}:
				case <-pumpCtx.Done():
				}
				return
			}
			select {
			case deliveries <- busResult{delivery: d}:
			case <-pumpCtx.Done():
				// Handler unwinding — nack so JS redelivers on rebind.
				if nackErr := d.Nack(); nackErr != nil {
					slog.DebugContext(ctx, "subscribe: nack on teardown failed",
						"session_id", info.ID, "error", nackErr)
				}
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			// Must not swallow ctx.Err — the telnet drain relies on the
			// RPC returning promptly when the gateway cancels the stream
			// (holomush-umxj).
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return oops.Code("SUBSCRIPTION_CANCELLED").With("session_id", info.ID).Wrap(ctx.Err())

		case r, ok := <-deliveries:
			if !ok {
				return nil
			}
			if r.err != nil {
				if errors.Is(r.err, context.Canceled) || errors.Is(r.err, io.EOF) {
					return nil
				}
				return oops.Code("SUBSCRIPTION_ERROR").With("session_id", info.ID).Wrap(r.err)
			}
			if sendErr := s.dispatchDelivery(ctx, info, r.delivery, stream, lf, connID); sendErr != nil {
				if errors.Is(sendErr, errStreamTerminated) {
					return nil
				}
				return sendErr
			}

		case ctrl, ok := <-ctrlCh:
			if !ok {
				return nil
			}
			if ctrlErr := s.applyFilterCtrl(ctx, info, busStream, filterSet, ctrl); ctrlErr != nil {
				slog.WarnContext(ctx, "subscribe: filter ctrl update failed",
					"session_id", info.ID, "stream", ctrl.stream, "error", ctrlErr)
			}
		}
	}
}

// dispatchDelivery converts a bus delivery to a proto frame, sends it, and
// acks the message on success. Move events are routed through the
// locationFollower — synthetic location_state is sent in lieu of the raw
// event when a cross-location move is detected. session_ended events that
// match this handler's session surface errStreamTerminated so the caller
// closes the stream gracefully.
//
// connID is the per-connection ULID for the Subscribe handler. When non-nil,
// the connection's FocusKey is read from the session store to determine
// whether a scene event should be downgraded to a SCENE_ACTIVITY badge
// (INV-SCENE-62) — non-focused member connections never receive event content
// for scenes they are not currently focused on.
func (s *CoreServer) dispatchDelivery(
	ctx context.Context,
	info *session.Info,
	delivery eventbus.Delivery,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
	lf *locationFollower,
	connID *ulid.ULID,
) error {
	event := delivery.Event()

	// AUDIT_ONLY events (e.g., crypto.totp_*, crypto.policy_set) MUST NOT
	// reach client streams. They flow through the bus solely so the audit
	// projection can persist them; we ack-and-skip here so JS retention
	// can age them out at the stream level. (holomush-jxo8.6.26.)
	if event.Rendering != nil && event.Rendering.DisplayTarget == eventbus.EventChannelAuditOnly {
		if ackErr := delivery.Ack(); ackErr != nil {
			slog.WarnContext(ctx, "subscribe: ack failed on audit-only skip; will redeliver",
				"session_id", info.ID, "event_id", event.ID.String(), "error", ackErr)
		}
		return nil
	}

	// Per-subject filter-at-delivery — load-bearing privacy gate per
	// holomush-iwzt §6.2 Tier 2. The stream classifiers (streamScopeFloor /
	// isCharacterStream / isLocationStream) are now dot-only (holomush-rops),
	// so we feed them the qualified NATS subject `event.Subject` directly;
	// the OpenSession-time minFloor (Tier 1) likewise classifies on NATS
	// subjects, so the floor is computed correctly at both tiers.
	//
	// Re-read SessionInfo per event so the filter picks up post-reattach /
	// post-move floor changes without subscriber restart. A follow-up bead
	// (holomush-hfb3) covers caching this in a per-Subscribe goroutine
	// local when latency profiling shows the per-event sessionStore.Get
	// matters.
	//
	// On lookup failure, fall back to the cached `info` parameter rather
	// than dropping: the only realistic Get failure in production is
	// "session not found" because the session was just deleted by quit /
	// evict (internal/grpc/server.go: EndSession → sessionStore.Delete).
	// The in-flight session_ended event in that case is the very event the
	// client needs to receive so it can close the stream gracefully —
	// fail-closed-by-drop would orphan the client on /terminal forever.
	// The cached `info` carries Subscribe-open LocationArrivedAt, which is
	// privacy-correct (never weaker than current state for legitimate
	// transitions: reattach within TTL preserves it; move only advances
	// it; delete is the only path that loses it and that path is the one
	// we want to forward through).
	//
	// Drop paths MUST ack the delivery — JetStream redelivers any unacked
	// event indefinitely on consumer reconnect.
	currentInfo, getErr := s.sessionStore.Get(ctx, info.ID)
	if getErr != nil {
		slog.DebugContext(ctx, "subscribe: filter-at-delivery using cached session info — store lookup failed",
			"session_id", info.ID, "event_id", event.ID.String(), "error", getErr)
		currentInfo = info
	}
	// Floor uses ns precision and >= semantics (INV-STORE-6 / INV-STORE-7,
	// gfo6 epic): publisher no longer truncates timestamps, so the
	// scope-floor comparison runs at full time.Now() resolution.
	floor := streamScopeFloor(currentInfo, string(event.Subject))
	if !floor.IsZero() && event.Timestamp.Before(floor) {
		slog.DebugContext(ctx, "subscribe: filter-at-delivery dropped event below scope floor",
			"session_id", info.ID, "event_id", event.ID.String(),
			"stream", string(event.Subject),
			"event_ts", event.Timestamp, "floor", floor)
		if ackErr := delivery.Ack(); ackErr != nil {
			slog.WarnContext(ctx, "subscribe: ack failed on filter-drop (below-floor) path; will redeliver",
				"session_id", info.ID, "event_id", event.ID.String(), "error", ackErr)
		}
		return nil
	}

	// E9.5 badge downgrade (INV-SCENE-62): a scene event delivered to a
	// member connection that is NOT focused on that scene becomes a
	// content-free SCENE_ACTIVITY ping. The event content (which may be
	// encrypted) is never forwarded. Lossy by design — clients re-sync
	// badge state via ListMyScenes. The below-floor check above ensures
	// only temporally-eligible events reach this guard.
	//
	// connID nil → no connection-level focus tracking (e.g., legacy callers
	// without connection_id) — fall through to normal send.
	if connID != nil {
		if sid, ok := extractSceneID(string(event.Subject)); ok {
			var currentFocus *session.FocusKey
			if conn, getErr := s.sessionStore.GetConnection(ctx, *connID); getErr == nil {
				currentFocus = conn.FocusKey
			}
			focusedOn := currentFocus != nil &&
				currentFocus.Kind == session.FocusKindScene &&
				currentFocus.TargetID.String() == sid
			if !focusedOn {
				// Mute / global-notify-off suppression (D-04). Consult the
				// injected checker BEFORE building the badge: if the member has
				// global notifications off or has muted this scene, drop the
				// already-content-free frame for BOTH surfaces (web badge +
				// telnet nudge). Fail-OPEN — a nil checker or any error delivers
				// the badge, because mute/notify-pref are preferences, not
				// access control (INV-SCENE-62 privacy is unaffected: the frame
				// carries no content either way).
				if s.sceneMute != nil {
					suppress, muteErr := s.sceneMute.ShouldSuppress(ctx,
						currentInfo.CharacterID.String(), currentInfo.PlayerID.String(), sid)
					switch {
					case muteErr != nil:
						slog.DebugContext(ctx, "subscribe: scene-mute check failed; delivering badge (fail-open)",
							"session_id", info.ID, "event_id", event.ID.String(), "scene_id", sid, "error", muteErr)
					case suppress:
						// Drop paths MUST ack — JetStream redelivers unacked
						// events indefinitely (mirrors the badge-send ack below).
						if ackErr := delivery.Ack(); ackErr != nil {
							slog.WarnContext(ctx, "subscribe: ack failed on scene-mute suppression; will redeliver",
								"session_id", info.ID, "event_id", event.ID.String(), "error", ackErr)
						}
						return nil
					}
				}
				badge := &corev1.SubscribeResponse{Frame: &corev1.SubscribeResponse_Control{
					Control: &corev1.ControlFrame{
						Signal:  corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY,
						SceneId: sid,
					},
				}}
				if sendErr := stream.Send(badge); sendErr != nil {
					if nackErr := delivery.Nack(); nackErr != nil {
						slog.DebugContext(ctx, "subscribe: nack after badge send failure",
							"session_id", info.ID, "error", nackErr)
					}
					return oops.With("event_id", event.ID.String()).Wrap(sendErr)
				}
				if ackErr := delivery.Ack(); ackErr != nil {
					slog.WarnContext(ctx, "subscribe: ack failed on badge downgrade; will redeliver",
						"session_id", info.ID, "event_id", event.ID.String(), "error", ackErr)
				}
				return nil
			}
		}
	}

	// locationFollower consumes move events on character streams and
	// replies with a synthetic location_state — in that case the raw
	// event is dropped (ack'd) rather than forwarded.
	handled := false
	// dispatchDelivery feeds the dot-only classifiers the qualified NATS
	// `event.Subject`, so the move guard uses isCharacterStream(event.Subject)
	// rather than a colon prefix. The wire frame.Stream
	// (toProtoSubscribeResponse) now carries that same qualified subject
	// (holomush-rops).
	if string(event.Type) == string(eventvocab.EventTypeMove) &&
		isCharacterStream(string(event.Subject)) &&
		lf != nil {
		handled = lf.handleMovePayload(ctx, eventvocab.EventType(event.Type), event.Payload, stream)
	}

	if !handled {
		if sendErr := stream.Send(s.toProtoSubscribeResponse(event, delivery.MetadataOnly())); sendErr != nil {
			// Do not ack — JS redelivers when the consumer rebinds.
			if nackErr := delivery.Nack(); nackErr != nil {
				slog.DebugContext(ctx, "subscribe: nack after send failure",
					"session_id", info.ID, "error", nackErr)
			}
			return oops.With("event_id", event.ID.String()).Wrap(sendErr)
		}
	}

	if ackErr := delivery.Ack(); ackErr != nil {
		slog.WarnContext(ctx, "subscribe: ack failed; will redeliver",
			"session_id", info.ID, "event_id", event.ID.String(), "error", ackErr)
	}

	// Terminal session_ended for this session → graceful close.
	if string(event.Type) == string(eventvocab.EventTypeSessionEnded) {
		var payload core.SessionEndedPayload
		if unmarshalErr := json.Unmarshal(event.Payload, &payload); unmarshalErr != nil {
			slog.WarnContext(ctx, "grpc: session_ended payload unmarshal failed — stream left open",
				"session_id", info.ID, "error", unmarshalErr)
			return nil
		}
		if payload.SessionID == info.ID {
			//nolint:errcheck // best-effort: client may already be disconnected
			_ = stream.Send(streamClosedFrame(payload.Reason))
			return errStreamTerminated
		}
	}
	return nil
}

// applyFilterCtrl applies a mid-session stream add/remove to the bus
// session's filter set via SetFilters. Location streams are rejected
// here so locationFollower remains the sole owner of those filters.
func (s *CoreServer) applyFilterCtrl(
	ctx context.Context,
	info *session.Info,
	busStream eventbus.SessionStream,
	filterSet map[eventbus.Subject]struct{},
	ctrl sessionStreamUpdate,
) error {
	if strings.HasPrefix(ctrl.stream, "location.") {
		slog.WarnContext(ctx, "plugin attempted to modify location stream — rejected",
			"session_id", info.ID, "stream", ctrl.stream)
		return nil
	}
	sub, err := s.toSubject(s.currentGameID(), ctrl.stream)
	if err != nil {
		return oops.With("session_id", info.ID).With("stream", ctrl.stream).Wrap(err)
	}
	if ctrl.add {
		if _, exists := filterSet[sub]; exists {
			return nil
		}
		filterSet[sub] = struct{}{}
	} else {
		if _, exists := filterSet[sub]; !exists {
			return nil
		}
		delete(filterSet, sub)
	}
	return oops.Wrap(busStream.SetFilters(ctx, filterSetToSlice(filterSet)))
}

// makeFilterUpdater returns the locationFilterUpdater the locationFollower
// invokes when a character move is detected. It maintains the same
// filterSet bookkeeping the ctrl path uses so the two stay in sync.
func (s *CoreServer) makeFilterUpdater(
	busStream eventbus.SessionStream,
	filterSet map[eventbus.Subject]struct{},
) locationFilterUpdater {
	return func(ctx context.Context, addStream, removeStream string) error {
		gameID := s.currentGameID()
		if addStream != "" {
			sub, err := s.toSubject(gameID, addStream)
			if err != nil {
				return oops.With("stream", addStream).Wrap(err)
			}
			filterSet[sub] = struct{}{}
		}
		if removeStream != "" {
			sub, err := s.toSubject(gameID, removeStream)
			if err != nil {
				return oops.With("stream", removeStream).Wrap(err)
			}
			delete(filterSet, sub)
		}
		return oops.Wrap(busStream.SetFilters(ctx, filterSetToSlice(filterSet)))
	}
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
