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
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/gatewaymetrics"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/telnet/gamenotice"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// testRenderings maps the event types these tests exercise to the
// rendering metadata that the core process's RenderingPublisher would
// otherwise stamp on outbound events at emit time. The gateway no
// longer holds a local VerbRegistry — rendering arrives on the wire
// via EventFrame.Rendering. Tests use withRendering to populate it.
var testRenderings = map[string]*corev1.RenderingMetadata{
	"core-communication:say":            {Category: "communication", Format: "speech", Label: "says", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:pose":           {Category: "communication", Format: "action", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:page":           {Category: "communication", Format: "speech", Label: "pages", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:whisper":        {Category: "communication", Format: "speech", Label: "whispers", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:whisper_notice": {Category: "communication", Format: "action", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:ooc":            {Category: "communication", Format: "action", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:pemit":          {Category: "command", Format: "narrative", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},

	// Host-owned builtins (registered by BootstrapVerbRegistry in production).
	"arrive":           {Category: "movement", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_BOTH, SourcePlugin: "builtin"},
	"leave":            {Category: "movement", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_BOTH, SourcePlugin: "builtin"},
	"system":           {Category: "system", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "builtin"},
	"command_response": {Category: "command", Format: "narrative", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "builtin"},
	"command_error":    {Category: "command", Format: "error", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "builtin"},
	"location_state":   {Category: "state", Format: "snapshot", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_STATE, SourcePlugin: "builtin"},
}

// withRendering populates ev.Rendering from testRenderings (if present).
// Tests use this to simulate the core process's RenderingPublisher.
func withRendering(ev *corev1.EventFrame) *corev1.EventFrame {
	if ev.Rendering != nil {
		return ev
	}
	if r, ok := testRenderings[ev.GetType()]; ok {
		ev.Rendering = r
	}
	return ev
}

// newTestHandler wraps NewGatewayHandler with DefaultLimits so existing
// tests remain a single line. Tests that need custom limits call
// NewGatewayHandler directly.
func newTestHandler(conn net.Conn, client CoreClient) *GatewayHandler {
	return NewGatewayHandler(conn, client, DefaultLimits)
}

// TestCoreClient_SatisfiedByGRPCClient verifies at compile time that
// *holoGRPC.Client implements the CoreClient interface.
func TestCoreClient_SatisfiedByGRPCClient(t *testing.T) {
	t.Helper()
	var _ CoreClient = (*holoGRPC.Client)(nil)
}

// mockCoreClient is a test double for CoreClient.
type mockCoreClient struct {
	createGuestResp *corev1.CreateGuestResponse
	createGuestErr  error

	authPlayerResp    *corev1.AuthenticatePlayerResponse
	authPlayerErr     error
	lastAuthPlayerReq *corev1.AuthenticatePlayerRequest

	selectCharResp    *corev1.SelectCharacterResponse
	selectCharErr     error
	lastSelectCharReq *corev1.SelectCharacterRequest

	createCharResp *corev1.CreateCharacterResponse
	createCharErr  error

	cmdResp              *corev1.HandleCommandResponse
	cmdErr               error
	cmdFn                func(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error)
	lastHandleCommandReq *corev1.HandleCommandRequest

	subStream        corev1.CoreService_SubscribeClient
	subErr           error
	subscribeFn      func(ctx context.Context, req *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error)
	lastSubscribeReq *corev1.SubscribeRequest

	discResp          *corev1.DisconnectResponse
	discErr           error
	lastDisconnectReq *corev1.DisconnectRequest

	logoutResp    *corev1.LogoutResponse
	logoutErr     error
	logoutCalled  bool
	lastLogoutReq *corev1.LogoutRequest

	listCharResp *corev1.ListCharactersResponse
	listCharErr  error

	refreshResp    *corev1.RefreshConnectionResponse
	refreshErr     error
	refreshCalls   atomic.Int32
	lastRefreshReq atomic.Pointer[corev1.RefreshConnectionRequest]
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
	if m.listCharResp != nil || m.listCharErr != nil {
		return m.listCharResp, m.listCharErr
	}
	return &corev1.ListCharactersResponse{}, nil
}

func (m *mockCoreClient) RequestPasswordReset(_ context.Context, _ *corev1.RequestPasswordResetRequest) (*corev1.RequestPasswordResetResponse, error) {
	return &corev1.RequestPasswordResetResponse{}, nil
}

func (m *mockCoreClient) ConfirmPasswordReset(_ context.Context, _ *corev1.ConfirmPasswordResetRequest) (*corev1.ConfirmPasswordResetResponse, error) {
	return &corev1.ConfirmPasswordResetResponse{}, nil
}

func (m *mockCoreClient) Logout(_ context.Context, req *corev1.LogoutRequest) (*corev1.LogoutResponse, error) {
	m.logoutCalled = true
	m.lastLogoutReq = req
	if m.logoutResp != nil || m.logoutErr != nil {
		return m.logoutResp, m.logoutErr
	}
	return &corev1.LogoutResponse{}, nil
}

func (m *mockCoreClient) CreateGuest(_ context.Context, _ *corev1.CreateGuestRequest) (*corev1.CreateGuestResponse, error) {
	return m.createGuestResp, m.createGuestErr
}

func (m *mockCoreClient) HandleCommand(ctx context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
	m.lastHandleCommandReq = req
	if m.cmdFn != nil {
		return m.cmdFn(ctx, req)
	}
	return m.cmdResp, m.cmdErr
}

func (m *mockCoreClient) Subscribe(ctx context.Context, req *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
	m.lastSubscribeReq = req
	if m.subscribeFn != nil {
		return m.subscribeFn(ctx, req)
	}
	return m.subStream, m.subErr
}

func (m *mockCoreClient) Disconnect(_ context.Context, req *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	m.lastDisconnectReq = req
	return m.discResp, m.discErr
}

func (m *mockCoreClient) GetCommandHistory(_ context.Context, _ *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	return &corev1.GetCommandHistoryResponse{Meta: &corev1.ResponseMeta{}, Success: true}, nil
}

func (m *mockCoreClient) RefreshConnection(_ context.Context, req *corev1.RefreshConnectionRequest) (*corev1.RefreshConnectionResponse, error) {
	m.refreshCalls.Add(1)
	m.lastRefreshReq.Store(req)
	if m.refreshResp == nil {
		return &corev1.RefreshConnectionResponse{}, m.refreshErr
	}
	return m.refreshResp, m.refreshErr
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
		createGuestResp: &corev1.CreateGuestResponse{
			Success:            true,
			PlayerSessionToken: "tok-guest-1",
			Characters:         []*corev1.CharacterSummary{{CharacterId: "char-1", CharacterName: "Guest-7"}},
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-1",
			CharacterName: "Guest-7",
		},
		// Prevent Subscribe goroutine from launching.
		subErr:   errors.New("no subscribe in this test"),
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
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

	// Disconnect cleanly.
	cancel()
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
		createGuestResp: &corev1.CreateGuestResponse{
			Success:            true,
			PlayerSessionToken: "tok-guest-2",
			Characters:         []*corev1.CharacterSummary{{CharacterId: "char-2", CharacterName: "Tester"}},
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-2",
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

	handler := newTestHandler(serverConn, client)
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
			{Frame: &corev1.SubscribeResponse_Event{Event: withRendering(&corev1.EventFrame{Type: string(core.EventTypeCommandResponse), Payload: payload})}},
		},
	}

	client := &mockCoreClient{
		createGuestResp: &corev1.CreateGuestResponse{
			Success:            true,
			PlayerSessionToken: "tok-guest-cr",
			Characters:         []*corev1.CharacterSummary{{CharacterId: "char-cr", CharacterName: "CRUser"}},
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-cr",
			CharacterName: "CRUser",
		},
		subStream: eventStream,
		discResp:  &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
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
			{Frame: &corev1.SubscribeResponse_Event{Event: withRendering(&corev1.EventFrame{Type: string(core.EventTypeCommandResponse), Payload: []byte("not-valid-json")})}},
		},
	}

	client := &mockCoreClient{
		createGuestResp: &corev1.CreateGuestResponse{
			Success:            true,
			PlayerSessionToken: "tok-guest-corrupt",
			Characters:         []*corev1.CharacterSummary{{CharacterId: "char-corrupt", CharacterName: "CorruptUser"}},
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-corrupt",
			CharacterName: "CorruptUser",
		},
		subStream: eventStream,
		discResp:  &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
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
		createGuestResp: &corev1.CreateGuestResponse{
			Success:            true,
			PlayerSessionToken: "tok-guest-sc",
			Characters:         []*corev1.CharacterSummary{{CharacterId: "char-sc", CharacterName: "SCUser"}},
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-sc",
			CharacterName: "SCUser",
		},
		subStream: eventStream,
		discResp:  &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
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

	// After STREAM_CLOSED the handler returns to character picker and may
	// write more output (character list). Drain the pipe so writes don't block.
	go func() { _, _ = io.Copy(io.Discard, clientConn) }()

	// Cancel context to let the handler exit.
	cancel()
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
		createGuestResp: &corev1.CreateGuestResponse{
			Success:            true,
			PlayerSessionToken: "tok-guest-rpc-err",
			Characters:         []*corev1.CharacterSummary{{CharacterId: "char-rpc-err", CharacterName: "RPCErrUser"}},
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-rpc-err",
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

	handler := newTestHandler(serverConn, client)
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

	handler := newTestHandler(serverConn, client)
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
			PlayerSessionToken: "tok-single",
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

	handler := newTestHandler(serverConn, client)
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
			Success:            true,
			PlayerSessionToken: "tok-multi",
			Characters:         twoCharList(),
		},
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
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
			Success:            true,
			PlayerSessionToken: "tok-idx",
			Characters:         twoCharList(),
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

	handler := newTestHandler(serverConn, client)
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

	// Verify the correct playerSessionToken and characterId were sent to the server.
	require.NotNil(t, client.lastSelectCharReq)
	assert.Equal(t, "tok-idx", client.lastSelectCharReq.GetPlayerSessionToken())
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
			Success:            true,
			PlayerSessionToken: "tok-name",
			Characters:         twoCharList(),
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

	handler := newTestHandler(serverConn, client)
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
			Success:            true,
			PlayerSessionToken: "tok-reattach",
			Characters:         twoCharList(),
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

	handler := newTestHandler(serverConn, client)
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
			Success:            true,
			PlayerSessionToken: "tok-create",
			Characters:         twoCharList(),
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

	handler := newTestHandler(serverConn, client)
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
			Success:            true,
			PlayerSessionToken: "tok-inv",
			Characters:         twoCharList(),
		},
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
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

	handler := newTestHandler(serverConn, client)
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

func TestFormatEvent_Communication_Speech(t *testing.T) {
	h := &GatewayHandler{}

	tests := []struct {
		name     string
		evType   string
		payload  string
		expected string
	}{
		{
			"core-communication:say",
			"core-communication:say",
			`{"character_name":"Alice","message":"Hello"}`,
			`Alice says, "Hello"`,
		},
		{
			"core-communication:page",
			"core-communication:page",
			`{"sender_name":"Bob","message":"Hey there"}`,
			`Bob pages, "Hey there"`,
		},
		{
			"core-communication:whisper",
			"core-communication:whisper",
			`{"sender_name":"Carol","message":"psst"}`,
			`Carol whispers, "psst"`,
		},
		{
			"CommunicationContent shape (actor_display_name, text)",
			"core-communication:say",
			`{"actor_id":"01H","actor_display_name":"Alaric","text":"hi"}`,
			`Alaric says, "hi"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := &corev1.EventFrame{
				Type:    tt.evType,
				Payload: []byte(tt.payload),
			}
			got := h.formatEvent(withRendering(ev))
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestFormatEvent_Communication_Action(t *testing.T) {
	h := &GatewayHandler{}

	tests := []struct {
		name     string
		evType   string
		payload  string
		expected string
	}{
		{
			"pose with space",
			"core-communication:pose",
			`{"character_name":"Alice","action":"waves happily."}`,
			"Alice waves happily.",
		},
		{
			"pose no_space",
			"core-communication:pose",
			`{"character_name":"Alice","action":"'s eyes widen.","no_space":true}`,
			"Alice's eyes widen.",
		},
		{
			"core-communication:whisper_notice",
			"core-communication:whisper_notice",
			`{"sender_name":"Bob","target_name":"Carol","notice":"whispers something to Carol."}`,
			"Bob whispers something to Carol.",
		},
		{
			"CommunicationContent shape (actor_display_name, text, no_space)",
			"core-communication:pose",
			`{"actor_id":"01H","actor_display_name":"Alaric","text":"waves","no_space":true}`,
			"Alaricwaves",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := &corev1.EventFrame{
				Type:    tt.evType,
				Payload: []byte(tt.payload),
			}
			got := h.formatEvent(withRendering(ev))
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestFormatEvent_Movement(t *testing.T) {
	h := &GatewayHandler{}

	tests := []struct {
		name     string
		evType   string
		payload  string
		expected string
	}{
		{
			"arrive",
			"arrive",
			`{"character_name":"Alice"}`,
			"Alice has arrived.",
		},
		{
			"leave with reason",
			"leave",
			`{"character_name":"Bob","reason":"north"}`,
			"Bob has left (north).",
		},
		{
			"leave without reason",
			"leave",
			`{"character_name":"Bob"}`,
			"Bob has left.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := &corev1.EventFrame{
				Type:    tt.evType,
				Payload: []byte(tt.payload),
			}
			got := h.formatEvent(withRendering(ev))
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestFormatEvent_Command(t *testing.T) {
	h := &GatewayHandler{}

	tests := []struct {
		name     string
		evType   string
		payload  string
		expected string
	}{
		{
			"command_response narrative",
			"command_response",
			`{"text":"You see a large room."}`,
			"You see a large room.",
		},
		{
			"command_error",
			"command_error",
			`{"text":"Permission denied."}`,
			"[ERROR] Permission denied.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := &corev1.EventFrame{
				Type:    tt.evType,
				Payload: []byte(tt.payload),
			}
			got := h.formatEvent(withRendering(ev))
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestFormatEvent_State_Suppressed(t *testing.T) {
	h := &GatewayHandler{}

	ev := &corev1.EventFrame{
		Type:    "location_state",
		Payload: []byte(`{"location":{"id":"loc-1","name":"Town Square"}}`),
	}
	got := h.formatEvent(withRendering(ev))
	assert.Equal(t, "", got, "state events should produce empty string for telnet")
}

func TestFormatEvent_System(t *testing.T) {
	h := &GatewayHandler{}

	ev := &corev1.EventFrame{
		Type:    "system",
		Payload: []byte(`{"message":"Server restarting in 5 minutes."}`),
	}
	got := h.formatEvent(withRendering(ev))
	assert.Equal(t, "Server restarting in 5 minutes.", got)
}

func TestFormatEventDropsEventWithNilRenderingAndIncrementsMetric(t *testing.T) {
	// INV-EVENTBUS-6: events arriving without RenderingMetadata are dropped at
	// the gateway and counted via gatewaymetrics.DroppedNilRenderingTotal.
	// A non-zero counter indicates an upstream invariant violation in the
	// core process's RenderingPublisher.
	h := &GatewayHandler{}

	before := testutil.ToFloat64(gatewaymetrics.DroppedNilRenderingTotal.WithLabelValues(gatewaymetrics.SurfaceTelnet, "custom_plugin_event"))

	ev := &corev1.EventFrame{
		Type:    "custom_plugin_event",
		Payload: []byte(`{"text":"Something happened."}`),
		// Rendering deliberately omitted.
	}
	got := h.formatEvent(ev)
	assert.Empty(t, got, "events without rendering must be dropped (return empty string)")

	after := testutil.ToFloat64(gatewaymetrics.DroppedNilRenderingTotal.WithLabelValues(gatewaymetrics.SurfaceTelnet, "custom_plugin_event"))
	assert.Equal(t, before+1, after, "drop counter must increment exactly once")
}

func TestFormatEventFallsBackForUnknownCategoryWithRendering(t *testing.T) {
	// When rendering IS present but the category is unrecognized (e.g., a
	// future plugin defining a new category), formatEvent invokes
	// formatFallback rather than dropping. The fallback extracts text from
	// the payload's text/message fields.
	h := &GatewayHandler{}

	ev := &corev1.EventFrame{
		Type:    "custom_plugin_event",
		Payload: []byte(`{"text":"Something happened."}`),
		Rendering: &corev1.RenderingMetadata{
			Category:      "future_category",
			Format:        "narrative",
			DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
			SourcePlugin:  "future-plugin",
		},
	}
	got := h.formatEvent(ev)
	assert.Equal(t, "Something happened.", got)
}

// --- QUIT / LOGOUT behaviour tests ---

// chanSubscribeStream is a channel-backed subscribe stream for tests that need
// to control event delivery timing (e.g., sending STREAM_CLOSED after quit).
type chanSubscribeStream struct {
	ch  chan *corev1.SubscribeResponse
	ctx context.Context
}

func newChanSubscribeStream(ctx context.Context) *chanSubscribeStream {
	return &chanSubscribeStream{
		ch:  make(chan *corev1.SubscribeResponse, 8),
		ctx: ctx,
	}
}

func (s *chanSubscribeStream) Recv() (*corev1.SubscribeResponse, error) {
	select {
	case resp, ok := <-s.ch:
		if !ok {
			return nil, io.EOF
		}
		return resp, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *chanSubscribeStream) Header() (metadata.MD, error) { return nil, nil }
func (s *chanSubscribeStream) Trailer() metadata.MD         { return nil }
func (s *chanSubscribeStream) CloseSend() error             { return nil }
func (s *chanSubscribeStream) Context() context.Context     { return s.ctx }
func (s *chanSubscribeStream) SendMsg(any) error            { return nil }
func (s *chanSubscribeStream) RecvMsg(any) error            { return nil }

// streamClosedFrame returns a STREAM_CLOSED control frame with the given message.
func streamClosedFrame(msg string) *corev1.SubscribeResponse {
	return &corev1.SubscribeResponse{
		Frame: &corev1.SubscribeResponse_Control{Control: &corev1.ControlFrame{
			Signal:  corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED,
			Message: msg,
		}},
	}
}

// TestQuitWhilePlaying_ReturnsToSelectMode verifies that QUIT while playing a
// character drains the event stream, then returns to the character picker
// instead of closing the connection.
func TestQuitWhilePlaying_ReturnsToSelectMode(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := newChanSubscribeStream(ctx)

	var quitSent bool
	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-quit",
			Characters:         twoCharList(),
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-quit",
			CharacterName: "Alaric",
		},
		subscribeFn: func(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
			return stream, nil
		},
		cmdFn: func(_ context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
			if req.GetCommand() == "quit" {
				quitSent = true
				// Simulate server sending STREAM_CLOSED after processing quit.
				go func() {
					stream.ch <- streamClosedFrame("Goodbye!")
				}()
			}
			return &corev1.HandleCommandResponse{Success: true}, nil
		},
		listCharResp: &corev1.ListCharactersResponse{Characters: twoCharList()},
		discResp:     &corev1.DisconnectResponse{Success: true},
	}

	handler := newTestHandler(serverConn, client)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.Handle(ctx)
	}()

	reset := withDeadline(t, clientConn)
	defer reset()

	r := bufio.NewReader(clientConn)
	readLines(t, r, 2) // banner

	// Login → selectMode
	_, err := clientConn.Write([]byte("connect alice secret\n"))
	require.NoError(t, err)
	readLinesUntil(t, r, "play")

	// Select character
	_, err = clientConn.Write([]byte("play 1\n"))
	require.NoError(t, err)
	readLinesUntil(t, r, "Alaric")

	// Quit while playing — should return to selectMode
	_, err = clientConn.Write([]byte("quit\n"))
	require.NoError(t, err)

	// Should see "Goodbye!" from drain, then character list again.
	lines := readLinesUntil(t, r, "play")
	combined := strings.Join(lines, "\n")
	assert.Contains(t, combined, "Goodbye!")
	assert.Contains(t, combined, "Alaric")
	assert.Contains(t, combined, "Beatrix")
	assert.True(t, quitSent, "quit command should have been sent to server")

	cancel()
	<-done
}

// TestQuitInSelectMode_LogsOut verifies that QUIT in selectMode calls
// the Logout RPC and closes the connection.
func TestQuitInSelectMode_LogsOut(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-sel-quit",
			Characters:         twoCharList(),
		},
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
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
	readLinesUntil(t, r, "play")

	// QUIT in selectMode → should logout and close
	_, err = clientConn.Write([]byte("quit\n"))
	require.NoError(t, err)

	// Should see "Goodbye!" and then the handler should exit.
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, strings.TrimRight(line, "\r\n"), "Goodbye!")

	assert.True(t, client.logoutCalled, "Logout RPC should be called")
	require.NotNil(t, client.lastLogoutReq)
	assert.Equal(t, "tok-sel-quit", client.lastLogoutReq.GetPlayerSessionToken())

	select {
	case <-done:
		// expected — handler exited
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after QUIT in selectMode")
	}
}

// TestLogoutWhilePlaying verifies that LOGOUT while playing sends quit to the
// server and calls the Logout RPC, then closes the connection.
func TestLogoutWhilePlaying(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := newChanSubscribeStream(ctx)

	var quitSent bool
	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-logout",
			Characters:         twoCharList(),
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-logout",
			CharacterName: "Alaric",
		},
		subscribeFn: func(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
			return stream, nil
		},
		cmdFn: func(_ context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
			if req.GetCommand() == "quit" {
				quitSent = true
				go func() {
					stream.ch <- streamClosedFrame("Goodbye!")
				}()
			}
			return &corev1.HandleCommandResponse{Success: true}, nil
		},
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	handler := newTestHandler(serverConn, client)
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
	readLinesUntil(t, r, "play")

	_, err = clientConn.Write([]byte("play 1\n"))
	require.NoError(t, err)
	readLinesUntil(t, r, "Alaric")

	// LOGOUT while playing — should quit + logout + close
	_, err = clientConn.Write([]byte("logout\n"))
	require.NoError(t, err)

	// Drain remaining output.
	go func() {
		for {
			_, readErr := r.ReadString('\n')
			if readErr != nil {
				return
			}
		}
	}()

	select {
	case <-done:
		// expected — handler exited
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after LOGOUT while playing")
	}

	assert.True(t, quitSent, "quit command should have been forwarded")
	assert.True(t, client.logoutCalled, "Logout RPC should be called")
	require.NotNil(t, client.lastLogoutReq)
	assert.Equal(t, "tok-logout", client.lastLogoutReq.GetPlayerSessionToken())
}

// TestHandleLogout_WhenNotAuthed_GuestPath verifies that LOGOUT when not
// authenticated (no playerSessionToken) sends "Goodbye!" and closes.
func TestHandleLogout_WhenNotAuthed_GuestPath(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.Handle(ctx)
	}()

	reset := withDeadline(t, clientConn)
	defer reset()

	r := bufio.NewReader(clientConn)
	readLines(t, r, 2) // banner

	// Guest not connected yet — send logout. Since there's no playerSessionToken
	// and not authed, handleLogout sends "Goodbye!" and sets quitting = true.
	_, err := clientConn.Write([]byte("logout\n"))
	require.NoError(t, err)

	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, strings.TrimRight(line, "\r\n"), "Goodbye!")

	// Handler should exit since loggingOut && no playerSessionToken.
	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after LOGOUT when not authed")
	}

	// Logout RPC should NOT be called since there's no playerSessionToken.
	assert.False(t, client.logoutCalled, "Logout RPC should not be called without session token")
}

// TestHandleLogout_LogoutRPCError verifies that a Logout RPC error does not
// prevent the handler from sending "Goodbye!" and exiting.
func TestHandleLogout_LogoutRPCError(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-logout-err",
			Characters:         twoCharList(),
		},
		logoutResp: nil,
		logoutErr:  errors.New("timeout"),
		discResp:   &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
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
	readLinesUntil(t, r, "play")

	// LOGOUT in selectMode — Logout RPC will fail
	_, err = clientConn.Write([]byte("logout\n"))
	require.NoError(t, err)

	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, strings.TrimRight(line, "\r\n"), "Goodbye!")

	// Despite the error, logout was attempted.
	assert.True(t, client.logoutCalled, "Logout RPC should have been called even though it failed")

	select {
	case <-done:
		// expected — handler exited
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after LOGOUT with RPC error")
	}
}

// TestRefreshCharacterList_Success verifies that after QUIT in playing mode,
// refreshCharacterList is called and the new character list is displayed.
func TestRefreshCharacterList_Success(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := newChanSubscribeStream(ctx)

	updatedChars := []*corev1.CharacterSummary{
		{CharacterId: "char-alaric", CharacterName: "Alaric", HasActiveSession: false},
		{CharacterId: "char-beatrix", CharacterName: "Beatrix", HasActiveSession: false},
		{CharacterId: "char-new", CharacterName: "Celeste", HasActiveSession: false},
	}

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-refresh",
			Characters:         twoCharList(),
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-refresh",
			CharacterName: "Alaric",
		},
		subscribeFn: func(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
			return stream, nil
		},
		cmdFn: func(_ context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
			if req.GetCommand() == "quit" {
				go func() {
					stream.ch <- streamClosedFrame("Goodbye!")
				}()
			}
			return &corev1.HandleCommandResponse{Success: true}, nil
		},
		// After refresh, return an updated list (includes Celeste).
		listCharResp: &corev1.ListCharactersResponse{Characters: updatedChars},
		discResp:     &corev1.DisconnectResponse{Success: true},
	}

	handler := newTestHandler(serverConn, client)
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
	readLinesUntil(t, r, "play")

	_, err = clientConn.Write([]byte("play 1\n"))
	require.NoError(t, err)
	readLinesUntil(t, r, "Alaric")

	_, err = clientConn.Write([]byte("quit\n"))
	require.NoError(t, err)

	// Should see the refreshed character list including Celeste.
	lines := readLinesUntil(t, r, "play")
	combined := strings.Join(lines, "\n")
	assert.Contains(t, combined, "Celeste", "refreshed character list should include new character")

	cancel()
	<-done
}

// TestRefreshCharacterList_Error verifies that when ListCharacters fails,
// an error is shown and selectMode still activates with an empty list.
func TestRefreshCharacterList_Error(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := newChanSubscribeStream(ctx)

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-refresh-err",
			Characters:         twoCharList(),
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-refresh-err",
			CharacterName: "Alaric",
		},
		subscribeFn: func(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
			return stream, nil
		},
		cmdFn: func(_ context.Context, req *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
			if req.GetCommand() == "quit" {
				go func() {
					stream.ch <- streamClosedFrame("Goodbye!")
				}()
			}
			return &corev1.HandleCommandResponse{Success: true}, nil
		},
		listCharErr: errors.New("database connection lost"),
		discResp:    &corev1.DisconnectResponse{Success: true},
	}

	handler := newTestHandler(serverConn, client)
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
	readLinesUntil(t, r, "play")

	_, err = clientConn.Write([]byte("play 1\n"))
	require.NoError(t, err)
	readLinesUntil(t, r, "Alaric")

	_, err = clientConn.Write([]byte("quit\n"))
	require.NoError(t, err)

	// Should see error about failing to refresh, then an empty character list.
	lines := readLinesUntil(t, r, "play")
	combined := strings.Join(lines, "\n")
	assert.Contains(t, strings.ToLower(combined), "failed to refresh",
		"should indicate refresh failure")

	cancel()
	<-done
}

// TestLogoutCommand_InSelectMode verifies that "logout" in selectMode dispatches
// to handleLogout (same as "quit" in selectMode).
func TestLogoutCommand_InSelectMode(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-sel-logout",
			Characters:         twoCharList(),
		},
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
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
	readLinesUntil(t, r, "play")

	// Send "logout" (not "quit") while in selectMode
	_, err = clientConn.Write([]byte("logout\n"))
	require.NoError(t, err)

	line, err := r.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, strings.TrimRight(line, "\r\n"), "Goodbye!")

	assert.True(t, client.logoutCalled, "Logout RPC should be called for 'logout' command")
	require.NotNil(t, client.lastLogoutReq)
	assert.Equal(t, "tok-sel-logout", client.lastLogoutReq.GetPlayerSessionToken())

	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after LOGOUT in selectMode")
	}
}

// TestServerInitiated_StreamClosed_ReturnsToSelectMode verifies that a
// server-initiated STREAM_CLOSED while playing returns to the character picker
// (when the player has a session token).
func TestServerInitiated_StreamClosed_ReturnsToSelectMode(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := newChanSubscribeStream(ctx)

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-server-close",
			Characters:         twoCharList(),
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-server-close",
			CharacterName: "Alaric",
		},
		subscribeFn: func(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
			return stream, nil
		},
		listCharResp: &corev1.ListCharactersResponse{Characters: twoCharList()},
		discResp:     &corev1.DisconnectResponse{Success: true},
	}

	handler := newTestHandler(serverConn, client)
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
	readLinesUntil(t, r, "play")

	_, err = clientConn.Write([]byte("play 1\n"))
	require.NoError(t, err)
	readLinesUntil(t, r, "Alaric")

	// Server initiates STREAM_CLOSED (not from quit).
	stream.ch <- streamClosedFrame("Server restarting.")

	// Should return to character picker.
	lines := readLinesUntil(t, r, "play")
	combined := strings.Join(lines, "\n")
	assert.Contains(t, combined, "Server restarting.")
	assert.Contains(t, combined, "Alaric", "character list should be shown after server-initiated disconnect")

	cancel()
	<-done
}

// TestPlayerSessionTokenSurvivesPlay verifies that playerSessionToken is NOT
// cleared after selecting a character, allowing return to selectMode.
func TestPlayerSessionTokenSurvivesPlay(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-persist",
			Characters:         twoCharList(),
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-persist",
			CharacterName: "Alaric",
		},
		subErr:   errors.New("no subscribe"),
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
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
	readLinesUntil(t, r, "play")

	_, err = clientConn.Write([]byte("play 1\n"))
	require.NoError(t, err)
	readLinesUntil(t, r, "Alaric")

	// After PLAY, the token must still be set.
	assert.Equal(t, "tok-persist", handler.playerSessionToken,
		"playerSessionToken must persist after character selection")

	cancel()
	<-done
}

// Post-auth RPC token forwarding (bd-jv7z, Task 8).
//
// The telnet gateway MUST forward the player_session_token (captured at
// AuthenticatePlayer / CreateGuest and preserved through SelectCharacter)
// on every post-auth RPC call. Without this, server-side
// ValidateSessionOwnership cannot enforce that the caller owns session_id.

// newForwardingTestHandler builds a minimal GatewayHandler suitable for
// directly invoking post-auth methods. The connection pair isn't driven
// via telnet input; the test exercises the RPC call path only.
func newForwardingTestHandler(t *testing.T, client CoreClient, token, sessionID string) (*GatewayHandler, func()) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	h := newTestHandler(serverConn, client)
	h.playerSessionToken = token
	h.sessionID = sessionID
	h.connectionID = "conn-forward-1"
	h.authed = true
	cleanup := func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	}
	// Drain anything the handler writes so send() doesn't block.
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				return
			}
		}
	}()
	return h, cleanup
}

func TestGatewayHandlerForwardsPlayerSessionTokenOnHandleCommandSay(t *testing.T) {
	const token = "tok-telnet-say"
	client := &mockCoreClient{
		cmdResp: &corev1.HandleCommandResponse{Success: true},
	}
	h, cleanup := newForwardingTestHandler(t, client, token, "sess-say")
	defer cleanup()

	h.handleSay(context.Background(), "hello")

	require.NotNil(t, client.lastHandleCommandReq)
	assert.Equal(t, token, client.lastHandleCommandReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-say", client.lastHandleCommandReq.GetSessionId())
}

func TestGatewayHandlerForwardsPlayerSessionTokenOnHandleCommandPose(t *testing.T) {
	const token = "tok-telnet-pose"
	client := &mockCoreClient{
		cmdResp: &corev1.HandleCommandResponse{Success: true},
	}
	h, cleanup := newForwardingTestHandler(t, client, token, "sess-pose")
	defer cleanup()

	h.handlePose(context.Background(), "waves")

	require.NotNil(t, client.lastHandleCommandReq)
	assert.Equal(t, token, client.lastHandleCommandReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-pose", client.lastHandleCommandReq.GetSessionId())
}

func TestGatewayHandlerForwardsPlayerSessionTokenOnHandleCommandGeneric(t *testing.T) {
	const token = "tok-telnet-generic"
	client := &mockCoreClient{
		cmdResp: &corev1.HandleCommandResponse{Success: true},
	}
	h, cleanup := newForwardingTestHandler(t, client, token, "sess-generic")
	defer cleanup()

	h.handleGenericCommand(context.Background(), "look", "")

	require.NotNil(t, client.lastHandleCommandReq)
	assert.Equal(t, token, client.lastHandleCommandReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-generic", client.lastHandleCommandReq.GetSessionId())
}

func TestGatewayHandlerForwardsPlayerSessionTokenOnHandleCommandQuit(t *testing.T) {
	const token = "tok-telnet-quit"
	client := &mockCoreClient{
		cmdResp: &corev1.HandleCommandResponse{Success: true},
	}
	h, cleanup := newForwardingTestHandler(t, client, token, "sess-quit")
	defer cleanup()

	h.handleQuit(context.Background())

	require.NotNil(t, client.lastHandleCommandReq)
	assert.Equal(t, token, client.lastHandleCommandReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-quit", client.lastHandleCommandReq.GetSessionId())
}

func TestGatewayHandlerForwardsPlayerSessionTokenOnSubscribe(t *testing.T) {
	const token = "tok-telnet-subscribe"
	// subscribeFn returns an error so the handler doesn't start the
	// reader goroutine — we only care that the request was captured.
	client := &mockCoreClient{
		subscribeFn: func(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
			return nil, errors.New("no stream in this test")
		},
	}
	h, cleanup := newForwardingTestHandler(t, client, token, "sess-sub")
	defer cleanup()

	_ = h.subscribeAndEnter(context.Background())

	require.NotNil(t, client.lastSubscribeReq)
	assert.Equal(t, token, client.lastSubscribeReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-sub", client.lastSubscribeReq.GetSessionId())
}

// TestGatewayHandlerPassesConnectionIDAndClientTypeOnSubscribe asserts that
// the telnet gateway generates a connection_id and passes it (plus
// client_type "telnet") on its Subscribe call so core can register the
// connection in the session store (bd-j2xj). The same connection_id is
// reused by the deferred Disconnect in Handle so the terminate path
// removes the correct per-connection entry.
func TestGatewayHandlerPassesConnectionIDAndClientTypeOnSubscribe(t *testing.T) {
	const token = "tok-telnet-connid"
	client := &mockCoreClient{
		subscribeFn: func(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
			return nil, errors.New("no stream in this test")
		},
	}
	h, cleanup := newForwardingTestHandler(t, client, token, "sess-connid")
	defer cleanup()

	_ = h.subscribeAndEnter(context.Background())

	require.NotNil(t, client.lastSubscribeReq)
	assert.NotEmpty(t, client.lastSubscribeReq.GetConnectionId(),
		"Subscribe must carry a connection_id so core can register the connection")
	assert.Equal(t, "telnet", client.lastSubscribeReq.GetClientType(),
		"telnet gateway must declare client_type = %q", "telnet")
	assert.Equal(t, client.lastSubscribeReq.GetConnectionId(), h.connectionID,
		"handler must persist the connection_id so the deferred Disconnect uses the same value")
}

func TestGatewayHandlerForwardsPlayerSessionTokenOnDisconnect(t *testing.T) {
	const token = "tok-telnet-disconnect"
	client := &mockCoreClient{
		createGuestResp: &corev1.CreateGuestResponse{
			Success:            true,
			PlayerSessionToken: token,
			Characters:         []*corev1.CharacterSummary{{CharacterId: "char-d", CharacterName: "Discon"}},
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-disc",
			CharacterName: "Discon",
		},
		subErr:   errors.New("no subscribe in this test"),
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := newTestHandler(serverConn, client)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.Handle(ctx)
	}()

	r := bufio.NewReader(clientConn)
	readLines(t, r, 2) // banner

	_, err := clientConn.Write([]byte("connect guest\n"))
	require.NoError(t, err)
	// Consume welcome line.
	_, err = r.ReadString('\n')
	require.NoError(t, err)

	// Disconnect — triggers the deferred Disconnect RPC.
	cancel()
	<-done

	require.NotNil(t, client.lastDisconnectReq)
	assert.Equal(t, token, client.lastDisconnectReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-disc", client.lastDisconnectReq.GetSessionId())
}

func TestReadDeadlineFiresOnIdleClient(t *testing.T) {
	before := testutil.ToFloat64(IdleTimeoutsTotal)

	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	client := &mockCoreClient{}

	handler := NewGatewayHandler(serverConn, client, Limits{
		IdleReadTimeout: 100 * time.Millisecond,
		WriteTimeout:    DefaultLimits.WriteTimeout,
		PreAuthTimeout:  DefaultLimits.PreAuthTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	// Drain BOTH welcome banner lines so the handler can proceed to the
	// scanner goroutine (net.Pipe is unbuffered — an undrained send blocks
	// the handler indefinitely).
	br := bufio.NewReader(clientConn)
	_, _ = br.ReadString('\n')
	_, _ = br.ReadString('\n')

	// Send NO bytes. Wait for handler to exit via idle timeout.
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not exit on idle deadline")
	}

	assert.Equal(t, before+1, testutil.ToFloat64(IdleTimeoutsTotal),
		"idle timeout must increment the counter")
}

func TestReadDeadlineResetsOnByte(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	client := &mockCoreClient{}

	handler := NewGatewayHandler(serverConn, client, Limits{
		IdleReadTimeout: 150 * time.Millisecond,
		WriteTimeout:    DefaultLimits.WriteTimeout,
		PreAuthTimeout:  DefaultLimits.PreAuthTimeout,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	// Drain BOTH welcome banner lines so the handler can proceed to the
	// scanner goroutine (net.Pipe is unbuffered).
	br := bufio.NewReader(clientConn)
	_, _ = br.ReadString('\n')
	_, _ = br.ReadString('\n')

	// Send a byte every 50 ms for 400 ms — total > 2 × IdleReadTimeout.
	// If the deadline resets on each read, the handler stays alive.
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	stop := time.After(400 * time.Millisecond)
Keepalive:
	for {
		select {
		case <-ticker.C:
			_, werr := clientConn.Write([]byte("a"))
			if werr != nil {
				break Keepalive
			}
		case <-stop:
			break Keepalive
		}
	}

	select {
	case <-done:
		t.Fatal("handler exited during keep-alive — deadline did not reset per read")
	default:
		// still running, as expected
	}
}

// mockDeadlineTrackingConn records SetWriteDeadline calls without a real socket.
type mockDeadlineTrackingConn struct {
	net.Conn
	writeDeadlines []time.Time
	writeBuf       []byte
}

func (m *mockDeadlineTrackingConn) SetWriteDeadline(t time.Time) error {
	m.writeDeadlines = append(m.writeDeadlines, t)
	return nil
}

func (m *mockDeadlineTrackingConn) Write(p []byte) (int, error) {
	m.writeBuf = append(m.writeBuf, p...)
	return len(p), nil
}

func (m *mockDeadlineTrackingConn) SetReadDeadline(_ time.Time) error { return nil }
func (m *mockDeadlineTrackingConn) Close() error                      { return nil }
func (m *mockDeadlineTrackingConn) RemoteAddr() net.Addr              { return &net.TCPAddr{} }

// newEOFStream returns a SubscribeClient whose first Recv returns io.EOF.
// Used by tests that care only about auth completing and the handler
// entering the event-loop idle state.
func newEOFStream() corev1.CoreService_SubscribeClient {
	return &eofSubStream{}
}

type eofSubStream struct{}

func (s *eofSubStream) Recv() (*corev1.SubscribeResponse, error) { return nil, io.EOF }
func (s *eofSubStream) Context() context.Context                 { return context.Background() }
func (s *eofSubStream) Header() (metadata.MD, error)             { return nil, nil }
func (s *eofSubStream) Trailer() metadata.MD                     { return nil }
func (s *eofSubStream) CloseSend() error                         { return nil }
func (s *eofSubStream) SendMsg(any) error                        { return nil }
func (s *eofSubStream) RecvMsg(any) error                        { return nil }

func TestPreAuthTimerFiresForUnauthedClient(t *testing.T) {
	before := testutil.ToFloat64(PreAuthTimeoutsTotal)

	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	client := &mockCoreClient{}
	handler := NewGatewayHandler(serverConn, client, Limits{
		IdleReadTimeout: DefaultLimits.IdleReadTimeout,
		WriteTimeout:    DefaultLimits.WriteTimeout,
		PreAuthTimeout:  100 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	// Drain welcome banner + any output (including "Authentication timeout.")
	// until EOF or deadline.
	scanner := bufio.NewScanner(clientConn)
	var sawTimeoutLine bool
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "Authentication timeout") {
				sawTimeoutLine = true
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not exit on pre-auth timeout")
	}
	// Wait briefly for the reader goroutine to drain any buffered lines.
	select {
	case <-readDone:
	case <-time.After(200 * time.Millisecond):
	}

	assert.True(t, sawTimeoutLine, "client must receive 'Authentication timeout.'")
	assert.Equal(t, before+1, testutil.ToFloat64(PreAuthTimeoutsTotal),
		"preauth counter must increment")
}

func TestPreAuthTimerCancelledAfterGuestConnect(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	// CreateGuest returns one character and a session token so the auto-
	// select path marks the handler authed.
	client := &mockCoreClient{
		createGuestResp: &corev1.CreateGuestResponse{
			Success:            true,
			PlayerSessionToken: "guest-token",
			Characters: []*corev1.CharacterSummary{
				{CharacterId: "char-guest", CharacterName: "Guest-1"},
			},
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "session-1",
			CharacterName: "Guest-1",
		},
	}
	client.subStream = newEOFStream()

	handler := NewGatewayHandler(serverConn, client, Limits{
		IdleReadTimeout: DefaultLimits.IdleReadTimeout,
		WriteTimeout:    DefaultLimits.WriteTimeout,
		PreAuthTimeout:  200 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	// Drain server output in a goroutine (net.Pipe is unbuffered; the
	// handler will send Welcome / Use: connect guest / "Welcome, Guest-1!" / ...
	go func() {
		scanner := bufio.NewScanner(clientConn)
		for scanner.Scan() {
		}
	}()

	// Give the handler time to print banner and reach its main loop
	// before we issue the command.
	time.Sleep(50 * time.Millisecond)

	// Issue connect guest.
	_, err := clientConn.Write([]byte("connect guest\n"))
	require.NoError(t, err)

	// Wait past pre-auth timeout. Handler must still be alive.
	time.Sleep(400 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("handler exited — pre-auth timer fired after successful auth")
	default:
	}

	cancel() // clean shutdown
	<-done
}

func TestPreAuthTimerCancelledAfterTwoPhaseSelect(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "player-token",
			Characters: []*corev1.CharacterSummary{
				{CharacterId: "char-alice", CharacterName: "Alice"},
			},
			DefaultCharacterId: "char-alice",
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "session-1",
			CharacterName: "Alice",
		},
	}
	client.subStream = newEOFStream()

	handler := NewGatewayHandler(serverConn, client, Limits{
		IdleReadTimeout: DefaultLimits.IdleReadTimeout,
		WriteTimeout:    DefaultLimits.WriteTimeout,
		PreAuthTimeout:  200 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		handler.Handle(ctx)
		close(done)
	}()

	go func() {
		scanner := bufio.NewScanner(clientConn)
		for scanner.Scan() {
		}
	}()

	time.Sleep(50 * time.Millisecond)

	_, err := clientConn.Write([]byte("connect alice password\n"))
	require.NoError(t, err)

	time.Sleep(400 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("handler exited — pre-auth timer fired after successful two-phase auth")
	default:
	}

	cancel()
	<-done
}

// TestGatewayHandlerDisconnect verifies that the "disconnect" command calls
// the Disconnect RPC with the correct session/connection/token fields, sends
// a confirmation to the wire, and exits the handler cleanly (quitting +
// loggingOut so the character picker is skipped).
func TestGatewayHandlerDisconnect(t *testing.T) {
	const (
		token  = "tok-telnet-disconnect-cmd"
		sessID = "sess-disconnect-cmd"
		connID = "conn-disconnect-cmd"
	)
	client := &mockCoreClient{
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	h := newTestHandler(serverConn, client)
	h.playerSessionToken = token
	h.sessionID = sessID
	h.connectionID = connID
	h.authed = true

	// Capture wire output so we can assert the confirmation message.
	wireOutput := make(chan []byte, 1)
	go func() {
		var accumulated []byte
		buf := make([]byte, 256)
		for {
			n, err := clientConn.Read(buf)
			if n > 0 {
				accumulated = append(accumulated, buf[:n]...)
			}
			if err != nil {
				wireOutput <- accumulated
				return
			}
		}
	}()

	h.handleDisconnect(context.Background())
	serverConn.Close() // signal EOF to the capture goroutine

	output := <-wireOutput

	require.NotNil(t, client.lastDisconnectReq, "Disconnect RPC must be called")
	assert.Equal(t, sessID, client.lastDisconnectReq.GetSessionId())
	assert.Equal(t, connID, client.lastDisconnectReq.GetConnectionId())
	assert.Equal(t, token, client.lastDisconnectReq.GetPlayerSessionToken())

	assert.Contains(t, string(output), "Disconnected", "confirmation message must be sent to the wire")

	assert.True(t, h.quitting, "quitting must be set so the handler exits")
	assert.True(t, h.loggingOut, "loggingOut must be set to skip the character picker")

	// Auth/session state must be cleared so the deferred Disconnect in Handle
	// does NOT re-fire Disconnect for the connection we just removed.
	assert.False(t, h.authed, "authed must be cleared after successful Disconnect")
	assert.Empty(t, h.sessionID, "sessionID must be cleared after successful Disconnect")
	assert.Empty(t, h.connectionID, "connectionID must be cleared after successful Disconnect")
}

// TestGatewayHandlerDisconnect_WhenNotAuthed verifies that "disconnect" before
// authentication sends an error message and does not call the Disconnect RPC.
func TestGatewayHandlerDisconnect_WhenNotAuthed(t *testing.T) {
	client := &mockCoreClient{}

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	h := newTestHandler(serverConn, client)
	// Not authed — leave all fields at zero values.

	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				return
			}
		}
	}()

	h.handleDisconnect(context.Background())

	assert.Nil(t, client.lastDisconnectReq, "Disconnect RPC must NOT be called when not authed")
	assert.False(t, h.quitting, "quitting must not be set when guard fires")
}

// TestDisconnectCommand_DispatchedFromProcessLine verifies that the string
// "disconnect" is recognized by processLine and calls handleDisconnect.
func TestDisconnectCommand_DispatchedFromProcessLine(t *testing.T) {
	const (
		token  = "tok-dispatch"
		sessID = "sess-dispatch"
		connID = "conn-dispatch"
	)
	client := &mockCoreClient{
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	h := newTestHandler(serverConn, client)
	h.playerSessionToken = token
	h.sessionID = sessID
	h.connectionID = connID
	h.authed = true

	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				return
			}
		}
	}()

	h.processLine(context.Background(), "disconnect")

	require.NotNil(t, client.lastDisconnectReq,
		"processLine('disconnect') must trigger the Disconnect RPC")
	assert.Equal(t, sessID, client.lastDisconnectReq.GetSessionId())
	assert.Equal(t, token, client.lastDisconnectReq.GetPlayerSessionToken())
}

// TestGatewayHandlerDisconnect_RPCError verifies that when the Disconnect RPC
// returns an error, the handler logs the error but still sends the confirmation
// message and marks quitting/loggingOut — the error is not fatal.
func TestGatewayHandlerDisconnect_RPCError(t *testing.T) {
	const (
		token  = "tok-disconnect-rpc-error"
		sessID = "sess-disconnect-rpc-error"
		connID = "conn-disconnect-rpc-error"
	)
	client := &mockCoreClient{
		discErr: errors.New("server unavailable"),
	}

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	h := newTestHandler(serverConn, client)
	h.playerSessionToken = token
	h.sessionID = sessID
	h.connectionID = connID
	h.authed = true

	wireOutput := make(chan []byte, 1)
	go func() {
		var accumulated []byte
		buf := make([]byte, 256)
		for {
			n, readErr := clientConn.Read(buf)
			if n > 0 {
				accumulated = append(accumulated, buf[:n]...)
			}
			if readErr != nil {
				wireOutput <- accumulated
				return
			}
		}
	}()

	h.handleDisconnect(context.Background())
	serverConn.Close()

	output := <-wireOutput

	// RPC was attempted.
	require.NotNil(t, client.lastDisconnectReq, "Disconnect RPC must be called even on error")

	// Confirmation message still sent (error is non-fatal).
	assert.Contains(t, string(output), "Disconnected",
		"confirmation message must be sent even when RPC fails")

	// State flags still set so the handler exits cleanly.
	assert.True(t, h.quitting, "quitting must be set even when RPC fails")
	assert.True(t, h.loggingOut, "loggingOut must be set even when RPC fails")

	// Auth/session state cleared even on RPC error so the deferred Disconnect
	// in Handle does NOT re-fire. The server may or may not have removed the
	// connection, but the client cannot know; clearing state avoids a
	// guaranteed-duplicate teardown attempt.
	assert.False(t, h.authed, "authed must be cleared even when RPC fails")
	assert.Empty(t, h.sessionID, "sessionID must be cleared even when RPC fails")
	assert.Empty(t, h.connectionID, "connectionID must be cleared even when RPC fails")
}

func TestSendSetsWriteDeadline(t *testing.T) {
	mc := &mockDeadlineTrackingConn{}
	h := &GatewayHandler{
		conn:   mc,
		limits: Limits{WriteTimeout: 30 * time.Second},
	}

	start := time.Now()
	h.send("hello world")

	require.Len(t, mc.writeDeadlines, 1, "send must set exactly one write deadline")
	delta := mc.writeDeadlines[0].Sub(start)
	assert.GreaterOrEqual(t, delta, 30*time.Second,
		"deadline must be at least WriteTimeout into the future")
	assert.Less(t, delta, 31*time.Second,
		"deadline must not be absurdly far in the future")
	assert.Contains(t, string(mc.writeBuf), "hello world",
		"send must actually write the message body")
}

// TestGatewayHandlerRefreshesLeaseWhileAuthed verifies that refreshOnce calls
// RefreshConnection with the current session/connection/token when authed.
// Verifies: I-LIVE-1
// Verifies: I-SURV-5
func TestGatewayHandlerRefreshesLeaseWhileAuthed(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	mc := &mockCoreClient{}
	h := newTestHandler(serverConn, mc)
	h.authed = true
	h.sessionID = "sess-1"
	h.connectionID = "conn-1"
	h.playerSessionToken = "tok-1"

	h.refreshOnce(context.Background())

	require.Equal(t, int32(1), mc.refreshCalls.Load())
	got := mc.lastRefreshReq.Load()
	require.NotNil(t, got)
	assert.Equal(t, "sess-1", got.GetSessionId())
	assert.Equal(t, "conn-1", got.GetConnectionId())
	assert.Equal(t, "tok-1", got.GetPlayerSessionToken())
}

// TestGatewayHandlerRefreshOnceNoopWhenNotAuthed verifies that refreshOnce
// is a no-op when the handler is not authenticated (no session in progress).
func TestGatewayHandlerRefreshOnceNoopWhenNotAuthed(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	mc := &mockCoreClient{}
	h := newTestHandler(serverConn, mc)
	h.authed = false

	h.refreshOnce(context.Background())

	assert.Equal(t, int32(0), mc.refreshCalls.Load())
}

// reconnectEventFrame builds a SubscribeResponse carrying a renderable speech
// event with the given id and message text, used to verify dedup of the single
// redelivery-overlap frame after a durable-resume reconnect.
func reconnectEventFrame(id, actor, message string) *corev1.SubscribeResponse {
	return &corev1.SubscribeResponse{
		Frame: &corev1.SubscribeResponse_Event{
			Event: &corev1.EventFrame{
				Id:   id,
				Type: "core-communication:say",
				Rendering: &corev1.RenderingMetadata{
					Category:      "communication",
					Format:        "speech",
					Label:         "says",
					DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
				},
				Payload: []byte(`{"character_name":"` + actor + `","message":"` + message + `"}`),
			},
		},
	}
}

// TestGatewayHandlerReconnectsOnCoreStreamClose verifies that when the core
// event stream closes while the telnet client is still connected, the handler
// re-subscribes (the durable consumer resumes server-side), shows a reconnecting
// notice, dedupes the single redelivered overlap frame by last event id, and
// continues — rather than terminating (holomush-rsoe6).
// Verifies: I-SURV-1
// Verifies: I-SURV-2
// Verifies: I-SURV-5
func TestGatewayHandlerReconnectsOnCoreStreamClose(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// stream1 carries E1 then closes (core-gone). stream2 resumes, redelivering
	// E1 (the single overlap frame) then E2.
	stream1 := newChanSubscribeStream(ctx)
	stream2 := newChanSubscribeStream(ctx)

	var subCalls atomic.Int32
	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-reconnect",
			Characters:         []*corev1.CharacterSummary{{CharacterId: "char-one", CharacterName: "Alaric"}},
			DefaultCharacterId: "char-one",
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-reconnect",
			CharacterName: "Alaric",
		},
		subscribeFn: func(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
			if subCalls.Add(1) == 1 {
				return stream1, nil
			}
			return stream2, nil
		},
		listCharResp: &corev1.ListCharactersResponse{
			Characters: []*corev1.CharacterSummary{{CharacterId: "char-one", CharacterName: "Alaric"}},
		},
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	// Short refresh interval keeps the reconnect ceiling (8×) modest; this test
	// never reaches it (stream2 subscribes on the first retry).
	limits := DefaultLimits
	limits.LeaseRefreshInterval = 50 * time.Millisecond
	handler := NewGatewayHandler(serverConn, client, limits)

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
	readLinesUntil(t, r, "Alaric") // welcome — first subscription established

	// Deliver E1 on the first stream, then close it (core-gone). Accumulate all
	// rendered lines across both subscriptions so dedup can be asserted on the
	// full transcript.
	all := make([]string, 0, 8)
	stream1.ch <- reconnectEventFrame("E1", "Alaric", "first")
	all = append(all, readLinesUntil(t, r, "first")...)
	close(stream1.ch) // Recv -> io.EOF -> eventCh closes -> !ok branch

	// The handler should emit a reconnecting notice and re-subscribe.
	all = append(all, readLinesUntil(t, r, "Reconnecting")...)
	all = append(all, readLinesUntil(t, r, "Reconnected")...)

	// stream2 redelivers E1 (overlap, must be deduped) then E2 (new).
	stream2.ch <- reconnectEventFrame("E1", "Alaric", "first")
	stream2.ch <- reconnectEventFrame("E2", "Alaric", "second")

	// Reading until "second" collects the post-reconnect lines. The redelivered
	// E1 ("first") must NOT appear again — it was deduped by lastEventID.
	all = append(all, readLinesUntil(t, r, "second")...)

	assert.GreaterOrEqual(t, int(subCalls.Load()), 2, "re-subscribed after core stream closed")

	firstCount := 0
	secondCount := 0
	for _, line := range all {
		if strings.Contains(line, "first") {
			firstCount++
		}
		if strings.Contains(line, "second") {
			secondCount++
		}
	}
	assert.Equal(t, 1, firstCount, "E1 rendered exactly once despite redelivery overlap")
	assert.Equal(t, 1, secondCount, "E2 rendered once after reconnect")

	cancel()
	<-done
}

// errorRecvStream is a CoreService_SubscribeClient whose first Recv returns the
// given error — modelling how the production core surfaces an immediate handler
// failure: grpc-go returns (nonNilStream, nil) from Subscribe and defers the
// handler error to the first Recv, NOT the Subscribe call.
type errorRecvStream struct {
	corev1.CoreService_SubscribeClient
	err error
}

func (e *errorRecvStream) Recv() (*corev1.SubscribeResponse, error) { return nil, e.err }

// TestGatewayHandlerTreatsWireSessionNotFoundAsTerminalAndDisconnects is the
// production-path regression guard for rsoe6.11.1 + rsoe6.11.2. It drives the
// error the way the fixed server produces it: the resubscribe stream's first
// Recv returns codes.Unauthenticated (the wire form of SESSION_NOT_FOUND, set
// by subscribeSessionNotFound). trySubscribe consumes that first frame
// synchronously and runs the recv error through holoGRPC.TranslateSubscribeErr,
// so resubscribe MUST classify it as terminal — return the client to the picker
// (enter selectMode, clear the session) AND issue a best-effort Disconnect to
// release the dead core-side connection (rsoe6.11.2) — rather than retrying for
// the full reconnect ceiling.
//
// This is the test the pre-fix code could not pass: before FIX 1 the wire code
// was codes.Unknown (bare oops, no GRPCStatus), TranslateSubscribeErr would have
// classified it RPC_FAILED, and resubscribe would have retried to the ceiling
// instead of returning to the picker; and before FIX 3 no Disconnect was issued.
// Verifies: I-SURV-1
func TestGatewayHandlerTreatsWireSessionNotFoundAsTerminalAndDisconnects(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// stream1 delivers one event then closes (core-gone) → triggers resubscribe.
	stream1 := newChanSubscribeStream(ctx)

	var subCalls atomic.Int32
	client := &mockCoreClient{
		// Single-char auto-select so `connect` establishes the session and the
		// first subscription directly (mirrors TestGatewayHandlerReconnectsOnCoreStreamClose).
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-reaped",
			Characters:         []*corev1.CharacterSummary{{CharacterId: "char-one", CharacterName: "Alaric"}},
			DefaultCharacterId: "char-one",
		},
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-reaped",
			CharacterName: "Alaric",
		},
		subscribeFn: func(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
			if subCalls.Add(1) == 1 {
				return stream1, nil
			}
			// Resubscribe: the reaped session crosses the wire as
			// codes.Unauthenticated (production shape post server-fix). The
			// error surfaces on the FIRST Recv, exactly as grpc-go does it.
			return &errorRecvStream{err: status.Error(codes.Unauthenticated, "session not found")}, nil
		},
		listCharResp: &corev1.ListCharactersResponse{
			Characters: []*corev1.CharacterSummary{{CharacterId: "char-one", CharacterName: "Alaric"}},
		},
		discResp: &corev1.DisconnectResponse{Success: true},
	}

	// Short refresh interval keeps the reconnect ceiling (8×) modest; a terminal
	// classification returns well before it, so the test is fast.
	limits := DefaultLimits
	limits.LeaseRefreshInterval = 50 * time.Millisecond
	handler := NewGatewayHandler(serverConn, client, limits)

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
	readLinesUntil(t, r, "Alaric") // welcome — first subscription established

	// Deliver one event, then close stream1 (core-gone) → resubscribe fires.
	// A distinct actor name (vs the other reconnect tests' "Alaric") keeps the
	// reconnectEventFrame helper's actor parameter genuinely varied across call
	// sites.
	stream1.ch <- reconnectEventFrame("E1", "Brenna", "first")
	readLinesUntil(t, r, "first")

	// Re-arm the I/O deadline: withDeadline sets a single absolute 2s window at
	// setup; the auth handshake + first event consumed part of it, and the
	// reconnect backoff that follows needs its own fresh window.
	reset()
	reset2 := withDeadline(t, clientConn)
	defer reset2()

	close(stream1.ch)

	// resubscribe hits codes.Unauthenticated on the first Recv → terminal:
	// return to the character picker rather than retrying to the ceiling. The
	// "Returning to character selection" line is emitted AFTER the best-effort
	// Disconnect in the Handle goroutine, so once we've read it the Disconnect
	// fields are already set (happens-before via the telnet write) and can be
	// read without a data race. Read on to "Your characters:" to confirm the
	// picker is actually shown.
	readLinesUntil(t, r, "Returning to character selection")
	readLinesUntil(t, r, "Your characters:")

	// Cancel + join so the Handle goroutine is fully stopped before we read its
	// fields, eliminating any -race window on the mock's unsynchronized fields.
	cancel()
	<-done

	// rsoe6.11.2: a best-effort Disconnect for the OLD connection must have been
	// issued before the session fields were cleared, so the core-side
	// session_connections row is released rather than leaked.
	require.NotNil(t, client.lastDisconnectReq,
		"abandoned reconnect must issue a Disconnect to release the dead connection (rsoe6.11.2)")
	assert.Equal(t, "sess-reaped", client.lastDisconnectReq.GetSessionId(),
		"Disconnect must target the reaped session")
	assert.Equal(t, "tok-reaped", client.lastDisconnectReq.GetPlayerSessionToken(),
		"Disconnect must carry the player session token so the ownership gate passes")

	// Terminal, not retried-to-ceiling: exactly one resubscribe attempt fired
	// (2 total Subscribe calls: initial + the one terminal resubscribe).
	assert.Equal(t, int32(2), subCalls.Load(),
		"terminal SESSION_NOT_FOUND must not retry; exactly one resubscribe attempt")
}

// TestSceneActivityLineCoalescesPerScenePerWindow drives the SCENE_ACTIVITY
// render decision directly: N frames for the same scene within one debounce
// window render exactly one leader; a frame after the window renders a second;
// distinct scenes debounce independently.
func TestSceneActivityLineCoalescesPerScenePerWindow(t *testing.T) {
	h := &GatewayHandler{}
	base := time.Now()

	// First frame for scene A renders.
	if got := h.sceneActivityLine("sceneA", base); got == "" {
		t.Fatal("first SCENE_ACTIVITY frame for a scene must render a leader")
	}
	// N more frames within the window coalesce to nothing.
	for i := 1; i <= 5; i++ {
		within := base.Add(sceneNudgeWindow - time.Second)
		if got := h.sceneActivityLine("sceneA", within); got != "" {
			t.Errorf("frame %d within window rendered %q, want coalesced (empty)", i, got)
		}
	}
	// A frame at/after the window boundary renders again.
	if got := h.sceneActivityLine("sceneA", base.Add(sceneNudgeWindow)); got == "" {
		t.Error("SCENE_ACTIVITY frame after the debounce window must render a second leader")
	}
	// A different scene in the same window renders its own leader (per-scene).
	if got := h.sceneActivityLine("sceneB", base); got == "" {
		t.Error("debounce is per scene_id: a distinct scene must render its own leader")
	}
}

// Verifies: INV-SCENE-70
//
// TestSceneActivityLineCarriesOnlySceneID asserts the telnet SCENE_ACTIVITY
// render path emits exactly the content-free gamenotice leader for the frame's
// scene id and carries no scene title or pose/content text — telnet privacy
// parity with INV-SCENE-62. The render function's only input is the scene id,
// so it is structurally incapable of leaking scene content.
func TestSceneActivityLineCarriesOnlySceneID(t *testing.T) {
	h := &GatewayHandler{}
	sceneID := "01SCENEXYZ"

	got := h.sceneActivityLine(sceneID, time.Now())

	require.Equal(t, gamenotice.Activity(sceneID), got,
		"rendered line must equal the content-free gamenotice leader")
	assert.Equal(t, "[>GAME: Scene #01SCENEXYZ has new activity]", got,
		"the nudge carries only the scene id, no title or pose content")
}
