// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	holoGRPC "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/web/v1/webv1connect"
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

	subStream corev1.CoreService_SubscribeClient
	subErr    error

	discResp *corev1.DisconnectResponse
	discErr  error

	cmdHistory       []string
	cmdHistoryErr    error // application-level failure (Success=false)
	cmdHistoryRPCErr error // transport/RPC-level failure (nil response)

	// Auth RPC fields
	authPlayerResp     *corev1.AuthenticatePlayerResponse
	authPlayerErr      error
	selectCharResp     *corev1.SelectCharacterResponse
	selectCharErr      error
	createPlayerResp   *corev1.CreatePlayerResponse
	createPlayerErr    error
	createCharResp     *corev1.CreateCharacterResponse
	createCharErr      error
	listCharsResp      *corev1.ListCharactersResponse
	listCharsErr       error
	logoutResp         *corev1.LogoutResponse
	logoutErr          error
	reqPwResetResp     *corev1.RequestPasswordResetResponse
	reqPwResetErr      error
	confirmPwResetResp *corev1.ConfirmPasswordResetResponse
	confirmPwResetErr  error
}

func (m *mockCoreClient) Authenticate(_ context.Context, _ *corev1.AuthenticateRequest) (*corev1.AuthenticateResponse, error) {
	return m.authResp, m.authErr
}

func (m *mockCoreClient) HandleCommand(_ context.Context, _ *corev1.HandleCommandRequest) (*corev1.HandleCommandResponse, error) {
	return m.cmdResp, m.cmdErr
}

func (m *mockCoreClient) Subscribe(_ context.Context, _ *corev1.SubscribeRequest) (corev1.CoreService_SubscribeClient, error) {
	return m.subStream, m.subErr
}

func (m *mockCoreClient) Disconnect(_ context.Context, _ *corev1.DisconnectRequest) (*corev1.DisconnectResponse, error) {
	return m.discResp, m.discErr
}

func (m *mockCoreClient) GetCommandHistory(_ context.Context, _ *corev1.GetCommandHistoryRequest) (*corev1.GetCommandHistoryResponse, error) {
	if m.cmdHistoryRPCErr != nil {
		return nil, m.cmdHistoryRPCErr
	}
	if m.cmdHistoryErr != nil {
		//nolint:nilerr // intentional: simulates application-level failure, not RPC error
		return &corev1.GetCommandHistoryResponse{
			Meta:    &corev1.ResponseMeta{},
			Success: false,
			Error:   m.cmdHistoryErr.Error(),
		}, nil
	}
	return &corev1.GetCommandHistoryResponse{
		Meta:     &corev1.ResponseMeta{},
		Success:  true,
		Commands: m.cmdHistory,
	}, nil
}

func (m *mockCoreClient) AuthenticatePlayer(_ context.Context, _ *corev1.AuthenticatePlayerRequest) (*corev1.AuthenticatePlayerResponse, error) {
	return m.authPlayerResp, m.authPlayerErr
}

func (m *mockCoreClient) SelectCharacter(_ context.Context, _ *corev1.SelectCharacterRequest) (*corev1.SelectCharacterResponse, error) {
	return m.selectCharResp, m.selectCharErr
}

func (m *mockCoreClient) CreatePlayer(_ context.Context, _ *corev1.CreatePlayerRequest) (*corev1.CreatePlayerResponse, error) {
	return m.createPlayerResp, m.createPlayerErr
}

func (m *mockCoreClient) CreateCharacter(_ context.Context, _ *corev1.CreateCharacterRequest) (*corev1.CreateCharacterResponse, error) {
	return m.createCharResp, m.createCharErr
}

func (m *mockCoreClient) ListCharacters(_ context.Context, _ *corev1.ListCharactersRequest) (*corev1.ListCharactersResponse, error) {
	return m.listCharsResp, m.listCharsErr
}

func (m *mockCoreClient) RequestPasswordReset(_ context.Context, _ *corev1.RequestPasswordResetRequest) (*corev1.RequestPasswordResetResponse, error) {
	return m.reqPwResetResp, m.reqPwResetErr
}

