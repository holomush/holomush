// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package grpc provides the gRPC server implementation for HoloMUSH Core.
package grpc

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// AuthResult contains the result of a successful authentication.
type AuthResult struct {
	CharacterID   ulid.ULID
	CharacterName string
	LocationID    ulid.ULID
	IsGuest       bool
}

// Authenticator validates credentials and returns character info.
type Authenticator interface {
	Authenticate(ctx context.Context, username, password string) (*AuthResult, error)
}

// SessionDefaults configures default values for new sessions.
type SessionDefaults struct {
	TTL        time.Duration
	MaxHistory int
	MaxReplay  int
}

// defaultMaxReplay is used when MaxReplay is not configured.
const defaultMaxReplay = 1000

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
	sessions        *core.SessionManager
	authenticator   Authenticator
	sessionStore    session.Store
	eventStore      core.EventStore
	worldQuerier    WorldQuerier
	sessionDefaults SessionDefaults
	disconnectHooks []func(session.Info)

	// newSessionID is used for generating session IDs. Can be overridden for testing.
	newSessionID func() ulid.ULID
}

// CoreServerOption configures a CoreServer.
type CoreServerOption func(*CoreServer)

// WithAuthenticator sets the authenticator for the server.
func WithAuthenticator(auth Authenticator) CoreServerOption {
	return func(s *CoreServer) {
		s.authenticator = auth
	}
}

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

