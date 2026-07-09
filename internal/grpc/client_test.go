// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	tlscerts "github.com/holomush/holomush/internal/tls"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	sceneaccessv1 "github.com/holomush/holomush/pkg/proto/holomush/sceneaccess/v1"
)

// closeWithCheck is a helper that closes an io.Closer and logs any error.
// Use with defer in tests to satisfy errcheck linter.
func closeWithCheck(t *testing.T, c io.Closer) {
	t.Helper()
	if err := c.Close(); err != nil {
		t.Logf("close error: %v", err)
	}
}

// mockCoreServer implements corev1.CoreServer for testing.
type mockCoreServer struct {
	corev1.UnimplementedCoreServiceServer

	handleCommandFunc func(context.Context, *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error)
	disconnectFunc    func(context.Context, *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error)
}

func (m *mockCoreServer) HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
	if m.handleCommandFunc != nil {
		return m.handleCommandFunc(ctx, req)
	}
	return &corev1.HandleCommandResponse{
		Meta: &corev1.ResponseMeta{
			RequestId: req.GetMeta().GetRequestId(),
			Timestamp: timestamppb.Now(),
		},
		Success: true,
	}, nil
}

func (m *mockCoreServer) Disconnect(ctx context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	if m.disconnectFunc != nil {
		return m.disconnectFunc(ctx, req)
	}
	return &corev1.DisconnectResponse{
		Meta: &corev1.ResponseMeta{
			RequestId: req.GetMeta().GetRequestId(),
			Timestamp: timestamppb.Now(),
		},
		Success: true,
	}, nil
}

func TestNewClient_MissingAddress(t *testing.T) {
	ctx := context.Background()
	_, err := NewClient(ctx, ClientConfig{})
	assert.Error(t, err, "NewClient() should return error for missing address")
}

func TestNewClient_ConnectsToServer(t *testing.T) {
	// Start a mock server
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to listen")
	defer closeWithCheck(t, lis)

	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-process test mock
	corev1.RegisterCoreServiceServer(server, &mockCoreServer{})

	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	// Create client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Address: lis.Addr().String(),
	})
	require.NoError(t, err)
	defer closeWithCheck(t, client)

	// Verify client is connected by making a call
	resp, err := client.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "test-req-1",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          "test-session",
		Command:            "look",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.GetSuccess(), "HandleCommand() success = false, want true")
}

func TestClient_HandleCommand(t *testing.T) {
	// Start a mock server
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to listen")
	defer closeWithCheck(t, lis)

	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-process test mock
	corev1.RegisterCoreServiceServer(server, &mockCoreServer{})

	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	// Create client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Address: lis.Addr().String(),
	})
	require.NoError(t, err)
	defer closeWithCheck(t, client)

	resp, err := client.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cmd-1",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          "session-123",
		Command:            "look",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.GetSuccess(), "HandleCommand() success = false, want true")
}

func TestClient_Disconnect(t *testing.T) {
	// Start a mock server
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to listen")
	defer closeWithCheck(t, lis)

	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-process test mock
	corev1.RegisterCoreServiceServer(server, &mockCoreServer{})

	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	// Create client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Address: lis.Addr().String(),
	})
	require.NoError(t, err)
	defer closeWithCheck(t, client)

	resp, err := client.Disconnect(ctx, &corev1.DisconnectRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "disc-1",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "session-123",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetSuccess(), "Disconnect() success = false, want true")
}

func TestClient_WithTLS(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game-123"

	// Generate CA
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "GenerateCA() error")

	// Generate server cert
	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "GenerateServerCert() error")

	// Generate client cert
	clientCert, err := tlscerts.GenerateClientCert(ca, "gateway")
	require.NoError(t, err, "GenerateClientCert() error")

	// Save certificates
	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "SaveCertificates() error")
	err = tlscerts.SaveClientCert(tmpDir, clientCert)
	require.NoError(t, err, "SaveClientCert() error")

	// Load server TLS config
	serverTLSConfig, err := tlscerts.LoadServerTLS(tmpDir, "core")
	require.NoError(t, err, "LoadServerTLS() error")

	// Start TLS server
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to listen")
	defer closeWithCheck(t, lis)

	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLSConfig)))
	corev1.RegisterCoreServiceServer(server, &mockCoreServer{})

	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	// Load client TLS config
	clientTLSConfig, err := tlscerts.LoadClientTLS(tmpDir, "gateway", gameID)
	require.NoError(t, err, "LoadClientTLS() error")

	// Create client with TLS
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Address:   lis.Addr().String(),
		TLSConfig: clientTLSConfig,
	})
	require.NoError(t, err)
	defer closeWithCheck(t, client)

	// Verify mTLS connection works
	resp, err := client.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "tls-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          "test-session",
		Command:            "look",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err, "HandleCommand() with TLS error")
	assert.True(t, resp.GetSuccess(), "HandleCommand() with TLS success = false, want true")
}

