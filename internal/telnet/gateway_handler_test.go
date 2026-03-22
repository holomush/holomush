// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/core"
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

	authPlayerResp  *corev1.AuthenticatePlayerResponse
	authPlayerErr   error
	lastAuthPlayerReq *corev1.AuthenticatePlayerRequest

	selectCharResp  *corev1.SelectCharacterResponse
	selectCharErr   error
	lastSelectCharReq *corev1.SelectCharacterRequest

	createCharResp *corev1.CreateCharacterResponse
	createCharErr  error

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

func (m *mockCoreClient) AuthenticatePlayer(_ context.Context, req *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error) {
	m.lastAuthPlayerReq = req
	return m.authPlayerResp, m.authPlayerErr
}

func (m *mockCoreClient) SelectCharacter(_ context.Context, req *corev1.SelectCharacterRequest) (*corev1.SelectCharacterResponse, error) {
	m.lastSelectCharReq = req
	return m.selectCharResp, m.selectCharErr
}

func (m *mockCoreClient) CreatePlayer(_ context.Context, _ *corev1.CreatePlayerRequest) (*corev1.CreatePlayerResponse, error) {
	return &corev1.CreatePlayerResponse{}, nil
}

func (m *mockCoreClient) CreateCharacter(_ context.Context, _ *corev1.CreateCharacterRequest) (*corev1.CreateCharacterResponse, error) {
	return m.createCharResp, m.createCharErr
}

func (m *mockCoreClient) ListCharacters(_ context.Context, _ *corev1.ListCharactersRequest) (*corev1.ListCharactersResponse, error) {
	return &corev1.ListCharactersResponse{}, nil
}

func (m *mockCoreClient) RequestPasswordReset(_ context.Context, _ *corev1.RequestPasswordResetRequest) (*corev1.RequestPasswordResetResponse, error) {
	return &corev1.RequestPasswordResetResponse{}, nil
}

func (m *mockCoreClient) ConfirmPasswordReset(_ context.Context, _ *corev1.ConfirmPasswordResetRequest) (*corev1.ConfirmPasswordResetResponse, error) {
	return &corev1.ConfirmPasswordResetResponse{}, nil
}