func (m *mockCoreClient) ConfirmPasswordReset(_ context.Context, _ *corev1.ConfirmPasswordResetRequest) (*corev1.ConfirmPasswordResetResponse, error) {
	return m.confirmPwResetResp, m.confirmPwResetErr
}

func (m *mockCoreClient) Logout(_ context.Context, _ *corev1.LogoutRequest) (*corev1.LogoutResponse, error) {
	return m.logoutResp, m.logoutErr
}

func TestHandler_Login_Success(t *testing.T) {
	client := &mockCoreClient{
		authResp: &corev1.AuthenticateResponse{
			Success:       true,
			SessionId:     "sess-abc",
			CharacterName: "Guest-1",
		},
	}
	h := NewHandler(client)

	resp, err := h.Login(context.Background(), connect.NewRequest(&webv1.LoginRequest{
		Username: "guest",
		Password: "",
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Equal(t, "sess-abc", resp.Msg.GetSessionId())
	assert.Equal(t, "Guest-1", resp.Msg.GetCharacterName())
	assert.Empty(t, resp.Msg.GetErrorMessage())
}

func TestHandler_Login_Failure(t *testing.T) {
	client := &mockCoreClient{
		authResp: &corev1.AuthenticateResponse{
			Success: false,
			Error:   "invalid credentials",
		},
	}
	h := NewHandler(client)

	resp, err := h.Login(context.Background(), connect.NewRequest(&webv1.LoginRequest{
		Username: "user",
		Password: "wrong",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.NotEmpty(t, resp.Msg.GetErrorMessage())
	assert.Empty(t, resp.Msg.GetSessionId())
}

func TestHandler_Login_RPCError(t *testing.T) {
	client := &mockCoreClient{
		authErr: errors.New("connection refused"),
	}
	h := NewHandler(client)

	resp, err := h.Login(context.Background(), connect.NewRequest(&webv1.LoginRequest{
		Username: "guest",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.NotEmpty(t, resp.Msg.GetErrorMessage())
}

func TestHandler_SendCommand_Success(t *testing.T) {
	client := &mockCoreClient{
		cmdResp: &corev1.HandleCommandResponse{
			Success: true,
		},
	}
	h := NewHandler(client)

	resp, err := h.SendCommand(context.Background(), connect.NewRequest(&webv1.SendCommandRequest{
		SessionId: "sess-abc",
		Text:      "say hello",
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
}

func TestHandler_Disconnect_Success(t *testing.T) {
	client := &mockCoreClient{
		discResp: &corev1.DisconnectResponse{Success: true},
	}
	h := NewHandler(client)

	resp, err := h.Disconnect(context.Background(), connect.NewRequest(&webv1.DisconnectRequest{
		SessionId: "sess-abc",
	}))
	require.NoError(t, err)
	assert.NotNil(t, resp.Msg)
}

func TestHandler_Disconnect_RPCError(t *testing.T) {
	client := &mockCoreClient{
		discErr: errors.New("core unavailable"),
	}
	h := NewHandler(client)

	// Best-effort: error is logged, not returned.
	resp, err := h.Disconnect(context.Background(), connect.NewRequest(&webv1.DisconnectRequest{
		SessionId: "sess-xyz",
	}))
	require.NoError(t, err)
	assert.NotNil(t, resp.Msg)
}

func TestHandler_GetCommandHistory_Success(t *testing.T) {
	client := &mockCoreClient{
		cmdHistory: []string{"look", "say hello", "go north"},
	}
	h := NewHandler(client)

	resp, err := h.GetCommandHistory(context.Background(), connect.NewRequest(&webv1.GetCommandHistoryRequest{
		SessionId: "sess-abc",
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{"look", "say hello", "go north"}, resp.Msg.GetCommands())
}

func TestHandler_GetCommandHistory_RPCError(t *testing.T) {
	client := &mockCoreClient{
		cmdHistoryRPCErr: errors.New("rpc error"),
	}
	h := NewHandler(client)

	resp, err := h.GetCommandHistory(context.Background(), connect.NewRequest(&webv1.GetCommandHistoryRequest{
		SessionId: "sess-abc",
	}))
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.GetCommands())
}

func TestHandler_GetCommandHistory_NotSuccess(t *testing.T) {
	client := &mockCoreClient{
		cmdHistoryErr: errors.New("session not found"),
	}
	h := NewHandler(client)

	resp, err := h.GetCommandHistory(context.Background(), connect.NewRequest(&webv1.GetCommandHistoryRequest{
		SessionId: "sess-abc",
	}))
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.GetCommands(), "non-success response should return empty commands")
}

func TestNewHandler_WithOptions(t *testing.T) {
	store := &mockSessionStore{}
	h := NewHandler(&mockCoreClient{}, WithSessionStore(store))
	assert.NotNil(t, h.sessionStore)
}

// mockSubscribeStream is a test double for corev1.CoreService_SubscribeClient.
// It returns pre-configured responses from a channel, then io.EOF.
type mockSubscribeStream struct {
	responses []*corev1.SubscribeResponse
	idx       int
}

func (m *mockSubscribeStream) Recv() (*corev1.SubscribeResponse, error) {
	if m.idx >= len(m.responses) {
		return nil, io.EOF
	}
	r := m.responses[m.idx]
	m.idx++
	return r, nil
}

func (m *mockSubscribeStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockSubscribeStream) Trailer() metadata.MD         { return nil }
func (m *mockSubscribeStream) CloseSend() error             { return nil }
func (m *mockSubscribeStream) Context() context.Context     { return context.Background() }
func (m *mockSubscribeStream) SendMsg(_ any) error          { return nil }
func (m *mockSubscribeStream) RecvMsg(_ any) error          { return nil }

// newStreamEventsServer starts an httptest server with the web handler and
// returns a connect client pointing at it, plus a cleanup function.
func newStreamEventsServer(t *testing.T, client CoreClient) (webv1connect.WebServiceClient, func()) {
	t.Helper()
	handler := NewHandler(client)
	_, h := webv1connect.NewWebServiceHandler(handler)
	srv := httptest.NewServer(h)
	wsc := webv1connect.NewWebServiceClient(http.DefaultClient, srv.URL)
	return wsc, srv.Close
}

func TestStreamEvents_ForwardsControlFrame(t *testing.T) {
	sub := &mockSubscribeStream{
		responses: []*corev1.SubscribeResponse{
			{
				Frame: &corev1.SubscribeResponse_Control{
					Control: &corev1.ControlFrame{
						Signal:  corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE,
						Message: "replay done",
					},
				},
			},
		},
	}
	client := &mockCoreClient{subStream: sub}
	wsc, cleanup := newStreamEventsServer(t, client)
	defer cleanup()

	stream, err := wsc.StreamEvents(context.Background(), connect.NewRequest(&webv1.StreamEventsRequest{
		SessionId: "sess-test",
	}))
	require.NoError(t, err)
	defer stream.Close()

	ok := stream.Receive()
	require.True(t, ok, "expected to receive a response")

	msg := stream.Msg()
	require.NotNil(t, msg)
	ctrl := msg.GetControl()
	require.NotNil(t, ctrl, "expected a ControlFrame, got: %v", msg)
	assert.Equal(t, webv1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE, ctrl.GetSignal())
	assert.Equal(t, "replay done", ctrl.GetMessage())
}

func TestStreamEvents_StreamClosedEndsStream(t *testing.T) {
	sub := &mockSubscribeStream{
		responses: []*corev1.SubscribeResponse{
			{
				Frame: &corev1.SubscribeResponse_Control{
					Control: &corev1.ControlFrame{
						Signal: corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED,
					},
				},
			},
		},
	}
	client := &mockCoreClient{subStream: sub}
	wsc, cleanup := newStreamEventsServer(t, client)
	defer cleanup()

	stream, err := wsc.StreamEvents(context.Background(), connect.NewRequest(&webv1.StreamEventsRequest{
		SessionId: "sess-test",
	}))
	require.NoError(t, err)
	defer stream.Close()

	// First receive: the STREAM_CLOSED control frame.
	ok := stream.Receive()
	require.True(t, ok, "expected to receive STREAM_CLOSED frame")
	ctrl := stream.Msg().GetControl()
	require.NotNil(t, ctrl)
	assert.Equal(t, webv1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED, ctrl.GetSignal())

	// Stream should now be done.
	ok = stream.Receive()
	assert.False(t, ok, "stream should be closed after STREAM_CLOSED")
}

// mockSessionStore is a minimal test double for session.Store.
// Only Get and GetCommandHistory are implemented; all other methods panic.
type mockSessionStore struct {
	commandHistory    []string
	commandHistoryErr error
	getErr            error
}

func (m *mockSessionStore) Get(_ context.Context, id string) (*session.Info, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return &session.Info{ID: id, Status: session.StatusActive}, nil
}

func (m *mockSessionStore) Set(_ context.Context, id string, _ *session.Info) error {
	return fmt.Errorf("mockSessionStore.Set(%q): not implemented", id)
}

func (m *mockSessionStore) Delete(_ context.Context, id string, _ string) error {
	return fmt.Errorf("mockSessionStore.Delete(%q): not implemented", id)
}

func (m *mockSessionStore) WatchSession(_ context.Context, sessionID string) (<-chan session.Event, error) {
	return nil, fmt.Errorf("mockSessionStore.WatchSession(%q): not implemented", sessionID)
}

func (m *mockSessionStore) FindByCharacter(_ context.Context, id ulid.ULID) (*session.Info, error) {
	return nil, fmt.Errorf("mockSessionStore.FindByCharacter(%q): not implemented", id)
}

func (m *mockSessionStore) ListByPlayer(_ context.Context, id ulid.ULID) ([]*session.Info, error) {
	return nil, fmt.Errorf("mockSessionStore.ListByPlayer(%q): not implemented", id)
}

func (m *mockSessionStore) ListExpired(_ context.Context) ([]*session.Info, error) {
	return nil, errors.New("mockSessionStore.ListExpired: not implemented")
}

func (m *mockSessionStore) UpdateStatus(_ context.Context, id string, _ session.Status, _ *time.Time, _ *time.Time) error {
	return fmt.Errorf("mockSessionStore.UpdateStatus(%q): not implemented", id)
}

func (m *mockSessionStore) ReattachCAS(_ context.Context, id string) (bool, error) {
	return false, fmt.Errorf("mockSessionStore.ReattachCAS(%q): not implemented", id)
}

func (m *mockSessionStore) UpdateCursors(_ context.Context, id string, _ map[string]ulid.ULID) error {
	return fmt.Errorf("mockSessionStore.UpdateCursors(%q): not implemented", id)
}

func (m *mockSessionStore) AppendCommand(_ context.Context, id string, _ string, _ int) error {
	return fmt.Errorf("mockSessionStore.AppendCommand(%q): not implemented", id)
}

func (m *mockSessionStore) GetCommandHistory(_ context.Context, _ string) ([]string, error) {
	return m.commandHistory, m.commandHistoryErr
}

func (m *mockSessionStore) AddConnection(_ context.Context, conn *session.Connection) error {
	return fmt.Errorf("mockSessionStore.AddConnection(%q): not implemented", conn.ID)
}

func (m *mockSessionStore) RemoveConnection(_ context.Context, id ulid.ULID) error {
	return fmt.Errorf("mockSessionStore.RemoveConnection(%q): not implemented", id)
}

func (m *mockSessionStore) CountConnections(_ context.Context, id string) (int, error) {
	return 0, fmt.Errorf("mockSessionStore.CountConnections(%q): not implemented", id)
}

func (m *mockSessionStore) CountConnectionsByType(_ context.Context, id string, _ string) (int, error) {
	return 0, fmt.Errorf("mockSessionStore.CountConnectionsByType(%q): not implemented", id)
}

func (m *mockSessionStore) UpdateGridPresent(_ context.Context, id string, _ bool) error {
	return fmt.Errorf("mockSessionStore.UpdateGridPresent(%q): not implemented", id)
}

func (m *mockSessionStore) ListActiveByLocation(_ context.Context, _ ulid.ULID) ([]*session.Info, error) {
	return nil, fmt.Errorf("mockSessionStore.ListActiveByLocation: not implemented")
}