// NewCoreServer creates a new Core gRPC server.
func NewCoreServer(engine *core.Engine, sessions *core.SessionManager, sessionStore session.Store, opts ...CoreServerOption) *CoreServer {
	s := &CoreServer{
		engine:       engine,
		sessions:     sessions,
		sessionStore: sessionStore,
		newSessionID: core.NewULID,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Authenticate validates credentials and creates a session.
// Connection registration is intentionally omitted here — the caller (telnet
// gateway or web StreamEvents handler) registers the connection after auth so
// it can supply the correct client type and stream list.
func (s *CoreServer) Authenticate(ctx context.Context, req *corev1.AuthenticateRequest) (*corev1.AuthenticateResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(ctx, "authenticate request",
		"request_id", requestID,
		"username", req.Username,
	)

	if s.authenticator == nil {
		return &corev1.AuthenticateResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   "authentication not configured",
		}, nil
	}

	result, err := s.authenticator.Authenticate(ctx, req.Username, req.Password)
	if err != nil {
		slog.InfoContext(ctx, "authentication failed",
			"request_id", requestID,
			"username", req.Username,
			"error", err,
		)
		return &corev1.AuthenticateResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Generate session and connection IDs
	sessionID := s.newSessionID()
	connID := core.NewULID()

	// Determine client type from request (default: "terminal")
	clientType := req.ClientType
	if clientType == "" {
		clientType = "terminal"
	}

	// Store session info for command processing
	now := time.Now()
	ttlSeconds := int(s.sessionDefaults.TTL.Seconds())
	if ttlSeconds <= 0 {
		ttlSeconds = 1800 // fallback
	}
	maxHistory := s.sessionDefaults.MaxHistory
	if maxHistory <= 0 {
		maxHistory = 500 // fallback
	}

	sessionInfo := &session.Info{
		ID:            sessionID.String(),
		CharacterID:   result.CharacterID,
		CharacterName: result.CharacterName,
		LocationID:    result.LocationID,
		IsGuest:       result.IsGuest,
		Status:        session.StatusActive,
		GridPresent:   true,
		EventCursors:  map[string]ulid.ULID{},
		TTLSeconds:    ttlSeconds,
		MaxHistory:    maxHistory,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.sessionStore.Set(ctx, sessionID.String(), sessionInfo); err != nil {
		slog.ErrorContext(ctx, "failed to store session",
			"request_id", requestID,
			"session_id", sessionID.String(),
			"error", err,
		)
		return &corev1.AuthenticateResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   "session creation failed",
		}, nil
	}

	// Connect to session manager (after successful persistence to avoid orphaned state)
	s.sessions.Connect(result.CharacterID, connID)

	// Register connection with client type.
	// For the web path, StreamEvents will re-register with the correct stream list;
	// this initial registration covers the gRPC-native (telnet gateway) path.
	connInfo := &session.Connection{
		ID:          connID,
		SessionID:   sessionID.String(),
		ClientType:  clientType,
		Streams:     []string{"location:" + result.LocationID.String()},
		ConnectedAt: now,
	}
	if err := s.sessionStore.AddConnection(ctx, connInfo); err != nil {
		// Error level: connection count will be wrong, affecting lifecycle decisions.
		slog.ErrorContext(ctx, "failed to register connection",
			"request_id", requestID,
			"session_id", sessionID.String(),
			"connection_id", connID.String(),
			"error", err,
		)
	}

	// Emit arrive event (best-effort — session is valid even if event fails)
	char := core.CharacterRef{ID: result.CharacterID, Name: result.CharacterName, LocationID: result.LocationID}
	if err := s.engine.HandleConnect(ctx, char); err != nil {
		slog.WarnContext(ctx, "arrive event failed",
			"request_id", requestID,
			"session_id", sessionID.String(),
			"character_id", result.CharacterID.String(),
			"error", err,
		)
	}

	slog.InfoContext(ctx, "authentication successful",
		"request_id", requestID,
		"session_id", sessionID.String(),
		"character_id", result.CharacterID.String(),
		"character_name", result.CharacterName,
	)

	return &corev1.AuthenticateResponse{
		Meta:          responseMeta(requestID),
		Success:       true,
		SessionId:     sessionID.String(),
		CharacterId:   result.CharacterID.String(),
		CharacterName: result.CharacterName,
		ConnectionId:  connID.String(),
	}, nil
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

// executeCommand parses and executes a command. Output is delivered via
// command_response events emitted to the character's personal stream.
func (s *CoreServer) executeCommand(ctx context.Context, info *session.Info, command string) error {
	parts := strings.SplitN(command, " ", 2)
	cmd := strings.ToLower(parts[0])
	if cmd == "" {
		return oops.Code("EMPTY_COMMAND").Errorf("empty command")
	}
	var arg string
	if len(parts) > 1 {
		arg = parts[1]
	}

	char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
	switch cmd {
	case "say":
		// Sender sees the broadcast say event from the location stream — no
		// command_response needed.
		if err := s.engine.HandleSay(ctx, char, arg); err != nil {
			return oops.Code("COMMAND_FAILED").With("command", "say").Wrap(err)
		}
		return nil

	case "pose", ":":
		// Sender sees the broadcast pose event from the location stream — no
		// command_response needed.
		if err := s.engine.HandlePose(ctx, char, arg); err != nil {
			return oops.Code("COMMAND_FAILED").With("command", "pose").Wrap(err)
		}
		return nil

	case "quit":
		// Emit a command_response with "Goodbye!" before disconnect.
		s.emitCommandResponse(ctx, char, "Goodbye!", false)

		// Explicit quit — terminate session immediately
		if err := s.engine.HandleDisconnect(ctx, char, "quit"); err != nil {
			slog.WarnContext(ctx, "leave event failed", "error", err)
		}
		if err := s.sessionStore.Delete(ctx, info.ID, "Goodbye!"); err != nil {
			slog.WarnContext(ctx, "session delete failed", "error", err)
		}
		s.sessions.Disconnect(info.CharacterID, ulid.ULID{})
		s.runDisconnectHooks(ctx, *info)
		return nil

	default:
		// Emit an error command_response for unknown commands.
		s.emitCommandResponse(ctx, char, "Unknown command: "+cmd, true)
		return nil
	}
}

// emitCommandResponse emits a command_response event to the character's
// personal stream. Best-effort: errors are logged but not propagated.
func (s *CoreServer) emitCommandResponse(ctx context.Context, char core.CharacterRef, text string, isError bool) {
	payload, err := json.Marshal(core.CommandResponsePayload{
		Text:    text,
		IsError: isError,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal command_response payload",
			"character_id", char.ID.String(),
			"error", err,
		)
		return
	}

	event := core.Event{
		ID:        core.NewULID(),
		Stream:    world.CharacterStream(char.ID),
		Type:      core.EventTypeCommandResponse,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorSystem, ID: "system"},
		Payload:   payload,
	}

	if s.eventStore == nil {
		slog.Debug("emitCommandResponse: eventStore not configured, event not emitted")
		return
	}

	if err := s.eventStore.Append(ctx, event); err != nil {
		slog.WarnContext(ctx, "failed to append command_response event",
			"character_id", char.ID.String(),
			"error", err,
		)
	}
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

// persistCursorAsync persists a cursor update to the session store in a background
// goroutine (best-effort, non-blocking). Uses context.Background() intentionally:
// the request ctx may be cancelled before the goroutine runs, but we still want
// the durable cursor write to complete.
func (s *CoreServer) persistCursorAsync(sessionID, streamName string, eventID ulid.ULID) {
	go func() {
		if err := s.sessionStore.UpdateCursors(context.Background(),
			sessionID, map[string]ulid.ULID{streamName: eventID}); err != nil {
			slog.Warn("cursor persist failed", "session_id", sessionID, "error", err)
		}
	}()
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
		if ev.Type == core.EventTypeMove && strings.HasPrefix(ev.Stream, world.StreamPrefixCharacter) {
			lf.handleEvent(ctx, ev, grpcStream)
		} else if sendErr := grpcStream.Send(eventToProto(ev)); sendErr != nil {
			return last, oops.With("event_id", ev.ID.String()).Wrap(sendErr)
		}
		last = ev.ID
		s.sessions.UpdateCursor(info.CharacterID, ev.Stream, ev.ID)
	}
	if last != afterID {
		s.persistCursorAsync(info.ID, streamName, last)
	}
	return last, nil
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

	info, err := s.sessionStore.Get(ctx, req.SessionId)
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

			s.sessions.Disconnect(info.CharacterID, ulid.ULID{})
			if err := s.sessions.EndSession(info.CharacterID); err != nil {
				slog.WarnContext(ctx, "end guest session failed",
					"request_id", requestID,
					"character_id", info.CharacterID.String(),
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

			s.sessions.Disconnect(info.CharacterID, ulid.ULID{})
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
	return grpc.NewServer(grpc.Creds(creds))
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
	)
}