func (m *mockCoreClient) Logout(_ context.Context, _ *corev1.LogoutRequest) (*corev1.LogoutResponse, error) {
	return &corev1.LogoutResponse{}, nil
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

func (m *mockCoreClient) GetCommandHistory(_ context.Context, _ *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	return &corev1.GetCommandHistoryResponse{Meta: &corev1.ResponseMeta{}, Success: true}, nil
}

// readLines reads exactly n lines from r, stripping \r\n.
//
//nolint:unparam // n varies in future tests
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

// mockSubscribeStream is a minimal implementation of
// grpc.ServerStreamingClient[corev1.SubscribeResponse].
type mockSubscribeStream struct {
	events []*corev1.SubscribeResponse
	idx    int
	err    error // returned after all events are consumed
}

func (m *mockSubscribeStream) Recv() (*corev1.SubscribeResponse, error) {
	if m.idx < len(m.events) {
		ev := m.events[m.idx]
		m.idx++
		return ev, nil
	}
	if m.err != nil {
		return nil, m.err
	}
	return nil, io.EOF
}

func (m *mockSubscribeStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockSubscribeStream) Trailer() metadata.MD         { return nil }
func (m *mockSubscribeStream) CloseSend() error             { return nil }
func (m *mockSubscribeStream) Context() context.Context     { return context.Background() }
func (m *mockSubscribeStream) SendMsg(any) error            { return nil }
func (m *mockSubscribeStream) RecvMsg(any) error            { return nil }

// TestGatewayHandler_SendProtoEvent_CommandResponse tests that a command_response
// event is forwarded to the client as plain text.
func TestGatewayHandler_SendProtoEvent_CommandResponse(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	payload, err := json.Marshal(core.CommandResponsePayload{Text: "Hello from server!"})
	require.NoError(t, err)

	eventStream := &mockSubscribeStream{
		events: []*corev1.SubscribeResponse{
			{Frame: &corev1.SubscribeResponse_Event{Event: &corev1.EventFrame{Type: string(core.EventTypeCommandResponse), Payload: payload}}},
		},
	}

	client := &mockCoreClient{
		authResp: &corev1.AuthenticateResponse{
			Success:       true,
			SessionId:     "sess-cr",
			ConnectionId:  "conn-cr",
			CharacterName: "CRUser",
		},
		subStream: eventStream,
		discResp:  &corev1.DisconnectResponse{Success: true},
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
	// Banner
	readLines(t, r, 2)

	_, err = clientConn.Write([]byte("connect guest\n"))
	require.NoError(t, err)
	// Welcome line
	_, err = r.ReadString('\n')
	require.NoError(t, err)

	// The event should arrive on the event channel and be forwarded.
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "Hello from server!", strings.TrimRight(line, "\r\n"))

	// Drain any remaining output (e.g. "Connection to server lost.") so the
	// handler is not blocked on a write when we cancel.
	go func() {
		for {
			_, readErr := r.ReadString('\n')
			if readErr != nil {
				return
			}
		}
	}()
	cancel()
	<-done
}

// TestGatewayHandler_SendProtoEvent_CorruptCommandResponse tests that a
// command_response event with corrupt JSON is silently dropped (no panic).
func TestGatewayHandler_SendProtoEvent_CorruptCommandResponse(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	eventStream := &mockSubscribeStream{
		events: []*corev1.SubscribeResponse{
			{Frame: &corev1.SubscribeResponse_Event{Event: &corev1.EventFrame{Type: string(core.EventTypeCommandResponse), Payload: []byte("not-valid-json")}}},
		},
	}

	client := &mockCoreClient{
		authResp: &corev1.AuthenticateResponse{
			Success:       true,
			SessionId:     "sess-corrupt",
			ConnectionId:  "conn-corrupt",
			CharacterName: "CorruptUser",
		},
		subStream: eventStream,
		discResp:  &corev1.DisconnectResponse{Success: true},
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
	readLines(t, r, 2) // banner

	_, err := clientConn.Write([]byte("connect guest\n"))
	require.NoError(t, err)
	_, err = r.ReadString('\n') // welcome
	require.NoError(t, err)

	// The corrupt event is dropped — no message forwarded. Drain any pending
	// output (e.g. "Connection to server lost.") and then cancel the context
	// to verify no panic occurred.
	go func() {
		for {
			_, readErr := r.ReadString('\n')
			if readErr != nil {
				return
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
}

// TestGatewayHandler_StreamClosed verifies that a STREAM_CLOSED control frame
// causes the handler to write the frame's message to the client and exit cleanly.
func TestGatewayHandler_StreamClosed(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	// Stream delivers a STREAM_CLOSED control frame with a "Goodbye!" message.
	eventStream := &mockSubscribeStream{
		events: []*corev1.SubscribeResponse{
			{Frame: &corev1.SubscribeResponse_Control{Control: &corev1.ControlFrame{
				Signal:  corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED,
				Message: "Goodbye!",
			}}},
		},
	}

	client := &mockCoreClient{
		authResp: &corev1.AuthenticateResponse{
			Success:       true,
			SessionId:     "sess-sc",
			ConnectionId:  "conn-sc",
			CharacterName: "SCUser",
		},
		subStream: eventStream,
		discResp:  &corev1.DisconnectResponse{Success: true},
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
	readLines(t, r, 2) // banner

	_, err := clientConn.Write([]byte("connect guest\n"))
	require.NoError(t, err)
	_, err = r.ReadString('\n') // welcome
	require.NoError(t, err)

	// The STREAM_CLOSED frame should deliver "Goodbye!" to the client.
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "Goodbye!", strings.TrimRight(line, "\r\n"))

	// Handler exits on STREAM_CLOSED — done channel should close without cancel.
	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after STREAM_CLOSED")
	}
}

// TestGatewayHandler_HandleGenericCommand_RPCError verifies that when
// HandleCommand returns an RPC-level error the client sees an error message.
func TestGatewayHandler_HandleGenericCommand_RPCError(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authResp: &corev1.AuthenticateResponse{
			Success:       true,
			SessionId:     "sess-rpc-err",
			ConnectionId:  "conn-rpc-err",
			CharacterName: "RPCErrUser",
		},
		cmdFn: func(_ context.Context, _ *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
			return nil, errors.New("transport error")
		},
		subErr:   errors.New("no subscribe"),
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
	readLines(t, r, 2)

	_, err := clientConn.Write([]byte("connect guest\n"))
	require.NoError(t, err)
	_, err = r.ReadString('\n')
	require.NoError(t, err)

	// Issue a generic command that will trigger the RPC error path.
	_, err = clientConn.Write([]byte("look\n"))
	require.NoError(t, err)

	line, err := r.ReadString('\n')
	require.NoError(t, err)
	line = strings.TrimRight(line, "\r\n")
	assert.Contains(t, strings.ToLower(line), "error")

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

// --- Two-phase auth tests ---

// twoCharList returns a pair of CharacterSummary values for use in tests.
func twoCharList() []*corev1.CharacterSummary {
	return []*corev1.CharacterSummary{
		{CharacterId: "char-alaric", CharacterName: "Alaric", HasActiveSession: false},
		{CharacterId: "char-beatrix", CharacterName: "Beatrix", HasActiveSession: false},
	}
}

// withDeadline sets a 2-second I/O deadline on conn and returns a reset function.
// Tests use this instead of goroutine-per-read to avoid concurrent reader races.
func withDeadline(t *testing.T, conn net.Conn) func() {
	t.Helper()
	require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
	return func() {
		if err := conn.SetDeadline(time.Time{}); err != nil {
			t.Logf("withDeadline reset: %v", err)
		}
	}
}

// readLinesUntil reads lines from r until one contains target, returning all
// lines read. Uses conn deadlines for timeout — no goroutine spawning.
func readLinesUntil(t *testing.T, r *bufio.Reader, target string) []string {
	t.Helper()
	var lines []string
	for {
		line, err := r.ReadString('\n')
		require.NoError(t, err, "timed out or error waiting for %q", target)
		line = strings.TrimRight(line, "\r\n")
		lines = append(lines, line)
		if strings.Contains(strings.ToLower(line), strings.ToLower(target)) {
			return lines
		}
	}
}

// TestGatewayHandler_TwoPhase_SingleCharAutoSelect verifies that when a player
// has exactly one character and it is the default, connect auto-selects it.
func TestGatewayHandler_TwoPhase_SingleCharAutoSelect(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerToken:        "tok-single",
			Characters:         []*corev1.CharacterSummary{{CharacterId: "char-one", CharacterName: "Alaric"}},
			DefaultCharacterId: "char-one",
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-single",
			CharacterName: "Alaric",
		},
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

	reset := withDeadline(t, clientConn)
	defer reset()

	r := bufio.NewReader(clientConn)
	readLines(t, r, 2) // banner

	_, err := clientConn.Write([]byte("connect alice secret\n"))
	require.NoError(t, err)

	// Should get welcome line with character name — no select-mode list.
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, strings.TrimRight(line, "\r\n"), "Alaric")

	cancel()
	<-done
}

// TestGatewayHandler_TwoPhase_MultiChar_ShowsListEntersSelectMode verifies
// that when a player has multiple characters, the list is displayed and
// the handler waits for PLAY/CREATE.
func TestGatewayHandler_TwoPhase_MultiChar_ShowsListEntersSelectMode(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:     true,
			PlayerToken: "tok-multi",
			Characters:  twoCharList(),
		},
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

	reset := withDeadline(t, clientConn)
	defer reset()

	r := bufio.NewReader(clientConn)
	readLines(t, r, 2) // banner

	_, err := clientConn.Write([]byte("connect alice secret\n"))
	require.NoError(t, err)

	// Read until the PLAY instruction line; collect all lines for assertion.
	lines := readLinesUntil(t, r, "play")

	combined := strings.Join(lines, "\n")
	assert.Contains(t, combined, "Alaric")
	assert.Contains(t, combined, "Beatrix")

	cancel()
	<-done
}

// TestGatewayHandler_TwoPhase_PlayByIndex selects a character by index number.
func TestGatewayHandler_TwoPhase_PlayByIndex(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:     true,
			PlayerToken: "tok-idx",
			Characters:  twoCharList(),
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-idx",
			CharacterName: "Alaric",
		},
		subErr:   errors.New("no subscribe"),
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

	reset := withDeadline(t, clientConn)
	defer reset()

	r := bufio.NewReader(clientConn)
	readLines(t, r, 2)

	_, err := clientConn.Write([]byte("connect alice secret\n"))
	require.NoError(t, err)

	readLinesUntil(t, r, "play")

	_, err = clientConn.Write([]byte("play 1\n"))
	require.NoError(t, err)

	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, strings.TrimRight(line, "\r\n"), "Alaric")

	// Verify the correct playerToken and characterId were sent to the server.
	require.NotNil(t, client.lastSelectCharReq)
	assert.Equal(t, "tok-idx", client.lastSelectCharReq.GetPlayerToken())
	assert.Equal(t, "char-alaric", client.lastSelectCharReq.GetCharacterId())

	cancel()
	<-done
}