func TestClient_KeepaliveConfig(t *testing.T) {
	// Start a mock server
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to listen")
	defer closeWithCheck(t, lis)

	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-process test mock
	corev1.RegisterCoreServiceServer(server, &mockCoreServer{})

	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	// Create client with custom keepalive settings
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Address:          lis.Addr().String(),
		KeepaliveTime:    20 * time.Second,
		KeepaliveTimeout: 10 * time.Second,
	})
	require.NoError(t, err)
	defer closeWithCheck(t, client)

	// Verify client works (keepalive is internal, just verify connection works)
	resp, err := client.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "keepalive-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          "test-session",
		Command:            "look",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.GetSuccess(), "HandleCommand() success = false, want true")
}

func TestClient_CoreClient(t *testing.T) {
	// Start a mock server
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to listen")
	defer closeWithCheck(t, lis)

	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-process test mock
	corev1.RegisterCoreServiceServer(server, &mockCoreServer{})

	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	// Create client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Address: lis.Addr().String(),
	})
	require.NoError(t, err)
	defer closeWithCheck(t, client)

	// Verify CoreClient() returns underlying client
	coreClient := client.CoreClient()
	require.NotNil(t, coreClient, "CoreClient() returned nil")

	// Use the underlying client directly
	resp, err := coreClient.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "direct-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          "test-session",
		Command:            "look",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err, "CoreClient().HandleCommand() error")
	assert.True(t, resp.GetSuccess(), "CoreClient().HandleCommand() success = false, want true")
}

func TestClient_Subscribe(t *testing.T) {
	// Start a mock server with Subscribe implementation
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to listen")
	defer closeWithCheck(t, lis)

	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-process test mock
	corev1.RegisterCoreServiceServer(server, &mockCoreServerWithSubscribe{})

	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	// Create client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Address: lis.Addr().String(),
	})
	require.NoError(t, err)
	defer closeWithCheck(t, client)

	// Call Subscribe
	stream, err := client.Subscribe(ctx, &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "subscribe-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "test-session",
	})
	require.NoError(t, err)

	// Read one event from the stream
	event, err := stream.Recv()
	require.NoError(t, err, "stream.Recv() error")
	assert.NotEmpty(t, event.GetEvent().GetId(), "Received event has empty ID")
}

// mockCoreServerWithSubscribe includes Subscribe implementation.
type mockCoreServerWithSubscribe struct {
	corev1.UnimplementedCoreServiceServer
}

func (m *mockCoreServerWithSubscribe) Subscribe(_ *corev1.SubscribeRequest, stream grpc.ServerStreamingServer[corev1.SubscribeResponse]) error {
	// Send one test event
	if err := stream.Send(&corev1.SubscribeResponse{
		Frame: &corev1.SubscribeResponse_Event{
			Event: &corev1.EventFrame{
				Id:        "test-event-1",
				Stream:    "location:test",
				Type:      "say",
				Timestamp: timestamppb.Now(),
				ActorType: "character",
				ActorId:   "char-123",
				Payload:   []byte(`{"message":"hello"}`),
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to send event: %w", err)
	}
	return nil
}

func TestClient_Close_NilConn(t *testing.T) {
	// Create a client with nil connection (simulated)
	client := &Client{
		conn:   nil,
		client: nil,
	}

	err := client.Close()
	assert.NoError(t, err, "Close() with nil conn should not error")
}

func TestClient_HandleCommand_RPCError(t *testing.T) {
	// Start a mock server that returns an error
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to listen")
	defer closeWithCheck(t, lis)

	mockServer := &mockCoreServer{
		handleCommandFunc: func(_ context.Context, _ *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
			return nil, io.EOF // Simulate RPC error
		},
	}

	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-process test mock
	corev1.RegisterCoreServiceServer(server, mockServer)

	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	// Create client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Address: lis.Addr().String(),
	})
	require.NoError(t, err)
	defer closeWithCheck(t, client)

	// Call HandleCommand - should get error
	_, err = client.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "error-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          "test-session",
		Command:            "say hello",
		PlayerSessionToken: testPlayerSessionToken,
	})
	assert.Error(t, err, "HandleCommand() should return error when RPC fails")
}

