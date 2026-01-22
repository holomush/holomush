// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package grpc provides the gRPC server implementation for HoloMUSH Core.
package grpc

import (
	"context"
	"crypto/tls"
	"log/slog"
	"strings"
	"sync"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// AuthResult contains the result of a successful authentication.
type AuthResult struct {
	CharacterID   ulid.ULID
	CharacterName string
	LocationID    ulid.ULID
}

// Authenticator validates credentials and returns character info.
type Authenticator interface {
	Authenticate(ctx context.Context, username, password string) (*AuthResult, error)
}

// SessionStore tracks gRPC session-to-character mappings.
type SessionStore interface {
	Get(sessionID string) (*SessionInfo, bool)
	Set(sessionID string, info *SessionInfo)
	Delete(sessionID string)
}

// SessionInfo contains information about a gRPC session.
type SessionInfo struct {
	CharacterID  ulid.ULID
	LocationID   ulid.ULID
	ConnectionID ulid.ULID
}

// InMemorySessionStore is an in-memory implementation of SessionStore.
type InMemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*SessionInfo
}

// NewInMemorySessionStore creates a new in-memory session store.
func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		sessions: make(map[string]*SessionInfo),
	}
}

// Get retrieves a session by ID.
func (s *InMemorySessionStore) Get(sessionID string) (*SessionInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info, ok := s.sessions[sessionID]
	return info, ok
}

// Set stores a session.
func (s *InMemorySessionStore) Set(sessionID string, info *SessionInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = info
}

// Delete removes a session.
func (s *InMemorySessionStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

// CoreServer implements the gRPC Core service.
type CoreServer struct {
	corev1.UnimplementedCoreServer

	engine        *core.Engine
	sessions      *core.SessionManager
	broadcaster   *core.Broadcaster
	authenticator Authenticator
	sessionStore  SessionStore

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
func WithSessionStore(store SessionStore) CoreServerOption {
	return func(s *CoreServer) {
		s.sessionStore = store
	}
}

// NewCoreServer creates a new Core gRPC server.
func NewCoreServer(engine *core.Engine, sessions *core.SessionManager, broadcaster *core.Broadcaster, opts ...CoreServerOption) *CoreServer {
	s := &CoreServer{
		engine:       engine,
		sessions:     sessions,
		broadcaster:  broadcaster,
		sessionStore: NewInMemorySessionStore(),
		newSessionID: core.NewULID,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Authenticate validates credentials and creates a session.
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

	// Connect to session manager
	s.sessions.Connect(result.CharacterID, connID)

	// Store session info for command processing
	s.sessionStore.Set(sessionID.String(), &SessionInfo{
		CharacterID:  result.CharacterID,
		LocationID:   result.LocationID,
		ConnectionID: connID,
	})

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

	// Look up session
	info, ok := s.sessionStore.Get(req.SessionId)
	if !ok {
		return &corev1.CommandResponse{
			Meta:    responseMeta(requestID),
			Success: false,
			Error:   "session not found",
		}, nil
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
func (s *CoreServer) executeCommand(ctx context.Context, info *SessionInfo, command string) (string, error) {
	parts := strings.SplitN(command, " ", 2)
	if len(parts) == 0 {
		return "", oops.Code("EMPTY_COMMAND").Errorf("empty command")
	}

	cmd := strings.ToLower(parts[0])
	var arg string
	if len(parts) > 1 {
		arg = parts[1]
	}

	switch cmd {
	case "say":
		if err := s.engine.HandleSay(ctx, info.CharacterID, info.LocationID, arg); err != nil {
			return "", oops.Code("COMMAND_FAILED").With("command", "say").Wrap(err)
		}
		return "You say: " + arg, nil

	case "pose", ":":
		if err := s.engine.HandlePose(ctx, info.CharacterID, info.LocationID, arg); err != nil {
			return "", oops.Code("COMMAND_FAILED").With("command", "pose").Wrap(err)
		}
		return "", nil

	default:
		return "", oops.Code("UNKNOWN_COMMAND").With("command", cmd).Errorf("unknown command: %s", cmd)
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
	info, ok := s.sessionStore.Get(req.SessionId)
	if !ok {
		return oops.Code("SESSION_NOT_FOUND").With("session_id", req.SessionId).Errorf("session not found")
	}

	// Subscribe to requested streams
	// Note: defers in loop are intentional - all subscriptions should be cleaned up when
	// the function exits, not at end of each iteration. The loop runs a fixed number of
	// times (len(req.Streams)) and all deferred Unsubscribes run on function return.
	channels := make([]chan core.Event, 0, len(req.Streams))
	for _, streamName := range req.Streams {
		ch := s.broadcaster.Subscribe(streamName)
		channels = append(channels, ch)
		//nolint:gocritic // deferInLoop: intentional; cleanup all subscriptions on function exit
		defer s.broadcaster.Unsubscribe(streamName, ch)
	}

	// Merge all channels into one
	merged := mergeChannels(ctx, channels)

	// Send events until context is cancelled
	for {
		select {
		case <-ctx.Done():
			slog.DebugContext(ctx, "subscription ended",
				"request_id", requestID,
				"session_id", req.SessionId,
				"reason", ctx.Err(),
			)
			return oops.Code("SUBSCRIPTION_CANCELLED").With("session_id", req.SessionId).Wrap(ctx.Err())

		case event, ok := <-merged:
			if !ok {
				return nil
			}

			// Update cursor
			s.sessions.UpdateCursor(info.CharacterID, event.Stream, event.ID)

			// Convert to proto and send
			protoEvent := &corev1.Event{
				Id:        event.ID.String(),
				Stream:    event.Stream,
				Type:      string(event.Type),
				Timestamp: timestamppb.New(event.Timestamp),
				ActorType: event.Actor.Kind.String(),
				ActorId:   event.Actor.ID,
				Payload:   event.Payload,
			}

			if err := stream.Send(protoEvent); err != nil {
				slog.WarnContext(ctx, "failed to send event",
					"request_id", requestID,
					"session_id", req.SessionId,
					"event_id", event.ID.String(),
					"error", err,
				)
				return oops.Code("SEND_FAILED").With("session_id", req.SessionId).With("event_id", event.ID.String()).Wrap(err)
			}
		}
	}
}

// Disconnect ends a session.
func (s *CoreServer) Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	requestID := ""
	if req.Meta != nil {
		requestID = req.Meta.RequestId
	}

	slog.DebugContext(ctx, "disconnect request",
		"request_id", requestID,
		"session_id", req.SessionId,
	)

	// Look up session
	info, ok := s.sessionStore.Get(req.SessionId)
	if !ok {
		// Session already gone, return success
		return &corev1.DisconnectResponse{
			Meta:    responseMeta(requestID),
			Success: true,
		}, nil
	}

	// Disconnect from session manager
	s.sessions.Disconnect(info.CharacterID, info.ConnectionID)

	// Remove from session store
	s.sessionStore.Delete(req.SessionId)

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
