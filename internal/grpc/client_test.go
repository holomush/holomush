// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	corev1 "github.com/holomush/holomush/internal/proto/holomush/core/v1"
	holomushtls "github.com/holomush/holomush/internal/tls"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	corev1.UnimplementedCoreServer

	authenticateFunc  func(context.Context, *corev1.AuthRequest) (*corev1.AuthResponse, error)
	handleCommandFunc func(context.Context, *corev1.CommandRequest) (*corev1.CommandResponse, error)
	disconnectFunc    func(context.Context, *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error)
}

func (m *mockCoreServer) Authenticate(ctx context.Context, req *corev1.AuthRequest) (*corev1.AuthResponse, error) {
	if m.authenticateFunc != nil {
		return m.authenticateFunc(ctx, req)
	}
	return &corev1.AuthResponse{
		Meta: &corev1.ResponseMeta{
			RequestId: req.GetMeta().GetRequestId(),
			Timestamp: timestamppb.Now(),
		},
		Success:       true,
		SessionId:     "test-session-id",
		CharacterId:   "test-char-id",
		CharacterName: "TestPlayer",
	}, nil
}

func (m *mockCoreServer) HandleCommand(ctx context.Context, req *corev1.CommandRequest) (*corev1.CommandResponse, error) {
	if m.handleCommandFunc != nil {
		return m.handleCommandFunc(ctx, req)
	}
	return &corev1.CommandResponse{
		Meta: &corev1.ResponseMeta{
			RequestId: req.GetMeta().GetRequestId(),
			Timestamp: timestamppb.Now(),
		},
		Success: true,
		Output:  "Command executed: " + req.GetCommand(),
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
	if err == nil {
		t.Error("NewClient() should return error for missing address")
	}
}

func TestNewClient_ConnectsToServer(t *testing.T) {
	// Start a mock server
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	server := grpc.NewServer()
	corev1.RegisterCoreServer(server, &mockCoreServer{})

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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	// Verify client is connected by making a call
	resp, err := client.Authenticate(ctx, &corev1.AuthRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "test-req-1",
			Timestamp: timestamppb.Now(),
		},
		Username: "testuser",
		Password: "testpass",
	})
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !resp.GetSuccess() {
		t.Error("Authenticate() success = false, want true")
	}
}

func TestClient_Authenticate(t *testing.T) {
	// Start a mock server
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	mockServer := &mockCoreServer{
		authenticateFunc: func(_ context.Context, req *corev1.AuthRequest) (*corev1.AuthResponse, error) {
			return &corev1.AuthResponse{
				Meta: &corev1.ResponseMeta{
					RequestId: req.GetMeta().GetRequestId(),
					Timestamp: timestamppb.Now(),
				},
				Success:       req.GetUsername() == "validuser",
				SessionId:     "session-123",
				CharacterId:   "char-456",
				CharacterName: "ValidUser",
				Error:         "",
			}, nil
		},
	}

	server := grpc.NewServer()
	corev1.RegisterCoreServer(server, mockServer)

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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	tests := []struct {
		name        string
		username    string
		wantSuccess bool
	}{
		{
			name:        "valid credentials",
			username:    "validuser",
			wantSuccess: true,
		},
		{
			name:        "invalid credentials",
			username:    "invaliduser",
			wantSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Authenticate(ctx, &corev1.AuthRequest{
				Meta: &corev1.RequestMeta{
					RequestId: "test-" + tt.name,
					Timestamp: timestamppb.Now(),
				},
				Username: tt.username,
				Password: "password",
			})
			if err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}
			if resp.GetSuccess() != tt.wantSuccess {
				t.Errorf("Authenticate() success = %v, want %v", resp.GetSuccess(), tt.wantSuccess)
			}
		})
	}
}

func TestClient_HandleCommand(t *testing.T) {
	// Start a mock server
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	server := grpc.NewServer()
	corev1.RegisterCoreServer(server, &mockCoreServer{})

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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	resp, err := client.HandleCommand(ctx, &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cmd-1",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "session-123",
		Command:   "look",
	})
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}
	if !resp.GetSuccess() {
		t.Error("HandleCommand() success = false, want true")
	}
	if resp.GetOutput() != "Command executed: look" {
		t.Errorf("HandleCommand() output = %q, want 'Command executed: look'", resp.GetOutput())
	}
}

