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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/timestamppb"

	accessTypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
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

// defaultMaxReplay is used when MaxReplay is not configured.
const defaultMaxReplay = 1000

// cursorCommitTimeout bounds the synchronous cursor commit inside
// replayAndSend. 1 second is ~1000x the healthy-DB latency, well below
// the "chat feels broken" threshold, and covers typical pool-wait hiccups.
// On timeout, the write is dropped (logged at error level) and the live
// loop continues — failure mode degrades to today's "duplicate-on-reconnect"
// behavior, never worse.
const cursorCommitTimeout = 1 * time.Second

// destroyedDrainQuiet is the maximum idle time the live loop waits for
// additional PG LISTEN/NOTIFY deliveries after Destroyed arrives, before
// sending STREAM_CLOSED. It must exceed typical NOTIFY propagation (~1ms)
// by a comfortable margin so events appended just before sessionStore.Delete
// (e.g. the quit handler's command_response "Goodbye!") are not dropped.
// 200ms is two orders of magnitude above observed NOTIFY latency and well
// under both downstream read windows: the telnet gateway's
// GatewayHandler.drainUntilClosed drainTimeout
// (internal/telnet/gateway_handler.go) and the test harness's
// testTelnetClient.ReadUntil 5s default
// (test/integration/telnet/e2e_test.go).
const destroyedDrainQuiet = 200 * time.Millisecond

// destroyedDrainHardCap bounds the total time the live loop will spend in
// the post-Destroyed drain, regardless of ongoing notification arrivals.
// Without a hard cap, a busy location stream (many chatty characters) can
// keep resetting destroyedDrainQuiet indefinitely and delay STREAM_CLOSED
// past both the gateway's drainUntilClosed timer and the test harness's 5s
// ReadUntil. 1s leaves comfortable headroom under the 5s ReadUntil while
// staying well below the gateway's drainTimeout, so the client normally
// receives the server's STREAM_CLOSED before the gateway falls back to
// its own "Goodbye!" delivery path.
const destroyedDrainHardCap = 1 * time.Second

