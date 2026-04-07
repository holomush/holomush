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

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
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

// streamNotification wraps a ULID event notification with the stream
// it came from. Relay goroutines send these to a shared channel so the
// live loop can select on a single source.
type streamNotification struct {
	stream  string
	eventID ulid.ULID
}

// startNotificationRelay starts a goroutine that reads ULID notifications
// from a subscription channel and forwards them as streamNotifications to
// the shared notifyCh. The goroutine exits when the source channel closes
// or ctx is cancelled.
func startNotificationRelay(
	ctx context.Context,
	stream string,
	source <-chan ulid.ULID,
	notifyCh chan<- streamNotification,
) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case id, ok := <-source:
				if !ok {
					return
				}
				select {
				case notifyCh <- streamNotification{stream: stream, eventID: id}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
}

// WorldQuerier provides read-only access to world model data for building
// location_state payloads during event streaming. Satisfied by *world.Service.
type WorldQuerier interface {
	GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*world.Location, error)
	GetExitsByLocation(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*world.Exit, error)
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

	// Look up session — distinguish not-found from other store errors.
	info, err := s.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		errMsg := "session not found"
		if oopsErr, ok := oops.AsOops(err); !ok || oopsErr.Code() != "SESSION_NOT_FOUND" {
			errMsg = "session lookup failed"
			slog.ErrorContext(ctx, "session store error",
				"request_id", requestID,
				"session_id", req.SessionId,
				"error", err,
			)
		}
		return &corev1.HandleCommandResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   errMsg,
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
	event := core.Event{
		ID:        core.NewULID(),
		Stream:    world.CharacterStream(char.ID),
		Type:      eventType,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorSystem, ID: "system"},
		Payload:   payload,
	}

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