func TestClient_Disconnect(t *testing.T) {
	// Start a mock server
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	server := grpc.NewServer()
	corev1.RegisterCoreServer(server, &mockCoreServer{})

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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	resp, err := client.Disconnect(ctx, &corev1.DisconnectRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "disc-1",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "session-123",
	})
	if err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if !resp.GetSuccess() {
		t.Error("Disconnect() success = false, want true")
	}
}

func TestClient_WithTLS(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game-123"

	// Generate CA
	ca, err := holomushtls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	// Generate server cert
	serverCert, err := holomushtls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	// Generate client cert
	clientCert, err := holomushtls.GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert() error = %v", err)
	}

	// Save certificates
	if err := holomushtls.SaveCertificates(tmpDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}
	if err := holomushtls.SaveClientCert(tmpDir, clientCert); err != nil {
		t.Fatalf("SaveClientCert() error = %v", err)
	}

	// Load server TLS config
	serverTLSConfig, err := holomushtls.LoadServerTLS(tmpDir, "core")
	if err != nil {
		t.Fatalf("LoadServerTLS() error = %v", err)
	}

	// Start TLS server
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLSConfig)))
	corev1.RegisterCoreServer(server, &mockCoreServer{})

	go func() {
		_ = server.Serve(lis)
	}()
	defer server.Stop()

	// Load client TLS config
	clientTLSConfig, err := holomushtls.LoadClientTLS(tmpDir, "gateway", gameID)
	if err != nil {
		t.Fatalf("LoadClientTLS() error = %v", err)
	}

	// Create client with TLS
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Address:   lis.Addr().String(),
		TLSConfig: clientTLSConfig,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	// Verify mTLS connection works
	resp, err := client.Authenticate(ctx, &corev1.AuthRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "tls-test",
			Timestamp: timestamppb.Now(),
		},
		Username: "testuser",
		Password: "testpass",
	})
	if err != nil {
		t.Fatalf("Authenticate() with TLS error = %v", err)
	}
	if !resp.GetSuccess() {
		t.Error("Authenticate() with TLS success = false, want true")
	}
}

func TestClient_KeepaliveConfig(t *testing.T) {
	// Start a mock server
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	server := grpc.NewServer()
	corev1.RegisterCoreServer(server, &mockCoreServer{})

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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	// Verify client works (keepalive is internal, just verify connection works)
	resp, err := client.Authenticate(ctx, &corev1.AuthRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "keepalive-test",
			Timestamp: timestamppb.Now(),
		},
		Username: "testuser",
		Password: "testpass",
	})
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !resp.GetSuccess() {
		t.Error("Authenticate() success = false, want true")
	}
}

func TestClient_CoreClient(t *testing.T) {
	// Start a mock server
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	server := grpc.NewServer()
	corev1.RegisterCoreServer(server, &mockCoreServer{})

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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	// Verify CoreClient() returns underlying client
	coreClient := client.CoreClient()
	if coreClient == nil {
		t.Error("CoreClient() returned nil")
	}

	// Use the underlying client directly
	resp, err := coreClient.Authenticate(ctx, &corev1.AuthRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "direct-test",
			Timestamp: timestamppb.Now(),
		},
		Username: "testuser",
		Password: "testpass",
	})
	if err != nil {
		t.Fatalf("CoreClient().Authenticate() error = %v", err)
	}
	if !resp.GetSuccess() {
		t.Error("CoreClient().Authenticate() success = false, want true")
	}
}

func TestClient_Subscribe(t *testing.T) {
	// Start a mock server with Subscribe implementation
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	server := grpc.NewServer()
	corev1.RegisterCoreServer(server, &mockCoreServerWithSubscribe{})

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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	// Call Subscribe
	stream, err := client.Subscribe(ctx, &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "subscribe-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "test-session",
		Streams:   []string{"location:test"},
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	// Read one event from the stream
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv() error = %v", err)
	}
	if event.GetId() == "" {
		t.Error("Received event has empty ID")
	}
}