// TestGatewayHandler_TwoPhase_PlayByName selects a character by name.
func TestGatewayHandler_TwoPhase_PlayByName(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:     true,
			PlayerToken: "tok-name",
			Characters:  twoCharList(),
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-name",
			CharacterName: "Beatrix",
		},
		subErr:   errors.New("no subscribe"),
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

	reset := withDeadline(t, clientConn)
	defer reset()

	r := bufio.NewReader(clientConn)
	readLines(t, r, 2)

	_, err := clientConn.Write([]byte("connect alice secret\n"))
	require.NoError(t, err)

	readLinesUntil(t, r, "play")

	_, err = clientConn.Write([]byte("play Beatrix\n"))
	require.NoError(t, err)

	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, strings.TrimRight(line, "\r\n"), "Beatrix")

	cancel()
	<-done
}

// TestGatewayHandler_TwoPhase_PlayReattach verifies that reattach shows the
// "Reattaching" message before the welcome line.
func TestGatewayHandler_TwoPhase_PlayReattach(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:     true,
			PlayerToken: "tok-reattach",
			Characters:  twoCharList(),
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-reattach",
			CharacterName: "Alaric",
			Reattached:    true,
		},
		subErr:   errors.New("no subscribe"),
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

	reset := withDeadline(t, clientConn)
	defer reset()

	r := bufio.NewReader(clientConn)
	readLines(t, r, 2)

	_, err := clientConn.Write([]byte("connect alice secret\n"))
	require.NoError(t, err)

	readLinesUntil(t, r, "play")

	_, err = clientConn.Write([]byte("play Alaric\n"))
	require.NoError(t, err)

	// Expect "Reattaching..." followed by "Welcome, Alaric!".
	lines := readLinesUntil(t, r, "welcome")
	combined := strings.ToLower(strings.Join(lines, "\n"))
	assert.Contains(t, combined, "reattach")

	cancel()
	<-done
}