func TestClient_Disconnect_RPCError(t *testing.T) {
	// Start a mock server that returns an error
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to listen")
	defer closeWithCheck(t, lis)

	mockServer := &mockCoreServer{
		disconnectFunc: func(_ context.Context, _ *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
			return nil, io.EOF // Simulate RPC error
		},
	}

	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-process test mock
	corev1.RegisterCoreServiceServer(server, mockServer)

	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	// Create client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Address: lis.Addr().String(),
	})
	require.NoError(t, err)
	defer closeWithCheck(t, client)

	// Call Disconnect - should get error
	_, err = client.Disconnect(ctx, &corev1.DisconnectRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "error-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "test-session",
	})
	assert.Error(t, err, "Disconnect() should return error when RPC fails")
}

func TestClient_Subscribe_StreamError(t *testing.T) {
	// Start a mock server that returns an error during streaming
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "failed to listen")
	defer closeWithCheck(t, lis)

	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-process test mock
	corev1.RegisterCoreServiceServer(server, &mockCoreServerWithSubscribeError{})

	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	// Create client
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Address: lis.Addr().String(),
	})
	require.NoError(t, err)
	defer closeWithCheck(t, client)

	// Call Subscribe - connection is established
	stream, err := client.Subscribe(ctx, &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "error-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "invalid-session",
	})
	require.NoError(t, err)

	// The error comes when we try to receive - server returns EOF
	_, err = stream.Recv()
	assert.Error(t, err, "stream.Recv() should return error when server returns EOF")
}

// mockCoreServerWithSubscribeError returns error for Subscribe.
type mockCoreServerWithSubscribeError struct {
	corev1.UnimplementedCoreServiceServer
}

func (m *mockCoreServerWithSubscribeError) Subscribe(_ *corev1.SubscribeRequest, _ grpc.ServerStreamingServer[corev1.SubscribeResponse]) error {
	return io.EOF // Simulate error - stream ends immediately
}

// TestTranslateCheckPlayerSessionErrPreservesAuthFailureAcrossWire asserts the
// client-side error translator (Task 6.5) re-injects the
// PLAYER_SESSION_NOT_FOUND oops code on codes.Unauthenticated, so the
// gateway's cookie-collision gate predicate can see auth failures across the
// gRPC boundary instead of the generic RPC_FAILED wrap.
func TestTranslateCheckPlayerSessionErrPreservesAuthFailureAcrossWire(t *testing.T) {
	tests := []struct {
		name         string
		input        error
		expectedCode string
	}{
		{
			name:         "codes.Unauthenticated translates to PLAYER_SESSION_NOT_FOUND oops code",
			input:        status.Error(codes.Unauthenticated, "PLAYER_SESSION_NOT_FOUND: unknown token"),
			expectedCode: "PLAYER_SESSION_NOT_FOUND",
		},
		{
			name:         "codes.NotFound falls through to RPC_FAILED",
			input:        status.Error(codes.NotFound, "not found"),
			expectedCode: "RPC_FAILED",
		},
		{
			name:         "non-status error falls through to RPC_FAILED",
			input:        errors.New("transport flake"),
			expectedCode: "RPC_FAILED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := translateCheckPlayerSessionErr(tt.input)
			require.Error(t, err)
			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok, "translator must always return an oops error")
			assert.Equal(t, tt.expectedCode, oopsErr.Code())
		})
	}
}

// TestNewClientWrapsURLParseFailureAsConnectionFailed pins the defensive
// branch where grpc.NewClient itself rejects the address (URL-parse error).
// In production this branch should never fire — addresses come from validated
// config — but the wrap matters when it does so callers see a CONNECTION_FAILED
// oops code rather than the raw grpc parse error. \x00 is rejected by net/url's
// invalid-control-character check; this is the only consistent way to provoke
// grpc.NewClient into returning an error rather than deferring to first-RPC.
func TestNewClientWrapsURLParseFailureAsConnectionFailed(t *testing.T) {
	ctx := context.Background()

	_, err := NewClient(ctx, ClientConfig{Address: "\x00"})

	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "constructor failure must surface as an oops error")
	assert.Equal(t, "CONNECTION_FAILED", oopsErr.Code())
}