// mockCoreServerWithSubscribe includes Subscribe implementation.
type mockCoreServerWithSubscribe struct {
	corev1.UnimplementedCoreServer
}

func (m *mockCoreServerWithSubscribe) Subscribe(_ *corev1.SubscribeRequest, stream grpc.ServerStreamingServer[corev1.Event]) error {
	// Send one test event
	if err := stream.Send(&corev1.Event{
		Id:        "test-event-1",
		Stream:    "location:test",
		Type:      "say",
		Timestamp: timestamppb.Now(),
		ActorType: "character",
		ActorId:   "char-123",
		Payload:   []byte(`{"message":"hello"}`),
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
	if err != nil {
		t.Errorf("Close() with nil conn should not error, got: %v", err)
	}
}

func TestClient_Authenticate_RPCError(t *testing.T) {
	// Start a mock server that returns an error
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	mockServer := &mockCoreServer{
		authenticateFunc: func(_ context.Context, _ *corev1.AuthRequest) (*corev1.AuthResponse, error) {
			return nil, io.EOF // Simulate RPC error
		},
	}

	server := grpc.NewServer()
	corev1.RegisterCoreServer(server, mockServer)

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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	// Call Authenticate - should get error
	_, err = client.Authenticate(ctx, &corev1.AuthRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "error-test",
			Timestamp: timestamppb.Now(),
		},
		Username: "testuser",
		Password: "testpass",
	})
	if err == nil {
		t.Error("Authenticate() should return error when RPC fails")
	}
}

func TestClient_HandleCommand_RPCError(t *testing.T) {
	// Start a mock server that returns an error
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	mockServer := &mockCoreServer{
		handleCommandFunc: func(_ context.Context, _ *corev1.CommandRequest) (*corev1.CommandResponse, error) {
			return nil, io.EOF // Simulate RPC error
		},
	}

	server := grpc.NewServer()
	corev1.RegisterCoreServer(server, mockServer)

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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	// Call HandleCommand - should get error
	_, err = client.HandleCommand(ctx, &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "error-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "test-session",
		Command:   "say hello",
	})
	if err == nil {
		t.Error("HandleCommand() should return error when RPC fails")
	}
}

func TestClient_Disconnect_RPCError(t *testing.T) {
	// Start a mock server that returns an error
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	mockServer := &mockCoreServer{
		disconnectFunc: func(_ context.Context, _ *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
			return nil, io.EOF // Simulate RPC error
		},
	}

	server := grpc.NewServer()
	corev1.RegisterCoreServer(server, mockServer)

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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	// Call Disconnect - should get error
	_, err = client.Disconnect(ctx, &corev1.DisconnectRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "error-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "test-session",
	})
	if err == nil {
		t.Error("Disconnect() should return error when RPC fails")
	}
}

func TestClient_Subscribe_StreamError(t *testing.T) {
	// Start a mock server that returns an error during streaming
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer closeWithCheck(t, lis)

	server := grpc.NewServer()
	corev1.RegisterCoreServer(server, &mockCoreServerWithSubscribeError{})

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
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer closeWithCheck(t, client)

	// Call Subscribe - connection is established
	stream, err := client.Subscribe(ctx, &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "error-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "invalid-session",
		Streams:   []string{"location:test"},
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	// The error comes when we try to receive - server returns EOF
	_, err = stream.Recv()
	if err == nil {
		t.Error("stream.Recv() should return error when server returns EOF")
	}
}

// mockCoreServerWithSubscribeError returns error for Subscribe.
type mockCoreServerWithSubscribeError struct {
	corev1.UnimplementedCoreServer
}

func (m *mockCoreServerWithSubscribeError) Subscribe(_ *corev1.SubscribeRequest, _ grpc.ServerStreamingServer[corev1.Event]) error {
	return io.EOF // Simulate error - stream ends immediately
}
