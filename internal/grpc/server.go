// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package grpc provides the gRPC server implementation for HoloMUSH Core.
package grpc

import (
	"context"
	"crypto/tls"
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

	engine        *core.Engine
	sessions      *core.SessionManager
	authenticator Authenticator
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
	output, err := s.executeCommand(ctx, info, req.Command)
	if err != nil {
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
		Output:  output,
	}, nil
}

// executeCommand parses and executes a command.
func (s *CoreServer) executeCommand(ctx context.Context, info *session.Info, command string) (string, error) {
	parts := strings.SplitN(command, " ", 2)
	if len(parts) == 0 {
		return "", oops.Code("EMPTY_COMMAND").Errorf("empty command")
	}

	cmd := strings.ToLower(parts[0])
	var arg string
	if len(parts) > 1 {
		arg = parts[1]
	}

	char := core.CharacterRef{ID: info.CharacterID, Name: info.CharacterName, LocationID: info.LocationID}
	switch cmd {
	case "say":
		if err := s.engine.HandleSay(ctx, char, arg); err != nil {
			return "", oops.Code("COMMAND_FAILED").With("command", "say").Wrap(err)
		}
		return "You say: " + arg, nil

	case "pose", ":":
		if err := s.engine.HandlePose(ctx, char, arg); err != nil {
			return "", oops.Code("COMMAND_FAILED").With("command", "pose").Wrap(err)
		}
		return "", nil

	case "quit":
		// Explicit quit — terminate session immediately
		if err := s.engine.HandleDisconnect(ctx, char, "quit"); err != nil {
			slog.WarnContext(ctx, "leave event failed", "error", err)
		}
		if err := s.sessionStore.Delete(ctx, info.ID); err != nil {
			slog.WarnContext(ctx, "session delete failed", "error", err)
		}
		s.sessions.Disconnect(info.CharacterID, ulid.ULID{})
		s.runDisconnectHooks(ctx, *info)
		return "Goodbye!", nil

	default:
		return "", oops.Code("UNKNOWN_COMMAND").With("command", cmd).Errorf("unknown command: %s", cmd)
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

// eventToProto converts a core.Event to a proto Event.
func eventToProto(ev core.Event) *corev1.SubscribeResponse {
	return &corev1.SubscribeResponse{
		Id:        ev.ID.String(),
		Stream:    ev.Stream,
		Type:      string(ev.Type),
		Timestamp: timestamppb.New(ev.Timestamp),
		ActorType: ev.Actor.Kind.String(),
		ActorId:   ev.Actor.ID,
		Payload:   ev.Payload,
	}
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

	// Look up session
	info, err := s.sessionStore.Get(ctx, req.SessionId)
	if err != nil {
		return oops.Code("SESSION_NOT_FOUND").With("session_id", req.SessionId).Errorf("session not found")
	}

	// Default to the session's location stream when none specified.
	streams := req.Streams
	if len(streams) == 0 {
		streams = []string{"location:" + info.LocationID.String()}
	}

	// Separate the location stream from other streams so the location
	// subscription can be dynamically swapped when the character moves.
	locStreamName := ""
	var otherStreams []string
	for _, streamName := range streams {
		if strings.HasPrefix(streamName, world.StreamPrefixLocation) {
			locStreamName = streamName
		} else {
			otherStreams = append(otherStreams, streamName)
		}
	}

	// Build allStreams: other streams + location stream (avoid append aliasing).
	allStreams := make([]string, 0, len(otherStreams)+1)
	allStreams = append(allStreams, otherStreams...)
	if locStreamName != "" {
		allStreams = append(allStreams, locStreamName)
	}

	// Always include the character stream for location-following. Move events
	// emitted to character:<id> let the subscriber detect when the character
	// changes location and trigger a location_state update.
	charStreamName := world.CharacterStream(info.CharacterID)
	allStreams = append(allStreams, charStreamName)

	// Shared channels for notification-driven live events.
	notifyCh := make(chan streamNotification, 100)
	errCh := make(chan error, len(allStreams))

	// Subscribe to each stream via eventStore and start relay goroutines.
	// IMPORTANT: subscriptions are set up BEFORE replay to guarantee no
	// events are missed. Spurious notifications for already-replayed events
	// result in empty Replay responses (harmless).
	for _, streamName := range allStreams {
		eventCh, subErrCh, subErr := s.eventStore.Subscribe(ctx, streamName)
		if subErr != nil {
			return oops.Code("SUBSCRIBE_FAILED").
				With("session_id", req.SessionId).
				With("stream", streamName).
				Wrap(subErr)
		}

		startNotificationRelay(ctx, streamName, eventCh, notifyCh)

		// Forward subscription errors.
		go func(sCh <-chan error) {
			select {
			case e, ok := <-sCh:
				if ok && e != nil {
					select {
					case errCh <- e:
					default:
					}
				}
			case <-ctx.Done():
			}
		}(subErrCh)
	}

	// Inject synthetic location_state so the client always has location context
	// on connect/reconnect, regardless of event replay cursor position.
	if s.worldQuerier != nil && !info.LocationID.IsZero() {
		syntheticLF := &locationFollower{
			characterID:  info.CharacterID,
			currentLocID: info.LocationID,
			worldQuerier: s.worldQuerier,
			sessionStore: s.sessionStore,
		}
		if locState, rsErr := syntheticLF.buildLocationState(ctx, info.LocationID); rsErr != nil {
			slog.WarnContext(ctx, "failed to build synthetic location_state",
				"request_id", requestID,
				"session_id", req.SessionId,
				"location_id", info.LocationID.String(),
				"error", rsErr,
			)
		} else {
			if sendErr := stream.Send(locState); sendErr != nil {
				return oops.Code("SEND_FAILED").With("session_id", req.SessionId).Wrap(sendErr)
			}
		}
	}

	// Replay missed events from session cursors.
	lastSentID := make(map[string]ulid.ULID)
	if req.ReplayFromCursor && s.eventStore != nil && len(info.EventCursors) > 0 {
		replayStreams := make([]string, 0, len(streams)+1)
		replayStreams = append(replayStreams, streams...)
		replayStreams = append(replayStreams, charStreamName)

		for _, streamName := range replayStreams {
			cursor, hasCursor := info.EventCursors[streamName]
			if !hasCursor {
				continue
			}

			events, replayErr := s.eventStore.Replay(ctx, streamName, cursor, s.maxReplay())
			if replayErr != nil {
				slog.WarnContext(ctx, "replay failed for stream",
					"request_id", requestID,
					"stream", streamName,
					"cursor", cursor.String(),
					"error", replayErr,
				)
				continue
			}

			for _, ev := range events {
				if sendErr := stream.Send(eventToProto(ev)); sendErr != nil {
					return oops.Code("SEND_FAILED").
						With("session_id", info.ID).
						With("event_id", ev.ID.String()).
						Wrap(sendErr)
				}
				s.sessions.UpdateCursor(info.CharacterID, ev.Stream, ev.ID)
				lastSentID[ev.Stream] = ev.ID
			}

			if len(events) > 0 {
				last := events[len(events)-1]
				s.persistCursorAsync(info.ID, streamName, last.ID)
			}

			slog.DebugContext(ctx, "replayed events",
				"request_id", requestID,
				"stream", streamName,
				"count", len(events),
			)
		}
	}

	// Build location-following state. When a move event is detected for this
	// character, forwardLiveEvents builds and sends a location_state for the
	// new location and switches the location subscription.
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

	// Send live events until context is cancelled
	return s.forwardLiveEvents(ctx, info, notifyCh, errCh, stream, requestID, req.SessionId, lf, lastSentID)
}

// forwardLiveEvents reads from the notification channel and replays events
// from the event store. The notification-driven approach means we only fetch
// events when the store tells us something new arrived.
func (s *CoreServer) forwardLiveEvents(
	ctx context.Context,
	info *session.Info,
	notifyCh <-chan streamNotification,
	errCh <-chan error,
	stream grpc.ServerStreamingServer[corev1.SubscribeResponse],
	requestID string,
	sessionID string,
	lf *locationFollower,
	lastSentID map[string]ulid.ULID,
) error {
	for {
		select {
		case <-ctx.Done():
			slog.DebugContext(ctx, "subscription ended",
				"request_id", requestID,
				"session_id", sessionID,
				"reason", ctx.Err(),
			)
			if ctx.Err() == context.Canceled {
				return nil // normal client disconnect
			}
			return oops.Code("SUBSCRIPTION_CANCELLED").With("session_id", sessionID).Wrap(ctx.Err())

		case subErr := <-errCh:
			return oops.Code("SUBSCRIPTION_ERROR").With("session_id", sessionID).Wrap(subErr)

		case notif := <-notifyCh:
			afterID := lastSentID[notif.stream]
			events, replayErr := s.eventStore.Replay(ctx, notif.stream, afterID, s.maxReplay())
			if replayErr != nil {
				slog.WarnContext(ctx, "live replay failed",
					"request_id", requestID,
					"stream", notif.stream,
					"after_id", afterID.String(),
					"error", replayErr,
				)
				continue
			}

			for _, event := range events {
				// Character-stream move events: handle location-following
				// but don't forward (the location-stream copy is forwarded).
				if event.Type == core.EventTypeMove && strings.HasPrefix(event.Stream, world.StreamPrefixCharacter) {
					lf.handleEvent(ctx, event, stream)
					s.sessions.UpdateCursor(info.CharacterID, event.Stream, event.ID)
					s.persistCursorAsync(sessionID, event.Stream, event.ID)
					lastSentID[event.Stream] = event.ID
					continue
				}

				if sendErr := stream.Send(eventToProto(event)); sendErr != nil {
					slog.WarnContext(ctx, "failed to send event",
						"request_id", requestID,
						"session_id", sessionID,
						"event_id", event.ID.String(),
						"error", sendErr,
					)
					return oops.Code("SEND_FAILED").With("session_id", sessionID).With("event_id", event.ID.String()).Wrap(sendErr)
				}

				s.sessions.UpdateCursor(info.CharacterID, event.Stream, event.ID)
				s.persistCursorAsync(sessionID, event.Stream, event.ID)
				lastSentID[event.Stream] = event.ID
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

			if err := s.sessionStore.Delete(ctx, req.SessionId); err != nil {
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