// TestClientSubscribeWrapsImmediateRPCErrorAsRPCFailed pins the synchronous
// error path on Client.Subscribe — when the server-side handler returns an
// error before any frame is sent, c.client.Subscribe returns the error from
// the initial RPC dispatch and the wrapper MUST surface RPC_FAILED. The
// existing TestClient_Subscribe_StreamError covers the streaming-error case
// (error surfaces from stream.Recv after Subscribe returns nil); this test
// covers the case where Subscribe itself returns the error.
//
// In practice grpc-go's server-streaming dispatch typically returns
// (nonNilStream, nil) and surfaces handler errors on the next Recv. To force
// the synchronous-error shape that this branch handles, drive the wrapper
// with a fake gRPC client whose Subscribe returns (nil, err) directly.
func TestClientSubscribeWrapsImmediateRPCErrorAsRPCFailed(t *testing.T) {
	ctx := context.Background()

	wrapper := &Client{
		client: &fakeCoreClient{
			subscribeErr: status.Error(codes.Internal, "synthetic dispatch failure"),
		},
	}

	_, err := wrapper.Subscribe(ctx, &corev1.SubscribeRequest{SessionId: "s"})

	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "Subscribe error must surface as oops")
	assert.Equal(t, "RPC_FAILED", oopsErr.Code())
}

// TestTranslateSubscribeErrClassifiesWireCodes is the regression guard for the
// rsoe6.11.1 P0: a reaped session crosses the Subscribe wire as
// codes.Unauthenticated (or codes.NotFound) and MUST decode back to the
// SESSION_NOT_FOUND oops code so the gateways treat it as terminal; every other
// status code (and non-status errors) MUST stay RPC_FAILED so a transient
// core-down is retried. This pins the fix at its source: the server stamps the
// wire code via subscribeSessionNotFound, and this translator decodes it.
func TestTranslateSubscribeErrClassifiesWireCodes(t *testing.T) {
	tests := []struct {
		name         string
		input        error
		expectedCode string
	}{
		{
			name:         "codes.Unauthenticated decodes to SESSION_NOT_FOUND (reaped session, terminal)",
			input:        status.Error(codes.Unauthenticated, "session not found"),
			expectedCode: "SESSION_NOT_FOUND",
		},
		{
			name:         "codes.NotFound decodes to SESSION_NOT_FOUND (terminal)",
			input:        status.Error(codes.NotFound, "session not found"),
			expectedCode: "SESSION_NOT_FOUND",
		},
		{
			name:         "codes.Unavailable stays RPC_FAILED (transient core-down, retry)",
			input:        status.Error(codes.Unavailable, "connection refused"),
			expectedCode: "RPC_FAILED",
		},
		{
			name:         "codes.Unknown stays RPC_FAILED (transient)",
			input:        status.Error(codes.Unknown, "boom"),
			expectedCode: "RPC_FAILED",
		},
		{
			name:         "non-status error stays RPC_FAILED",
			input:        errors.New("transport flake"),
			expectedCode: "RPC_FAILED",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TranslateSubscribeErr(tt.input)
			require.Error(t, got)
			oopsErr, ok := oops.AsOops(got)
			require.True(t, ok, "translator must always return an oops error")
			assert.Equal(t, tt.expectedCode, oopsErr.Code())
		})
	}
}

// TestSubscribeSessionNotFoundStampsUnauthenticatedWireCode pins the server
// half of the rsoe6.11.1 fix: subscribeSessionNotFound MUST carry a gRPC status
// code on the wire (codes.Unauthenticated) rather than a bare oops error, which
// grpc-go would surface to the client as codes.Unknown — indistinguishable from
// a transient fault and thus undecodable by TranslateSubscribeErr.
func TestSubscribeSessionNotFoundStampsUnauthenticatedWireCode(t *testing.T) {
	err := subscribeSessionNotFound("test-session")
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "SESSION_NOT_FOUND must be a gRPC status error so the wire code is classifiable")
	assert.Equal(t, codes.Unauthenticated, st.Code(),
		"SESSION_NOT_FOUND must cross the wire as Unauthenticated so the gateway decodes it as terminal")
	// Round-trip: the wire code must decode back to the SESSION_NOT_FOUND oops
	// code via the client translator.
	decoded := TranslateSubscribeErr(err)
	oopsErr, ok := oops.AsOops(decoded)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", oopsErr.Code(),
		"server wire code MUST round-trip back to SESSION_NOT_FOUND through the client translator")
}