// TestGatewayHandler_TwoPhase_CreateCharacter verifies CREATE <name> creates a
// character and auto-enters the game.
func TestGatewayHandler_TwoPhase_CreateCharacter(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:     true,
			PlayerToken: "tok-create",
			Characters:  twoCharList(),
		},
		createCharResp: &corev1.CreateCharacterResponse{
			Success:       true,
			CharacterId:   "char-new",
			CharacterName: "NewChar",
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-new",
			CharacterName: "NewChar",
		},
		subErr:   errors.New("no subscribe"),
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

	reset := withDeadline(t, clientConn)
	defer reset()

	r := bufio.NewReader(clientConn)
	readLines(t, r, 2)

	_, err := clientConn.Write([]byte("connect alice secret\n"))
	require.NoError(t, err)

	readLinesUntil(t, r, "play")

	_, err = clientConn.Write([]byte("create NewChar\n"))
	require.NoError(t, err)

	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, strings.TrimRight(line, "\r\n"), "NewChar")

	cancel()
	<-done
}

// TestGatewayHandler_TwoPhase_InvalidCommandInSelectMode verifies that unknown
// commands in selectMode show the help prompt.
func TestGatewayHandler_TwoPhase_InvalidCommandInSelectMode(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:     true,
			PlayerToken: "tok-inv",
			Characters:  twoCharList(),
		},
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

	reset := withDeadline(t, clientConn)
	defer reset()

	r := bufio.NewReader(clientConn)
	readLines(t, r, 2)

	_, err := clientConn.Write([]byte("connect alice secret\n"))
	require.NoError(t, err)

	readLinesUntil(t, r, "play")

	// Send a garbage command.
	_, err = clientConn.Write([]byte("look\n"))
	require.NoError(t, err)

	line, err := r.ReadString('\n')
	require.NoError(t, err)
	line = strings.TrimRight(line, "\r\n")
	assert.Contains(t, strings.ToLower(line), "play")

	cancel()
	<-done
}

// TestGatewayHandler_TwoPhase_AuthFailure verifies that a failed AuthenticatePlayer
// shows an error and does not enter selectMode.
func TestGatewayHandler_TwoPhase_AuthFailure(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:      false,
			ErrorMessage: "invalid credentials",
		},
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

	reset := withDeadline(t, clientConn)
	defer reset()

	r := bufio.NewReader(clientConn)
	readLines(t, r, 2)

	_, err := clientConn.Write([]byte("connect alice wrongpass\n"))
	require.NoError(t, err)

	line, err := r.ReadString('\n')
	require.NoError(t, err)
	line = strings.TrimRight(line, "\r\n")
	assert.Contains(t, strings.ToLower(line), "login failed")

	cancel()
	<-done
}
