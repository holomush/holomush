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
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

// WorldQuerier provides read-only access to world model data for building
// room_state payloads during event streaming. Satisfied by *world.Service.
type WorldQuerier interface {
	GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*world.Location, error)
	GetExitsByLocation(ctx context.Context, subjectID string, locationID ulid.ULID) ([]*world.Exit, error)
	GetCharactersByLocation(ctx context.Context, subjectID string, locationID ulid.ULID, opts world.ListOptions) ([]*world.Character, error)
}

// CoreServer implements the gRPC Core service.
type CoreServer struct {
	corev1.UnimplementedCoreServer

	engine          *core.Engine
	sessions        *core.SessionManager
	broadcaster     *core.Broadcaster
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

// WithWorldQuerier sets the world querier for building room_state payloads
// during event streaming (room-following).
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
func NewCoreServer(engine *core.Engine, sessions *core.SessionManager, broadcaster *core.Broadcaster, sessionStore session.Store, opts ...CoreServerOption) *CoreServer {
	s := &CoreServer{
		engine:       engine,
		sessions:     sessions,
		broadcaster:  broadcaster,
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
func (s *CoreServer) Authenticate(ctx context.Context, req *corev1.AuthRequest) (*corev1.AuthResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(ctx, "authenticate request",
		"request_id", requestID,
		"username", req.Username,
	)

	if s.authenticator == nil {
		return &corev1.AuthResponse{
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
		return &corev1.AuthResponse{
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
		return &corev1.AuthResponse{
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

	return &corev1.AuthResponse{
		Meta:          responseMeta(requestID),
		Success:       true,
		SessionId:     sessionID.String(),
		CharacterId:   result.CharacterID.String(),
		CharacterName: result.CharacterName,
		ConnectionId:  connID.String(),
	}, nil
}

// HandleCommand processes a game command.
func (s *CoreServer) HandleCommand(ctx context.Context, req *corev1.CommandRequest) (*corev1.CommandResponse, error) {
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
		return &corev1.CommandResponse{
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
		return &corev1.CommandResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &corev1.CommandResponse{
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

// eventToProto converts a core.Event to a proto Event.
func eventToProto(ev core.Event) *corev1.Event {
	return &corev1.Event{
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
func (s *CoreServer) Subscribe(req *corev1.SubscribeRequest, stream grpc.ServerStreamingServer[corev1.Event]) error {
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

	// Subscribe to requested streams.
	// Note: defers in loop are intentional - all subscriptions should be cleaned up when
	// the function exits, not at end of each iteration. The loop runs a fixed number of
	// times (len(streams)) and all deferred Unsubscribes run on function return.
	channels := make([]chan core.Event, 0, len(streams))
	for _, streamName := range streams {
		ch := s.broadcaster.Subscribe(streamName)
		channels = append(channels, ch)
		//nolint:gocritic // deferInLoop: intentional; cleanup all subscriptions on function exit
		defer s.broadcaster.Unsubscribe(streamName, ch)
	}

	// Subscribe to the character stream for room-following. Move events
	// emitted to character:<id> let the subscriber detect when the character
	// changes location and trigger a room_state update.
	charStreamName := world.CharacterStream(info.CharacterID)
	charCh := s.broadcaster.Subscribe(charStreamName)
	channels = append(channels, charCh)
	defer s.broadcaster.Unsubscribe(charStreamName, charCh)

	// Merge all channels into one
	merged := mergeChannels(ctx, channels)

	// Replay missed events if requested
	if req.ReplayFromCursor && s.eventStore != nil && len(info.EventCursors) > 0 {
		if err := s.replayMissedEvents(ctx, info, streams, merged, stream, requestID); err != nil {
			return err
		}
	}

	// Build room-following state. When a move event is detected for this
	// character, forwardLiveEvents builds and sends a room_state for the
	// new location.
	rf := &roomFollower{
		characterID:    info.CharacterID,
		currentLocID:   info.LocationID,
		worldQuerier:   s.worldQuerier,
		broadcaster:    s.broadcaster,
	}

	// Send live events until context is cancelled
	return s.forwardLiveEventsWithRoomFollow(ctx, info, merged, stream, requestID, req.SessionId, rf)
}

// replayMissedEvents replays historical events from the EventStore, then sends
// any live events that arrived during replay (with deduplication). After this
// method returns, the caller owns `merged` exclusively for live forwarding.
func (s *CoreServer) replayMissedEvents(
	ctx context.Context,
	info *session.Info,
	streams []string,
	merged <-chan core.Event,
	stream grpc.ServerStreamingServer[corev1.Event],
	requestID string,
) error {
	maxReplay := s.sessionDefaults.MaxReplay
	if maxReplay <= 0 {
		maxReplay = defaultMaxReplay
	}

	// Start drainer goroutine to capture live events while we replay.
	// Without this, the broadcaster's buffered channel (size 100) can fill
	// and block, causing event loss.
	var bufMu sync.Mutex
	var liveBuf []core.Event
	stopDrain := make(chan struct{})
	drainDone := make(chan struct{})

	go func() {
		defer close(drainDone)
		for {
			select {
			case <-stopDrain:
				return
			case <-ctx.Done():
				return
			case ev, ok := <-merged:
				if !ok {
					return
				}
				bufMu.Lock()
				liveBuf = append(liveBuf, ev)
				bufMu.Unlock()
			}
		}
	}()

	// Replay historical events from each stream
	replayedIDs := make(map[ulid.ULID]struct{})

	for _, streamName := range streams {
		cursor, hasCursor := info.EventCursors[streamName]
		if !hasCursor {
			continue
		}

		events, err := s.eventStore.Replay(ctx, streamName, cursor, maxReplay)
		if err != nil {
			slog.WarnContext(ctx, "replay failed for stream",
				"request_id", requestID,
				"stream", streamName,
				"cursor", cursor.String(),
				"error", err,
			)
			continue
		}

		for _, ev := range events {
			replayedIDs[ev.ID] = struct{}{}

			if err := stream.Send(eventToProto(ev)); err != nil {
				close(stopDrain)
				<-drainDone
				return oops.Code("SEND_FAILED").
					With("session_id", info.ID).
					With("event_id", ev.ID.String()).
					Wrap(err)
			}
			s.sessions.UpdateCursor(info.CharacterID, ev.Stream, ev.ID)
		}

		// Persist only the final cursor per stream after all replay events are sent
		// (batch update rather than per-event goroutines).
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

	// Stop the drainer and wait for it to exit
	close(stopDrain)
	<-drainDone

	// Drain any remaining events from merged (non-blocking)
drainLoop:
	for {
		select {
		case ev, ok := <-merged:
			if !ok {
				break drainLoop
			}
			liveBuf = append(liveBuf, ev)
		default:
			break drainLoop
		}
	}

	// Send buffered live events with deduplication
	for _, ev := range liveBuf {
		if _, seen := replayedIDs[ev.ID]; seen {
			continue
		}
		if err := stream.Send(eventToProto(ev)); err != nil {
			return oops.Code("SEND_FAILED").
				With("session_id", info.ID).
				With("event_id", ev.ID.String()).
				Wrap(err)
		}
		s.sessions.UpdateCursor(info.CharacterID, ev.Stream, ev.ID)
		s.persistCursorAsync(info.ID, ev.Stream, ev.ID)
	}

	return nil
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

// mergeChannels merges multiple event channels into one.
func mergeChannels(ctx context.Context, channels []chan core.Event) <-chan core.Event {
	merged := make(chan core.Event, 100)

	var wg sync.WaitGroup
	for _, ch := range channels {
		wg.Add(1)
		go func(c <-chan core.Event) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-c:
					if !ok {
						return
					}
					select {
					case merged <- event:
					case <-ctx.Done():
						return
					}
				}
			}
		}(ch)
	}

	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged
}

// NewGRPCServer creates a new gRPC server with mTLS credentials.
func NewGRPCServer(tlsConfig *tls.Config) *grpc.Server {
	creds := credentials.NewTLS(tlsConfig)
	return grpc.NewServer(grpc.Creds(creds))
}

// NewGRPCServerInsecure creates a new gRPC server without TLS (for testing).
func NewGRPCServerInsecure() *grpc.Server {
	return grpc.NewServer()
}
