// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	holoGRPC "github.com/holomush/holomush/internal/grpc"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// TestCoreClient_SatisfiedByGRPCClient verifies at compile time that
// *holoGRPC.Client implements the CoreClient interface.
func TestCoreClient_SatisfiedByGRPCClient(t *testing.T) {
	t.Helper()
	var _ CoreClient = (*holoGRPC.Client)(nil)
}

// mockCoreClient is a test double for CoreClient.
type mockCoreClient struct {
	authResp *corev1.AuthenticateResponse
	authErr  error

	cmdResp *corev1.HandleCommandResponse
	cmdErr  error
	cmdFn   func(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error)

	subStream corev1.CoreService_SubscribeClient
	subErr    error

	discResp *corev1.DisconnectResponse
	discErr  error
}

func (m *mockCoreClient) Authenticate(_ context.Context, _ *corev1.AuthenticateRequest) (*corev1.AuthenticateResponse, error) {
	return m.authResp, m.authErr
}

func (m *mockCoreClient) HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
	if m.cmdFn != nil {
		return m.cmdFn(ctx, req)
	}
	return m.cmdResp, m.cmdErr
}

func (m *mockCoreClient) Subscribe(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
	return m.subStream, m.subErr
}

func (m *mockCoreClient) Disconnect(_ context.Context, _ *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	return m.discResp, m.discErr
}

// readLines reads n lines from r, stripping \r\n.
func readLines(t *testing.T, r *bufio.Reader, n int) []string {
	t.Helper()
	lines := make([]string, 0, n)
	for range n {
		line, err := r.ReadString('\n')
		require.NoError(t, err)
		lines = append(lines, strings.TrimRight(line, "\r\n"))
	}
	return lines
}

// TestGatewayHandler_GuestConnect verifies the guest connect flow: welcome
// banner is sent, and after "connect guest" the character name is acknowledged.
func TestGatewayHandler_GuestConnect(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authResp: &corev1.AuthenticateResponse{
			Success:       true,
			SessionId:     "sess-1",
			CharacterId:   "char-1",
			CharacterName: "Guest-7",
		},
		// Prevent Subscribe goroutine from launching.
		subErr:   errors.New("no subscribe in this test"),
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := NewGatewayHandler(serverConn, client)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.Handle(ctx)
	}()

	r := bufio.NewReader(clientConn)

	// Read welcome banner (2 lines).
	banner := readLines(t, r, 2)
	assert.Equal(t, "Welcome to HoloMUSH!", banner[0])
	assert.Equal(t, "Use: connect guest", banner[1])

	// Send connect command.
	_, err := clientConn.Write([]byte("connect guest\n"))
	require.NoError(t, err)

	// Read the welcome-back line.
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	line = strings.TrimRight(line, "\r\n")
	assert.Contains(t, line, "Guest-7")

	// Disconnect cleanly. "Goodbye!" is now delivered via event stream,
	// not inline — the handler just exits.
	_, err = clientConn.Write([]byte("quit\n"))
	require.NoError(t, err)

	<-done
}

// TestGatewayHandler_SayCommand verifies that after authentication a "say"
// command is forwarded to the server. Output is no longer echoed inline — it
// arrives via broadcast events on the location stream.
func TestGatewayHandler_SayCommand(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	var receivedCmd string
	cmdCalled := make(chan struct{})
	client := &mockCoreClient{
		authResp: &corev1.AuthenticateResponse{
			Success:       true,
			SessionId:     "sess-2",
			CharacterId:   "char-2",
			CharacterName: "Tester",
		},
		cmdFn: func(_ context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
			receivedCmd = req.GetCommand()
			close(cmdCalled)
			return &corev1.HandleCommandResponse{Success: true}, nil
		},
		// Prevent Subscribe goroutine from launching.
		subErr:   errors.New("no subscribe in this test"),
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := NewGatewayHandler(serverConn, client)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.Handle(ctx)
	}()

	r := bufio.NewReader(clientConn)

	// Consume banner.
	readLines(t, r, 2)

	// Connect.
	_, err := clientConn.Write([]byte("connect guest\n"))
	require.NoError(t, err)
	// Consume welcome-back line.
	_, err = r.ReadString('\n')
	require.NoError(t, err)

	// Say something — the command is forwarded to the server.
	_, err = clientConn.Write([]byte("say Hello world\n"))
	require.NoError(t, err)

	// Wait for the command to reach the server.
	<-cmdCalled
	assert.Equal(t, "say Hello world", receivedCmd)

	// Clean up.
	cancel()
	<-done
}

// TestGatewayHandler_RejectsCommandsBeforeAuth verifies that commands sent
// before authentication are rejected with an appropriate error message.
func TestGatewayHandler_RejectsCommandsBeforeAuth(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		// No auth needed — test exercises the unauthenticated path.
		subErr:   errors.New("no subscribe in this test"),
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := NewGatewayHandler(serverConn, client)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.Handle(ctx)
	}()

	r := bufio.NewReader(clientConn)

	// Consume banner.
	readLines(t, r, 2)

	// Send say before connecting.
	_, err := clientConn.Write([]byte("say hi\n"))
	require.NoError(t, err)

	// Read error response.
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	line = strings.TrimRight(line, "\r\n")
	assert.Contains(t, strings.ToLower(line), "must connect first")

	// Clean up.
	clientConn.Close()
	<-done
}