// subscribeStream sets up a LISTEN subscription on a stream and starts a
// relay goroutine that forwards ULID notifications to notifyCh. Subscription
// errors are forwarded to errCh. Returns an error if the LISTEN setup fails.
func subscribeStream(ctx context.Context, store core.EventStore, stream string, notifyCh chan<- streamNotification, errCh chan<- error) error {
	eventCh, subErrCh, err := store.Subscribe(ctx, stream)
	if err != nil {
		return oops.With("stream", stream).Wrap(err)
	}
	startNotificationRelay(ctx, stream, eventCh, notifyCh)
	go func() {
		select {
		case e, ok := <-subErrCh:
			if ok && e != nil {
				select {
				case errCh <- e:
				default:
				}
			}
		case <-ctx.Done():
		}
	}()
	return nil
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

// Subscribe opens a stream of events for the session.
func (s *CoreServer) Subscribe(req *corev1.SubscribeRequest, stream grpc.ServerStreamingServer[corev1.SubscribeResponse]) error {
	ctx := stream.Context()
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(ctx, "subscribe request",
		"request_id", requestID,
		"session_id", req.SessionId,
		"streams", req.Streams,
	)

	if s.eventStore == nil {
		return oops.Code("NOT_CONFIGURED").Errorf("event store not configured")
	}

	// Acquire the per-session cursor lock around the session Get so the
	// EventCursors map we capture here reflects any in-flight commit by a
	// previous Subscribe handler for this session. Releasing the lock
	// before the rest of the handler runs keeps the read-side critical
	// section to a single DB SELECT — the absolute minimum needed to
	// close the Finding 1 race (holomush-9ues). The captured cursors
	// are immutable thereafter from this handler's perspective.
	info, err := func() (*session.Info, error) {
		unlock := s.cursorLocks.lock(req.SessionId)
		defer unlock()
		return s.sessionStore.Get(ctx, req.SessionId)
	}()
	if err != nil {
		return oops.Code("SESSION_NOT_FOUND").With("session_id", req.SessionId).Errorf("session not found")
	}

	// Determine requested streams; default to the session's location.
	streams := req.Streams
	if len(streams) == 0 {
		streams = []string{world.LocationStream(info.LocationID)}
	}

	// Separate location stream (dynamically swapped on move) from static streams.
	var locStreamName string
	var staticStreams []string
	for _, sn := range streams {
		if strings.HasPrefix(sn, world.StreamPrefixLocation) {
			locStreamName = sn
		} else {
			staticStreams = append(staticStreams, sn)
		}
	}
	charStreamName := world.CharacterStream(info.CharacterID)
	alreadyHasCharStream := false
	for _, sn := range staticStreams {
		if sn == charStreamName {
			alreadyHasCharStream = true
			break
		}
	}
	if !alreadyHasCharStream {
		staticStreams = append(staticStreams, charStreamName)
	}

	// All streams = static + location.
	allStreams := make([]string, 0, len(staticStreams)+1)
	allStreams = append(allStreams, staticStreams...)
	if locStreamName != "" {
		allStreams = append(allStreams, locStreamName)
	}

	// Set up LISTEN subscriptions before any replay to avoid missing events.
	notifyCh := make(chan streamNotification, 100)
	errCh := make(chan error, len(allStreams))
	for _, sn := range allStreams {
		if subErr := subscribeStream(ctx, s.eventStore, sn, notifyCh, errCh); subErr != nil {
			return oops.Code("SUBSCRIBE_FAILED").With("session_id", req.SessionId).With("stream", sn).Wrap(subErr)
		}
	}

	// Synthetic location_state so the client has location context immediately.
	if s.worldQuerier != nil && !info.LocationID.IsZero() {
		syntheticLF := &locationFollower{
			characterID:  info.CharacterID,
			currentLocID: info.LocationID,
			worldQuerier: s.worldQuerier,
			sessionStore: s.sessionStore,
		}
		if locState, rsErr := syntheticLF.buildLocationState(ctx, info.LocationID); rsErr == nil {
			if sendErr := stream.Send(locState); sendErr != nil {
				return oops.Code("SEND_FAILED").With("session_id", req.SessionId).Wrap(sendErr)
			}
		}
	}

	lf := &locationFollower{
		characterID:   info.CharacterID,
		currentLocID:  info.LocationID,
		worldQuerier:  s.worldQuerier,
		sessionStore:  s.sessionStore,
		eventStore:    s.eventStore,
		locStreamName: locStreamName,
		notifyCh:      notifyCh,
		errCh:         errCh,
	}
	defer func() {
		if lf.locCancel != nil {
			lf.locCancel()
		}
	}()

	// Single replay pass: catches both historical cursor-based events
	// (reconnection) and events appended during LISTEN setup (race window).
	lastSentID := make(map[string]ulid.ULID)
	for _, sn := range allStreams {
		cursor := lastSentID[sn]
		if req.ReplayFromCursor {
			if c, ok := info.EventCursors[sn]; ok {
				cursor = c
			}
		}
		last, sendErr := s.replayAndSend(ctx, info, sn, cursor, stream, lf)
		if sendErr != nil {
			return oops.Code("SEND_FAILED").With("session_id", info.ID).Wrap(sendErr)
		}
		lastSentID[sn] = last
	}

	// Signal to the client that the replay phase is complete.
	if err := stream.Send(&corev1.SubscribeResponse{
		Frame: &corev1.SubscribeResponse_Control{
			Control: &corev1.ControlFrame{
				Signal: corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE,
			},
		},
	}); err != nil {
		return oops.With("session_id", req.SessionId).Wrap(err)
	}

	// Watch for session destruction so we can emit STREAM_CLOSED.
	sessionCh, watchErr := s.sessionStore.WatchSession(ctx, req.SessionId)
	if watchErr != nil {
		slog.Warn("failed to watch session lifecycle",
			"session_id", req.SessionId, "error", watchErr)
		// sessionCh is nil — the case will never be selected (graceful degradation).
	}

	// Live event loop: select on notifications, replay from lastSentID.
	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				return nil
			}
			return oops.Code("SUBSCRIPTION_CANCELLED").With("session_id", req.SessionId).Wrap(ctx.Err())

		case subErr := <-errCh:
			return oops.Code("SUBSCRIPTION_ERROR").With("session_id", req.SessionId).Wrap(subErr)

		case notif := <-notifyCh:
			last, sendErr := s.replayAndSend(ctx, info, notif.stream, lastSentID[notif.stream], stream, lf)
			if sendErr != nil {
				return oops.Code("SEND_FAILED").With("session_id", req.SessionId).Wrap(sendErr)
			}
			lastSentID[notif.stream] = last

		case ev, ok := <-sessionCh:
			if !ok {
				return nil // channel closed without event
			}
			if ev.Type == session.Destroyed {
				// Best-effort: send STREAM_CLOSED, ignore send errors.
				//nolint:errcheck // best-effort: client may already be disconnected
				_ = stream.Send(&corev1.SubscribeResponse{
					Frame: &corev1.SubscribeResponse_Control{
						Control: &corev1.ControlFrame{
							Signal:  corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED,
							Message: ev.Message,
						},
					},
				})
				return nil
			}
		}
	}
}

// Disconnect removes a connection and handles session lifecycle.
// When all terminal/telnet connections close but comms_hub remains, the
// character phases out (leave event) but the session stays active.
// When ALL connections close, non-guest sessions detach with TTL;
// guest sessions are deleted immediately.
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
func (s *CoreServer) GetCommandHistory(ctx context.Context, req *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	sessionID := req.GetSessionId()
	if sessionID == "" {
		return &corev1.GetCommandHistoryResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   "session_id is required",
		}, nil
	}

	if _, err := s.sessionStore.Get(ctx, sessionID); err != nil {
		if oopsErr, ok := oops.AsOops(err); ok && oopsErr.Code() == "SESSION_NOT_FOUND" {
			return &corev1.GetCommandHistoryResponse{
				Meta:    responseMeta(requestID),
				Success: false,
				Error:   "session not found",
			}, nil
		}
		return nil, oops.Code("COMMAND_HISTORY_FAILED").With("session_id", sessionID).Wrap(err)
	}

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
func NewGRPCServer(tlsConfig *tls.Config) *grpc.Server {
	creds := credentials.NewTLS(tlsConfig)
	return grpc.NewServer(
		grpc.Creds(creds),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
}

// NewGRPCServerInsecure creates a new gRPC server without TLS (for testing).
// Includes a permissive keepalive enforcement policy to prevent "too_many_pings"
// rejections during long-running integration tests.
func NewGRPCServerInsecure() *grpc.Server {
	return grpc.NewServer(
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
}