// fakeCoreClient is a minimal corev1.CoreServiceClient that lets a single
// test exercise the wrapper-level error branch on Client.Subscribe without
// spinning up a real grpc.Server. Only Subscribe is implemented; other
// methods panic if invoked, which would surface a test boundary violation.
type fakeCoreClient struct {
	corev1.CoreServiceClient
	subscribeErr error
}

func (f *fakeCoreClient) Subscribe(_ context.Context, _ *corev1.SubscribeRequest, _ ...grpc.CallOption) (corev1.CoreService_SubscribeClient, error) {
	return nil, f.subscribeErr
}

// fakeSceneAccessClient embeds the generated SceneAccessServiceClient (nil) and
// overrides only the two methods the mute/notify client stubs call. Un-overridden
// methods panic if invoked — these tests never call them.
type fakeSceneAccessClient struct {
	sceneaccessv1.SceneAccessServiceClient
	muteResp   *sceneaccessv1.MuteSceneResponse
	muteErr    error
	notifyResp *sceneaccessv1.SetSceneNotifyPrefResponse
	notifyErr  error
}

func (f *fakeSceneAccessClient) MuteScene(_ context.Context, _ *sceneaccessv1.MuteSceneRequest, _ ...grpc.CallOption) (*sceneaccessv1.MuteSceneResponse, error) {
	return f.muteResp, f.muteErr
}

func (f *fakeSceneAccessClient) SetSceneNotifyPref(_ context.Context, _ *sceneaccessv1.SetSceneNotifyPrefRequest, _ ...grpc.CallOption) (*sceneaccessv1.SetSceneNotifyPrefResponse, error) {
	return f.notifyResp, f.notifyErr
}

// TestClientMuteSceneForwardsAndWrapsError proves the BFF client stub forwards
// a MuteScene call to the facade and wraps a facade error with oops.Code("RPC_FAILED").
func TestClientMuteSceneForwardsAndWrapsError(t *testing.T) {
	t.Run("forwards to facade and returns response on success", func(t *testing.T) {
		c := &Client{sceneAccessClient: &fakeSceneAccessClient{muteResp: &sceneaccessv1.MuteSceneResponse{}}}
		resp, err := c.MuteScene(context.Background(), &sceneaccessv1.MuteSceneRequest{SceneId: "s1", Muted: true})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("wraps facade error as RPC_FAILED", func(t *testing.T) {
		c := &Client{sceneAccessClient: &fakeSceneAccessClient{muteErr: status.Error(codes.PermissionDenied, "not a participant")}}
		_, err := c.MuteScene(context.Background(), &sceneaccessv1.MuteSceneRequest{SceneId: "s1", Muted: true})
		require.Error(t, err)
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, "RPC_FAILED", oopsErr.Code())
	})
}

// TestClientSetSceneNotifyPrefForwardsAndWrapsError proves the notify-pref client
// stub forwards to the facade and wraps a facade error with oops.Code("RPC_FAILED").
func TestClientSetSceneNotifyPrefForwardsAndWrapsError(t *testing.T) {
	t.Run("forwards to facade and returns response on success", func(t *testing.T) {
		c := &Client{sceneAccessClient: &fakeSceneAccessClient{notifyResp: &sceneaccessv1.SetSceneNotifyPrefResponse{}}}
		resp, err := c.SetSceneNotifyPref(context.Background(), &sceneaccessv1.SetSceneNotifyPrefRequest{Enabled: false})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("wraps facade error as RPC_FAILED", func(t *testing.T) {
		c := &Client{sceneAccessClient: &fakeSceneAccessClient{notifyErr: status.Error(codes.Internal, "boom")}}
		_, err := c.SetSceneNotifyPref(context.Background(), &sceneaccessv1.SetSceneNotifyPrefRequest{Enabled: false})
		require.Error(t, err)
		oopsErr, ok := oops.AsOops(err)
		require.True(t, ok)
		assert.Equal(t, "RPC_FAILED", oopsErr.Code())
	})
}