// replayCompleteFrame returns a SubscribeResponse containing the
// REPLAY_COMPLETE control signal.
func replayCompleteFrame() *corev1.SubscribeResponse {
	return &corev1.SubscribeResponse{
		Frame: &corev1.SubscribeResponse_Control{
			Control: &corev1.ControlFrame{
				Signal: corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE,
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

// CoreServer implements the gRPC Core service.
type CoreServer struct {
	corev1.UnimplementedCoreServiceServer

	engine          *core.Engine
	sessionStore    session.Store
	eventStore      core.EventStore
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

	// Plugin stream contribution and mid-session stream control.
	streamContributor SessionStreamContributor
	streamRegistry    *SessionStreamRegistry

	// focusCoordinator manages session focus memberships and replay policy.
	// Nil until the Subscribe handler refactor (B7) wires it into the live loop.
	focusCoordinator focus.Coordinator

	// accessEngine evaluates ABAC policies for stream read authorization (Layer 2).
	// Nil if ABAC is not configured (public stream reads will be denied).
	accessEngine accessTypes.AccessPolicyEngine

	// afterLISTENHook fires between LISTEN setup and replay — used in tests.
	afterLISTENHook func()

	// newSessionID is used for generating session IDs. Can be overridden for testing.
	newSessionID func() ulid.ULID
	verbRegistry *core.VerbRegistry

	// cursorLocks serializes the per-session "Send + commit cursor"
	// critical section in replayAndSend against a concurrent Subscribe's
	// cursor read on reconnect, deterministically closing the Finding 1
	// race documented in holomush-9ues. Always non-nil after construction.
	cursorLocks *cursorLockMap

	// cursorCommitHook is an optional test seam invoked inside
	// replayAndSend's per-event critical section, AFTER Send returns and
	// BEFORE UpdateCursors. Production sets this to nil. Tests use it to
	// pause inside the critical section and drive a concurrent Subscribe
	// to assert deterministic closure of Finding 1.
	cursorCommitHook func(ctx context.Context, sessionID string, eventID ulid.ULID)
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

// WithEventStore sets the event store for replay support.
func WithEventStore(store core.EventStore) CoreServerOption {
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

// WithCursorCommitHook installs a test seam invoked inside replayAndSend's
// per-event critical section, AFTER Send returns and BEFORE UpdateCursors.
// Production code MUST NOT use this — it exists so integration tests can
// pause inside the critical section to drive a concurrent Subscribe and
// assert that Finding 1 is deterministically closed (holomush-9ues).
func WithCursorCommitHook(hook func(ctx context.Context, sessionID string, eventID ulid.ULID)) CoreServerOption {
	return func(s *CoreServer) {
		s.cursorCommitHook = hook
	}
}

// CursorLockRefCount returns the number of holders + queued waiters on
// the per-session cursor lock for sessionID, or 0 if no entry exists.
//
// This is an integration-test synchronization helper for the Finding 1
// closure spec (holomush-9ues): tests need to detect when a concurrent
// Subscribe handler has queued behind an in-flight commit, and polling
// on the refcount is the only race-free way to do that without timing
// heuristics. Production code MUST NOT call this — a refcount snapshot
// has no semantic meaning outside the lock map's internals.
func (s *CoreServer) CursorLockRefCount(sessionID string) int {
	return s.cursorLocks.refCount(sessionID)
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

// WithAccessEngine sets the ABAC policy engine for stream read authorization.
func WithAccessEngine(engine accessTypes.AccessPolicyEngine) CoreServerOption {
	return func(s *CoreServer) { s.accessEngine = engine }
}

// WithAfterLISTENHook sets a callback fired between LISTEN setup and replay.
// For testing only.
func WithAfterLISTENHook(hook func()) CoreServerOption {
	return func(s *CoreServer) {
		s.afterLISTENHook = hook
	}
}

// NewCoreServer creates a new Core gRPC server.
func NewCoreServer(engine *core.Engine, sessionStore session.Store, dispatcher *command.Dispatcher, cmdServices *command.Services, opts ...CoreServerOption) *CoreServer {
	s := &CoreServer{
		engine:       engine,
		sessionStore: sessionStore,
		dispatcher:   dispatcher,
		cmdServices:  cmdServices,
		newSessionID: core.NewULID,
		cursorLocks:  newCursorLockMap(),
	}

	for _, opt := range opts {
		opt(s)
	}

	if dispatcher == nil || cmdServices == nil {
		panic("grpc.NewCoreServer: dispatcher and cmdServices must not be nil")
	}

	return s
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

	slog.DebugContext(ctx, "handle command request",
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
		slog.DebugContext(ctx, "session ownership validation failed",
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
		slog.WarnContext(ctx, "command history append failed",
			"session_id", req.SessionId,
			"error", appendErr,
		)
	}

	// Parse and execute command
	if err := s.executeCommand(ctx, info, req.Command); err != nil {
		slog.WarnContext(ctx, "command execution failed",
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
func (s *CoreServer) executeCommand(ctx context.Context, info *session.Info, input string) error {
	return s.executeViaDispatcher(ctx, info, input)
}

// executeViaDispatcher uses the unified command.Dispatcher for command
// execution. Handler output written to the CommandExecution's io.Writer is
// captured in a buffer and emitted as a command_response event afterward.
func (s *CoreServer) executeViaDispatcher(ctx context.Context, info *session.Info, input string) error {
	char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}

	sessionID, parseErr := ulid.Parse(info.ID)
	if parseErr != nil {
		return oops.Code("INVALID_SESSION_ID").
			With("session_id", info.ID).
			Wrap(parseErr)
	}

	var buf bytes.Buffer
	exec, err := command.NewCommandExecution(command.CommandExecutionConfig{
		CharacterID:   info.CharacterID,
		PlayerID:      info.PlayerID,
		LocationID:    info.LocationID,
		CharacterName: info.CharacterName,
		SessionID:     sessionID,
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
		if dcErr := s.engine.HandleDisconnect(ctx, char, "quit"); dcErr != nil {
			slog.WarnContext(ctx, "leave event failed", "error", dcErr)
		}
		if delErr := s.sessionStore.Delete(ctx, info.ID, "Goodbye!"); delErr != nil {
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

		if dcErr := s.engine.HandleDisconnect(ctx, booted.CharacterRef, "booted"); dcErr != nil {
			slog.WarnContext(ctx, "boot leave event failed",
				"target_id", booted.CharacterRef.ID.String(),
				"error", dcErr)
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
	payload, err := json.Marshal(core.CommandResponsePayload{
		Text: text,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal command_response payload",
			"character_id", char.ID.String(),
			"error", err,
		)
		return oops.Code("COMMAND_RESPONSE_MARSHAL_FAILED").Wrap(err)
	}

	eventType := core.EventTypeCommandResponse
	if isError {
		eventType = core.EventTypeCommandError
	}
	event := core.NewEvent(world.CharacterStream(char.ID), eventType, core.Actor{
		Kind: core.ActorSystem,
		ID:   "system",
	}, payload)

	if s.eventStore == nil {
		slog.DebugContext(ctx, "emitCommandResponse: eventStore not configured, event not emitted")
		return nil
	}

	if err := s.eventStore.Append(ctx, event); err != nil {
		slog.WarnContext(ctx, "failed to append command_response event",
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
					slog.ErrorContext(ctx, "disconnect hook panicked",
						"panic", r,
						"stack", string(debug.Stack()),
					)
				}
			}()
			hook(info)
		}()
	}
}

// maxReplay returns the configured maximum replay count, or the default.
func (s *CoreServer) maxReplay() int {
	if s.sessionDefaults.MaxReplay > 0 {
		return s.sessionDefaults.MaxReplay
	}
	return defaultMaxReplay
}

// eventToProto converts a core.Event to a proto Event wrapped in an EventFrame.
func eventToProto(ev core.Event) *corev1.SubscribeResponse {
	return &corev1.SubscribeResponse{
		Frame: &corev1.SubscribeResponse_Event{
			Event: &corev1.EventFrame{
				Id:        ev.ID.String(),
				Stream:    ev.Stream,
				Type:      string(ev.Type),
				Timestamp: timestamppb.New(ev.Timestamp),
				ActorType: ev.Actor.Kind.String(),
				ActorId:   ev.Actor.ID,
				Payload:   ev.Payload,
			},
		},
	}
}

// replayAndSend fetches events after afterID on the given stream and sends
// them to the client. Character-stream move events are consumed by the
// location follower instead of being forwarded. Returns the last sent event
// ID (or afterID if no events were replayed) and any send error.
//
// Each event flows through sendAndCommitEvent, which holds the per-session
// cursor lock around the Send + UpdateCursors atom. Per-event commit (rather
// than batch-end commit) keeps the maximum contiguous lock-hold bounded by
// one event's Send latency plus one DB UPDATE — the smallest critical
// section that still deterministically closes the Finding 1 race documented
// in holomush-9ues. Concurrent Subscribes for the same session may slip in
// between events rather than waiting for the entire batch.
func (s *CoreServer) replayAndSend(
	ctx context.Context,
	info *session.Info,
	streamName string,
	afterID ulid.ULID,
	grpcStream grpc.ServerStreamingServer[corev1.SubscribeResponse],
	lf *locationFollower,
) (ulid.ULID, error) {
	events, err := s.eventStore.Replay(ctx, streamName, afterID, s.maxReplay())
	if err != nil {
		slog.WarnContext(ctx, "replay failed", "stream", streamName, "error", err)
		return afterID, nil // non-fatal: skip this stream
	}
	last := afterID
	for _, ev := range events {
		if sendErr := s.sendAndCommitEvent(ctx, info, streamName, ev, grpcStream, lf); sendErr != nil {
			return last, sendErr
		}
		last = ev.ID
	}
	return last, nil
}

// sendAndCommitEvent delivers a single event to the client and commits the
// per-stream cursor under the per-session lock, in that order.
//
// The critical section spans Send → (optional cursorCommitHook test seam)
// → UpdateCursors. Holding the lock across Send is necessary: any acquisition
// after Send leaves a TOCTOU window where a concurrent Subscribe can read
// the stale cursor between "wire bytes left" and "lock acquired", reproducing
// the very race this method is meant to close.
//
// Cursor commit failures are logged but do not surface to the caller —
// they degrade gracefully to "duplicate-on-reconnect", never worse.
func (s *CoreServer) sendAndCommitEvent(
	ctx context.Context,
	info *session.Info,
	streamName string,
	ev core.Event,
	grpcStream grpc.ServerStreamingServer[corev1.SubscribeResponse],
	lf *locationFollower,
) error {
	unlock := s.cursorLocks.lock(info.ID)
	defer unlock()

	// Character-stream move events are routed through locationFollower
	// so the client receives a synthetic location_state for the new
	// location. handleEvent returns true only when it fully consumed
	// the event (built and Sent the location_state); for all other
	// paths — nil worldQuerier, payload unmarshal failure, same
	// location, buildLocationState failure, or location_state Send
	// failure — it returns false and the raw event must still be
	// forwarded so the client is not left in a stale state with a
	// silently-advanced cursor.
	handled := false
	if ev.Type == core.EventTypeMove &&
		strings.HasPrefix(ev.Stream, world.StreamPrefixCharacter) &&
		lf != nil {
		handled = lf.handleEvent(ctx, ev, grpcStream)
	}
	if !handled {
		if sendErr := grpcStream.Send(eventToProto(ev)); sendErr != nil {
			return oops.With("event_id", ev.ID.String()).Wrap(sendErr)
		}
	}

	// Test seam: pause inside the critical section so integration tests can
	// race a concurrent Subscribe and assert it does not observe the stale
	// cursor. Production sets this to nil. See WithCursorCommitHook.
	if hook := s.cursorCommitHook; hook != nil {
		hook(ctx, info.ID, ev.ID)
	}

	// Synchronous, bounded-timeout cursor commit. Uses a fresh context so a
	// client disconnect (which cancels `ctx`) does not abort the write,
	// matching the semantics the old persistCursorAsync intended.
	commitCtx, cancel := context.WithTimeout(context.Background(), cursorCommitTimeout)
	defer cancel()
	if updateErr := s.sessionStore.UpdateCursors(commitCtx, info.ID,
		map[string]ulid.ULID{streamName: ev.ID}); updateErr != nil {
		slog.ErrorContext(ctx, "cursor commit failed",
			"session_id", info.ID,
			"stream", streamName,
			"event_id", ev.ID.String(),
			"error", updateErr)
	}
	return nil
}

// drainNotificationsUntilQuiet consumes pending sub.Notifications and
// forwards their events to the client, returning when one of: no new
// notification arrives within `quiet`, the aggregate drain time reaches
// `hardCap`, ctx is cancelled, or sub.Errors() fires. Send errors are
// logged at Warn (not propagated) because the caller is already about to
// close the stream with STREAM_CLOSED and the stream may already be
// half-torn-down; we want a visible signal if this ever hides a real bug.
//
// The two deadlines address different failure modes:
//   - `quiet` covers async NOTIFY delivery lag — the quit handler's
//     command_response event is Appended synchronously before
//     sessionStore.Delete, but the PG LISTEN/NOTIFY round-trip to the
//     pgSubscription goroutine runs in the millisecond range. Without a
//     small wait here, the drain would often see an empty channel at the
//     exact moment Destroyed arrives.
//   - `hardCap` prevents busy location streams (many chatty characters)
//     from resetting the quiet timer indefinitely and delaying
//     STREAM_CLOSED past downstream windows.
//
// Used from the Destroyed branch of the Subscribe live loop to close
// holomush-umxj Mode B's NOTIFY-delivery-lag sub-problem. The complementary
// registration-window sub-problem is closed by the atomic check-and-register
// inside MemStore / PostgresSessionStore.WatchSession.
func (s *CoreServer) drainNotificationsUntilQuiet(
	ctx context.Context,
	info *session.Info,
	sub core.Subscription,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
	lf *locationFollower,
	quiet time.Duration,
	hardCap time.Duration,
) {
	quietTimer := time.NewTimer(quiet)
	defer quietTimer.Stop()
	hardTimer := time.NewTimer(hardCap)
	defer hardTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-hardTimer.C:
			return
		case <-sub.Errors():
			return
		case notif := <-sub.Notifications():
			cursor := ulid.ULID{}
			if c, ok := info.EventCursors[notif.Stream]; ok {
				cursor = c
			}
			last, sendErr := s.replayAndSend(ctx, info, notif.Stream, cursor, stream, lf)
			if sendErr != nil {
				slog.WarnContext(ctx, "drain: send failed",
					"session_id", info.ID, "stream", notif.Stream, "error", sendErr)
				return
			}
			info.EventCursors[notif.Stream] = last
			if !quietTimer.Stop() {
				select {
				case <-quietTimer.C:
				default:
				}
			}
			quietTimer.Reset(quiet)
		case <-quietTimer.C:
			return
		}
	}
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
func (s *CoreServer) Subscribe(req *corev1.SubscribeRequest, stream grpc.ServerStreamingServer[corev1.SubscribeResponse]) error {
	ctx := stream.Context()
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(ctx, "subscribe request",
		"request_id", requestID,
		"session_id", req.SessionId,
	)

	if s.eventStore == nil {
		return oops.Code("NOT_CONFIGURED").Errorf("event store not configured")
	}

	// Validate session ownership before any other work. Enumeration-safe:
	// every failure mode collapses to the same SESSION_NOT_FOUND error.
	if _, err := auth.ValidateSessionOwnership(
		ctx,
		s.playerSessionRepo,
		s.sessionStore,
		req.GetPlayerSessionToken(),
		req.GetSessionId(),
	); err != nil {
		slog.DebugContext(ctx, "subscribe session ownership validation failed",
			"request_id", requestID,
			"session_id", req.SessionId,
			"error", err,
		)
		return oops.Code("SESSION_NOT_FOUND").With("session_id", req.SessionId).Errorf("session not found")
	}

	// 1. Session lookup under the cursor lock (Finding 1 closure).
	info, err := func() (*session.Info, error) {
		unlock := s.cursorLocks.lock(req.SessionId)
		defer unlock()
		return s.sessionStore.Get(ctx, req.SessionId)
	}()
	if err != nil {
		return oops.Code("SESSION_NOT_FOUND").With("session_id", req.SessionId).Errorf("session not found")
	}

	// Register the lifecycle watcher immediately, before replay, so any
	// Delete fired during the rest of Subscribe setup (AddConnection,
	// ReattachCAS, RestoreFocus, SubscribeSession, replay) is captured.
	//
	// Atomicity note: Get above and WatchSession below do NOT run under a
	// shared mu — the cursor lock is released before WatchSession is
	// called. Closure of the Get↔WatchSession window is instead provided
	// by the store itself: MemStore / PostgresSessionStore.WatchSession
	// takes its own internal mu, checks session existence, and if the
	// session has already been deleted by the time mu is acquired it
	// returns a channel pre-loaded with Destroyed and closed. This makes
	// the live loop handle the late-delete case as if the notification
	// had arrived normally. See holomush-umxj Mode B.
	sessionCh, watchErr := s.sessionStore.WatchSession(ctx, req.SessionId)
	if watchErr != nil {
		slog.Warn("failed to watch session lifecycle",
			"session_id", req.SessionId, "error", watchErr)
	}
	// Guard: the Store interface permits (nil, err). A receive on a nil
	// channel blocks forever, which would leave the live loop observing
	// no lifecycle events and hanging until ctx cancel. Substitute a
	// never-firing channel so the select still terminates on ctx.Done()
	// and sub.Errors() as expected.
	if sessionCh == nil {
		sessionCh = make(chan session.Event)
	}

	// Register the caller's connection in core (bd-j2xj). Connection
	// lifecycle belongs in the Subscribe RPC, not in the web gateway.
	//
	// Transitional gate: only runs when connection_id is non-empty. The
	// telnet gateway passes connection_id + client_type after Task 8; the
	// web gateway still calls AddConnection itself and does NOT pass a
	// connection_id yet. Task 14 makes connection_id required and removes
	// the web gateway's direct AddConnection call, retiring this gate.
	if req.GetConnectionId() != "" {
		connID, parseErr := ulid.Parse(req.GetConnectionId())
		if parseErr != nil || req.GetClientType() == "" {
			return oops.Code("SUBSCRIBE_INVALID_CONNECTION").
				With("session_id", req.GetSessionId()).
				Errorf("subscribe: connection_id must be a valid ULID and client_type must be set")
		}
		// Streams is initialized to an empty slice (not nil) because the
		// session_connections.streams column is NOT NULL; pgx serializes a
		// Go nil []string as SQL NULL, which would fail the insert.
		// Per-stream subscriptions are tracked in memory by the Subscribe
		// loop; the column is a placeholder for future queries.
		conn := &session.Connection{
			ID:          connID,
			SessionID:   req.GetSessionId(),
			ClientType:  req.GetClientType(),
			Streams:     []string{},
			ConnectedAt: time.Now(),
		}
		if addErr := s.sessionStore.AddConnection(ctx, conn); addErr != nil {
			return oops.Code("SUBSCRIBE_ADD_CONNECTION_FAILED").
				With("session_id", req.GetSessionId()).
				With("connection_id", req.GetConnectionId()).
				Wrap(addErr)
		}
		// Deferred cleanup fires on any exit path: client disconnect,
		// context cancel, error return, STREAM_CLOSED. Uses a fresh
		// context so a cancelled stream context does not abort the
		// removal (same pattern as cursor commit).
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), cursorCommitTimeout)
			defer cancel()
			if rmErr := s.sessionStore.RemoveConnection(cleanupCtx, connID); rmErr != nil {
				slog.WarnContext(ctx, "subscribe: failed to remove connection on stream close",
					"connection_id", connID.String(),
					"session_id", req.GetSessionId(),
					"error", rmErr,
				)
			}
		}()
	}

	// 2. Reattach if detached.
	if info.Status == session.StatusDetached {
		ok, casErr := s.sessionStore.ReattachCAS(ctx, req.SessionId)
		if casErr != nil {
			return oops.Code("SESSION_REATTACH_FAILED").With("session_id", req.SessionId).Wrap(casErr)
		}
		if !ok {
			return oops.Code("SESSION_REATTACH_LOST").
				With("session_id", req.SessionId).
				Errorf("session reattach CAS lost — another handler won the race")
		}
	}

	// 3. RestoreFocus — produces the full stream list and replay modes.
	var plan focus.RestorePlan
	if s.focusCoordinator != nil {
		var planErr error
		plan, planErr = s.focusCoordinator.RestoreFocus(ctx, req.SessionId)
		if planErr != nil {
			slog.WarnContext(ctx, "RestoreFocus failed, falling back to empty plan",
				"session_id", req.SessionId, "error", planErr)
		}
	}

	// Ensure ambient streams are present even without a coordinator.
	if len(plan.Streams) == 0 {
		plan.Streams = append(plan.Streams,
			focus.StreamWithMode{Stream: world.CharacterStream(info.CharacterID), Mode: focus.ReplayModeFromCursor},
		)
		if !info.LocationID.IsZero() {
			plan.Streams = append(plan.Streams,
				focus.StreamWithMode{Stream: world.LocationStream(info.LocationID), Mode: focus.ReplayModeFromCursor},
			)
		}
		// Query plugin-contributed streams when no FocusCoordinator
		// is wired (the coordinator handles this internally when
		// present).
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
				plan.Streams = append(plan.Streams,
					focus.StreamWithMode{Stream: ps, Mode: focus.ReplayModeFromCursor},
				)
			}
		}
	}

	// 4. SubscribeSession (Variant A — strict cross-stream ordering via single PG connection).
	sub, subErr := s.eventStore.SubscribeSession(ctx)
	if subErr != nil {
		return oops.Code("SUBSCRIBE_FAILED").With("session_id", req.SessionId).Wrap(subErr)
	}
	defer func() {
		if closeErr := sub.Close(); closeErr != nil {
			slog.WarnContext(ctx, "subscription close failed", "session_id", req.SessionId, "error", closeErr)
		}
	}()

	// 5. Add all streams from plan.
	for _, sm := range plan.Streams {
		if addErr := sub.AddStream(ctx, sm.Stream); addErr != nil {
			slog.WarnContext(ctx, "failed to add stream from plan",
				"session_id", req.SessionId, "stream", sm.Stream, "error", addErr)
		}
	}

	// 6. locationFollower
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
		sub:           sub,
	}
	if sendErr := lf.sendSynthetic(ctx, stream); sendErr != nil {
		return oops.Code("SEND_FAILED").With("session_id", req.SessionId).Wrap(sendErr)
	}

	// 7. Control channel for mid-session stream updates.
	ctrlCh := make(chan sessionStreamUpdate, 16)
	if s.streamRegistry != nil {
		s.streamRegistry.Register(info.ID, ctrlCh)
		defer s.streamRegistry.Deregister(info.ID, ctrlCh)
	}

	if s.afterLISTENHook != nil {
		s.afterLISTENHook()
	}

	// 8. Merge-sort replay (I-15).
	if replayErr := s.replayRestorePlan(ctx, info, plan, stream, lf); replayErr != nil {
		return replayErr
	}

	// 9. REPLAY_COMPLETE
	if err := stream.Send(replayCompleteFrame()); err != nil {
		return oops.With("session_id", req.SessionId).Wrap(err)
	}

	// 10. Live loop — single select on Subscription notifications + control + session.
	// sessionCh was registered earlier (step 1) so any Delete during setup
	// is captured. See the note there for the rationale.
	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				return nil
			}
			return oops.Code("SUBSCRIPTION_CANCELLED").With("session_id", req.SessionId).Wrap(ctx.Err())

		case subLoopErr := <-sub.Errors():
			return oops.Code("SUBSCRIPTION_ERROR").With("session_id", req.SessionId).Wrap(subLoopErr)

		case notif := <-sub.Notifications():
			cursor := ulid.ULID{}
			if c, ok := info.EventCursors[notif.Stream]; ok {
				cursor = c
			}
			last, sendErr := s.replayAndSend(ctx, info, notif.Stream, cursor, stream, lf)
			if sendErr != nil {
				return oops.Code("SEND_FAILED").With("session_id", req.SessionId).Wrap(sendErr)
			}
			info.EventCursors[notif.Stream] = last

		case ev, ok := <-sessionCh:
			if !ok {
				// Defensive fallback: current MemStore and
				// PostgresSessionStore both always buffer a Destroyed
				// event into the channel before closing it, so the normal
				// path takes the ev.Type == Destroyed branch below. A
				// bare channel close without Destroyed would indicate
				// either a future Store implementation that skips the
				// buffered event or a programmer error; in either case,
				// close the stream with an empty message rather than
				// hang.
				//nolint:errcheck // best-effort: client may already be disconnected
				_ = stream.Send(streamClosedFrame(""))
				return nil
			}
			if ev.Type == session.Destroyed {
				// Drain pending stream notifications (see
				// drainNotificationsUntilQuiet) so events emitted just
				// before Delete reach the client ahead of STREAM_CLOSED.
				s.drainNotificationsUntilQuiet(ctx, info, sub, stream, lf,
					destroyedDrainQuiet, destroyedDrainHardCap)
				//nolint:errcheck // best-effort: client may already be disconnected
				_ = stream.Send(streamClosedFrame(ev.Message))
				return nil
			}

		case ctrl, ok := <-ctrlCh:
			if !ok {
				return nil
			}
			if ctrlErr := s.applyCtrlUpdate(ctx, info, sub, ctrl, stream, lf); ctrlErr != nil {
				return oops.Code("SEND_FAILED").With("session_id", info.ID).Wrap(ctrlErr)
			}
		}
	}
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

	slog.DebugContext(ctx, "disconnect request",
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
		slog.DebugContext(ctx, "disconnect session ownership validation failed",
			"request_id", requestID,
			"session_id", req.SessionId,
			"error", err,
		)
		return &corev1.DisconnectResponse{
			Meta:    responseMeta(requestID),
			Success: false,
		}, nil
	}

	// Remove specific connection if provided
	if req.ConnectionId != "" {
		connID, parseErr := ulid.Parse(req.ConnectionId)
		if parseErr == nil {
			if err := s.sessionStore.RemoveConnection(ctx, connID); err != nil {
				slog.WarnContext(ctx, "failed to remove connection",
					"request_id", requestID,
					"connection_id", req.ConnectionId,
					"error", err,
				)
			}
		}
	}

	// Look up session
	info, err := s.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		//nolint:nilerr // intentional: session already gone, return success (idempotent)
		return &corev1.DisconnectResponse{
			Meta:    responseMeta(requestID),
			Success: true,
		}, nil
	}

	// Count remaining connections by type.
	// NOTE: These counts are read separately from the RemoveConnection above,
	// creating a small race window. In practice this is benign — concurrent
	// disconnects may both observe totalCount==0 and both call UpdateStatus,
	// but UpdateStatus is idempotent. A proper transactional
	// RemoveConnectionAndCount would eliminate this race.
	// TODO: replace two CountConnectionsByType calls with a single query.
	totalCount, err := s.sessionStore.CountConnections(ctx, req.SessionId)
	if err != nil {
		slog.WarnContext(ctx, "failed to count connections — skipping lifecycle transition",
			"request_id", requestID,
			"session_id", req.SessionId,
			"error", err,
		)
		return &corev1.DisconnectResponse{Meta: responseMeta(requestID), Success: true}, nil
	}

	termCount, err := s.sessionStore.CountConnectionsByType(ctx, req.SessionId, "terminal")
	if err != nil {
		slog.WarnContext(ctx, "failed to count terminal connections",
			"request_id", requestID,
			"error", err,
		)
	}
	telCount, err := s.sessionStore.CountConnectionsByType(ctx, req.SessionId, "telnet")
	if err != nil {
		slog.WarnContext(ctx, "failed to count telnet connections",
			"request_id", requestID,
			"error", err,
		)
	}
	gridConns := termCount + telCount

	if totalCount == 0 {
		// No connections at all
		if info.IsGuest {
			// Guests can't reconnect — delete immediately
			char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
			if err := s.engine.HandleDisconnect(ctx, char, "quit"); err != nil {
				slog.WarnContext(ctx, "leave event failed",
					"request_id", requestID,
					"error", err,
				)
			}

			if err := s.sessionStore.Delete(ctx, req.SessionId, "Guest session ended"); err != nil {
				slog.WarnContext(ctx, "failed to delete guest session",
					"request_id", requestID,
					"error", err,
				)
			}

			// Run disconnect hooks for guests
			s.runDisconnectHooks(ctx, *info)
		} else {
			// Non-guest: detach session with TTL instead of deleting
			now := time.Now()
			ttlSeconds := info.TTLSeconds
			if ttlSeconds <= 0 {
				ttlSeconds = 1800 // default 30 minutes
			}
			expiresAt := now.Add(time.Duration(ttlSeconds) * time.Second)
			if err := s.sessionStore.UpdateStatus(ctx, req.SessionId,
				session.StatusDetached, &now, &expiresAt); err != nil {
				slog.WarnContext(ctx, "failed to detach session",
					"request_id", requestID,
					"error", err,
				)
			}

			// Do NOT emit leave event — player may reconnect.
			// Reaper handles leave events when TTL expires.
		}
	} else if gridConns == 0 && info.GridPresent {
		// Only comms_hub connections remain — phase out from grid
		char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
		if err := s.engine.HandleDisconnect(ctx, char, "phased out"); err != nil {
			slog.WarnContext(ctx, "phase-out leave event failed",
				"request_id", requestID,
				"session_id", req.SessionId,
				"error", err,
			)
		}
		if err := s.sessionStore.UpdateGridPresent(ctx, req.SessionId, false); err != nil {
			slog.WarnContext(ctx, "failed to update grid presence",
				"request_id", requestID,
				"session_id", req.SessionId,
				"error", err,
			)
		}
	}

	slog.InfoContext(ctx, "session disconnected",
		"request_id", requestID,
		"session_id", req.SessionId,
		"character_id", info.CharacterID.String(),
	)

	return &corev1.DisconnectResponse{
		Meta:    responseMeta(requestID),
		Success: true,
	}, nil
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
		slog.DebugContext(ctx, "get_command_history session ownership validation failed",
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
